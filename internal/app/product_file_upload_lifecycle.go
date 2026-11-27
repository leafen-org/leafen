package app

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const fileUploadJSONBodyBytes = 4096

var (
	errFileUploadInvalid           = errors.New(errorCodeFileUploadInvalid)
	errFileUploadResumableRequired = errors.New(errorCodeFileUploadResumableRequired)
	errFileUploadNotFound          = errors.New(errorCodeFileUploadNotFound)
	errFileUploadRequestIDReused   = errors.New(errorCodeFileUploadRequestIDReused)
	errFileUploadPartConflict      = errors.New(errorCodeFileUploadPartConflict)
	errFileUploadIncomplete        = errors.New(errorCodeFileUploadIncomplete)
	errFileUploadExpired           = errors.New(errorCodeFileUploadExpired)
	errFileUploadUnavailable       = errors.New(errorCodeFileUploadUnavailable)
	fileUploadIDPattern            = regexp.MustCompile(`^[A-Za-z0-9_-]{15}$`)
	// What a part or a completion got wrong, said to whoever sent it
	// (2026-11-13: an agent heard only "Check the file upload request." for
	// a part sent without its hash, and "Send the remaining file parts" for
	// a completion with another request ID than it was prepared with).
	errFileUploadPartHash     = fmt.Errorf("%w: part hash header", errFileUploadInvalid)
	errFileUploadPartMismatch = fmt.Errorf("%w: part bytes", errFileUploadInvalid)
	errFileUploadOtherRequest = fmt.Errorf("%w: prepared with another request", errFileUploadRequestIDReused)
)

func prepareProductFileUpload(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := fileUploadPrepareInput{}
	if err := event.BindBody(&input); err != nil {
		return fileUploadError(event, bodyRefusal(errFileUploadInvalid, "file upload", err), "prepare-bind")
	}
	if err := normalizeProductFileUploadInput(&input); err != nil {
		return fileUploadError(event, err, "prepare-validate")
	}
	if err := requireDataDiskRoom(event.App, input.Size); err != nil {
		return writeServerDiskLow(event, "file-upload", err)
	}

	release := acquireProductFileOperationLock(
		productFileOperationScopedNamespace(productFileOperationUploadRequest, user.Id),
		input.RequestID,
	)
	defer release()
	upload, created, err := admitProductFileUpload(event.App, user.Id, input)
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "file-upload-metadata")
	}
	if err != nil {
		return fileUploadError(event, err, "prepare-admit")
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	return event.JSON(status, productFileUploadStateResponse(upload, !created))
}

func putProductFileUploadPart(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	uploadID, partNumber, digest, err := productFileUploadPartRequest(event)
	if err != nil {
		return fileUploadError(event, err, "part-validate")
	}
	updated, duplicate, stage, err := storeProductFileUploadPart(event.App, user.Id, uploadID, partNumber, digest, event.Request.Body)
	if err != nil {
		return fileUploadError(event, err, stage)
	}
	return event.JSON(http.StatusOK, productFileUploadPartResponse(updated, duplicate))
}

// storeProductFileUploadPart keeps one part of an actor's resumable upload,
// saying at which stage it was refused: the one boundary a part goes
// through.
func storeProductFileUploadPart(
	app core.App, actorID, uploadID string, partNumber int, digest string, body io.Reader,
) (storedProductFileUpload, bool, string, error) {
	release := acquireProductFileOperationLock(productFileOperationResumable, uploadID)
	defer release()
	upload, err := loadAuthorizedProductFileUpload(app, actorID, uploadID)
	if err != nil {
		return storedProductFileUpload{}, false, "part-authorize", err
	}
	if upload.Status == fileUploadStatusCleanupPending {
		return storedProductFileUpload{}, false, "part-state", errFileUploadExpired
	}
	if upload.Kind != fileUploadKindResumable || upload.Status != fileUploadStatusPending {
		return storedProductFileUpload{}, false, "part-state", errFileUploadIncomplete
	}
	expectedBytes, err := expectedProductFileUploadPartBytes(app, upload, partNumber)
	if err != nil {
		return storedProductFileUpload{}, false, "part-order", err
	}
	updated, duplicate, err := persistProductFileUploadPart(app, upload, partNumber, expectedBytes, digest, body)
	if err != nil {
		return storedProductFileUpload{}, false, "part-store", err
	}
	return updated, duplicate, "", nil
}

func productFileUploadPartRequest(event *core.RequestEvent) (string, int, string, error) {
	uploadID := strings.TrimSpace(event.Request.PathValue("uploadId"))
	partNumber, err := strconv.Atoi(strings.TrimSpace(event.Request.PathValue("partNumber")))
	digest := strings.ToLower(strings.TrimSpace(event.Request.Header.Get("X-Leafen-Part-SHA256")))
	if !fileUploadIDPattern.MatchString(uploadID) {
		return "", 0, "", errFileUploadIDRefused
	}
	if err != nil || partNumber < 0 || partNumber >= fileTransferMaxParts {
		return "", 0, "", fileUploadRefused("the part number in the path is 0 to %d", fileTransferMaxParts-1)
	}
	if !sha256Pattern.MatchString(digest) {
		return "", 0, "", errFileUploadPartHash
	}
	return uploadID, partNumber, digest, nil
}

func completeProductFileUpload(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	uploadID := strings.TrimSpace(event.Request.PathValue("uploadId"))
	input := fileUploadCompleteInput{}
	if err := event.BindBody(&input); err != nil {
		return fileUploadError(event, bodyRefusal(errFileUploadInvalid, "file upload's completion", err), "complete-bind")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	if !fileUploadIDPattern.MatchString(uploadID) {
		return fileUploadError(event, errFileUploadIDRefused, "complete-validate")
	}
	if !validProductFileUploadRequestID(input.RequestID) {
		return fileUploadError(event, errFileUploadRequestID, "complete-validate")
	}
	upload, record, duplicate, stage, err := finishProductFileUpload(event.App, user.Id, uploadID, input.RequestID)
	if err != nil {
		return fileUploadError(event, err, stage)
	}
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, productFileUploadCompleteResponse(upload, record, duplicate))
}

// finishProductFileUpload publishes an actor's resumable upload whose parts
// are all in, or answers again for one already published, saying at which
// stage it was refused.
func finishProductFileUpload(
	app core.App, actorID, uploadID, requestID string,
) (storedProductFileUpload, *core.Record, bool, string, error) {
	release := acquireProductFileOperationLock(productFileOperationResumable, uploadID)
	defer release()
	upload, err := loadAuthorizedProductFileUpload(app, actorID, uploadID)
	if err != nil {
		return upload, nil, false, "complete-authorize", err
	}
	if upload.Kind != fileUploadKindResumable {
		return upload, nil, false, "complete-request", errFileUploadRequestIDReused
	}
	if upload.RequestID != requestID {
		return upload, nil, false, "complete-request", errFileUploadOtherRequest
	}
	if upload.Status == fileUploadStatusCompleted {
		record, err := settleProductFileUploadCompletion(app, upload)
		if err != nil {
			return upload, nil, false, "complete-replay", err
		}
		return upload, record, true, "", nil
	}
	if upload.Status == fileUploadStatusCleanupPending {
		return upload, nil, false, "complete-state", errFileUploadExpired
	}
	if upload.Status != fileUploadStatusPending && upload.Status != fileUploadStatusSettling {
		return upload, nil, false, "complete-state", errFileUploadIncomplete
	}
	if upload.Status == fileUploadStatusSettling {
		_, exists, err := findProductFilePublicationRecord(app, upload)
		if err != nil {
			return upload, nil, false, "complete-resume-record", err
		}
		if exists {
			record, err := settleProductFileUploadCompletion(app, upload)
			if err != nil {
				return upload, nil, false, "complete-resume-settle", err
			}
			upload.Status = fileUploadStatusCompleted
			notifyFileText(app)
			return upload, record, true, "", nil
		}
	}
	parts, err := exactProductFileUploadParts(app, upload)
	if err != nil {
		return upload, nil, false, "complete-parts", err
	}
	duplicate := upload.Status == fileUploadStatusSettling
	if _, err := streamAndPublishProductFileUpload(app, upload, parts); err != nil {
		return upload, nil, false, "complete-publish", err
	}
	upload.Status = fileUploadStatusSettling
	upload.FileID = upload.UploadID
	record, err := settleProductFileUploadCompletion(app, upload)
	if err != nil {
		return upload, nil, false, "complete-cleanup", err
	}
	upload.Status = fileUploadStatusCompleted
	// A document's words are read for search soon after it arrives.
	notifyFileText(app)
	return upload, record, duplicate, "", nil
}

func normalizeProductFileUploadInput(input *fileUploadPrepareInput) error {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.Name = strings.TrimSpace(input.Name)
	input.ContentType = strings.TrimSpace(input.ContentType)
	input.PageID = strings.TrimSpace(input.PageID)
	input.Purpose = strings.TrimSpace(input.Purpose)
	if input.ContentType == "" {
		input.ContentType = "application/octet-stream"
	}
	if err := productFileUploadInputRefusal(input); err != nil {
		return err
	}
	if !normalizeProductFileUploadScope(input) {
		return productFileUploadScopeRefusal(input)
	}
	contentType, err := admitStoredFileType(input.Name, input.ContentType)
	if err != nil {
		return err
	}
	input.ContentType = contentType
	return nil
}

func validProductFileUploadRequestID(requestID string) bool {
	return requestID != "" && len([]rune(requestID)) <= fileTransferRequestIDChars
}

func loadAuthorizedProductFileUpload(
	app core.App,
	actorID, uploadID string,
) (storedProductFileUpload, error) {
	upload, err := loadProductFileUploadByID(app, actorID, uploadID)
	if errors.Is(err, sql.ErrNoRows) {
		return storedProductFileUpload{}, errFileUploadNotFound
	}
	if err != nil {
		return storedProductFileUpload{}, err
	}
	if _, err := authorizeProductFileUpload(app, actorID, upload.WorkspaceID, upload.uploadScope()); err != nil {
		return storedProductFileUpload{}, err
	}
	return upload, nil
}

func authorizeProductFileUploadMetadata(
	app core.App,
	authorization workspaceAuthorization,
	name, contentType string,
) error {
	if authorization.Organization.OrganizationID == "" {
		return nil
	}
	return enforceOrganizationDLPContent(app, authorization.Organization.OrganizationID, map[string]any{
		"name": name, "contentType": contentType,
	})
}

func expectedProductFileUploadPartBytes(
	app core.App,
	upload storedProductFileUpload,
	partNumber int,
) (int64, error) {
	part, err := loadProductFileUploadPart(app, upload.UploadID, partNumber)
	if err == nil {
		return part.Size, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if partNumber != upload.NextPart || upload.ReceivedBytes >= upload.TotalSize {
		return 0, errFileUploadPartConflict
	}
	remaining := upload.TotalSize - upload.ReceivedBytes
	return min(remaining, int64(fileTransferPartBytes)), nil
}

func exactProductFileUploadParts(
	app core.App,
	upload storedProductFileUpload,
) ([]storedProductFileUploadPart, error) {
	if upload.ReceivedBytes != upload.TotalSize || upload.NextPart <= 0 ||
		upload.NextPart > fileTransferMaxParts {
		return nil, errFileUploadIncomplete
	}
	parts, err := loadProductFileUploadParts(app, upload.UploadID)
	if err != nil {
		return nil, err
	}
	var total int64
	for index, part := range parts {
		if part.PartNumber != index || part.Size <= 0 || part.Size > fileTransferPartBytes ||
			!sha256Pattern.MatchString(part.SHA256) || strings.TrimSpace(part.StorageKey) == "" {
			return nil, errFileUploadIncomplete
		}
		total += part.Size
	}
	if len(parts) != upload.NextPart || total != upload.TotalSize {
		return nil, errFileUploadIncomplete
	}
	return parts, nil
}

func productFileUploadStateResponse(
	upload storedProductFileUpload,
	duplicate bool,
) fileUploadStateResponse {
	return fileUploadStateResponse{
		UploadID: upload.UploadID, Status: upload.Status,
		ReceivedBytes: upload.ReceivedBytes, NextPart: upload.NextPart,
		PartBytes: fileTransferPartBytes, Duplicate: duplicate,
	}
}

func productFileUploadPartResponse(
	upload storedProductFileUpload,
	duplicate bool,
) fileUploadPartResponse {
	return fileUploadPartResponse{
		UploadID: upload.UploadID, Status: upload.Status,
		ReceivedBytes: upload.ReceivedBytes, NextPart: upload.NextPart,
		Duplicate: duplicate,
	}
}

func productFileUploadCompleteResponse(
	upload storedProductFileUpload,
	record *core.Record,
	duplicate bool,
) fileUploadCompleteResponse {
	return fileUploadCompleteResponse{
		UploadID: upload.UploadID, Status: fileUploadStatusCompleted,
		Duplicate: duplicate, File: fileRecordResponse(record),
	}
}

func fileUploadError(event *core.RequestEvent, err error, stage string) error {
	if quotaError, ok := organizationStorageAPIError(event, err, "file_upload", stage); ok {
		return quotaError
	}
	if storageError, ok := fileOperationalStorageAPIError(event, err, "file_upload", stage); ok {
		return storageError
	}
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "file-upload-metadata")
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
	status, code, message := http.StatusServiceUnavailable,
		errorCodeFileUploadUnavailable, "Couldn't continue the file upload."
	switch {
	case errors.Is(err, errFileUploadResumableRequired):
		status, code, message = http.StatusRequestEntityTooLarge,
			errorCodeFileUploadResumableRequired, "This file needs a multipart upload."
	case errors.Is(err, errFileUploadPartHash):
		status, code, message = http.StatusBadRequest, errorCodeFileUploadInvalid,
			"Send the part's bytes as the body, with their SHA-256 in hex in the "+nativeOperationPartHashHeader+" header."
	case errors.Is(err, errFileUploadPartMismatch):
		status, code, message = http.StatusBadRequest, errorCodeFileUploadInvalid,
			fmt.Sprintf("The part's bytes don't match its %s header or its size: %d bytes, the last part what remains.",
				nativeOperationPartHashHeader, fileTransferPartBytes)
	case errors.Is(err, errFileUploadInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodeFileUploadInvalid, refusalMessage("Check the file upload request.", refusalReason(err))
	case errors.Is(err, errFileUploadOtherRequest):
		status, code, message = http.StatusConflict,
			errorCodeFileUploadRequestIDReused, "Use the requestId the upload was prepared with."
	case errors.Is(err, errFileUploadNotFound), isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodeFileUploadNotFound, "File upload not found."
	case errors.Is(err, errFileUploadRequestIDReused):
		status, code, message = http.StatusConflict,
			errorCodeFileUploadRequestIDReused, "This request ID was used for a different file upload."
	case errors.Is(err, errFileUploadPartConflict):
		status, code, message = http.StatusConflict,
			errorCodeFileUploadPartConflict, "The file part was already stored with different content."
	case errors.Is(err, errFileUploadIncomplete):
		status, code, message = http.StatusConflict,
			errorCodeFileUploadIncomplete, "Send the remaining file parts, then complete again."
	case errors.Is(err, errFileUploadExpired):
		status, code, message = http.StatusGone,
			errorCodeFileUploadExpired, "The inactive file upload expired. Start again."
	case errors.Is(err, errProductFileIntegrity):
		status, code, message = http.StatusConflict,
			errorCodeFileIntegrityFailed, "Couldn't verify the stored file's integrity."
	}
	return writeAPIError(
		event, status, code, message, "file_upload", stage, code, err,
		diagnosticSourcePosition(1),
	)
}
