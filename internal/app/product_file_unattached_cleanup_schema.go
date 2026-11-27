package app

import (
	"context"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const productFileUploadsCompletedFileIndex = "idx_leafen_file_uploads_completed_file"

type productFileNeverAttachedBackfillResult struct {
	Processed int
	HasMore   bool
}

type storedProductFileNeverAttachedCandidate struct {
	FileID      string `db:"file_id"`
	WorkspaceID string `db:"workspace_id"`
	CompletedAt int64  `db:"completed_at"`
}

func productFileNeverAttachedCleanupDueAt(completedAt int64) int64 {
	seconds := fileUnattachedRetentionSeconds + fileReferenceDeleteGraceSeconds
	return completedAt + int64(seconds)*int64(time.Second/time.Millisecond)
}

func scheduleProductFileNeverAttachedCleanup(
	ctx context.Context,
	app core.App,
	fileID, workspaceID string,
	completedAt int64,
) (bool, error) {
	if fileID == "" || workspaceID == "" || completedAt <= 0 {
		return false, errFileUploadUnavailable
	}
	var authoritative int
	if err := app.DB().NewQuery(`SELECT EXISTS(
		SELECT 1 FROM {{leafen_files}}
		WHERE [[id]]={:fileId} AND [[workspace]]={:workspaceId}
	)`).WithContext(ctx).Bind(dbx.Params{
		"fileId": fileID, "workspaceId": workspaceID,
	}).Row(&authoritative); err != nil {
		return false, err
	}
	if authoritative != 1 {
		return false, errFileUploadUnavailable
	}
	referenced, err := findActiveProductFileReference(app, fileID)
	if err != nil || referenced {
		return false, err
	}
	pendingDelete, err := productFileDeletionPending(app, fileID)
	if err != nil || pendingDelete {
		return false, err
	}
	due := productFileNeverAttachedCleanupDueAt(completedAt)
	result, err := app.DB().NewQuery(`INSERT INTO {{leafen_file_reference_cleanups}}
		([[file_id]],[[workspace_id]],[[status]],[[cleanup_due_at]],
		 [[cleanup_attempts]],[[cleanup_error_code]],[[created_at]],[[updated_at]])
		VALUES ({:fileId},{:workspaceId},{:pending},{:due},0,'',{:completedAt},{:completedAt})
		ON CONFLICT ([[file_id]]) DO NOTHING`).WithContext(ctx).Bind(dbx.Params{
		"fileId": fileID, "workspaceId": workspaceID,
		"pending": fileReferenceCleanupStatusPending,
		"due":     due, "completedAt": completedAt,
	}).Execute()
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func backfillProductFileNeverAttachedCleanups(
	ctx context.Context,
	app core.App,
	limit int,
) (productFileNeverAttachedBackfillResult, error) {
	result := productFileNeverAttachedBackfillResult{}
	available, err := productFileUnattachedCleanupSchemaAvailable(ctx, app)
	if err != nil || !available {
		return result, err
	}
	if limit <= 0 || limit > fileReferenceCleanupTurnLimit {
		limit = fileReferenceCleanupTurnLimit
	}
	candidates := []storedProductFileNeverAttachedCandidate{}
	err = app.DB().NewQuery(`SELECT [[upload]].[[file_id]],
		[[upload]].[[workspace_id]],[[upload]].[[completed_at]]
		FROM {{leafen_file_uploads}} AS [[upload]]
		INDEXED BY [[idx_leafen_file_uploads_completed_file]]
		JOIN {{leafen_files}} AS [[file]]
			ON [[file]].[[id]]=[[upload]].[[file_id]]
			AND [[file]].[[workspace]]=[[upload]].[[workspace_id]]
		WHERE [[upload]].[[status]]={:completed}
			AND [[upload]].[[completed_at]]>0 AND [[upload]].[[file_id]]!=''
			AND NOT EXISTS (SELECT 1 FROM {{leafen_file_references}} AS [[reference]]
				WHERE [[reference]].[[file_id]]=[[upload]].[[file_id]])
			AND NOT EXISTS (SELECT 1 FROM {{leafen_custom_emojis}} AS [[emoji]]
				WHERE [[emoji]].[[file_id]]=[[upload]].[[file_id]])
			AND NOT EXISTS (SELECT 1 FROM {{leafen_file_delete_receipts}} AS [[deletion]]
				WHERE [[deletion]].[[file_id]]=[[upload]].[[file_id]]
				AND [[deletion]].[[status]]={:deletePending})
			AND NOT EXISTS (SELECT 1 FROM {{leafen_file_reference_cleanups}} AS [[cleanup]]
				WHERE [[cleanup]].[[file_id]]=[[upload]].[[file_id]])
		ORDER BY [[upload]].[[completed_at]],[[upload]].[[file_id]]
		LIMIT {:candidateLimit}`).WithContext(ctx).Bind(dbx.Params{
		"completed":      fileUploadStatusCompleted,
		"deletePending":  fileDeleteStatusPending,
		"candidateLimit": limit + 1,
	}).All(&candidates)
	if err != nil {
		return result, err
	}
	result.HasMore = len(candidates) > limit
	if result.HasMore {
		candidates = candidates[:limit]
	}
	for _, candidate := range candidates {
		scheduled, err := scheduleProductFileNeverAttachedCleanup(
			ctx, app, candidate.FileID, candidate.WorkspaceID, candidate.CompletedAt,
		)
		if err != nil {
			return result, err
		}
		if scheduled {
			result.Processed++
		}
	}
	return result, nil
}

func productFileUnattachedCleanupSchemaAvailable(
	ctx context.Context,
	app core.App,
) (bool, error) {
	var tables, indexes int
	if err := app.DB().NewQuery(`SELECT
		EXISTS(SELECT 1 FROM [[sqlite_master]] WHERE [[type]]='table'
			AND [[name]]='leafen_file_uploads'),
		EXISTS(SELECT 1 FROM [[sqlite_master]] WHERE [[type]]='index'
			AND [[name]]='idx_leafen_file_uploads_completed_file')
	`).WithContext(ctx).Row(&tables, &indexes); err != nil {
		return false, err
	}
	if tables == 0 {
		return false, nil
	}
	if indexes != 1 {
		return false, errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return true, nil
}
