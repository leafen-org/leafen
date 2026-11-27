package app

import "net/url"

// The address the start settings give (LEAFEN_PUBLIC_URL) is the server's
// first address, not its only one (2026-11-22, user-named: an administrator
// behind their own reverse proxy who turned to the built-in Tailscale was
// refused until the container was made again without it). It is the
// server's address while no way out is chosen — remote access never set, or
// set off — and a way the administrator chooses in the settings, through an
// agent or on the command line wins from then on, with no restart. The start
// settings may still name it then; the remote access screen only says so
// (serverTunnelStatus.ConfiguredInUse).
//
// Moving away from it is an address change like any other
// (server_address_forwarding.go): an https start address is the address its
// requests came to through the administrator's own proxy, and forwards to the
// chosen one while that proxy still sends it here, as their own address does;
// an http one names the server inside its own network and doesn't forward.

// startAddressStands says whether the start settings' address is the
// server's address under these remote access settings.
func startAddressStands(settings serverTunnelSettings) bool {
	return configuredServerAddress() != "" && (settings.Mode == serverTunnelOff || settings.Mode == "")
}

// standingStartAddress is the start settings' address while it is the
// server's address, as the settings chosen last leave it; "" once a way out
// was chosen, or with none written.
func standingStartAddress() string {
	settings := serverTunnelChosen.Load()
	if settings != nil && !startAddressStands(*settings) {
		return ""
	}
	return configuredServerAddress()
}

// startAddressCanonical is the start settings' address as the address the
// server is reached at, under settings that choose no way out: an https one,
// through the administrator's own proxy; none otherwise.
func startAddressCanonical(settings serverTunnelSettings) addressCanonical {
	if !startAddressStands(settings) {
		return addressCanonical{}
	}
	written := configuredServerAddress()
	if parsed, err := url.Parse(written); err != nil || parsed.Scheme != "https" {
		return addressCanonical{}
	}
	return addressCanonical{Origin: written, Kind: forwardKindProxy}
}
