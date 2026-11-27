package app

import "net/http"

// What remote access is waiting for, said the same on the remote access
// screen and to an agent (2026-11-22: for a minute after signing in to
// Tailscale the status said only "starting", and whether it waited for a
// certificate, an allowed public address or the public DNS was guessed from
// the container's log): off; starting; for Tailscale signing in, a tailnet
// admin approving the device, the public address (HTTPS and Funnel) being
// allowed — each with its page — the address's certificate being fetched,
// and its name spreading through the public DNS; open; retrying. A read of
// it through an agent's leafen_call waits for the next stage
// (mcp_call_wait.go).
const (
	tunnelStageOff           = "off"
	tunnelStageStarting      = "starting"
	tunnelStageSignIn        = "signIn"
	tunnelStageApproveDevice = "approveDevice"
	tunnelStageAllowFunnel   = "allowFunnel"
	tunnelStageCertificate   = "certificate"
	tunnelStagePublishing    = "publishing"
	tunnelStageOpen          = "open"
	tunnelStageRetrying      = "retrying"
)

// stageLocked is the stage remote access is in under these settings; the
// tunnel's lock is held.
func (tunnel *serverTunnel) stageLocked(settings serverTunnelSettings) string {
	switch tunnel.state {
	case serverTunnelStateOn:
		return tunnelStageOpen
	case serverTunnelStateRetrying:
		return tunnelStageRetrying
	case serverTunnelStateStarting:
	default:
		return tunnelStageOff
	}
	if settings.Mode != serverTunnelTailscale {
		return tunnelStageStarting
	}
	switch {
	case tunnel.tailscaleLogin != "":
		return tunnelStageSignIn
	case tunnel.tailscaleAllow != "" && tunnel.tailscaleStep == tunnelStageApproveDevice:
		return tunnelStageApproveDevice
	case tunnel.tailscaleAllow != "":
		return tunnelStageAllowFunnel
	case tunnel.tailscaleCertificate:
		return tunnelStageCertificate
	case tunnel.tailscalePublishing:
		return tunnelStagePublishing
	}
	return tunnelStageStarting
}

// setTailscaleStep says which of Tailscale's waits for a person the device is in.
func (tunnel *serverTunnel) setTailscaleStep(step string) {
	tunnel.mu.Lock()
	tunnel.tailscaleStep = step
	tunnel.mu.Unlock()
}

// tunnelStageChanges is, for leafen_call's wait on GET /admin/tunnel, whether
// to keep waiting after an answer: from the first answer on, until the stage
// differs from that one's — at once when remote access is open or off, with
// nothing to wait for.
func tunnelStageChanges() func(nativeOperationResult) bool {
	first := ""
	return func(result nativeOperationResult) bool {
		body, _ := result.Body.(map[string]any)
		stage, _ := body["stage"].(string)
		if result.Status != http.StatusOK || stage == "" {
			return false
		}
		if first == "" {
			first = stage
			return stage != tunnelStageOpen && stage != tunnelStageOff
		}
		return stage == first
	}
}
