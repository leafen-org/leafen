package app

import (
	"encoding/hex"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func workspaceSearchPageToken(pageID string) string {
	return "p" + hex.EncodeToString([]byte(pageID))
}

func workspaceSearchScopeToken(workspaceID string) string {
	return "w" + hex.EncodeToString([]byte(workspaceID))
}

func workspaceSearchFTSTerm(column, token string) string {
	return column + `:"` + strings.ReplaceAll(token, `"`, `""`) + `"`
}

// deleteWorkspaceSearchFTSPage lets go of a page's rows in the index, its
// own and its chunks' (workspace_search_chunks.go), a bounded batch at a time.
func deleteWorkspaceSearchFTSPage(app core.App, pageID string) (bool, error) {
	more, err := deleteWorkspaceSearchFTSRows(app, workspaceSearchFTSTerm("page_token", workspaceSearchPageToken(pageID))+
		" OR "+workspaceSearchFTSTerm("page_token", workspaceSearchChunkPageToken(pageID)))
	if err != nil || more {
		return more, err
	}
	// The chunks kept with them, whose rows are gone.
	_, err = app.DB().NewQuery(`DELETE FROM {{leafen_search_chunks}} WHERE [[page_id]] = {:pageId}`).
		Bind(dbx.Params{"pageId": pageID}).Execute()
	return false, err
}

// deleteWorkspaceSearchFTSPageRow lets go of a page's own row alone.
func deleteWorkspaceSearchFTSPageRow(app core.App, pageID string) (bool, error) {
	return deleteWorkspaceSearchFTSRows(app, workspaceSearchFTSTerm("page_token", workspaceSearchPageToken(pageID)))
}

func deleteWorkspaceSearchFTSRows(app core.App, match string) (bool, error) {
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_search_pages_fts}} WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_search_pages_fts}}
			WHERE {{leafen_search_pages_fts}} MATCH {:pageMatch}
			ORDER BY [[rowid]] LIMIT {:limit}
		)
	`).Bind(dbx.Params{
		"pageMatch": match,
		"limit":     maxPagePermanentDeleteItemsPerRequest,
	}).Execute()
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected >= maxPagePermanentDeleteItemsPerRequest, nil
}

// cleanWorkspaceSearchProjection clears, a bounded batch at a time, what the
// search projection still keeps of a workspace whose pages are all gone:
// database row pages leave the workspace with their database, not through
// the page lanes, so their queued, projected and indexed entries stay
// behind. Each batch goes in the order of the index its table keeps by
// workspace, which reads only that batch's rows. Reports whether more is
// left.
//
// A batch of kept chunks or pages takes its rows in the index with it, read
// by where each stands there (fts_row), in the same step. The index is read
// by the workspace's tokens last, once the kept and queued rows are gone, for
// rows no kept one names: such a read walks every row the workspace's token
// ever had, deleted ones too. With it read on every step, the lane took 6 to
// 8 minutes for a workspace of 100,000 database rows; read once, about half
// a minute (2026-11-10).
func cleanWorkspaceSearchProjection(app core.App, workspaceID string, limit int) (bool, error) {
	// The statements of a batch read the same rows, by the same query in one
	// transaction, and all run; the first batch that finds any is the step.
	batches := [][]string{
		{
			`DELETE FROM {{leafen_search_pages_fts}} WHERE [[rowid]] IN (
				SELECT [[fts_row]] FROM {{leafen_search_chunks}} WHERE [[workspace_id]] = {:workspaceId} ORDER BY [[rowid]] LIMIT {:limit})`,
			`DELETE FROM {{leafen_search_chunks}} WHERE [[rowid]] IN (
				SELECT [[rowid]] FROM {{leafen_search_chunks}} WHERE [[workspace_id]] = {:workspaceId} ORDER BY [[rowid]] LIMIT {:limit})`,
		},
		{
			`DELETE FROM {{leafen_search_pages_fts}} WHERE [[rowid]] IN (
				SELECT [[fts_row]] FROM {{leafen_search_pages}} WHERE [[workspace_id]] = {:workspaceId} ORDER BY [[page_id]] LIMIT {:limit})`,
			`DELETE FROM {{leafen_search_pages}} WHERE [[rowid]] IN (
				SELECT [[rowid]] FROM {{leafen_search_pages}} WHERE [[workspace_id]] = {:workspaceId} ORDER BY [[page_id]] LIMIT {:limit})`,
		},
		{`DELETE FROM {{leafen_search_dirty}} WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_search_dirty}} WHERE [[workspace_id]] = {:workspaceId} ORDER BY [[page_id]] LIMIT {:limit})`},
		{`DELETE FROM {{leafen_search_pages_fts}} WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_search_pages_fts}} WHERE {{leafen_search_pages_fts}} MATCH {:scope}
			ORDER BY [[rowid]] LIMIT {:limit})`},
		{`DELETE FROM {{leafen_search_pages_fts}} WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_search_pages_fts}} WHERE {{leafen_search_pages_fts}} MATCH {:chunkScope}
			ORDER BY [[rowid]] LIMIT {:limit})`},
	}
	params := dbx.Params{
		"scope":       workspaceSearchFTSTerm("workspace_token", workspaceSearchScopeToken(workspaceID)),
		"chunkScope":  workspaceSearchFTSTerm("workspace_token", workspaceSearchChunkScopeToken(workspaceID)),
		"workspaceId": workspaceID, "limit": limit,
	}
	for _, batch := range batches {
		found := false
		for _, statement := range batch {
			result, err := app.DB().NewQuery(statement).Bind(params).Execute()
			if err != nil {
				return false, err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return false, err
			}
			found = found || affected > 0
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}
