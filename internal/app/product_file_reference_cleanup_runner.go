package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

type productFileReferenceCleanupFunc func(context.Context, core.App, string, int64) error
type productFileReferenceCleanupSliceResult = productFileMaintenanceSliceResult

type productFileReferenceCleanupRunner struct {
	app     core.App
	clock   durableJobClock
	cleanup productFileReferenceCleanupFunc
}

func newProductFileReferenceCleanupRunnerWithClock(app core.App, clock durableJobClock, cleanup productFileReferenceCleanupFunc) *productFileReferenceCleanupRunner {
	return &productFileReferenceCleanupRunner{app: app, clock: clock, cleanup: cleanup}
}
func (runner *productFileReferenceCleanupRunner) runSlice(ctx context.Context, scheduledAt time.Time) (productFileReferenceCleanupSliceResult, error) {
	result := productFileReferenceCleanupSliceResult{}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if scheduledAt.IsZero() || runner.cleanup == nil {
		return result, errors.New(errorCodeFileReferenceCleanupFailed)
	}
	now := scheduledAt.UTC().UnixMilli()
	pageBackfill, err := backfillProductFilePageReferences(
		ctx, runner.app, fileReferenceCleanupTurnLimit,
	)
	if err != nil {
		return result, err
	}
	if pageBackfill.Processed > 0 || pageBackfill.HasMore {
		result.Processed = pageBackfill.Processed
		result.HasMore = true
		return result, nil
	}
	rowPageIconBackfill, err := backfillDatabaseRowPageIconReferences(
		ctx, runner.app, fileReferenceCleanupTurnLimit,
	)
	if err != nil {
		return result, err
	}
	if rowPageIconBackfill.Processed > 0 || rowPageIconBackfill.HasMore {
		result.Processed = rowPageIconBackfill.Processed
		result.HasMore = true
		return result, nil
	}
	rows, err := loadDueProductFileReferenceCleanups(ctx, runner.app, now)
	if err != nil {
		return result, err
	}
	for _, cleanup := range rows {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		outcome, processErr := runner.processCleanup(ctx, cleanup, scheduledAt)
		result.addWorkspaceOutcome(cleanup.WorkspaceID, outcome)
		if processErr != nil {
			return result, processErr
		}
		result.Processed++
	}
	if len(rows) == fileReferenceCleanupTurnLimit {
		result.HasMore = true
	} else {
		backfill, backfillErr := backfillProductFileNeverAttachedCleanups(
			ctx, runner.app, fileReferenceCleanupTurnLimit-len(rows),
		)
		if backfillErr != nil {
			return result, backfillErr
		}
		result.Processed += backfill.Processed
		result.HasMore = backfill.HasMore
	}
	var dueHasMore bool
	dueHasMore, result.NextDue, err = productFileReferenceCleanupFrontier(ctx, runner.app, now)
	if err != nil {
		return result, err
	}
	result.HasMore = result.HasMore || dueHasMore
	receipts, err := retireProductFileDeleteReceipts(ctx, runner.app, now)
	if err != nil {
		return result, err
	}
	result.Processed += receipts.Deleted
	result.HasMore = result.HasMore || receipts.HasMore
	if result.NextDue == 0 || (receipts.NextDue > 0 && receipts.NextDue < result.NextDue) {
		result.NextDue = receipts.NextDue
	}
	return result, nil
}
func (runner *productFileReferenceCleanupRunner) processCleanup(
	ctx context.Context,
	cleanup storedProductFileReferenceCleanup,
	scheduledAt time.Time,
) (productFileMaintenanceObjectOutcome, error) {
	outcome := productFileMaintenanceObjectOutcome{}
	now := scheduledAt.UTC().UnixMilli()
	attempt := cleanup.CleanupAttempts + 1
	release := acquireProductFileOperationLock(productFileOperationReferenceCleanup, cleanup.FileID)
	defer release()
	claimed, err := claimProductFileReferenceCleanup(ctx, runner.app, cleanup.FileID, now)
	if err != nil {
		if ctx.Err() != nil {
			return outcome, ctx.Err()
		}
		if fileMaintenanceOperationalCauseCode(err) != "" {
			return outcome, err
		}
		reference := newDiagnosticReference()
		outcome.FailedObjects = 1
		outcome.DiagnosticReference = reference
		if retryErr := deferProductFileReferenceCleanup(
			ctx, runner.app, cleanup, scheduledAt,
			runnerFinishedAt(runner.clock, scheduledAt), attempt,
			fileReferenceCleanupRetrySeconds, errorCodeFileReferenceCleanupFailed, outcome,
		); retryErr != nil {
			return outcome, errors.Join(err, retryErr)
		}
		logDurableJobDiagnostic(runner.app, errorCodeFileReferenceCleanupFailed, reference, cleanup.WorkspaceID, "file_reference.cleanup", "claim-policy", err)
		return outcome, nil
	}
	if !claimed {
		return outcome, nil
	}
	quotaRelease, err := prepareProductOrganizationStorageAdmission(
		ctx, runner.app, cleanup.WorkspaceID, now,
	)
	if err != nil {
		reference := newDiagnosticReference()
		outcome.FailedObjects = 1
		outcome.DiagnosticReference = reference
		if retryErr := deferProductFileReferenceCleanup(
			ctx, runner.app, cleanup, scheduledAt,
			runnerFinishedAt(runner.clock, scheduledAt), attempt,
			fileReferenceCleanupRetrySeconds,
			errorCodeOrganizationStorageReconciliationPending, outcome,
		); retryErr != nil {
			return outcome, errors.Join(err, retryErr)
		}
		logDurableJobDiagnostic(
			runner.app, errorCodeFileReferenceCleanupFailed, reference,
			cleanup.WorkspaceID, "file_reference.cleanup", "quota-ready", err,
		)
		return outcome, nil
	}
	defer quotaRelease()
	if err := runner.cleanup(ctx, runner.app, cleanup.FileID, now); err != nil {
		if ctx.Err() != nil {
			return outcome, ctx.Err()
		}
		retrySeconds := fileReferenceCleanupRetrySeconds
		stage := "delete-storage"
		if errors.Is(err, errLegalHoldPreventsPermanentDelete) {
			retrySeconds = fileReferenceCleanupPolicyRecheckSeconds
			stage = "legal-hold"
		}
		reference := newDiagnosticReference()
		outcome.FailedObjects = 1
		outcome.DiagnosticReference = reference
		if retryErr := deferProductFileReferenceCleanup(
			ctx, runner.app, cleanup, scheduledAt,
			runnerFinishedAt(runner.clock, scheduledAt), attempt,
			retrySeconds, errorCodeFileReferenceCleanupFailed, outcome,
		); retryErr != nil {
			return outcome, errors.Join(err, retryErr)
		}
		logDurableJobDiagnostic(runner.app, errorCodeFileReferenceCleanupFailed, reference, cleanup.WorkspaceID, "file_reference.cleanup", stage, err)
		return outcome, nil
	}
	outcome.DeletedObjects = 1
	if err := completeProductFileReferenceCleanup(
		ctx, runner.app, cleanup, scheduledAt,
		runnerFinishedAt(runner.clock, scheduledAt), attempt, outcome,
	); err != nil {
		return productFileMaintenanceObjectOutcome{}, err
	}
	return outcome, nil
}
func loadDueProductFileReferenceCleanups(ctx context.Context, app core.App, now int64) ([]storedProductFileReferenceCleanup, error) {
	rows := []storedProductFileReferenceCleanup{}
	err := app.DB().NewQuery(`SELECT [[file_id]],[[workspace_id]],[[status]],[[cleanup_due_at]],[[cleanup_attempts]],[[cleanup_error_code]]
		FROM {{leafen_file_reference_cleanups}} WHERE [[cleanup_due_at]]>0 AND [[cleanup_due_at]]<={:now}
			AND [[status]] IN ({:pending},{:cleanupPending}) ORDER BY [[cleanup_due_at]],[[file_id]] LIMIT {:limit}`).WithContext(ctx).Bind(dbx.Params{"now": now, "pending": fileReferenceCleanupStatusPending, "cleanupPending": fileReferenceCleanupStatusCleanupPending, "limit": fileReferenceCleanupTurnLimit}).All(&rows)
	return rows, err
}
func claimProductFileReferenceCleanup(ctx context.Context, app core.App, fileID string, now int64) (bool, error) {
	claimed := false
	err := app.RunInTransaction(func(txApp core.App) error {
		var row storedProductFileReferenceCleanup
		if err := txApp.DB().NewQuery(`SELECT [[file_id]],[[workspace_id]],[[status]],[[cleanup_due_at]],[[cleanup_attempts]],[[cleanup_error_code]] FROM {{leafen_file_reference_cleanups}} WHERE [[file_id]]={:fileId}`).WithContext(ctx).Bind(dbx.Params{"fileId": fileID}).One(&row); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if row.CleanupDueAt > now {
			return nil
		}
		referenced, err := findActiveProductFileReference(txApp, fileID)
		if err != nil {
			return err
		}
		if referenced {
			return cancelProductFileReferenceCleanup(ctx, txApp, fileID)
		}
		pendingDelete, err := productFileDeletionPending(txApp, fileID)
		if err != nil {
			return err
		}
		if pendingDelete {
			return cancelProductFileReferenceCleanup(ctx, txApp, fileID)
		}
		result, err := txApp.DB().NewQuery(`UPDATE {{leafen_file_reference_cleanups}} SET [[status]]={:cleanupPending}, [[cleanup_attempts]]=[[cleanup_attempts]]+1, [[cleanup_error_code]]='', [[updated_at]]={:now} WHERE [[file_id]]={:fileId} AND [[cleanup_due_at]]<={:now} AND [[status]] IN ({:pending},{:cleanupPending})`).WithContext(ctx).Bind(dbx.Params{"cleanupPending": fileReferenceCleanupStatusCleanupPending, "pending": fileReferenceCleanupStatusPending, "fileId": fileID, "now": now}).Execute()
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		claimed = count == 1
		return err
	})
	return claimed, err
}
func deferProductFileReferenceCleanup(
	ctx context.Context,
	app core.App,
	cleanup storedProductFileReferenceCleanup,
	scheduledAt time.Time,
	finishedAt time.Time,
	attempt int,
	retrySeconds int,
	code string,
	outcome productFileMaintenanceObjectOutcome,
) error {
	now := scheduledAt.UTC().UnixMilli()
	next := now + int64(retrySeconds)*int64(time.Second/time.Millisecond)
	return app.RunInTransaction(func(txApp core.App) error {
		updated, err := txApp.DB().NewQuery(`UPDATE {{leafen_file_reference_cleanups}}
			SET [[status]]={:pending}, [[cleanup_due_at]]={:next},
				[[cleanup_attempts]]=CASE WHEN [[cleanup_attempts]]<{:attempt}
					THEN {:attempt} ELSE [[cleanup_attempts]] END,
				[[cleanup_error_code]]={:code}, [[updated_at]]={:now}
			WHERE [[file_id]]={:fileId}`).WithContext(ctx).Bind(dbx.Params{
			"pending": fileReferenceCleanupStatusPending, "next": next,
			"attempt": attempt, "code": code, "now": now, "fileId": cleanup.FileID,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := updated.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errFileMaintenanceStateInvalid, err)
		}
		return recordProductFileMaintenanceOutcome(
			ctx, txApp, scheduledAt, finishedAt, cleanup.WorkspaceID,
			productFileMaintenanceSourceReference, cleanup.FileID, attempt, outcome,
		)
	})
}
func completeProductFileReferenceCleanup(
	ctx context.Context,
	app core.App,
	cleanup storedProductFileReferenceCleanup,
	scheduledAt time.Time,
	finishedAt time.Time,
	attempt int,
	outcome productFileMaintenanceObjectOutcome,
) error {
	now := scheduledAt.UTC().UnixMilli()
	return app.RunInTransaction(func(txApp core.App) error {
		if err := releaseProductOrganizationStorage(ctx, txApp, cleanup.FileID, now); err != nil {
			return err
		}
		deleted, err := txApp.DB().Delete(filesCollection, dbx.HashExp{
			"id": cleanup.FileID, "workspace": cleanup.WorkspaceID,
		}).WithContext(ctx).Execute()
		if err != nil {
			return err
		}
		count, err := deleted.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			deleted, err = txApp.DB().NewQuery(`DELETE FROM {{leafen_file_reference_cleanups}}
				WHERE [[file_id]]={:fileId} AND [[status]]={:cleanupPending}`).WithContext(ctx).Bind(dbx.Params{
				"fileId":         cleanup.FileID,
				"cleanupPending": fileReferenceCleanupStatusCleanupPending,
			}).Execute()
			if err != nil {
				return err
			}
			count, err = deleted.RowsAffected()
		}
		if err != nil || count != 1 {
			return errors.Join(errFileMaintenanceStateInvalid, err)
		}
		return recordProductFileMaintenanceOutcome(
			ctx, txApp, scheduledAt, finishedAt, cleanup.WorkspaceID,
			productFileMaintenanceSourceReference, cleanup.FileID, attempt, outcome,
		)
	})
}
func productFileReferenceCleanupFrontier(ctx context.Context, app core.App, now int64) (bool, int64, error) {
	var frontier struct {
		HasMore int           `db:"has_more"`
		NextDue sql.NullInt64 `db:"next_due"`
	}
	err := app.DB().NewQuery(`SELECT EXISTS(SELECT 1 FROM {{leafen_file_reference_cleanups}} WHERE [[cleanup_due_at]]>0 AND [[cleanup_due_at]]<={:now} AND [[status]] IN ({:pending},{:cleanupPending}) LIMIT 1) AS [[has_more]], MIN([[cleanup_due_at]]) AS [[next_due]] FROM {{leafen_file_reference_cleanups}} WHERE [[status]] IN ({:pending},{:cleanupPending})`).WithContext(ctx).Bind(dbx.Params{"now": now, "pending": fileReferenceCleanupStatusPending, "cleanupPending": fileReferenceCleanupStatusCleanupPending}).One(&frontier)
	if err != nil {
		return false, 0, err
	}
	if !frontier.NextDue.Valid {
		return frontier.HasMore == 1, 0, nil
	}
	return frontier.HasMore == 1, frontier.NextDue.Int64, nil
}
func notifyProductFileReferenceCleanupAfterCommit(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(productFileMaintenanceRunnerStoreKey).(*productFileMaintenanceRunner); ok {
			runner.notify()
		}
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				notify()
			}
			return nil
		})
		return
	}
	notify()
}
func cleanupProductFileReferenceStorageWithContext(ctx context.Context, app core.App, fileID string, _ int64) error {
	record, err := app.FindRecordById(filesCollection, fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	workspaceID := record.GetString("workspace")
	organizationID, err := loadLegalHoldWorkspaceOrganization(app, workspaceID)
	if err != nil {
		return err
	}
	if err := assertNoActiveLegalHoldForPermanentDelete(app, organizationID, legalHoldDeletionTarget{WorkspaceID: workspaceID, CustodianUserIDs: []string{record.GetString("created_by")}}); err != nil {
		return err
	}
	fsys, err := app.NewFilesystem()
	if err != nil {
		return err
	}
	defer fsys.Close()
	fsys.SetContext(ctx)
	if err := fsys.Delete(productFileStorageKey(record)); err != nil && !errors.Is(err, filesystem.ErrNotFound) {
		return err
	}
	return deleteProductFilePreviews(fsys, record)
}
