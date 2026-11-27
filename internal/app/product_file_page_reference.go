package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	productFileReferenceOwnerPageIcon  = "page_icon"
	productFileReferenceOwnerPageCover = "page_cover"
	productFilePageBackfillID          = "page_presentation_v1"
	productFilePageBackfillTable       = "leafen_page_file_reference_backfill"
	productFilePageIconIndex           = "idx_leafen_pages_image_icon"
	productFilePageCoverIndex          = "idx_leafen_pages_cover"
)

type productFilePageBackfillResult struct {
	Processed int
	HasMore   bool
}

type storedProductFilePageBackfill struct {
	PageCursor string `db:"page_cursor"`
	Completed  bool   `db:"completed"`
}

type storedProductFilePageBackfillPage struct {
	ID        string `db:"id"`
	Workspace string `db:"workspace"`
}

type storedProductFilePageReference struct {
	WorkspaceID string `db:"workspace_id"`
	FileID      string `db:"file_id"`
}

func replaceProductFilePageReferences(
	app core.App,
	workspaceID, pageID, icon, iconType, cover string,
) error {
	requested := map[string]string{}
	if iconID, managed := productFilePresentationID(icon); iconType == "image" && managed {
		requested[productFileReferenceOwnerPageIcon] = iconID
	}
	if coverID, managed := productFilePresentationID(cover); managed {
		requested[productFileReferenceOwnerPageCover] = coverID
	}
	uniqueFileIDs := map[string]struct{}{}
	for _, fileID := range requested {
		uniqueFileIDs[fileID] = struct{}{}
	}
	fileIDs := make([]string, 0, len(uniqueFileIDs))
	for fileID := range uniqueFileIDs {
		fileIDs = append(fileIDs, fileID)
	}
	sort.Strings(fileIDs)
	if len(fileIDs) > 0 {
		if _, err := loadGraphFileAuthorityRecords(app, workspaceID, fileIDs); err != nil {
			return err
		}
	}
	prior := []struct {
		FileID string `db:"file_id"`
	}{}
	if err := app.DB().NewQuery(`
		DELETE FROM {{leafen_file_references}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:pageId}
			AND [[owner_kind]] IN ({:iconOwner}, {:coverOwner})
		RETURNING [[file_id]]
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "pageId": pageID,
		"iconOwner":  productFileReferenceOwnerPageIcon,
		"coverOwner": productFileReferenceOwnerPageCover,
	}).All(&prior); err != nil {
		return err
	}
	for ownerKind, fileID := range requested {
		if _, err := app.DB().Insert("leafen_file_references", dbx.Params{
			"workspace_id": workspaceID, "page_id": pageID,
			"owner_kind": ownerKind, "owner_id": pageID,
			"property_id": "", "file_id": fileID,
		}).Execute(); err != nil {
			return err
		}
	}
	affected := make([]string, 0, len(prior)+len(fileIDs))
	for _, row := range prior {
		affected = append(affected, row.FileID)
	}
	affected = append(affected, fileIDs...)
	return reconcileProductFileReferenceLifecycle(app, workspaceID, affected)
}

func backfillProductFilePageReferences(
	ctx context.Context,
	app core.App,
	limit int,
) (productFilePageBackfillResult, error) {
	result := productFilePageBackfillResult{}
	if limit <= 0 || limit > fileReferenceCleanupTurnLimit {
		limit = fileReferenceCleanupTurnLimit
	}
	state := storedProductFilePageBackfill{}
	if err := app.DB().NewQuery(`
		SELECT [[page_cursor]],[[completed]]
		FROM {{leafen_page_file_reference_backfill}} WHERE [[id]]={:id}
	`).WithContext(ctx).Bind(dbx.Params{"id": productFilePageBackfillID}).One(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, errors.New(errorCodeSchemaMigrationHistoryInvalid)
		}
		return result, err
	}
	if state.Completed {
		return result, nil
	}
	err := app.RunInTransaction(func(txApp core.App) error {
		state := storedProductFilePageBackfill{}
		if err := txApp.DB().NewQuery(`
			SELECT [[page_cursor]],[[completed]]
			FROM {{leafen_page_file_reference_backfill}} WHERE [[id]]={:id}
		`).WithContext(ctx).Bind(dbx.Params{"id": productFilePageBackfillID}).One(&state); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New(errorCodeSchemaMigrationHistoryInvalid)
			}
			return err
		}
		if state.Completed {
			return nil
		}
		pages := []storedProductFilePageBackfillPage{}
		if err := txApp.DB().NewQuery(`
			SELECT [[id]],[[workspace]] FROM {{leafen_pages}}
			WHERE [[id]]>{:cursor} ORDER BY [[id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"cursor": state.PageCursor, "limit": limit,
		}).All(&pages); err != nil {
			return err
		}
		if len(pages) == 0 {
			_, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_page_file_reference_backfill}}
				SET [[completed]]=1,[[updated_at]]={:now} WHERE [[id]]={:id}
			`).WithContext(ctx).Bind(dbx.Params{
				"id": productFilePageBackfillID, "now": time.Now().UTC().UnixMilli(),
			}).Execute()
			return err
		}
		params := dbx.Params{
			"iconOwner":       productFileReferenceOwnerPageIcon,
			"coverOwner":      productFileReferenceOwnerPageCover,
			"authority":       fileAuthorityWorkspace,
			"deletePending":   fileDeleteStatusPending,
			"uploadCompleted": fileUploadStatusCompleted,
		}
		bindings := make([]string, 0, len(pages))
		for index, page := range pages {
			key := fmt.Sprintf("page%d", index)
			params[key] = page.ID
			bindings = append(bindings, "{:"+key+"}")
		}
		pageSet := strings.Join(bindings, ",")
		prior := []storedProductFilePageReference{}
		if err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_file_references}}
			WHERE [[page_id]] IN (` + pageSet + `)
				AND [[owner_kind]] IN ({:iconOwner},{:coverOwner})
			RETURNING [[workspace_id]],[[file_id]]
		`).WithContext(ctx).Bind(params).All(&prior); err != nil {
			return err
		}
		// "+" keeps the pages' icon and cover indexes out: with statistics
		// from a night that had no such page, SQLite read that index whole
		// instead of finding these pages by key (2026-10-18). A database
		// page's icon is held by its row's own reference
		// (database_row_page_icon, the row page icon pass), never by a page
		// icon reference on it: one left there outlived the icon it held
		// when the row's icon changed, and this pass takes it away.
		if _, err := txApp.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_file_references}}
				([[workspace_id]],[[page_id]],[[owner_kind]],[[owner_id]],[[property_id]],[[file_id]])
			SELECT [[page]].[[workspace]],[[page]].[[id]],{:iconOwner},[[page]].[[id]],'',[[file]].[[id]]
			FROM {{leafen_pages}} AS [[page]]
			JOIN {{leafen_files}} AS [[file]] ON [[file]].[[id]]=` + productFilePresentationSQL("[[page]].[[icon]]") + `
				AND [[file]].[[workspace]]=[[page]].[[workspace]]
				AND [[file]].[[authority_state]]={:authority} AND [[file]].[[created_by]]!=''
			WHERE [[page]].[[id]] IN (` + pageSet + `)
				AND +[[page]].[[icon_type]]='image' AND +[[page]].[[icon]]!=''
				AND NOT EXISTS (SELECT 1 FROM {{leafen_database_row_pages}} AS [[row_page]]
					WHERE [[row_page]].[[row_id]]=[[page]].[[id]])
				AND NOT EXISTS (SELECT 1 FROM {{leafen_file_delete_receipts}} AS [[deletion]]
					WHERE [[deletion]].[[file_id]]=[[file]].[[id]] AND [[deletion]].[[status]]={:deletePending})
				AND NOT EXISTS (SELECT 1 FROM {{leafen_file_uploads}} AS [[upload]]
					WHERE [[upload]].[[file_id]]=[[file]].[[id]] AND [[upload]].[[status]]!={:uploadCompleted})
		`).WithContext(ctx).Bind(params).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_file_references}}
				([[workspace_id]],[[page_id]],[[owner_kind]],[[owner_id]],[[property_id]],[[file_id]])
			SELECT [[page]].[[workspace]],[[page]].[[id]],{:coverOwner},[[page]].[[id]],'',[[file]].[[id]]
			FROM {{leafen_pages}} AS [[page]]
			JOIN {{leafen_files}} AS [[file]] ON [[file]].[[id]]=` + productFilePresentationSQL("[[page]].[[cover]]") + `
				AND [[file]].[[workspace]]=[[page]].[[workspace]]
				AND [[file]].[[authority_state]]={:authority} AND [[file]].[[created_by]]!=''
			WHERE [[page]].[[id]] IN (` + pageSet + `) AND +[[page]].[[cover]]!=''
				AND NOT EXISTS (SELECT 1 FROM {{leafen_file_delete_receipts}} AS [[deletion]]
					WHERE [[deletion]].[[file_id]]=[[file]].[[id]] AND [[deletion]].[[status]]={:deletePending})
				AND NOT EXISTS (SELECT 1 FROM {{leafen_file_uploads}} AS [[upload]]
					WHERE [[upload]].[[file_id]]=[[file]].[[id]] AND [[upload]].[[status]]!={:uploadCompleted})
		`).WithContext(ctx).Bind(params).Execute(); err != nil {
			return err
		}
		current := []storedProductFilePageReference{}
		if err := txApp.DB().NewQuery(`
			SELECT [[workspace_id]],[[file_id]] FROM {{leafen_file_references}}
			WHERE [[page_id]] IN (` + pageSet + `)
				AND [[owner_kind]] IN ({:iconOwner},{:coverOwner})
		`).WithContext(ctx).Bind(params).All(&current); err != nil {
			return err
		}
		affected := map[string][]string{}
		for _, reference := range append(prior, current...) {
			affected[reference.WorkspaceID] = append(affected[reference.WorkspaceID], reference.FileID)
		}
		for workspaceID, fileIDs := range affected {
			if err := reconcileProductFileReferenceLifecycle(txApp, workspaceID, fileIDs); err != nil {
				return err
			}
		}
		result.Processed = len(pages)
		result.HasMore = len(pages) == limit
		completed := !result.HasMore
		_, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_page_file_reference_backfill}}
			SET [[page_cursor]]={:cursor},[[completed]]={:completed},[[updated_at]]={:now}
			WHERE [[id]]={:id}
		`).WithContext(ctx).Bind(dbx.Params{
			"id": productFilePageBackfillID, "cursor": pages[len(pages)-1].ID,
			"completed": completed, "now": time.Now().UTC().UnixMilli(),
		}).Execute()
		return err
	})
	return result, err
}
