package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

func uploadProductFileDirect(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	uploads, err := event.FindUploadedFiles("file")
	if event.Request.MultipartForm != nil {
		defer event.Request.MultipartForm.RemoveAll()
	}
	if err != nil || len(uploads) != 1 {
		return directFileUploadError(event, fileUploadRefused("the body is a multipart form with one file in its file field"), "direct-file")
	}
	upload := uploads[0]
	input := fileUploadPrepareInput{
		RequestID:   strings.TrimSpace(event.Request.FormValue("requestId")),
		WorkspaceID: strings.TrimSpace(event.Request.FormValue("workspaceId")),
		Name:        strings.TrimSpace(upload.OriginalName),
		Size:        upload.Size,
		ContentType: directProductFileContentType(event),
		PageID:      strings.TrimSpace(event.Request.FormValue("pageId")),
		Purpose:     strings.TrimSpace(event.Request.FormValue("purpose")),
	}
	if err := normalizeProductFileUploadInput(&input); err != nil {
		return directFileUploadError(event, err, "direct-validate")
	}
	if err := requireDataDiskRoom(event.App, input.Size); err != nil {
		return writeServerDiskLow(event, "file-upload", err)
	}
	if input.Size > fileTransferDirectBytes {
		return directFileUploadError(event, errFileUploadResumableRequired, "direct-validate")
	}
	digest, err := fileDigest(upload.Reader)
	if err != nil {
		return directFileUploadError(event, err, "direct-digest")
	}

	release := acquireProductFileOperationLock(
		productFileOperationScopedNamespace(productFileOperationUploadRequest, user.Id),
		input.RequestID,
	)
	defer release()
	receipt, created, err := admitDirectProductFileUpload(event.App, user.Id, input, digest)
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "file-upload-metadata")
	}
	if err != nil {
		return directFileUploadError(event, err, "direct-admit")
	}
	uploadRelease := acquireProductFileOperationLock(
		productFileOperationResumable, receipt.UploadID,
	)
	defer uploadRelease()
	if receipt.Status == fileUploadStatusCompleted {
		record, err := completedDirectProductFileUpload(event.App, receipt)
		if err != nil {
			return directFileUploadError(event, err, "direct-replay")
		}
		return event.JSON(http.StatusOK, fileRecordResponse(record))
	}
	record, err := publishDirectProductFileUpload(event.App, receipt, upload, digest)
	if err != nil {
		return directFileUploadError(event, err, "direct-publish")
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	return event.JSON(status, fileRecordResponse(record))
}

func directProductFileContentType(event *core.RequestEvent) string {
	if event.Request.MultipartForm != nil {
		if headers := event.Request.MultipartForm.File["file"]; len(headers) == 1 {
			if value := strings.TrimSpace(headers[0].Header.Get("Content-Type")); value != "" {
				return value
			}
		}
	}
	return "application/octet-stream"
}

func admitDirectProductFileUpload(
	app core.App,
	actorID string,
	input fileUploadPrepareInput,
	digest string,
) (storedProductFileUpload, bool, error) {
	return admitDirectProductFileUploadWithID(app, actorID, input, digest, "")
}

func admitDirectProductFileUploadWithID(
	app core.App,
	actorID string,
	input fileUploadPrepareInput,
	digest string,
	uploadID string,
) (storedProductFileUpload, bool, error) {
	result := storedProductFileUpload{}
	created := false
	if uploadID != "" && !workspaceExportFileRecordIDPattern.MatchString(uploadID) {
		return result, false, errFileUploadInvalid
	}
	requestHash := productFileUploadRequestHash(fileUploadKindDirect, input, digest)
	quotaRelease, err := prepareProductOrganizationStorageAdmission(
		context.Background(), app, input.WorkspaceID, time.Now().UTC().UnixMilli(),
	)
	if err != nil {
		return result, false, err
	}
	defer quotaRelease()
	err = app.RunInTransaction(func(txApp core.App) error {
		existing, err := loadProductFileUploadByRequest(txApp, actorID, input.RequestID)
		if err == nil {
			if existing.Kind != fileUploadKindDirect || existing.RequestHash != requestHash {
				return errFileUploadRequestIDReused
			}
			if existing.Status == fileUploadStatusCleanupPending {
				return errFileUploadExpired
			}
			if _, err := authorizeProductFileUpload(txApp, actorID, existing.WorkspaceID, existing.uploadScope()); err != nil {
				return err
			}
			result = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		authorization, err := authorizeProductFileUpload(txApp, actorID, input.WorkspaceID, input.uploadScope())
		if err != nil {
			return err
		}
		if err := authorizeProductFileUploadMetadata(
			txApp, authorization, input.Name, input.ContentType,
		); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		cleanupDueAt := productFileUploadCleanupDueAt(now)
		if uploadID == "" {
			uploadID = core.GenerateDefaultRandomId()
		}
		result = storedProductFileUpload{
			ActorID: actorID, RequestID: input.RequestID, RequestHash: requestHash,
			UploadID: uploadID, Kind: fileUploadKindDirect,
			WorkspaceID: input.WorkspaceID, OriginalName: input.Name,
			ContentType: input.ContentType, TotalSize: input.Size,
			Status: fileUploadStatusPending, SHA256: digest, CleanupDueAt: cleanupDueAt,
			PageID: input.PageID, Purpose: input.Purpose,
		}
		if _, err := reserveProductOrganizationStorage(
			context.Background(), txApp, result.WorkspaceID, result.UploadID,
			result.TotalSize, now,
		); err != nil {
			return err
		}
		_, err = txApp.DB().Insert(productFileUploadsTable, dbx.Params{
			"actor_id": result.ActorID, "request_id": result.RequestID,
			"request_hash": result.RequestHash, "upload_id": result.UploadID,
			"kind": result.Kind, "workspace_id": result.WorkspaceID,
			"original_name": result.OriginalName, "content_type": result.ContentType,
			"total_size": result.TotalSize, "status": result.Status,
			"received_bytes": result.TotalSize, "next_part": 1,
			"file_id": "", "sha256": result.SHA256,
			"created_at": now, "updated_at": now, "completed_at": 0,
			"cleanup_due_at": cleanupDueAt, "cleanup_attempts": 0,
			"cleanup_error_code": "", "page_id": result.PageID, "purpose": result.Purpose,
		}).Execute()
		if err != nil {
			return err
		}
		if _, err := ensureNotionObjectIdentity(
			txApp, notionObjectIdentityFileUpload, result.WorkspaceID, result.UploadID,
		); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err == nil && created {
		notifyProductFileUploadCleanupAfterCommit(app)
	}
	return result, created, err
}

func publishDirectProductFileUpload(
	app core.App,
	upload storedProductFileUpload,
	file *filesystem.File,
	digest string,
) (*core.Record, error) {
	current, resuming, err := claimProductFileUploadPublication(app, upload, digest)
	if err != nil {
		return nil, err
	}
	if current.Status == fileUploadStatusCompleted {
		return completedDirectProductFileUpload(app, current)
	}
	record, exists, err := findProductFilePublicationRecord(app, current)
	if err != nil {
		return nil, err
	}
	if !exists {
		collection, err := app.FindCollectionByNameOrId(filesCollection)
		if err != nil {
			return nil, err
		}
		record = newProductFileRecord(collection, current, file, digest)
		if resuming {
			if err := reclaimUncommittedProductFileRecordStorage(app, record); err != nil {
				return nil, err
			}
		}
		if err := publishProductFileRecordOutsideTransaction(app, record); err != nil {
			return nil, err
		}
	} else if !productFileRecordMatchesUpload(record, current, digest) {
		return nil, errFileUploadUnavailable
	}
	current.FileID = record.Id
	current.SHA256 = digest
	return settleProductFileUploadCompletion(app, current)
}

func completedDirectProductFileUpload(
	app core.App,
	upload storedProductFileUpload,
) (*core.Record, error) {
	if upload.Status != fileUploadStatusCompleted || upload.FileID == "" {
		return nil, errFileUploadUnavailable
	}
	if _, err := authorizeProductFileUpload(app, upload.ActorID, upload.WorkspaceID, upload.uploadScope()); err != nil {
		return nil, err
	}
	record, err := app.FindRecordById(filesCollection, upload.FileID)
	if err != nil || !productFileRecordMatchesUpload(record, upload, upload.SHA256) {
		return nil, errors.Join(errFileUploadUnavailable, err)
	}
	return record, nil
}

func directFileUploadError(event *core.RequestEvent, err error, stage string) error {
	if quotaError, ok := organizationStorageAPIError(event, err, "file_upload", stage); ok {
		return quotaError
	}
	if storageError, ok := fileOperationalStorageAPIError(event, err, "file_upload", stage); ok {
		return storageError
	}
	if code := storedFileTypeAdmissionCode(err); code != "" {
		message := "Check the file type."
		if code == errorCodeFileUnsafeType {
			message = "Executable web files can't be uploaded."
		}
		return writeAPIError(
			event, http.StatusBadRequest, code, message, "file_upload", stage, code, err,
			diagnosticSourcePosition(1),
		)
	}
	if errors.Is(err, errFileUploadResumableRequired) ||
		errors.Is(err, errFileUploadRequestIDReused) {
		return fileUploadError(event, err, stage)
	}
	if errors.Is(err, errFileUploadInvalid) {
		return fileUploadError(event, err, stage)
	}
	if isPermissionDenied(err) {
		return writeAPIError(
			event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.",
			"file_upload", stage, errorCodeWorkspaceNotFound, err, diagnosticSourcePosition(1),
		)
	}
	return writeAPIError(
		event, http.StatusInternalServerError, errorCodeFileUploadFailed, "Couldn't save the file.",
		"file_upload", stage, errorCodeFileUploadFailed, err, diagnosticSourcePosition(1),
	)
}
