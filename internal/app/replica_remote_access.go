package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A standby taking over takes the server's address with its data (the
// built-in tunnel decision's changes of 2026-10-19): the replica carries the
// Tailscale device's state, the server's identity and its own certificates,
// so the server that took over answers at the same Tailscale address with the
// same identity. Two copies must never answer at once — Tailscale marks a
// device whose state runs on two machines "Duplicate node key", and two
// connectors of one Cloudflare tunnel share its traffic — so a server whose
// data moved runs no remote access at all, and a paired primary that can't
// raise its beat lets go of what it shares with copies of its data before the
// standby may take over (30 seconds by the standby's clock), taking it back
// when its beat goes out again.

// remoteAccessReleaseAfter is how long a paired primary goes paused before
// it lets go: the pause starts 10 seconds after its last beat, so this is
// about 25 seconds after it, inside the standby's 30.
const remoteAccessReleaseAfter = 15 * time.Second

// remoteAccessReleased is set while a paused primary has let its remote
// access go.
var remoteAccessReleased atomic.Bool

// sharesWithCopies says whether a setting's way in is the same on a copy of
// the data: the Tailscale device, a Cloudflare tunnel's token, the server's
// own certificate.
func sharesWithCopies(app core.App, settings serverTunnelSettings) bool {
	switch settings.Mode {
	case serverTunnelTailscale, serverTunnelToken:
		return true
	case serverTunnelOwnAddress:
		return addressTLSPort(app, settings.Address) > 0
	}
	return false
}

// notePausedRemoteAccess lets the shared remote access go once a paired
// primary has been paused long enough, and takes it back when the pause ends
// and the data is still this server's.
func (runner *replicationRunner) notePausedRemoteAccess(paused bool) {
	now := time.Now()
	if paused {
		since := runner.pausedSince.Load()
		if since == 0 {
			runner.pausedSince.Store(now.UnixMilli())
			return
		}
		if remoteAccessReleased.Load() || now.Sub(time.UnixMilli(since)) < remoteAccessReleaseAfter {
			return
		}
		settings, err := loadServerTunnelSettings(runner.app)
		if err != nil || !sharesWithCopies(runner.app, settings) {
			return
		}
		remoteAccessReleased.Store(true)
		tunnel := serverTunnelOf(runner.app)
		tunnel.stop()
		tunnel.set(serverTunnelStateOff, "", failedTunnel(tunnelHeldReplicaUnreachable,
			"This server can't reach its replica, so its address waits for the standby or for the replica to answer again."))
		tunnel.noteAddressChange(settings)
		runner.app.Logger().Warn("replica: paused too long; remote access let go so the standby may take the address over")
		return
	}
	runner.pausedSince.Store(0)
	if !remoteAccessReleased.Swap(false) {
		return
	}
	if readReplicaFence(runner.app.DataDir()) != nil {
		return
	}
	if settings, err := loadServerTunnelSettings(runner.app); err == nil {
		_ = serverTunnelOf(runner.app).apply(settings)
	}
}

// holdUntilOwnershipConfirmed keeps a paired primary from opening what it
// shares with copies at its start: it may come back after its standby took
// over, so it waits for its first beat (which reads the owner) — then it opens
// it, or finds it moved and never does.
func holdUntilOwnershipConfirmed(app core.App, settings serverTunnelSettings) {
	if !sharesWithCopies(app, settings) {
		return
	}
	holder := replicaHolder{}
	if raw, err := os.ReadFile(filepath.Join(app.DataDir(), replicaHolderName)); err == nil && json.Unmarshal(raw, &holder) == nil && holder.Paired {
		remoteAccessReleased.Store(true)
	}
}

// remoteAccessHeld says why remote access waits, for the screen.
func remoteAccessHeld(app core.App) tunnelFailure {
	if readReplicaFence(app.DataDir()) != nil {
		return failedTunnel(tunnelHeldMoved, "This server's data moved to another server, which answers at its address.")
	}
	return failedTunnel(tunnelHeldWaiting,
		"This server waits to confirm it still holds its data (its standby may have taken over) before opening its address.")
}

// remoteAccessHeldBack says remote access mustn't run here now: the data
// moved elsewhere, or a paused primary let it go.
func remoteAccessHeldBack(app core.App) bool {
	return remoteAccessReleased.Load() || (app != nil && readReplicaFence(app.DataDir()) != nil)
}

// becomeMoved keeps that this server's data went elsewhere: it sends and
// takes no more changes.
func (runner *replicationRunner) becomeMoved(moved errReplicaMoved) {
	fence := replicaFence{MovedTo: moved.origin, At: time.Now().UnixMilli()}
	if err := writeRestoreFile(runner.app.DataDir(), replicaFencedName, fence); err != nil {
		runner.app.Logger().Error("replica: couldn't keep that the server moved", "error", err)
	}
	runner.setStatus(func(status *replicationStatus) {
		*status = replicationStatus{State: replicationStateMoved, MovedTo: moved.origin}
	})
	// The address follows the server that took over, which holds the same
	// device and keys: nothing outside reaches this one any more
	// (replica_remote_access.go).
	if settings, err := loadServerTunnelSettings(runner.app); err == nil {
		tunnel := serverTunnelOf(runner.app)
		tunnel.stop()
		if settings.Mode != serverTunnelOff && settings.Mode != "" {
			tunnel.set(serverTunnelStateOff, "", remoteAccessHeld(runner.app))
		}
		tunnel.noteAddressChange(settings)
	}
	runner.app.Logger().Warn("replica: another server took the replica over; this one takes no more changes", "movedTo", moved.origin)
}
