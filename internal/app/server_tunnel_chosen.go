package app

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// The address the administrator chose is the server's canonical address (the
// built-in tunnel decision's changes of 2026-10-19): links, invitations,
// mails, agent connection texts and sign-in provider returns all use it: the
// one chosen in Settings › Server › Access from outside — their own address,
// a tunnel of their own Cloudflare account, the Tailscale name (even while
// Tailscale is briefly away, so links don't change meanwhile) — or a quick
// tunnel's only while it runs, since its address dies with it; with no way
// chosen, the address written in LEAFEN_PUBLIC_URL
// (server_tunnel_start_address.go); else none, and a request's own address
// serves.

var serverTunnelChosen atomic.Pointer[serverTunnelSettings]

// noteChosenTunnelSettings keeps the setting the canonical address comes from.
func noteChosenTunnelSettings(settings serverTunnelSettings) {
	kept := settings
	serverTunnelChosen.Store(&kept)
}

// chosenServerOrigin is the address chosen in the settings, "" with none.
func chosenServerOrigin() string {
	settings := serverTunnelChosen.Load()
	if settings == nil {
		return runningTunnelOrigin()
	}
	switch settings.Mode {
	case serverTunnelOwnAddress, serverTunnelToken:
		return settings.Address
	case serverTunnelTailscale:
		if settings.TailscaleAddress != "" {
			return "https://" + settings.TailscaleAddress
		}
	case serverTunnelQuick:
		return runningTunnelOrigin()
	}
	return ""
}

// requestChosenOrigin is the chosen address for a request made at its host
// (a tunnel's, or the administrator's own proxy, which hands the request on
// decrypted, so the request alone would say http), "" for one made elsewhere.
func requestChosenOrigin(request *http.Request) string {
	if tunnel := requestTunnelOrigin(request); tunnel != "" {
		return tunnel
	}
	chosen := chosenServerOrigin()
	if chosen == "" || request == nil {
		return ""
	}
	parsed, err := url.Parse(chosen)
	if err != nil {
		return ""
	}
	host := request.Host
	if name, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		host = name
	}
	if !strings.EqualFold(host, parsed.Hostname()) {
		return ""
	}
	return chosen
}
