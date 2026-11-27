package app

import (
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func (r *durableJobRunner) prepareWorkspaceArchiveImportUpload(event *core.RequestEvent) error {
	user, workspaceID, ok, err := authorizeWorkspaceArchiveUpload(event, "archive-upload-prepare")
	if !ok {
		return err
	}
	input := workspaceArchiveImportUploadPrepareInput{}
	if err := event.BindBody(&input); err != nil {
		return workspaceArchiveUploadInvalid(event)
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Filename = strings.TrimSpace(input.Filename)
	input.SHA256 = strings.ToLower(strings.TrimSpace(input.SHA256))
	if !validWorkspaceArchiveUploadInput(input) {
		return workspaceArchiveUploadInvalid(event)
	}
	if existing, found, err := r.workspaceArchiveUploadJob(
		user.Id, workspaceID, input.RequestID, input.SHA256,
	); err != nil {
		return workspaceArchiveUploadReconnectError(event, err, "archive-upload-reconnect")
	} else if found {
		state := workspaceArchiveUploadStateFromJob(input, workspaceID, existing)
		return event.JSON(http.StatusOK, state)
	}
	if fresh, err := workspaceImportArchiveTargetFresh(event.App, workspaceID); err != nil {
		return workspaceImportAPIError(event, err, "archive-upload-freshness")
	} else if !fresh {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportTargetNotFresh, "A Leafen archive can only be imported into an empty workspace.")
	}
	uploadInput := fileUploadPrepareInput{
		RequestID: input.RequestID, WorkspaceID: workspaceID, Name: input.Filename,
		Size: input.Bytes, ContentType: workspaceExportArchiveMIME,
	}
	if err := normalizeProductFileUploadInput(&uploadInput); err != nil {
		return workspaceArchiveUploadInvalid(event)
	}
	release := acquireProductFileOperationLock(
		productFileOperationScopedNamespace(productFileOperationUploadRequest, user.Id),
		input.RequestID,
	)
	defer release()
	upload, created, err := admitProductFileUploadKind(
		event.App, user.Id, uploadInput, fileUploadKindWorkspaceArchive, input.SHA256,
	)
	if err != nil {
		return fileUploadError(event, err, "archive-prepare")
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	return event.JSON(status, workspaceArchiveUploadState(upload, !created, nil))
}

func (r *durableJobRunner) putWorkspaceArchiveImportUploadPart(event *core.RequestEvent) error {
	user, workspaceID, ok, err := authorizeWorkspaceArchiveUpload(event, "archive-upload-part")
	if !ok {
		return err
	}
	uploadID, partNumber, digest, err := productFileUploadPartRequest(event)
	if err != nil {
		return fileUploadError(event, err, "archive-part-validate")
	}
	release := acquireProductFileOperationLock(productFileOperationResumable, uploadID)
	defer release()
	upload, err := loadAuthorizedProductFileUpload(event.App, user.Id, uploadID)
	if err != nil {
		return fileUploadError(event, err, "archive-part-authorize")
	}
	if upload.Kind != fileUploadKindWorkspaceArchive || upload.WorkspaceID != workspaceID ||
		upload.Status != fileUploadStatusPending {
		return fileUploadError(event, errFileUploadIncomplete, "archive-part-state")
	}
	expectedBytes, err := expectedProductFileUploadPartBytes(event.App, upload, partNumber)
	if err != nil {
		return fileUploadError(event, err, "archive-part-order")
	}
	updated, duplicate, err := persistProductFileUploadPart(
		event.App, upload, partNumber, expectedBytes, digest, event.Request.Body,
	)
	if err != nil {
		return fileUploadError(event, err, "archive-part-store")
	}
	return event.JSON(http.StatusOK, workspaceArchiveUploadState(updated, duplicate, nil))
}

func (r *durableJobRunner) completeWorkspaceArchiveImportUpload(event *core.RequestEvent) error {
	user, workspaceID, ok, err := authorizeWorkspaceArchiveUpload(event, "archive-upload-complete")
	if !ok {
		return err
	}
	input := workspaceArchiveImportUploadCompleteInput{}
	if err := event.BindBody(&input); err != nil {
		return workspaceArchiveUploadInvalid(event)
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	uploadID := strings.TrimSpace(event.Request.PathValue("uploadId"))
	if named, err := workspaceArchiveUploadNamed(event, input.RequestID, uploadID); !named {
		return err
	}
	if job, found, err := r.workspaceArchiveUploadJobByID(
		user.Id, workspaceID, input.RequestID, uploadID,
	); err != nil {
		return workspaceArchiveUploadReconnectError(event, err, "archive-upload-complete-reconnect")
	} else if found {
		return event.JSON(http.StatusOK, job)
	}

	release := acquireProductFileOperationLock(productFileOperationResumable, uploadID)
	defer release()
	upload, err := loadAuthorizedProductFileUpload(event.App, user.Id, uploadID)
	if err != nil {
		return fileUploadError(event, err, "archive-complete-authorize")
	}
	if upload.Kind == fileUploadKindWorkspaceArchive && upload.WorkspaceID == workspaceID && upload.RequestID != input.RequestID {
		return fileUploadError(event, errFileUploadOtherRequest, "archive-complete-request")
	}
	if upload.Kind != fileUploadKindWorkspaceArchive || upload.WorkspaceID != workspaceID ||
		upload.Status != fileUploadStatusPending {
		return fileUploadError(event, errFileUploadIncomplete, "archive-complete-state")
	}
	expectedParts := int((upload.TotalSize + fileTransferPartBytes - 1) / fileTransferPartBytes)
	if upload.ReceivedBytes != upload.TotalSize || upload.NextPart != expectedParts {
		return fileUploadError(event, errFileUploadIncomplete, "archive-complete-parts")
	}
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite)
	if err != nil {
		return workspaceImportAPIError(event, err, "archive-complete-reauthorize")
	}
	dlpScanner, err := newOrganizationDLPContentScanner(
		event.App, authorization.Organization.OrganizationID,
	)
	if err != nil {
		return writeDLPPolicyError(event, err, "workspace-archive-import")
	}
	return r.admitWorkspaceArchivePreprocessing(
		event, user.Id, workspaceID, input.RequestID, uploadID,
		workspaceImportArchiveUpload{
			StorageKey: uploadID + ".leafen.zip",
			SHA256:     upload.SHA256,
			Bytes:      upload.TotalSize,
		},
		dlpScanner, true, false,
	)
}

func (r *durableJobRunner) cancelWorkspaceArchiveImportUpload(event *core.RequestEvent) error {
	user, workspaceID, ok, err := authorizeWorkspaceArchiveUpload(event, "archive-upload-cancel")
	if !ok {
		return err
	}
	input := workspaceArchiveImportUploadCompleteInput{}
	if err := event.BindBody(&input); err != nil {
		return workspaceArchiveUploadInvalid(event)
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	uploadID := strings.TrimSpace(event.Request.PathValue("uploadId"))
	if named, err := workspaceArchiveUploadNamed(event, input.RequestID, uploadID); !named {
		return err
	}
	release := acquireProductFileOperationLock(productFileOperationResumable, uploadID)
	defer release()
	upload, err := loadAuthorizedProductFileUpload(event.App, user.Id, uploadID)
	if err != nil {
		return fileUploadError(event, err, "archive-cancel-authorize")
	}
	if upload.Kind == fileUploadKindWorkspaceArchive && upload.WorkspaceID == workspaceID && upload.RequestID != input.RequestID {
		return fileUploadError(event, errFileUploadOtherRequest, "archive-cancel-request")
	}
	if upload.Kind != fileUploadKindWorkspaceArchive || upload.WorkspaceID != workspaceID ||
		upload.Status != fileUploadStatusPending {
		return fileUploadError(event, errFileUploadIncomplete, "archive-cancel-state")
	}
	if err := markWorkspaceArchiveUploadForCleanup(event.App, upload); err != nil {
		return fileUploadError(event, err, "archive-cancel-cleanup")
	}
	state := workspaceArchiveUploadState(upload, false, nil)
	state.Status = workspaceArchiveImportUploadStatusCancelled
	return event.JSON(http.StatusOK, state)
}

// authorizeWorkspaceArchiveUpload is the person importing into a workspace; a
// refusal is answered here, ok false.
func authorizeWorkspaceArchiveUpload(
	event *core.RequestEvent,
	stage string,
) (*core.Record, string, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return nil, "", false, apiError(
			event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.",
		)
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite); err != nil {
		if isPermissionDenied(err) {
			return nil, "", false, apiError(
				event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.",
			)
		}
		return nil, "", false, workspaceImportAPIError(event, err, stage)
	}
	return user, workspaceID, true, nil
}

func workspaceArchiveUploadReconnectError(
	event *core.RequestEvent,
	err error,
	stage string,
) error {
	if errors.Is(err, errFileUploadRequestIDReused) {
		return fileUploadError(event, err, stage)
	}
	return workspaceImportAPIError(event, err, stage)
}

// validWorkspaceArchiveUploadInput takes any zip's name: an archive is known by
// what it holds, and a browser saving a name twice writes "… (1).zip".
func validWorkspaceArchiveUploadInput(input workspaceArchiveImportUploadPrepareInput) bool {
	return validProductFileUploadRequestID(input.RequestID) && input.Filename != "" &&
		filepath.Base(input.Filename) == input.Filename && strings.HasSuffix(strings.ToLower(input.Filename), ".zip") &&
		input.Bytes > 0 && input.Bytes <= maxNativeFileSize && sha256Pattern.MatchString(input.SHA256)
}

// workspaceArchiveUploadNamed is whether a call on an upload names it by an
// ID an upload may have and gives its requestId; false when it refused the
// call, err then that answer's write.
func workspaceArchiveUploadNamed(event *core.RequestEvent, requestID, uploadID string) (bool, error) {
	if !fileUploadIDPattern.MatchString(uploadID) {
		return false, apiError(event, http.StatusNotFound, errorCodeArchiveUploadNotFound, "Archive upload not found.")
	}
	if !validProductFileUploadRequestID(requestID) {
		apiErrorAbout(event, nil, apiErrorField{Name: "requestId", Problem: "required: the requestId the upload was prepared with"})
		return false, workspaceArchiveUploadInvalid(event)
	}
	return true, nil
}

func workspaceArchiveUploadInvalid(event *core.RequestEvent) error {
	return apiError(event, http.StatusBadRequest, errorCodeWorkspaceImportInvalid, "Check the Leafen archive details.")
}

func workspaceArchiveUploadState(
	upload storedProductFileUpload,
	duplicate bool,
	job *workspaceImportJob,
) workspaceArchiveImportUploadState {
	return workspaceArchiveImportUploadState{
		UploadID: upload.UploadID, WorkspaceID: upload.WorkspaceID, RequestID: upload.RequestID,
		Status:        workspaceArchiveImportUploadStatusUploading,
		ReceivedBytes: upload.ReceivedBytes, NextPart: upload.NextPart,
		PartBytes: fileTransferPartBytes, ArchiveBytes: upload.TotalSize,
		ArchiveSHA256: upload.SHA256, Duplicate: duplicate, Job: job,
	}
}

func workspaceArchiveUploadStateFromJob(
	input workspaceArchiveImportUploadPrepareInput,
	workspaceID string,
	job workspaceImportJob,
) workspaceArchiveImportUploadState {
	status := workspaceArchiveImportUploadStatusImporting
	switch job.Status {
	case workspaceImportStatusCompleted:
		status = workspaceArchiveImportUploadStatusCompleted
	case workspaceImportStatusCancelled:
		status = workspaceArchiveImportUploadStatusCancelled
	case workspaceImportStatusFailed:
		status = workspaceArchiveImportUploadStatusFailed
	}
	return workspaceArchiveImportUploadState{
		UploadID: job.ID, WorkspaceID: workspaceID, RequestID: input.RequestID,
		Status: status, ReceivedBytes: input.Bytes,
		NextPart:  int((input.Bytes + fileTransferPartBytes - 1) / fileTransferPartBytes),
		PartBytes: fileTransferPartBytes, ArchiveBytes: input.Bytes,
		ArchiveSHA256: input.SHA256, Duplicate: true, Job: &job,
	}
}

func (r *durableJobRunner) workspaceArchiveUploadJob(
	ownerID string,
	workspaceID string,
	requestID string,
	digest string,
) (workspaceImportJob, bool, error) {
	stored, err := loadStoredJobByRequest(r.app, ownerID, requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return workspaceImportJob{}, false, nil
	}
	if err != nil {
		return workspaceImportJob{}, false, err
	}
	if stored.Kind != workspaceImportJobKind || stored.WorkspaceID != workspaceID ||
		stored.RequestHash != workspaceImportArchiveRequestHash(workspaceID, digest) {
		return workspaceImportJob{}, false, errFileUploadRequestIDReused
	}
	job, err := r.workspaceImportResponse(stored.ID, ownerID, workspaceID, true)
	return job, err == nil, err
}

func (r *durableJobRunner) workspaceArchiveUploadJobByID(
	ownerID string,
	workspaceID string,
	requestID string,
	uploadID string,
) (workspaceImportJob, bool, error) {
	stored, err := loadStoredJobByRequest(r.app, ownerID, requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return workspaceImportJob{}, false, nil
	}
	if err != nil {
		return workspaceImportJob{}, false, err
	}
	if stored.Kind != workspaceImportJobKind || stored.WorkspaceID != workspaceID || stored.ID != uploadID {
		return workspaceImportJob{}, false, errFileUploadRequestIDReused
	}
	job, err := r.workspaceImportResponse(stored.ID, ownerID, workspaceID, true)
	return job, err == nil, err
}

func markWorkspaceArchiveUploadForCleanup(app core.App, upload storedProductFileUpload) error {
	now := time.Now().UTC().UnixMilli()
	updated, err := app.DB().Update(productFileUploadsTable, dbx.Params{
		"status": fileUploadStatusCleanupPending, "cleanup_due_at": now, "updated_at": now,
	}, dbx.HashExp{
		"actor_id": upload.ActorID, "request_id": upload.RequestID,
		"upload_id": upload.UploadID, "kind": fileUploadKindWorkspaceArchive,
		"status": fileUploadStatusPending,
	}).Execute()
	if err != nil {
		return err
	}
	count, err := updated.RowsAffected()
	if err != nil || count != 1 {
		return errors.Join(errFileUploadUnavailable, err)
	}
	notifyProductFileUploadCleanupAfterCommit(app)
	return nil
}
