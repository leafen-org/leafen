package app

import (
	"context"
	"time"
)

// The tunnel setting another process changes — the command line's `leafen
// tailscale` on a running server (server_tailscale_command.go) — reaches the
// running server within seconds.

// serverTunnelFollowEvery is how often the server looks for a tunnel setting
// another process changed.
const serverTunnelFollowEvery = 3 * time.Second

// followSettings applies a tunnel setting another process changed — the
// command line's `leafen tailscale` on a running server — within seconds.
func (tunnel *serverTunnel) followSettings(ctx context.Context) {
	ticker := time.NewTicker(serverTunnelFollowEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		settings, err := loadServerTunnelSettings(tunnel.app)
		if err != nil {
			continue
		}
		tunnel.mu.Lock()
		applied := tunnel.appliedUpdated
		tunnel.mu.Unlock()
		if settings.Updated != applied {
			_ = tunnel.apply(settings)
		}
	}
}

// stopFollowing ends following the setting, as the server closes.
func (tunnel *serverTunnel) stopFollowing() {
	tunnel.mu.Lock()
	cancel := tunnel.followCancel
	tunnel.followCancel = nil
	tunnel.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
