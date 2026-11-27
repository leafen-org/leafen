package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// moveRowBlockTrashToPageTrash writes a row block trash entry as page block
// trash on its database page and returns the page snapshot it wrote.
func moveRowBlockTrashToPageTrash(app core.App, row storedDatabaseRowBlockTrash) (recoverableBlockTrashSnapshot, error) {
	stored, err := parseDatabaseRowBlockTrashSnapshot(row)
	if err != nil {
		return recoverableBlockTrashSnapshot{}, err
	}
	// Siblings keep their order through each block's previous sibling.
	order := make([]databaseRowBlockTrashBlock, len(stored.Blocks))
	copy(order, stored.Blocks)
	sort.SliceStable(order, func(left, right int) bool {
		if order[left].ParentID != order[right].ParentID {
			return order[left].ParentID < order[right].ParentID
		}
		if order[left].Position != order[right].Position {
			return order[left].Position < order[right].Position
		}
		return order[left].ID < order[right].ID
	})
	previous := map[string]string{}
	lastInParent := map[string]string{}
	for _, block := range order {
		previous[block.ID] = lastInParent[block.ParentID]
		lastInParent[block.ParentID] = block.ID
	}
	snapshot := recoverableBlockTrashSnapshot{Format: 1, RootID: stored.RootID}
	for index, block := range stored.Blocks {
		content := map[string]any{}
		if err := json.Unmarshal([]byte(block.Content), &content); err != nil {
			return recoverableBlockTrashSnapshot{}, err
		}
		spans := databaseRowBlockTextSpans(content)
		for _, key := range rowBodyStoredTextKeys {
			delete(content, key)
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return recoverableBlockTrashSnapshot{}, err
		}
		afterID := previous[block.ID]
		if index == 0 {
			afterID = ""
		}
		snapshot.Blocks = append(snapshot.Blocks, recoverableBlockTrashBlock{
			ID: block.ID, ParentID: block.ParentID, AfterID: afterID, Position: block.Position,
			Type: block.Type, Content: encoded, Text: notionGraphTextSpansDelta(spans),
		})
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return recoverableBlockTrashSnapshot{}, err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_block_trash_tombstones}}
			([[root_block_id]],[[owner_id]],[[workspace_id]],[[page_id]],[[parent_id]],[[predecessor_id]],
			 [[successor_id]],[[snapshot_json]],[[block_count]],[[byte_size]],[[deleted_by]],[[deleted_at]],[[expires_at]])
		VALUES ({:root},{:owner},{:workspaceId},{:pageId},{:parentId},{:predecessorId},
			{:successorId},{:snapshot},{:count},{:bytes},{:deletedBy},{:deletedAt},{:expiresAt})
	`).Bind(dbx.Params{
		"root": row.RootBlockID, "owner": row.OwnerID, "workspaceId": row.WorkspaceID, "pageId": row.RowID,
		"parentId": row.ParentID, "predecessorId": row.PredecessorID, "successorId": row.SuccessorID,
		"snapshot": string(encoded), "count": len(snapshot.Blocks), "bytes": len(encoded),
		"deletedBy": row.DeletedBy, "deletedAt": row.DeletedAt, "expiresAt": row.ExpiresAt,
	}).Execute()
	return snapshot, err
}
