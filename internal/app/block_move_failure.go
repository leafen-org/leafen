package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func scheduleReversibleStructuralFailure(
	app core.App,
	jobID, reference string,
	applyErr error,
) (bool, error) {
	if handled, recoveryErr := schedulePageDuplicationFailure(
		app, jobID, reference, applyErr,
	); handled {
		if recoveryErr != nil {
			return true, errors.Join(applyErr, recoveryErr)
		}
		return true, nil
	}
	if handled, recoveryErr := schedulePageWorkspaceMoveFailure(
		app, jobID, reference, applyErr,
	); handled {
		if recoveryErr != nil {
			return true, errors.Join(applyErr, recoveryErr)
		}
		return true, nil
	}
	if handled, recoveryErr := scheduleBlockMoveFailure(
		app, jobID, reference, applyErr,
	); handled {
		if recoveryErr != nil {
			return true, errors.Join(applyErr, recoveryErr)
		}
		return true, nil
	}
	return false, nil
}

func scheduleBlockMoveFailure(
	app core.App,
	jobID, reference string,
	applyErr error,
) (bool, error) {
	job, err := loadStoredJob(app, jobID, "")
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Kind != blockMoveJobKind) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	detail, err := loadBlockMoveJobByID(app, jobID, "")
	if err != nil {
		return true, err
	}
	diagnosticCode := errorCodeBlockMoveUnavailable
	if selection, ok := persistentWriteStorageErrorSelection(
		applyErr, persistentWriteBlockMove,
	); ok {
		diagnosticCode = selection.Code
	}
	now := time.Now().UTC().UnixMilli()
	if detail.Phase == blockMovePhaseRollingBack {
		err = rescheduleBlockMoveFailure(app, jobID, reference, now)
		if err == nil {
			logDurableJobDiagnostic(
				app, diagnosticCode, reference, jobID,
				"block-move.apply", "rollback-retry-scheduled", applyErr,
			)
		}
		return true, err
	}
	if job.Attempt+1 >= job.MaxAttempts {
		err = app.RunInTransaction(func(txApp core.App) error {
			if err := beginBlockMoveRollback(txApp, detail, diagnosticCode); err != nil {
				return err
			}
			_, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_jobs}}
				SET [[attempt]]=0,[[diagnostic_reference]]={:reference},[[updated]]={:updatedAt}
				WHERE [[id]]={:jobId}
			`).Bind(dbx.Params{
				"jobId": jobID, "reference": reference, "updatedAt": now,
			}).Execute()
			return err
		})
		if err == nil {
			logDurableJobDiagnostic(
				app, diagnosticCode, reference, jobID,
				"block-move.apply", "rollback-scheduled", applyErr,
			)
		}
		return true, err
	}
	err = rescheduleBlockMoveFailure(app, jobID, reference, now)
	if err == nil {
		logDurableJobDiagnostic(
			app, diagnosticCode, reference, jobID,
			"block-move.apply", "retry-scheduled", applyErr,
		)
	}
	return true, err
}

func rescheduleBlockMoveFailure(
	app core.App,
	jobID, reference string,
	now int64,
) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[attempt]]=[[attempt]]+1,[[diagnostic_reference]]={:reference},
			[[not_before]]={:notBefore},[[updated]]={:updatedAt}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{
		"jobId": jobID, "reference": reference,
		"notBefore": now + durableJobReadyDelay.Milliseconds(), "updatedAt": now,
	}).Execute()
	return err
}
