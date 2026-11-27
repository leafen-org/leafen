package app

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The ways an earlier address stays reachable while it forwards
// (server_address_forwarding.go): its kept certificate on its own port, or
// Tailscale's earlier device, each run until the address stops forwarding,
// and the kept certificates let go after.

// forwardingCertificates are the kept certificates of earlier addresses that
// forward on a port, for the listener there to pick by name.
func forwardingCertificates(app core.App, port int) []tls.Certificate {
	active := forwardingActive.Load()
	if active == nil {
		return nil
	}
	certificates := []tls.Certificate{}
	for _, forward := range *active {
		if forward.Kind != forwardKindTLS || forward.Port != port {
			continue
		}
		dir := addressTLSDir(app, forward.From)
		if certificate, err := tls.LoadX509KeyPair(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")); err == nil {
			certificates = append(certificates, certificate)
		}
	}
	return certificates
}

// keepForwardingWays keeps the server reachable at earlier addresses it held
// while they forward — their own certificate on their port, Tailscale's old
// device — when the current setting doesn't already, until the server
// closes. It looks again each minute and when the setting changes. With
// remote access off it keeps none (2026-11-11: an old address's Funnel stayed
// open outside under "끔"); turned on again, they forward until their time.
func (tunnel *serverTunnel) keepForwardingWays(ctx context.Context, changed <-chan struct{}) {
	running := map[string]context.CancelFunc{}
	stopAll := func() {
		for key, cancel := range running {
			cancel()
			delete(running, key)
		}
	}
	defer stopAll()
	for {
		wanted := map[string]addressForward{}
		settings := serverTunnelChosen.Load()
		// Nor does a moved or let-go server.
		if active := forwardingActive.Load(); active != nil && settings != nil && settings.Mode != serverTunnelOff && settings.Mode != "" &&
			!remoteAccessHeldBack(tunnel.app) {
			for _, forward := range *active {
				switch {
				case forward.Kind == forwardKindTLS:
					// The current address's own listener serves it on its port.
					if settings.Mode == serverTunnelOwnAddress && addressTLSPort(tunnel.app, settings.Address) == forward.Port {
						continue
					}
					wanted["tls:"+strconv.Itoa(forward.Port)+":"+forward.From] = forward
				case forward.Kind == forwardKindTailscale && settings.Mode != serverTunnelTailscale && tailscaleBuiltIn:
					wanted["tailscale:"+forward.From] = forward
				}
			}
		}
		for key, cancel := range running {
			if _, ok := wanted[key]; !ok {
				cancel()
				delete(running, key)
			}
		}
		for key, forward := range wanted {
			if _, ok := running[key]; ok {
				continue
			}
			wayCtx, cancel := context.WithCancel(ctx)
			running[key] = cancel
			// Waited for as the server stops (server_background.go).
			if tunnel.app != nil {
				serverBackgroundOf(tunnel.app).run("address forwarding way", func(context.Context) { tunnel.runForwardingWay(wayCtx, forward) })
			} else {
				go tunnel.runForwardingWay(wayCtx, forward)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-changed:
		case <-time.After(time.Minute):
			if tunnel.app != nil {
				forwardingMu.Lock()
				active := activeForwards(loadAddressForwarding(tunnel.app), time.Now())
				forwardingActive.Store(&active)
				forwardingMu.Unlock()
				tidyAddressCertificates(tunnel.app, active)
				noticeForwardEnds(tunnel.app)
			}
		}
	}
}

// runForwardingWay keeps one earlier address reachable, answering every
// request there with the redirect — an outside program's call goes on to
// the server through the way in, keeping the address it came to, and is
// answered there (forwardServedInPlace).
func (tunnel *serverTunnel) runForwardingWay(ctx context.Context, forward addressForward) {
	var through http.Handler
	if local, err := url.Parse(tunnel.localURL); err == nil && tunnel.localURL != "" {
		through = ownAddressProxy(local)
		if forward.Kind == forwardKindTailscale {
			through = tailscaleProxy(local)
		}
	}
	redirect := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if through != nil && forwardServedInPlace(request) {
			through.ServeHTTP(writer, request)
			return
		}
		noteForwardUse(tunnel.app, forward.From, forwardUseOf(request))
		allowForwardAcrossOrigins(writer, request)
		http.Redirect(writer, request, strings.TrimRight(forward.To, "/")+request.URL.RequestURI(), http.StatusPermanentRedirect)
	})
	for ctx.Err() == nil {
		var err error
		switch forward.Kind {
		case forwardKindTLS:
			err = serveForwardingTLS(ctx, tunnel.app, forward, redirect)
		case forwardKindTailscale:
			err = tunnel.serveForwardingTailscale(ctx, redirect, func() { forwardSignedOut.Delete(forward.From) })
		}
		if err != nil && tunnel.app != nil {
			tunnel.app.Logger().Warn("an earlier address couldn't keep forwarding", "address", forward.From, "error", err)
		}
		again := forwardingWayRetry
		if errors.Is(err, errTailscaleForwardSignedOut) {
			again = forwardingWaySignedOutRetry
			forwardSignedOut.Store(forward.From, true)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(again):
		}
	}
}

const (
	forwardingWayRetry = 30 * time.Second
	// A signed-out device is signed in again only by turning Tailscale on, so
	// its way tries seldom: each try starts the device and asks Tailscale for
	// a sign-in.
	forwardingWaySignedOutRetry = 15 * time.Minute
)

// errTailscaleForwardSignedOut: an earlier Tailscale address's device asks to
// be signed in or approved again, which its forwarding way never waits for
// (server_tunnel_tailscale.go).
var errTailscaleForwardSignedOut = errors.New("the earlier Tailscale device asks to be logged in or approved again " +
	"(turning Tailscale on does it); until then its address doesn't forward")

// serveForwardingTLS takes HTTPS on an earlier address's port with its kept
// certificate, until the forward ends.
func serveForwardingTLS(ctx context.Context, app core.App, forward addressForward, handler http.Handler) error {
	dir := addressTLSDir(app, forward.From)
	certificate, err := tls.LoadX509KeyPair(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"))
	if err != nil {
		return err
	}
	listener, err := tls.Listen("tcp", net.JoinHostPort("", strconv.Itoa(forward.Port)),
		&tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	if err := server.Serve(listener); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// noteAddressChange records the setting's address and tells the loop
// keeping earlier addresses reachable. A Tailscale address is recorded — and
// earlier ones forwarded to it — only once it is public and on (runTailscale
// says so first): applied before that, turned on again, the setting forwarded
// the old address to one still opening (2026-11-11), as the first one had.
func (tunnel *serverTunnel) noteAddressChange(settings serverTunnelSettings) {
	if settings.Mode == serverTunnelTailscale && runningTunnelOrigin() != "https://"+settings.TailscaleAddress {
		settings.TailscaleAddress = ""
	}
	noteCanonicalChange(tunnel.app, settings)
	if tunnel.forwardingChanged != nil {
		select {
		case tunnel.forwardingChanged <- struct{}{}:
		default:
		}
	}
}

// tidyAddressCertificates lets the kept certificate of an address go once it
// is neither the current address nor forwarding any more.
func tidyAddressCertificates(app core.App, active []addressForward) {
	keep := map[string]bool{}
	if settings := serverTunnelChosen.Load(); settings != nil && settings.Mode == serverTunnelOwnAddress {
		keep[strings.ToLower(hostOf(settings.Address))] = true
	}
	for _, forward := range active {
		keep[strings.ToLower(hostOf(forward.From))] = true
	}
	root := filepath.Join(app.DataDir(), addressTLSFolder)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && !keep[entry.Name()] {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}
