package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Which blocks changed since a page's index entry was made, so the indexer
// reads those alone (workspace_search_memo.go). A page's waiting row holds the
// blocks its changes named and the version the last of them left it at: the
// blocks are all that changed only while that is the row's version — any
// other change counts the version on without them, and from then the whole
// page is made again. A save names the blocks whose text it changed; a graph
// write the blocks it made, moved, deleted or set the text of, with
// searchOrderChanged when the page's order may have changed
// (workspace_search_memo_order.go).

// workspaceSearchDirtyBlocksMost is how many blocks a page waits with; with
// more, its whole page is made again.
const workspaceSearchDirtyBlocksMost = 64

// searchOrderChanged among a page's waiting blocks says the page's order may
// have changed too: a block made, moved or deleted. No block has this ID.
const searchOrderChanged = "#order"

// changedBlocks is the blocks that alone changed since the page was last
// made — none when only the page's own fields did — and whether its order
// may have changed, or false when it is to be made whole.
func (dirty storedSearchDirty) changedBlocks() ([]string, bool, bool) {
	if dirty.BlocksVersion != dirty.Version || dirty.Blocks == "" {
		return nil, false, false
	}
	named := []string{}
	if err := json.Unmarshal([]byte(dirty.Blocks), &named); err != nil || len(named) > workspaceSearchDirtyBlocksMost {
		return nil, false, false
	}
	blocks, reordered := make([]string, 0, len(named)), false
	for _, id := range named {
		if id == searchOrderChanged {
			reordered = true
		} else {
			blocks = append(blocks, id)
		}
	}
	return blocks, reordered, true
}

// markSearchPageFieldsDirty marks a page waiting for its own fields alone —
// its title and lead text, which making a kept page again always reads —
// keeping the blocks it already waits for.
func markSearchPageFieldsDirty(ctx context.Context, app core.App, page *core.Record) error {
	if page == nil || page.Id == "" || page.GetString("workspace") == "" {
		return errors.New(errorCodeWorkspaceSearchProjectionFailed)
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_search_dirty}} ([[page_id]], [[workspace_id]], [[version]], [[enqueued_at]], [[blocks]], [[blocks_version]])
		VALUES ({:pageId}, {:workspaceId}, 1, {:now}, '[]', 1)
		ON CONFLICT ([[page_id]]) DO UPDATE SET
			[[workspace_id]] = excluded.[[workspace_id]],
			[[version]] = [[leafen_search_dirty]].[[version]] + 1,
			[[enqueued_at]] = excluded.[[enqueued_at]],
			[[blocks_version]] = CASE WHEN [[leafen_search_dirty]].[[blocks_version]] = [[leafen_search_dirty]].[[version]]
				THEN [[leafen_search_dirty]].[[version]] + 1 ELSE 0 END
	`).WithContext(ctx).Bind(dbx.Params{
		"pageId": page.Id, "workspaceId": page.GetString("workspace"), "now": time.Now().UnixMilli(),
	}).Execute(); err != nil {
		return err
	}
	wakeWorkspaceSearchIndexer(app)
	return markReminderPagesDirty(ctx, app, []string{page.Id}, time.Now().UTC())
}

// graphWriteSearchBlocks is what a graph write names for search: the blocks
// it made, moved, deleted or set the text of, and searchOrderChanged when it
// made, moved or deleted one; false when its caller didn't say the write
// changes the page's text through those alone (SearchFromNamedBlocks), or it
// named none, and the whole page is made.
func graphWriteSearchBlocks(input graphMutationInput, hooks contentGraphMutationHooks) ([]string, bool) {
	if !hooks.SearchFromNamedBlocks || hooks.ApplyAdditionalText != nil || hooks.WorkspaceImportReplaceJobID != "" ||
		hooks.WorkspaceImportReplaceRollback {
		return nil, false
	}
	named := map[string]struct{}{}
	reordered := false
	for _, operation := range input.Operations {
		switch {
		case operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil:
			named[operation.Block.ID], reordered = struct{}{}, true
		case operation.Kind == graphOperationKindMoveBlock && operation.Move != nil:
			named[operation.Move.BlockID], reordered = struct{}{}, true
		case operation.Kind == graphOperationKindDeleteBlock && operation.Deletion != nil:
			named[operation.Deletion.BlockID], reordered = struct{}{}, true
		}
	}
	for _, update := range input.TextUpdates {
		named[update.BlockID] = struct{}{}
	}
	for target := range hooks.TextSourceCopies {
		named[target] = struct{}{}
	}
	for id := range hooks.LiftedTextBlockIDs {
		named[id] = struct{}{}
	}
	if len(named) == 0 {
		return nil, false
	}
	blocks := make([]string, 0, len(named)+1)
	for id := range named {
		if id == "" {
			return nil, false
		}
		blocks = append(blocks, id)
	}
	sort.Strings(blocks)
	if reordered {
		blocks = append(blocks, searchOrderChanged)
	}
	return blocks, true
}

// markSearchBlocksDirtyContext marks a page waiting for its blocks' text
// alone; with none or too many named, the whole page waits.
func markSearchBlocksDirtyContext(ctx context.Context, app core.App, pageID string, blockIDs []string) error {
	if len(blockIDs) == 0 || len(blockIDs) > workspaceSearchDirtyBlocksMost {
		return markSearchDirtyByPageIDContext(ctx, app, pageID)
	}
	var page struct {
		WorkspaceID string `db:"workspace_id"`
	}
	err := app.DB().NewQuery(`SELECT [[workspace]] AS [[workspace_id]] FROM {{leafen_pages}} WHERE [[id]] = {:pageId}`).
		WithContext(ctx).Bind(dbx.Params{"pageId": pageID}).One(&page)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if page.WorkspaceID == "" {
		return errors.New(errorCodeWorkspaceSearchProjectionFailed)
	}
	encoded, err := json.Marshal(blockIDs)
	if err != nil {
		return err
	}
	// Set from the row as it was: its blocks go on only while its version
	// was theirs, and while they stay few.
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_search_dirty}} ([[page_id]], [[workspace_id]], [[version]], [[enqueued_at]], [[blocks]], [[blocks_version]])
		VALUES ({:pageId}, {:workspaceId}, 1, {:now}, {:blocks}, 1)
		ON CONFLICT ([[page_id]]) DO UPDATE SET
			[[workspace_id]] = excluded.[[workspace_id]],
			[[version]] = [[leafen_search_dirty]].[[version]] + 1,
			[[enqueued_at]] = excluded.[[enqueued_at]],
			[[blocks]] = CASE WHEN [[leafen_search_dirty]].[[blocks_version]] = [[leafen_search_dirty]].[[version]] THEN (
				SELECT json_group_array([[value]]) FROM (
					SELECT [[value]] FROM json_each([[leafen_search_dirty]].[[blocks]])
					UNION SELECT [[value]] FROM json_each(excluded.[[blocks]]))
			) ELSE '[]' END,
			[[blocks_version]] = CASE WHEN [[leafen_search_dirty]].[[blocks_version]] = [[leafen_search_dirty]].[[version]] AND (
				SELECT COUNT(*) FROM (
					SELECT [[value]] FROM json_each([[leafen_search_dirty]].[[blocks]])
					UNION SELECT [[value]] FROM json_each(excluded.[[blocks]]))
			) <= {:most} THEN [[leafen_search_dirty]].[[version]] + 1 ELSE 0 END
	`).WithContext(ctx).Bind(dbx.Params{
		"pageId": pageID, "workspaceId": page.WorkspaceID, "now": time.Now().UnixMilli(),
		"blocks": string(encoded), "most": workspaceSearchDirtyBlocksMost,
	}).Execute(); err != nil {
		return err
	}
	wakeWorkspaceSearchIndexer(app)
	return markReminderPagesDirty(ctx, app, []string{pageID}, time.Now().UTC())
}
