package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A browser stays signed in while it is used: a sign-in lasts thirty days
// without use, and a request on a sign-in renewed more than a day ago
// extends it by another thirty days and hands the browser a token that
// lasts as long. Only a request that says it takes the new token renews
// (the app's own requests), so no renewal is lost on a path that can't keep
// it. The token the request carried keeps working until its own end; a
// revoked or expired sign-in is never renewed. A managed user's sign-in is
// never renewed past its organization's session duration (deadline_at). A
// device that joined with a code (account_device_codes.go) keeps its end,
// until it is cut off.

const (
	// authSessionIdleLifetime is how long a sign-in lasts without use.
	authSessionIdleLifetime = 30 * 24 * time.Hour
	// authSessionRenewInterval is how often a sign-in in use is renewed.
	authSessionRenewInterval = 24 * time.Hour
	// authSessionTokenHeader carries a renewed sign-in's token.
	authSessionTokenHeader = "X-Leafen-Session-Token"
	// authSessionRenewHeader is set ("1") on a request that takes a renewed token.
	authSessionRenewHeader = "X-Leafen-Session-Renew"
)

// renewBrowserSession extends the open session a request came on when it
// was last renewed (or made) more than a renewal interval ago, and sets the
// new token on the response. Requests racing to renew it renew it once.
// Whether it is due is read first: the renewal writes, and a write waits for
// the one write connection, so every request of the app waited behind the
// changes under way though it renews at most once a day (2026-11-14, NAS
// condition: an API write burst held each read of a page opening 1–3 s).
func renewBrowserSession(event *core.RequestEvent, sessionID string) error {
	if event.Request.Header.Get(authSessionRenewHeader) != "1" {
		return nil
	}
	now := time.Now().UTC()
	nowMillis := now.UnixMilli()
	params := dbx.Params{
		"id": sessionID, "ownerId": event.Auth.Id, "now": nowMillis,
		"expiresAt": now.Add(authSessionIdleLifetime).UnixMilli(),
		"due":       now.Add(-authSessionRenewInterval).UnixMilli(),
		"device":    authSessionMethodDevice,
	}
	var due int
	if err := event.App.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_auth_sessions}}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId}
			AND [[revoked_at]] = 0 AND [[expires_at]] > {:now}
			AND COALESCE([[rotated_at]], [[created_at]]) <= {:due}
	`).Bind(params).Row(&due); err != nil || due == 0 {
		return err
	}
	result, err := event.App.DB().NewQuery(`
		UPDATE {{leafen_auth_sessions}}
		SET [[expires_at]] = CASE WHEN [[sign_in_method]] = {:device} THEN [[expires_at]]
				WHEN [[deadline_at]] > 0 AND [[deadline_at]] < {:expiresAt}
				THEN [[deadline_at]] ELSE {:expiresAt} END,
			[[rotated_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId}
			AND [[revoked_at]] = 0 AND [[expires_at]] > {:now}
			AND COALESCE([[rotated_at]], [[created_at]]) <= {:due}
	`).Bind(params).Execute()
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return err
	}
	// The token lasts as long as the sign-in now does.
	var expiresAt int64
	if err := event.App.DB().NewQuery(`SELECT [[expires_at]] FROM {{leafen_auth_sessions}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": sessionID}).Row(&expiresAt); err != nil {
		return err
	}
	token, err := newBrowserSessionToken(event.Auth, sessionID, time.UnixMilli(expiresAt).Sub(now))
	if err != nil {
		return err
	}
	event.Response.Header().Set(authSessionTokenHeader, token)
	return nil
}
