package app

import (
	"context"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// When the server's address changes, everyone on it is told in their inbox
// (2026-10-20): the new address and, of what keeps the old one, what they use
// — their bookmarks always; notifications, turned on again at the new
// address; passkeys, made for the old address; app passwords, which sync
// apps keep the address beside; and, for administrators, the apps' webhooks
// written at outside services. A browser signed in there carries its sign-in
// over by itself while the old address forwards (address_handoff.go).

const (
	addressFixBookmarks     = "bookmarks"
	addressFixNotifications = "notifications"
	addressFixPasskeys      = "passkeys"
	addressFixSync          = "sync"
	addressFixWebhooks      = "webhooks"
)

// tellAddressChange tells, behind the change and stopped with the server
// (server_background.go), whoever the change of address concerns: the
// agents that came through the old one when it doesn't forward, every
// active person, and the administrators about their sign-in providers.
func tellAddressChange(app core.App, previous, address string) {
	background := serverBackgroundOf(app)
	background.behind("agent reconnect notice", func() { notifyAgentsCutOff(app, previous, address) })
	background.behind("address change notice", func() { notifyAddressChanged(app, previous, address) })
	background.behind("login redirect URIs", func() { keepAuthProviderReturnsBefore(app, previous, address) })
}

// notifyAddressChanged tells each active person the server's new address and
// what of theirs keeps the old one.
func notifyAddressChanged(app core.App, previous, address string) {
	people := []struct {
		ID            string `db:"id"`
		Notifications bool   `db:"notifications"`
		Passkeys      bool   `db:"passkeys"`
		Sync          bool   `db:"sync"`
		Webhooks      bool   `db:"webhooks"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[user]].[[id]],
			EXISTS (SELECT 1 FROM {{leafen_web_push_subscriptions}} [[push]] WHERE [[push]].[[user_id]] = [[user]].[[id]]) AS [[notifications]],
			EXISTS (SELECT 1 FROM {{leafen_passkeys}} [[passkey]] WHERE [[passkey]].[[user_id]] = [[user]].[[id]] AND [[passkey]].[[rp_id]] = {:previousHost}) AS [[passkeys]],
			EXISTS (SELECT 1 FROM {{leafen_app_passwords}} [[password]] WHERE [[password]].[[user_id]] = [[user]].[[id]]) AS [[sync]],
			EXISTS (SELECT 1 FROM {{leafen_administrators}} [[administrator]] WHERE [[administrator]].[[user_id]] = [[user]].[[id]])
				AND EXISTS (SELECT 1 FROM {{leafen_app_hook_secrets}}) AS [[webhooks]]
		FROM {{users}} [[user]]
		JOIN {{leafen_administrator_account_states}} [[account]] ON [[account]].[[user_id]] = [[user]].[[id]] AND [[account]].[[status]] = 'active'
		ORDER BY [[user]].[[id]]
	`).Bind(dbx.Params{"previousHost": hostOf(previous)}).All(&people); err != nil {
		app.Logger().Warn("leafen address change notice failed", "error", err)
		return
	}
	now := time.Now().UnixMilli()
	key := "address-changed:" + appNoticeID("address-changed", address)
	projections := []productNotificationProjection{}
	for _, person := range people {
		workspaceID, err := personNoticeWorkspace(app, person.ID, "")
		if err != nil || workspaceID == "" {
			continue
		}
		fixes := []string{addressFixBookmarks}
		for _, fix := range []struct {
			uses bool
			name string
		}{{person.Notifications, addressFixNotifications}, {person.Passkeys, addressFixPasskeys}, {person.Sync, addressFixSync}, {person.Webhooks, addressFixWebhooks}} {
			if fix.uses {
				fixes = append(fixes, fix.name)
			}
		}
		projections = append(projections, productNotificationProjection{
			ID: appNoticeID("address-changed", address, person.ID), EventID: appNoticeID("address-changed-event", address),
			WorkspaceID: workspaceID, UserID: person.ID, ActivityKey: key, Kind: "system",
			PageID: agentNoticePagePrefix + "address", Title: address, Preview: strings.Join(fixes, ","),
			Target: "/settings?section=account", OccurredAt: now,
		})
	}
	if len(projections) == 0 {
		return
	}
	if _, err := upsertProductNotifications(context.Background(), app, projections, now); err != nil {
		app.Logger().Warn("leafen address change notice failed", "error", err)
	}
}
