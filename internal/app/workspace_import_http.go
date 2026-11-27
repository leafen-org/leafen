package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func (r *durableJobRunner) getWorkspaceImport(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
	} else if err != nil {
		return workspaceImportAPIError(event, err, "authorize-read")
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	response, err := r.workspaceImportResponse(jobID, user.Id, workspaceID, false)
	if errors.Is(err, sql.ErrNoRows) {
		message := "Import not found."
		if workspaceImportFromNotion(r.app, jobID, user.Id, workspaceID) {
			message = workspaceImportIsNotionImport
		}
		return apiError(event, http.StatusNotFound, errorCodeJobNotFound, message)
	}
	if err != nil {
		return workspaceImportAPIError(event, err, "read")
	}
	return event.JSON(http.StatusOK, response)
}

// listWorkspaceImports lists the person's imports into the workspace, newest
// first, as the settings' "가져오기 → 완료됨" lists them (Notion's, seen
// 2026-10-08): each with the title its first page has now. The workspace
// keeps a bounded number of finished ones, so the list stays short.
func (r *durableJobRunner) listWorkspaceImports(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
	} else if err != nil {
		return workspaceImportAPIError(event, err, "authorize-list")
	}
	ids := []string{}
	if err := r.app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_jobs}}
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId} AND [[kind]] = {:kind}
			AND COALESCE(json_extract([[payload_json]], '$.batchJobId'), '') = ''
		ORDER BY [[created]] DESC, [[id]] DESC LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": user.Id, "workspaceId": workspaceID, "kind": workspaceImportJobKind,
		"limit": workspaceImportTerminalRetentionPerWorkspace + 14,
	}).Column(&ids); err != nil {
		return workspaceImportAPIError(event, err, "list")
	}
	list := workspaceImportJobList{Jobs: make([]workspaceImportJob, 0, len(ids))}
	for _, id := range ids {
		job, err := r.workspaceImportResponse(id, user.Id, workspaceID, false)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return workspaceImportAPIError(event, err, "list")
		}
		if job.RootPageID != nil {
			var title string
			// A page deleted for good is out of everyone's sight, its import's
			// row too (page_deletion_hold.go).
			err := r.app.DB().NewQuery(`
				SELECT [[title]] FROM {{leafen_pages}} WHERE [[id]] = {:id} AND [[workspace]] = {:workspaceId}
					AND ` + pageOutsideDeletionFenceSQL("{:id}", "{:workspaceId}") + `
			`).Bind(dbx.Params{"id": *job.RootPageID, "workspaceId": workspaceID}).Row(&title)
			if err == nil {
				job.RootPageTitle = &title
			} else if errors.Is(err, sql.ErrNoRows) {
				job.RootPageID, job.RootPageIDs = nil, []string{}
			} else {
				return workspaceImportAPIError(event, err, "list")
			}
		}
		list.Jobs = append(list.Jobs, job)
	}
	return event.JSON(http.StatusOK, list)
}

func (r *durableJobRunner) cancelWorkspaceImport(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
	} else if err != nil {
		return workspaceImportAPIError(event, err, "authorize-cancel")
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	missing := false
	unavailable := false
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if _, authErr := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceWrite); authErr != nil {
			return authErr
		}
		job, loadErr := loadStoredJob(txApp, jobID, user.Id)
		if errors.Is(loadErr, sql.ErrNoRows) || (loadErr == nil && !ownedJobMatches(job, workspaceImportJobKind, workspaceID)) {
			missing = true
			return nil
		}
		if loadErr != nil {
			return loadErr
		}
		if job.Status == durableJobStatusCanceled {
			return nil
		}
		if job.Status != durableJobStatusQueued {
			unavailable = true
			return nil
		}
		payload, decodeErr := decodeWorkspaceImportPayload(job)
		if decodeErr != nil {
			return decodeErr
		}
		if payload.Phase == workspaceImportPhasePublishCleanup {
			unavailable = true
			return nil
		}
		payload.CancelRequested = true
		encoded, encodeErr := json.Marshal(payload)
		if encodeErr != nil {
			return encodeErr
		}
		now := r.clock.now().UnixMilli()
		_, updateErr := txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": job.ID, "owner": user.Id, "payload": string(encoded),
			"notBefore": now, "updated": now,
		}).Execute()
		return updateErr
	})
	if missing {
		return apiError(event, http.StatusNotFound, errorCodeJobNotFound, "Import not found.")
	}
	if unavailable {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportStateInvalid, "An import that has started or finished publishing can't be canceled.")
	}
	if err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
		}
		return workspaceImportAPIError(event, err, "cancel")
	}
	r.notify()
	response, err := r.workspaceImportResponse(jobID, user.Id, workspaceID, false)
	if err != nil {
		return workspaceImportAPIError(event, err, "cancel-response")
	}
	return event.JSON(http.StatusOK, response)
}

func (r *durableJobRunner) workspaceImportResponse(id, owner, workspaceID string, duplicate bool) (workspaceImportJob, error) {
	job, err := loadStoredJob(r.app, id, owner)
	if err != nil || !ownedJobMatches(job, workspaceImportJobKind, workspaceID) {
		if err == nil {
			err = sql.ErrNoRows
		}
		return workspaceImportJob{}, err
	}
	payload, err := decodeWorkspaceImportPayload(job)
	if err != nil {
		return workspaceImportJob{}, err
	}
	status := workspaceImportStatusQueued
	switch job.Status {
	case durableJobStatusQueued:
		// Ended, its cleanup waiting on its page deleted for good
		// (workspace_import_cleanup_deleted.go).
		if ended, endedErr := workspaceImportCleanupBehindDeletion(r.app, job, payload); endedErr != nil {
			return workspaceImportJob{}, endedErr
		} else if ended {
			status = workspaceImportEndedStatus(payload)
		} else if payload.CancelRequested {
			status = workspaceImportStatusCancelling
		} else if (job.ChunkCount > 0 || payload.Phase != workspaceImportPhaseStaged) && payload.Phase != workspaceImportPhaseBatchUpload {
			status = workspaceImportStatusRunning
		}
	case durableJobStatusCompleted:
		status = workspaceImportStatusCompleted
	case durableJobStatusCanceled:
		status = workspaceImportStatusCancelled
	case durableJobStatusFailed:
		status = workspaceImportStatusFailed
	default:
		return workspaceImportJob{}, workspaceImportStateError()
	}
	rootPageIDs := []string{}
	var rootPageID *string
	if status == workspaceImportStatusCompleted || payload.AppendToExistingPage || payload.ReplaceExistingPage {
		rootPageIDs = append(rootPageIDs, payload.RootPageIDs...)
		if primary := workspaceImportPrimaryRootPageID(payload); primary != "" {
			rootPageID = &primary
		}
	}
	title := ""
	if payload.Format == workspaceImportFormatFileBatch {
		title = payload.Title
	}
	return workspaceImportJob{
		ID: job.ID, WorkspaceID: job.WorkspaceID, RequestID: job.RequestID, Format: payload.Format, Title: title,
		Status: status, Phase: payload.Phase, ProcessedItems: job.Processed, TotalItems: job.Total,
		PageCount:           payload.ArchivePages,
		RootPageID:          rootPageID,
		RootPageIDs:         rootPageIDs,
		ErrorCode:           optionalWorkspaceImportString(job.ErrorCode),
		ErrorDetail:         workspaceImportJobErrorDetail(job, payload),
		DiagnosticReference: optionalWorkspaceImportString(job.DiagnosticReference),
		Duplicate:           duplicate, Created: job.Created, Updated: job.Updated,
		Warning: workspaceImportPlaceWarning(payload),
	}, nil
}

func decodeWorkspaceImportPayload(job storedJob) (workspaceImportJobPayload, error) {
	payload := workspaceImportJobPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil ||
		(payload.Format != workspaceImportFormatMarkdown && payload.Format != workspaceImportFormatCSV &&
			payload.Format != workspaceImportFormatNativeArchiveV2 && payload.Format != workspaceImportFormatFileBatch) {
		return workspaceImportJobPayload{}, workspaceImportStateError()
	}
	ordinary := payload.SchemaVersion == 1 && !payload.AppendToExistingPage && payload.Title != ""
	appendPayload := payload.SchemaVersion == 2 && payload.AppendToExistingPage &&
		payload.Format == workspaceImportFormatMarkdown && workspaceImportPrimaryRootPageID(payload) != ""
	replacePayload := payload.SchemaVersion == 3 && payload.ReplaceExistingPage && !payload.AppendToExistingPage &&
		payload.Format == workspaceImportFormatMarkdown && workspaceImportPrimaryRootPageID(payload) != ""
	mergePayload := payload.SchemaVersion == 4 && payload.AppendToExistingPage && !payload.ReplaceExistingPage &&
		payload.Format == workspaceImportFormatCSV && payload.MergeDataSourceID != "" && payload.MergeRows > 0 &&
		workspaceImportPrimaryRootPageID(payload) != ""
	if !ordinary && !appendPayload && !replacePayload && !mergePayload {
		return workspaceImportJobPayload{}, workspaceImportStateError()
	}
	if len(payload.RootPageIDs) == 0 && payload.RootPageID != "" {
		payload.RootPageIDs = []string{payload.RootPageID}
	}
	if payload.RootPageID == "" && len(payload.RootPageIDs) > 0 {
		payload.RootPageID = payload.RootPageIDs[0]
	}
	return payload, nil
}

func loadStoredWorkspaceImportSource(app core.App, jobID string) (storedWorkspaceImportSource, error) {
	source := storedWorkspaceImportSource{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]], [[schema_version]], [[format]], [[storage_key]], [[sha256]],
			[[source_bytes]], [[chunk_count]], [[chunk_hashes_json]],
			[[archive_storage_key]], [[archive_sha256]], [[archive_bytes]],
			[[file_index_storage_key]], [[file_index_sha256]], [[file_count]], [[file_bytes]]
		FROM {{leafen_workspace_import_sources}} WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": jobID}).One(&source)
	return source, err
}

func optionalWorkspaceImportString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

// workspaceImportJobErrorDetail is what a refused import says about where
// and what was wrong, alongside the error code it explains.
func workspaceImportJobErrorDetail(job storedJob, payload workspaceImportJobPayload) string {
	if job.ErrorCode == "" || payload.TerminalErrorCode != job.ErrorCode {
		return ""
	}
	return payload.TerminalErrorDetail
}
