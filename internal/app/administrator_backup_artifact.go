package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	administratorBackupContentType = "application/zip"
	// How many times the data folder is packed when a file vanishes from it.
	administratorBackupPackTries = 3
)

type expiredAdministratorBackupArtifact struct {
	JobID    string `db:"job_id"`
	Filename string `db:"filename"`
}

func (r *durableJobRunner) createAdministratorBackupArtifact(
	ctx context.Context,
	job storedJob,
	payload administratorBackupJobPayload,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := requireAdministrator(r.app, job.Owner); err != nil {
		return err
	}
	progress := startBackupProgress(r.app, backupFileProgressStoreKey, backupProgressKindFile, job.ID)
	artifact, err := buildAdministratorBackupArtifact(withBackupProgress(ctx, progress), r.app, job)
	endBackupProgress(r.app, backupFileProgressStoreKey, progress)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = deleteAdministratorBackupFile(r.app, artifact.Filename)
		}
	}()
	err = r.app.RunInTransaction(func(txApp core.App) error {
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
		currentPayload := administratorBackupJobPayload{}
		if json.Unmarshal([]byte(current.PayloadJSON), &currentPayload) != nil ||
			currentPayload.Phase != administratorBackupPhasePublish ||
			currentPayload.FormatVersion != backupFormatVersion {
			return errors.New("administrator backup publication state changed")
		}
		now := r.clock.now()
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_administrator_backup_artifacts}}
				([[job_id]], [[owner]], [[format]], [[format_version]], [[filename]],
				 [[content_type]], [[bytes]], [[sha256]], [[created_at]])
			VALUES
				({:jobId}, {:owner}, {:format}, {:formatVersion}, {:filename},
				 {:contentType}, {:bytes}, {:sha256}, {:createdAt})
			ON CONFLICT ([[job_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"jobId": current.ID, "owner": current.Owner,
			"format": backupFormat, "formatVersion": backupFormatVersion,
			"filename": artifact.Filename, "contentType": administratorBackupContentType,
			"bytes": artifact.Bytes, "sha256": artifact.SHA256, "createdAt": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		currentPayload.Phase = administratorBackupPhaseCompleted
		encoded, err := json.Marshal(currentPayload)
		if err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = 'completed', [[payload_json]] = {:payload},
				[[processed_items]] = [[total_items]], [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{"id": current.ID, "payload": string(encoded), "updated": now.UnixMilli()}).Execute()
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err == nil && updated == 1 {
			keep = true
		}
		return err
	})
	return err
}

func buildAdministratorBackupArtifact(
	ctx context.Context,
	app core.App,
	job storedJob,
) (storedAdministratorBackupArtifact, error) {
	filename := "leafen_administrator_backup_" + job.ID + ".zip"
	progress := backupProgressFrom(ctx)
	backups, err := app.NewBackupsFilesystem()
	if err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	defer backups.Close()
	backups.SetContext(ctx)
	if exists, err := backups.Exists(filename); err != nil {
		return storedAdministratorBackupArtifact{}, err
	} else if exists {
		progress.restart(backupProgressStepCheck, 0, 0)
		return inspectAdministratorBackupFile(ctx, backups, filename, job)
	}

	rawName := "leafen_administrator_raw_" + security.RandomString(16) + ".zip"
	stopWatching := watchAdministratorBackupArchive(ctx, app, progress)
	err = createBackupUnderLease(ctx, app, rawName)
	// PocketBase's packing fails on a file deleted while it goes through the
	// data folder (2026-11-22); packed again, that file is no longer there.
	for tries := 1; tries < administratorBackupPackTries && errors.Is(err, fs.ErrNotExist) && ctx.Err() == nil; tries++ {
		progress.setDone(0)
		err = createBackupUnderLease(ctx, app, rawName)
	}
	stopWatching()
	if err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	defer backups.Delete(rawName)
	rawReader, err := backups.GetReader(rawName)
	if err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	defer rawReader.Close()

	staging, err := os.MkdirTemp("", "leafen-administrator-backup-")
	if err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	defer os.RemoveAll(staging)
	rawPath := filepath.Join(staging, "raw.zip")
	rawFile, err := os.OpenFile(rawPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	// Copied out, then written again as Leafen's: two passes over the
	// packed bytes, each counting half.
	progress.restart(backupProgressStepWrite, rawReader.Size(), 0)
	_, copyErr := io.Copy(rawFile, progress.passReader(rawReader))
	closeErr := errors.Join(rawFile.Sync(), rawFile.Close())
	if err := errors.Join(copyErr, closeErr); err != nil {
		return storedAdministratorBackupArtifact{}, err
	}

	versionedPath := filepath.Join(staging, filename)
	versioned, err := os.OpenFile(versionedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	writeErr := writeVersionedBackupArchive(rawPath, progress.passWriter(versioned))
	closeErr = errors.Join(versioned.Sync(), versioned.Close())
	if err := errors.Join(writeErr, closeErr); err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	if err := syncDirectory(staging); err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	progress.restart(backupProgressStepCheck, 0, 0)
	if err := validateBackupArchive(versionedPath); err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	digest, bytes, err := fileSHA256(versionedPath)
	if err != nil || bytes <= 0 {
		return storedAdministratorBackupArtifact{}, errors.Join(err, errors.New(errorCodeAdministratorBackupArtifactInvalid))
	}
	if exists, err := backups.Exists(filename); err != nil {
		return storedAdministratorBackupArtifact{}, err
	} else if exists {
		return inspectAdministratorBackupFile(ctx, backups, filename, job)
	}
	progress.restart(backupProgressStepKeep, 0, 0)
	upload, err := filesystem.NewFileFromPath(versionedPath)
	if err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	if err := backups.UploadFile(upload, filename); err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	return storedAdministratorBackupArtifact{
		JobID: job.ID, Owner: job.Owner, Filename: filename, Bytes: bytes, SHA256: digest,
	}, nil
}

func inspectAdministratorBackupFile(
	ctx context.Context,
	backups *filesystem.System,
	filename string,
	job storedJob,
) (storedAdministratorBackupArtifact, error) {
	path, cleanup, err := copyAdministratorBackupToTemporary(ctx, backups, filename)
	if err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	defer cleanup()
	if err := validateBackupArchive(path); err != nil {
		return storedAdministratorBackupArtifact{}, err
	}
	digest, bytes, err := fileSHA256(path)
	if err != nil || bytes <= 0 {
		return storedAdministratorBackupArtifact{}, errors.Join(err, errors.New(errorCodeAdministratorBackupArtifactInvalid))
	}
	return storedAdministratorBackupArtifact{
		JobID: job.ID, Owner: job.Owner, Filename: filename, Bytes: bytes, SHA256: digest,
	}, nil
}

func copyAdministratorBackupToTemporary(
	ctx context.Context,
	backups *filesystem.System,
	filename string,
) (string, func(), error) {
	backups.SetContext(ctx)
	reader, err := backups.GetReader(filename)
	if err != nil {
		return "", func() {}, err
	}
	defer reader.Close()
	directory, err := os.MkdirTemp("", "leafen-administrator-backup-read-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	path := filepath.Join(directory, "backup.zip")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	_, copyErr := io.Copy(file, reader)
	closeErr := errors.Join(file.Sync(), file.Close())
	if err := errors.Join(copyErr, closeErr); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := syncDirectory(directory); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

func deleteAdministratorBackupFile(app core.App, filename string) error {
	backups, err := app.NewBackupsFilesystem()
	if err != nil {
		return err
	}
	defer backups.Close()
	err = backups.Delete(filename)
	if errors.Is(err, filesystem.ErrNotFound) {
		return nil
	}
	return err
}

func cleanupExpiredAdministratorBackups(app core.App, now time.Time, retention time.Duration) error {
	expired := []expiredAdministratorBackupArtifact{}
	if err := app.DB().NewQuery(`
		SELECT [[job_id]], [[filename]] FROM {{leafen_administrator_backup_artifacts}}
		WHERE [[created_at]] < {:cutoff}
		ORDER BY [[created_at]], [[job_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"cutoff": now.Add(-retention).UnixMilli(), "limit": instanceProductExportCleanupRows,
	}).All(&expired); err != nil {
		return err
	}
	for _, artifact := range expired {
		if !validAdministratorBackupFilename(artifact.JobID, artifact.Filename) {
			return errors.New(errorCodeAdministratorBackupArtifactInvalid)
		}
		if err := deleteAdministratorBackupFile(app, artifact.Filename); err != nil {
			return err
		}
		if err := app.RunInTransaction(func(txApp core.App) error {
			if _, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_administrator_backup_artifacts}} WHERE [[job_id]] = {:jobId}
			`).Bind(dbx.Params{"jobId": artifact.JobID}).Execute(); err != nil {
				return err
			}
			_, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_jobs}} WHERE [[id]] = {:jobId} AND [[kind]] = {:kind}
			`).Bind(dbx.Params{"jobId": artifact.JobID, "kind": administratorBackupJobKind}).Execute()
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *durableJobRunner) downloadAdministratorBackup(event *core.RequestEvent) error {
	user, ok := authorizeAdministratorBackupRequest(event)
	if !ok {
		return nil
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	job, err := r.administratorBackupResponse(jobID, user.Id)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeAdministratorBackupNotFound, "Backup not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorBackupReadFailed, "Couldn't read the backup state.", err)
	}
	if job.Status != administratorBackupStatusCompleted {
		return apiError(event, http.StatusConflict, errorCodeAdministratorBackupNotReady, "Only a completed backup can be downloaded.")
	}
	artifact, err := loadAdministratorBackupArtifact(event.App, jobID, user.Id)
	if err != nil || !validAdministratorBackupFilename(jobID, artifact.Filename) {
		return apiError(event, http.StatusConflict, errorCodeAdministratorBackupArtifactInvalid, "Couldn't check the backup file.")
	}
	backups, err := event.App.NewBackupsFilesystem()
	if err != nil {
		return apiError(event, http.StatusConflict, errorCodeAdministratorBackupArtifactInvalid, "Couldn't check the backup file.")
	}
	defer backups.Close()
	path, cleanup, err := copyAdministratorBackupToTemporary(event.Request.Context(), backups, artifact.Filename)
	if err != nil {
		return apiError(event, http.StatusConflict, errorCodeAdministratorBackupArtifactInvalid, "Couldn't check the backup file.")
	}
	defer cleanup()
	digest, bytes, err := fileSHA256(path)
	if err != nil || bytes != artifact.Bytes || digest != artifact.SHA256 || validateBackupArchive(path) != nil {
		return apiError(event, http.StatusConflict, errorCodeAdministratorBackupArtifactInvalid, "Couldn't check the backup file.")
	}
	file, err := os.Open(path)
	if err != nil {
		return apiError(event, http.StatusConflict, errorCodeAdministratorBackupArtifactInvalid, "Couldn't check the backup file.")
	}
	defer file.Close()
	event.Response.Header().Set("Content-Type", administratorBackupContentType)
	event.Response.Header().Set("Content-Disposition", contentDisposition("attachment", artifact.Filename))
	event.Response.Header().Set("Content-Length", strconv.FormatInt(artifact.Bytes, 10))
	event.Response.Header().Set("ETag", fmt.Sprintf("\"sha256:%s\"", artifact.SHA256))
	event.Response.Header().Set("X-Content-Type-Options", "nosniff")
	event.Response.Header().Set("Cache-Control", "private, no-store")
	event.Response.WriteHeader(http.StatusOK)
	_, err = io.Copy(event.Response, file)
	return err
}

func validAdministratorBackupFilename(jobID, filename string) bool {
	return filename == "leafen_administrator_backup_"+jobID+".zip" && filepath.Base(filename) == filename
}
