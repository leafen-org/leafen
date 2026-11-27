package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An OAuth client's refresh token changes at every refresh, and the one it
// replaces is kept (as its hash) as retired: Leafen's clients are public
// (dynamic registration, PKCE, no secret), so a stolen refresh token is told
// from the client's own by the one of them that later sends a token already
// replaced (RFC 9700 4.14.2, OAuth 2.1). A retired token that comes back
// within mcpOAuthRefreshRetryGrace is a retry whose response was lost and
// gets a new pair; one that comes back later ends its connection.

// mcpOAuthRefreshRetryGrace is how long a replaced refresh token still
// refreshes: a client that lost the response retries at once, within its
// request timeout (commonly 30 seconds), and a minute is as long as the
// longest such window other servers keep (Okta's at most 60 seconds) — no
// longer, as a thief's use within it goes unnoticed.
const mcpOAuthRefreshRetryGrace = time.Minute

// findRetiredMCPOAuthRefresh is the connection a replaced refresh token
// belonged to and when it was replaced.
func findRetiredMCPOAuthRefresh(app core.App, refreshHash string) (string, int64, error) {
	retired := struct {
		ConnectionID string `db:"connection_id"`
		RetiredAt    int64  `db:"retired_at"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[connection_id]], [[retired_at]] FROM {{leafen_mcp_oauth_retired_refresh_tokens}}
		WHERE [[token_hash]] = {:hash}
	`).Bind(dbx.Params{"hash": refreshHash}).One(&retired)
	return retired.ConnectionID, retired.RetiredAt, err
}

func retireMCPOAuthRefresh(app core.App, refreshHash string, connectionID string, now int64) error {
	_, err := app.DB().NewQuery(`
		INSERT OR IGNORE INTO {{leafen_mcp_oauth_retired_refresh_tokens}} ([[token_hash]], [[connection_id]], [[retired_at]])
		VALUES ({:hash}, {:connectionId}, {:now})
	`).Bind(dbx.Params{"hash": refreshHash, "connectionId": connectionID, "now": now}).Execute()
	return err
}

// endMCPOAuthConnectionOnRefreshReuse ends a connection whose replaced
// refresh token came back after the retry grace, with what it made.
func endMCPOAuthConnectionOnRefreshReuse(app core.App, connectionID string) error {
	now := time.Now().UnixMilli()
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_mcp_connections}}
		SET [[revoked_at]] = {:now}, [[retire_after]] = {:retireAfter}
		WHERE [[id]] = {:id} AND [[revoked_at]] = 0
	`).Bind(dbx.Params{
		"now": now, "retireAfter": now + int64(mcpCredentialRetentionMilliseconds), "id": connectionID,
	}).Execute()
	if err != nil {
		return err
	}
	if ended, err := result.RowsAffected(); err != nil || ended == 0 {
		return err
	}
	if err := revokeConnectionChildren(app, []string{connectionID}, now); err != nil {
		return err
	}
	notifyMCPCredentialRegistryAfterCommit(app)
	app.Logger().Warn("leafen mcp connection ended: a replaced refresh token was used again", "connection", connectionID)
	return nil
}

func findMCPOAuthConnectionByID(app core.App, connectionID string) (storedMCPOAuthConnection, error) {
	var connection storedMCPOAuthConnection
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[client_name]], [[token_hash]], [[token_expires_at]],
			[[refresh_token_hash]], [[refresh_expires_at]], [[oauth_client_id]], [[oauth_resource]],
			[[oauth_scope]], [[revoked_at]]
		FROM {{leafen_mcp_connections}}
		WHERE [[id]] = {:id} AND [[refresh_token_hash]] <> ''
	`).Bind(dbx.Params{"id": connectionID}).One(&connection)
	return connection, err
}

// findMCPOAuthConnectionForRefresh is the connection a refresh token is for:
// its current one, or one it replaced within the retry grace. A replaced
// token after the grace ends its connection (reused), and finds none.
func findMCPOAuthConnectionForRefresh(app core.App, refreshHash string) (connection storedMCPOAuthConnection, reused bool, err error) {
	connection, err = findMCPOAuthConnectionByRefresh(app, refreshHash)
	if !errors.Is(err, sql.ErrNoRows) {
		return connection, false, err
	}
	connectionID, retiredAt, err := findRetiredMCPOAuthRefresh(app, refreshHash)
	if err != nil {
		return connection, false, err
	}
	if time.Now().UnixMilli()-retiredAt >= mcpOAuthRefreshRetryGrace.Milliseconds() {
		return connection, true, endMCPOAuthConnectionOnRefreshReuse(app, connectionID)
	}
	connection, err = findMCPOAuthConnectionByID(app, connectionID)
	return connection, false, err
}
