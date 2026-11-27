package app

import (
	"net/url"
	"os"
	"strings"
	"time"
)

// Leafen's own remote access can be off — no Tailscale address, no address
// of its own — while something in front of the server serves it from
// outside: a NAS's reverse proxy or a router's port forward (2026-11-16: the
// status said "off" for a server people opened from outside through its
// NAS). The server can't see that proxy, but it knows the addresses its
// administrators' browsers have opened it at (server_known_address.go): one
// outside this computer and its network says it is reached from outside.

// serverReachedFrom is an address outside this computer and its network the
// server was opened at, and when last.
type serverReachedFrom struct {
	Origin string `json:"origin"`
	LastAt string `json:"lastAt"`
}

// openedFromOutside lists, while Leafen's own remote access is off, the
// outside addresses the server was opened at, newest first.
func (tunnel *serverTunnel) openedFromOutside(settings serverTunnelSettings) []serverReachedFrom {
	if tunnel.app == nil || settings.Mode != serverTunnelOff {
		return nil
	}
	rows := []struct {
		Origin string `db:"origin"`
		UsedAt int64  `db:"used_at"`
	}{}
	if err := tunnel.app.DB().NewQuery(`SELECT [[origin]], [[used_at]] FROM {{leafen_server_administrator_addresses}}
		ORDER BY [[used_at]] DESC, [[origin]]`).All(&rows); err != nil {
		return nil
	}
	reached := []serverReachedFrom{}
	for _, row := range rows {
		parsed, err := url.Parse(row.Origin)
		if err != nil || !hostOutsideNetwork(parsed.Host) {
			continue
		}
		reached = append(reached, serverReachedFrom{Origin: row.Origin, LastAt: time.UnixMilli(row.UsedAt).UTC().Format(time.RFC3339)})
	}
	if len(reached) == 0 {
		return nil
	}
	return reached
}

// hostOutsideNetwork says whether a host (with or without its port) names a
// place outside this computer and its local network: not this computer's
// name, nor a host hostNamedInNetwork knows by itself.
func hostOutsideNetwork(hostPort string) bool {
	host, nearby := hostNamedInNetwork(hostPort)
	if nearby {
		return false
	}
	if machine, err := os.Hostname(); err == nil && strings.EqualFold(strings.TrimSuffix(machine, "."), host) {
		return false
	}
	return true
}

// hostNamedInNetwork is the host (lowercase, without its port) and whether it
// says by itself that it is this computer or on its local network: this
// computer, a private, link-local or shared address, a name with no dot, or a
// local network's name — the one table's (network_reach.go).
func hostNamedInNetwork(hostPort string) (string, bool) {
	host := hostName(hostPort)
	return host, host == "" || placeOfHost(host) != placeAnywhere
}

// remoteAccessOffInstructions says what "off" means: Leafen opens nothing
// itself, and whether the server is reached from outside all the same.
func remoteAccessOffInstructions(reached []serverReachedFrom) string {
	if len(reached) == 0 {
		return "Off means Leafen itself opens no way in from outside (no Tailscale address, no address of its own), and no administrator has opened it from outside: " +
			"as far as the server knows, it answers this computer and its network only."
	}
	return "Off means only that Leafen itself opens no way in from outside (no Tailscale address, no address of its own). The server is reached from outside all the same: " +
		"an administrator opened it at " + reached[0].Origin + " (last " + reached[0].LastAt + "; reachedFrom lists every such address), so something in front of it — " +
		"a NAS's reverse proxy or a router's port forward — serves it. Left to do: that to the person. This tool with set_address makes that address the server's own, for mails' and agents' links."
}
