package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A tunnel's address changing (the built-in tunnel decision): a quick tunnel
// gets a new address each time it starts. When it comes up at an address
// other than the one it had last, the people whose agents came through the
// old one are told, in their inbox, to connect them again; their connections
// show it until they come through the new address. Remote access no longer
// opens by itself at the first run (2026-10-19): the administrator chooses an
// address when something first needs one (RemoteAddressChoice on the screen).

const agentNoticePagePrefix = "agents:"

// currentTunnelAddress is the address agents outside reach the server at
// through its tunnel as set: a quick tunnel's latest, a tunnel of the
// administrator's own its address, Tailscale's public name, none when it is
// off.
func currentTunnelAddress(app core.App) string {
	settings, err := loadServerTunnelSettings(app)
	if err != nil {
		return ""
	}
	switch settings.Mode {
	case serverTunnelQuick:
		return settings.QuickAddress
	case serverTunnelToken:
		return settings.Address
	case serverTunnelTailscale:
		if settings.TailscaleAddress != "" {
			return "https://" + settings.TailscaleAddress
		}
	case serverTunnelOwnAddress:
		return settings.Address
	}
	return ""
}

// noteQuickTunnelAddress keeps the address a quick tunnel came up at and,
// when it had another before, tells whoever's agents came through that one.
func noteQuickTunnelAddress(app core.App, address string) {
	var previous string
	err := app.DB().NewQuery(`SELECT [[quick_address]] FROM {{leafen_server_tunnel}} WHERE [[id]] = 'tunnel'`).Row(&previous)
	if errors.Is(err, sql.ErrNoRows) || err != nil || previous == address {
		return
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_server_tunnel}} SET [[quick_address]] = {:address} WHERE [[id]] = 'tunnel'`).
		Bind(dbx.Params{"address": address}).Execute(); err != nil {
		app.Logger().Warn("leafen tunnel address not kept", "error", err)
		return
	}
	if previous != "" {
		notifyAgentReconnect(app, previous, address)
	}
}

// notifyAgentReconnect tells each person with a live connection that came
// through the old address to connect it again at the new one, called when
// the old address doesn't forward (notifyAgentsCutOff): an OAuth one can't
// renew there either.
func notifyAgentReconnect(app core.App, previous, address string) {
	now := time.Now().UnixMilli()
	owners := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[owner_id]] FROM {{leafen_mcp_connections}}
		WHERE [[reached_via]] = {:previous} AND [[revoked_at]] = 0 AND ([[token_expires_at]] = 0 OR [[token_expires_at]] > {:now})
		ORDER BY [[owner_id]]
	`).Bind(dbx.Params{"previous": previous, "now": now}).Column(&owners); err != nil {
		app.Logger().Warn("leafen reconnect notice failed", "error", err)
		return
	}
	key := "agent-reconnect:" + appNoticeID("agent-reconnect", address)
	projections := []productNotificationProjection{}
	for _, owner := range owners {
		workspaceID, err := personNoticeWorkspace(app, owner, "")
		if err != nil || workspaceID == "" {
			continue
		}
		projections = append(projections, productNotificationProjection{
			ID: appNoticeID("agent-reconnect", address, owner), EventID: appNoticeID("agent-reconnect-event", address),
			WorkspaceID: workspaceID, UserID: owner, ActivityKey: key, Kind: "system",
			PageID: agentNoticePagePrefix + "reconnect", Title: address,
			Target: "/settings?section=agents", OccurredAt: now,
		})
	}
	if len(projections) == 0 {
		return
	}
	if _, err := upsertProductNotifications(context.Background(), app, projections, now); err != nil {
		app.Logger().Warn("leafen reconnect notice failed", "error", err)
	}
}

// mcpConnectionNeedsReconnect says a connection came through a tunnel
// address the server no longer has.
func mcpConnectionNeedsReconnect(reachedVia, current string) bool {
	return reachedVia != "" && reachedVia != current
}
