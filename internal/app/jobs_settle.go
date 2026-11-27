package app

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/pocketbase/dbx"
)

// durableJobStoreRetryDelay is how long a job whose turn found the database
// busy or its storage unavailable waits before its next turn, as a batch
// import's document does (workspace_import_batch_runner.go).
const durableJobStoreRetryDelay = 5 * time.Second

// settleReadyJob settles a job's turn. One that went through forgets the
// Notion import's failures before it; one that failed is tried again or
// failed as its kind says.
//
// Which job failed is read, never looked up inside a write: the write
// connection is lent only with the database's write lock
// (sqlite_write_gate.go), and while another program holds that lock a write
// waits its time and is refused before it begins (2026-11-11: an import's
// failure handler opened its write to see whether the job was an import, was
// refused, so the import's own "busy, try again" was skipped and the runner
// went on to fail the import as any other job).
//
// A busy or unavailable store passes (sqlite_errors.go): the job tries again
// from where it was, a little later, whatever its kind, and is never failed
// for it. A write the store refuses here leaves the job queued as it was, and
// its next turn settles it again.
func (r *durableJobRunner) settleReadyJob(ctx context.Context, jobID string, applyErr error) error {
	if applyErr == nil {
		return r.settleNotionImportTurn(jobID)
	}
	if errors.Is(applyErr, context.Canceled) && ctx.Err() != nil {
		return ctx.Err()
	}
	reference := newDiagnosticReference()
	// Page copies, block moves and wiki changes read their own kind and
	// count a busy store among their attempts.
	if handled, recoveryErr := scheduleReversibleStructuralFailure(
		r.app, jobID, reference, applyErr,
	); handled {
		return recoveryErr
	}
	if handled, recoveryErr := scheduleWikiMutationFailure(
		r.app, jobID, reference, applyErr,
	); handled {
		if recoveryErr != nil {
			return errors.Join(applyErr, recoveryErr)
		}
		return nil
	}
	job, readErr := loadStoredJob(r.app, jobID, "")
	if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
		return errors.Join(applyErr, readErr)
	}
	queued := readErr == nil && job.Status == durableJobStatusQueued
	if queued && sqliteOperationalErrorClass(applyErr) != sqliteOperationalNone {
		return r.retryDurableJobAfterStore(job, reference, applyErr)
	}
	// A handler that answers with an error didn't get its write: the job
	// stays queued as it was, never failed in its place.
	if queued && job.Kind == notionImportJobKind {
		if handled, retryErr := r.scheduleNotionProviderRateLimit(jobID, reference, applyErr); handled || retryErr != nil {
			if retryErr != nil {
				logDurableJobDiagnostic(
					r.app, errorCodeNotionImportProviderFailed, reference, jobID,
					"notion-import.apply", "rate-limit-scheduled", applyErr,
				)
			} else {
				logDurableJobScheduledAgain(
					r.app, notionProviderWaitLevel(applyErr), errorCodeNotionImportProviderFailed, reference, jobID,
					"notion-import.apply", "rate-limit-scheduled", "", applyErr,
				)
			}
			if retryErr != nil {
				return errors.Join(applyErr, retryErr)
			}
			return nil
		}
	}
	if queued && job.Kind == workspaceImportJobKind {
		// One meeting its page held for a while waits for it; one refused for
		// good ends in words, its publishing too (workspace_import_held.go).
		if waited, waitErr := r.waitWorkspaceImportHeld(job, reference, applyErr); waitErr != nil {
			return errors.Join(applyErr, waitErr)
		} else if waited {
			return nil
		}
		code := stableWorkspaceImportFailureCode(applyErr)
		if handled, failureErr := r.scheduleWorkspaceImportFailure(
			r.app, jobID, reference, code, workspaceImportRefusalDetail(r.app, job, applyErr),
			retryWorkspaceImportPublication(applyErr) && !workspaceImportRefusedForGood(applyErr),
		); handled || failureErr != nil {
			logDurableJobDiagnostic(
				r.app, code, reference, jobID,
				"workspace-import.apply", "recovery-scheduled", applyErr,
			)
			if failureErr != nil {
				return errors.Join(applyErr, failureErr)
			}
			return nil
		}
	}
	// An export whose page is held a while waits for it (workspace_export_held.go).
	if queued {
		if waited, waitErr := r.waitWorkspaceExportHeld(job, reference, applyErr); waitErr != nil {
			return errors.Join(applyErr, waitErr)
		} else if waited {
			return nil
		}
	}
	failureCode := ""
	var notionRateLimit *notionProviderRateLimitError
	if errors.As(applyErr, &notionRateLimit) {
		failureCode = errorCodeNotionImportProviderFailed
	}
	// A Notion import that ran out of attempts while Notion was away or busy
	// says Notion didn't answer, not that Leafen stopped (2026-11-06: Notion
	// unreachable for two minutes ended with "리펜이 가져오는 도중 멈췄습니다").
	if failureCode == "" && job.Kind == notionImportJobKind && notionProviderMayPass(applyErr) {
		failureCode = errorCodeNotionImportProviderFailed
	}
	if failureCode == "" {
		failureCode = stableNotionImportFailureCode(applyErr)
	}
	if failureCode == "" {
		failureCode = stableWorkspaceExportFailureCode(applyErr)
	}
	if failureCode == "" {
		failureCode = stableOrganizationStorageFailureCode(applyErr)
	}
	if job.Kind == administratorAccountCleanupJobKind {
		failureCode = errorCodeAdministratorAccountCleanupFailed
	}
	if failureCode == "" {
		failureCode = errorCodeJobRunnerFailed
	}
	logDurableJobDiagnostic(r.app, failureCode, reference, jobID, "job.apply", "execute", applyErr)
	if failErr := r.fail(jobID, reference, failureCode); failErr != nil {
		logDurableJobDiagnostic(r.app, failureCode, reference, jobID, "job.apply", "failure-record", failErr)
		return errors.Join(applyErr, failErr)
	}
	// A migration's run says where it stopped and why (notion_migration_failure.go).
	if job.Kind == notionImportJobKind {
		r.recordNotionImportFailure(ctx, jobID, reference, failureCode, applyErr)
	}
	return nil
}

// retryDurableJobAfterStore has a job whose turn found the store busy or
// unavailable try again from where it was: its phase, attempts and error stay
// as they were, and only its next turn moves a little later — when the store
// lets that be written at all. An import says so in its own words.
func (r *durableJobRunner) retryDurableJobAfterStore(job storedJob, reference string, applyErr error) error {
	busy := isSQLiteBusyError(applyErr)
	// Scheduled again, it is said as a warning (diagnostic_job_again.go).
	if job.Kind == workspaceImportJobKind {
		code := errorCodeWorkspaceImportStorageUnavailable
		if busy {
			code = errorCodeWorkspaceImportDatabaseBusy
		}
		logDurableJobScheduledAgain(
			r.app, slog.LevelWarn, code, reference, job.ID, "workspace-import.apply", "recovery-scheduled", "", applyErr,
		)
	} else {
		causeCode := persistentWriteStorageUnavailableCauseCode
		if busy {
			causeCode = persistentWriteDatabaseBusyCauseCode
		}
		logDurableJobScheduledAgain(
			r.app, slog.LevelWarn, errorCodeJobRunnerFailed, reference, job.ID, "job.apply", "retry-scheduled", causeCode, applyErr,
		)
	}
	now := r.clock.now().UnixMilli()
	notBefore := now + durableJobStoreRetryDelay.Milliseconds()
	if _, err := r.app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[not_before]] = {:notBefore}, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued' AND [[not_before]] < {:notBefore}
	`).Bind(dbx.Params{"id": job.ID, "notBefore": notBefore, "updated": now}).Execute(); err != nil {
		return errors.Join(applyErr, err)
	}
	return nil
}
