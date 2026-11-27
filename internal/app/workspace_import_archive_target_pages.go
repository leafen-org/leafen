package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A Leafen archive comes only into an empty workspace: one with no pages but
// those the server keeps for the workspace itself — its People database and
// its people's pages (people_database.go). Every workspace has them, made by
// the server as its people come — before an import, while it runs or after —
// and no archive brings them, an export leaving them out as private to the
// server. workspaceArchiveTargetPages is that rule for every count an archive
// import makes of its target's pages: its admission, its staging, its
// content check, its page making and its publish (2026-11-16: the publish
// counted People's page as one the import had not brought, and retried
// forever). The same rule counts what a workspace holds wherever that is
// shown or compared: a move's counts on both servers (workspace_move_counts.go),
// the server administrator's page count of a workspace and what goes with a
// removed app; and what the whole server holds: a server move's counts on
// both servers (countServerContents), and the pages the server's start, a
// Notion migration and the backup suggestion count (serverHoldsCounts).

// workspaceArchiveTargetPageSQL reports whether the page aliased counts
// among its workspace's pages for an archive import: any page but the
// server's own, People's database page and its rows' pages.
func workspaceArchiveTargetPageSQL(page string) string {
	return `NOT (` + page + `.[[` + privatePageOwnerField + `]] = '` + leafenSystemAuthorID + `'
		OR ` + page + `.[[data_source_id]] IN (SELECT [[people]].[[data_source_id]]
			FROM {{leafen_workspace_people_databases}} AS [[people]]
			WHERE [[people]].[[workspace_id]] = ` + page + `.[[workspace]]))`
}

// workspaceArchiveTargetPages counts the workspace's pages by that rule,
// database rows' pages too unless pagesOnly.
func workspaceArchiveTargetPages(app core.App, workspaceID string, pagesOnly bool) (int, error) {
	count, only := 0, 0
	if pagesOnly {
		only = 1
	}
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_pages}} AS [[page]]
		WHERE [[page]].[[workspace]] = {:workspaceId} AND ({:pagesOnly} = 0 OR [[page]].[[data_source_id]] = '')
			AND ` + workspaceArchiveTargetPageSQL("[[page]]") + `
	`).Bind(dbx.Params{"workspaceId": workspaceID, "pagesOnly": only}).Row(&count)
	return count, err
}
