package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database's cover, as Notion's "커버 추가" puts one over an inline
// database opened alone (seen 2026-10-01): it shows there only, not over the
// database where a page holds it. A full-page database's cover is its page's.
const (
	productFileReferenceOwnerDataSourceCover = "data_source_cover"
)

// replaceProductFileDataSourceCoverReference keeps exactly one reference for
// a managed cover image owned by the data source on its current page.
func replaceProductFileDataSourceCoverReference(app core.App, workspaceID, pageID, dataSourceID, cover string) error {
	if err := deleteProductFileDataSourceCoverReference(app, workspaceID, pageID, dataSourceID); err != nil {
		return err
	}
	coverID, managed := productFilePresentationID(cover)
	if !managed {
		return nil
	}
	if _, err := loadGraphFileAuthorityRecords(app, workspaceID, []string{coverID}); err != nil {
		return err
	}
	return insertProductFileReferences(
		app, workspaceID, pageID, productFileReferenceOwnerDataSourceCover, dataSourceID, "",
		graphFilesValue{{FileID: coverID}},
	)
}

func deleteProductFileDataSourceCoverReference(app core.App, workspaceID, pageID, dataSourceID string) error {
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
		"ownerKind": productFileReferenceOwnerDataSourceCover, "dataSourceId": dataSourceID,
	}).All(&rows); err != nil {
		return err
	}
	fileIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		fileIDs = append(fileIDs, row.FileID)
	}
	return reconcileProductFileReferenceLifecycle(app, workspaceID, fileIDs)
}
