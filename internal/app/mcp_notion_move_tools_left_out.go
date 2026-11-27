package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A connection with no Notion move to make leaves the move's tools after its
// guide out of its list (the user, 2026-11-26: a session at work in a folder
// whose connection reached one personal workspace, already moved, had all
// thirteen on its list): one that can't make a move — the person's 06
// decision 4, a list filtered by what the connection may do — and one whose
// workspaces' moves are done, as the server keeps them. A new Leafen's first
// connection lists them all, so the first ten minutes stay as they are; a
// move that goes on, or one in a workspace not moved yet, keeps them. The
// guide stays and its answer runs them through leafen_call
// (notion_migration_guide_calls.go). A folder's connection is an ordinary
// one: it can move where its scope and its person's rights reach.

// Why a connection's list leaves the move's tools out.
const (
	mcpNotionMoveCannot = "cannot"
	mcpNotionMoveDone   = "done"
)

// mcpNotionMoveToolsLeftOut says why the connection's list leaves the move's
// tools out, or "" where it lists them. It can't make a move when its scope
// only reads, its page holds it to chosen pages, or its person may add pages
// in none of its workspaces, as a move's start needs (mcpNotionMigrationWorkspace);
// its moves are done when each of its workspaces holds a move of its
// person's whose run ended and none still going.
func mcpNotionMoveToolsLeftOut(app core.App, authority mcpAuthority) string {
	if authority.Scope < mcpToolScope("leafen_notion_migration_start") || authority.PagesNarrowed {
		return mcpNotionMoveCannot
	}
	writable := 0
	for _, workspaceID := range authority.WorkspaceIDs {
		if _, err := authorizeWorkspace(app, authority.owner(), workspaceID, permissionWorkspaceWrite); err == nil {
			writable++
		}
	}
	if writable == 0 {
		return mcpNotionMoveCannot
	}
	for _, workspaceID := range authority.WorkspaceIDs {
		if !mcpNotionMoveDoneIn(app, authority.owner(), workspaceID) {
			return ""
		}
	}
	return mcpNotionMoveDone
}

// mcpNotionMoveDoneIn says whether the person's moves into the workspace
// are done: one ran to its end, and none is under way.
func mcpNotionMoveDoneIn(app core.App, owner, workspaceID string) bool {
	state := struct {
		Done  int `db:"done"`
		Going int `db:"going"`
	}{}
	err := app.DB().NewQuery(`SELECT
		COALESCE(SUM([[run_state]] = 'done'), 0) AS [[done]],
		COALESCE(SUM([[run_state]] != 'done' AND [[status]] != 'failed'), 0) AS [[going]]
		FROM {{leafen_notion_migrations}} INDEXED BY [[idx_leafen_notion_migrations_owner]]
		WHERE [[workspace_id]] = {:workspace} AND [[owner]] = {:owner}`).
		Bind(dbx.Params{"workspace": workspaceID, "owner": owner}).One(&state)
	return err == nil && state.Done > 0 && state.Going == 0
}
