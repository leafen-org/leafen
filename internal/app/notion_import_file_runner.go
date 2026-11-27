package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/security"
)

func (r *durableJobRunner) discoverNotionFileChunk(
	ctx context.Context,
	job storedJob,
	payload notionImportJobPayload,
	client notionProviderClient,
) error {
	sourceID := payload.PendingFileIDs[0]
	task, err := loadNotionFileTask(r.app, job.ID, sourceID)
	if err != nil {
		return err
	}
	if task.Status == "copied" || task.Status == "omitted" {
		settleNotionFilePayload(&payload, sourceID)
		return r.commitNotionFileProgress(job, payload)
	}
	madeBefore, err := notionImportFileMadeBefore(r.app, job, payload, sourceID)
	if err != nil {
		return err
	}
	if madeBefore {
		if _, err := r.app.DB().NewQuery(`
			DELETE FROM {{leafen_notion_import_file_tasks}} WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId}
		`).Bind(dbx.Params{"jobId": job.ID, "sourceId": sourceID}).Execute(); err != nil {
			return err
		}
		settleNotionFilePayload(&payload, sourceID)
		return r.commitNotionFileProgress(job, payload)
	}
	runPlan, err := loadNotionMigrationRunPlan(r.app, job.Owner, payload)
	if err != nil {
		return err
	}
	if target, _ := notionImportPresentationFileTarget(sourceID); runPlan.skipsFiles() && !task.Blueprint && target == "" {
		settleNotionFilePayload(&payload, sourceID)
		return r.commitNotionFileOmitted(job, payload, sourceID, errorCodeNotionMigrationFileSkipped)
	}
	plainURL, err := decryptNotionFileURL(r.app, task.URLCipher)
	if err != nil {
		return err
	}
	fetched, fetchErr := fetchNotionFile(ctx, r.app, plainURL)
	var failure *notionFileFetchFailure
	if errors.As(fetchErr, &failure) && failure.kind == "refresh" {
		fetched, fetchErr = r.refreshAndFetchNotionFile(ctx, job, payload, sourceID, task.Kind, client)
	}
	if fetchErr != nil {
		if errors.As(fetchErr, &failure) && failure.kind == "permanent" {
			if task.Blueprint {
				return errors.New(failure.code)
			}
			settleNotionFilePayload(&payload, sourceID)
			return r.commitNotionFileOmitted(job, payload, sourceID, failure.code)
		}
		code := errorCodeNotionImportFileTransient
		if errors.As(fetchErr, &failure) {
			code = failure.code
		}
		if err := r.commitNotionFileRetryable(job, payload, sourceID, code); err != nil {
			return errors.Join(fetchErr, err)
		}
		return fetchErr
	}
	target, _ := notionImportPresentationFileTarget(sourceID)
	if !notionManagedFileContentCompatible(task.Kind, graphFileBlockContent{ContentType: fetched.ContentType}) ||
		target != "" && !strings.HasPrefix(strings.ToLower(fetched.ContentType), "image/") {
		failure := &notionFileFetchFailure{code: errorCodeNotionImportFilePermanent, kind: "permanent"}
		if task.Blueprint {
			return errors.New(failure.code)
		}
		settleNotionFilePayload(&payload, sourceID)
		return r.commitNotionFileOmitted(job, payload, sourceID, failure.code)
	}
	defer os.Remove(fetched.Path)
	settleNotionFilePayload(&payload, sourceID)
	return r.commitNotionFileCopied(job, payload, task, fetched)
}

func (r *durableJobRunner) refreshAndFetchNotionFile(
	ctx context.Context,
	job storedJob,
	payload notionImportJobPayload,
	sourceID string,
	blockKind string,
	client notionProviderClient,
) (notionFetchedFile, error) {
	target, owner := notionImportPresentationFileTarget(sourceID)
	_, _, _, propertyFile := notionImportPropertyFileTarget(sourceID)
	if target != "" || propertyFile {
		var rawURL, name string
		var err error
		if propertyFile {
			rawURL, name, err = refreshNotionImportPropertyFileURL(ctx, client, sourceID)
		} else {
			rawURL, name, err = refreshNotionImportPresentationFileURL(ctx, client, target, owner)
		}
		if err != nil {
			return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFileRefreshFailed, kind: "transient"}
		}
		key, err := notionImportEncryptionKey(r.app)
		if err != nil {
			return notionFetchedFile{}, err
		}
		cipher, err := security.Encrypt([]byte(rawURL), key)
		if err != nil {
			return notionFetchedFile{}, err
		}
		if err := r.updateNotionFileCipher(job, payload, sourceID, name, cipher); err != nil {
			return notionFetchedFile{}, err
		}
		return fetchNotionFile(ctx, r.app, rawURL)
	}
	block, err := client.block(ctx, sourceID)
	if err != nil || block.ID != sourceID || notionImportFileKind(block) != blockKind {
		return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFileRefreshFailed, kind: "transient"}
	}
	staged, err := prepareNotionFileSource(r.app, "", "", false, 0, block)
	if err != nil {
		return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFileRefreshFailed, kind: "transient"}
	}
	if err := r.updateNotionFileCipher(job, payload, sourceID, staged.Title, staged.FileURLCipher); err != nil {
		return notionFetchedFile{}, err
	}
	providerFile, ok := notionProviderBlockFile(block)
	if !ok || providerFile.Type != "file" {
		return notionFetchedFile{}, &notionFileFetchFailure{code: errorCodeNotionImportFileRefreshFailed, kind: "transient"}
	}
	return fetchNotionFile(ctx, r.app, providerFile.File.URL)
}

func loadNotionFileTask(app core.App, jobID, sourceID string) (storedNotionFileTask, error) {
	var task storedNotionFileTask
	err := app.DB().NewQuery(`
		SELECT [[task]].[[source_id]], [[source]].[[kind]], [[source]].[[blueprint]], [[task]].[[name]],
			[[task]].[[url_cipher]], [[task]].[[status]], [[task]].[[file_id]],
			[[task]].[[error_code]], [[task]].[[attempt]]
		FROM {{leafen_notion_import_file_tasks}} AS [[task]]
		JOIN {{leafen_notion_import_sources}} AS [[source]]
			ON [[source]].[[job_id]] = [[task]].[[job_id]]
			AND [[source]].[[source_id]] = [[task]].[[source_id]]
		WHERE [[task]].[[job_id]] = {:jobId} AND [[task]].[[source_id]] = {:sourceId}
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": sourceID}).One(&task)
	return task, err
}

// notionImportFileMadeBefore reports a file an earlier run of the migration
// brought and this one won't use: what holds it was made then, and the source
// hasn't changed its row's cell since (notion_migration_source_change.go). A
// run again copying it anew would only leave a copy nothing points at.
func notionImportFileMadeBefore(app core.App, job storedJob, payload notionImportJobPayload, sourceID string) (bool, error) {
	if payload.MigrationID == "" {
		return false, nil
	}
	holder, cell := sourceID, ""
	if rowID, propertyID, _, property := notionImportPropertyFileTarget(sourceID); property {
		holder, cell = rowID, propertyID
	} else if suffix, owner := notionImportPresentationFileTarget(sourceID); suffix != "" {
		holder = owner
	}
	made := []struct {
		Kind string `db:"kind"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[kind]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:holder} AND [[kind]] IN ('block','page','row','data_source')
	`).Bind(dbx.Params{"jobId": job.ID, "holder": holder}).All(&made); err != nil {
		return false, err
	}
	if len(made) == 0 {
		return false, nil
	}
	for _, mapping := range made {
		if mapping.Kind != "row" {
			continue
		}
		source, found, err := notionImportStagedSource(app, job.ID, holder)
		if err != nil {
			return false, err
		}
		if !found {
			continue
		}
		// A file is a staged cell's or the row's own, never a preserved
		// formula's text: the row compares as staged, without the plan.
		if change, err := notionMigrationRowChanged(app, payload, nil, source); err != nil ||
			change.Changed && (change.Cells == nil || cell == "" || change.Cells[cell]) {
			return false, err
		}
	}
	return true, nil
}

// notionImportFileName is the name Notion shows for a file, cut to what a file
// name holds; a hosted file's address spells it its own way, spaces as "_".
func notionImportFileName(task storedNotionFileTask, fetched notionFetchedFile) string {
	name := []rune(strings.TrimSpace(task.Name))
	if len(name) == 0 {
		return fetched.Name
	}
	return string(name[:min(len(name), maxGraphExternalFileNameRunes)])
}

func decryptNotionFileURL(app core.App, cipher string) (string, error) {
	key, err := notionImportEncryptionKey(app)
	if err != nil {
		return "", err
	}
	plain, err := security.Decrypt(cipher, key)
	return string(plain), err
}

func (r *durableJobRunner) updateNotionFileCipher(
	job storedJob,
	payload notionImportJobPayload,
	sourceID, name, cipher string,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if _, err := authorizeNotionImportTurn(txApp, current, payload); err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_import_file_tasks}}
			SET [[name]] = {:name}, [[url_cipher]] = {:cipher}, [[status]] = 'pending',
				[[error_code]] = '', [[updated]] = {:updated}
			WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
				AND [[status]] IN ('pending', 'retryable')
		`).Bind(dbx.Params{
			"jobId": current.ID, "sourceId": sourceID, "name": name,
			"cipher": cipher, "updated": time.Now().UTC().UnixMilli(),
		}).Execute()
		return err
	})
}

func (r *durableJobRunner) commitNotionFileCopied(
	job storedJob,
	payload notionImportJobPayload,
	task storedNotionFileTask,
	fetched notionFetchedFile,
) error {
	upload, err := filesystem.NewFileFromPath(fetched.Path)
	if err != nil {
		return err
	}
	nowMillis := time.Now().UTC().UnixMilli()
	quotaRelease, err := prepareProductOrganizationStorageAdmission(
		context.Background(), r.app, job.WorkspaceID, nowMillis,
	)
	if err != nil {
		return err
	}
	defer quotaRelease()
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if _, err := authorizeNotionImportTurn(txApp, current, payload); err != nil {
			return err
		}
		collection, err := txApp.FindCollectionByNameOrId(filesCollection)
		if err != nil {
			return err
		}
		record := core.NewRecord(collection)
		record.Id = core.GenerateDefaultRandomId()
		if _, err := reserveProductOrganizationStorage(
			context.Background(), txApp, job.WorkspaceID, record.Id, fetched.Size, nowMillis,
		); err != nil {
			return err
		}
		setWorkspaceFileRecordAuthority(record, job.WorkspaceID, payload.AuthorityOwnerID)
		record.Set("original_name", notionImportFileName(task, fetched))
		record.Set("content_type", fetched.ContentType)
		record.Set("size", fetched.Size)
		record.Set("sha256", fetched.SHA256)
		record.Set("blob", upload)
		if err := txApp.Save(record); err != nil {
			return err
		}
		if err := settleProductFileStorageIdentity(txApp, record); err != nil {
			return err
		}
		now := time.Now().UTC()
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_import_file_tasks}}
			SET [[status]] = 'copied', [[file_id]] = {:fileId}, [[url_cipher]] = '',
				[[error_code]] = '', [[attempt]] = [[attempt]] + 1, [[updated]] = {:updated}
			WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
				AND [[status]] IN ('pending', 'retryable')
		`).Bind(dbx.Params{
			"jobId": job.ID, "sourceId": task.SourceID, "fileId": record.Id, "updated": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		return commitNotionFileJobProgress(txApp, current, payload, now)
	})
}

func (r *durableJobRunner) commitNotionFileOmitted(
	job storedJob,
	payload notionImportJobPayload,
	sourceID string,
	errorCode string,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if _, err := authorizeNotionImportTurn(txApp, current, payload); err != nil {
			return err
		}
		now := time.Now().UTC()
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_import_file_tasks}}
			SET [[status]] = 'omitted', [[file_id]] = '', [[url_cipher]] = '',
				[[error_code]] = {:errorCode}, [[attempt]] = [[attempt]] + 1, [[updated]] = {:updated}
			WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
				AND [[status]] IN ('pending', 'retryable')
		`).Bind(dbx.Params{
			"jobId": job.ID, "sourceId": sourceID, "errorCode": errorCode, "updated": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		return commitNotionFileJobProgress(txApp, current, payload, now)
	})
}

func (r *durableJobRunner) commitNotionFileRetryable(
	job storedJob,
	payload notionImportJobPayload,
	sourceID, errorCode string,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if _, err := authorizeNotionImportTurn(txApp, current, payload); err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_import_file_tasks}}
			SET [[status]] = 'retryable', [[error_code]] = {:errorCode},
				[[attempt]] = [[attempt]] + 1, [[updated]] = {:updated}
			WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
				AND [[status]] IN ('pending', 'retryable')
		`).Bind(dbx.Params{
			"jobId": current.ID, "sourceId": sourceID, "errorCode": errorCode,
			"updated": time.Now().UTC().UnixMilli(),
		}).Execute()
		return err
	})
}

func (r *durableJobRunner) commitNotionFileProgress(job storedJob, payload notionImportJobPayload) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if _, err := authorizeNotionImportTurn(txApp, current, payload); err != nil {
			return err
		}
		return commitNotionFileJobProgress(txApp, current, payload, time.Now().UTC())
	})
}

func commitNotionFileJobProgress(app core.App, job storedJob, payload notionImportJobPayload, now time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	chunkIndex := job.ChunkCount + 1
	if err := recordNotionImportChunk(app, job.ID, chunkIndex, 0, "", notionDiscoveryDigest(nil), now); err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[payload_json]] = {:payload}, [[chunk_count]] = {:chunkCount},
			[[not_before]] = {:notBefore}, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'
	`).Bind(dbx.Params{
		"id": job.ID, "payload": string(encoded), "chunkCount": chunkIndex,
		"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
	}).Execute()
	return err
}

func settleNotionFilePayload(payload *notionImportJobPayload, sourceID string) {
	if len(payload.PendingFileIDs) > 0 && payload.PendingFileIDs[0] == sourceID {
		payload.PendingFileIDs = payload.PendingFileIDs[1:]
	}
}
