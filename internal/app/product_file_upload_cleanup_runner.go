package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type productFileUploadCleanupFunc func(context.Context, core.App, string) error

type productFileUploadCleanupSliceResult = productFileMaintenanceSliceResult

type productFileUploadCleanupRunner struct {
	app     core.App
	clock   durableJobClock
	cleanup productFileUploadCleanupFunc
}

func newProductFileUploadCleanupRunnerWithClock(
	app core.App,
	clock durableJobClock,
	cleanup productFileUploadCleanupFunc,
) *productFileUploadCleanupRunner {
	return &productFileUploadCleanupRunner{
		app: app, clock: clock, cleanup: cleanup,
	}
}

func (runner *productFileUploadCleanupRunner) runSlice(
	ctx context.Context,
	scheduledAt time.Time,
) (productFileUploadCleanupSliceResult, error) {
	return runner.runFilteredSlice(ctx, scheduledAt, scheduledAt.UTC().UnixMilli(), "", "")
}

func (runner *productFileUploadCleanupRunner) runWorkspaceSlice(
	ctx context.Context,
	scheduledAt time.Time,
	workspaceID string,
	manualJobID string,
) (productFileUploadCleanupSliceResult, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	manualJobID = strings.TrimSpace(manualJobID)
	if workspaceID == "" || manualJobID == "" {
		return productFileUploadCleanupSliceResult{}, errFileMaintenanceStateInvalid
	}
	job, err := loadProductFileManualCleanupJob(ctx, runner.app, manualJobID)
	if err != nil || job.WorkspaceID != workspaceID ||
		job.Status != fileManualCleanupStatusRunning || job.ScheduledAt <= 0 {
		return productFileUploadCleanupSliceResult{}, errors.Join(
			errFileMaintenanceStateInvalid, err,
		)
	}
	return runner.runFilteredSlice(
		ctx, scheduledAt, job.ScheduledAt, workspaceID, manualJobID,
	)
}

func (runner *productFileUploadCleanupRunner) runFilteredSlice(
	ctx context.Context,
	scheduledAt time.Time,
	cutoffMillis int64,
	workspaceID string,
	manualJobID string,
) (productFileUploadCleanupSliceResult, error) {
	result := productFileUploadCleanupSliceResult{}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if scheduledAt.IsZero() || runner.cleanup == nil {
		return result, errors.New(errorCodeFileUploadCleanupFailed)
	}
	now := cutoffMillis
	if now <= 0 {
		return result, errFileMaintenanceStateInvalid
	}
	uploads, err := loadDueProductFileUploads(ctx, runner.app, now, workspaceID)
	if err != nil {
		return result, err
	}
	for _, upload := range uploads {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		outcome, processErr := runner.processUpload(
			ctx, upload, scheduledAt, manualJobID,
		)
		result.addWorkspaceOutcome(upload.WorkspaceID, outcome)
		if processErr != nil {
			return result, processErr
		}
		result.Processed++
	}
	result.HasMore, result.NextDue, err = productFileUploadCleanupFrontier(
		ctx, runner.app, now, workspaceID,
	)
	return result, err
}

func (runner *productFileUploadCleanupRunner) processUpload(
	ctx context.Context,
	upload storedProductFileUpload,
	scheduledAt time.Time,
	manualJobID string,
) (productFileMaintenanceObjectOutcome, error) {
	outcome := productFileMaintenanceObjectOutcome{}
	now := scheduledAt.UTC().UnixMilli()
	attempt := upload.CleanupTries + 1
	release := acquireProductFileOperationLock(productFileOperationResumable, upload.UploadID)
	defer release()
	claimed, err := claimProductFileUploadCleanup(ctx, runner.app, upload.UploadID, now)
	if err != nil || !claimed {
		return outcome, err
	}
	quotaRelease, err := prepareProductOrganizationStorageAdmission(
		ctx, runner.app, upload.WorkspaceID, now,
	)
	if err != nil {
		reference := newDiagnosticReference()
		outcome.FailedObjects = 1
		outcome.DiagnosticReference = reference
		if retryErr := deferProductFileUploadCleanup(
			ctx, runner.app, upload, scheduledAt,
			runnerFinishedAt(runner.clock, scheduledAt), attempt,
			errorCodeOrganizationStorageReconciliationPending, outcome, manualJobID,
		); retryErr != nil {
			return outcome, errors.Join(err, retryErr)
		}
		logDurableJobDiagnostic(
			runner.app, errorCodeFileUploadCleanupFailed, reference,
			upload.WorkspaceID, "file_upload.cleanup", "quota-ready", err,
		)
		return outcome, nil
	}
	defer quotaRelease()
	if err := runner.cleanup(ctx, runner.app, upload.UploadID); err != nil {
		if ctx.Err() != nil {
			return outcome, ctx.Err()
		}
		reference := newDiagnosticReference()
		outcome.FailedObjects = 1
		outcome.DiagnosticReference = reference
		if retryErr := deferProductFileUploadCleanup(
			ctx, runner.app, upload, scheduledAt,
			runnerFinishedAt(runner.clock, scheduledAt), attempt,
			errorCodeFileUploadCleanupFailed, outcome, manualJobID,
		); retryErr != nil {
			return outcome, errors.Join(err, retryErr)
		}
		logDurableJobDiagnostic(
			runner.app, errorCodeFileUploadCleanupFailed, reference, upload.WorkspaceID,
			"file_upload.cleanup", "delete-prefix", err,
		)
		return outcome, nil
	}
	outcome.Expired = 1
	outcome.DeletedObjects = 1
	if err := completeProductFileUploadCleanup(
		ctx, runner.app, upload, scheduledAt,
		runnerFinishedAt(runner.clock, scheduledAt), attempt, outcome, manualJobID,
	); err != nil {
		return productFileMaintenanceObjectOutcome{}, err
	}
	return outcome, nil
}

func loadDueProductFileUploads(
	ctx context.Context,
	app core.App,
	now int64,
	workspaceID string,
) ([]storedProductFileUpload, error) {
	uploads := []storedProductFileUpload{}
	err := productFileUploadCleanupDueQuery(app, now, workspaceID).
		WithContext(ctx).All(&uploads)
	return uploads, err
}

func claimProductFileUploadCleanup(
	ctx context.Context,
	app core.App,
	uploadID string,
	now int64,
) (bool, error) {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_file_uploads}}
		SET [[status]] = {:cleanupPending}, [[cleanup_attempts]] = [[cleanup_attempts]] + 1,
			[[cleanup_error_code]] = '', [[cleanup_due_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[upload_id]] = {:uploadId} AND [[cleanup_due_at]] > 0
			AND [[cleanup_due_at]] <= {:now}
			AND [[status]] IN ({:pending}, {:cleanupPending})
	`).WithContext(ctx).Bind(dbx.Params{
		"cleanupPending": fileUploadStatusCleanupPending, "now": now,
		"uploadId": uploadID, "pending": fileUploadStatusPending,
	}).Execute()
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func deferProductFileUploadCleanup(
	ctx context.Context,
	app core.App,
	upload storedProductFileUpload,
	scheduledAt time.Time,
	finishedAt time.Time,
	attempt int,
	errorCode string,
	outcome productFileMaintenanceObjectOutcome,
	manualJobID string,
) error {
	now := scheduledAt.UTC().UnixMilli()
	nextDue := now + int64(fileTransferUploadCleanupRetrySeconds)*int64(time.Second/time.Millisecond)
	return app.RunInTransaction(func(txApp core.App) error {
		updated, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_file_uploads}}
			SET [[cleanup_due_at]] = {:nextDue}, [[cleanup_error_code]] = {:errorCode},
				[[updated_at]] = {:now}
			WHERE [[upload_id]] = {:uploadId} AND [[status]] = {:cleanupPending}
		`).WithContext(ctx).Bind(dbx.Params{
			"nextDue": nextDue, "errorCode": errorCode, "now": now,
			"uploadId": upload.UploadID, "cleanupPending": fileUploadStatusCleanupPending,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := updated.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errFileMaintenanceStateInvalid, err)
		}
		return recordProductFileUploadMaintenanceOutcome(
			ctx, txApp, scheduledAt, finishedAt, upload.WorkspaceID,
			upload.UploadID, attempt, outcome, manualJobID,
		)
	})
}

func completeProductFileUploadCleanup(
	ctx context.Context,
	app core.App,
	upload storedProductFileUpload,
	scheduledAt time.Time,
	finishedAt time.Time,
	attempt int,
	outcome productFileMaintenanceObjectOutcome,
	manualJobID string,
) error {
	now := scheduledAt.UTC().UnixMilli()
	return app.RunInTransaction(func(txApp core.App) error {
		if err := releaseProductOrganizationStorageIfPresent(
			ctx, txApp, upload.UploadID, now,
		); err != nil {
			return err
		}
		if upload.NotionMode != "" {
			if _, err := txApp.DB().Delete(productFileUploadPartsTable, dbx.HashExp{
				"upload_id": upload.UploadID,
			}).WithContext(ctx).Execute(); err != nil {
				return err
			}
			updated, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_file_uploads}}
				SET [[status]]={:expired}, [[cleanup_due_at]]=0,
					[[cleanup_error_code]]='', [[notion_external_url_cipher]]='',
					[[notion_import_next_at]]=0, [[updated_at]]={:now}
				WHERE [[upload_id]]={:uploadId} AND [[status]]={:cleanupPending}
			`).WithContext(ctx).Bind(dbx.Params{
				"expired": fileUploadStatusExpired, "now": now,
				"uploadId":       upload.UploadID,
				"cleanupPending": fileUploadStatusCleanupPending,
			}).Execute()
			if err != nil {
				return err
			}
			count, err := updated.RowsAffected()
			if err != nil || count != 1 {
				return errors.Join(errFileMaintenanceStateInvalid, err)
			}
			return recordProductFileUploadMaintenanceOutcome(
				ctx, txApp, scheduledAt, finishedAt, upload.WorkspaceID,
				upload.UploadID, attempt, outcome, manualJobID,
			)
		}
		deleted, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_file_uploads}}
			WHERE [[upload_id]] = {:uploadId} AND [[status]] = {:cleanupPending}
		`).WithContext(ctx).Bind(dbx.Params{
			"uploadId": upload.UploadID, "cleanupPending": fileUploadStatusCleanupPending,
		}).Execute()
		if err != nil {
			return err
		}
		count, err := deleted.RowsAffected()
		if err != nil || count != 1 {
			return errors.Join(errFileMaintenanceStateInvalid, err)
		}
		return recordProductFileUploadMaintenanceOutcome(
			ctx, txApp, scheduledAt, finishedAt, upload.WorkspaceID,
			upload.UploadID, attempt, outcome, manualJobID,
		)
	})
}

func recordProductFileUploadMaintenanceOutcome(
	ctx context.Context,
	app core.App,
	scheduledAt time.Time,
	finishedAt time.Time,
	workspaceID string,
	uploadID string,
	attempt int,
	outcome productFileMaintenanceObjectOutcome,
	manualJobID string,
) error {
	if manualJobID != "" {
		return recordProductFileMaintenanceOutcomeForManualJob(
			ctx, app, scheduledAt, finishedAt, workspaceID,
			productFileMaintenanceSourceUpload, uploadID, attempt, outcome, manualJobID,
		)
	}
	return recordProductFileMaintenanceOutcome(
		ctx, app, scheduledAt, finishedAt, workspaceID,
		productFileMaintenanceSourceUpload, uploadID, attempt, outcome,
	)
}

func productFileUploadCleanupFrontier(
	ctx context.Context,
	app core.App,
	now int64,
	workspaceID string,
) (bool, int64, error) {
	var frontier struct {
		HasMore int           `db:"has_more"`
		NextDue sql.NullInt64 `db:"next_due"`
	}
	err := productFileUploadCleanupFrontierQuery(app, now, workspaceID).
		WithContext(ctx).One(&frontier)
	if err != nil {
		return false, 0, err
	}
	nextDue := int64(0)
	if frontier.NextDue.Valid {
		nextDue = frontier.NextDue.Int64
	}
	return frontier.HasMore == 1, nextDue, nil
}

func notifyProductFileUploadCleanupAfterCommit(app core.App) {
	notify := func() {
		runner, ok := app.Store().Get(productFileMaintenanceRunnerStoreKey).(*productFileMaintenanceRunner)
		if ok {
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

func cleanupProductFileUploadStorageWithContext(
	ctx context.Context,
	app core.App,
	uploadID string,
) error {
	fsys, err := app.NewFilesystem()
	if err != nil {
		return err
	}
	defer fsys.Close()
	fsys.SetContext(ctx)
	return errors.Join(fsys.DeletePrefix(productFileUploadPrefix(uploadID))...)
}
