package app

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The first setup asks no code of a person on the server's own network (user
// decision 2026-11-19, which changed the 2026-10-20 one: a person who had
// just installed Leafen on a NAS was stopped by a code they couldn't know
// where to find): a request from this computer (loopback), a private network
// (RFC 1918, IPv6 unique local), a link-local address, or Tailscale
// (100.64.0.0/10) makes the first administrator from a name alone. Behind a
// reverse proxy — a NAS's (Synology's adds X-Real-IP and X-Forwarded-For),
// Caddy, Nginx — the connection comes from the proxy, so where that
// connection is the network's own, every original address the proxy passed
// on (Forwarded, X-Forwarded-For, X-Real-Ip) must be the network's too. A
// header is read only behind such a peer: one sent from outside straight to
// the server proves nothing, and one a person outside sends through the
// proxy stands left of the address the proxy adds, which is theirs. Nothing
// to set up for anyone. A request from the outside internet still takes the
// code (setup_code.go), and its screen names where the setup link is.

// tunnelClientUnknownKey marks a request that came through Leafen's own
// tunnel door with no person's address (server_tunnel_door.go): it came from
// outside all the same.
type tunnelClientUnknownKey struct{}

// requestFromNetwork says whether a request comes from this computer or its
// network, through every proxy it passed.
func requestFromNetwork(request *http.Request) bool {
	if unknown, _ := request.Context().Value(tunnelClientUnknownKey{}).(bool); unknown {
		return false
	}
	if !addressInNetwork(parseForwardedAddress(request.RemoteAddr)) {
		return false
	}
	for _, original := range forwardedOriginalAddresses(request.Header) {
		if !addressInNetwork(parseForwardedAddress(original)) {
			return false
		}
	}
	return true
}

// forwardedOriginalAddresses lists every address the proxies in front named
// as a request's origin, as written.
func forwardedOriginalAddresses(header http.Header) []string {
	addresses := []string{}
	for _, value := range header.Values("Forwarded") {
		for _, element := range strings.Split(value, ",") {
			for _, pair := range strings.Split(element, ";") {
				name, given, found := strings.Cut(strings.TrimSpace(pair), "=")
				if found && strings.EqualFold(strings.TrimSpace(name), "for") {
					addresses = append(addresses, strings.Trim(strings.TrimSpace(given), `"`))
				}
			}
		}
	}
	for _, name := range []string{"X-Forwarded-For", "X-Real-Ip"} {
		for _, value := range header.Values(name) {
			for _, address := range strings.Split(value, ",") {
				if address = strings.TrimSpace(address); address != "" {
					addresses = append(addresses, address)
				}
			}
		}
	}
	return addresses
}

// parseForwardedAddress reads an address as a connection or a proxy writes
// it: bare, in brackets, or with its port. An obfuscated or "unknown" one
// reads as nil.
func parseForwardedAddress(written string) net.IP {
	written = strings.TrimSpace(written)
	if host, _, err := net.SplitHostPort(written); err == nil {
		written = host
	}
	written = strings.Trim(written, "[]")
	if zone := strings.IndexByte(written, '%'); zone >= 0 {
		written = written[:zone]
	}
	return net.ParseIP(written)
}

// markTunnelClientUnknown keeps on a request that it came through the door
// without a person's address.
func markTunnelClientUnknown(e *core.RequestEvent) {
	e.Request = e.Request.WithContext(context.WithValue(e.Request.Context(), tunnelClientUnknownKey{}, true))
}

// setupHomeAddress is an address in the server's own network the server
// knows opens it, for the outside's setup screen to name: the one written
// (LEAFEN_PUBLIC_URL) when it is in the network, else the network's address
// this request's connection came in on that a person there opens
// (addressToOpenInNetwork) — not in a container, whose addresses are its own
// little network's rather than the home's, nor on a connection that came in
// over TLS, whose certificate names the server, not an address: an address
// would open with a certificate warning. "" when it knows none.
func setupHomeAddress(request *http.Request) string {
	if written := configuredServerAddress(); written != "" {
		if parsed, err := url.Parse(written); err == nil && !hostOutsideNetwork(parsed.Host) && !mcpLoopbackHost(parsed.Host) {
			return written
		}
	}
	if runningInContainer() || request.TLS != nil {
		return ""
	}
	local, ok := request.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || local == nil {
		return ""
	}
	address, err := netip.ParseAddrPort(local.String())
	if err != nil || !addressToOpenInNetwork(address.Addr()) {
		return ""
	}
	return "http://" + netip.AddrPortFrom(address.Addr().WithZone("").Unmap(), address.Port()).String()
}
