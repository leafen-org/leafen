package app

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// The outside way a first-setup restore left to an agent elsewhere (Claude
// on the web, ChatGPT) opens first (user decision 2026-11-19): the
// built-in tunnel's one-time link, or Tailscale, run for the restore alone.
// It is no setting of the server's: nothing is saved, it isn't the server's
// address for links, and it closes when the restore code ends — the
// restore's restart, its time, the screen going back or the server getting
// an administrator. Its Tailscale device is an ephemeral one of its own,
// which Tailscale takes away once it goes, so the restored server's own
// device and address, which come back from the backup, meet no other.
// Through it the server answers only the agent's restore routes, its MCP
// endpoint and the health check.

const setupRestoreTailscaleState = "restore-state"

// setupRestoreOutside is what the screen shows of the outside way.
type setupRestoreOutside struct {
	Mode                string `json:"mode"`
	State               string `json:"state"`
	Address             string `json:"address,omitempty"`
	Failure             string `json:"failure,omitempty"`
	FailureKind         string `json:"failureKind,omitempty"` // said in the screen's words (server_tunnel_failure.go)
	TailscaleLogin      string `json:"tailscaleLogin,omitempty"`
	TailscaleAllow      string `json:"tailscaleAllow,omitempty"`
	TailscalePublishing bool   `json:"tailscalePublishing,omitzero"`
}

// openSetupRestoreOutside opens the outside way for the restore. The
// server's own settings stay as they are, and the setting it follows
// (server_tunnel_follow.go) isn't taken as changed.
func openSetupRestoreOutside(app core.App, mode string) {
	current, err := loadServerTunnelSettings(app)
	if err != nil {
		current = serverTunnelSettings{Mode: serverTunnelOff}
	}
	_ = serverTunnelOf(app).apply(serverTunnelSettings{Mode: mode, Updated: current.Updated, RestoreScoped: true})
}

// closeSetupRestoreOutside closes the restore's outside way, if it is still
// the one running, and goes back to what the settings say.
func closeSetupRestoreOutside(app core.App) {
	tunnel := serverTunnelOf(app)
	tunnel.mu.Lock()
	scoped := tunnel.restoreScoped
	tunnel.mu.Unlock()
	if !scoped {
		return
	}
	tunnel.stop()
	tunnel.mu.Lock()
	tunnel.restoreScoped = false
	tunnel.mu.Unlock()
	_ = os.RemoveAll(filepath.Join(app.DataDir(), tailscaleFolderName, setupRestoreTailscaleState))
	if settings, err := loadServerTunnelSettings(app); err == nil && settings.Mode != serverTunnelOff {
		_ = tunnel.apply(settings)
	}
}

// setupRestoreOutsideOf is the outside way as it runs now.
func setupRestoreOutsideOf(app core.App, mode string) *setupRestoreOutside {
	tunnel := serverTunnelOf(app)
	tunnel.mu.Lock()
	defer tunnel.mu.Unlock()
	if !tunnel.restoreScoped {
		return &setupRestoreOutside{Mode: mode, State: serverTunnelStateOff}
	}
	return &setupRestoreOutside{Mode: mode, State: tunnel.state, Address: tunnel.url, Failure: tunnel.failure.text, FailureKind: tunnel.failure.kind,
		TailscaleLogin: tunnel.tailscaleLogin, TailscaleAllow: tunnel.tailscaleAllow, TailscalePublishing: tunnel.tailscalePublishing}
}

// noteSettingsChosen takes the settings applied as where the server's
// address comes from — not a restore's own outside way, which is no
// address of the server's.
func (tunnel *serverTunnel) noteSettingsChosen(settings serverTunnelSettings) {
	if settings.RestoreScoped {
		return
	}
	noteChosenTunnelSettings(settings)
	tunnel.noteAddressChange(settings)
}

// tailscaleState is the folder the Tailscale device keeps its state in, and
// whether the device is ephemeral: the restore's own is.
func (tunnel *serverTunnel) tailscaleState() (string, bool) {
	tunnel.mu.Lock()
	scoped := tunnel.restoreScoped
	tunnel.mu.Unlock()
	if scoped {
		return filepath.Join(tunnel.app.DataDir(), tailscaleFolderName, setupRestoreTailscaleState), true
	}
	return filepath.Join(tunnel.app.DataDir(), tailscaleFolderName, "state"), false
}

// setupRestoreOutsidePath says whether the restore's outside way answers a path.
func setupRestoreOutsidePath(path string) bool {
	switch path {
	case setupRestoreAgentMCPPath, setupRestoreAgentPath + "/status", setupRestoreAgentPath + "/find",
		setupRestoreAgentPath + "/run", apiRoot + "/health":
		return true
	}
	return false
}

// registerSetupRestoreOutsideScope keeps what comes through the restore's
// outside way to the restore.
func registerSetupRestoreOutsideScope(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenSetupRestoreOutsideScope", Priority: -99000,
		Func: func(e *core.RequestEvent) error {
			tunnel := serverTunnelOf(e.App)
			tunnel.mu.Lock()
			scoped := tunnel.restoreScoped
			tunnel.mu.Unlock()
			if scoped && requestTunnelOrigin(e.Request) != "" && !setupRestoreOutsidePath(e.Request.URL.Path) {
				return apiError(e, http.StatusNotFound, errorCodeNotFound, "Not found: this address is open only for restoring Leafen from a backup.")
			}
			return e.Next()
		},
	})
}
