package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An import that ended — refused or cancelled — takes out what it had put in
// the page it adds to (its cleanup). Once that page is deleted for good, its
// fence keeps every write out for its 30 days (page_deletion_hold.go), the
// cleanup's too, so the cleanup waits: for the page to be brought back, when
// it takes out what the import put in, or to be gone, the import's blocks
// with it. It looks again every workspaceImportCleanupRetryDelay as a quiet
// wait — no failure counted, nothing written to the log, the reason the
// import ended kept — and the import shows as ended meanwhile: failed, saying
// the page was deleted for good and what bringing it back does, or cancelled;
// never as running (2026-11-20: it showed as running for up to 30 days, and
// as the account not being allowed to change the page).

// workspaceImportCleanupBehindDeletion reports whether an ended import's
// cleanup waits on its page deleted for good.
func workspaceImportCleanupBehindDeletion(app core.App, job storedJob, payload workspaceImportJobPayload) (bool, error) {
	if payload.Phase != workspaceImportPhaseCleanup || (!payload.AppendToExistingPage && !payload.ReplaceExistingPage) {
		return false, nil
	}
	pageID := job.PageID
	if pageID == "" {
		pageID = workspaceImportPrimaryRootPageID(payload)
	}
	if pageID == "" {
		return false, nil
	}
	return pagePermanentDeleteFenceActive(app, pageID)
}

// workspaceImportEndedStatus is how an import shows once it has ended but
// its cleanup waits: failed when it was refused, else cancelled.
func workspaceImportEndedStatus(payload workspaceImportJobPayload) string {
	if payload.TerminalErrorCode != "" {
		return workspaceImportStatusFailed
	}
	return workspaceImportStatusCancelled
}

// cleanupWorkspaceImportOrWait takes an ended import's cleanup turn, or waits
// quietly while its page is deleted for good. One that ended on its page in
// the trash says from then on that the page was deleted for good.
func (r *durableJobRunner) cleanupWorkspaceImportOrWait(job storedJob, payload workspaceImportJobPayload) error {
	waits, err := workspaceImportCleanupBehindDeletion(r.app, job, payload)
	if err != nil || !waits {
		if err != nil {
			return err
		}
		return r.cleanupWorkspaceImport(job, payload)
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil {
			return err
		}
		now := r.clock.now().UnixMilli()
		params := dbx.Params{"id": current.ID, "notBefore": now + workspaceImportCleanupRetryDelay.Milliseconds()}
		if currentPayload.CancelRequested || currentPayload.TerminalErrorCode != errorCodePageInTrash {
			_, err = txApp.DB().NewQuery(`
				UPDATE {{leafen_jobs}} SET [[not_before]] = {:notBefore} WHERE [[id]] = {:id} AND [[status]] = 'queued'
			`).Bind(params).Execute()
			return err
		}
		currentPayload.TerminalErrorCode = errorCodePageNotFound
		currentPayload.TerminalErrorDetail = workspaceImportTargetGoneWords(txApp, current, errorCodePageNotFound)
		encoded, err := json.Marshal(currentPayload)
		if err != nil {
			return err
		}
		params["payload"], params["errorCode"], params["updated"] = string(encoded), errorCodePageNotFound, now
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[error_code]] = {:errorCode},
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(params).Execute()
		return err
	})
}
