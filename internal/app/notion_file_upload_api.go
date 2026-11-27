package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const notionAPIFileUploadsPath = notionAPIRoot + "/file_uploads"

func serveNotionFileUploadCompatibility(event *core.RequestEvent) (bool, error) {
	path := event.Request.URL.Path
	if path == notionAPIFileUploadsPath {
		switch event.Request.Method {
		case http.MethodGet:
			return true, serveNotionFileUploadList(event)
		case http.MethodPost:
			return true, serveNotionFileUploadCreate(event)
		default:
			return false, nil
		}
	}
	if !strings.HasPrefix(path, notionAPIFileUploadsPath+"/") {
		return false, nil
	}
	remainder := strings.TrimPrefix(path, notionAPIFileUploadsPath+"/")
	parts := strings.Split(remainder, "/")
	if len(parts) == 1 && parts[0] != "" && event.Request.Method == http.MethodGet {
		return true, serveNotionFileUploadRetrieve(event, parts[0])
	}
	if len(parts) == 2 && parts[0] != "" && event.Request.Method == http.MethodPost {
		switch parts[1] {
		case "send":
			return true, serveNotionFileUploadSend(event, parts[0])
		case "complete":
			return true, serveNotionFileUploadComplete(event, parts[0])
		}
	}
	// Sent by another method, an upload's contents are refused with Notion's
	// code, and its words say the way (2026-11-26: Notion's say only
	// "Invalid request URL.", which an agent read as no such API).
	if len(parts) == 2 && parts[0] != "" && (parts[1] == "send" || parts[1] == "complete") {
		return true, notionCompatibilityError(event, http.StatusBadRequest, notionErrorInvalidRequestURL, notionFileUploadSendWay)
	}
	return false, nil
}

// notionFileUploadSendWay is how an upload's contents go, as Notion's API
// takes them.
const notionFileUploadSendWay = "Invalid request URL: a file upload's contents go by POST to its upload_url, /v1/file_uploads/{id}/send, " +
	"as multipart/form-data with the file in the field file (and part_number for a multi_part upload), and a multi_part upload " +
	"finishes by POST /v1/file_uploads/{id}/complete: curl -sS -X POST -H 'Authorization: Bearer <token>' -H 'Notion-Version: " + notionCurrentAPIVersion + "' " +
	"-F 'file=@<local file>' '<upload_url>'."

func serveNotionFileUploadCreate(event *core.RequestEvent) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(
		event, "files:write", permissionWorkspaceWrite,
	)
	if err != nil || !ok {
		return err
	}
	input := notionFileUploadCreateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	kind, err := normalizeNotionFileUploadCreate(&input)
	if err != nil {
		return notionFileUploadError(event, err)
	}
	if err := requireDataDiskRoom(event.App, 0); err != nil {
		return notionServerDiskLow(event, err)
	}
	if input.Mode == notionFileUploadModeExternal {
		if err := validateNotionExternalFileUploadSource(
			event.Request.Context(), input.ExternalURL,
		); err != nil {
			return notionFileUploadError(event, err)
		}
	}
	upload, err := admitNotionFileUpload(event.App, workspace.Identity, input, kind)
	if err != nil {
		return notionFileUploadError(event, err)
	}
	return writeNotionFileUpload(event, workspace.Identity, upload)
}

func serveNotionFileUploadRetrieve(event *core.RequestEvent, publicID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(
		event, "files:read", permissionWorkspaceRead,
	)
	if err != nil || !ok {
		return err
	}
	upload, err := loadAuthorizedNotionFileUpload(event.App, workspace.Identity, publicID)
	if err != nil {
		return notionFileUploadError(event, err)
	}
	upload, err = refreshNotionFileUploadAttachment(event.App, upload)
	if err != nil {
		return notionFileUploadError(event, err)
	}
	return writeNotionFileUpload(event, workspace.Identity, upload)
}

func serveNotionFileUploadSend(event *core.RequestEvent, publicID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(
		event, "files:write", permissionWorkspaceWrite,
	)
	if err != nil || !ok {
		return err
	}
	upload, err := loadAuthorizedNotionFileUpload(event.App, workspace.Identity, publicID)
	if err != nil {
		return notionFileUploadError(event, err)
	}
	release := acquireProductFileOperationLock(productFileOperationResumable, upload.UploadID)
	defer release()
	upload, err = loadAuthorizedNotionFileUpload(event.App, workspace.Identity, publicID)
	if err != nil {
		return notionFileUploadError(event, err)
	}
	if upload.Status == fileUploadStatusCompleted {
		return writeNotionFileUpload(event, workspace.Identity, upload)
	}
	if upload.Kind != fileUploadKindNotionSingle && upload.Kind != fileUploadKindNotionMulti {
		return notionFileUploadError(event, notionInvalid(errFileUploadInvalid, "Only a single_part or multi_part upload takes file contents."))
	}
	event.Request.Body = http.MaxBytesReader(
		event.Response, event.Request.Body, notionFileUploadMaxPartBytes+(2<<20),
	)
	files, err := event.FindUploadedFiles("file")
	if event.Request.MultipartForm != nil {
		defer event.Request.MultipartForm.RemoveAll()
	}
	if err != nil || len(files) != 1 {
		return notionFileUploadError(event, notionInvalid(errFileUploadInvalid, "The request needs exactly one file in the multipart field file."))
	}
	partNumber := 0
	partValue := strings.TrimSpace(event.Request.FormValue("part_number"))
	if upload.Kind == fileUploadKindNotionMulti {
		parsed, parseErr := strconv.Atoi(partValue)
		if parseErr != nil || parsed < 1 || parsed > upload.NotionPartsTotal {
			return notionFileUploadError(event, notionInvalid(errFileUploadInvalid, fmt.Sprintf("part_number must be a whole number from 1 to %d.", upload.NotionPartsTotal)))
		}
		partNumber = parsed - 1
	} else if partValue != "" {
		return notionFileUploadError(event, notionInvalid(errFileUploadInvalid, "A single_part upload takes no part_number."))
	}
	name, contentType, err := notionFileUploadPartMetadata(
		upload, files[0], directProductFileContentType(event),
	)
	if err != nil {
		return notionFileUploadError(event, err)
	}
	// A single part is joined at once (dataDiskPartsBytes); a part of many,
	// when the upload is completed.
	incoming := files[0].Size
	if upload.Kind == fileUploadKindNotionSingle {
		incoming = dataDiskPartsBytes(incoming)
	}
	if err := requireDataDiskRoom(event.App, incoming); err != nil {
		return notionServerDiskLow(event, err)
	}
	upload, _, err = persistNotionProductFileUploadPart(
		event.App, upload, partNumber, files[0], name, contentType,
	)
	if err != nil {
		return notionFileUploadError(event, err)
	}
	if upload.Kind == fileUploadKindNotionSingle {
		upload, err = completeNotionFileUpload(event.App, upload)
		if err != nil {
			return notionFileUploadError(event, err)
		}
		notifyFileText(event.App)
	}
	return writeNotionFileUpload(event, workspace.Identity, upload)
}

func serveNotionFileUploadComplete(event *core.RequestEvent, publicID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(
		event, "files:write", permissionWorkspaceWrite,
	)
	if err != nil || !ok {
		return err
	}
	upload, err := loadAuthorizedNotionFileUpload(event.App, workspace.Identity, publicID)
	if err != nil {
		return notionFileUploadError(event, err)
	}
	if upload.Kind != fileUploadKindNotionMulti {
		return notionFileUploadError(event, notionInvalid(errFileUploadInvalid, "Only a multi_part upload is completed this way."))
	}
	release := acquireProductFileOperationLock(productFileOperationResumable, upload.UploadID)
	defer release()
	upload, err = loadAuthorizedNotionFileUpload(event.App, workspace.Identity, publicID)
	if err == nil {
		upload, err = completeNotionFileUpload(event.App, upload)
	}
	if err != nil {
		return notionFileUploadError(event, err)
	}
	notifyFileText(event.App)
	return writeNotionFileUpload(event, workspace.Identity, upload)
}

func completeNotionFileUpload(
	app core.App,
	upload storedProductFileUpload,
) (storedProductFileUpload, error) {
	if upload.Status == fileUploadStatusCompleted {
		_, err := settleProductFileUploadCompletion(app, upload)
		return upload, err
	}
	if notionFileUploadPublicStatus(upload, time.Now().UTC().UnixMilli()) != "pending" {
		return storedProductFileUpload{}, errFileUploadExpired
	}
	if upload.Status != fileUploadStatusPending && upload.Status != fileUploadStatusSettling {
		return storedProductFileUpload{}, errFileUploadIncomplete
	}
	parts, err := loadProductFileUploadParts(app, upload.UploadID)
	if err != nil {
		return storedProductFileUpload{}, err
	}
	expectedParts := 1
	if upload.Kind == fileUploadKindNotionMulti {
		expectedParts = upload.NotionPartsTotal
	}
	if len(parts) != expectedParts || upload.NextPart != expectedParts ||
		upload.ReceivedBytes != upload.TotalSize || upload.TotalSize <= 0 {
		return storedProductFileUpload{}, errFileUploadIncomplete
	}
	for index, part := range parts {
		if part.PartNumber != index || part.Size <= 0 || part.Size > notionFileUploadMaxPartBytes ||
			upload.Kind == fileUploadKindNotionMulti && index < len(parts)-1 &&
				part.Size < notionFileUploadMinPartBytes {
			return storedProductFileUpload{}, errFileUploadIncomplete
		}
	}
	if _, err := streamAndPublishProductFileUpload(app, upload, parts); err != nil {
		return storedProductFileUpload{}, err
	}
	upload.Status = fileUploadStatusSettling
	upload.FileID = upload.UploadID
	if _, err := settleProductFileUploadCompletion(app, upload); err != nil {
		return storedProductFileUpload{}, err
	}
	completed, err := loadProductFileUploadByID(app, upload.ActorID, upload.UploadID)
	if err == nil {
		recordFileUploadWebhookEvent(app, eventWebhookFileUploadCompleted, completed, pageHistoryConnection{}, nil)
	}
	return completed, err
}

func writeNotionFileUpload(
	event *core.RequestEvent,
	identity notionOAuthBearerIdentity,
	upload storedProductFileUpload,
) error {
	response, err := notionFileUploadObject(event, identity, upload)
	if err != nil {
		return notionFileUploadError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func notionFileUploadObject(
	event *core.RequestEvent,
	identity notionOAuthBearerIdentity,
	upload storedProductFileUpload,
) (map[string]any, error) {
	publicID, err := notionObjectIdentityPublicID(
		event.App, notionObjectIdentityFileUpload, upload.WorkspaceID, upload.UploadID,
	)
	if err != nil {
		return nil, err
	}
	createdBy, err := notionCompatibleUserID(upload.ActorID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().UnixMilli()
	status := notionFileUploadPublicStatus(upload, now)
	response := map[string]any{
		"object": "file_upload", "id": publicID,
		"created_time":     notionFileUploadTimestamp(upload.CreatedAt),
		"created_by":       map[string]any{"id": createdBy, "type": "person"},
		"last_edited_time": notionFileUploadTimestamp(upload.UpdatedAt),
		"in_trash":         status == "expired", "expiry_time": nil,
		"status": status, "filename": nil, "content_type": nil, "content_length": nil,
	}
	if expiresAt := notionFileUploadExpiresAt(upload); expiresAt > 0 {
		response["expiry_time"] = notionFileUploadTimestamp(expiresAt)
	}
	if upload.OriginalName != "" {
		response["filename"] = upload.OriginalName
	}
	if upload.ContentType != "" {
		response["content_type"] = upload.ContentType
	}
	if upload.TotalSize > 0 {
		response["content_length"] = upload.TotalSize
	}
	if status == "pending" && upload.NotionMode != notionFileUploadModeExternal {
		origin, err := publicServerOrigin(event.Request)
		if err != nil {
			return nil, err
		}
		response["upload_url"] = origin + notionAPIFileUploadsPath + "/" + publicID + "/send"
		if upload.NotionMode == notionFileUploadModeMulti {
			response["complete_url"] = origin + notionAPIFileUploadsPath + "/" + publicID + "/complete"
		}
	}
	if upload.NotionMode == notionFileUploadModeMulti {
		response["number_of_parts"] = map[string]any{
			"total": upload.NotionPartsTotal, "sent": upload.NextPart,
		}
	}
	if upload.NotionMode == notionFileUploadModeExternal && status == "uploaded" {
		response["file_import_result"] = map[string]any{
			"imported_time": notionFileUploadTimestamp(upload.CompletedAt),
			"type":          "success", "success": map[string]any{},
		}
	}
	if upload.NotionMode == notionFileUploadModeExternal && status == "failed" {
		response["file_import_result"] = notionFileUploadFailureObject(upload)
	}
	_ = identity
	return response, nil
}

func notionFileUploadFailureObject(upload storedProductFileUpload) map[string]any {
	parameter := any(nil)
	if upload.NotionFailureParameter != "" {
		parameter = upload.NotionFailureParameter
	}
	statusCode := any(nil)
	if upload.NotionFailureStatus > 0 {
		statusCode = upload.NotionFailureStatus
	}
	return map[string]any{
		"type": "error",
		"error": map[string]any{
			"type": upload.NotionFailureType, "code": upload.NotionFailureCode,
			"message":   upload.NotionFailureMessage,
			"parameter": parameter, "status_code": statusCode,
		},
	}
}

func notionFileUploadTimestamp(value int64) string {
	if value <= 0 {
		value = time.Now().UTC().UnixMilli()
	}
	return notionMinuteTimestamp(time.UnixMilli(value))
}

func refreshNotionFileUploadAttachment(
	app core.App,
	upload storedProductFileUpload,
) (storedProductFileUpload, error) {
	if upload.Status != fileUploadStatusCompleted || upload.NotionAttachedAt > 0 || upload.FileID == "" {
		return upload, nil
	}
	referenced, err := findActiveProductFileReference(app, upload.FileID)
	if err != nil || !referenced {
		return upload, err
	}
	now := time.Now().UTC().UnixMilli()
	if err := markNotionFileUploadAttached(app, upload.FileID, now); err != nil {
		return upload, err
	}
	upload.NotionAttachedAt = now
	upload.UpdatedAt = now
	return upload, nil
}

func notionFileUploadError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errFileUploadNotFound), errors.Is(err, sql.ErrNoRows):
		return notionCompatibilityError(
			event, http.StatusNotFound, notionErrorObjectNotFound,
			"There's no file upload with the requested ID.",
		)
	case errors.Is(err, errFileUploadPartConflict), errors.Is(err, errFileUploadRequestIDReused):
		return notionCompatibilityError(
			event, http.StatusConflict, "conflict_error", "The file upload conflicts with stored content.",
		)
	// Notion has no error of its own for a storage limit; the validation
	// error says which limit refused it (2026-10-16: it read only "invalid").
	case errors.Is(err, errServerDiskLow):
		return notionServerDiskLow(event, err)
	case errors.Is(err, errOrganizationStorageLimitExceeded):
		return notionPageValidationError(event, "The organization's storage limit is reached. Ask an organization admin to raise the limit or remove files.")
	case errors.Is(err, errFileUploadInvalid), errors.Is(err, errFileUploadIncomplete),
		errors.Is(err, errFileUploadExpired), storedFileTypeAdmissionCode(err) != "":
		return notionPageValidationError(event, notionFileUploadRefusalReason(err))
	case isDLPPolicyDecisionError(err):
		return notionDLPPolicyError(event, err)
	case isPermissionDenied(err):
		return notionCompatibilityError(
			event, http.StatusNotFound, notionErrorObjectNotFound,
			"There's no file upload with the requested ID.",
		)
	default:
		return notionRequestNotCompleted(event, err)
	}
}

// notionFileUploadRefusalReason says which value or limit refused an upload
// (2026-10-16: an unsupported type and a bad part alike read "invalid").
func notionFileUploadRefusalReason(err error) string {
	switch storedFileTypeAdmissionCode(err) {
	case errorCodeFileUnsafeType:
		return "Files of this type can't be uploaded: a browser could run them."
	case errorCodeFileContentTypeInvalid:
		return "content_type isn't a valid media type."
	}
	switch {
	case errors.Is(err, errFileUploadExpired):
		return "The file upload expired. Create a new one."
	case errors.Is(err, errFileUploadIncomplete):
		return "The file upload isn't complete. Send every part first."
	}
	return notionAdminRefusalReason(err, "The file upload request is invalid.")
}

// notionServerDiskLow answers an upload refused for the server's disk.
// Notion has no answer of its own for it; 503 says to try again later, and
// the message says why, to whoever asked (data_disk_refusal.go).
func notionServerDiskLow(event *core.RequestEvent, refusal error) error {
	serverDiskRetryAfter(event, refusal)
	return notionCompatibilityError(event, http.StatusServiceUnavailable, "service_unavailable",
		serverDiskRefusalMessage(event, refusal))
}
