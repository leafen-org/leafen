package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// countWorkspaceMoveContents is what a move checks it carried: the
// workspace's pages, files and comments but those of the server's own pages
// — its People database and its people's pages, which every workspace has
// from its making and no move carries, each server keeping its own
// (workspaceArchiveTargetPageSQL). By that one rule a new workspace reads
// empty, ready to take a workspace moved here, and the two sides' counts
// compare only what went (2026-11-16: People made with every workspace
// read a new one as holding pages, and its people as pages left behind).
// A file counts unless every page that holds it is one of the server's own;
// a file's other references are found by the file, the workspace read after
// (+), or SQLite, which keeps no statistics, reads the workspace's every
// reference for each file.
func countWorkspaceMoveContents(app core.App, workspaceID string) (int, int, int) {
	var pages, files, comments int
	_ = app.DB().NewQuery(`WITH [[own]] AS (
			SELECT [[page]].[[id]] FROM {{leafen_pages}} AS [[page]]
			WHERE [[page]].[[workspace]] = {:id} AND NOT (`+workspaceArchiveTargetPageSQL("[[page]]")+`)
		)
		SELECT
		(SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[workspace]] = {:id} AND [[notion_import_staging]] = FALSE
			AND [[id]] NOT IN (SELECT [[id]] FROM [[own]])),
		(SELECT COUNT(*) FROM {{leafen_files}} WHERE [[workspace]] = {:id}) - (
			SELECT COUNT(DISTINCT [[held]].[[file_id]]) FROM {{leafen_file_references}} AS [[held]]
			WHERE [[held]].[[workspace_id]] = {:id} AND [[held]].[[page_id]] IN (SELECT [[id]] FROM [[own]])
				AND [[held]].[[file_id]] IN (SELECT [[id]] FROM {{leafen_files}} WHERE [[workspace]] = {:id})
				AND NOT EXISTS (SELECT 1 FROM {{leafen_file_references}} AS [[other]]
					WHERE +[[other]].[[workspace_id]] = {:id} AND [[other]].[[file_id]] = [[held]].[[file_id]]
						AND [[other]].[[page_id]] NOT IN (SELECT [[id]] FROM [[own]]))),
		(SELECT COUNT(*) FROM {{leafen_comments}} WHERE [[workspace_id]] = {:id}
			AND [[page_id]] NOT IN (SELECT [[id]] FROM [[own]]))`).
		Bind(dbx.Params{"id": workspaceID}).Row(&pages, &files, &comments)
	return pages, files, comments
}
