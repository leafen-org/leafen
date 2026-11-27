package app

import (
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion has no empty column: a column whose last block goes is gone, and a
// list left with one column is no list, its blocks standing where it stood.
// The editor keeps that rule (columnLayoutModel.ts tidyColumns), and an
// import that leaves a column's content out keeps it too, when it shows the
// page (2026-11-16: a column holding only a synced copy whose original
// didn't come, a sub-page or database the plan left out, or a block Leafen
// doesn't take stood empty after the import).

// tidyNotionImportColumns removes, on one imported page, the columns the
// import left empty, and lifts the blocks of a list's last column out of it
// in place, as actorID's change.
func tidyNotionImportColumns(app core.App, job storedJob, payload notionImportJobPayload, pageID string) error {
	operations, err := notionImportColumnTidying(app, pageID)
	if err != nil || len(operations) == 0 {
		return err
	}
	batches := map[string]*structuralMutationBatch{}
	batch, err := loadStructuralMutationBatch(app, pageID, batches)
	if err != nil {
		return err
	}
	if err := applyNotionGraphOperations(
		app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, operations,
	); err != nil {
		return err
	}
	return saveStructuralMutationBatches(app, job.Owner, batches)
}

// notionImportColumnTidying is what tidyColumns does to a page's column
// lists with an empty column: the empty columns go; with one column left its
// blocks move after the list, in order, and the list goes.
func notionImportColumnTidying(app core.App, pageID string) ([]graphOperation, error) {
	blocks := []storedNotionBlock{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]],[[position]],[[type]] FROM {{leafen_blocks}}
		WHERE [[page_id]]={:pageId} AND (
			[[type]]='column_list'
			OR [[parent_id]] IN (SELECT [[id]] FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId} AND [[type]] IN ('column_list','column')))
	`).Bind(dbx.Params{"pageId": pageID}).All(&blocks); err != nil {
		return nil, err
	}
	children := map[string][]storedNotionBlock{}
	lists := []storedNotionBlock{}
	for _, block := range blocks {
		if block.Type == graphBlockTypeColumnList {
			lists = append(lists, block)
		}
		if block.ParentID != "" {
			children[block.ParentID] = append(children[block.ParentID], block)
		}
	}
	for _, siblings := range children {
		sort.Slice(siblings, func(i, j int) bool {
			if siblings[i].Position != siblings[j].Position {
				return siblings[i].Position < siblings[j].Position
			}
			return siblings[i].ID < siblings[j].ID
		})
	}
	sort.Slice(lists, func(i, j int) bool { return lists[i].ID < lists[j].ID })
	operations := []graphOperation{}
	remove := func(id string) {
		operations = append(operations, graphOperation{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: id}})
	}
	for _, list := range lists {
		columns, empty := []storedNotionBlock{}, 0
		for _, column := range children[list.ID] {
			if column.Type != graphBlockTypeColumn {
				continue
			}
			if len(children[column.ID]) == 0 {
				empty++
				remove(column.ID)
				continue
			}
			columns = append(columns, column)
		}
		if empty == 0 || len(columns) > 1 {
			continue
		}
		after := list.ID
		for _, column := range columns {
			for _, child := range children[column.ID] {
				anchor := after
				move := &graphBlockMove{BlockID: child.ID, AfterID: &anchor}
				if list.ParentID != "" {
					parent := list.ParentID
					move.ParentID = &parent
				}
				operations = append(operations, graphOperation{Kind: graphOperationKindMoveBlock, Move: move})
				after = child.ID
			}
		}
		remove(list.ID)
	}
	return operations, nil
}
