package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"syscall"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func (r *durableJobRunner) applyAdministratorBackupChunk(ctx context.Context, job storedJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload := administratorBackupJobPayload{}
	if json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil ||
		payload.FormatVersion != backupFormatVersion || job.MaxAttempts != administratorBackupMaxAttempts {
		return r.failAdministratorBackupJob(job, errorCodeAdministratorBackupCreateFailed, errors.New("invalid administrator backup payload"))
	}
	var err error
	switch payload.Phase {
	case administratorBackupPhaseSnapshot:
		err = r.prepareAdministratorBackupPublish(job, payload)
	case administratorBackupPhasePublish:
		err = r.createAdministratorBackupArtifact(ctx, job, payload)
	default:
		err = errors.New("invalid administrator backup phase")
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return err
	}
	return r.failAdministratorBackupJob(job, errorCodeAdministratorBackupCreateFailed, err)
}

func (r *durableJobRunner) prepareAdministratorBackupPublish(
	job storedJob,
	payload administratorBackupJobPayload,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := requireAdministrator(txApp, current.Owner); err != nil {
			return err
		}
		if json.Unmarshal([]byte(current.PayloadJSON), &payload) != nil || payload.Phase != administratorBackupPhaseSnapshot {
			return errors.New("administrator backup snapshot phase changed")
		}
		payload.Phase = administratorBackupPhasePublish
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := r.clock.now()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[processed_items]] = 1,
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "payload": string(encoded),
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		return err
	})
}

func (r *durableJobRunner) failAdministratorBackupJob(job storedJob, code string, cause error) error {
	payload := administratorBackupJobPayload{}
	if json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil ||
		(payload.Phase != administratorBackupPhaseSnapshot && payload.Phase != administratorBackupPhasePublish) {
		payload.Phase = administratorBackupPhaseSnapshot
	}
	payload.FormatVersion = backupFormatVersion
	// Its common causes have codes of their own, and why it failed stays
	// with it in the system's words (backup_failure_reason.go).
	if code == errorCodeAdministratorBackupCreateFailed {
		code = administratorBackupFailureCode(cause)
	}
	payload.Reason = backupFailureReason(cause)
	encoded, encodeErr := json.Marshal(payload)
	if encodeErr != nil {
		return encodeErr
	}
	reference := newDiagnosticReference()
	_, err := r.app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[status]] = 'failed', [[error_code]] = {:errorCode},
			[[diagnostic_reference]] = {:reference}, [[attempt]] = [[attempt]] + 1,
			[[payload_json]] = {:payload}, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'
	`).Bind(dbx.Params{
		"id": job.ID, "errorCode": code, "reference": reference,
		"payload": string(encoded),
		"updated": time.Now().UTC().UnixMilli(),
	}).Execute()
	if err != nil {
		return err
	}
	logDurableJobDiagnosticAt(r.app, diagnosticSourcePosition(0), code, reference, job.ID, "administrator-backup.run", "terminal", "", cause,
		"causeReason", payload.Reason)
	return nil
}

// administratorBackupFailureCode is why a backup file couldn't be made: no
// room for it, another backup or restore running (PocketBase makes one at a
// time), or anything else.
func administratorBackupFailureCode(cause error) string {
	switch {
	case errors.Is(cause, syscall.ENOSPC), errors.Is(cause, syscall.EDQUOT):
		return errorCodeAdministratorBackupNoSpace
	case cause != nil && strings.Contains(cause.Error(), "another backup/restore operation has already been started"):
		return errorCodeAdministratorBackupBusy
	default:
		return errorCodeAdministratorBackupCreateFailed
	}
}
