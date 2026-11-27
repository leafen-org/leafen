package app

import "unicode/utf8"

// What a refused file upload says to fix (request_refusal.go): the field
// and why after "Check the file upload request." Status and code stay the
// route's.

// fileUploadRefused refuses a file upload for the reason given.
func fileUploadRefused(format string, args ...any) error {
	return refuse(errFileUploadInvalid, format, args...)
}

// errFileUploadRequestID is a request id that isn't one, as every upload
// route says it.
var errFileUploadRequestID = fileUploadRefused(
	"requestId is a new id of yours, 1–%d characters, so the same upload sent again isn't made twice", fileTransferRequestIDChars)

// errFileUploadIDRefused is an upload id in the path that isn't one.
var errFileUploadIDRefused = fileUploadRefused("the upload ID in the path isn't one a prepared upload gave")

// productFileUploadInputRefusal is why an upload's fields can't be taken,
// or nil.
func productFileUploadInputRefusal(input *fileUploadPrepareInput) error {
	switch {
	case !validProductFileUploadRequestID(input.RequestID):
		return errFileUploadRequestID
	case input.WorkspaceID == "" || len(input.WorkspaceID) > maxWorkspacePermissionIdentifierLength:
		return fileUploadRefused("workspaceId names the workspace the file goes to, 1–%d characters", maxWorkspacePermissionIdentifierLength)
	case input.Name == "" || utf8.RuneCountInString(input.Name) > 2000:
		return fileUploadRefused("name is the file's name, 1–2000 characters")
	case input.Size <= 0 || input.Size > fileTransferMaxFileBytes ||
		(input.Size+fileTransferPartBytes-1)/fileTransferPartBytes > fileTransferMaxParts:
		return fileUploadRefused("size is the file's bytes, 1 to %d", int64(fileTransferMaxFileBytes))
	}
	return nil
}

// productFileUploadScopeRefusal is why an upload's page and purpose can't be
// taken, the scope normalizeProductFileUploadScope refused.
func productFileUploadScopeRefusal(input *fileUploadPrepareInput) error {
	if input.PageID == "" {
		return fileUploadRefused("purpose goes with a pageId")
	}
	return fileUploadRefused("pageId is at most %d characters, and purpose is %s or %s",
		maxWorkspacePermissionIdentifierLength, fileUploadPurposePage, fileUploadPurposeComment)
}
