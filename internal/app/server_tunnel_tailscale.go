//go:build !ios && !android

package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/logtail"
	"tailscale.com/tsnet"
)

// The administrator's own Tailscale, built in (decided 2026-10-19): the
// server runs a Tailscale device of its own — nothing is installed on the
// computer — signed in to the administrator's Tailscale account, and takes a
// public HTTPS address from it by Funnel (name.tailnet.ts.net). Connections
// pass Tailscale but are decrypted here, in this process. The device's state
// sits in the data folder, so a backup brings the same device and address
// back. Tailscale's own diagnostic log upload is turned off: Leafen sends no
// usage data. While Tailscale waits for the administrator (signing in,
// allowing the public address) or can't be reached, the server doesn't turn
// to a quick tunnel by itself: no third party the administrator didn't pick
// carries its traffic.

const tailscaleBuiltIn = true

const (
	tailscaleCheckEvery = 2 * time.Second
	// tailscaleNoteEvery notes the certificate Tailscale holds now, so one it
	// renewed counts as the server's own before the logs show it.
	tailscaleNoteEvery = time.Hour
)

var tailscaleLogsOff sync.Once

// tailscaleNodeMu is held by whoever runs the device — the runner or an old
// address's forwarding (server_address_forwarding.go) — never both at once.
var tailscaleNodeMu sync.Mutex

type tailscaleSourceKey struct{}

// runTailscale keeps the Tailscale address up until it ends, saying why it
// did.
func (tunnel *serverTunnel) runTailscale(ctx context.Context) tunnelFailure {
	tailscaleNodeMu.Lock()
	defer tailscaleNodeMu.Unlock()
	if ctx.Err() != nil {
		return failedTunnel(tunnelFailureStopped, "Tailscale stopped.")
	}
	tailscaleLogsOff.Do(logtail.Disable)
	stateDir, ephemeral := tunnel.tailscaleState()
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return failedTunnel(tunnelFailureProgram, "Tailscale's folder couldn't be made: "+err.Error())
	}
	hostname := tailscaleHostname(tunnel.app)
	node := &tsnet.Server{Dir: stateDir, Hostname: hostname, Ephemeral: ephemeral, UserLogf: func(string, ...any) {}}
	defer node.Close()
	if err := node.Start(); err != nil {
		return failedTunnel(tunnelFailureProgram, "Tailscale didn't start: "+err.Error())
	}
	client, err := node.LocalClient()
	if err != nil {
		return failedTunnel(tunnelFailureProgram, "Tailscale didn't start: "+err.Error())
	}
	tunnel.mu.Lock()
	tunnel.tailscaleLogout = client.Logout
	tunnel.mu.Unlock()
	defer func() {
		tunnel.mu.Lock()
		tunnel.tailscaleLogout = nil
		tunnel.mu.Unlock()
	}()
	status := tunnel.waitTailscaleRunning(ctx, client)
	if status == nil {
		return failedTunnel(tunnelFailureStopped, "Tailscale stopped.")
	}
	if status = tunnel.waitTailscaleFunnel(ctx, client, status); status == nil {
		return failedTunnel(tunnelFailureStopped, "Tailscale stopped.")
	}
	address := strings.TrimSuffix(status.Self.DNSName, ".")
	if address == "" {
		return failedTunnel(tunnelFailureStopped, "Tailscale gave no name.")
	}
	if label, _, _ := strings.Cut(address, "."); label != hostname {
		tunnel.setTailscaleWaiting("", "", hostname)
	}
	notes := filepath.Join(tunnel.app.DataDir(), addressWatchFolder, address)
	if err := os.MkdirAll(notes, 0o700); err != nil {
		return failedTunnel(tunnelFailurePublicAddress, err.Error())
	}
	noteAddressName(notes)
	if alert := readAddressCertificateAlert(notes); alert != nil {
		return failedTunnel(tunnelFailureCertificateElsewhere,
			"A certificate for this address came from elsewhere; the public address stays off until an administrator says it is theirs.")
	}
	certificates := &tailscaleCertificates{client: client, notes: notes}
	listener, err := listenTailscaleAddress(node,
		tsnet.FunnelTLSConfig(&tls.Config{GetCertificate: certificates.get, NextProtos: []string{"h2", "http/1.1"}, MinVersion: tls.VersionTLS12}))
	if err != nil {
		return failedTunnel(tunnelFailurePublicAddress, "Tailscale's public address didn't open: "+err.Error())
	}
	defer listener.Close()
	local, err := url.Parse(tunnel.localURL)
	if err != nil {
		return failedTunnel(tunnelFailurePublicAddress, err.Error())
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ended := noTunnelFailure
	var endedOnce sync.Once
	fail := func(reason tunnelFailure) { endedOnce.Do(func() { ended = reason; cancel() }) }
	// The first certificate is fetched while the address opens; the first
	// visitor's handshake would wait for it anyway.
	go tunnel.fetchFirstTailscaleCertificate(ctx, certificates, address)
	go tunnel.watchTailscaleCertificates(ctx, certificates, address, fail)
	server := &http.Server{Handler: tailscaleProxy(local), ReadHeaderTimeout: 30 * time.Second,
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			if source := tailscaleConnSource(conn); source != "" {
				return context.WithValue(ctx, tailscaleSourceKey{}, source)
			}
			return ctx
		}}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fail(failedTunnel(tunnelFailurePublicAddress, "Tailscale's public address stopped: "+err.Error()))
		}
	}()
	defer server.Close()
	// Said on, and taken as the server's address — for links, a phone's QR,
	// forwarding from an earlier one — once the name is in the public DNS
	// (server_tunnel_tailscale_published.go).
	if !tunnel.waitTailscalePublished(ctx, address) {
		if !ended.empty() {
			return ended
		}
		return failedTunnel(tunnelFailureStopped, "Tailscale stopped.")
	}
	// A restore's own device is no address of the server's to keep.
	if !ephemeral {
		if _, err := tunnel.app.DB().NewQuery(`
			UPDATE {{leafen_server_tunnel}} SET [[tailscale_address]] = {:address} WHERE [[id]] = 'tunnel'
		`).Bind(dbx.Params{"address": address}).Execute(); err != nil {
			return failedTunnel(tunnelFailurePublicAddress, "The Tailscale address couldn't be kept: "+err.Error())
		}
	}
	// On first, so the address counts as the server's (noteAddressChange).
	tunnel.set(serverTunnelStateOn, "https://"+address, noTunnelFailure)
	if current, err := loadServerTunnelSettings(tunnel.app); err == nil {
		noteChosenTunnelSettings(current)
		tunnel.noteAddressChange(current)
	}
	tunnel.writeTailscaleStatus("", "", address)
	<-ctx.Done()
	if !ended.empty() {
		return ended
	}
	return failedTunnel(tunnelFailureStopped, "Tailscale stopped.")
}

// tailscaleConnSource is the address a connection to the device's address
// came from, which its requests carry to the server (tailscaleProxy): the
// person's own that Funnel hands on, or a tailnet device's own tailnet
// address (100.64.0.0/10, fd7a:115c:a1e0::/48), its connection's far end —
// which the first setup's same-network rule reads as Tailscale's, as it
// reads one reaching the server's own port (setup_same_network.go, user
// decision 2026-11-19). "" when neither is known — a far end outside those
// addresses too, as one a subnet router passes on: then from outside.
func tailscaleConnSource(conn net.Conn) string {
	secured, ok := conn.(*tls.Conn)
	if !ok {
		return ""
	}
	inner := secured.NetConn()
	if funnel, ok := inner.(*ipn.FunnelConn); ok {
		if funnel.Src.IsValid() {
			return funnel.Src.Addr().String()
		}
		return ""
	}
	remote, err := netip.ParseAddrPort(inner.RemoteAddr().String())
	if address := remote.Addr().Unmap(); err == nil && (tailnetIPv4.Contains(address) || tailnetIPv6.Contains(address)) {
		return address.String()
	}
	return ""
}

// Tailscale's own addresses for a tailnet's devices.
var (
	tailnetIPv4 = netip.MustParsePrefix("100.64.0.0/10")
	tailnetIPv6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// listenTailscaleAddress opens the device's HTTPS address, name.tailnet.ts.net:443,
// for both ways it is reached, on the same certificate: Funnel's, from the
// internet, and the tailnet's own — a device of the administrator's with
// Tailscale on, whose MagicDNS gives the address as the device's tailnet
// address, not Funnel's (2026-11-22: the administrator's own Mac was refused
// while it listened to Funnel alone). Leafen's sign-in stands for both;
// each request reaches the server with the address it came from
// (tailscaleConnSource), judged as any other's.
func listenTailscaleAddress(node *tsnet.Server, options ...tsnet.FunnelOption) (net.Listener, error) {
	return node.ListenFunnel("tcp", ":443", options...)
}

// waitTailscaleRunning waits until the device is signed in, showing where
// the administrator signs in meanwhile; nil once the tunnel ends.
func (tunnel *serverTunnel) waitTailscaleRunning(ctx context.Context, client *local.Client) *ipnstate.Status {
	for {
		status, err := client.StatusWithoutPeers(ctx)
		if err == nil {
			switch status.BackendState {
			case ipn.Running.String():
				if status.Self != nil {
					tunnel.setTailscaleWaiting("", "", "")
					return status
				}
			case ipn.NeedsLogin.String():
				tunnel.setTailscaleWaiting(status.AuthURL, "", "")
			case ipn.NeedsMachineAuth.String():
				// The tailnet asks its admin to approve new devices.
				tunnel.setTailscaleStep(tunnelStageApproveDevice)
				tunnel.setTailscaleWaiting("", tailscaleMachinesPage, "")
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(tailscaleCheckEvery):
		}
	}
}

// tailscaleMachinesPage is where a tailnet's admin approves a new device or
// turns on HTTPS and Funnel when Tailscale names no page of its own.
const tailscaleMachinesPage = "https://login.tailscale.com/admin/machines"

// waitTailscaleFunnel waits until the tailnet lets the device have a public
// address (HTTPS and Funnel), showing where to allow it; nil once the tunnel
// ends.
func (tunnel *serverTunnel) waitTailscaleFunnel(ctx context.Context, client *local.Client, status *ipnstate.Status) *ipnstate.Status {
	for ipn.CheckFunnelAccess(443, status.Self) != nil || len(status.CertDomains) == 0 {
		allow := ""
		if answer, err := client.QueryFeature(ctx, "funnel"); err == nil && answer != nil {
			allow = answer.URL
		}
		if allow == "" {
			allow = tailscaleMachinesPage
		}
		tunnel.setTailscaleStep(tunnelStageAllowFunnel)
		tunnel.setTailscaleWaiting("", allow, "")
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(tailscaleCheckEvery):
		}
		next, err := client.StatusWithoutPeers(ctx)
		if err == nil && next.Self != nil {
			status = next
		}
	}
	tunnel.setTailscaleWaiting("", "", "")
	return status
}

// tailscaleCertificates serves the certificate Tailscale holds, noting each
// new one as the server's own before it's used.
type tailscaleCertificates struct {
	client *local.Client
	notes  string
	mu     sync.Mutex
	noted  map[string]bool
}

func (certificates *tailscaleCertificates) note(der []byte) {
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return
	}
	serial := certificate.SerialNumber.Text(16)
	certificates.mu.Lock()
	defer certificates.mu.Unlock()
	if certificates.noted[serial] {
		return
	}
	if noteAddressCertificate(certificates.notes, der) == nil {
		if certificates.noted == nil {
			certificates.noted = map[string]bool{}
		}
		certificates.noted[serial] = true
	}
}

func (certificates *tailscaleCertificates) get(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	certificate, err := certificates.client.GetCertificate(hello)
	if err == nil && certificate != nil && len(certificate.Certificate) > 0 {
		certificates.note(certificate.Certificate[0])
		rememberServedCertificate(hello.ServerName, certificate.Certificate[0])
	}
	return certificate, err
}

// noteHeld notes the certificate Tailscale holds for the address now,
// getting one if it has none.
func (certificates *tailscaleCertificates) noteHeld(ctx context.Context, address string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	certificatePEM, _, err := certificates.client.CertPair(ctx, address)
	if err != nil {
		return
	}
	if block, _ := pem.Decode(certificatePEM); block != nil {
		certificates.note(block.Bytes)
		rememberServedCertificate(address, block.Bytes)
	}
}

// watchTailscaleCertificates reads the certificate logs for the address
// (server_address_ct_watch.go). A certificate the server didn't note is
// checked against the one Tailscale holds now — a renewal the server hadn't
// served yet is its own — and otherwise closes the public address and tells
// the administrators.
func (tunnel *serverTunnel) watchTailscaleCertificates(ctx context.Context, certificates *tailscaleCertificates, address string, fail func(tunnelFailure)) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(tailscaleNoteEvery):
				certificates.noteHeld(ctx, address)
			}
		}
	}()
	for ctx.Err() == nil {
		var found *addressCertificateAlert
		watchAddressCertificates(ctx, certificates.notes, address, func(alert *addressCertificateAlert) { found = alert })
		if found == nil {
			return
		}
		certificates.noteHeld(ctx, address)
		if issued := readAddressIssued(certificates.notes); containsSerial(issued.Serials, found.Serial) {
			continue
		}
		raiseAddressCertificateAlert(tunnel.app, certificates.notes, address, found)
		fail(failedTunnel(tunnelFailureCertificateElsewhere,
			"A certificate for this address came from elsewhere; the public address is off until an administrator says it is theirs."))
		return
	}
}

func containsSerial(serials []string, serial string) bool {
	for _, held := range serials {
		if held == normalSerial(serial) {
			return true
		}
	}
	return false
}

// tailscaleProxy hands the address's requests to the server with the
// address they came from (tailscaleConnSource); only this process's own
// paths know the door's secret (server_tunnel_door.go).
func tailscaleProxy(local *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(local)
	proxy.FlushInterval = -1
	base := proxy.Director
	proxy.Director = func(request *http.Request) {
		base(request)
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Header.Del(tunnelClientAddressHeader)
		request.Header.Del("Cf-Connecting-Ip")
		if source, ok := request.Context().Value(tailscaleSourceKey{}).(string); ok && source != "" {
			request.Header.Set(tunnelClientAddressHeader, source)
		}
		request.Header.Set(tunnelDoorHeader, tunnelDoorSecret)
	}
	return proxy
}

// serveForwardingTailscale opens the device's address again — Funnel's and
// the tailnet's (listenTailscaleAddress) — for an earlier Tailscale address
// while it forwards, answering with the redirect; it waits while the runner
// holds the device. It never waits for a sign-in holding the
// device (2026-11-11: an earlier device asking for one held it, so turning
// Tailscale on again stayed opening with no sign-in link): nobody sees a
// forwarding way's sign-in, so a device asking to be signed in or approved,
// or not running within tailscaleForwardUpWait, is let go at once. up is
// told when the device runs, signed in.
func (tunnel *serverTunnel) serveForwardingTailscale(ctx context.Context, handler http.Handler, up func()) error {
	tailscaleNodeMu.Lock()
	defer tailscaleNodeMu.Unlock()
	if ctx.Err() != nil {
		return nil
	}
	tailscaleLogsOff.Do(logtail.Disable)
	node := &tsnet.Server{Dir: filepath.Join(tunnel.app.DataDir(), tailscaleFolderName, "state"), Hostname: tailscaleHostname(tunnel.app),
		UserLogf: func(string, ...any) {}}
	defer node.Close()
	stopClosing := context.AfterFunc(ctx, func() { _ = node.Close() })
	defer stopClosing()
	if err := node.Start(); err != nil {
		return err
	}
	client, err := node.LocalClient()
	if err != nil {
		return err
	}
	if err := waitForwardingTailscaleUp(ctx, client); err != nil || ctx.Err() != nil {
		return err
	}
	up()
	// Running, the device's own wait in ListenFunnel ends at once. Should it
	// ask for a sign-in just then, that wait outlasts even the device's
	// close, so the way lets the device go as it ends without it.
	type funnelOpened struct {
		listener net.Listener
		err      error
	}
	opened := make(chan funnelOpened, 1)
	go func() {
		listener, err := listenTailscaleAddress(node)
		opened <- funnelOpened{listener, err}
	}()
	var listener net.Listener
	select {
	case <-ctx.Done():
		return nil
	case result := <-opened:
		if result.err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return result.err
		}
		listener = result.listener
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

// tailscaleForwardUpWait is how long an earlier address's device may take to
// run again before its forwarding way lets it go and tries later.
const tailscaleForwardUpWait = time.Minute

// waitForwardingTailscaleUp waits until an earlier address's device runs, nil
// also once the way ends; never for a sign-in.
func waitForwardingTailscaleUp(ctx context.Context, client *local.Client) error {
	deadline := time.Now().Add(tailscaleForwardUpWait)
	for {
		status, err := client.StatusWithoutPeers(ctx)
		if err == nil {
			switch status.BackendState {
			case ipn.Running.String():
				if status.Self != nil {
					return nil
				}
			case ipn.NeedsLogin.String(), ipn.NeedsMachineAuth.String():
				return errTailscaleForwardSignedOut
			}
		}
		if time.Now().After(deadline) {
			return errors.New("the earlier Tailscale device didn't come up")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(tailscaleCheckEvery):
		}
	}
}
