package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

func (r *durableJobRunner) retryNotionImportFileOutcome(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	sourceID := strings.TrimSpace(event.Request.PathValue("sourceId"))
	var job storedJob
	var task storedNotionFileTask
	var payload notionImportJobPayload
	var token string
	var copiedOutcome notionImportFileOutcome
	readDisposition := ""
	// readErr is what failed a read the disposition names as an error.
	var readErr error
	err := withAuthorizedNotionImportRead(event.Request.Context(), event.App, user.Id, workspaceID, func(txApp core.App) error {
		job, readErr = loadStoredJob(txApp, jobID, user.Id)
		if errors.Is(readErr, sql.ErrNoRows) ||
			(readErr == nil && (job.WorkspaceID != workspaceID || job.Kind != notionImportJobKind)) {
			readDisposition = "job-missing"
			return nil
		}
		if readErr != nil {
			readDisposition = "job-error"
			return nil
		}
		if job.Status != durableJobStatusCompleted {
			readDisposition = "job-unavailable"
			return nil
		}
		task, readErr = loadNotionFileTask(txApp, job.ID, sourceID)
		if errors.Is(readErr, sql.ErrNoRows) {
			readDisposition = "task-missing"
			return nil
		}
		if readErr != nil {
			readDisposition = "task-error"
			return nil
		}
		if task.Status == "copied" {
			copiedOutcome = notionImportFileOutcomeFromStored(txApp, job.ID, task)
			readDisposition = "copied"
			return nil
		}
		if task.Status != "omitted" && task.Status != "retryable" {
			readDisposition = "task-unavailable"
			return nil
		}
		if readErr = decodeNotionImportPayload(job.PayloadJSON, &payload); readErr != nil {
			readDisposition = "payload-error"
			return nil
		}
		connection, connectionErr := loadNotionConnection(txApp, payload.ConnectionID, job.Owner, workspaceID)
		if connectionErr != nil {
			readDisposition = "connection-missing"
			return nil
		}
		token, readErr = decryptNotionConnection(txApp, connection)
		if readErr != nil {
			readDisposition = "connection-error"
			return nil
		}
		readDisposition = "provider"
		return nil
	})
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	} else if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportFileCopyRequired, "Couldn't check the file recovery permission.", err)
	}
	switch readDisposition {
	case "job-missing":
		return apiError(event, http.StatusNotFound, errorCodeNotionImportNotFound, "Import job not found.")
	case "job-error", "job-unavailable":
		return apiError(event, http.StatusConflict, errorCodeNotionImportFileCopyRequired, "Only files from a completed import can be recovered.")
	case "task-missing":
		return apiError(event, http.StatusNotFound, errorCodeNotionImportNotFound, "File result not found.")
	case "task-error":
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportFileCopyRequired, "Couldn't read the file results.", readErr)
	case "copied":
		return event.JSON(http.StatusOK, copiedOutcome)
	case "task-unavailable":
		return apiError(event, http.StatusConflict, errorCodeNotionImportFileCopyRequired, "This file can't be retried.")
	case "payload-error":
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportFileCopyRequired, "Couldn't read the file recovery state.", readErr)
	case "connection-missing":
		return apiError(event, http.StatusConflict, errorCodeNotionImportConnectionNotFound, "Notion connection not found.")
	case "connection-error":
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportFileCopyRequired, "Couldn't read the Notion connection.", readErr)
	}
	client := r.notionProviderClient(payload.ConnectionID, token)
	// An icon, cover or files value is read again from the page, database or
	// row that owns it.
	if notionImportOwnedFileSource(sourceID) {
		return r.retryNotionImportOwnedFile(event, user.Id, job, payload, task, client)
	}
	block, providerErr := client.block(event.Request.Context(), sourceID)
	if err := authorizeNotionImportPublication(event.App, user.Id, workspaceID); isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	} else if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportFileCopyRequired, "Couldn't check the file recovery permission.", err)
	}
	providerFile, fileBlock := notionProviderBlockFile(block)
	if providerErr != nil || block.ID != sourceID || notionImportFileKind(block) != task.Kind || !fileBlock || providerFile.Type != "file" {
		return apiError(event, http.StatusConflict, errorCodeNotionImportFilePermanent, "The Notion source file isn't valid yet.")
	}
	fetched, err := fetchNotionFile(event.Request.Context(), event.App, providerFile.File.URL)
	if err != nil {
		return retryNotionFileFetchError(event, job, payload, task, err)
	}
	defer os.Remove(fetched.Path)
	if !notionManagedFileContentCompatible(task.Kind, graphFileBlockContent{ContentType: fetched.ContentType}) {
		return apiError(event, http.StatusConflict, errorCodeNotionImportFilePermanent, "The Notion source file isn't valid yet.")
	}
	outcome, err := r.commitRecoveredNotionFile(event.App, job, payload, task, fetched)
	if err != nil {
		if quotaError, ok := organizationStorageAPIError(
			event, err, "notion_import.file", "recover-quota",
		); ok {
			return quotaError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportFileCopyRequired, "Couldn't recover the file.", err)
	}
	return event.JSON(http.StatusOK, outcome)
}

func decodeNotionImportPayload(raw string, payload *notionImportJobPayload) error {
	return json.Unmarshal([]byte(raw), payload)
}

func retryNotionFileFetchError(
	event *core.RequestEvent,
	job storedJob,
	payload notionImportJobPayload,
	task storedNotionFileTask,
	fetchErr error,
) error {
	var failure *notionFileFetchFailure
	code := errorCodeNotionImportFileTransient
	status := "retryable"
	if errors.As(fetchErr, &failure) {
		code = failure.code
		if failure.kind == "permanent" {
			status = "omitted"
		}
	}
	stateErr := event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizeNotionImportTurn(txApp, job, payload); err != nil {
			return err
		}
		_, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_import_file_tasks}}
			SET [[status]] = {:status}, [[error_code]] = {:errorCode}, [[url_cipher]] = '',
				[[attempt]] = [[attempt]] + 1, [[updated]] = {:updated}
			WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[status]] IN ('omitted', 'retryable')
		`).Bind(dbx.Params{
			"jobId": job.ID, "sourceId": task.SourceID, "status": status,
			"errorCode": code, "updated": time.Now().UTC().UnixMilli(),
		}).Execute()
		return err
	})
	if isPermissionDenied(stateErr) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if stateErr != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportFileCopyRequired, "Couldn't save the file recovery state.", stateErr)
	}
	return apiError(event, http.StatusConflict, code, "The Notion source file isn't valid yet.")
}

// commitRecoveredNotionFile keeps a file a person's retry fetched again,
// through app, the request's (request_write_stop.go).
func (r *durableJobRunner) commitRecoveredNotionFile(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
	task storedNotionFileTask,
	fetched notionFetchedFile,
) (notionImportFileOutcome, error) {
	upload, err := filesystem.NewFileFromPath(fetched.Path)
	if err != nil {
		return notionImportFileOutcome{}, err
	}
	nowMillis := time.Now().UTC().UnixMilli()
	quotaRelease, err := prepareProductOrganizationStorageAdmission(
		context.Background(), r.app, job.WorkspaceID, nowMillis,
	)
	if err != nil {
		return notionImportFileOutcome{}, err
	}
	defer quotaRelease()
	err = app.RunInTransaction(func(txApp core.App) error {
		record, err := storeRecoveredNotionFile(txApp, job, payload, task, upload, fetched, nowMillis)
		if err != nil || record == nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		var source storedNotionSource
		if err := txApp.DB().NewQuery(`
			SELECT [[sequence]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]], [[text]]
			FROM {{leafen_notion_import_sources}} WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
		`).Bind(dbx.Params{"jobId": job.ID, "sourceId": task.SourceID}).One(&source); err != nil {
			return err
		}
		structuralDocuments := map[string]*structuralMutationBatch{}
		applyMappings, err := loadNotionApplyMappings(txApp, job.ID, []storedNotionSource{source})
		if err != nil {
			return err
		}
		if copied, _, _, err := applyNotionFile(txApp, job, payload, source, structuralDocuments, applyMappings); err != nil || !copied {
			return errors.Join(err, errors.New(errorCodeNotionImportFileCopyRequired))
		}
		if err := saveStructuralMutationBatches(txApp, job.Owner, structuralDocuments); err != nil {
			return err
		}
		if err := applyMappings.flush(txApp, job.ID); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_import_reports}}
			SET [[blocks]] = [[blocks]] + 1, [[files_copied]] = [[files_copied]] + 1,
				[[files_omitted]] = CASE WHEN [[files_omitted]] > 0 THEN [[files_omitted]] - 1 ELSE 0 END,
				[[updated]] = {:updated}
			WHERE [[job_id]] = {:jobId}
		`).Bind(dbx.Params{"jobId": job.ID, "updated": now}).Execute(); err != nil {
			return err
		}
		return forgetNotionMigrationFileIssue(txApp, payload.MigrationID, task.SourceID)
	})
	if err != nil {
		return notionImportFileOutcome{}, err
	}
	task, err = loadNotionFileTask(r.app, job.ID, task.SourceID)
	if err != nil {
		return notionImportFileOutcome{}, err
	}
	return notionImportFileOutcomeFromStored(r.app, job.ID, task), nil
}

// storeRecoveredNotionFile keeps a recovered Notion file in the workspace and
// marks its task copied; it returns nil when the task was copied meanwhile.
func storeRecoveredNotionFile(
	txApp core.App,
	job storedJob,
	payload notionImportJobPayload,
	task storedNotionFileTask,
	upload *filesystem.File,
	fetched notionFetchedFile,
	nowMillis int64,
) (*core.Record, error) {
	if _, err := authorizeNotionImportTurn(txApp, job, payload); err != nil {
		return nil, err
	}
	currentTask, err := loadNotionFileTask(txApp, job.ID, task.SourceID)
	if err != nil || currentTask.Status == "copied" {
		return nil, err
	}
	collection, err := txApp.FindCollectionByNameOrId(filesCollection)
	if err != nil {
		return nil, err
	}
	record := core.NewRecord(collection)
	record.Id = core.GenerateDefaultRandomId()
	if _, err := reserveProductOrganizationStorage(
		context.Background(), txApp, job.WorkspaceID, record.Id, fetched.Size, nowMillis,
	); err != nil {
		return nil, err
	}
	setWorkspaceFileRecordAuthority(record, job.WorkspaceID, payload.AuthorityOwnerID)
	record.Set("original_name", notionImportFileName(task, fetched))
	record.Set("content_type", fetched.ContentType)
	record.Set("size", fetched.Size)
	record.Set("sha256", fetched.SHA256)
	record.Set("blob", upload)
	if err := txApp.Save(record); err != nil {
		return nil, err
	}
	if err := settleProductFileStorageIdentity(txApp, record); err != nil {
		return nil, err
	}
	if _, err := txApp.DB().NewQuery(`
		UPDATE {{leafen_notion_import_file_tasks}}
		SET [[status]] = 'copied', [[file_id]] = {:fileId}, [[url_cipher]] = '',
			[[error_code]] = '', [[attempt]] = [[attempt]] + 1, [[updated]] = {:updated}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[status]] IN ('omitted', 'retryable')
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourceId": task.SourceID, "fileId": record.Id, "updated": time.Now().UTC().UnixMilli(),
	}).Execute(); err != nil {
		return nil, err
	}
	return record, nil
}

func notionImportFileOutcomeFromStored(app core.App, jobID string, task storedNotionFileTask) notionImportFileOutcome {
	var parent struct {
		PageID    string `db:"page_id"`
		PageTitle string `db:"page_title"`
	}
	_ = app.DB().NewQuery(`
		SELECT COALESCE(mapping.[[native_id]], '') AS [[page_id]], parent.[[title]] AS [[page_title]]
		FROM {{leafen_notion_import_sources}} source
		JOIN {{leafen_notion_import_sources}} parent
			ON parent.[[job_id]] = source.[[job_id]] AND parent.[[source_id]] = source.[[parent_source_id]]
		LEFT JOIN {{leafen_notion_import_mappings}} mapping
			ON mapping.[[job_id]] = parent.[[job_id]] AND mapping.[[source_id]] = parent.[[source_id]] AND mapping.[[kind]] = 'page'
		WHERE source.[[job_id]] = {:jobId} AND source.[[source_id]] = {:sourceId}
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": task.SourceID}).One(&parent)
	return notionImportFileOutcome{
		SourceID: task.SourceID, Status: task.Status, ErrorCode: task.ErrorCode,
		FileID: task.FileID, PageID: parent.PageID, PageTitle: parent.PageTitle,
	}
}
