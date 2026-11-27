package app

import (
	"encoding/json"
	"slices"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A linked database is a view of a database, not a database of the page's
// own: reverting the change that took one away brings its block back with
// the views it owned then, its tabs with their filters and sorts, and
// reverting the change that made one takes its block away, its views going
// with it, as the block's views live and go with it
// (page_history_linked_views_back.go). It was kept as a database itself:
// a revert of an agent's removal of a linked view reverted nothing
// (2026-11-25). A database the page holds, and a linked one whose database
// can't be shown any more (databaseBlockCanReturn), stay as they are and are
// named, as before; so does a data source the reverted changes changed beyond
// the views of the linked databases they made or took away.

// pageHistoryLinkedBlock says whether a block, as a state of the page holds
// it, is a linked database's.
func pageHistoryLinkedBlock(block pageHistoryRevertBlock) bool {
	if block.Type != graphBlockTypeInlineDatabase {
		return false
	}
	content, err := parseDatabaseContainerBlockContent(block.Content)
	return err == nil && content.LinkedDatabaseSource
}

// pageHistoryLinkedBlocksChanged are the linked databases' blocks a replayed
// event made or took away.
func pageHistoryLinkedBlocksChanged(changes []pageHistoryRevertChange) map[string]bool {
	linked := map[string]bool{}
	for _, change := range changes {
		if change.before.Present != change.after.Present &&
			(pageHistoryLinkedBlock(change.before) || pageHistoryLinkedBlock(change.after)) {
			linked[change.id] = true
		}
	}
	return linked
}

// pageHistoryPriorDatabases keeps, before an event is replayed, the states of
// the data sources it changes, which the replay replaces
// (applyDatabaseChanges), each nil where the page didn't hold it; with no
// changes named, all the page's.
func pageHistoryPriorDatabases(
	state pageHistoryMaterializedState, changes pageHistoryDatabaseChanges,
) map[string]*pageHistoryDatabaseState {
	prior := map[string]*pageHistoryDatabaseState{}
	keep := func(sourceID string) {
		if database, held := state.Databases[sourceID]; held {
			prior[sourceID] = &database
		} else {
			prior[sourceID] = nil
		}
	}
	if changes == nil {
		for sourceID := range state.Databases {
			keep(sourceID)
		}
	}
	for sourceID := range changes {
		keep(sourceID)
	}
	return prior
}

// pageHistoryViewsOwnedBy are the views a linked database's block owned
// before an event: of the data sources the event changed as they were before
// it, of the others as they are.
func pageHistoryViewsOwnedBy(
	blockID string, databases map[string]pageHistoryDatabaseState, prior map[string]*pageHistoryDatabaseState,
) []graphDatabaseView {
	views := []graphDatabaseView{}
	add := func(sourceID string, state pageHistoryDatabaseState) {
		for _, view := range state.Views {
			if view.ContainerID == blockID {
				views = append(views, graphDatabaseView{
					ID: view.ID, DataSourceID: sourceID, ContainerID: blockID, Type: view.Type, Name: view.Name,
					Position: view.Position, Config: append(json.RawMessage(nil), view.Config...),
				})
			}
		}
	}
	for sourceID, state := range databases {
		if _, changed := prior[sourceID]; !changed {
			add(sourceID, state)
		}
	}
	for sourceID, state := range prior {
		if state != nil {
			add(sourceID, *state)
		}
	}
	sort.Slice(views, func(left, right int) bool {
		if views[left].Position != views[right].Position {
			return views[left].Position < views[right].Position
		}
		return views[left].ID < views[right].ID
	})
	return views
}

// pageHistoryOnlyLinkedViewsChanged is whether a data source changed from
// before to after (nil where the page didn't hold it) only in the views of
// the given linked databases' blocks.
func pageHistoryOnlyLinkedViewsChanged(before, after *pageHistoryDatabaseState, blocks map[string]bool) bool {
	rest := func(state *pageHistoryDatabaseState) string {
		if state == nil {
			return "{}"
		}
		kept := *state
		kept.Views = nil
		for _, view := range state.Views {
			if !blocks[view.ContainerID] {
				kept.Views = append(kept.Views, view)
			}
		}
		encoded, _ := json.Marshal(kept)
		return string(encoded)
	}
	return rest(before) == rest(after)
}

// planPageHistoryRevertLinkedBlocks settles which linked databases' blocks
// the plan brings back or takes away as blocks: each it takes away, and each
// it brings back whose database can still be shown. The views each brings
// back are those it owned that no page holds now.
func planPageHistoryRevertLinkedBlocks(app core.App, pageID string, plan *pageHistoryRevertPlan) error {
	plan.Linked = map[string]bool{}
	var page struct {
		Owner       string `db:"owner"`
		WorkspaceID string `db:"workspace"`
	}
	for id, target := range plan.Blocks {
		now := plan.Current[id]
		if target.Present == now.Present || !pageHistoryLinkedBlock(target) && !pageHistoryLinkedBlock(now) {
			continue
		}
		if target.Present {
			if page.WorkspaceID == "" {
				if err := app.DB().NewQuery(`
					SELECT [[workspace]].[[owner]], [[page]].[[workspace]] FROM {{leafen_pages}} AS [[page]]
					JOIN {{leafen_workspaces}} AS [[workspace]] ON [[workspace]].[[id]] = [[page]].[[workspace]]
					WHERE [[page]].[[id]] = {:pageId}
				`).Bind(dbx.Params{"pageId": pageID}).One(&page); err != nil {
					return err
				}
			}
			content, err := parseDatabaseContainerBlockContent(target.Content)
			if err != nil {
				continue
			}
			if stands, err := databaseBlockCanReturn(app, page.Owner, page.WorkspaceID, pageID, id, content); err != nil {
				return err
			} else if !stands {
				continue
			}
			if err := keepUnheldPageHistoryViews(app, plan, id); err != nil {
				return err
			}
		}
		plan.Linked[id] = true
	}
	return nil
}

// keepUnheldPageHistoryViews leaves of a block's views to bring back those no
// page holds now.
func keepUnheldPageHistoryViews(app core.App, plan *pageHistoryRevertPlan, blockID string) error {
	views := plan.Views[blockID]
	if len(views) == 0 {
		return nil
	}
	ids := make([]string, 0, len(views))
	for _, view := range views {
		ids = append(ids, view.ID)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	held := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_views}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&held); err != nil {
		return err
	}
	kept := views[:0:0]
	for _, view := range views {
		if !slices.Contains(held, view.ID) {
			kept = append(kept, view)
		}
	}
	plan.Views[blockID] = kept
	return nil
}

// pageHistoryRevertLinkedViews are the views the linked databases' blocks
// the operations bring back make again, after the blocks.
func pageHistoryRevertLinkedViews(plan *pageHistoryRevertPlan, operations []graphOperation) []graphOperation {
	views := []graphOperation{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || !plan.Linked[operation.Block.ID] ||
			plan.Current[operation.Block.ID].Present {
			continue
		}
		for index := range plan.Views[operation.Block.ID] {
			view := plan.Views[operation.Block.ID][index]
			views = append(views, graphOperation{Kind: graphOperationKindUpsertView, View: &view})
		}
	}
	return views
}
