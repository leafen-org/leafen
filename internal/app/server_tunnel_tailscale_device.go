package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The Tailscale device's name, what the server says of it, and the
// administrator's ways to sign it out (server_tunnel_tailscale.go).

// tailscaleFolderName holds the device's state and the server's notes.
const tailscaleFolderName = "tailscale"

// tailscaleHostname is the device name the server asks Tailscale for,
// chosen the first time from the server's name and kept beside the device's
// state, so renaming the server later never moves the public address (the
// name under the tailnet's).
func tailscaleHostname(app core.App) string {
	path := filepath.Join(app.DataDir(), tailscaleFolderName, "hostname")
	if data, err := os.ReadFile(path); err == nil {
		if kept := strings.TrimSpace(string(data)); kept != "" {
			return kept
		}
	}
	chosen := tailscaleHostnameFromServerName(app)
	if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
		_ = os.WriteFile(path, []byte(chosen+"\n"), 0o600)
	}
	return chosen
}

// tailscaleHostnameFromServerName is the server's name in plain letters —
// its alias's English word unless its administrators named it in such
// letters — or "leafen" (2026-10-20: a Korean workspace's server was always
// "leafen", and a person's or computer's name would show in the address).
func tailscaleHostnameFromServerName(app core.App) string {
	settings, err := loadAdministratorSettings(app)
	if err != nil {
		return "leafen"
	}
	slug := serverLatinName(settings.ServerName, settings.ServerAlias)
	if len(slug) < 3 {
		return "leafen"
	}
	return slug
}

// tailscaleAddressServed is the Tailscale address while remote access uses
// it, so the packaged apps remember the server's identity there
// (server_identity.go).
func tailscaleAddressServed(app core.App) string {
	settings, err := loadServerTunnelSettings(app)
	if err != nil || settings.Mode != serverTunnelTailscale {
		return ""
	}
	return settings.TailscaleAddress
}

// setTailscaleWaiting shows where the administrator signs in or allows the
// public address while the server waits, and the device name Tailscale
// didn't give.
func (tunnel *serverTunnel) setTailscaleWaiting(login, allow, renamed string) {
	tunnel.mu.Lock()
	changed := login != tunnel.tailscaleLogin || allow != tunnel.tailscaleAllow
	tunnel.tailscaleLogin, tunnel.tailscaleAllow = login, allow
	if renamed != "" || (login == "" && allow == "") {
		tunnel.tailscaleRenamed = renamed
	}
	tunnel.mu.Unlock()
	if !changed {
		return
	}
	// An install whose screen hasn't been opened (Docker, a NAS) learns the
	// way from its log too.
	hint := func(message, address string) {
		tunnel.app.Logger().Warn(message, "url", address)
		if !tunnel.app.IsDev() {
			productionDiagnosticLogger.Warn(message, "url", address)
		}
	}
	switch {
	case login != "":
		hint("Tailscale: log in to give this server its public address (any browser)", login)
	case allow != "":
		hint("Tailscale: allow the public address (HTTPS and Funnel) for this server", allow)
	}
	tunnel.writeTailscaleStatus(login, allow, "")
}

// tailscaleStatusFile is where the server tells the command line how
// Tailscale stands (server_tailscale_command.go).
const tailscaleStatusFile = "status.json"

type tailscaleStatus struct {
	Login   string `json:"login,omitempty"`
	Allow   string `json:"allow,omitempty"`
	Address string `json:"address,omitempty"`
	// The address is open, its name not yet in the public DNS
	// (server_tunnel_tailscale_published.go).
	Publishing bool  `json:"publishing,omitzero"`
	At         int64 `json:"at"`
}

func (tunnel *serverTunnel) writeTailscaleStatus(login, allow, address string) {
	tunnel.writeTailscaleStatusFile(tailscaleStatus{Login: login, Allow: allow, Address: address})
}

func (tunnel *serverTunnel) writeTailscalePublishing() {
	tunnel.writeTailscaleStatusFile(tailscaleStatus{Publishing: true})
}

func (tunnel *serverTunnel) writeTailscaleStatusFile(status tailscaleStatus) {
	dir := filepath.Join(tunnel.app.DataDir(), tailscaleFolderName)
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	status.At = time.Now().UnixMilli()
	data, _ := json.Marshal(status)
	_ = os.WriteFile(filepath.Join(dir, tailscaleStatusFile), data, 0o600)
}

// logoutTailscaleRoute signs the running Tailscale device out of the
// administrator's account and turns remote access off: turning Tailscale on
// again asks to sign in.
func logoutTailscaleRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	tunnel := serverTunnelOf(event.App)
	tunnel.mu.Lock()
	logout := tunnel.tailscaleLogout
	tunnel.mu.Unlock()
	if logout == nil {
		return apiError(event, http.StatusConflict, errorCodeServerTunnelInvalid, "Tailscale isn't running on this server.")
	}
	ctx, cancel := context.WithTimeout(event.Request.Context(), 30*time.Second)
	defer cancel()
	if err := logout(ctx); err != nil {
		return apiErrorFrom(event, http.StatusBadGateway, errorCodeServerTunnelFailed, "Tailscale couldn't log the device out.", err)
	}
	// The device's addresses went with it: none forwards from here on.
	noteTailscaleSignedOut(event.App)
	if _, err := event.App.DB().NewQuery(`UPDATE {{leafen_server_tunnel}} SET [[mode]] = 'off' WHERE [[id]] = 'tunnel'`).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeServerTunnelFailed, "Couldn't turn remote access off.", err)
	}
	settings, err := loadServerTunnelSettings(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeServerTunnelFailed, "Couldn't read the tunnel.", err)
	}
	_ = tunnel.apply(settings)
	return event.JSON(http.StatusOK, tunnel.status(settings))
}
