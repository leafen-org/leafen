package app

import (
	"errors"
	"net/http"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The address each connection last came through (reached_via): an agent's
// (noteMCPConnectionReach), a program's calling the API with its key, and a
// Notion-compatible integration's — its OAuth grant, an internal
// connection's grant, a personal access token (2026-11-22, user decision on
// an address change's forwarding end: whoever's connection still uses an
// earlier address is told to move it before the forward ends,
// server_address_forwarding_end_notice.go). A program's address is written
// only when it changes, noted in memory for the run and written behind the
// call, so a call waits for no write.

// connectionReachNoted is the address each connection was last noted coming
// through in this run, by "mcp:", "grant:" or "token:" and its id.
var connectionReachNoted sync.Map

// noteConnectionReachBehind writes where a connection came through, when it
// changed since this run last noted it, behind the call.
func noteConnectionReachBehind(app core.App, table, key, id, via string) {
	if id == "" {
		return
	}
	if noted, ok := connectionReachNoted.Load(key + id); ok && noted == via {
		return
	}
	connectionReachNoted.Store(key+id, via)
	serverBackgroundOf(app).behind("connection reach", func() {
		_, _ = app.DB().NewQuery(`UPDATE {{` + table + `}} SET [[reached_via]] = {:via} WHERE [[id]] = {:id} AND [[reached_via]] <> {:via}`).
			Bind(dbx.Params{"via": via, "id": id}).Execute()
	})
}

// noteKeyUse notes a call with a key: its use, and, for a call from outside
// the server, the address it came through.
func noteKeyUse(app core.App, request *http.Request, authority userKeyAuthority, internal bool) {
	noteMCPConnectionUse(app, authority.ConnectionID, authority.LastUsedAt)
	if !internal {
		noteConnectionReachBehind(app, "leafen_mcp_connections", "mcp:", authority.ConnectionID, mcpReachedVia(request))
	}
}

// noteNotionCredentialReach notes the address a Notion-compatible
// integration's call came through: a personal access token's own, else its
// grant's (an OAuth integration's or an internal connection's).
func noteNotionCredentialReach(app core.App, request *http.Request, identity notionOAuthBearerIdentity) {
	if identity.ClientID == "personal-access-token" {
		noteConnectionReachBehind(app, "leafen_notion_personal_access_tokens", "token:", identity.GrantID, mcpReachedVia(request))
		return
	}
	noteConnectionReachBehind(app, notionOAuthGrantsTable, "grant:", identity.GrantID, mcpReachedVia(request))
}

// ensureNativeConnectionReachSchema gives the Notion-compatible grants and
// personal access tokens the address they came through, as agents'
// connections have it.
func ensureNativeConnectionReachSchema(app core.App) error {
	for _, statement := range []string{
		`ALTER TABLE {{leafen_notion_oauth_grants}} ADD COLUMN [[reached_via]] TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE {{leafen_notion_personal_access_tokens}} ADD COLUMN [[reached_via]] TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := app.DB().NewQuery(statement).Execute(); err != nil {
			return err
		}
	}
	return validateNativeConnectionReachSchema(app)
}

func validateNativeConnectionReachSchema(app core.App) error {
	var columns int
	if err := app.DB().NewQuery(`SELECT
		(SELECT COUNT(*) FROM pragma_table_info('leafen_notion_oauth_grants') WHERE [[name]] = 'reached_via' AND [[notnull]] = 1) +
		(SELECT COUNT(*) FROM pragma_table_info('leafen_notion_personal_access_tokens') WHERE [[name]] = 'reached_via' AND [[notnull]] = 1)`).
		Row(&columns); err != nil {
		return err
	}
	if columns != 2 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
