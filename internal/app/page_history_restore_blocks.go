package app

import (
	"database/sql"
	"errors"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// returnMovedPageHistorySubPages moves back under the page the sub-pages a
// version holds that have since moved to another page, as Notion's restore
// does (seen 2026-09-30): each leaves the page it went to, and the restore
// puts its block where the version had it. A full-page database comes back
// only when the restore takes the page's databases back too. One in the
// trash, in another workspace, in a locked page, or that the reader can't
// move stays where it is.
func returnMovedPageHistorySubPages(
	app core.App, actorID, workspaceID, pageID, requestID string, target pageHistoryMaterializedState,
	databases bool,
) error {
	metadata, err := structuralMetadata(target.Structural)
	if err != nil {
		return err
	}
	for id, entry := range metadata {
		if !isChildPageBlock(id, entry) {
			continue
		}
		page, err := app.FindRecordById(pagesCollection, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if page.GetBool("in_trash") || page.GetString("parent_id") == pageID || page.GetString("workspace") != workspaceID ||
			isFullPageDatabase(page) && !databases {
			continue
		}
		err = setChildPageParent(app, actorID, id, pageID, "page-history-restore:"+requestID)
		if err != nil && !errors.Is(err, errChildPageBlockPageChange) {
			return err
		}
	}
	return nil
}

// bringBackPageHistoryBlocks returns to a version the blocks that have since
// gone to another page, as Notion's restore brings moved content back: the
// blocks come back as the version had them, under new IDs, and the moved ones
// stay where they went with what was done to them there. A database or a
// synced block can't be in two places, so it stays where it went, with the
// blocks inside it, as does a sub-page that couldn't move back. It reports
// the inline databases that stay, each with its place among the page's top
// blocks, for a restore that moves them back.
func bringBackPageHistoryBlocks(
	app core.App, pageID string, target pageHistoryMaterializedState,
) ([]blockMoveReturn, error) {
	metadata, err := structuralMetadata(target.Structural)
	if err != nil || len(metadata) == 0 {
		return nil, err
	}
	ids := make([]any, 0, len(metadata))
	for id := range metadata {
		ids = append(ids, id)
	}
	elsewhere := []string{}
	for start := 0; start < len(ids); start += 500 {
		found := []string{}
		if err := app.DB().Select("id").From("leafen_blocks").Where(dbx.And(
			dbx.In("id", ids[start:min(start+500, len(ids))]...), dbx.Not(dbx.HashExp{"page_id": pageID}),
		)).Column(&found); err != nil {
			return nil, err
		}
		elsewhere = append(elsewhere, found...)
	}
	if len(elsewhere) == 0 {
		return nil, nil
	}
	children := map[string][]string{}
	for id, entry := range metadata {
		children[entry.ParentID] = append(children[entry.ParentID], id)
	}
	moved := map[string]struct{}{}
	for queue := elsewhere; len(queue) > 0; queue = queue[1:] {
		if _, seen := moved[queue[0]]; !seen {
			moved[queue[0]] = struct{}{}
			queue = append(queue, children[queue[0]]...)
		}
	}
	staying := map[string]struct{}{}
	var stay func(id string)
	stay = func(id string) {
		if _, done := staying[id]; done {
			return
		}
		staying[id] = struct{}{}
		for _, child := range children[id] {
			stay(child)
		}
	}
	for id := range moved {
		switch metadata[id].Type {
		case graphBlockTypeChildPage, graphBlockTypeInlineDatabase, graphBlockTypeChildDatabase, graphBlockTypeSyncedBlock:
			stay(id)
		}
	}
	renamed := map[string]string{}
	for id := range moved {
		if _, stays := staying[id]; !stays {
			renamed[id] = core.GenerateDefaultRandomId()
		}
	}
	newID := func(id string) string {
		if renamed[id] != "" {
			return renamed[id]
		}
		return id
	}
	// Every parent holding a moved block, and every renamed block, takes its
	// order anew.
	parents := map[string]struct{}{}
	for id := range moved {
		if _, stays := staying[metadata[id].ParentID]; !stays {
			parents[metadata[id].ParentID] = struct{}{}
		}
	}
	for id := range renamed {
		parents[id] = struct{}{}
	}
	orders := map[string][]string{}
	for parentID := range parents {
		order, err := structuralOrder(target.Structural, parentID)
		if err != nil {
			return nil, err
		}
		kept := make([]string, 0, len(order))
		for _, id := range order {
			if _, stays := staying[id]; !stays {
				kept = append(kept, newID(id))
			}
		}
		orders[parentID] = kept
	}
	returns, err := pageHistoryDatabaseReturns(app, target, metadata, moved, staying, newID)
	if err != nil {
		return nil, err
	}
	// A moved block's comments went with it, so the block that comes back
	// has its text without their marks.
	texts := make(map[string][]crdt.Delta, len(renamed))
	for id := range renamed {
		delta := target.Collaboration.GetText(ygoBlockTextRootPrefix + id).ToDelta()
		for index, op := range delta {
			if _, marked := op.Attributes["commentId"]; !marked {
				continue
			}
			attributes := make(crdt.Attributes, len(op.Attributes))
			for key, value := range op.Attributes {
				if key != "commentId" {
					attributes[key] = value
				}
			}
			delta[index].Attributes = attributes
		}
		texts[id] = delta
	}
	target.Structural.Transact(func(transaction *crdt.Transaction) {
		metadataMap := transaction.GetMap(ygoBlockMetadataRoot)
		for id := range staying {
			metadataMap.Delete(transaction, id)
			if order := transaction.GetArray(blockOrderRoot(id)); order.Len() > 0 {
				order.Delete(transaction, 0, order.Len())
			}
		}
		for parentID, order := range orders {
			array := transaction.GetArray(blockOrderRoot(parentID))
			if array.Len() > 0 {
				array.Delete(transaction, 0, array.Len())
			}
			if len(order) > 0 {
				transaction.GetArray(blockOrderRoot(newID(parentID))).Push(transaction, stringSliceAsAny(order))
			}
		}
		for id, to := range renamed {
			entry := metadata[id]
			metadataMap.Delete(transaction, id)
			metadataMap.Set(transaction, to, map[string]any{
				"parentId": newID(entry.ParentID), "type": entry.Type, "content": entry.Content,
			})
		}
	})
	target.Collaboration.Transact(func(transaction *crdt.Transaction) {
		for id, delta := range texts {
			if len(delta) > 0 {
				transaction.GetText(ygoBlockTextRootPrefix+renamed[id]).ApplyDelta(transaction, delta)
			}
		}
	})
	return returns, nil
}

// pageHistoryDatabaseReturns are the inline databases a version holds that
// stay on other pages, in the version's order, each where it is now and with
// the place it takes back: after the nearest top block before it that is on
// the page, or after the top block that held it. One inside a block that
// stays goes with that block.
func pageHistoryDatabaseReturns(
	app core.App, target pageHistoryMaterializedState, metadata map[string]structuralBlockMetadata,
	moved, staying map[string]struct{}, newID func(string) string,
) ([]blockMoveReturn, error) {
	top, err := structuralOrder(target.Structural, "")
	if err != nil {
		return nil, err
	}
	type placed struct {
		place blockMoveReturn
		index int
	}
	found := []placed{}
	for id := range moved {
		entry := metadata[id]
		if entry.Type != graphBlockTypeInlineDatabase {
			continue
		}
		if _, held := staying[entry.ParentID]; held {
			continue
		}
		place := blockMoveReturn{BlockID: id, After: []string{}}
		if err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_blocks}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": id}).Row(&place.SourcePageID); errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return nil, err
		}
		root := id
		for metadata[root].ParentID != "" {
			root = metadata[root].ParentID
		}
		index := structuralIndex(top, root)
		if root != id {
			place.After = append(place.After, newID(root))
		}
		// A database before it that also comes back is there by then.
		for before := index - 1; before >= 0; before-- {
			_, stays := staying[top[before]]
			if !stays || metadata[top[before]].Type == graphBlockTypeInlineDatabase {
				place.After = append(place.After, newID(top[before]))
			}
		}
		found = append(found, placed{place: place, index: index})
	}
	sort.Slice(found, func(left, right int) bool {
		if found[left].index != found[right].index {
			return found[left].index < found[right].index
		}
		return found[left].place.BlockID < found[right].place.BlockID
	})
	returns := make([]blockMoveReturn, len(found))
	for position, item := range found {
		returns[position] = item.place
	}
	return returns, nil
}

// bringBackPageHistorySubPages takes out of the trash the sub-pages whose
// blocks a restored version holds, as Notion's restore brings them back. A
// sub-page this person can't change stays in the trash.
func bringBackPageHistorySubPages(
	app core.App, actorID, pageID, requestID string, target pageHistoryMaterializedState,
) error {
	metadata, err := structuralMetadata(target.Structural)
	if err != nil {
		return err
	}
	for id, entry := range metadata {
		if !isChildPageBlock(id, entry) {
			continue
		}
		page, err := app.FindRecordById(pagesCollection, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if !page.GetBool("in_trash") || page.GetString("parent_id") != pageID {
			continue
		}
		err = setChildPageTrash(app, actorID, id, false, "page-history-restore:"+requestID)
		if err != nil && !errors.Is(err, errChildPageBlockPageChange) {
			return err
		}
	}
	return nil
}

func replacePageHistoryBlockRoots(current, target *crdt.Doc) error {
	currentMetadata, err := structuralMetadata(current)
	if err != nil {
		return err
	}
	targetMetadata, err := structuralMetadata(target)
	if err != nil {
		return err
	}
	parents := map[string]struct{}{"": {}}
	for _, metadata := range currentMetadata {
		parents[metadata.ParentID] = struct{}{}
	}
	for _, metadata := range targetMetadata {
		parents[metadata.ParentID] = struct{}{}
	}
	targetOrders := map[string][]string{}
	for parentID := range parents {
		order, err := structuralOrder(target, parentID)
		if err != nil {
			return err
		}
		targetOrders[parentID] = order
	}
	current.Transact(func(transaction *crdt.Transaction) {
		metadataMap := transaction.GetMap(ygoBlockMetadataRoot)
		for id := range currentMetadata {
			metadataMap.Delete(transaction, id)
		}
		for parentID := range parents {
			order := transaction.GetArray(blockOrderRoot(parentID))
			if order.Len() > 0 {
				order.Delete(transaction, 0, order.Len())
			}
		}
		for id, metadata := range targetMetadata {
			metadataMap.Set(transaction, id, map[string]any{
				"parentId": metadata.ParentID, "type": metadata.Type, "content": metadata.Content,
			})
		}
		for parentID, ids := range targetOrders {
			if len(ids) > 0 {
				transaction.GetArray(blockOrderRoot(parentID)).Push(transaction, stringSliceAsAny(ids))
			}
		}
	})
	return nil
}

func stringSliceAsAny(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}
