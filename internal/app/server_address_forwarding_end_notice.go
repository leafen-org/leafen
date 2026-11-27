package app

import (
	"context"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An earlier address's forward ends: its connections are told to move
// first (2026-11-22, user decision (다) on the forward's end): about 14
// days before it ends, once, and as it ends, once, each person whose
// connection still came through it last — an agent's, a program's with a
// key, a Notion-compatible integration's (connection_reach.go) — gets a
// notice in their inbox naming the new address and those connections,
// which opens where they are. One moved to the new address isn't named. The
// forward's length and the year's cap don't change; nothing is asked of
// anyone. The notices go by the forward's record (forwarding.json), so a
// forward is told about once at each stage, a server that was down when it
// ended tells it as it starts, and one that ended long before isn't told.

const (
	addressMoveSoonNotice  = "address-move-soon"
	addressMoveEndedNotice = "address-move-ended"
)

// addressForwardNoticeBefore is how long before a forward ends its
// connections are told: 14 of its 90 days.
func addressForwardNoticeBefore() time.Duration { return addressForwardFor() * 14 / 90 }

// noticeForwardEnds tells the connections of each forward that ends soon or
// has just ended, once each, looked at each minute (keepForwardingWays).
func noticeForwardEnds(app core.App) {
	type due struct {
		forward addressForward
		stage   string
	}
	now := time.Now()
	dues := []due{}
	forwardingMu.Lock()
	forwarding := loadAddressForwarding(app)
	changed := false
	chosen := chosenServerOrigin()
	for index := range forwarding.Forwards {
		forward := &forwarding.Forwards[index]
		if forward.From == chosen || forward.EndNoticed {
			continue
		}
		until := time.UnixMilli(forward.Until)
		switch {
		case !now.Before(until):
			// One that ended long before, while the server was down, ends unsaid.
			if now.Sub(until) <= addressForwardAfterUse() {
				dues = append(dues, due{*forward, addressMoveEndedNotice})
			}
			forward.SoonNoticed, forward.EndNoticed, changed = true, true, true
		case !forward.SoonNoticed && until.Sub(now) <= addressForwardNoticeBefore():
			dues = append(dues, due{*forward, addressMoveSoonNotice})
			forward.SoonNoticed, changed = true, true
		}
	}
	if changed {
		saveAddressForwarding(app, forwarding)
	}
	forwardingMu.Unlock()
	for _, item := range dues {
		tellConnectionsToMove(app, item.forward, item.stage, now)
	}
}

// tellConnectionsToMove tells each person whose live connections came
// through the forward's address last, naming them, where they are moved:
// the agents' screen for an agent or a key, the integrations' for a
// Notion-compatible integration only.
func tellConnectionsToMove(app core.App, forward addressForward, stage string, now time.Time) {
	from := strings.TrimRight(forward.From, "/")
	rows := []struct {
		Owner string `db:"owner"`
		Name  string `db:"name"`
		Agent bool   `db:"agent"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[owner_id]] AS [[owner]], [[client_name]] AS [[name]], 1 AS [[agent]] FROM {{leafen_mcp_connections}}
		WHERE [[reached_via]] = {:from} AND [[revoked_at]] = 0 AND ([[token_expires_at]] = 0 OR [[token_expires_at]] > {:now})
		UNION ALL
		SELECT [[owner_id]], [[client_name]], 0 FROM {{leafen_notion_oauth_grants}}
		WHERE [[reached_via]] = {:from} AND [[status]] = 'active'
		UNION ALL
		SELECT [[creator_id]], [[name]], 0 FROM {{leafen_notion_personal_access_tokens}}
		WHERE [[reached_via]] = {:from} AND [[status]] = 'active' AND [[revoked_at]] IS NULL AND [[expires_at]] > {:now}
		ORDER BY 1, 2
	`).Bind(dbx.Params{"from": from, "now": now.UnixMilli()}).All(&rows); err != nil {
		app.Logger().Warn("leafen forward end notice failed", "address", from, "error", err)
		return
	}
	type owner struct {
		names []string
		agent bool
	}
	owners := map[string]*owner{}
	order := []string{}
	for _, row := range rows {
		if owners[row.Owner] == nil {
			owners[row.Owner] = &owner{}
			order = append(order, row.Owner)
		}
		owners[row.Owner].names = append(owners[row.Owner].names, row.Name)
		owners[row.Owner].agent = owners[row.Owner].agent || row.Agent
	}
	key := stage + ":" + appNoticeID(stage, from)
	projections := []productNotificationProjection{}
	for _, id := range order {
		workspaceID, err := personNoticeWorkspace(app, id, "")
		if err != nil || workspaceID == "" {
			continue
		}
		target := "/settings?section=integrations"
		if owners[id].agent {
			target = "/settings?section=agents"
		}
		projections = append(projections, productNotificationProjection{
			ID: appNoticeID(stage, from, id), EventID: appNoticeID(stage+"-event", from),
			WorkspaceID: workspaceID, UserID: id, ActivityKey: key, Kind: "system",
			PageID: agentNoticePagePrefix + "move", Title: strings.TrimRight(forward.To, "/"),
			Preview: strings.Join(owners[id].names, ", "), Target: target, OccurredAt: now.UnixMilli(),
		})
	}
	if _, err := upsertProductNotifications(context.Background(), app, projections, now.UnixMilli()); err != nil {
		app.Logger().Warn("leafen forward end notice failed", "address", from, "error", err)
	}
}
