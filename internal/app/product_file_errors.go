package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

const (
	fileStorageUnavailableCauseCode = "FILE_STORAGE_UNAVAILABLE"
	fileDatabaseBusyCauseCode       = "FILE_DATABASE_BUSY"
)

func fileOperationalStorageAPIError(
	event *core.RequestEvent,
	err error,
	operation string,
	stage string,
) (error, bool) {
	storageUnavailable := isSQLiteStorageUnavailableError(err)
	databaseBusy := isSQLiteBusyError(err)
	if !storageUnavailable && !databaseBusy {
		return nil, false
	}

	code := errorCodeFileUploadStorageUnavailable
	message := "Retry the same file upload after server storage recovers."
	causeCode := fileStorageUnavailableCauseCode
	if operation == "file_delete" {
		code = errorCodeFileDeleteStorageUnavailable
		message = "Retry the same file deletion after server storage recovers."
	}
	if databaseBusy {
		code = errorCodeFileUploadDatabaseBusy
		message = "Retry the same file upload after the other server operation finishes."
		causeCode = fileDatabaseBusyCauseCode
		if operation == "file_delete" {
			code = errorCodeFileDeleteDatabaseBusy
			message = "Retry the same file deletion after the other server operation finishes."
		}
	}

	return writeAPIError(
		event,
		http.StatusServiceUnavailable,
		code,
		message,
		operation,
		stage,
		causeCode,
		err,
		diagnosticSourcePosition(1),
	), true
}
