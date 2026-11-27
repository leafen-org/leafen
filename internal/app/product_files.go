package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const (
	filesCollection                = "leafen_files"
	maxNativeFileSize              = int64(fileTransferMaxFileBytes)
	maxNativeDirectFileRequestSize = int64(fileTransferDirectBytes) + apis.DefaultMaxBodySize

	errorCodeFileUploadFailed   = "LEAFEN_FILE_UPLOAD_FAILED"
	errorCodeFileNotFound       = "LEAFEN_FILE_NOT_FOUND"
	errorCodeFileDownloadFailed = "LEAFEN_FILE_DOWNLOAD_FAILED"
)

func registerProductFileRoutes(event *core.ServeEvent) {
	event.Router.Route(fileTransferDirectUploadMethod, apiRoot+fileTransferDirectUploadPath, uploadFile).
		Bind(apis.BodyLimit(maxNativeDirectFileRequestSize))
	event.Router.Route(fileTransferPrepareMethod, apiRoot+fileTransferPreparePath, prepareFileUpload).
		Bind(apis.BodyLimit(fileUploadJSONBodyBytes))
	event.Router.Route(fileTransferPartMethod, apiRoot+fileTransferPartPath, uploadFilePart).
		Bind(apis.BodyLimit(fileTransferPartBytes + fileUploadJSONBodyBytes))
	event.Router.Route(fileTransferCompleteMethod, apiRoot+fileTransferCompletePath, completeFileUpload).
		Bind(apis.BodyLimit(fileUploadJSONBodyBytes))
	event.Router.Route(fileTransferDownloadMethod, apiRoot+fileTransferDownloadPath, downloadFile)
	event.Router.Route(
		fileTransferIssueCapabilityMethod,
		apiRoot+fileTransferIssueCapabilityPath,
		issueFileDownloadCapability,
	)
	event.Router.Route(
		fileTransferDownloadCapabilityMethod,
		apiRoot+fileTransferDownloadCapabilityPath,
		downloadFileWithCapability,
	)
	event.Router.Route(fileDeleteMethod, apiRoot+fileDeletePath, deleteFile).
		Bind(apis.BodyLimit(fileDeleteMaxBodyBytes))
}

func uploadFile(event *core.RequestEvent) error {
	return uploadProductFileDirect(event)
}

func prepareFileUpload(event *core.RequestEvent) error {
	return prepareProductFileUpload(event)
}

func uploadFilePart(event *core.RequestEvent) error {
	return putProductFileUploadPart(event)
}

func completeFileUpload(event *core.RequestEvent) error {
	return completeProductFileUpload(event)
}

func downloadFile(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	return serveAuthorizedProductFile(
		event, user.Id, strings.TrimSpace(event.Request.PathValue("id")), false,
	)
}

func fileDigest(reader interface {
	Open() (io.ReadSeekCloser, error)
}) (string, error) {
	file, err := reader.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func fileRecordResponse(record *core.Record) fileTransferFile {
	return fileTransferFile{
		ID: record.Id, WorkspaceID: record.GetString("workspace"), Name: record.GetString("original_name"),
		Size: int64(record.GetFloat("size")), ContentType: record.GetString("content_type"),
		SHA256: record.GetString("sha256"),
	}
}

func setWorkspaceFileRecordAuthority(record *core.Record, workspaceID string, createdBy string) {
	record.Set("workspace", workspaceID)
	record.Set("authority_state", fileAuthorityWorkspace)
	record.Set("created_by", createdBy)
}

func workspaceFileRecordBelongsTo(record *core.Record, workspaceID string) bool {
	return record != nil && workspaceID != "" && record.GetString("workspace") == workspaceID &&
		record.GetString("authority_state") == fileAuthorityWorkspace && record.GetString("created_by") != ""
}

func authorizeWorkspaceFile(
	app core.App,
	principalID string,
	fileID string,
	requirement permissionRequirement,
) (*core.Record, workspaceAuthorization, error) {
	record, err := loadAccessWorkspaceFile(app, fileID)
	if err != nil {
		return nil, workspaceAuthorization{}, err
	}
	workspaceID := record.GetString("workspace")
	authorization, err := authorizeWorkspace(app, principalID, workspaceID, requirement)
	// A member reads it where it is held too (product_file_member_read.go).
	if err == nil && requirement == permissionWorkspaceRead {
		open, openErr := memberProductFileOpen(app, principalID, workspaceID, fileID, record.GetString("created_by"))
		if openErr != nil {
			return nil, workspaceAuthorization{}, openErr
		}
		if !open {
			err = permissionDenied()
		}
	}
	if isPermissionDenied(err) && requirement == permissionWorkspaceRead {
		authorization, err = authorizeReferencedProductFileRead(app, principalID, workspaceID, fileID)
	}
	if isPermissionDenied(err) && requirement == permissionWorkspaceRead {
		authorization, err = authorizeUploadedProductFileRead(app, principalID, workspaceID, fileID)
	}
	if err != nil {
		return nil, workspaceAuthorization{}, err
	}
	return record, authorization, nil
}

// loadAccessWorkspaceFile reads a file anyone may be let to: uploaded whole,
// not being deleted, and kept by its workspace.
func loadAccessWorkspaceFile(app core.App, fileID string) (*core.Record, error) {
	record, err := app.FindRecordById(filesCollection, fileID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, permissionDenied()
	}
	if err != nil {
		return nil, err
	}
	if pendingDelete, err := productFileDeletionPending(app, fileID); err != nil || pendingDelete {
		return nil, deniedUnless(err)
	}
	if incompleteUpload, err := productFileUploadIncomplete(app, fileID); err != nil || incompleteUpload {
		return nil, deniedUnless(err)
	}
	if !workspaceFileRecordBelongsTo(record, record.GetString("workspace")) {
		return nil, permissionDenied()
	}
	return record, nil
}
