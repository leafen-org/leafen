package app

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedProductFileReferenceCleanup struct {
	FileID           string `db:"file_id"`
	WorkspaceID      string `db:"workspace_id"`
	Status           string `db:"status"`
	CleanupDueAt     int64  `db:"cleanup_due_at"`
	CleanupAttempts  int    `db:"cleanup_attempts"`
	CleanupErrorCode string `db:"cleanup_error_code"`
}

func reconcileProductFileReferenceLifecycle(app core.App, workspaceID string, fileIDs []string) error {
	unique := map[string]struct{}{}
	for _, fileID := range fileIDs {
		if fileID != "" {
			unique[fileID] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(unique))
	for fileID := range unique {
		ordered = append(ordered, fileID)
	}
	sort.Strings(ordered)
	now := time.Now().UTC().UnixMilli()
	due := now + int64(fileReferenceDeleteGraceSeconds)*int64(time.Second/time.Millisecond)
	scheduled := false
	for _, fileID := range ordered {
		var current storedProductFileReferenceCleanup
		currentErr := app.DB().NewQuery(`SELECT [[file_id]],[[workspace_id]],[[status]],[[cleanup_due_at]],[[cleanup_attempts]],[[cleanup_error_code]]
			FROM {{leafen_file_reference_cleanups}} WHERE [[file_id]]={:fileId}`).Bind(dbx.Params{"fileId": fileID}).One(&current)
		if currentErr != nil && !errors.Is(currentErr, sql.ErrNoRows) {
			return currentErr
		}
		referenced, err := findActiveProductFileReference(app, fileID)
		if err != nil {
			return err
		}
		if referenced {
			if currentErr == nil && current.Status == fileReferenceCleanupStatusCleanupPending {
				return errFileDeleteUnavailable
			}
			if _, err := app.DB().Delete(productFileReferenceCleanupsTable, dbx.HashExp{"file_id": fileID, "status": fileReferenceCleanupStatusPending}).Execute(); err != nil {
				return err
			}
			continue
		}
		record, err := app.FindRecordById(filesCollection, fileID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if !workspaceFileRecordBelongsTo(record, workspaceID) {
			return errFileReferenced
		}
		if currentErr == nil && current.Status == fileReferenceCleanupStatusCleanupPending {
			return errFileDeleteUnavailable
		}
		_, err = app.DB().NewQuery(`INSERT INTO {{leafen_file_reference_cleanups}}
			([[file_id]],[[workspace_id]],[[status]],[[cleanup_due_at]],[[cleanup_attempts]],[[cleanup_error_code]],[[created_at]],[[updated_at]])
			VALUES ({:fileId},{:workspaceId},{:pending},{:due},0,'',{:now},{:now})
			ON CONFLICT ([[file_id]]) DO UPDATE SET [[workspace_id]]=excluded.[[workspace_id]],
				[[status]]=excluded.[[status]],[[cleanup_due_at]]=excluded.[[cleanup_due_at]],
				[[cleanup_attempts]]=0,[[cleanup_error_code]]='',[[updated_at]]=excluded.[[updated_at]]
			WHERE {{leafen_file_reference_cleanups}}.[[status]]={:pending}`).Bind(dbx.Params{"fileId": fileID, "workspaceId": workspaceID, "pending": fileReferenceCleanupStatusPending, "due": due, "now": now}).Execute()
		if err != nil {
			return err
		}
		scheduled = true
	}
	if scheduled {
		notifyProductFileReferenceCleanupAfterCommit(app)
	}
	return nil
}

func cancelProductFileReferenceCleanup(ctx context.Context, app core.App, fileID string) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_file_reference_cleanups}}
		WHERE [[file_id]]={:fileId} AND [[status]]={:pending}`).WithContext(ctx).Bind(dbx.Params{"fileId": fileID, "pending": fileReferenceCleanupStatusPending}).Execute()
	return err
}
