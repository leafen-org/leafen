package app

// A database page's trashed blocks were kept in their own trash before
// database page bodies became page bodies. Older workspace archives still
// carry such entries; an import reads them in this form and keeps them as
// page block trash on the row page.

import "encoding/json"

type databaseRowBlockTrashBlock struct {
	ID           string `json:"id"`
	ParentID     string `json:"parentId"`
	Position     string `json:"position"`
	Type         string `json:"type"`
	Content      string `json:"content"`
	CreatedBy    string `json:"createdBy"`
	LastEditedBy string `json:"lastEditedBy"`
	Created      int64  `json:"created"`
	Updated      int64  `json:"updated"`
}

type storedDatabaseRowBlockTrash struct {
	RootBlockID     string `db:"root_block_id"`
	OwnerID         string `db:"owner_id"`
	WorkspaceID     string `db:"workspace_id"`
	ContainerPageID string `db:"container_page_id"`
	RowID           string `db:"row_id"`
	ParentID        string `db:"parent_id"`
	PredecessorID   string `db:"predecessor_id"`
	SuccessorID     string `db:"successor_id"`
	SnapshotJSON    string `db:"snapshot_json"`
	BlockCount      int    `db:"block_count"`
	ByteSize        int    `db:"byte_size"`
	DeletedBy       string `db:"deleted_by"`
	DeletedAt       int64  `db:"deleted_at"`
	ExpiresAt       int64  `db:"expires_at"`
}

type databaseRowBlockTrashWire struct {
	Format int                          `json:"format"`
	RootID string                       `json:"rootId"`
	Blocks []databaseRowBlockTrashBlock `json:"blocks"`
}

func parseDatabaseRowBlockTrashSnapshot(
	row storedDatabaseRowBlockTrash,
) (databaseRowBlockTrashWire, error) {
	snapshot := databaseRowBlockTrashWire{}
	if json.Unmarshal([]byte(row.SnapshotJSON), &snapshot) != nil || snapshot.Format != 1 ||
		snapshot.RootID != row.RootBlockID || len(snapshot.Blocks) != row.BlockCount ||
		row.ByteSize != len([]byte(row.SnapshotJSON)) || len(snapshot.Blocks) == 0 ||
		snapshot.Blocks[0].ID != snapshot.RootID {
		return databaseRowBlockTrashWire{}, errDatabaseRowPageConflict
	}
	seen := map[string]struct{}{}
	for index, block := range snapshot.Blocks {
		if block.ID == "" || block.Type == "" || block.Content == "" || block.Created < 1 || block.Updated < 1 {
			return databaseRowBlockTrashWire{}, errDatabaseRowPageConflict
		}
		if _, duplicate := seen[block.ID]; duplicate {
			return databaseRowBlockTrashWire{}, errDatabaseRowPageConflict
		}
		if index == 0 {
			if block.ParentID != row.ParentID {
				return databaseRowBlockTrashWire{}, errDatabaseRowPageConflict
			}
		} else if _, parent := seen[block.ParentID]; !parent {
			return databaseRowBlockTrashWire{}, errDatabaseRowPageConflict
		}
		content := map[string]any{}
		if json.Unmarshal([]byte(block.Content), &content) != nil || content == nil {
			return databaseRowBlockTrashWire{}, errDatabaseRowPageConflict
		}
		seen[block.ID] = struct{}{}
	}
	return snapshot, nil
}
