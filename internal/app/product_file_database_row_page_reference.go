package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	productFileReferenceOwnerDatabaseRowPageIcon = "database_row_page_icon"
	databaseRowPageIconBackfillID                = "database_row_page_icon_v1"
)

type databaseRowPageIconBackfillResult struct {
	Processed int
	HasMore   bool
}

type storedDatabaseRowPageIconBackfill struct {
	RowCursor string `db:"row_cursor"`
	Completed bool   `db:"completed"`
}

type storedDatabaseRowPageIconBackfillRow struct {
	RowID           string `db:"row_id"`
	WorkspaceID     string `db:"workspace_id"`
	ContainerPageID string `db:"container_page_id"`
}

func replaceProductFileDatabaseRowPageIconReference(
	app core.App,
	workspaceID, containerPageID, rowID, icon, iconType string,
) error {
	if err := deleteProductFileDatabaseRowPageIconReference(
		app, workspaceID, containerPageID, rowID,
	); err != nil {
		return err
	}
	iconID, managed := productFilePresentationID(icon)
	if iconType != "image" || !managed {
		return nil
	}
	if _, err := loadGraphFileAuthorityRecords(app, workspaceID, []string{iconID}); err != nil {
		return err
	}
	return insertProductFileReferences(
		app, workspaceID, containerPageID,
		productFileReferenceOwnerDatabaseRowPageIcon, rowID, "",
		graphFilesValue{{FileID: iconID}},
	)
}

// deleteProductFileDatabaseRowPageIconReference takes away what holds a
// database page's icon file: the row's own reference, and a page icon
// reference on the row page itself, which a database page doesn't hold (the
// page pass and template copies once wrote one, and it kept the old icon's
// file after the icon changed).
func deleteProductFileDatabaseRowPageIconReference(
	app core.App,
	workspaceID, containerPageID, rowID string,
) error {
	rows := []struct {
		FileID string `db:"file_id"`
	}{}
	// The references are found by their page; the workspace is read after
	// (+), or SQLite, which keeps no statistics, reads the workspace's
	// every reference.
	if err := app.DB().NewQuery(`
		DELETE FROM {{leafen_file_references}}
		WHERE +[[workspace_id]]={:workspaceId} AND [[owner_id]]={:rowId} AND (
			([[page_id]]={:pageId} AND [[owner_kind]]={:ownerKind})
			OR ([[page_id]]={:rowId} AND [[owner_kind]]={:pageIconKind}))
		RETURNING [[file_id]]
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "pageId": containerPageID,
		"ownerKind": productFileReferenceOwnerDatabaseRowPageIcon, "rowId": rowID,
		"pageIconKind": productFileReferenceOwnerPageIcon,
	}).All(&rows); err != nil {
		return err
	}
	fileIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		fileIDs = append(fileIDs, row.FileID)
	}
	return reconcileProductFileReferenceLifecycle(app, workspaceID, fileIDs)
}

func backfillDatabaseRowPageIconReferences(
	ctx context.Context,
	app core.App,
	limit int,
) (databaseRowPageIconBackfillResult, error) {
	result := databaseRowPageIconBackfillResult{}
	if limit <= 0 || limit > fileReferenceCleanupTurnLimit {
		limit = fileReferenceCleanupTurnLimit
	}
	state := storedDatabaseRowPageIconBackfill{}
	if err := app.DB().NewQuery(`
		SELECT [[row_cursor]],[[completed]]
		FROM {{leafen_database_row_page_icon_reference_backfill}} WHERE [[id]]={:id}
	`).WithContext(ctx).Bind(dbx.Params{"id": databaseRowPageIconBackfillID}).One(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, errors.New(errorCodeSchemaMigrationHistoryInvalid)
		}
		return result, err
	}
	if state.Completed {
		return result, nil
	}
	err := app.RunInTransaction(func(txApp core.App) error {
		state := storedDatabaseRowPageIconBackfill{}
		if err := txApp.DB().NewQuery(`
			SELECT [[row_cursor]],[[completed]]
			FROM {{leafen_database_row_page_icon_reference_backfill}} WHERE [[id]]={:id}
		`).WithContext(ctx).Bind(dbx.Params{"id": databaseRowPageIconBackfillID}).One(&state); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New(errorCodeSchemaMigrationHistoryInvalid)
			}
			return err
		}
		if state.Completed {
			return nil
		}
		rows := []storedDatabaseRowPageIconBackfillRow{}
		if err := txApp.DB().NewQuery(`
			SELECT [[row_id]],[[workspace_id]],[[container_page_id]]
			FROM {{leafen_database_row_pages}}
			WHERE [[row_id]]>{:cursor} ORDER BY [[row_id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"cursor": state.RowCursor, "limit": limit,
		}).All(&rows); err != nil {
			return err
		}
		if len(rows) == 0 {
			_, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_database_row_page_icon_reference_backfill}}
				SET [[completed]]=1,[[updated_at]]={:now} WHERE [[id]]={:id}
			`).WithContext(ctx).Bind(dbx.Params{
				"id": databaseRowPageIconBackfillID, "now": time.Now().UTC().UnixMilli(),
			}).Execute()
			return err
		}
		for _, row := range rows {
			if err := replaceDatabaseRowPageIconReferenceFromStoredPresentation(
				txApp, row.WorkspaceID, row.ContainerPageID, row.RowID,
			); err != nil {
				return err
			}
		}
		result.Processed = len(rows)
		result.HasMore = len(rows) == limit
		_, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_database_row_page_icon_reference_backfill}}
			SET [[row_cursor]]={:cursor},[[updated_at]]={:now} WHERE [[id]]={:id}
		`).WithContext(ctx).Bind(dbx.Params{
			"id": databaseRowPageIconBackfillID, "cursor": rows[len(rows)-1].RowID,
			"now": time.Now().UTC().UnixMilli(),
		}).Execute()
		return err
	})
	return result, err
}

func replaceDatabaseRowPageIconReferenceFromStoredPresentation(
	app core.App,
	workspaceID, containerPageID, rowID string,
) error {
	var presentation struct {
		Icon     string `db:"icon"`
		IconType string `db:"icon_type"`
	}
	if err := app.DB().NewQuery(`
		SELECT [[row_record]].[[icon]],[[row_record]].[[icon_type]] FROM {{leafen_database_row_pages}} AS [[row_page]]
		JOIN {{leafen_pages}} AS [[row_record]] ON [[row_record]].[[id]]=[[row_page]].[[row_id]]
		WHERE [[row_page]].[[row_id]]={:rowId} AND [[row_page]].[[workspace_id]]={:workspaceId}
			AND [[row_page]].[[container_page_id]]={:pageId}
	`).Bind(dbx.Params{
		"rowId": rowID, "workspaceId": workspaceID, "pageId": containerPageID,
	}).One(&presentation); err != nil {
		return err
	}
	if err := deleteProductFileDatabaseRowPageIconReference(
		app, workspaceID, containerPageID, rowID,
	); err != nil {
		return err
	}
	iconID, managed := productFilePresentationID(presentation.Icon)
	if presentation.IconType != "image" || !managed {
		return nil
	}
	var eligible int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_files}} AS [[file]]
		WHERE [[file]].[[id]]={:fileId} AND [[file]].[[workspace]]={:workspaceId}
			AND [[file]].[[authority_state]]={:authority} AND [[file]].[[created_by]]!=''
			AND NOT EXISTS (SELECT 1 FROM {{leafen_file_delete_receipts}} AS [[deletion]]
				WHERE [[deletion]].[[file_id]]=[[file]].[[id]] AND [[deletion]].[[status]]={:deletePending})
			AND NOT EXISTS (SELECT 1 FROM {{leafen_file_uploads}} AS [[upload]]
				WHERE [[upload]].[[file_id]]=[[file]].[[id]] AND [[upload]].[[status]]!={:uploadCompleted})
	`).Bind(dbx.Params{
		"fileId": iconID, "workspaceId": workspaceID,
		"authority": fileAuthorityWorkspace, "deletePending": fileDeleteStatusPending,
		"uploadCompleted": fileUploadStatusCompleted,
	}).Row(&eligible); err != nil {
		return err
	}
	if eligible != 1 {
		return nil
	}
	return insertProductFileReferences(
		app, workspaceID, containerPageID,
		productFileReferenceOwnerDatabaseRowPageIcon, rowID, "",
		graphFilesValue{{FileID: iconID}},
	)
}

// replaceProductFileRowPageLookReferences writes a database page's look
// references from what the page keeps, as a template's look given to it
// needs: its icon's as the row's own reference, its cover's as a page's.
func replaceProductFileRowPageLookReferences(app core.App, rowID string) error {
	row := struct {
		WorkspaceID     string `db:"workspace_id"`
		ContainerPageID string `db:"container_page_id"`
		Cover           string `db:"cover"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[row_page]].[[workspace_id]],[[row_page]].[[container_page_id]],[[row_record]].[[cover]]
		FROM {{leafen_database_row_pages}} AS [[row_page]]
		JOIN {{leafen_pages}} AS [[row_record]] ON [[row_record]].[[id]]=[[row_page]].[[row_id]]
		WHERE [[row_page]].[[row_id]]={:rowId}
	`).Bind(dbx.Params{"rowId": rowID}).One(&row); err != nil {
		return err
	}
	if err := replaceDatabaseRowPageIconReferenceFromStoredPresentation(
		app, row.WorkspaceID, row.ContainerPageID, rowID,
	); err != nil {
		return err
	}
	return replaceProductFilePageReferences(app, row.WorkspaceID, rowID, "", "none", row.Cover)
}
