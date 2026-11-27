package app

import (
	"database/sql"
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database page's body lives as a page body, and its trashed blocks as page
// block trash on the database page (row_block_page_trash_schema.go). An
// archive of version 13 and after holds such an entry as a page's: a
// rowBlockTrash line with the blockTrash snapshot (blocks with their places
// and text, and the views their databases own) and the database page's row.
// It comes back as a page's entry does, on the database page, with its
// databases there in the trash, and restoring it brings them back with their
// views. An archive of version 12 and before held it in the row form a
// database page's body had once (blocks with their text in their content,
// without the views), and a database in it failed the whole import
// (2026-11-17). Such an entry is read into the page form first; a linked
// database in it, whose views the row form lost, comes back with one table
// view of its source under the ID of the view it showed.

// workspaceImportArchiveRowTrashRowFormFormatVersion is the last archive
// whose database page block trash is in the row form.
const workspaceImportArchiveRowTrashRowFormFormatVersion = 12

type workspaceImportArchiveRowTrashValue struct {
	archiveBlockTrashValue
	RowID string `json:"rowId"`
}

// readWorkspaceImportArchiveRowTrashAsNow puts an older archive's database
// page block trash entry in the page form, in place.
func readWorkspaceImportArchiveRowTrashAsNow(formatVersion int, entity workspaceImportArchiveEntity) error {
	if entity.Kind != workspaceExportItemRowBlockTrash ||
		formatVersion > workspaceImportArchiveRowTrashRowFormFormatVersion {
		return nil
	}
	value, err := decodeWorkspaceImportArchiveRowBlockTrash(entity.Value)
	if err != nil {
		return err
	}
	snapshot, err := workspaceImportArchiveRowTrashPageSnapshot(value.Snapshot)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return workspaceImportInputError()
	}
	pageForm := map[string]any{}
	if json.Unmarshal(encoded, &pageForm) != nil {
		return workspaceImportInputError()
	}
	entity.Value["snapshot"] = pageForm
	return nil
}

// workspaceImportArchiveRowTrashPageSnapshot is a row form snapshot in the
// page form, as moveRowBlockTrashToPageTrash moved the server's own: siblings
// keep their order through each block's previous sibling, and a block's text
// leaves its content for its text.
func workspaceImportArchiveRowTrashPageSnapshot(
	rowForm archiveRowBlockTrashSnapshot,
) (recoverableBlockTrashSnapshot, error) {
	order := slices.Clone(rowForm.Blocks)
	slices.SortStableFunc(order, func(left, right archiveRowBlockTrashBlock) int {
		if left.ParentID != right.ParentID {
			return strings.Compare(left.ParentID, right.ParentID)
		}
		if left.Position != right.Position {
			return strings.Compare(left.Position, right.Position)
		}
		return strings.Compare(left.ID, right.ID)
	})
	previous, lastInParent := map[string]string{}, map[string]string{}
	for _, block := range order {
		previous[block.ID] = lastInParent[block.ParentID]
		lastInParent[block.ParentID] = block.ID
	}
	snapshot := recoverableBlockTrashSnapshot{Format: 1, RootID: rowForm.RootID}
	for index, block := range rowForm.Blocks {
		content := map[string]any{}
		if json.Unmarshal(block.Content, &content) != nil {
			return recoverableBlockTrashSnapshot{}, workspaceImportInputError()
		}
		spans := databaseRowBlockTextSpans(content)
		for _, key := range rowBodyStoredTextKeys {
			delete(content, key)
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return recoverableBlockTrashSnapshot{}, workspaceImportInputError()
		}
		afterID := previous[block.ID]
		if index == 0 {
			afterID = ""
		}
		pageBlock := recoverableBlockTrashBlock{
			ID: block.ID, ParentID: block.ParentID, AfterID: afterID, Position: block.Position,
			Type: block.Type, Content: encoded, Text: notionGraphTextSpansDelta(spans),
		}
		if linked, isLinked := recoverableLinkedDatabaseContainer(pageBlock); isLinked {
			snapshot.Views = append(snapshot.Views, graphDatabaseView{
				ID: linked.ViewID, DataSourceID: linked.DataSourceID, ContainerID: block.ID,
				Type: graphViewTypeTable, Name: "", Position: "a0", Config: json.RawMessage(`{}`),
			})
		}
		snapshot.Blocks = append(snapshot.Blocks, pageBlock)
	}
	return snapshot, nil
}

// decodeWorkspaceImportArchiveRowTrash reads a database page's trash entry in
// the page form: a page's entry whose blocks a database page's body holds.
func decodeWorkspaceImportArchiveRowTrash(raw map[string]any) (workspaceImportArchiveRowTrashValue, error) {
	page, _, err := decodeWorkspaceImportArchiveBlockTrash(raw)
	if err != nil {
		return workspaceImportArchiveRowTrashValue{}, err
	}
	rowID, _ := raw["rowId"].(string)
	if !validWorkspaceImportArchiveRowBlockID(rowID) || !validWorkspaceImportArchiveRowBlockID(page.PageID) {
		return workspaceImportArchiveRowTrashValue{}, workspaceImportInputError()
	}
	for _, block := range page.Snapshot.Blocks {
		if !validWorkspaceImportArchiveRowBlockID(block.ID) || !workspaceImportArchiveRowBlockType(block.Type) {
			return workspaceImportArchiveRowTrashValue{}, workspaceImportInputError()
		}
	}
	return workspaceImportArchiveRowTrashValue{archiveBlockTrashValue: page, RowID: rowID}, nil
}

func workspaceImportArchiveRowBlockTrashItem(db *sql.DB, encoded string) (workspaceImportStagedItem, error) {
	raw := map[string]any{}
	if json.Unmarshal([]byte(encoded), &raw) != nil {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	value, err := decodeWorkspaceImportArchiveRowTrash(raw)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	pageID, err := workspaceImportArchiveHostReference(db, value.PageID)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	rowID, err := workspaceImportArchiveReference(db, value.RowID, workspaceExportItemRow)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	trash, err := workspaceImportArchiveStagedTrash(db, value.archiveBlockTrashValue, workspaceExportItemRowPageBlock)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	return workspaceImportStagedItem{PageID: pageID, RowBlockTrash: &workspaceImportStagedRowBlockTrash{
		RootBlockID: trash.RootBlockID, RowID: rowID, Trash: trash,
	}}, nil
}

func (r *durableJobRunner) applyWorkspaceArchiveRowBlockTrash(job storedJob, payload workspaceImportJobPayload, chunk workspaceImportStagedChunk, nextOffset int64) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, currentPayload, authorization, err := loadWorkspaceImportRowBlockTurn(txApp, job, payload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		for _, item := range chunk.Items {
			trash := item.RowBlockTrash
			if trash == nil || item.PageID == "" || trash.RootBlockID == "" || trash.RowID == "" {
				return workspaceImportStateError()
			}
			if err := validateWorkspaceImportRowBlockOwner(txApp, current, authorization, item.PageID, trash.RowID); err != nil {
				return err
			}
			if trash.Trash != nil {
				if trash.Trash.RootBlockID != trash.RootBlockID {
					return workspaceImportStateError()
				}
				if err := insertWorkspaceImportArchiveBlockTrash(
					txApp, authorization, current.Owner, trash.RowID, trash.Trash, now,
				); err != nil {
					return err
				}
				continue
			}
			if err := insertWorkspaceImportArchiveRowFormTrash(txApp, current, authorization, item.PageID, trash, now); err != nil {
				return err
			}
		}
		return finishWorkspaceImportRowBlockTurn(r, txApp, current, currentPayload, chunk, nextOffset)
	})
}

// insertWorkspaceImportArchiveRowFormTrash puts a trash entry an import staged
// in the row form on the database page, as page block trash.
func insertWorkspaceImportArchiveRowFormTrash(
	txApp core.App,
	current storedJob,
	authorization workspaceAuthorization,
	pageID string,
	trash *workspaceImportStagedRowBlockTrash,
	now int64,
) error {
	if trash.ExpiresAt <= now {
		return nil
	}
	for index := range trash.Snapshot.Blocks {
		trash.Snapshot.Blocks[index].CreatedBy = current.Owner
		trash.Snapshot.Blocks[index].LastEditedBy = current.Owner
	}
	encoded, err := json.Marshal(trash.Snapshot)
	if err != nil || len(encoded) == 0 || len(encoded) > maxYgoUpdateBytes {
		return workspaceImportInputError()
	}
	stored := storedDatabaseRowBlockTrash{
		RootBlockID: trash.RootBlockID, OwnerID: authorization.OwnerID,
		WorkspaceID: authorization.WorkspaceID, ContainerPageID: pageID, RowID: trash.RowID,
		ParentID: trash.ParentID, PredecessorID: trash.PredecessorID, SuccessorID: trash.SuccessorID,
		SnapshotJSON: string(encoded), BlockCount: len(trash.Snapshot.Blocks), ByteSize: len(encoded),
		DeletedBy: current.Owner, DeletedAt: trash.DeletedAt, ExpiresAt: trash.ExpiresAt,
	}
	if _, err := parseDatabaseRowBlockTrashSnapshot(stored); err != nil {
		return workspaceImportInputError()
	}
	ids := make([]string, len(trash.Snapshot.Blocks))
	for index, block := range trash.Snapshot.Blocks {
		ids[index] = block.ID
	}
	idsJSON, _ := json.Marshal(ids)
	var active int
	if err := txApp.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_blocks}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(idsJSON)}).Row(&active); err != nil || active != 0 {
		if err != nil {
			return err
		}
		return workspaceImportStateErrorAt("archive-apply-row-block-trash-id")
	}
	snapshot, err := moveRowBlockTrashToPageTrash(txApp, stored)
	if err != nil {
		return err
	}
	files, err := recoverableBlockTrashManagedFiles(snapshot)
	if err != nil {
		return err
	}
	if err := insertProductFileReferences(
		txApp, stored.WorkspaceID, stored.RowID, productFileReferenceOwnerBlockTrash,
		stored.RootBlockID, "", files,
	); err != nil {
		return err
	}
	notifyRecoverableBlockTrashRetentionAfterCommit(txApp)
	return nil
}

// insertWorkspaceImportArchiveBlockTrash puts an imported trash entry on its
// page, a page or a database page, with its databases there in the trash.
func insertWorkspaceImportArchiveBlockTrash(
	app core.App,
	authorization workspaceAuthorization,
	deletedBy, pageID string,
	trash *workspaceImportStagedBlockTrash,
	now int64,
) error {
	encoded, err := json.Marshal(trash.Snapshot)
	if err != nil {
		return workspaceImportInputError()
	}
	row := storedRecoverableBlockTrash{
		RootBlockID: trash.RootBlockID, OwnerID: authorization.OwnerID,
		WorkspaceID: authorization.WorkspaceID, PageID: pageID,
		ParentID: trash.ParentID, PredecessorID: trash.PredecessorID, SuccessorID: trash.SuccessorID,
		SnapshotJSON: string(encoded), BlockCount: len(trash.Snapshot.Blocks), ByteSize: len(encoded),
		DeletedBy: deletedBy, DeletedAt: trash.DeletedAt, ExpiresAt: trash.ExpiresAt,
	}
	snapshot, err := parseRecoverableBlockTrashSnapshot(row)
	if err != nil || row.ExpiresAt <= row.DeletedAt {
		return workspaceImportInputError()
	}
	if row.ExpiresAt <= now {
		return nil
	}
	ids := make([]string, len(snapshot.Blocks))
	for index, block := range snapshot.Blocks {
		ids[index] = block.ID
	}
	idsJSON, _ := json.Marshal(ids)
	var active int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_blocks}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(idsJSON)}).Row(&active); err != nil {
		return err
	}
	if active != 0 {
		return workspaceImportStateErrorAt("archive-apply-block-trash-id")
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_block_trash_tombstones}}
			([[root_block_id]],[[owner_id]],[[workspace_id]],[[page_id]],[[parent_id]],
			 [[predecessor_id]],[[successor_id]],[[snapshot_json]],[[block_count]],[[byte_size]],
			 [[deleted_by]],[[deleted_at]],[[expires_at]])
		VALUES
			({:rootBlockId},{:ownerId},{:workspaceId},{:pageId},{:parentId},
			 {:predecessorId},{:successorId},{:snapshotJSON},{:blockCount},{:byteSize},
			 {:deletedBy},{:deletedAt},{:expiresAt})
	`).Bind(dbx.Params{
		"rootBlockId": row.RootBlockID, "ownerId": row.OwnerID, "workspaceId": row.WorkspaceID,
		"pageId": row.PageID, "parentId": row.ParentID, "predecessorId": row.PredecessorID,
		"successorId": row.SuccessorID, "snapshotJSON": row.SnapshotJSON,
		"blockCount": row.BlockCount, "byteSize": row.ByteSize, "deletedBy": row.DeletedBy,
		"deletedAt": row.DeletedAt, "expiresAt": row.ExpiresAt,
	}).Execute()
	if err == nil {
		for _, block := range snapshot.Blocks {
			if block.Type != graphBlockTypeInlineDatabase {
				continue
			}
			content, _, valid := recoverableDatabaseContainer(block)
			if !valid {
				return workspaceImportInputError()
			}
			if err = projectDatabaseContainerBlock(
				app, authorization.OwnerID, authorization.WorkspaceID, pageID,
				block.ID, block.ParentID, block.Position, content,
			); err != nil {
				return err
			}
		}
		err = markRecoverableDatabaseContainerTrash(
			app, authorization.OwnerID, authorization.WorkspaceID, pageID,
			row.DeletedBy, snapshot, row.DeletedAt, row.ExpiresAt,
		)
	}
	if err == nil {
		files, fileErr := recoverableBlockTrashManagedFiles(snapshot)
		if fileErr != nil {
			return workspaceImportInputError()
		}
		err = insertProductFileReferences(
			app, row.WorkspaceID, row.PageID,
			productFileReferenceOwnerBlockTrash, row.RootBlockID, "", files,
		)
	}
	if err == nil {
		notifyRecoverableBlockTrashRetentionAfterCommit(app)
	}
	return err
}
