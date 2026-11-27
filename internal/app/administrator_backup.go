package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const administratorBackupJobKind = "administrator-backup"

type administratorBackupJobPayload struct {
	FormatVersion int    `json:"formatVersion"`
	Phase         string `json:"phase"`
	// Why it failed, in the system's own words (backup_failure_reason.go).
	Reason string `json:"reason,omitempty"`
}

type storedAdministratorBackupArtifact struct {
	JobID     string `db:"job_id"`
	Owner     string `db:"owner"`
	Filename  string `db:"filename"`
	Bytes     int64  `db:"bytes"`
	SHA256    string `db:"sha256"`
	CreatedAt int64  `db:"created_at"`
}

func registerAdministratorBackupRoutes(event *core.ServeEvent, jobs *durableJobRunner) {
	event.Router.POST(apiRoot+"/admin/backups", jobs.createAdministratorBackup)
	event.Router.GET(apiRoot+"/admin/backups/current", jobs.getCurrentAdministratorBackup)
	event.Router.GET(apiRoot+"/admin/backups/{jobId}", jobs.getAdministratorBackup)
	event.Router.POST(apiRoot+"/admin/backups/{jobId}/retry", jobs.retryAdministratorBackup)
	event.Router.POST(apiRoot+"/admin/backups/{jobId}/cancel", jobs.cancelAdministratorBackup)
	event.Router.GET(apiRoot+"/admin/backups/{jobId}/download", jobs.downloadAdministratorBackup)
}

func (r *durableJobRunner) createAdministratorBackup(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	input := administratorBackupInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the backup request details.")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	if input.RequestID == "" || len([]rune(input.RequestID)) > administratorBackupRequestIDRunes {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the backup request details.")
	}
	if err := cleanupExpiredAdministratorBackups(event.App, time.Now().UTC(), instanceProductExportRetention); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAdministratorBackupCreateFailed, "Couldn't clean up expired backups.", "administrator-backup.create", "retention", errorCodeAdministratorBackupCreateFailed, err)
	}

	requestHash := administratorBackupRequestHash(input)
	jobID := "administrator-backup-" + security.RandomString(15)
	replayed := false
	identityReused := false
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if err := requireAdministrator(txApp, user.Id); err != nil {
			return err
		}
		existing, loadErr := loadStoredJobByRequest(txApp, user.Id, input.RequestID)
		if loadErr == nil {
			if existing.Kind != administratorBackupJobKind || existing.RequestHash != requestHash {
				identityReused = true
				return nil
			}
			jobID, replayed = existing.ID, true
			return nil
		}
		if !errors.Is(loadErr, sql.ErrNoRows) {
			return loadErr
		}
		now := r.clock.now()
		payload, err := json.Marshal(administratorBackupJobPayload{
			FormatVersion: backupFormatVersion,
			Phase:         administratorBackupPhaseSnapshot,
		})
		if err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
				 [[status]], [[error_code]], [[diagnostic_reference]], [[not_before]], [[created]], [[updated]],
				 [[payload_json]], [[cursor]], [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]],
				 [[chunk_count]], [[captured_bytes]])
			VALUES
				({:id}, {:owner}, '', {:requestId}, {:requestHash}, {:kind}, '',
				 'queued', '', '', {:notBefore}, {:now}, {:now},
				 {:payload}, '', 0, 1, 0, {:maxAttempts}, 0, 0)
			ON CONFLICT ([[owner]], [[request_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": jobID, "owner": user.Id, "requestId": input.RequestID,
			"requestHash": requestHash, "kind": administratorBackupJobKind,
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "now": now.UnixMilli(),
			"payload": string(payload), "maxAttempts": defaultInstanceProductExportMaxAttempts,
		}).Execute()
		if err != nil {
			return err
		}
		inserted, err := result.RowsAffected()
		if err != nil || inserted == 1 {
			return err
		}
		existing, err = loadStoredJobByRequest(txApp, user.Id, input.RequestID)
		if err != nil {
			return err
		}
		if existing.Kind != administratorBackupJobKind || existing.RequestHash != requestHash {
			identityReused = true
			return nil
		}
		jobID, replayed = existing.ID, true
		return nil
	})
	if identityReused {
		return apiError(event, http.StatusConflict, errorCodeAdministratorBackupIdentityReused, "The job request ID was used for a different job.")
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAdministratorBackupCreateFailed, "Couldn't start the backup.", "administrator-backup.create", "admission", errorCodeAdministratorBackupCreateFailed, err)
	}
	r.notify()
	job, err := r.administratorBackupResponse(jobID, user.Id)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorBackupReadFailed, "Couldn't read the backup state.", err)
	}
	status := http.StatusAccepted
	if replayed {
		status = http.StatusOK
	}
	return event.JSON(status, administratorBackupJobResult{Replayed: replayed, Job: job})
}

func (r *durableJobRunner) getAdministratorBackup(event *core.RequestEvent) error {
	user, ok := authorizeAdministratorBackupRequest(event)
	if !ok {
		return nil
	}
	job, err := r.administratorBackupResponse(strings.TrimSpace(event.Request.PathValue("jobId")), user.Id)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeAdministratorBackupNotFound, "Backup not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorBackupReadFailed, "Couldn't read the backup state.", err)
	}
	return event.JSON(http.StatusOK, administratorBackupJobResult{Job: job})
}

func (r *durableJobRunner) getCurrentAdministratorBackup(event *core.RequestEvent) error {
	user, ok := authorizeAdministratorBackupRequest(event)
	if !ok {
		return nil
	}
	rows := []readyJob{}
	err := event.App.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_jobs}}
		WHERE [[owner]] = {:owner} AND [[kind]] = {:kind}
		ORDER BY [[updated]] DESC, [[id]] DESC LIMIT 1
	`).Bind(dbx.Params{"owner": user.Id, "kind": administratorBackupJobKind}).All(&rows)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorBackupReadFailed, "Couldn't read the backup state.", err)
	}
	result := administratorBackupCurrentResult{}
	result.RestoreRuns, result.RestoreProgram = administratorBackupRestoreWay(event.App)
	if len(rows) == 0 {
		return event.JSON(http.StatusOK, result)
	}
	job, err := r.administratorBackupResponse(rows[0].ID, user.Id)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorBackupReadFailed, "Couldn't read the backup state.", err)
	}
	result.Job = &job
	// A backup made before stays downloadable while its file is kept, under a
	// later one cancelled, failed or still being made (2026-11-07: the row
	// showed only "취소됨" and no way to the file made a minute before).
	if job.Status != administratorBackupStatusCompleted {
		if finished, kept := r.keptAdministratorBackup(event.App, user.Id); kept {
			result.Finished = &finished
		}
	}
	return event.JSON(http.StatusOK, result)
}

// keptAdministratorBackup is the newest finished backup whose file the
// server still keeps.
func (r *durableJobRunner) keptAdministratorBackup(app core.App, owner string) (administratorBackupJob, bool) {
	artifacts := []storedAdministratorBackupArtifact{}
	if err := app.DB().NewQuery(`
		SELECT [[job_id]], [[owner]], [[filename]], [[bytes]], [[sha256]], [[created_at]]
		FROM {{leafen_administrator_backup_artifacts}}
		WHERE [[owner]] = {:owner} ORDER BY [[created_at]] DESC LIMIT 5
	`).Bind(dbx.Params{"owner": owner}).All(&artifacts); err != nil || len(artifacts) == 0 {
		return administratorBackupJob{}, false
	}
	backups, err := app.NewBackupsFilesystem()
	if err != nil {
		return administratorBackupJob{}, false
	}
	defer backups.Close()
	for _, artifact := range artifacts {
		if exists, err := backups.Exists(artifact.Filename); err != nil || !exists {
			continue
		}
		if job, err := r.administratorBackupResponse(artifact.JobID, owner); err == nil && job.Status == administratorBackupStatusCompleted {
			return job, true
		}
	}
	return administratorBackupJob{}, false
}

func (r *durableJobRunner) retryAdministratorBackup(event *core.RequestEvent) error {
	return r.controlAdministratorBackup(event, true)
}

func (r *durableJobRunner) cancelAdministratorBackup(event *core.RequestEvent) error {
	return r.controlAdministratorBackup(event, false)
}

func (r *durableJobRunner) controlAdministratorBackup(event *core.RequestEvent, retry bool) error {
	user, ok := authorizeAdministratorBackupRequest(event)
	if !ok {
		return nil
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	authorize := func(app core.App) error { return requireAdministrator(app, user.Id) }
	var outcome storedJobControlOutcome
	var err error
	if retry {
		outcome, err = r.retryOwnedStoredJob(event.App, jobID, user.Id, administratorBackupJobKind, "", authorize, nil)
	} else {
		outcome, err = r.cancelOwnedStoredJob(event.App, jobID, user.Id, administratorBackupJobKind, "", authorize)
	}
	if outcome.missing {
		return apiError(event, http.StatusNotFound, errorCodeAdministratorBackupNotFound, "Backup not found.")
	}
	if outcome.unavailable {
		code, message := errorCodeAdministratorBackupCancelUnavailable, "This backup can't be canceled."
		if retry {
			code, message = errorCodeAdministratorBackupRetryUnavailable, "This backup can't be retried."
		}
		return apiError(event, http.StatusConflict, code, message)
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorBackupCreateFailed, "Couldn't change the backup state.", err)
	}
	if retry {
		r.notify()
	}
	job, err := r.administratorBackupResponse(jobID, user.Id)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorBackupReadFailed, "Couldn't read the backup state.", err)
	}
	status := http.StatusOK
	if retry {
		status = http.StatusAccepted
	}
	return event.JSON(status, administratorBackupJobResult{Job: job})
}

func authorizeAdministratorBackupRequest(event *core.RequestEvent) (*core.Record, bool) {
	if !authorizeAdministratorRequest(event) {
		return nil, false
	}
	user, ok := authenticatedUser(event)
	return user, ok
}

func requireAdministrator(app core.App, owner string) error {
	administrator, err := isAdministrator(app, owner)
	if err != nil {
		return err
	}
	if !administrator {
		return errors.New(errorCodeAdministratorNotFound)
	}
	return nil
}

func (r *durableJobRunner) administratorBackupResponse(jobID, owner string) (administratorBackupJob, error) {
	job, err := loadStoredJob(r.app, jobID, owner)
	if err != nil {
		return administratorBackupJob{}, err
	}
	if job.Kind != administratorBackupJobKind {
		return administratorBackupJob{}, sql.ErrNoRows
	}
	payload := administratorBackupJobPayload{}
	if json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil || payload.FormatVersion != backupFormatVersion {
		if job.Status != durableJobStatusQueued {
			return administratorBackupJob{}, errors.New(errorCodeAdministratorBackupReadFailed)
		}
		// The runner durably normalizes malformed queued payloads to a failed
		// state. Until that commit wins, report the actual queued state instead
		// of making status unreadable during the restart race.
		payload = administratorBackupJobPayload{
			FormatVersion: backupFormatVersion,
			Phase:         administratorBackupPhaseSnapshot,
		}
	}
	artifact, artifactErr := loadAdministratorBackupArtifact(r.app, job.ID, owner)
	if artifactErr != nil && !errors.Is(artifactErr, sql.ErrNoRows) {
		return administratorBackupJob{}, artifactErr
	}
	status := administratorBackupStatusPending
	switch job.Status {
	case durableJobStatusQueued:
		if payload.Phase != administratorBackupPhaseSnapshot {
			status = administratorBackupStatusRunning
		}
	case durableJobStatusCompleted:
		status = administratorBackupStatusCompleted
	case durableJobStatusFailed:
		status = administratorBackupStatusFailed
	case durableJobStatusCanceled:
		status = administratorBackupStatusCanceled
	default:
		return administratorBackupJob{}, errors.New(errorCodeAdministratorBackupReadFailed)
	}
	createdAt := time.UnixMilli(job.Created).UTC().Format(time.RFC3339Nano)
	updatedAt := time.UnixMilli(job.Updated).UTC().Format(time.RFC3339Nano)
	var completedAt *string
	if artifactErr == nil {
		value := time.UnixMilli(artifact.CreatedAt).UTC().Format(time.RFC3339Nano)
		completedAt = &value
	}
	return administratorBackupJob{
		ID: job.ID, RequestID: job.RequestID, Status: status, Phase: payload.Phase,
		ArtifactBytes: artifact.Bytes, ArtifactSHA256: artifact.SHA256,
		ErrorCode: job.ErrorCode, DiagnosticReference: job.DiagnosticReference, ErrorReason: payload.Reason,
		CanRetry:  job.Status == durableJobStatusFailed,
		CanCancel: job.Status == durableJobStatusQueued,
		CreatedAt: createdAt, UpdatedAt: updatedAt, CompletedAt: completedAt,
		// How far its file has got while it is made (administrator_backup_progress.go).
		Progress: administratorBackupProgressOf(r.app, job.ID),
	}, nil
}

func loadAdministratorBackupArtifact(app core.App, jobID, owner string) (storedAdministratorBackupArtifact, error) {
	artifact := storedAdministratorBackupArtifact{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]], [[owner]], [[filename]], [[bytes]], [[sha256]], [[created_at]]
		FROM {{leafen_administrator_backup_artifacts}}
		WHERE [[job_id]] = {:jobId} AND [[owner]] = {:owner}
	`).Bind(dbx.Params{"jobId": jobID, "owner": owner}).One(&artifact)
	return artifact, err
}

func administratorBackupRequestHash(input administratorBackupInput) string {
	digest := sha256.Sum256([]byte(administratorBackupJobKind + "\x00" + input.RequestID + "\x00" + backupFormat))
	return hex.EncodeToString(digest[:])
}
