package app

import (
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// Who opens an address by its host alone — this computer, its network, or
// anyone — is one table, contracts/network-reach.json
// (network_reach.generated.go), which the web reads too
// (web/src/lib/addressReach.ts): every judgement here of whether a host is
// this computer's, its network's or outside stands on placeOfHost
// (2026-11-22: the server counted .lan names and tailnet addresses as the
// network's and the web didn't, so the two told the same address apart).

type hostPlace int

const (
	placeAnywhere hostPlace = iota
	placeNetwork
	placeThisComputer
)

// hostName is a host, with or without its port, as the table reads it:
// lowercase, without brackets or a final dot.
func hostName(hostPort string) string {
	host := strings.TrimSpace(hostPort)
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	return strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
}

// placeOfHost says where a host (hostName's) is by its name or address
// alone. The unspecified address names this computer as a host; "" is
// nowhere in particular.
func placeOfHost(host string) hostPlace {
	if host == "" {
		return placeAnywhere
	}
	if address, err := netip.ParseAddr(host); err == nil {
		for _, unspecified := range unspecifiedAddresses {
			if address.WithZone("").Unmap() == unspecified {
				return placeThisComputer
			}
		}
		return placeOfAddress(address)
	}
	if loopbackHostName(host) {
		return placeThisComputer
	}
	if networkOneLabelNames && !strings.Contains(host, ".") {
		return placeNetwork
	}
	for _, suffix := range networkHostSuffixes {
		if strings.HasSuffix(host, suffix) {
			return placeNetwork
		}
	}
	return placeAnywhere
}

// placeOfAddress is where an IP address is: this computer's loopback, the
// network's private, link-local, unique-local or shared (tailnet) ranges, or
// anywhere.
func placeOfAddress(address netip.Addr) hostPlace {
	address = address.WithZone("").Unmap()
	for _, prefix := range thisComputerRanges {
		if prefix.Contains(address) {
			return placeThisComputer
		}
	}
	for _, prefix := range networkRanges {
		if prefix.Contains(address) {
			return placeNetwork
		}
	}
	return placeAnywhere
}

// loopbackHostName says whether a host's name or address is this computer's
// own loopback (every .localhost name is, RFC 6761, as browsers and this
// computer's resolver answer it); the unspecified address isn't one.
func loopbackHostName(host string) bool {
	for _, name := range thisComputerHostNames {
		if host == name {
			return true
		}
	}
	for _, suffix := range thisComputerHostSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	address, err := netip.ParseAddr(host)
	return err == nil && placeOfAddress(address) == placeThisComputer
}

// mcpLoopbackHost says whether a host, with or without its port, is this
// computer's loopback.
func mcpLoopbackHost(hostPort string) bool {
	return loopbackHostName(hostName(hostPort))
}

// addressToOpenInNetwork says whether an IP address is the network's and one
// a person there opens: not a link-local one each computer gives itself on
// one link (the table's selfAssignedRanges), which others in the network
// don't reach and a browser can't take with an IPv6 one's zone.
func addressToOpenInNetwork(address netip.Addr) bool {
	address = address.WithZone("").Unmap()
	if placeOfAddress(address) != placeNetwork {
		return false
	}
	for _, prefix := range networkSelfAssignedRanges {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

// addressInNetwork says whether an IP address is this computer's or its
// network's.
func addressInNetwork(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	return ok && placeOfAddress(address) != placeAnywhere
}

// outsideOrigin says whether an address is one the server opens at from
// outside: https, at a host outside this computer and its network
// (hostOutsideNetwork). The web judges a page's address the same way
// (outsideAddress), from the same table.
func outsideOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && hostOutsideNetwork(parsed.Host)
}
