//go:build !ios && !android

package app

import "context"

// fetchFirstTailscaleCertificate fetches the address's first certificate as
// it opens, the stage saying so meanwhile (server_tunnel_stage.go). Phones
// have no Tailscale built in, so no such stage.
func (tunnel *serverTunnel) fetchFirstTailscaleCertificate(ctx context.Context, certificates *tailscaleCertificates, address string) {
	tunnel.mu.Lock()
	tunnel.tailscaleCertificate = true
	tunnel.mu.Unlock()
	certificates.noteHeld(ctx, address)
	tunnel.mu.Lock()
	tunnel.tailscaleCertificate = false
	tunnel.mu.Unlock()
}
