package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const workspaceImportCleanupRetryDelay = 30 * time.Second

func (r *durableJobRunner) applyWorkspaceImportChunk(ctx context.Context, job storedJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := decodeWorkspaceImportPayload(job)
	if err != nil {
		return err
	}
	if payload.CancelRequested && payload.Phase != workspaceImportPhaseCleanup &&
		payload.Phase != workspaceImportPhasePublishCleanup {
		return r.beginWorkspaceImportCleanup(job, payload, "")
	}
	if payload.Format == workspaceImportFormatFileBatch && payload.Phase != workspaceImportPhaseCleanup {
		return r.applyWorkspaceImportBatchTurn(ctx, job, payload)
	}
	if payload.Format == workspaceImportFormatNativeArchiveV2 &&
		payload.Phase == workspaceImportPhaseArchivePrepare {
		return r.prepareWorkspaceArchiveImportChunk(job, payload)
	}
	if payload.Format == workspaceImportFormatNativeArchiveV2 &&
		(payload.Phase == workspaceImportPhaseStaged ||
			(payload.Phase == workspaceImportPhaseApply && !payload.ReadyToPublish)) {
		return r.applyWorkspaceArchiveImportChunk(job, payload)
	}
	switch payload.Phase {
	case workspaceImportPhaseStaged:
		if payload.ReplaceExistingPage {
			return r.snapshotWorkspaceMarkdownReplace(job, payload)
		}
		if payload.AppendToExistingPage {
			return r.prepareWorkspaceMarkdownAppend(job, payload)
		}
		return r.createWorkspaceImportRoot(job, payload)
	case workspaceImportPhaseApply:
		if payload.ReadyToPublish {
			if payload.ReplaceExistingPage {
				return r.beginWorkspaceMarkdownReplaceDelete(job, payload)
			}
			return r.publishWorkspaceImport(job, payload)
		}
		return r.applyWorkspaceImportItems(job, payload)
	case workspaceImportPhaseReplaceDelete:
		return r.deleteWorkspaceMarkdownReplaceSnapshot(job, payload)
	case workspaceImportPhasePublishCleanup:
		return r.cleanupPublishedWorkspaceImportIntoRow(ctx, job, payload)
	case workspaceImportPhaseCleanup:
		return r.cleanupWorkspaceImportOrWait(job, payload)
	default:
		return workspaceImportStateError()
	}
}

func (r *durableJobRunner) createWorkspaceImportRoot(job storedJob, payload workspaceImportJobPayload) error {
	if !payload.SourceVerified {
		source, err := loadStoredWorkspaceImportSource(r.app, job.ID)
		if err != nil {
			return workspaceImportStateError()
		}
		if err := verifyWorkspaceImportSource(r.app.DataDir(), source); err != nil {
			return err
		}
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		payload, err = decodeWorkspaceImportPayload(current)
		if err != nil {
			return err
		}
		if payload.CancelRequested {
			payload.Phase = workspaceImportPhaseCleanup
			payload.CleanupLane = 4
			return r.updateWorkspaceImportPayload(txApp, current, payload, current.Cursor, current.Processed, false)
		}
		payload.SourceVerified = true
		authorization, err := authorizeWorkspace(txApp, current.Owner, current.WorkspaceID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		nextCursor, err := nextWorkspaceUserStateCursor(txApp, authorization.OwnerID)
		if err != nil {
			return err
		}
		pages, err := txApp.FindCollectionByNameOrId(pagesCollection)
		if err != nil {
			return err
		}
		page := core.NewRecord(pages)
		page.Id = newPageID()
		if payload.ReservedRootPageID != "" {
			page.Id = payload.ReservedRootPageID
		}
		page.Set("owner", authorization.OwnerID)
		page.Set("workspace", authorization.WorkspaceID)
		page.Set("title", payload.Title)
		page.Set("content", "")
		page.Set("revision", 0)
		page.Set("cursor", nextCursor)
		if err := placeWorkspaceImportRootUnder(
			txApp, current.Owner, authorization.WorkspaceID, payload.ParentPageID, payload.TeamspaceID, page,
		); err != nil {
			return err
		}
		applyPageTrashState(page, false, "")
		initializePageStyleMetadata(page)
		initializePageCustomizationMetadata(page)
		page.Set("notion_import_staging", true)
		setNewPageCustodianProvenance(page, current.Owner)
		createdAt := r.clock.now().UnixMilli()
		if err := txApp.Save(page); err != nil {
			return err
		}
		if err := recordNotionAdminLegalHoldPageInteraction(txApp, pageCreatedInteraction(
			authorization.Organization.OrganizationID, authorization.WorkspaceID,
			page.Id, current.Owner, createdAt,
		)); err != nil {
			return err
		}
		payload.RootPageID = page.Id
		payload.RootPageIDs = []string{page.Id}
		payload.Phase = workspaceImportPhaseApply
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[page_id]] = {:pageId}, [[payload_json]] = {:payload},
				[[chunk_count]] = [[chunk_count]] + 1, [[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "owner": current.Owner, "pageId": page.Id, "payload": string(encoded),
			"notBefore": now + durableJobReadyDelay.Milliseconds(), "updated": now,
		}).Execute()
		return err
	})
}

// workspaceImportTurnDelay is how long an import waits after a turn before
// its next: long enough for another writer to come in, short enough that the
// wait doesn't set the import's pace (2026-10-14: at 250 ms an import of
// 50 items a turn spent most of its time waiting).
const workspaceImportTurnDelay = 20 * time.Millisecond

func (r *durableJobRunner) updateWorkspaceImportPayload(app core.App, job storedJob, payload workspaceImportJobPayload, cursor string, processed int, countTurn bool) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := r.clock.now().UnixMilli()
	chunkIncrement := 0
	if countTurn {
		chunkIncrement = 1
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[payload_json]] = {:payload}, [[cursor]] = {:cursor}, [[processed_items]] = {:processed},
			[[chunk_count]] = [[chunk_count]] + {:chunkIncrement}, [[not_before]] = {:notBefore}, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
	`).Bind(dbx.Params{
		"id": job.ID, "owner": job.Owner, "payload": string(encoded), "cursor": cursor,
		"processed": processed, "chunkIncrement": chunkIncrement,
		"notBefore": now + workspaceImportTurnDelay.Milliseconds(), "updated": now,
	}).Execute()
	return err
}

func (r *durableJobRunner) publishWorkspaceImport(job storedJob, payload workspaceImportJobPayload) error {
	if _, err := loadStoredWorkspaceImportSource(r.app, job.ID); err != nil {
		return workspaceImportStateError()
	}
	err := r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		payload, err = decodeWorkspaceImportPayload(current)
		if err != nil || !payload.ReadyToPublish || current.Processed != current.Total {
			if err != nil {
				return err
			}
			if payload.Format == workspaceImportFormatNativeArchiveV2 {
				return workspaceImportStateErrorAt("archive-publish-readiness")
			}
			return workspaceImportStateError()
		}
		if payload.CancelRequested {
			payload.Phase = workspaceImportPhaseCleanup
			payload.CleanupLane = 4
			return r.updateWorkspaceImportPayload(txApp, current, payload, current.Cursor, current.Processed, false)
		}
		if payload.Format == workspaceImportFormatNativeArchiveV2 {
			err = r.publishWorkspaceArchiveImport(txApp, current, &payload)
			if err != nil {
				return err
			}
		} else {
			rootPageID := workspaceImportPrimaryRootPageID(payload)
			if payload.AppendToExistingPage {
				if _, err := authorizeWorkspaceImportTarget(txApp, current.Owner, current.WorkspaceID, rootPageID); err != nil {
					return err
				}
				if payload.FillsPlace {
					if err := nameWorkspaceImportFilledPage(txApp, current, payload); err != nil {
						return err
					}
				}
			} else {
				authorization, err := authorizeWorkspace(txApp, current.Owner, current.WorkspaceID, permissionWorkspaceWrite)
				if err != nil {
					return err
				}
				page, err := txApp.FindRecordById(pagesCollection, rootPageID)
				if err != nil || page.GetString("workspace") != authorization.WorkspaceID ||
					page.GetString("owner") != authorization.OwnerID || !page.GetBool("notion_import_staging") {
					return workspaceImportStateError()
				}
				page.Set("notion_import_staging", false)
				if err := txApp.Save(page); err != nil {
					return err
				}
				if err := requestChildPageBlockBackfill(
					txApp, current.WorkspaceID, page.GetString("parent_id"), page.Id,
				); err != nil {
					return err
				}
				if err := publishWorkspaceImportRootPlacement(
					txApp, current.Owner, authorization.OwnerID, current.WorkspaceID, page,
				); err != nil {
					return err
				}
				if err := markSearchDirty(txApp, page); err != nil {
					return err
				}
			}
		}
		payload.Phase = workspaceImportPhasePublishCleanup
		payload.TerminalErrorCode, payload.TerminalErrorDetail = "", ""
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[page_id]] = {:pageId}, [[payload_json]] = {:payload},
				[[error_code]] = '', [[diagnostic_reference]] = '',
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "owner": current.Owner,
			"pageId": workspaceImportPrimaryRootPageID(payload), "payload": string(encoded),
			"notBefore": now + durableJobReadyDelay.Milliseconds(), "updated": now,
		}).Execute()
		return err
	})
	return workspaceImportPublicationRetry(err)
}

func (r *durableJobRunner) cleanupPublishedWorkspaceImport(job storedJob, payload workspaceImportJobPayload) error {
	// A moved workspace's pages' history goes in once its pages are in.
	if payload.History != nil && !payload.History.Done {
		return r.applyWorkspaceMoveHistoryTurn(job, payload)
	}
	// Then who could open what, once (workspace_move_access.go).
	if payload.MoveAccess != nil && !payload.MoveAccess.Applied {
		return r.applyWorkspaceMoveAccessTurn(job, payload)
	}
	source, err := loadStoredWorkspaceImportSource(r.app, job.ID)
	if err != nil {
		return workspaceImportStateErrorAt("publish-cleanup-source")
	}
	keys := []string{source.StorageKey, source.ArchiveStorageKey, source.FileIndexStorageKey}
	if payload.History != nil {
		keys = append(keys, payload.History.StorageKey)
		for _, file := range payload.History.Files {
			keys = append(keys, file.StorageKey)
		}
	}
	for _, key := range keys {
		if key != "" {
			if err := removeWorkspaceImportSourceFile(r.app.DataDir(), key); err != nil {
				return err
			}
		}
	}
	if err := removeWorkspaceImportArchivePreparationFiles(r.app.DataDir(), job.ID); err != nil {
		return err
	}
	err = r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		payload, err = decodeWorkspaceImportPayload(current)
		if err != nil || payload.Phase != workspaceImportPhasePublishCleanup ||
			!payload.ReadyToPublish || current.Processed != current.Total {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("publish-cleanup-readiness")
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_workspace_import_sources}} WHERE [[job_id]] = {:jobId}
		`).Bind(dbx.Params{"jobId": current.ID}).Execute(); err != nil {
			return err
		}
		payload.Phase = workspaceImportPhaseCompleted
		payload.TerminalErrorCode, payload.TerminalErrorDetail = "", ""
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = 'completed', [[payload_json]] = {:payload},
				[[error_code]] = '', [[diagnostic_reference]] = '', [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "owner": current.Owner, "payload": string(encoded), "updated": now,
		}).Execute(); err != nil {
			return err
		}
		return cleanupWorkspaceImportTerminalHistory(txApp, current.WorkspaceID)
	})
	return err
}

func (r *durableJobRunner) beginWorkspaceImportCleanup(job storedJob, payload workspaceImportJobPayload, code string) error {
	payload.Phase = workspaceImportPhaseCleanup
	payload.CleanupLane = 4
	if code != "" {
		payload.TerminalErrorCode = code
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		return r.updateWorkspaceImportPayload(txApp, current, payload, current.Cursor, current.Processed, false)
	})
}

func (r *durableJobRunner) cleanupWorkspaceImport(job storedJob, payload workspaceImportJobPayload) error {
	if payload.Format == workspaceImportFormatNativeArchiveV2 {
		return r.cleanupWorkspaceArchiveImport(job, payload)
	}
	if payload.Format == workspaceImportFormatFileBatch {
		return r.cleanupWorkspaceImportBatch(job, payload)
	}
	if payload.AppendToExistingPage {
		return r.cleanupWorkspaceMarkdownAppend(job, payload)
	}
	if payload.ReplaceExistingPage {
		return r.cleanupWorkspaceMarkdownReplace(job, payload)
	}
	rootPageID := workspaceImportPrimaryRootPageID(payload)
	if rootPageID != "" && payload.CleanupLane < pagePermanentDeleteCleanupLaneCount {
		return r.app.RunInTransaction(func(txApp core.App) error {
			current, err := loadStoredJob(txApp, job.ID, job.Owner)
			if err != nil || current.Status != durableJobStatusQueued {
				return err
			}
			payload, err = decodeWorkspaceImportPayload(current)
			if err != nil {
				return err
			}
			more, err := cleanPagePermanentDeleteResidueLane(txApp, rootPageID, payload.CleanupLane)
			if err != nil {
				return err
			}
			if !more {
				payload.CleanupLane++
			}
			return r.updateWorkspaceImportPayload(txApp, current, payload, current.Cursor, current.Processed, true)
		})
	}
	source, err := loadStoredWorkspaceImportSource(r.app, job.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if err := removeWorkspaceImportSourceFile(r.app.DataDir(), source.StorageKey); err != nil {
			return err
		}
	}
	if payload.History != nil {
		if err := removeWorkspaceImportSourceFile(r.app.DataDir(), payload.History.StorageKey); err != nil {
			return err
		}
	}
	return r.finishWorkspaceImportCleanup(job, payload)
}

func (r *durableJobRunner) finishWorkspaceImportCleanup(job storedJob, payload workspaceImportJobPayload) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		payload, err = decodeWorkspaceImportPayload(current)
		if err != nil {
			return err
		}
		rootPageID := workspaceImportPrimaryRootPageID(payload)
		if rootPageID != "" {
			page, pageErr := txApp.FindRecordById(pagesCollection, rootPageID)
			if pageErr == nil {
				if !page.GetBool("notion_import_staging") {
					return workspaceImportStateError()
				}
				if err := txApp.Delete(page); err != nil {
					return err
				}
			} else if !errors.Is(pageErr, sql.ErrNoRows) {
				return pageErr
			}
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_workspace_import_sources}} WHERE [[job_id]] = {:jobId}`).Bind(dbx.Params{"jobId": current.ID}).Execute(); err != nil {
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
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = {:status}, [[page_id]] = '', [[payload_json]] = {:payload},
				[[error_code]] = {:errorCode}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "owner": current.Owner, "status": status, "payload": string(encoded),
			"errorCode": payload.TerminalErrorCode, "updated": now,
		}).Execute()
		if err != nil {
			return err
		}
		return cleanupWorkspaceImportTerminalHistory(txApp, current.WorkspaceID)
	})
}

func (r *durableJobRunner) scheduleWorkspaceImportFailure(
	app core.App, jobID, reference, code, detail string,
	retryPublication bool,
) (bool, error) {
	handled := false
	err := app.RunInTransaction(func(txApp core.App) error {
		job, err := loadStoredJob(txApp, jobID, "")
		if err != nil || job.Kind != workspaceImportJobKind || job.Status != durableJobStatusQueued {
			return err
		}
		handled = true
		payload, decodeErr := decodeWorkspaceImportPayload(job)
		if decodeErr != nil {
			appendTarget := false
			if job.PageID != "" {
				if page, pageErr := txApp.FindRecordById(pagesCollection, job.PageID); pageErr == nil {
					appendTarget = !page.GetBool("notion_import_staging")
				}
			}
			payloadVersion := 1
			if appendTarget {
				payloadVersion = 2
			}
			payload = workspaceImportJobPayload{
				SchemaVersion: payloadVersion, Format: workspaceImportFormatMarkdown, Title: "corrupt import",
				RootPageID: job.PageID, RootPageIDs: []string{job.PageID}, AppendToExistingPage: appendTarget,
			}
		}
		retryDelay := durableJobReadyDelay
		preservePublication := retryPublication && payload.Phase == workspaceImportPhaseApply &&
			payload.ReadyToPublish && job.Processed == job.Total
		// An ended import's cleanup keeps why it ended; its own failure is only tried again.
		if payload.Phase == workspaceImportPhaseCleanup && payload.TerminalErrorCode != "" {
			code, detail = payload.TerminalErrorCode, payload.TerminalErrorDetail
		}
		if payload.Phase == workspaceImportPhaseCleanup || payload.Phase == workspaceImportPhasePublishCleanup {
			retryDelay = workspaceImportCleanupRetryDelay
		} else if !preservePublication {
			payload.Phase = workspaceImportPhaseCleanup
			payload.CleanupLane = 4
		}
		if !payload.CancelRequested {
			payload.TerminalErrorCode, payload.TerminalErrorDetail = code, detail
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[error_code]] = {:errorCode},
				[[diagnostic_reference]] = {:reference},
				[[attempt]] = CASE WHEN [[attempt]] < [[max_attempts]] THEN [[attempt]] + 1 ELSE [[attempt]] END,
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": job.ID, "payload": string(encoded), "errorCode": code, "reference": reference,
			"notBefore": now + retryDelay.Milliseconds(), "updated": now,
		}).Execute()
		return err
	})
	return handled, err
}
