package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A residue lane clears its table once, but work beside the deletion can
// write into it again after the lane has passed. The search indexer settles
// the pages a workspace still has waiting, and each settle writes the
// workspace's search state: the row page lane, later than the search state's
// lane, takes row pages' own databases, whose places in search are marked
// waiting as they go, and the indexer wrote the state again behind its lane;
// the last step found it and refused at every retry, so the deletion never
// ended (2026-11-11). So the last step, finding a lane's rows again, sends
// the deletion back to that lane rather than refusing, and the lanes after it
// run again; an empty one costs a read of its index. What no lane clears
// still refuses the last step.

// workspaceDeletionPhaseToReturnTo is the phase that finds what was written
// behind it, or "" when nothing was. A page duplicated or imported into the
// workspace after its pages went sends the deletion back to its pages, and a
// file, custom emoji or export written after its files went back to its
// files. A database's row page goes only with its database, so one left
// without it still refuses the last step.
func workspaceDeletionPhaseToReturnTo(app core.App, workspaceID string) (string, error) {
	var pages, rowPages, files int
	if err := app.DB().NewQuery(`
		SELECT
			(SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[workspace]] = {:workspaceId} AND [[data_source_id]] = ''),
			(SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[workspace]] = {:workspaceId} AND [[data_source_id]] != ''),
			(SELECT COUNT(*) FROM {{leafen_files}} WHERE [[workspace]] = {:workspaceId})
				+ (SELECT COUNT(*) FROM {{leafen_custom_emojis}} WHERE [[workspace_id]] = {:workspaceId})
				+ (SELECT COUNT(*) FROM {{leafen_workspace_export_artifacts}} WHERE [[workspace_id]] = {:workspaceId})
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&pages, &rowPages, &files); err != nil {
		return "", err
	}
	switch {
	case pages != 0:
		return workspaceDeletionPhasePages, nil
	case rowPages != 0:
		return "", errWorkspaceDeleteConflict
	case files != 0:
		return workspaceDeletionPhaseFiles, nil
	}
	return "", nil
}

// workspaceDeletionLaneToReturnTo is the first residue lane that finds rows
// of the workspace again, or -1 when none does. A table that other lanes'
// tables hang on sends the deletion back to the first of those, so its rows
// never go before theirs.
func workspaceDeletionLaneToReturnTo(app core.App, workspaceID string) (int, error) {
	reads := []string{}
	for index, lane := range workspaceDeletionResidueLanes {
		for _, read := range workspaceDeletionLaneReads(lane) {
			reads = append(reads, fmt.Sprintf(
				"SELECT %d AS [[lane]] WHERE EXISTS (%s)",
				workspaceDeletionLaneFirstOf(index), read,
			))
		}
	}
	var lane int
	err := app.DB().NewQuery(
		"SELECT COALESCE(MIN([[lane]]), -1) FROM (" + strings.Join(reads, " UNION ALL ") + ")",
	).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&lane)
	return lane, err
}

// workspaceDeletionLaneReads are the reads that find what a lane clears, each
// by its table's index led by the workspace: a table's own rows, or the rows
// a shared owner's lane takes. A lane whose table hangs on another's is found
// through that table's.
func workspaceDeletionLaneReads(lane workspaceDeletionResidueLane) []string {
	switch {
	case lane.SharedOwner == "page-permanent-delete-receipts":
		return []string{`SELECT 1 FROM {{leafen_page_permanent_delete_jobs}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[status]] = 'completed'`}
	case lane.SharedOwner == "workspace-search-projection":
		reads := []string{}
		for _, table := range []string{"leafen_search_chunks", "leafen_search_pages", "leafen_search_dirty"} {
			reads = append(reads, "SELECT 1 FROM {{"+table+"}} WHERE [[workspace_id]] = {:workspaceId}")
		}
		return reads
	case lane.SharedOwner == "page-owned-leftovers":
		return workspaceDeletionPageLeftoverReads()
	case lane.SharedOwner != "" || lane.ParentTable != "":
		return nil
	}
	return []string{fmt.Sprintf(
		"SELECT 1 FROM {{%s}} WHERE [[%s]] = {:workspaceId}", lane.Table, lane.Key,
	)}
}

// workspaceDeletionLaneFirstOf is the lane to go back to for a lane's rows:
// the first lane of a table that hangs on its table, or the lane itself.
func workspaceDeletionLaneFirstOf(index int) int {
	table := workspaceDeletionResidueLanes[index].Table
	for first, lane := range workspaceDeletionResidueLanes[:index] {
		if table != "" && lane.ParentTable == table {
			return first
		}
	}
	return index
}
