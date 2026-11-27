package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const productFileReferenceOwnerDataSourceIcon = "data_source_icon"

// replaceProductFileDataSourceIconReference keeps exactly one reference for a
// managed image icon owned by the data source on its current page.
func replaceProductFileDataSourceIconReference(
	app core.App,
	workspaceID, pageID, dataSourceID, icon, iconType string,
) error {
	if err := deleteProductFileDataSourceIconReference(app, workspaceID, pageID, dataSourceID); err != nil {
		return err
	}
	iconID, managed := productFilePresentationID(icon)
	if iconType != graphDataSourceIconImage || !managed {
		return nil
	}
	if _, err := loadGraphFileAuthorityRecords(app, workspaceID, []string{iconID}); err != nil {
		return err
	}
	return insertProductFileReferences(
		app, workspaceID, pageID, productFileReferenceOwnerDataSourceIcon, dataSourceID, "",
		graphFilesValue{{FileID: iconID}},
	)
}

func deleteProductFileDataSourceIconReference(
	app core.App,
	workspaceID, pageID, dataSourceID string,
) error {
	rows := []struct {
		FileID string `db:"file_id"`
	}{}
	if err := app.DB().NewQuery(`
		DELETE FROM {{leafen_file_references}}
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
			AND [[owner_kind]]={:ownerKind} AND [[owner_id]]={:dataSourceId}
		RETURNING [[file_id]]
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "pageId": pageID,
		"ownerKind": productFileReferenceOwnerDataSourceIcon, "dataSourceId": dataSourceID,
	}).All(&rows); err != nil {
		return err
	}
	fileIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		fileIDs = append(fileIDs, row.FileID)
	}
	return reconcileProductFileReferenceLifecycle(app, workspaceID, fileIDs)
}
