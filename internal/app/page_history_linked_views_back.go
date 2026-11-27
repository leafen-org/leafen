package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A linked database's views live and go with its block: they go when it
// leaves the page (deleteOrphanedLinkedDatabaseViews), and what brings the
// block back brings them back with it — the editor's ⌘Z and ⌘⇧Z
// (editorHistoryModel.ts), the trash's restore (settleReturnedBlockTrash), a
// revert of the change that took it away (page_history_revert_linked_views.go)
// and a version of the page restored with its databases. A version restored
// without them leaves the page's databases as they are, the views of those
// standing on the page and a linked database the page doesn't hold now away
// (page_history_restore_later.go); with them, a linked database it brings
// back, none of whose own views is left, takes the views it had then, its
// tabs with their filters and sorts. Without them it came back reading that
// its database couldn't be used. Notion's restore brings it back as an empty
// table cut off from its data source (seen 2026-11-25 twice), a broken
// result Leafen doesn't follow (DIV).
//
// A restored version writes the page's blocks itself, not through the
// content graph, so it settles their databases' rows as the graph does
// (database_block_return.go): a database block it takes away leaves as the
// graph's removal takes it (an own database to the trash, a linked one's row
// gone), and one it brings back takes its row again. It wrote the blocks
// alone: a linked database brought back had no row — not among favorites, no
// public ID — and one the restore took away kept its row, a database left on
// no page and in no trash (2026-11-25).

// takeAwayPageHistoryDatabaseBlocks reads the page's database blocks, by ID
// with their content, before a restore rewrites its blocks, and takes away
// the databases of those the version's blocks don't hold, as the content
// graph's removal does (deleteDatabaseContainerBlocks).
func takeAwayPageHistoryDatabaseBlocks(
	app core.App, actorID, pageID string, blocks []pageHistoryBlockWire,
) (map[string]string, error) {
	rows := []struct {
		ID      string `db:"id"`
		Content string `db:"content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[content]] FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId} AND [[type]]={:type} ORDER BY [[id]]
	`).Bind(dbx.Params{"pageId": pageID, "type": graphBlockTypeInlineDatabase}).All(&rows); err != nil {
		return nil, err
	}
	held := map[string]struct{}{}
	for _, block := range blocks {
		if block.Type == graphBlockTypeInlineDatabase {
			held[block.ID] = struct{}{}
		}
	}
	now := make(map[string]string, len(rows))
	gone := []string{}
	for _, row := range rows {
		now[row.ID] = row.Content
		if _, kept := held[row.ID]; !kept {
			gone = append(gone, row.ID)
		}
	}
	return now, deleteDatabaseContainerBlocks(app, gone, actorID)
}

// bringBackPageHistoryDatabaseBlocks writes the database row of each
// database block the version brings back, or holds otherwise than the page
// did, as the content graph writes it (projectDatabaseBlock); one that can't
// stand as its database stays a block alone (databaseBlockCanReturn).
func bringBackPageHistoryDatabaseBlocks(
	app core.App, authorization pageAuthorization, now map[string]string, blocks []pageHistoryBlockWire,
) error {
	owner, workspaceID, pageID := authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID, authorization.Page.Id
	for _, block := range blocks {
		if block.Type != graphBlockTypeInlineDatabase {
			continue
		}
		if content, found := now[block.ID]; found && pageHistorySameJSON(content, string(block.Content)) {
			continue
		}
		content, err := parseDatabaseContainerBlockContent(block.Content)
		if err != nil {
			continue
		}
		if stands, err := databaseBlockCanReturn(app, owner, workspaceID, pageID, block.ID, content); err != nil {
			return err
		} else if !stands {
			continue
		}
		if err := projectDatabaseBlock(
			app, owner, workspaceID, pageID, block.ID, block.Type, block.ParentID, block.Position, block.Content,
		); err != nil {
			return err
		}
	}
	return nil
}

// returnPageHistoryLinkedViews makes again, after a version's blocks are back,
// the views the version's linked databases had that stand on the page again
// with no view of their own, unless the page holds a view of that ID.
func returnPageHistoryLinkedViews(
	app core.App, authorization pageAuthorization, actorID string,
	structural *crdt.Doc, target pageHistoryMaterializedState,
) error {
	metadata, err := structuralMetadata(structural)
	if err != nil {
		return err
	}
	kept := map[string][]graphOperation{}
	for sourceID, state := range target.Databases {
		for _, view := range state.Views {
			if entry, live := metadata[view.ContainerID]; view.ContainerID == "" || !live ||
				entry.Type != graphBlockTypeInlineDatabase {
				continue
			}
			kept[view.ContainerID] = append(kept[view.ContainerID], graphOperation{
				Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
					ID: view.ID, DataSourceID: sourceID, ContainerID: view.ContainerID, Type: view.Type,
					Name: view.Name, Position: view.Position, Config: append(json.RawMessage(nil), view.Config...),
				},
			})
		}
	}
	if len(kept) == 0 {
		return nil
	}
	containers, viewIDs := make([]string, 0, len(kept)), []string{}
	for containerID, operations := range kept {
		containers = append(containers, containerID)
		for _, operation := range operations {
			viewIDs = append(viewIDs, operation.View.ID)
		}
	}
	encodedContainers, err := json.Marshal(containers)
	if err != nil {
		return err
	}
	encodedViews, err := json.Marshal(viewIDs)
	if err != nil {
		return err
	}
	held := []struct {
		ID          string `db:"id"`
		ContainerID string `db:"container_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[container_id]] FROM {{leafen_database_views}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:views}))
			OR ([[page_id]] = {:pageId} AND [[container_id]] IN (SELECT [[value]] FROM json_each({:containers})))
	`).Bind(dbx.Params{
		"views": string(encodedViews), "containers": string(encodedContainers), "pageId": authorization.Page.Id,
	}).All(&held); err != nil {
		return err
	}
	taken := map[string]struct{}{}
	for _, view := range held {
		delete(kept, view.ContainerID)
		taken[view.ID] = struct{}{}
	}
	views := []graphOperation{}
	for _, operations := range kept {
		for _, operation := range operations {
			if _, found := taken[operation.View.ID]; !found {
				views = append(views, operation)
			}
		}
	}
	if len(views) == 0 {
		return nil
	}
	sort.Slice(views, func(left, right int) bool {
		a, b := views[left].View, views[right].View
		if a.ContainerID != b.ContainerID {
			return a.ContainerID < b.ContainerID
		}
		if a.Position != b.Position {
			return a.Position < b.Position
		}
		return a.ID < b.ID
	})
	return applyDatabaseGraphMutation(
		app, structural, authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID,
		authorization.Page.Id, actorID, views,
	)
}
