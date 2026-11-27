package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A synced copy holds no blocks of its own: it shows its original's
// (notion_synced_copy_children.go), so a block under it is seen nowhere. A
// content change that would leave one there — a block made or moved under a
// copy, or a block holding blocks made a copy — is refused, naming the
// original the blocks go under instead. The public API's append sends them
// to the original itself (notionAppendUnderOriginal), as Notion's API does,
// its answer listing the original's children; a content change names each
// block's parent and the sibling it follows as its writer's own graph holds
// them, and the original may be on another page, which this page's change
// can't write, so the writer is told where they go rather than finding them
// where it didn't put them. The app's editor puts them under the original
// (syncedCopyMove.ts).

// validateStructuralGraphPlaces checks where a change's blocks go: under
// parents and after anchors that are there (validateStructuralGraphReferences)
// and under no synced copy. A change bringing back blocks as they were stored
// (storedBlocks) may hold some a write left under a copy before 2026-11-21;
// they come back and then go where they are seen
// (applyStoredContentGraphMutation).
func validateStructuralGraphPlaces(
	app core.App, owner, pageID string, projection graphProjection, operations []graphOperation, storedBlocks bool,
) error {
	if err := validateStructuralGraphReferences(projection, operations); err != nil {
		return err
	}
	if storedBlocks {
		return nil
	}
	return refuseBlocksUnderSyncedCopies(app, owner, pageID, projection, operations)
}

// refuseBlocksUnderSyncedCopies refuses the blocks a change leaves under a
// synced copy, each block and copy as the change leaves them.
func refuseBlocksUnderSyncedCopies(
	app core.App, owner, pageID string, projection graphProjection, operations []graphOperation,
) error {
	// The blocks the change writes, by the original each shows as a copy
	// ("" for one it leaves no copy), and where it writes them.
	written, writtenAt := map[string]string{}, map[string]int{}
	placed, deleted := map[string]string{}, map[string]bool{}
	for index, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			block := operation.Block
			written[block.ID] = notionSyncedCopySourceID(storedNotionBlock{Type: block.Type, Content: string(block.Content)})
			writtenAt[block.ID] = index
			placed[block.ID] = graphOptionalID(block.ParentID)
		case graphOperationKindMoveBlock:
			placed[operation.Move.BlockID] = graphOptionalID(operation.Move.ParentID)
		case graphOperationKindDeleteBlock:
			deleted[operation.Deletion.BlockID] = true
		}
	}
	stored := []string{}
	for _, parentID := range placed {
		if _, own := written[parentID]; !own && projection.blocks[parentID].Type == graphBlockTypeSyncedBlock {
			stored = append(stored, parentID)
		}
	}
	originals, err := loadStoredSyncedCopyOriginals(app, owner, pageID, stored)
	if err != nil {
		return err
	}
	for index, operation := range operations {
		field, parentID := "block", ""
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			parentID = graphOptionalID(operation.Block.ParentID)
		case graphOperationKindMoveBlock:
			field, parentID = "move", graphOptionalID(operation.Move.ParentID)
		default:
			continue
		}
		original, own := written[parentID]
		if !own {
			original = originals[parentID]
		}
		if parentID != "" && original != "" {
			return graphRefused(errorCodeGraphReferenceInvalid, "A synced copy can't hold blocks.",
				"operations[%d].%s.parentId %q is a synced block copy, which shows its original's blocks: "+
					"put the block under the original, block %q", index, field, parentID, original)
		}
	}
	made := []string{}
	for id, original := range written {
		if original != "" && !deleted[id] {
			made = append(made, id)
		}
	}
	held, err := loadGraphBlockChildrenOf(app, owner, pageID, made)
	if err != nil {
		return err
	}
	for _, child := range held {
		if parentID, moved := placed[child.ID]; deleted[child.ID] || moved && parentID != child.ParentID {
			continue
		}
		return graphRefused(errorCodeGraphReferenceInvalid, "A block holding blocks can't become a synced copy.",
			"operations[%d].block makes %q a copy of synced block %q while block %q is under it, which a copy "+
				"doesn't show: move or delete its blocks first", writtenAt[child.ParentID], child.ParentID,
			written[child.ParentID], child.ID)
	}
	return nil
}

func graphOptionalID(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

// loadStoredSyncedCopyOriginals are the originals the stored synced copies
// among ids on the page show; a block that is no copy isn't among them.
func loadStoredSyncedCopyOriginals(app core.App, owner, pageID string, ids []string) (map[string]string, error) {
	originals := map[string]string{}
	if len(ids) == 0 {
		return originals, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		ID       string `db:"id"`
		Original string `db:"original"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[id]], TRIM(COALESCE(` + syncedBlockOriginalSQL + `, '')) AS [[original]] FROM {{leafen_blocks}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[type]] = 'synced_block'
			AND [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "ids": string(encoded)}).All(&rows)
	for _, row := range rows {
		if row.Original != "" {
			originals[row.ID] = row.Original
		}
	}
	return originals, err
}

type graphBlockChild struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
}

// loadGraphBlockChildrenOf are the stored blocks right under the parents on
// the page, in order.
func loadGraphBlockChildrenOf(app core.App, owner, pageID string, parentIDs []string) ([]graphBlockChild, error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	sort.Strings(parentIDs)
	encoded, err := json.Marshal(parentIDs)
	if err != nil {
		return nil, err
	}
	children := []graphBlockChild{}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]] FROM {{leafen_blocks}} INDEXED BY [[idx_leafen_blocks_page_order]]
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[parent_id]] IN (SELECT [[value]] FROM json_each({:parents}))
		ORDER BY [[parent_id]], [[position]], [[id]]
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "parents": string(encoded)}).All(&children)
	return children, err
}
