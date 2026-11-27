package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	productFileReferenceOwnerBlock      = "block"
	productFileReferenceOwnerBlockTrash = "block_trash"
	// A page history span's file (schema_baseline.json).
	productFileReferenceOwnerPageHistory = "page_history"
	productFileReferenceOwnerRow         = "database_row_value"
	productFileReferenceOwnerComment     = "comment"
	productFileReferenceOwnerTemplate    = "database_template"
	productFileReferenceWriteBatch       = 100
	productFileDeleteReceiptsTable       = "leafen_file_delete_receipts"
)

func replaceProductFileBlockReference(
	app core.App,
	workspaceID, pageID string,
	block *graphBlock,
) error {
	if err := deleteProductFileBlockReferences(app, workspaceID, pageID, []string{block.ID}); err != nil {
		return err
	}
	if graphBlockCarriesManagedFileReference(block) {
		if _, err := parseGraphFileBlockContent(block.Content); err != nil {
			return err
		}
	}
	fileID := graphBlockKeptFileID(block)
	if fileID == "" {
		return nil
	}
	return insertProductFileReferences(
		app, workspaceID, pageID, productFileReferenceOwnerBlock, block.ID, "",
		graphFilesValue{{FileID: fileID}},
	)
}

func deleteProductFileBlockReferences(
	app core.App,
	workspaceID, pageID string,
	blockIDs []string,
) error {
	affected := []string{}
	for start := 0; start < len(blockIDs); start += productFileReferenceWriteBatch {
		end := min(start+productFileReferenceWriteBatch, len(blockIDs))
		params := dbx.Params{
			"workspaceId": workspaceID, "pageId": pageID,
			"ownerKind": productFileReferenceOwnerBlock,
		}
		bindings := make([]string, 0, end-start)
		for index, blockID := range blockIDs[start:end] {
			key := fmt.Sprintf("owner%d", index)
			params[key] = blockID
			bindings = append(bindings, "{:"+key+"}")
		}
		rows := []struct {
			FileID string `db:"file_id"`
		}{}
		if err := app.DB().NewQuery(`
			DELETE FROM {{leafen_file_references}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:pageId}
				AND [[owner_kind]] = {:ownerKind}
				AND [[owner_id]] IN (` + strings.Join(bindings, ",") + `)
			RETURNING [[file_id]]
		`).Bind(params).All(&rows); err != nil {
			return err
		}
		for _, row := range rows {
			affected = append(affected, row.FileID)
		}
	}
	return reconcileProductFileReferenceLifecycle(app, workspaceID, affected)
}

func replaceProductFileRowReferences(
	app core.App,
	workspaceID, pageID, rowID, propertyID string,
	cell databaseCellAuthority,
) error {
	if err := deleteProductFileRowReferences(app, workspaceID, pageID, rowID, propertyID); err != nil {
		return err
	}
	if cell.Type != graphValueTypeFiles {
		return nil
	}
	encoded, err := json.Marshal(cell.Value)
	if err != nil {
		return err
	}
	items, err := parseGraphFileValueItems(encoded)
	if err != nil {
		return err
	}
	return insertProductFileReferences(
		app, workspaceID, pageID, productFileReferenceOwnerRow, rowID, propertyID, items,
	)
}

func deleteProductFileRowReferences(
	app core.App,
	workspaceID, pageID, rowID, propertyID string,
) error {
	rows := []struct {
		FileID string `db:"file_id"`
	}{}
	err := app.DB().NewQuery(`
		DELETE FROM {{leafen_file_references}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:pageId}
			AND [[owner_kind]] = {:ownerKind}
			AND ({:rowId} = '' OR [[owner_id]] = {:rowId})
			AND ({:propertyId} = '' OR [[property_id]] = {:propertyId})
		RETURNING [[file_id]]
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "pageId": pageID,
		"ownerKind": productFileReferenceOwnerRow,
		"rowId":     rowID, "propertyId": propertyID,
	}).All(&rows)
	if err != nil {
		return err
	}
	fileIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		fileIDs = append(fileIDs, row.FileID)
	}
	return reconcileProductFileReferenceLifecycle(app, workspaceID, fileIDs)
}

func deleteProductFileCommentReferences(
	app core.App,
	workspaceID, pageID, commentID string,
) error {
	rows := []struct {
		FileID string `db:"file_id"`
	}{}
	err := app.DB().NewQuery(`
		DELETE FROM {{leafen_file_references}}
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
			AND [[owner_kind]]={:ownerKind} AND [[owner_id]]={:commentId}
		RETURNING [[file_id]]
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "pageId": pageID,
		"ownerKind": productFileReferenceOwnerComment, "commentId": commentID,
	}).All(&rows)
	if err != nil {
		return err
	}
	fileIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		fileIDs = append(fileIDs, row.FileID)
	}
	return reconcileProductFileReferenceLifecycle(app, workspaceID, fileIDs)
}

func deleteProductFileSourceReferences(
	app core.App,
	workspaceID, pageID, sourceID string,
) error {
	rows := []struct {
		FileID string `db:"file_id"`
	}{}
	err := app.DB().NewQuery(`
		DELETE FROM {{leafen_file_references}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:pageId}
			AND [[owner_kind]] IN ({:rowOwnerKind},{:iconOwnerKind})
			AND [[owner_id]] IN (
				SELECT [[id]] FROM {{leafen_database_rows}}
				WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:pageId}
					AND [[data_source_id]] = {:sourceId}
			)
		RETURNING [[file_id]]
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "pageId": pageID,
		"rowOwnerKind":  productFileReferenceOwnerRow,
		"iconOwnerKind": productFileReferenceOwnerDatabaseRowPageIcon,
		"sourceId":      sourceID,
	}).All(&rows)
	if err != nil {
		return err
	}
	fileIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		fileIDs = append(fileIDs, row.FileID)
	}
	return reconcileProductFileReferenceLifecycle(app, workspaceID, fileIDs)
}

func insertProductFileReferences(
	app core.App,
	workspaceID, pageID, ownerKind, ownerID, propertyID string,
	items graphFilesValue,
) error {
	fileIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.FileID != "" {
			fileIDs = append(fileIDs, item.FileID)
		}
	}
	for start := 0; start < len(fileIDs); start += productFileReferenceWriteBatch {
		end := min(start+productFileReferenceWriteBatch, len(fileIDs))
		params := dbx.Params{
			"workspaceId": workspaceID, "pageId": pageID,
			"ownerKind": ownerKind, "ownerId": ownerID, "propertyId": propertyID,
		}
		values := make([]string, 0, end-start)
		for index, fileID := range fileIDs[start:end] {
			key := fmt.Sprintf("file%d", index)
			params[key] = fileID
			values = append(values, `(
				{:workspaceId}, {:pageId}, {:ownerKind}, {:ownerId}, {:propertyId}, {:`+key+`}
			)`)
		}
		if _, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_file_references}}
				([[workspace_id]], [[page_id]], [[owner_kind]], [[owner_id]], [[property_id]], [[file_id]])
			VALUES ` + strings.Join(values, ",") + `
		`).Bind(params).Execute(); err != nil {
			return err
		}
	}
	now := time.Now().UTC().UnixMilli()
	for _, fileID := range fileIDs {
		if err := markNotionFileUploadAttached(app, fileID, now); err != nil {
			return err
		}
	}
	return reconcileProductFileReferenceLifecycle(app, workspaceID, fileIDs)
}
