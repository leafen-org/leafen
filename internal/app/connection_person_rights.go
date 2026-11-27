package app

import (
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person's connection works as its person (user decisions of 2026-11-26,
// on the agent full-permission decision of 2026-10-16), in one of two kinds,
// and neither has a grade of its own to pick, widen or show:
//
//   - An agent connection is its person's shadow: made by Leafen's MCP OAuth,
//     a one-time connection code, a device code, `leafen connect` or another
//     of their agents' yes (agent_connection 1). It does everything its
//     person may, in every one of their workspaces, those they come to be in
//     later too (user_key_reach_gained.go); nothing narrows it, and a call
//     that needs a workspace no target of its decides names one
//     (mcpWorkspaceNeeded).
//   - An MCP token, made on its page (Settings › agents' MCP address and
//     key, the developer tools' MCP tokens, POST /mcp/connections), is as
//     Notion's: one workspace, the tools turned on on its page and the pages
//     chosen there. What it may do follows those tools (mcpScopeForTools).
//
// What either may do in a workspace is what its person may there, checked
// where each call acts; an organization's key policy still holds it as it
// holds its person's keys (userKeyPolicyCap), and what can't be undone still
// asks its person in the conversation. An installed app's connection keeps
// the access its app declares (appPersonAccess), and an outside app granted
// the native API over OAuth the scope it was granted: those are the app's.
// What each kind hears of what it can do, and of the way to more, is its
// kind's (connection_kind_ways.go).

// personConnectionScopeName is the scope a person's connection is made with:
// everything its person may, until its page turns tools off.
var personConnectionScopeName = userKeyScopeNames[userKeyScopeAdmin]

// agentConnectionRequestPrefixes start the request a one-time code, a device
// code or `leafen connect` makes a connection with (an OAuth connection is
// told by its grant instead).
var agentConnectionRequestPrefixes = []string{"code:", "device:", "command:"}

// agentConnectionRequest says whether a connection made with this request is
// an agent connection rather than an MCP token.
func agentConnectionRequest(requestID string) bool {
	for _, prefix := range agentConnectionRequestPrefixes {
		if strings.HasPrefix(requestID, prefix) {
			return true
		}
	}
	return false
}

// oauthAgentConnection says whether an OAuth grant makes an agent
// connection: an agent's program granted Leafen's MCP, not an app
// registered with Leafen (lapp_, whose own account or person it acts as)
// nor an outside app granted the native API.
func oauthAgentConnection(clientID, resource string) bool {
	return clientID != "" && !strings.HasPrefix(clientID, appRegistrationClientPrefix) && !oauthResourceIsNative(resource)
}

// appRegistrationClientPrefix starts the OAuth client of an app registered
// with Leafen (app_registrations.go).
const appRegistrationClientPrefix = "lapp_"

// agentConnectionWorkspaceIDs are the workspaces an agent connection of the
// person's reaches: all of theirs it may (userKeyPersonWorkspacesSQL).
func agentConnectionWorkspaceIDs(app core.App, ownerID string) ([]string, error) {
	workspaceIDs, err := userKeyPersonWorkspaces(app, ownerID)
	if err == nil && len(workspaceIDs) == 0 {
		err = errAgentConnectNoWorkspace
	}
	return uniqueNonemptyStrings(workspaceIDs), err
}

// mcpTokenNotAgent is why an MCP token — kept to one workspace, or to the
// pages chosen on its page — doesn't do what only a connection with all its
// person's reach does.
func mcpTokenNotAgent(doing string) string {
	return doing + " takes an agent connection, which reaches all its person's workspaces and pages; this one is an MCP token kept " +
		"to its workspace or to the pages chosen on its page, as Notion's tokens are. Your person connects you as an agent by a " +
		"connection code or `leafen connect`."
}

// errMCPTokenOneWorkspace refuses a new MCP token naming several workspaces.
var errMCPTokenOneWorkspace = errors.New("An MCP token is for one workspace, as Notion's are: make one token for each workspace " +
	"(an agent connection, by a connection code or `leafen connect`, reaches all of its person's workspaces).")

// Migration 866 marks every agent connection kept before it (an OAuth grant
// for MCP, a request a code, a device code or the command made) and gives it
// its person's rights in all their workspaces, its default and a Notion
// move's lent rights gone, and a key's request for more still waiting ended;
// one whose person can't act now — gone,
// deactivated or suspended — and that would gain by it is ended instead, so
// it never comes back with more than it had: its agent connects again. An
// MCP token keeps what it may do and its workspaces — the scope it was made
// with shows on its page as the tools above it turned off, and turning one
// on there gives what that tool needs (mcpScopeForTools) —; only its default
// goes, as no connection keeps one.

const agentConnectionMarkSQL = `[[app_install_id]] = '' AND (
	([[oauth_client_id]] <> '' AND [[oauth_client_id]] NOT LIKE '` + appRegistrationClientPrefix + `%'
		AND [[oauth_resource]] NOT LIKE '%` + apiRoot + `')
	OR [[request_id]] LIKE 'code:%' OR [[request_id]] LIKE 'device:%' OR [[request_id]] LIKE 'command:%')`

func ensureNativePersonConnectionKindsSchema(app core.App) error {
	if _, err := app.DB().NewQuery(`ALTER TABLE {{leafen_mcp_connections}}
		ADD COLUMN [[agent_connection]] INTEGER NOT NULL DEFAULT 0`).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_mcp_connections}} SET [[agent_connection]] = 1 WHERE ` + agentConnectionMarkSQL).Execute(); err != nil {
		return err
	}
	// A key's request for more, still waiting, asks for nothing now: it ends.
	_, err := app.DB().NewQuery(`UPDATE {{leafen_mcp_device_connections}} SET [[expires_at]] = {:now}
		WHERE [[status]] = 'pending' AND [[widens_connection]] <> ''`).Bind(dbx.Params{"now": time.Now().UnixMilli()}).Execute()
	return err
}

type keptPersonConnection struct {
	ID       string `db:"id"`
	OwnerID  string `db:"owner_id"`
	Scope    string `db:"access_scope"`
	All      int64  `db:"all_workspaces"`
	Disabled string `db:"disabled_tools"`
	Agent    int64  `db:"agent_connection"`
	MayAct   int64  `db:"may_act"`
}

func liftAgentConnections(app core.App) error {
	rows := []keptPersonConnection{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[owner_id]], [[access_scope]], [[all_workspaces]], [[disabled_tools]],
			[[agent_connection]], ` + accountMayActSQL("[[owner_id]]") + ` AS [[may_act]]
		FROM {{leafen_mcp_connections}}
		WHERE [[revoked_at]] = 0 AND [[app_install_id]] = '' AND ([[oauth_client_id]] = '' OR [[oauth_resource]] NOT LIKE '%` + apiRoot + `')`).
		All(&rows); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for _, row := range rows {
		change := dbx.Params{"default_workspace_id": "", "migration_id": "", "migration_scope_until": 0}
		if row.Agent == 1 {
			kept, _ := parseUserKeyScope(row.Scope)
			full := userKeyScopeAdmin
			if disabled := parseMCPNameList(row.Disabled); len(disabled) > 0 {
				full = mcpScopeForTools(app, disabled)
			}
			if row.MayAct != 1 && (full > kept || row.All == 0) {
				change = dbx.Params{"revoked_at": now}
			} else {
				change["access_scope"], change["all_workspaces"] = userKeyScopeNames[full], 1
			}
		}
		if _, err := app.DB().Update(mcpConnectionsTable, change, dbx.HashExp{"id": row.ID}).Execute(); err != nil {
			return err
		}
		if _, revoked := change["revoked_at"]; row.Agent == 1 && !revoked {
			keepUserKeyReachingAll(app, row.ID, row.OwnerID)
		}
	}
	return nil
}

func validateAgentConnectionsLifted(app core.App) error {
	var columns, narrowed int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_mcp_connections')
		WHERE [[name]] = 'agent_connection' AND [[notnull]] = 1`).Row(&columns); err != nil {
		return err
	}
	if columns != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_mcp_connections}}
		WHERE [[revoked_at]] = 0 AND [[app_install_id]] = '' AND ([[default_workspace_id]] <> '' OR [[agent_connection]] = 1 AND [[all_workspaces]] = 0)`).
		Row(&narrowed); err != nil {
		return err
	}
	if narrowed != 0 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
