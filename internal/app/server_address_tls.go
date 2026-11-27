package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The administrator's own address with a certificate of their own (decided
// 2026-10-19, "when Tailscale can't be used"): on a closed network with a
// company certificate, or behind port forwarding with no proxy, the server
// takes HTTPS itself on the chosen port with that certificate and hands the
// requests on to Leafen's own entrance. Without a certificate the address is
// served by the administrator's own proxy (the "address" mode alone). Set by
// the agent's settings tool, the command line or the screen; the server then
// checks the address — its name resolves, and what certificate answers
// there, until when, and whether this computer trusts it.

const (
	addressTLSFolder  = "address-tls"
	addressTLSPortKey = "port.json"
)

// serverAddressCheck is what the server found at its own address.
type serverAddressCheck struct {
	// Address is the address looked at; a check of another is never shown.
	Address   string   `json:"address"`
	CheckedAt int64    `json:"checkedAt"`
	Resolves  []string `json:"resolves,omitempty"`
	Names     []string `json:"names,omitempty"`
	NotAfter  int64    `json:"notAfter,omitzero"`
	Trusted   bool     `json:"trusted"`
	// Problem names what's wrong first: "dns" (the name doesn't resolve),
	// "unreachable", "name" (the certificate is for another name),
	// "expired", "untrusted" (this computer doesn't trust its issuer).
	Problem string `json:"problem,omitempty"`
}

// addressTLSDir is where an address's own certificate is kept, a folder per
// name, so an earlier address keeps its certificate while it forwards
// (server_address_forwarding.go).
func addressTLSDir(app core.App, address string) string {
	return filepath.Join(app.DataDir(), addressTLSFolder, strings.ToLower(hostOf(address)))
}

// addressTLSPort is the port the server takes HTTPS on with an address's
// own certificate, 0 without one.
func addressTLSPort(app core.App, address string) int {
	if hostOf(address) == "" {
		return 0
	}
	dir := addressTLSDir(app, address)
	if _, err := os.Stat(filepath.Join(dir, "cert.pem")); err != nil {
		return 0
	}
	kept := struct {
		Port int `json:"port"`
	}{}
	if data, err := os.ReadFile(filepath.Join(dir, addressTLSPortKey)); err == nil {
		_ = json.Unmarshal(data, &kept)
	}
	if kept.Port <= 0 || kept.Port > 65535 {
		return 443
	}
	return kept.Port
}

// setOwnAddress makes an https address the server's own, with a certificate
// and key (PEM) it serves itself on a port, or none for the administrator's
// proxy. It refuses a certificate that doesn't cover the address's name, has
// expired or doesn't match its key.
func setOwnAddress(app core.App, address, certificatePEM, keyPEM string, port int) (serverTunnelSettings, error) {
	address = strings.TrimRight(strings.TrimSpace(address), "/")
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return serverTunnelSettings{}, errors.New("give the address as https://your-domain")
	}
	dir := addressTLSDir(app, parsed.String())
	if strings.TrimSpace(certificatePEM) != "" {
		pair, err := tls.X509KeyPair([]byte(certificatePEM), []byte(keyPEM))
		if err != nil {
			return serverTunnelSettings{}, fmt.Errorf("the certificate and key don't go together: %w", err)
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return serverTunnelSettings{}, err
		}
		if err := leaf.VerifyHostname(parsed.Hostname()); err != nil {
			return serverTunnelSettings{}, fmt.Errorf("the certificate isn't for %s", parsed.Hostname())
		}
		if time.Now().After(leaf.NotAfter) {
			return serverTunnelSettings{}, errors.New("the certificate has expired")
		}
		if port <= 0 || port > 65535 {
			port = 443
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return serverTunnelSettings{}, err
		}
		portData, _ := json.Marshal(map[string]int{"port": port})
		for name, data := range map[string][]byte{"cert.pem": []byte(certificatePEM), "key.pem": []byte(keyPEM), addressTLSPortKey: portData} {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				return serverTunnelSettings{}, err
			}
		}
	} else {
		for _, name := range []string{"cert.pem", "key.pem", addressTLSPortKey} {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return serverTunnelSettings{}, err
			}
		}
	}
	updated := time.Now().UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_server_tunnel}} ([[id]], [[mode]], [[address]], [[updated]]) VALUES ('tunnel', 'address', {:address}, {:updated})
		ON CONFLICT ([[id]]) DO UPDATE SET [[mode]] = 'address', [[address]] = excluded.[[address]], [[updated]] = excluded.[[updated]]
	`).Bind(dbx.Params{"address": parsed.String(), "updated": updated}).Execute(); err != nil {
		return serverTunnelSettings{}, err
	}
	return loadServerTunnelSettings(app)
}

// runOwnAddressTLS takes HTTPS on the address's port with the kept
// certificate until the tunnel ends, saying why it stopped.
func (tunnel *serverTunnel) runOwnAddressTLS(ctx context.Context, settings serverTunnelSettings) tunnelFailure {
	dir := addressTLSDir(tunnel.app, settings.Address)
	certificate, err := tls.LoadX509KeyPair(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"))
	if err != nil {
		return failedTunnel(tunnelFailureOwnAddress, "The address's certificate couldn't be read: "+err.Error())
	}
	if len(certificate.Certificate) > 0 {
		rememberServedCertificate(hostOf(settings.Address), certificate.Certificate[0])
	}
	port := addressTLSPort(tunnel.app, settings.Address)
	// An earlier address forwarding on the same port answers here with its
	// own certificate, picked by name.
	certificates := append([]tls.Certificate{certificate}, forwardingCertificates(tunnel.app, port)...)
	listener, err := tls.Listen("tcp", net.JoinHostPort("", strconv.Itoa(port)), &tls.Config{Certificates: certificates,
		NextProtos: []string{"h2", "http/1.1"}, MinVersion: tls.VersionTLS12})
	if err != nil {
		return failedTunnel(tunnelFailureOwnAddress, fmt.Sprintf("HTTPS couldn't be taken on port %d: %v", port, err))
	}
	local, err := url.Parse(tunnel.localURL)
	if err != nil {
		listener.Close()
		return failedTunnel(tunnelFailureOwnAddress, err.Error())
	}
	server := &http.Server{Handler: ownAddressProxy(local), ReadHeaderTimeout: 30 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	tunnel.set(serverTunnelStateOn, settings.Address, noTunnelFailure)
	select {
	case <-ctx.Done():
		_ = server.Close()
		return failedTunnel(tunnelFailureOwnAddress, "The address stopped.")
	case err := <-served:
		return failedTunnel(tunnelFailureOwnAddress, "HTTPS on the address stopped: "+err.Error())
	}
}

func hostOf(address string) string {
	if parsed, err := url.Parse(address); err == nil {
		return parsed.Hostname()
	}
	return ""
}

// ownAddressProxy hands requests taken on the address to Leafen's entrance
// with the person's own address (server_tunnel_door.go).
func ownAddressProxy(local *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(local)
	proxy.FlushInterval = -1
	base := proxy.Director
	proxy.Director = func(request *http.Request) {
		client := ""
		if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
			client = host
		}
		base(request)
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Header.Del(tunnelClientAddressHeader)
		request.Header.Del("Cf-Connecting-Ip")
		if client != "" {
			request.Header.Set(tunnelClientAddressHeader, client)
		}
		request.Header.Set(tunnelDoorHeader, tunnelDoorSecret)
	}
	return proxy
}

// checkOwnAddress looks at the address from here: whether its name
// resolves, and the certificate that answers on its port.
func checkOwnAddress(ctx context.Context, address string) serverAddressCheck {
	check := serverAddressCheck{Address: address, CheckedAt: time.Now().UnixMilli()}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		check.Problem = "dns"
		return check
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupHost(ctx, parsed.Hostname())
	if err != nil || len(addresses) == 0 {
		check.Problem = "dns"
		return check
	}
	check.Resolves = addresses
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 8 * time.Second},
		Config: &tls.Config{ServerName: parsed.Hostname(), InsecureSkipVerify: true}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(parsed.Hostname(), port))
	if err != nil {
		check.Problem = "unreachable"
		return check
	}
	defer conn.Close()
	certificates := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certificates) == 0 {
		check.Problem = "unreachable"
		return check
	}
	leaf := certificates[0]
	check.Names, check.NotAfter = leaf.DNSNames, leaf.NotAfter.UnixMilli()
	intermediates := x509.NewCertPool()
	for _, certificate := range certificates[1:] {
		intermediates.AddCert(certificate)
	}
	_, verifyErr := leaf.Verify(x509.VerifyOptions{DNSName: parsed.Hostname(), Intermediates: intermediates})
	check.Trusted = verifyErr == nil
	switch {
	case leaf.VerifyHostname(parsed.Hostname()) != nil:
		check.Problem = "name"
	case time.Now().After(leaf.NotAfter):
		check.Problem = "expired"
	case !check.Trusted:
		check.Problem = "untrusted"
	}
	return check
}

// checkAddressSoon checks the own address a moment after it applies, for
// the screen and the agent's tool.
func (tunnel *serverTunnel) checkAddressSoon(address string) {
	go func() {
		time.Sleep(time.Second)
		check := checkOwnAddress(context.Background(), address)
		tunnel.mu.Lock()
		tunnel.addressCheck = &check
		tunnel.mu.Unlock()
	}()
}

// ownAddressTLSPort is the port the server takes HTTPS on for its own
// address, 0 behind the administrator's proxy or in another mode.
func (tunnel *serverTunnel) ownAddressTLSPort(settings serverTunnelSettings) int {
	if settings.Mode != serverTunnelOwnAddress || tunnel.app == nil {
		return 0
	}
	return addressTLSPort(tunnel.app, settings.Address)
}

// addressCheckOf is the check of the address the settings name, none while
// another address's check is all there is (the screen may read a setting
// the server hasn't applied yet).
func (tunnel *serverTunnel) addressCheckOf(settings serverTunnelSettings) *serverAddressCheck {
	if settings.Mode != serverTunnelOwnAddress || tunnel.addressCheck == nil || tunnel.addressCheck.Address != settings.Address {
		return nil
	}
	return tunnel.addressCheck
}
