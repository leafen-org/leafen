package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	authStorageUnavailableCauseCode = "AUTH_STORAGE_UNAVAILABLE"
	authDatabaseBusyCauseCode       = "AUTH_DATABASE_BUSY"
)

func classifyAuthStorageError(err error) (apiFailureSpec, bool) {
	switch {
	case isSQLiteStorageUnavailableError(err):
		return apiFailureSpec{
			Status:    http.StatusServiceUnavailable,
			Code:      errorCodeAuthStorageUnavailable,
			Message:   "The authentication store is unavailable, so the request wasn't saved.",
			Stage:     "storage-write",
			CauseCode: authStorageUnavailableCauseCode,
		}, true
	case isSQLiteBusyError(err):
		return apiFailureSpec{
			Status:    http.StatusServiceUnavailable,
			Code:      errorCodeAuthDatabaseBusy,
			Message:   "The authentication store is locked by another operation, so the request wasn't saved.",
			Stage:     "storage-write",
			CauseCode: authDatabaseBusyCauseCode,
		}, true
	default:
		return apiFailureSpec{}, false
	}
}

func authStorageAPIError(
	event *core.RequestEvent,
	err error,
	operation string,
	stage string,
) (error, bool) {
	failure, ok := classifyAuthStorageError(err)
	if !ok {
		return nil, false
	}
	if normalized := strings.TrimSpace(stage); normalized != "" {
		failure.Stage = normalized
	}
	return apiErrorWithCause(
		event, failure.Status, failure.Code, failure.Message, operation,
		failure.Stage, failure.CauseCode, err,
	), true
}
