package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type workspaceImportStagedBlockTrash struct {
	RootBlockID   string                        `json:"rootBlockId"`
	ParentID      string                        `json:"parentId"`
	PredecessorID string                        `json:"predecessorId"`
	SuccessorID   string                        `json:"successorId"`
	Snapshot      recoverableBlockTrashSnapshot `json:"snapshot"`
	DeletedAt     int64                         `json:"deletedAt"`
	ExpiresAt     int64                         `json:"expiresAt"`
}

func decodeWorkspaceImportArchiveBlockTrash(
	raw map[string]any,
) (archiveBlockTrashValue, []byte, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return archiveBlockTrashValue{}, nil, workspaceImportInputError()
	}
	value := archiveBlockTrashValue{}
	if json.Unmarshal(encoded, &value) != nil ||
		strings.TrimSpace(value.RootBlockID) != value.RootBlockID || value.RootBlockID == "" ||
		len(value.RootBlockID) > maxGraphIdentifierLength ||
		strings.TrimSpace(value.PageID) != value.PageID || value.PageID == "" ||
		len(value.PageID) > maxGraphIdentifierLength ||
		strings.TrimSpace(value.ParentID) != value.ParentID || len(value.ParentID) > maxGraphIdentifierLength ||
		strings.TrimSpace(value.PredecessorID) != value.PredecessorID || len(value.PredecessorID) > maxGraphIdentifierLength ||
		strings.TrimSpace(value.SuccessorID) != value.SuccessorID || len(value.SuccessorID) > maxGraphIdentifierLength ||
		value.DeletedAt < 0 || value.ExpiresAt <= value.DeletedAt ||
		value.BlockCount < 1 ||
		value.Snapshot.Format != 1 || value.Snapshot.RootID != value.RootBlockID ||
		len(value.Snapshot.Blocks) != value.BlockCount {
		return archiveBlockTrashValue{}, nil, workspaceImportInputError()
	}
	snapshotJSON, err := json.Marshal(value.Snapshot)
	if err != nil || len(snapshotJSON) == 0 || len(snapshotJSON) > maxYgoUpdateBytes {
		return archiveBlockTrashValue{}, nil, workspaceImportInputError()
	}
	row := storedRecoverableBlockTrash{
		RootBlockID: value.RootBlockID, PageID: value.PageID, ParentID: value.ParentID,
		PredecessorID: value.PredecessorID, SuccessorID: value.SuccessorID,
		SnapshotJSON: string(snapshotJSON), BlockCount: value.BlockCount, ByteSize: len(snapshotJSON),
		DeletedAt: value.DeletedAt, ExpiresAt: value.ExpiresAt,
	}
	if _, err := parseRecoverableBlockTrashSnapshot(row); err != nil {
		return archiveBlockTrashValue{}, nil, workspaceImportInputError()
	}
	for _, block := range value.Snapshot.Blocks {
		if strings.TrimSpace(block.ID) != block.ID || len(block.ID) > maxGraphIdentifierLength ||
			strings.TrimSpace(block.ParentID) != block.ParentID || len(block.ParentID) > maxGraphIdentifierLength ||
			strings.TrimSpace(block.AfterID) != block.AfterID || len(block.AfterID) > maxGraphIdentifierLength {
			return archiveBlockTrashValue{}, nil, workspaceImportInputError()
		}
	}
	return value, snapshotJSON, nil
}

func workspaceImportArchiveBlockTrashItem(
	db *sql.DB, encoded string,
) (workspaceImportStagedItem, error) {
	raw := map[string]any{}
	if json.Unmarshal([]byte(encoded), &raw) != nil {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	value, _, err := decodeWorkspaceImportArchiveBlockTrash(raw)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	pageID, err := workspaceImportArchiveReference(db, value.PageID, workspaceExportItemPage)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	staged, err := workspaceImportArchiveStagedTrash(db, value, workspaceExportItemBlockTrash)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	return workspaceImportStagedItem{PageID: pageID, BlockTrash: staged}, nil
}

// workspaceImportArchiveStagedTrash maps a trash entry's blocks, whose IDs
// map as blockKind's (a page's trashed blocks, or a database page's), with the
// views its databases own.
func workspaceImportArchiveStagedTrash(
	db *sql.DB, value archiveBlockTrashValue, blockKind string,
) (*workspaceImportStagedBlockTrash, error) {
	rootID, err := workspaceImportArchiveReference(db, value.RootBlockID, blockKind)
	if err != nil {
		return nil, err
	}
	parentID, err := workspaceImportArchiveOptionalLiveOrTrashBlockReference(db, value.ParentID)
	if err != nil {
		return nil, err
	}
	predecessorID, err := workspaceImportArchiveOptionalLiveOrTrashBlockReference(db, value.PredecessorID)
	if err != nil {
		return nil, err
	}
	successorID, err := workspaceImportArchiveOptionalLiveOrTrashBlockReference(db, value.SuccessorID)
	if err != nil {
		return nil, err
	}
	mapped := recoverableBlockTrashSnapshot{Format: value.Snapshot.Format, RootID: rootID}
	mapped.Blocks = make([]recoverableBlockTrashBlock, 0, len(value.Snapshot.Blocks))
	blockIDs := map[string]string{}
	// Views a database owns travel with the entry, including a mixed
	// database's views of the sources it links.
	ownedViewContainers := map[string]string{}
	for _, view := range value.Snapshot.Views {
		ownedViewContainers[view.ID] = view.ContainerID
	}
	for _, source := range value.Snapshot.Blocks {
		id, err := workspaceImportArchiveReference(db, source.ID, blockKind)
		if err != nil {
			return nil, err
		}
		blockIDs[source.ID] = id
		mappedParent := parentID
		if source.ID != value.RootBlockID {
			mappedParent, err = workspaceImportArchiveReference(db, source.ParentID, blockKind)
			if err != nil {
				return nil, err
			}
		}
		mappedAfter := predecessorID
		if source.ID != value.RootBlockID {
			mappedAfter = ""
			if source.AfterID != "" {
				mappedAfter, err = workspaceImportArchiveReference(db, source.AfterID, blockKind)
				if err != nil {
					return nil, err
				}
			}
		}
		sourceContent := source.Content
		selectsOwnedView := false
		if content, _, database := recoverableDatabaseContainer(source); database {
			selectsOwnedView = ownedViewContainers[content.ViewID] == source.ID
		}
		if _, linked := recoverableLinkedDatabaseContainer(source); linked || selectsOwnedView {
			// The selected view travels with the entry and is mapped below.
			if sourceContent, err = workspaceImportArchiveLinkedBlockContent(sourceContent, ""); err != nil {
				return nil, err
			}
		}
		content, err := rewriteWorkspaceImportArchiveBlockTrashJSON(db, sourceContent)
		if err != nil {
			return nil, err
		}
		text, err := rewriteWorkspaceImportArchiveBlockTrashDeltas(db, source.Text)
		if err != nil {
			return nil, err
		}
		mapped.Blocks = append(mapped.Blocks, recoverableBlockTrashBlock{
			ID: id, ParentID: mappedParent, AfterID: mappedAfter, Position: source.Position,
			Type: source.Type, Content: content, Text: text,
		})
	}
	if err := workspaceImportArchiveTrashLinkedViews(db, value.Snapshot, &mapped, blockIDs); err != nil {
		return nil, err
	}
	mapped, err = workspaceImportStructuredTableTrashSnapshot(mapped)
	if err != nil {
		return nil, err
	}
	return &workspaceImportStagedBlockTrash{
		RootBlockID: rootID, ParentID: parentID, PredecessorID: predecessorID,
		SuccessorID: successorID, Snapshot: mapped,
		DeletedAt: value.DeletedAt, ExpiresAt: value.ExpiresAt,
	}, nil
}

func workspaceImportStructuredTableTrashSnapshot(
	snapshot recoverableBlockTrashSnapshot,
) (recoverableBlockTrashSnapshot, error) {
	blocks := make([]recoverableBlockTrashBlock, 0, len(snapshot.Blocks))
	for _, block := range snapshot.Blocks {
		if block.Type != legacySimpleTableBlockType {
			blocks = append(blocks, block)
			continue
		}
		tableContent, rows, err := nativeStructuredTableContents(block.Content)
		if err != nil {
			return recoverableBlockTrashSnapshot{}, workspaceImportInputError()
		}
		encodedTable, err := json.Marshal(tableContent)
		if err != nil {
			return recoverableBlockTrashSnapshot{}, workspaceImportInputError()
		}
		block.Type = graphBlockTypeTable
		block.Content = encodedTable
		blocks = append(blocks, block)
		previousID := ""
		for index, content := range rows {
			encodedRow, err := json.Marshal(content)
			if err != nil {
				return recoverableBlockTrashSnapshot{}, workspaceImportInputError()
			}
			rowID := nativeStructuredTableRowID(block.ID, index)
			blocks = append(blocks, recoverableBlockTrashBlock{
				ID: rowID, ParentID: block.ID, AfterID: previousID,
				Position: fmt.Sprintf("archive-table:%010d", index),
				Type:     graphBlockTypeTableRow, Content: encodedRow,
			})
			previousID = rowID
		}
	}
	snapshot.Blocks = blocks
	return snapshot, nil
}

func appendWorkspaceImportArchiveBlockTrash(
	w *workspaceImportArchiveChunkWriter, db *sql.DB,
) error {
	rows, err := db.Query(`
		SELECT value_json FROM entities WHERE kind = ? ORDER BY page_id, sequence
	`, workspaceExportItemBlockTrash)
	if err != nil {
		return workspaceImportStorageError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		if rows.Scan(&encoded) != nil {
			return workspaceImportInputError()
		}
		item, err := workspaceImportArchiveBlockTrashItem(db, encoded)
		if err != nil {
			return err
		}
		if err := w.append(item); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return workspaceImportStorageError(err)
	}
	return nil
}

func rewriteWorkspaceImportArchiveBlockTrashJSON(
	db *sql.DB, raw json.RawMessage,
) (json.RawMessage, error) {
	value := any(nil)
	if json.Unmarshal(raw, &value) != nil {
		return nil, workspaceImportInputError()
	}
	rewritten, err := rewriteWorkspaceImportArchiveBlockTrashValue(db, "", value)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(rewritten)
	if err != nil {
		return nil, workspaceImportInputError()
	}
	return encoded, nil
}

func rewriteWorkspaceImportArchiveBlockTrashDeltas(
	db *sql.DB, deltas any,
) ([]crdt.Delta, error) {
	encoded, err := json.Marshal(deltas)
	if err != nil {
		return nil, workspaceImportInputError()
	}
	value := any(nil)
	if json.Unmarshal(encoded, &value) != nil {
		return nil, workspaceImportInputError()
	}
	rewritten, err := rewriteWorkspaceImportArchiveBlockTrashValue(db, "", value)
	if err != nil {
		return nil, err
	}
	encoded, err = json.Marshal(rewritten)
	if err != nil {
		return nil, workspaceImportInputError()
	}
	var result []crdt.Delta
	if json.Unmarshal(encoded, &result) != nil {
		return nil, workspaceImportInputError()
	}
	return result, nil
}

func rewriteWorkspaceImportArchiveBlockTrashValue(db *sql.DB, field string, value any) (any, error) {
	switch typed := value.(type) {
	case string:
		switch field {
		case "pageId", "childPageId":
			if typed == "" {
				return typed, nil
			}
			return workspaceImportArchiveReference(db, typed, workspaceExportItemPage)
		case "blockId", "parentBlockId", "sourceContainerId", "databaseContainerId", "syncedBlockId":
			return workspaceImportArchiveOptionalLiveOrTrashBlockReference(db, typed)
		case dataSourceTrashOriginField:
			// A detached data source may outlive the database it came from; a
			// missing origin restores as its own database instead of failing.
			target, err := workspaceImportArchiveReference(db, typed, workspaceExportItemBlock)
			if err != nil {
				return "", nil
			}
			return target, nil
		default:
			return rewriteWorkspaceImportArchiveValue(db, field, typed)
		}
	case []any:
		for index, child := range typed {
			rewritten, err := rewriteWorkspaceImportArchiveBlockTrashValue(db, field, child)
			if err != nil {
				return nil, err
			}
			typed[index] = rewritten
		}
	case map[string]any:
		defer keepSyncedCopyOfPage(typed, syncedCopyPage(typed))
		for key, child := range typed {
			// A removed button keeps its actions as a live one's are kept.
			rewrite := rewriteWorkspaceImportArchiveBlockTrashValue
			if workspaceImportArchiveActionFields[key] {
				rewrite = rewriteWorkspaceImportArchiveValue
			}
			rewritten, err := rewrite(db, key, child)
			if err != nil {
				return nil, err
			}
			typed[key] = rewritten
		}
	}
	return value, nil
}

func (r *durableJobRunner) applyWorkspaceArchiveBlockTrash(
	job storedJob,
	payload workspaceImportJobPayload,
	chunk workspaceImportStagedChunk,
	nextOffset int64,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued || current.Cursor != job.Cursor {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.ChunkIndex != payload.ChunkIndex || currentPayload.ItemOffset != 0 {
			if err != nil {
				return err
			}
			return workspaceImportStateError()
		}
		authorization, err := authorizeWorkspace(txApp, current.Owner, current.WorkspaceID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		for _, item := range chunk.Items {
			if err := storeWorkspaceImportArchiveBlockTrash(
				txApp, current, authorization, item, r.clock.now().UnixMilli(),
			); err != nil {
				return err
			}
		}
		currentPayload.SourceVerified = true
		currentPayload.ItemOffset = 0
		currentPayload.ChunkIndex++
		currentPayload.ReadyToPublish = chunk.Last
		currentPayload.Phase = workspaceImportPhaseApply
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, strconv.FormatInt(nextOffset, 10),
			current.Processed+len(chunk.Items), true,
		)
	})
}

func storeWorkspaceImportArchiveBlockTrash(
	app core.App,
	job storedJob,
	authorization workspaceAuthorization,
	item workspaceImportStagedItem,
	now int64,
) error {
	trash := item.BlockTrash
	if trash == nil || item.PageID == "" || trash.RootBlockID == "" {
		return workspaceImportStateError()
	}
	var pageCount int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_pages}} [[page]]
		JOIN {{leafen_workspace_archive_import_pages}} [[owned]] ON [[owned]].[[page_id]]=[[page]].[[id]]
		WHERE [[owned]].[[job_id]]={:jobId} AND [[page]].[[id]]={:pageId}
			AND [[page]].[[workspace]]={:workspaceId} AND [[page]].[[owner]]={:ownerId}
			AND [[page]].[[notion_import_staging]]=TRUE
	`).Bind(dbx.Params{
		"jobId": job.ID, "pageId": item.PageID, "workspaceId": authorization.WorkspaceID,
		"ownerId": authorization.OwnerID,
	}).Row(&pageCount); err != nil || pageCount != 1 {
		if err != nil {
			return err
		}
		return workspaceImportStateErrorAt("archive-apply-block-trash-page")
	}
	return insertWorkspaceImportArchiveBlockTrash(app, authorization, job.Owner, item.PageID, trash, now)
}
