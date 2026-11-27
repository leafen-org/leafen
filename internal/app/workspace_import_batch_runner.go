package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A batch's job has each document go in by the same import job a single file
// has, its page and folders made right before: a few at a time, each ending
// on its own, so one that fails leaves the others going. A person's edits come
// first (write_lane.go), and the batch waits between its turns.

const (
	workspaceImportBatchPagesPerTurn      = 25
	workspaceImportBatchDocumentsAtOnce   = 8
	workspaceImportBatchTurnDelay         = 100 * time.Millisecond
	workspaceImportBatchBusyDelay         = 5 * time.Second
	workspaceImportBatchBusyTurnsPerStuck = 12
)

func (r *durableJobRunner) applyWorkspaceImportBatchTurn(ctx context.Context, job storedJob, payload workspaceImportJobPayload) error {
	var err error
	switch payload.Phase {
	case workspaceImportPhaseBatchUpload:
		return r.closeIdleWorkspaceImportBatch(job, payload)
	case workspaceImportPhaseBatchPages:
		err = r.makeWorkspaceImportBatchPages(job)
	case workspaceImportPhaseBatchDocuments:
		err = r.advanceWorkspaceImportBatchDocuments(ctx, job, payload)
	default:
		return workspaceImportStateErrorAt("batch-phase")
	}
	if err != nil && ctx.Err() == nil && workspaceImportBatchMayWait(err) {
		// The database or disk being busy doesn't end the batch.
		return r.scheduleWorkspaceImportBatch(job, workspaceImportBatchBusyDelay, nil)
	}
	return err
}

func workspaceImportBatchMayWait(err error) bool {
	code := stableWorkspaceImportFailureCode(err)
	return code == errorCodeWorkspaceImportDatabaseBusy || code == errorCodeWorkspaceImportStorageUnavailable
}

// closeIdleWorkspaceImportBatch ends a batch whose files stopped coming.
func (r *durableJobRunner) closeIdleWorkspaceImportBatch(job storedJob, payload workspaceImportJobPayload) error {
	var notBefore int64
	if err := r.app.DB().NewQuery(`SELECT [[not_before]] FROM {{leafen_jobs}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": job.ID}).Row(&notBefore); err != nil {
		return err
	}
	if notBefore > r.clock.now().UnixMilli() {
		return nil
	}
	return r.beginWorkspaceImportCleanup(job, payload, "")
}

// scheduleWorkspaceImportBatch writes what a turn changed on the batch as it
// is now, a cancel asked for meanwhile included, and when its next turn is.
func (r *durableJobRunner) scheduleWorkspaceImportBatch(
	job storedJob,
	delay time.Duration,
	change func(txApp core.App, payload *workspaceImportJobPayload) error,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		payload, err := decodeWorkspaceImportPayload(current)
		if err != nil {
			return err
		}
		if change != nil {
			if err := change(txApp, &payload); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[chunk_count]] = [[chunk_count]] + 1,
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": job.ID, "payload": string(encoded), "updated": now, "notBefore": now + delay.Milliseconds(),
		}).Execute()
		return err
	})
}

// makeWorkspaceImportBatchPages ends the step before the documents. A batch's
// pages are made as its documents go in, a few at a time, with the IDs their
// links name, so a batch cancelled or stopped leaves no empty pages for the
// files it didn't bring in (2026-11-06: cancelled at 0 of 600, the 600 pages
// made first stayed, empty, and the window said nothing came in).
func (r *durableJobRunner) makeWorkspaceImportBatchPages(job storedJob) error {
	return r.scheduleWorkspaceImportBatch(job, 0, func(txApp core.App, current *workspaceImportJobPayload) error {
		if current.Phase != workspaceImportPhaseBatchPages {
			return nil
		}
		if err := fillWorkspaceImportBatchPlace(txApp, job.ID, *current); err != nil {
			return err
		}
		var root string
		err := txApp.DB().NewQuery(`
			SELECT [[page_id]] FROM {{leafen_workspace_import_batch_items}}
			WHERE [[job_id]] = {:jobId} AND [[parent_position]] = -1 AND [[kind]] <> 'skipped'
			ORDER BY [[position]] LIMIT 1
		`).Bind(dbx.Params{"jobId": job.ID}).Row(&root)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		current.RootPageID, current.RootPageIDs = root, nil
		if root != "" {
			current.RootPageIDs = []string{root}
		}
		current.Phase = workspaceImportPhaseBatchDocuments
		return nil
	})
}

const workspaceImportBatchItemColumns = `[[position]], [[parent_position]], [[kind]], [[turn]], [[name]], [[title]], [[page_id]],
	[[page_ready]], [[state]], [[format]], [[storage_key]], [[storage_offset]], [[storage_bytes]], [[sha256]],
	[[child_job_id]], [[error_code]], [[message]], [[details]]`

func loadWorkspaceImportBatchItemsInState(app core.App, jobID, state string, limit int) ([]storedWorkspaceImportBatchItem, error) {
	rows := []storedWorkspaceImportBatchItem{}
	err := app.DB().NewQuery(`
		SELECT ` + workspaceImportBatchItemColumns + ` FROM {{leafen_workspace_import_batch_items}}
		WHERE [[job_id]] = {:jobId} AND [[state]] = {:state}
		ORDER BY [[turn]], [[position]] LIMIT {:limit}
	`).Bind(dbx.Params{"jobId": jobID, "state": state, "limit": limit}).All(&rows)
	return rows, err
}

// settleWorkspaceImportBatchChildren records the documents whose imports
// ended, whose job records then leave with them, and gives those still going.
func (r *durableJobRunner) settleWorkspaceImportBatchChildren(job storedJob) ([]storedWorkspaceImportBatchItem, error) {
	active, err := loadWorkspaceImportBatchItemsInState(r.app, job.ID, workspaceImportBatchItemImporting, workspaceImportBatchDocumentsAtOnce*2)
	if err != nil {
		return nil, err
	}
	going := make([]storedWorkspaceImportBatchItem, 0, len(active))
	for _, item := range active {
		child, err := loadStoredJob(r.app, item.ChildJobID, job.Owner)
		state, code, detail := workspaceImportBatchItemFailed, "", ""
		switch {
		case errors.Is(err, sql.ErrNoRows):
			code = errorCodeWorkspaceImportUnavailable
		case err != nil:
			return nil, err
		case child.Status == durableJobStatusQueued:
			going = append(going, item)
			continue
		case child.Status == durableJobStatusCompleted:
			state = workspaceImportBatchItemImported
		case child.Status == durableJobStatusCanceled:
			state = workspaceImportBatchItemCancelled
		default:
			code = child.ErrorCode
			if childPayload, decodeErr := decodeWorkspaceImportPayload(child); decodeErr == nil {
				detail = workspaceImportJobErrorDetail(child, childPayload)
			}
		}
		if err := r.app.RunInTransaction(func(txApp core.App) error {
			message := item.Message
			if detail != "" {
				message = detail
			}
			if _, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_workspace_import_batch_items}}
				SET [[state]] = {:state}, [[error_code]] = {:code}, [[message]] = {:message},
					[[page_ready]] = CASE WHEN {:state} = 'imported' THEN 1 ELSE [[page_ready]] END, [[child_job_id]] = ''
				WHERE [[job_id]] = {:jobId} AND [[position]] = {:position} AND [[state]] = 'importing'
			`).Bind(dbx.Params{
				"jobId": job.ID, "position": item.Position, "state": state, "code": code, "message": message,
			}).Execute(); err != nil {
				return err
			}
			if state == workspaceImportBatchItemImported {
				if _, err := txApp.DB().NewQuery(`
					UPDATE {{leafen_jobs}} SET [[processed_items]] = [[processed_items]] + 1 WHERE [[id]] = {:id}
				`).Bind(dbx.Params{"id": job.ID}).Execute(); err != nil {
					return err
				}
				// A database's row pages are there once it is: their bodies
				// and the pages under them can go in.
				if item.Kind == workspaceImportBatchItemKindDatabase {
					if _, err := txApp.DB().NewQuery(`
						UPDATE {{leafen_workspace_import_batch_items}} SET [[page_ready]] = 1
						WHERE [[job_id]] = {:jobId} AND [[parent_position]] = {:position} AND [[kind]] = 'document'
					`).Bind(dbx.Params{"jobId": job.ID, "position": item.Position}).Execute(); err != nil {
						return err
					}
				}
			}
			_, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_jobs}} WHERE [[id]] = {:id} AND [[status]] <> 'queued'
			`).Bind(dbx.Params{"id": item.ChildJobID}).Execute()
			return err
		}); err != nil {
			return nil, err
		}
	}
	return going, nil
}

// workspaceImportBatchEmptyRowPage reports a database's row page with no
// body (an export lists a row's properties alone, which its CSV gives): its
// row is all of it, and nothing goes into the row's page, as the row had none.
func (r *durableJobRunner) workspaceImportBatchEmptyRowPage(jobID string, item storedWorkspaceImportBatchItem) (bool, error) {
	if item.Kind != workspaceImportBatchItemKindDocument || item.ParentPosition < 0 || item.Format != workspaceImportFormatMarkdown {
		return false, nil
	}
	var parentKind string
	if err := r.app.DB().NewQuery(`
		SELECT [[kind]] FROM {{leafen_workspace_import_batch_items}} WHERE [[job_id]] = {:jobId} AND [[position]] = {:position}
	`).Bind(dbx.Params{"jobId": jobID, "position": item.ParentPosition}).Row(&parentKind); err != nil || parentKind != workspaceImportBatchItemKindDatabase {
		return false, err
	}
	text, err := readWorkspaceImportBatchText(r.app.DataDir(), item)
	return err == nil && strings.TrimSpace(text) == "", err
}

// completeWorkspaceImportBatch ends a batch with all its files gone in or
// reported.
func (r *durableJobRunner) completeWorkspaceImportBatch(job storedJob) error {
	if err := removeWorkspaceImportBatchText(r.app.DataDir(), job.ID); err != nil {
		return err
	}
	if payload, err := decodeWorkspaceImportPayload(job); err == nil {
		if err := r.writeWorkspaceImportBatchLog(job, payload); err != nil {
			r.app.Logger().Warn("workspace import log page not made", "jobId", job.ID, "error", err)
		}
	}
	return r.finishWorkspaceImportBatch(job, durableJobStatusCompleted, workspaceImportPhaseCompleted, "")
}

func (r *durableJobRunner) finishWorkspaceImportBatch(job storedJob, status, phase, errorCode string) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		payload, err := decodeWorkspaceImportPayload(current)
		if err != nil {
			return err
		}
		payload.Phase = phase
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = {:status}, [[payload_json]] = {:payload}, [[error_code]] = {:errorCode}, [[updated]] = {:updated},
				[[processed_items]] = (SELECT COUNT(*) FROM {{leafen_workspace_import_batch_items}}
					WHERE [[job_id]] = {:id} AND [[state]] = 'imported' AND [[kind]] <> 'folder')
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "owner": current.Owner, "status": status, "payload": string(encoded),
			"errorCode": errorCode, "updated": r.clock.now().UnixMilli(),
		}).Execute()
		if err != nil {
			return err
		}
		return cleanupWorkspaceImportTerminalHistory(txApp, current.WorkspaceID)
	})
}

// cleanupWorkspaceImportBatch ends a batch cancelled, or one that can't go on:
// what came in stays and the rest is reported as not brought in, its pages
// never made. The few documents under way go in when cancelled, so none is
// left an empty page and the count says them; one that can't go on asks them
// to stop.
func (r *durableJobRunner) cleanupWorkspaceImportBatch(job storedJob, payload workspaceImportJobPayload) error {
	going, err := r.settleWorkspaceImportBatchChildren(job)
	if err != nil {
		return err
	}
	if len(going) > 0 {
		for _, item := range going {
			if payload.TerminalErrorCode == "" {
				break
			}
			if err := r.requestWorkspaceImportChildCancel(item.ChildJobID, job.Owner); err != nil {
				return err
			}
		}
		return r.scheduleWorkspaceImportBatch(job, workspaceImportBatchTurnDelay, nil)
	}
	if _, err := r.app.DB().NewQuery(`
		UPDATE {{leafen_workspace_import_batch_items}} SET [[state]] = 'cancelled'
		WHERE [[job_id]] = {:jobId} AND [[state]] IN ('pending', 'received')
	`).Bind(dbx.Params{"jobId": job.ID}).Execute(); err != nil {
		return err
	}
	if err := removeWorkspaceImportBatchText(r.app.DataDir(), job.ID); err != nil {
		return err
	}
	if payload.TerminalErrorCode != "" {
		return r.finishWorkspaceImportBatch(job, durableJobStatusFailed, workspaceImportPhaseFailed, payload.TerminalErrorCode)
	}
	return r.finishWorkspaceImportBatch(job, durableJobStatusCanceled, workspaceImportPhaseCancelled, "")
}

// requestWorkspaceImportChildCancel asks a document's import to stop, unless
// it has gone past stopping or was asked already.
func (r *durableJobRunner) requestWorkspaceImportChildCancel(childID, owner string) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		child, err := loadStoredJob(txApp, childID, owner)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && child.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		payload, err := decodeWorkspaceImportPayload(child)
		if err != nil {
			return err
		}
		if payload.CancelRequested || payload.Phase == workspaceImportPhasePublishCleanup {
			return nil
		}
		payload.CancelRequested = true
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}} SET [[payload_json]] = {:payload}, [[not_before]] = {:now}, [[updated]] = {:now}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{"id": child.ID, "payload": string(encoded), "now": now}).Execute()
		return err
	})
}
