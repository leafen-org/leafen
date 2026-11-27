package app

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// pageHistoryLaterBlocks settles the page's databases and what the page holds
// now that the version it goes back to doesn't, as Notion's restore does
// (seen 2026-11-06): a sub-page made since goes to the trash, where it can be
// restored. Without the page's databases (the restore's option left off) the
// databases stay as they are now: one made since stays, after the version's
// blocks, and one the version held that the page doesn't hold now stays away
// — an inline or full-page database since moved to the trash, a linked view
// since removed (seen 2026-11-25 twice, in different tabs: none of them came
// back, nor showed in the restore's preview); before, it came back, a linked
// view with its tabs. With the databases, the version's come back and those made
// since leave, a full-page one to the trash. It returns the sub-pages to put
// in the trash once the page is restored. The page's structure is only read
// here.
func pageHistoryLaterBlocks(
	app core.App, pageID string, target pageHistoryMaterializedState, restoreDatabases bool,
	documents *pageHistoryDocuments,
) ([]string, error) {
	current, _, found, err := documents.read(app, ygoStructuralStream, pageID)
	if err != nil || !found {
		return nil, err
	}
	now, err := structuralMetadata(current)
	if err != nil {
		return nil, err
	}
	then, err := structuralMetadata(target.Structural)
	if err != nil {
		return nil, err
	}
	if !restoreDatabases {
		if err := leavePageHistoryDatabasesAway(app, target, now, then); err != nil {
			return nil, err
		}
	}
	order, err := structuralOrder(current, "")
	if err != nil {
		return nil, err
	}
	// The page's own top blocks in their order first, then any deeper ones.
	ids := append([]string{}, order...)
	listed := map[string]bool{}
	for _, id := range order {
		listed[id] = true
	}
	for id := range now {
		if !listed[id] {
			ids = append(ids, id)
		}
	}
	trash := []string{}
	kept := []string{}
	for _, id := range ids {
		entry := now[id]
		if _, there := then[id]; there {
			continue
		}
		database, err := isPageHistoryDatabaseBlock(app, id, entry)
		if err != nil {
			return nil, err
		}
		switch {
		case database && !restoreDatabases:
			kept = append(kept, id)
		case database:
			// Taken back with the page's databases: it leaves the page, and a
			// full-page database goes to the trash.
			if isChildPageBlock(id, entry) || entry.Type == graphBlockTypeChildDatabase {
				if child, _ := entry.Content["childPageId"].(string); child != "" {
					trash = append(trash, child)
				}
			}
		case isChildPageBlock(id, entry):
			trash = append(trash, id)
		}
	}
	if len(kept) > 0 {
		target.Structural.Transact(func(transaction *crdt.Transaction) {
			metadata := transaction.GetMap(ygoBlockMetadataRoot)
			for _, id := range kept {
				entry := now[id]
				metadata.Set(transaction, id, map[string]any{
					"parentId": "", "type": entry.Type, "content": entry.Content,
				})
			}
			transaction.GetArray(blockOrderRoot("")).Push(transaction, stringSliceAsAny(kept))
		})
	}
	return trash, nil
}

// leavePageHistoryDatabasesAway takes out of the version's blocks the
// databases the page doesn't hold now, so that a restore leaving the
// databases as they are doesn't bring them back. A database moved to another
// page stays there too, as it did (bringBackPageHistoryBlocks).
func leavePageHistoryDatabasesAway(
	app core.App, target pageHistoryMaterializedState, now, then map[string]structuralBlockMetadata,
) error {
	away := map[string][]string{}
	for id, entry := range then {
		if _, there := now[id]; there {
			continue
		}
		database, err := isPageHistoryDatabaseBlock(app, id, entry)
		if err != nil {
			return err
		}
		if database {
			away[entry.ParentID] = append(away[entry.ParentID], id)
		}
	}
	if len(away) == 0 {
		return nil
	}
	orders := map[string][]string{}
	for parentID := range away {
		order, err := structuralOrder(target.Structural, parentID)
		if err != nil {
			return err
		}
		orders[parentID] = order
	}
	target.Structural.Transact(func(transaction *crdt.Transaction) {
		metadata := transaction.GetMap(ygoBlockMetadataRoot)
		for parentID, ids := range away {
			leaving := map[string]bool{}
			for _, id := range ids {
				leaving[id] = true
				metadata.Delete(transaction, id)
			}
			array := transaction.GetArray(blockOrderRoot(parentID))
			// From the end, so each index still stands.
			for index := len(orders[parentID]) - 1; index >= 0; index-- {
				if leaving[orders[parentID][index]] {
					array.Delete(transaction, index, 1)
				}
			}
		}
	})
	return nil
}

// isPageHistoryDatabaseBlock is whether a block is a database: an inline one (a
// linked view too), or a full-page one under the page.
func isPageHistoryDatabaseBlock(app core.App, id string, entry structuralBlockMetadata) (bool, error) {
	if entry.Type == graphBlockTypeInlineDatabase || entry.Type == graphBlockTypeChildDatabase {
		return true, nil
	}
	if entry.Type != graphBlockTypeChildPage {
		return false, nil
	}
	child, _ := entry.Content["childPageId"].(string)
	if child == "" {
		return false, nil
	}
	page, err := app.FindRecordById(pagesCollection, child)
	if err != nil {
		return false, nil
	}
	return isFullPageDatabase(page), nil
}

// trashPageHistoryLaterSubPages puts the sub-pages a restore left out in the
// trash, each as the person restoring would.
func trashPageHistoryLaterSubPages(app core.App, actorID, requestID string, pageIDs []string) error {
	for _, id := range pageIDs {
		page, err := app.FindRecordById(pagesCollection, id)
		if err != nil || page.GetBool("in_trash") {
			continue
		}
		input := normalizePageMetadataInput(pageMetadataInput{
			Title: page.GetString("title"), ParentID: page.GetString("parent_id"), InTrash: true,
			MutationID: "page-history-restore-trash:" + requestID + ":" + id, BaseCursor: page.GetInt("cursor"),
		})
		if _, _, err := applyPageMetadata(app, actorID, id, input, pageMetadataRequestHash(id, input)); err != nil {
			return err
		}
	}
	return nil
}
