package app

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Rows a workspace's pages own that outlive the page lanes. The page lanes
// clear a page's rows table by table, each in a step of its own, and work
// beside the deletion can write a page-owned row after its table's lane has
// passed: an automation's schedule reads its definition, which the lanes
// clear last, and writes the run's event and occurrence behind their lanes;
// a row's follower fanout, which no page lane clears, writes its
// notifications in a later transaction than the one that read it (seen in
// the code, 2026-11-11). Some rows no writer leaves: the trash's record of a
// data source kept in the trash where it stands goes with its source only on
// the trash's retention's next turn. The page-owned tables keep no lane by
// workspace, so such a row stopped the last step for good.
//
// Once the workspace's pages are all gone, a row a page-owned table keeps for
// the workspace is one no page owns any more. So the "page-owned-leftovers"
// residue lane clears every page-owned table the inventory names by
// workspace, a window a step, and the last step finds them through
// workspaceDeletionLaneReads and sends the deletion back to the lane when one
// is written again. The pages themselves go only through the page lanes (the
// last step sends a page written late back to them), and the search
// projection's tables through their own lane, which takes their full-text
// rows with them. Every page-owned table keeps an index led by its workspace
// (validateWorkspaceDeletionAccessPaths), which a window and a read use.

// workspaceDeletionPageLeftoverTables are the page-owned tables the lane
// clears, in the inventory's order: all but the pages and the search
// projection's tables.
var workspaceDeletionPageLeftoverTables = func() []workspaceDeletionResidueLane {
	tables := []workspaceDeletionResidueLane{}
	for _, authority := range workspaceDeletionDirectAuthorities {
		if authority.Disposition != workspaceDeletionAuthorityPage {
			continue
		}
		switch authority.Table {
		case "leafen_pages", "leafen_search_dirty", "leafen_search_pages", "leafen_search_chunks":
			continue
		}
		tables = append(tables, workspaceDeletionResidueLane{Table: authority.Table, Key: authority.Key})
	}
	return tables
}()

// cleanWorkspacePageLeftovers deletes a window of the workspace's rows from
// the first of the lane's tables that has any, by the index led by its key,
// in that index's order so no window sorts the workspace's rows; a table's
// foreign keys take what hangs on its rows with them. Reports whether a
// window found any, so the lane ends on a step that finds none.
func cleanWorkspacePageLeftovers(app core.App, workspaceID string, limit int) (bool, error) {
	for _, leftover := range workspaceDeletionPageLeftoverTables {
		result, err := app.DB().NewQuery(fmt.Sprintf(`
			DELETE FROM {{%s}} WHERE [[rowid]] IN (
				SELECT [[rowid]] FROM {{%s}} WHERE [[%s]] = {:workspaceId} LIMIT {:limit}
			)
		`, leftover.Table, leftover.Table, leftover.Key)).Bind(dbx.Params{
			"workspaceId": workspaceID, "limit": limit,
		}).Execute()
		if err != nil {
			return false, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		if rows > 0 {
			return true, nil
		}
	}
	return false, nil
}

// workspaceDeletionPageLeftoverReads find the rows the lane clears.
func workspaceDeletionPageLeftoverReads() []string {
	reads := make([]string, 0, len(workspaceDeletionPageLeftoverTables))
	for _, leftover := range workspaceDeletionPageLeftoverTables {
		reads = append(reads, fmt.Sprintf(
			"SELECT 1 FROM {{%s}} WHERE [[%s]] = {:workspaceId}", leftover.Table, leftover.Key,
		))
	}
	return reads
}
