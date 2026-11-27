package app

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func advanceProductOrganizationStorageReconciliation(
	ctx context.Context,
	app core.App,
	organizationID string,
	now int64,
) (productOrganizationStorageReconciliationResult, error) {
	if app.IsTransactional() {
		return advanceProductOrganizationStorageReconciliationInTransaction(
			ctx, app, organizationID, now,
		)
	}
	release := acquireProductFileOperationLock(
		productOrganizationStorageLockNamespace, organizationID,
	)
	defer release()
	return advanceProductOrganizationStorageReconciliationLocked(
		ctx, app, organizationID, now,
	)
}

func advanceProductOrganizationStorageReconciliationLocked(
	ctx context.Context,
	app core.App,
	organizationID string,
	now int64,
) (productOrganizationStorageReconciliationResult, error) {
	result := productOrganizationStorageReconciliationResult{}
	err := app.RunInTransaction(func(txApp core.App) error {
		var advanceErr error
		result, advanceErr = advanceProductOrganizationStorageReconciliationInTransaction(
			ctx, txApp, organizationID, now,
		)
		return advanceErr
	})
	return result, err
}

func advanceProductOrganizationStorageReconciliationInTransaction(
	ctx context.Context,
	app core.App,
	organizationID string,
	now int64,
) (productOrganizationStorageReconciliationResult, error) {
	result := productOrganizationStorageReconciliationResult{}
	if organizationID == "" || now <= 0 {
		return result, errOrganizationStorageStateInvalid
	}
	if usage, err := loadProductOrganizationStorageUsage(ctx, app, organizationID); err == nil {
		result.Revision = usage.Version
		return result, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	state, err := loadProductOrganizationStorageReconciliation(ctx, app, organizationID)
	if errors.Is(err, sql.ErrNoRows) {
		state = storedProductOrganizationStorageReconciliation{
			OrganizationID: organizationID,
			AfterUploadID:  organizationStorageReconciliationRows,
		}
		if err := insertProductOrganizationStorageReconciliation(ctx, app, state, now); err != nil {
			return result, err
		}
	} else if err != nil {
		return result, err
	}
	if err := validateProductOrganizationStorageReconciliation(state, organizationID); err != nil {
		return result, err
	}

	remaining := organizationStorageReconciliationWindow
	if strings.HasPrefix(state.AfterUploadID, organizationStorageReconciliationRows) {
		items, hasMore, err := loadProductOrganizationStorageActiveReservations(
			ctx, app, organizationID, state.AfterWorkspaceID, remaining,
		)
		if err != nil {
			return result, err
		}
		if err := applyProductOrganizationStorageActiveReservations(
			organizationID, items, &state,
		); err != nil {
			return result, err
		}
		result.Processed += len(items)
		remaining -= len(items)
		if len(items) > 0 {
			state.AfterWorkspaceID = items[len(items)-1].ID
		}
		if hasMore {
			return persistProductOrganizationStorageReconciliation(
				ctx, app, state, now, result,
			)
		}
		state.AfterWorkspaceID = ""
		state.AfterUploadID = organizationStorageReconciliationFiles
	}
	if remaining == 0 {
		return persistProductOrganizationStorageReconciliation(ctx, app, state, now, result)
	}
	if strings.HasPrefix(state.AfterUploadID, organizationStorageReconciliationFiles) {
		if tableKnown(app, filesCollection) {
			cursor := strings.TrimPrefix(state.AfterUploadID, organizationStorageReconciliationFiles)
			items, hasMore, err := loadProductOrganizationStorageRecoveryFiles(
				ctx, app, organizationID, state.AfterWorkspaceID, cursor, remaining,
			)
			if err != nil {
				return result, err
			}
			if err := applyProductOrganizationStorageRecoveryItems(
				ctx, app, organizationID, items, now, &state,
			); err != nil {
				return result, err
			}
			result.Processed += len(items)
			remaining -= len(items)
			if len(items) > 0 {
				last := items[len(items)-1]
				state.AfterWorkspaceID = last.WorkspaceID
				state.AfterUploadID = organizationStorageReconciliationFiles + last.ReservationID
			}
			if hasMore {
				return persistProductOrganizationStorageReconciliation(
					ctx, app, state, now, result,
				)
			}
		}
		state.AfterWorkspaceID = ""
		state.AfterUploadID = organizationStorageReconciliationUpload
	}
	if remaining == 0 {
		return persistProductOrganizationStorageReconciliation(ctx, app, state, now, result)
	}
	uploadCursor := strings.TrimPrefix(
		state.AfterUploadID, organizationStorageReconciliationUpload,
	)
	items, hasMore, err := loadProductOrganizationStorageRecoveryUploads(
		ctx, app, organizationID, state.AfterWorkspaceID, uploadCursor, remaining,
	)
	if err != nil {
		return result, err
	}
	if err := applyProductOrganizationStorageRecoveryItems(
		ctx, app, organizationID, items, now, &state,
	); err != nil {
		return result, err
	}
	result.Processed += len(items)
	if len(items) > 0 {
		last := items[len(items)-1]
		state.AfterWorkspaceID = last.WorkspaceID
		state.AfterUploadID = organizationStorageReconciliationUpload + last.ReservationID
	}
	if hasMore {
		return persistProductOrganizationStorageReconciliation(ctx, app, state, now, result)
	}
	if state.Revision == int64(^uint64(0)>>1) {
		return result, errOrganizationStorageStateInvalid
	}
	state.Revision++
	if _, err := app.DB().Insert("leafen_organization_storage_usage", dbx.Params{
		"organization_id": organizationID, "reserved_bytes": state.ReservedBytes,
		"version": 0, "updated_at": now,
	}).Execute(); err != nil {
		return result, errors.Join(errOrganizationStorageStateInvalid, err)
	}
	if _, err := app.DB().Delete("leafen_organization_storage_reconciliation", dbx.HashExp{
		"organization_id": organizationID,
	}).Execute(); err != nil {
		return result, err
	}
	result.Revision = state.Revision
	return result, nil
}

func loadProductOrganizationStorageActiveReservations(
	ctx context.Context,
	app core.App,
	organizationID string,
	afterReservationID string,
	limit int,
) ([]productOrganizationStorageActiveReservation, bool, error) {
	items := []productOrganizationStorageActiveReservation{}
	err := app.DB().NewQuery(`
		SELECT [[reservation]].[[reservation_id]], [[reservation]].[[workspace_id]],
			[[workspace]].[[organization]] AS [[workspace_organization_id]],
			[[reservation]].[[bytes]]
		FROM {{leafen_organization_storage_reservations}} AS [[reservation]]
			INDEXED BY [[idx_leafen_organization_storage_reservations_org_status_id]]
		JOIN {{leafen_workspaces}} AS [[workspace]]
			ON [[workspace]].[[id]]=[[reservation]].[[workspace_id]]
		WHERE [[reservation]].[[organization_id]]={:organizationId}
			AND [[reservation]].[[status]]={:active}
			AND [[reservation]].[[reservation_id]]>{:afterId}
		ORDER BY [[reservation]].[[reservation_id]]
		LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID, "active": organizationStorageReservationActive,
		"afterId": afterReservationID, "limit": limit + 1,
	}).All(&items)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

func applyProductOrganizationStorageActiveReservations(
	organizationID string,
	items []productOrganizationStorageActiveReservation,
	state *storedProductOrganizationStorageReconciliation,
) error {
	for _, item := range items {
		if item.ID == "" || item.WorkspaceID == "" ||
			item.WorkspaceOrganization != organizationID || item.Bytes <= 0 {
			return errOrganizationStorageStateInvalid
		}
		if state.ReservedBytes > int64(^uint64(0)>>1)-item.Bytes {
			return errOrganizationStorageStateInvalid
		}
		state.ReservedBytes += item.Bytes
	}
	return nil
}

func loadProductOrganizationStorageRecoveryFiles(
	ctx context.Context,
	app core.App,
	organizationID string,
	afterWorkspaceID string,
	afterFileID string,
	limit int,
) ([]productOrganizationStorageRecoveryItem, bool, error) {
	items := []productOrganizationStorageRecoveryItem{}
	err := app.DB().NewQuery(`
		SELECT [[file]].[[id]] AS [[reservation_id]], [[file]].[[workspace]] AS [[workspace_id]],
			CAST([[file]].[[size]] AS INTEGER) AS [[bytes]],
			[[reservation]].[[reservation_id]] AS [[existing_id]],
			[[reservation]].[[organization_id]] AS [[existing_organization_id]],
			[[reservation]].[[workspace_id]] AS [[existing_workspace_id]],
			[[reservation]].[[bytes]] AS [[existing_bytes]],
			[[reservation]].[[status]] AS [[existing_status]]
		FROM {{leafen_files}} AS [[file]] INDEXED BY [[idx_leafen_files_workspace_id]]
		JOIN {{leafen_workspaces}} AS [[workspace]]
			ON [[workspace]].[[id]]=[[file]].[[workspace]]
		LEFT JOIN {{leafen_organization_storage_reservations}} AS [[reservation]]
			ON [[reservation]].[[reservation_id]]=[[file]].[[id]]
		WHERE [[workspace]].[[organization]]={:organizationId}
			AND ([[file]].[[workspace]]>{:afterWorkspace}
				OR ([[file]].[[workspace]]={:afterWorkspace} AND [[file]].[[id]]>{:afterId}))
			AND NOT EXISTS (SELECT 1 FROM {{leafen_file_uploads}} AS [[upload]]
				WHERE [[upload]].[[upload_id]]=[[file]].[[id]] OR [[upload]].[[file_id]]=[[file]].[[id]])
		ORDER BY [[file]].[[workspace]],[[file]].[[id]]
		LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID, "afterWorkspace": afterWorkspaceID,
		"afterId": afterFileID, "limit": limit + 1,
	}).All(&items)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

func loadProductOrganizationStorageRecoveryUploads(
	ctx context.Context,
	app core.App,
	organizationID string,
	afterWorkspaceID string,
	afterUploadID string,
	limit int,
) ([]productOrganizationStorageRecoveryItem, bool, error) {
	activeCompleted := ""
	if tableKnown(app, filesCollection) {
		activeCompleted = ` AND ([[upload]].[[status]]!='completed' OR EXISTS (
			SELECT 1 FROM {{leafen_files}} AS [[file]] WHERE [[file]].[[id]]=
				CASE WHEN [[upload]].[[file_id]]='' THEN [[upload]].[[upload_id]] ELSE [[upload]].[[file_id]] END))`
	}
	items := []productOrganizationStorageRecoveryItem{}
	err := app.DB().NewQuery(`
		SELECT [[upload]].[[upload_id]] AS [[reservation_id]],
			[[upload]].[[workspace_id]], [[upload]].[[total_size]] AS [[bytes]],
			[[reservation]].[[reservation_id]] AS [[existing_id]],
			[[reservation]].[[organization_id]] AS [[existing_organization_id]],
			[[reservation]].[[workspace_id]] AS [[existing_workspace_id]],
			[[reservation]].[[bytes]] AS [[existing_bytes]],
			[[reservation]].[[status]] AS [[existing_status]]
		FROM {{leafen_file_uploads}} AS [[upload]]
			INDEXED BY [[idx_leafen_file_uploads_workspace_status_upload]]
		JOIN {{leafen_workspaces}} AS [[workspace]]
			ON [[workspace]].[[id]]=[[upload]].[[workspace_id]]
		LEFT JOIN {{leafen_organization_storage_reservations}} AS [[reservation]]
			ON [[reservation]].[[reservation_id]]=[[upload]].[[upload_id]]
		WHERE [[workspace]].[[organization]]={:organizationId}
			AND [[upload]].[[status]] IN ('pending','settling','completed','cleanup_pending')
			AND ([[upload]].[[workspace_id]]>{:afterWorkspace}
				OR ([[upload]].[[workspace_id]]={:afterWorkspace}
					AND [[upload]].[[upload_id]]>{:afterId}))` + activeCompleted + `
		ORDER BY [[upload]].[[workspace_id]],[[upload]].[[upload_id]]
		LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID, "afterWorkspace": afterWorkspaceID,
		"afterId": afterUploadID, "limit": limit + 1,
	}).All(&items)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

func applyProductOrganizationStorageRecoveryItems(
	ctx context.Context,
	app core.App,
	organizationID string,
	items []productOrganizationStorageRecoveryItem,
	now int64,
	state *storedProductOrganizationStorageReconciliation,
) error {
	missing := make([]productOrganizationStorageRecoveryItem, 0, len(items))
	for _, item := range items {
		if item.ReservationID == "" || item.WorkspaceID == "" || item.Bytes <= 0 {
			return errOrganizationStorageStateInvalid
		}
		if item.ExistingID.Valid {
			if item.ExistingID.String != item.ReservationID ||
				item.ExistingOrganization.String != organizationID ||
				item.ExistingWorkspace.String != item.WorkspaceID ||
				item.ExistingBytes.Int64 != item.Bytes ||
				(item.ExistingStatus.String != organizationStorageReservationActive &&
					item.ExistingStatus.String != organizationStorageReservationReleased) {
				return errOrganizationStorageStateInvalid
			}
		} else {
			missing = append(missing, item)
			if state.ReservedBytes > int64(^uint64(0)>>1)-item.Bytes {
				return errOrganizationStorageStateInvalid
			}
			state.ReservedBytes += item.Bytes
		}
	}
	if len(missing) == 0 {
		return nil
	}
	values := make([]string, 0, len(missing))
	params := dbx.Params{
		"organizationId": organizationID, "active": organizationStorageReservationActive,
		"now": now,
	}
	for index, item := range missing {
		suffix := strconv.Itoa(index)
		params["id"+suffix] = item.ReservationID
		params["workspace"+suffix] = item.WorkspaceID
		params["bytes"+suffix] = item.Bytes
		values = append(values, "({:id"+suffix+"},{:organizationId},{:workspace"+suffix+"},{:bytes"+suffix+"},{:active},{:now},{:now},0)")
	}
	_, err := app.DB().NewQuery(`INSERT INTO {{leafen_organization_storage_reservations}}
		([[reservation_id]],[[organization_id]],[[workspace_id]],[[bytes]],[[status]],[[created_at]],[[updated_at]],[[released_at]])
		VALUES ` + strings.Join(values, ",") + `
		ON CONFLICT ([[reservation_id]]) DO NOTHING`).WithContext(ctx).Bind(params).Execute()
	return err
}

func loadProductOrganizationStorageReconciliation(
	ctx context.Context,
	app core.App,
	organizationID string,
) (storedProductOrganizationStorageReconciliation, error) {
	state := storedProductOrganizationStorageReconciliation{}
	err := app.DB().NewQuery(`SELECT [[organization_id]],[[after_workspace_id]],[[after_upload_id]],
		[[reserved_bytes]],[[revision]] FROM {{leafen_organization_storage_reconciliation}}
		WHERE [[organization_id]]={:organizationId}`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID,
	}).One(&state)
	return state, err
}

func insertProductOrganizationStorageReconciliation(
	ctx context.Context,
	app core.App,
	state storedProductOrganizationStorageReconciliation,
	now int64,
) error {
	_, err := app.DB().Insert("leafen_organization_storage_reconciliation", dbx.Params{
		"organization_id":    state.OrganizationID,
		"after_workspace_id": state.AfterWorkspaceID,
		"after_upload_id":    state.AfterUploadID,
		"reserved_bytes":     state.ReservedBytes,
		"revision":           state.Revision, "updated_at": now,
	}).Execute()
	return err
}

func persistProductOrganizationStorageReconciliation(
	ctx context.Context,
	app core.App,
	state storedProductOrganizationStorageReconciliation,
	now int64,
	result productOrganizationStorageReconciliationResult,
) (productOrganizationStorageReconciliationResult, error) {
	if state.Revision == int64(^uint64(0)>>1) {
		return result, errOrganizationStorageStateInvalid
	}
	state.Revision++
	updated, err := app.DB().NewQuery(`UPDATE {{leafen_organization_storage_reconciliation}}
		SET [[after_workspace_id]]={:afterWorkspace}, [[after_upload_id]]={:afterUpload},
			[[reserved_bytes]]={:reservedBytes}, [[revision]]={:nextRevision}, [[updated_at]]={:now}
		WHERE [[organization_id]]={:organizationId} AND [[revision]]={:revision}`).WithContext(ctx).Bind(dbx.Params{
		"afterWorkspace": state.AfterWorkspaceID, "afterUpload": state.AfterUploadID,
		"reservedBytes": state.ReservedBytes, "nextRevision": state.Revision, "now": now,
		"organizationId": state.OrganizationID, "revision": state.Revision - 1,
	}).Execute()
	if err != nil {
		return result, err
	}
	count, err := updated.RowsAffected()
	if err != nil || count != 1 {
		return result, errors.Join(errOrganizationStorageStateInvalid, err)
	}
	result.HasMore = true
	result.Revision = state.Revision
	return result, nil
}

func validateProductOrganizationStorageReconciliation(
	state storedProductOrganizationStorageReconciliation,
	organizationID string,
) error {
	validPhase := strings.HasPrefix(state.AfterUploadID, organizationStorageReconciliationRows) ||
		strings.HasPrefix(state.AfterUploadID, organizationStorageReconciliationFiles) ||
		strings.HasPrefix(state.AfterUploadID, organizationStorageReconciliationUpload)
	if state.OrganizationID != organizationID || state.ReservedBytes < 0 ||
		state.Revision < 0 || !validPhase {
		return errOrganizationStorageStateInvalid
	}
	return nil
}
