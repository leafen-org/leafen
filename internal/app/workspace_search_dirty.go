package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A change that search reads marks its page dirty: one row per page, its
// version bumped by each change, which the indexer settles
// (workspace_search_indexer.go, workspace_search_projection.go).
//
// A sub-page's title is a line of the page whose body holds its block
// (searchPageChildLines). Making, moving, trashing or restoring a sub-page
// changes that block, which marks the page holding it
// (saveServerStructuralBatch); a new title marks it when the index takes the
// sub-page's title anew (searchEntryRetitled), whatever wrote it — a rename
// from the page, the API or an agent, a version restored, an import.

// searchEntryRetitled is whether a page's index entry held another title than
// the one it is made with now; a page not indexed yet held none.
func searchEntryRetitled(app core.App, projection searchPageProjection) (bool, error) {
	stored := struct {
		Title       string `db:"title"`
		TitleFolded string `db:"title_folded"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[title]], [[title_folded]] FROM {{leafen_search_pages}} WHERE [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"pageId": projection.PageID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return stored.Title != projection.Title || stored.TitleFolded != projection.TitleFolded, nil
}

// markSearchSubPageLineDirty marks the page whose body holds a sub-page's
// block, or a wiki's home holding the link that stands for its page there
// (searchPageChildLines), to be made whole again with the page's line.
func markSearchSubPageLineDirty(app core.App, workspaceID, pageID string) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_search_dirty}} ([[page_id]], [[workspace_id]], [[version]], [[enqueued_at]])
		SELECT [[id]], [[workspace]], 1, {:now} FROM (
			SELECT [[holder]].[[id]], [[holder]].[[workspace]]
			FROM {{leafen_blocks}} [[block]] JOIN {{leafen_pages}} [[holder]] ON [[holder]].[[id]] = [[block]].[[page_id]]
			WHERE [[block]].[[id]] = {:pageId} AND [[block]].[[type]] = {:type}
			UNION
			SELECT [[holder]].[[id]], [[holder]].[[workspace]]
			FROM {{leafen_block_page_references}} [[reference]] INDEXED BY [[idx_leafen_block_page_references_target]]
			JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[reference]].[[block_id]]
			JOIN {{leafen_pages}} [[holder]] ON [[holder]].[[id]] = [[block]].[[page_id]]
			WHERE [[reference]].[[workspace_id]] = {:workspaceId} AND [[reference]].[[target_page_id]] = {:pageId}
				AND ` + wikiPageLinkSQL("[[block]]") + `
		) WHERE TRUE
		ON CONFLICT ([[page_id]]) DO UPDATE SET
			[[workspace_id]] = excluded.[[workspace_id]],
			[[version]] = [[leafen_search_dirty]].[[version]] + 1,
			[[enqueued_at]] = excluded.[[enqueued_at]]
	`).Bind(dbx.Params{
		"pageId": pageID, "workspaceId": workspaceID, "type": graphBlockTypeChildPage, "now": time.Now().UnixMilli(),
	}).Execute()
	return err
}

func markSearchDirty(app core.App, page *core.Record) error {
	return markSearchDirtyRecords(app, []*core.Record{page})
}

func markSearchDirtyRecords(app core.App, pages []*core.Record) error {
	targets := make([]searchDirtyTarget, 0, len(pages))
	for _, page := range pages {
		if page == nil || page.Id == "" || page.GetString("workspace") == "" {
			return errors.New(errorCodeWorkspaceSearchProjectionFailed)
		}
		targets = append(targets, searchDirtyTarget{
			PageID: page.Id, WorkspaceID: page.GetString("workspace"),
		})
	}
	return markSearchDirtyTargets(context.Background(), app, targets)
}

func markSearchDirtyAuthority(ctx context.Context, app core.App, pageID, workspaceID string) error {
	return markSearchDirtyTargets(ctx, app, []searchDirtyTarget{{
		PageID: pageID, WorkspaceID: workspaceID,
	}})
}

func markSearchDirtyTargets(ctx context.Context, app core.App, targets []searchDirtyTarget) error {
	if len(targets) == 0 {
		return nil
	}
	for _, target := range targets {
		if target.PageID == "" || target.WorkspaceID == "" {
			return errors.New(errorCodeWorkspaceSearchProjectionFailed)
		}
	}
	encoded, err := json.Marshal(targets)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_search_dirty}} ([[page_id]], [[workspace_id]], [[version]], [[enqueued_at]])
		SELECT json_extract([[value]], '$.pageId'),
			json_extract([[value]], '$.workspaceId'), 1, {:now}
		FROM json_each({:targets}) WHERE TRUE
		ON CONFLICT ([[page_id]]) DO UPDATE SET
			[[workspace_id]] = excluded.[[workspace_id]],
			[[version]] = [[leafen_search_dirty]].[[version]] + 1,
			[[enqueued_at]] = excluded.[[enqueued_at]]
	`).Bind(dbx.Params{
		"targets": string(encoded), "now": time.Now().UnixMilli(),
	}).WithContext(ctx).Execute()
	if err != nil {
		return err
	}
	wakeWorkspaceSearchIndexer(app)
	// Content that changes for search may change reminders too.
	pageIDs := make([]string, 0, len(targets))
	for _, target := range targets {
		pageIDs = append(pageIDs, target.PageID)
	}
	return markReminderPagesDirty(ctx, app, pageIDs, time.Now().UTC())
}

func markSearchDirtyByPageID(app core.App, pageID string) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return markSearchDirty(app, page)
}

func markSearchDirtyByPageIDContext(ctx context.Context, app core.App, pageID string) error {
	var page struct {
		WorkspaceID string `db:"workspace_id"`
	}
	err := app.DB().NewQuery(`
		SELECT [[workspace]] AS [[workspace_id]]
		FROM {{leafen_pages}} WHERE [[id]] = {:pageId}
	`).WithContext(ctx).Bind(dbx.Params{"pageId": pageID}).One(&page)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if page.WorkspaceID == "" {
		return errors.New(errorCodeWorkspaceSearchProjectionFailed)
	}
	return markSearchDirtyAuthority(ctx, app, pageID, page.WorkspaceID)
}
