package app

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func (r *durableJobRunner) cleanupWorkspaceArchiveImport(job storedJob, payload workspaceImportJobPayload) error {
	if payload.ArchivePrepareUpload {
		if err := ensureWorkspaceArchiveUploadCleanup(r.app, job.Owner, job.ID); err != nil {
			return err
		}
	}
	_ = os.Remove(workspaceImportArchiveAssemblyPath(
		filepath.Join(r.app.DataDir(), workspaceImportDirectory), job.ID,
	))
	if err := removeWorkspaceImportArchivePreparationFiles(r.app.DataDir(), job.ID); err != nil {
		return err
	}
	var page struct {
		PageID string `db:"page_id"`
	}
	err := r.app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_workspace_archive_import_pages}}
		WHERE [[job_id]] = {:jobId} ORDER BY [[sequence]] LIMIT 1
	`).Bind(dbx.Params{"jobId": job.ID}).One(&page)
	if err == nil {
		return r.cleanupWorkspaceArchiveImportPage(job, payload, page.PageID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			DELETE FROM {{leafen_custom_emojis}}
			WHERE [[workspace_import_job_id]]={:jobId} AND [[workspace_id]]={:workspaceId}
		`).Bind(dbx.Params{
			"jobId": current.ID, "workspaceId": current.WorkspaceID,
		}).Execute()
		if err != nil {
			return err
		}
		return deleteWorkspaceArchiveImportTeamspaces(txApp, current.ID, current.WorkspaceID)
	}); err != nil {
		return err
	}
	source, sourceErr := loadStoredWorkspaceImportSource(r.app, job.ID)
	if sourceErr != nil && !errors.Is(sourceErr, sql.ErrNoRows) {
		return sourceErr
	}
	if sourceErr == nil {
		if payload.ArchiveCleanupFileIndex < source.FileCount {
			return r.cleanupWorkspaceArchiveImportFile(job, payload, source)
		}
		for _, key := range []string{
			source.StorageKey, source.ArchiveStorageKey, source.FileIndexStorageKey,
		} {
			if err := removeWorkspaceImportSourceFile(r.app.DataDir(), key); err != nil {
				return err
			}
		}
	}
	return r.finishWorkspaceArchiveImportCleanup(job, payload)
}

func removeWorkspaceImportArchivePreparationFiles(appDir, jobID string) error {
	for _, key := range []string{
		workspaceImportArchivePreparationMapKey(jobID),
		workspaceImportArchivePreparationMapTemporaryKey(jobID),
		"." + jobID + workspaceImportArchivePreparationOutputSuffix,
		"." + jobID + workspaceImportArchivePreparationIndexSuffix,
	} {
		if err := removeWorkspaceImportSourceFile(appDir, key); err != nil {
			return err
		}
	}
	return nil
}

func (r *durableJobRunner) cleanupWorkspaceArchiveImportPage(
	job storedJob,
	payload workspaceImportJobPayload,
	pageID string,
) error {
	if payload.CleanupLane < pagePermanentDeleteCleanupLaneCount {
		return r.app.RunInTransaction(func(txApp core.App) error {
			current, err := loadStoredJob(txApp, job.ID, job.Owner)
			if err != nil || current.Status != durableJobStatusQueued {
				return err
			}
			payload, err = decodeWorkspaceImportPayload(current)
			if err != nil {
				return err
			}
			more, err := cleanPagePermanentDeleteResidueLane(txApp, pageID, payload.CleanupLane)
			if err != nil {
				return err
			}
			if !more {
				payload.CleanupLane++
			}
			return r.updateWorkspaceImportPayload(txApp, current, payload, current.Cursor, current.Processed, true)
		})
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		storedPage, pageErr := txApp.FindRecordById(pagesCollection, pageID)
		if pageErr == nil {
			if !storedPage.GetBool("notion_import_staging") || storedPage.GetString("workspace") != current.WorkspaceID {
				return workspaceImportStateError()
			}
			if err := txApp.Delete(storedPage); err != nil {
				return err
			}
		} else if !errors.Is(pageErr, sql.ErrNoRows) {
			return pageErr
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_workspace_archive_import_pages}}
			WHERE [[job_id]] = {:jobId} AND [[page_id]] = {:pageId}
		`).Bind(dbx.Params{"jobId": current.ID, "pageId": pageID}).Execute(); err != nil {
			return err
		}
		payload, err = decodeWorkspaceImportPayload(current)
		if err != nil {
			return err
		}
		payload.CleanupLane = 4
		return r.updateWorkspaceImportPayload(txApp, current, payload, current.Cursor, current.Processed, true)
	})
}

func (r *durableJobRunner) cleanupWorkspaceArchiveImportFile(
	job storedJob,
	payload workspaceImportJobPayload,
	source storedWorkspaceImportSource,
) error {
	frontier := payload
	frontier.ArchiveFileIndex = payload.ArchiveCleanupFileIndex
	frontier.ArchiveFileCursor = payload.ArchiveCleanupFileCursor
	frontier.ArchiveFileChain = payload.ArchiveCleanupFileChain
	record, nextCursor, err := loadWorkspaceImportArchiveFile(r.app.DataDir(), source, frontier)
	if err != nil {
		return err
	}
	release := acquireProductFileOperationLock(productFileOperationReferenceCleanup, record.TargetID)
	defer release()
	stored, findErr := r.app.FindRecordById(filesCollection, record.TargetID)
	if findErr == nil {
		if !workspaceFileRecordBelongsTo(stored, job.WorkspaceID) {
			return workspaceImportStateErrorAt("archive-file-cleanup-owner")
		}
		referenced, err := findActiveProductFileReference(r.app, record.TargetID)
		if err != nil || referenced {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("archive-file-cleanup-reference")
		}
		if err := deleteProductFileStorage(r.app, stored); err != nil {
			return workspaceImportFailureAt(err, "archive-file-cleanup-storage")
		}
	} else if !errors.Is(findErr, sql.ErrNoRows) {
		return findErr
	}
	return r.advanceWorkspaceArchiveImportFileCleanup(job, payload, record, nextCursor)
}

func (r *durableJobRunner) advanceWorkspaceArchiveImportFileCleanup(
	job storedJob,
	payload workspaceImportJobPayload,
	record workspaceImportArchiveFile,
	nextCursor int64,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.ArchiveCleanupFileIndex != payload.ArchiveCleanupFileIndex ||
			currentPayload.ArchiveCleanupFileCursor != payload.ArchiveCleanupFileCursor ||
			currentPayload.ArchiveCleanupFileChain != payload.ArchiveCleanupFileChain {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("archive-file-cleanup-frontier")
		}
		referenced, err := findActiveProductFileReference(txApp, record.TargetID)
		if err != nil || referenced {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("archive-file-cleanup-reference")
		}
		stored, findErr := txApp.FindRecordById(filesCollection, record.TargetID)
		if findErr == nil {
			if !workspaceFileRecordBelongsTo(stored, current.WorkspaceID) {
				return workspaceImportStateErrorAt("archive-file-cleanup-owner")
			}
			if err := txApp.Delete(stored); err != nil {
				return err
			}
		} else if !errors.Is(findErr, sql.ErrNoRows) {
			return findErr
		}
		reservation, reservationErr := loadProductOrganizationStorageReservation(
			context.Background(), txApp, record.TargetID,
		)
		if reservationErr == nil {
			if reservation.WorkspaceID != current.WorkspaceID {
				return workspaceImportStateErrorAt("archive-file-cleanup-quota-owner")
			}
			if err := releaseProductOrganizationStorage(
				context.Background(), txApp, record.TargetID, r.clock.now().UTC().UnixMilli(),
			); err != nil {
				return err
			}
		} else if !errors.Is(reservationErr, sql.ErrNoRows) {
			return reservationErr
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_file_reference_cleanups}} WHERE [[file_id]] = {:fileId}
		`).Bind(dbx.Params{"fileId": record.TargetID}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_file_uploads}}
			WHERE [[upload_id]] = {:fileId} AND [[workspace_id]] = {:workspaceId}
		`).Bind(dbx.Params{
			"fileId": record.TargetID, "workspaceId": current.WorkspaceID,
		}).Execute(); err != nil {
			return err
		}
		currentPayload.ArchiveCleanupFileIndex++
		currentPayload.ArchiveCleanupFileCursor = nextCursor
		currentPayload.ArchiveCleanupFileChain = record.ChainSHA256
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, current.Cursor, current.Processed, true,
		)
	})
}

func (r *durableJobRunner) finishWorkspaceArchiveImportCleanup(job storedJob, payload workspaceImportJobPayload) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		payload, err = decodeWorkspaceImportPayload(current)
		if err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_workspace_import_sources}} WHERE [[job_id]] = {:jobId}`).Bind(dbx.Params{"jobId": current.ID}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_workspace_archive_import_targets}} WHERE [[job_id]] = {:jobId}`).Bind(dbx.Params{"jobId": current.ID}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_workspace_archive_import_task_settings}} WHERE [[job_id]] = {:jobId}`).Bind(dbx.Params{"jobId": current.ID}).Execute(); err != nil {
			return err
		}
		status := durableJobStatusCanceled
		payload.Phase = workspaceImportPhaseCancelled
		if payload.TerminalErrorCode != "" {
			status = durableJobStatusFailed
			payload.Phase = workspaceImportPhaseFailed
		}
		payload.RootPageID = ""
		payload.RootPageIDs = nil
		encoded, err := encodeWorkspaceImportPayload(payload)
		if err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = {:status}, [[page_id]] = '', [[payload_json]] = {:payload},
				[[error_code]] = {:errorCode}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "owner": current.Owner, "status": status,
			"payload": encoded, "errorCode": payload.TerminalErrorCode, "updated": now,
		}).Execute(); err != nil {
			return err
		}
		return cleanupWorkspaceImportTerminalHistory(txApp, current.WorkspaceID)
	})
}
