package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// userKeyReachesSomeMessage is what an app's connection for only some of
// its person's workspaces hears on a call that crosses them — making or
// joining a workspace, signing in, keys and connections — with the way to
// one for all of them, granted again on Leafen's connection screen
// (2026-11-13: an agent asked to make a workspace heard only that it
// couldn't). An agent connection and an MCP token hear their own kind's
// words (connection_kind_ways.go).
const userKeyReachesSomeMessage = "This key reaches only some of your workspaces, so it can't do this; a key for all of them can. " +
	"To get one, connect again: your person chooses all their workspaces on Leafen's connection screen, " +
	"or allows the code you show them (a connection asked for by code reaches all of them)."

// keepUserKeyReachingAll keeps a key made for all of its person's
// workspaces (all_workspaces) reaching each one the person chooses from
// (userKeyPersonWorkspacesSQL), as those come and go. One they came to be
// in after it was made — one it or another of their keys made or joined,
// one they made or were invited to on their own screen — it reaches too, as
// the agent works on with them there; one it named that they no longer read
// (they left it, were removed, it is being deleted) it lets go, where a key
// for some of them stops being valid. Its connection's workspaces change as
// its key is next read, the way every reader of them sees it (the agent's
// calls, its MCP tools, its person's list of connections); a read finding
// nothing to change writes nothing. Without it a key reached only the
// workspaces it named when made (2026-11-13: one of a person's two such
// keys made a workspace, and the other, no longer for all of them, was
// refused what only such a key does). A change that couldn't be made is
// written to the log, and the key works with the workspaces it has.
func keepUserKeyReachingAll(app core.App, connectionID, ownerID string) {
	changes := []struct {
		WorkspaceID string `db:"workspace_id"`
		Gained      bool   `db:"gained"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[person]].[[id]] AS [[workspace_id]], 1 AS [[gained]] FROM (` + userKeyPersonWorkspacesSQL + `) [[person]]
		WHERE [[person]].[[id]] NOT IN (
			SELECT [[workspace_id]] FROM {{` + mcpConnectionWorkspacesTable + `}} WHERE [[connection_id]] = {:connection})
		UNION ALL
		SELECT [[named]].[[workspace_id]], 0 FROM {{` + mcpConnectionWorkspacesTable + `}} [[named]]
		WHERE [[named]].[[connection_id]] = {:connection}
			AND [[named]].[[workspace_id]] NOT IN (SELECT [[id]] FROM (` + userKeyPersonWorkspacesSQL + `))
	`).Bind(dbx.Params{"person": ownerID, "active": organizationMemberStatusActive, "connection": connectionID}).
		All(&changes); err != nil {
		app.Logger().Warn("leafen key's workspaces unread", "connection", connectionID, "error", err)
		return
	}
	now := time.Now().UnixMilli()
	for _, change := range changes {
		params := dbx.Params{"connection": connectionID, "workspace": change.WorkspaceID, "now": now}
		var err error
		if change.Gained {
			_, err = app.DB().NewQuery(`
				INSERT OR IGNORE INTO {{` + mcpConnectionWorkspacesTable + `}} ([[connection_id]], [[workspace_id]], [[created]])
				SELECT {:connection}, {:workspace}, {:now}
				WHERE EXISTS (SELECT 1 FROM {{leafen_mcp_connections}}
					WHERE [[id]] = {:connection} AND [[revoked_at]] = 0 AND [[all_workspaces]] = 1)
			`).Bind(params).Execute()
		} else if _, denied := authorizeWorkspace(app, ownerID, change.WorkspaceID, permissionWorkspaceRead); isPermissionDenied(denied) {
			// One they still read outside the ones they choose from (a guest's
			// place, an import's staging) it keeps.
			_, err = app.DB().NewQuery(`
				DELETE FROM {{` + mcpConnectionWorkspacesTable + `}}
				WHERE [[connection_id]] = {:connection} AND [[workspace_id]] = {:workspace}
			`).Bind(params).Execute()
		} else {
			err = denied
		}
		if err != nil {
			app.Logger().Warn("leafen key's workspaces not kept to its person's",
				"connection", connectionID, "workspace", change.WorkspaceID, "error", err)
		}
	}
}

// keepUserKeysReachingAll does it for each of a person's live keys made
// for all of their workspaces, before their list of connections shows
// what each reaches.
func keepUserKeysReachingAll(app core.App, ownerID string) {
	connectionIDs := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_mcp_connections}}
		WHERE [[owner_id]] = {:owner} AND [[revoked_at]] = 0 AND [[all_workspaces]] = 1
	`).Bind(dbx.Params{"owner": ownerID}).Column(&connectionIDs); err != nil {
		app.Logger().Warn("leafen keys for all workspaces unread", "owner", ownerID, "error", err)
		return
	}
	for _, connectionID := range connectionIDs {
		keepUserKeyReachingAll(app, connectionID, ownerID)
	}
}
