package app

import (
	"net/netip"
	"strings"
)

// Where an app's outside calls may never go, said the same way when a
// package is checked and when a call is refused (2026-10-16: a relay to
// 127.0.0.1 passed validate and the administrator's review, then every call
// was refused as reaching "a place apps may not").

// appUnreachableAddress names what an address is when apps may never reach
// it, even on the private network, or "".
func appUnreachableAddress(address netip.Addr) string {
	address = address.Unmap()
	switch {
	case !address.IsValid() || address.IsUnspecified() || address.IsMulticast() || address.IsInterfaceLocalMulticast() || address.IsLinkLocalMulticast():
		return "an address no single machine answers"
	case address.IsLoopback():
		return "this server's own machine (loopback)"
	case address == appRelayMetadataV6 || address == netip.MustParseAddr("169.254.169.254"):
		return "a cloud's metadata address"
	case address.IsLinkLocalUnicast():
		return "a link-local address"
	}
	return ""
}

// appUnreachableHost is appUnreachableAddress for a host as a manifest
// writes it: an IP address, or a name only this machine answers to.
func appUnreachableHost(host string) string {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	if address, err := netip.ParseAddr(host); err == nil {
		return appUnreachableAddress(address)
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "localhost.localdomain" {
		return "this server's own machine (loopback)"
	}
	return ""
}

// appUnreachableError is a refused destination with what it is.
type appUnreachableError struct{ reason string }

func (failure *appUnreachableError) Error() string { return "outbound destination: " + failure.reason }

func (failure *appUnreachableError) Is(target error) bool {
	return target == errOutboundUnsafeDestination
}

// appRelayAddressAllowed says whether an app's entry may reach an address: a
// public one, and a private one (RFC 1918, unique local, the shared
// 100.64.0.0/10) only when the administrator let it. It is a safety rule, not
// the network reach table's question of who opens an address
// (network_reach.go): link-local addresses, which that table counts as the
// network's, stay refused here — a cloud's metadata address is one
// (appUnreachableAddress) — whatever the table holds.
func appRelayAddressAllowed(address netip.Addr, port string, private bool) bool {
	address = address.Unmap()
	if outboundAddressIsPublic(address) {
		return !appRelayIsOwnPort(address, port)
	}
	if !private || !address.IsValid() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() ||
		address.IsUnspecified() || address.IsMulticast() || address.IsInterfaceLocalMulticast() || address == appRelayMetadataV6 {
		return false
	}
	return (address.IsPrivate() || outboundSharedAddressPrefix.Contains(address)) && !appRelayIsOwnPort(address, port)
}

// appRelayRefusal says why an entry may not reach an address.
func appRelayRefusal(address netip.Addr, port string, private bool) string {
	if reason := appUnreachableAddress(address); reason != "" {
		return reason + ", which apps may never reach"
	}
	if appRelayIsOwnPort(address.Unmap(), port) {
		return "Leafen itself, which apps reach through its API instead"
	}
	if !private {
		return `a private network address, which the entry reaches only with "privateNetwork": true`
	}
	return "a place apps may not reach"
}
