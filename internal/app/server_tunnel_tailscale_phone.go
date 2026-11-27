//go:build ios || android

package app

import (
	"context"
	"net/http"
	"net/url"
)

// Tailscale isn't built into the phone apps (decided 2026-10-19): a phone's
// own server stops when the app closes, so it opens no public address; the
// apps point people to an always-on server instead.

const tailscaleBuiltIn = false

func (tunnel *serverTunnel) runTailscale(context.Context) tunnelFailure {
	return failedTunnel(tunnelFailureNoTailscale, "Tailscale isn't built into Leafen on phones; use it on an always-on server.")
}

func (tunnel *serverTunnel) serveForwardingTailscale(context.Context, http.Handler, func()) error {
	return nil
}

// tailscaleProxy is the way in for a Tailscale address, which phones don't open.
func tailscaleProxy(local *url.URL) http.Handler { return ownAddressProxy(local) }
