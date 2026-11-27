package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const fileManualCleanupMaxBodyBytes = 4 * 1024

func registerProductFileManualCleanupRoutes(event *core.ServeEvent) {
	event.Router.Route(
		workspaceFileManualCleanupStartMethod,
		apiRoot+workspaceFileManualCleanupStartPath,
		startProductFileManualCleanup,
	).Bind(apis.BodyLimit(fileManualCleanupMaxBodyBytes))
	event.Router.Route(
		workspaceFileManualCleanupReadMethod,
		apiRoot+workspaceFileManualCleanupReadPath,
		readProductFileManualCleanup,
	)
}

func startProductFileManualCleanup(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := fileManualCleanupInput{}
	if err := event.BindBody(&input); err != nil || workspaceID == "" {
		return productFileManualCleanupAPIError(event, errFileManualCleanupInvalid, "validate")
	}
	if strings.TrimSpace(input.RequestID) == "" {
		return refuseMissingInput(event, errorCodeFileManualCleanupInvalid, "requestId", "a new id of yours, so the same request sent again isn't done twice.")
	}
	if _, err := authorizeWorkspace(
		event.App, user.Id, workspaceID, permissionWorkspaceOwner,
	); err != nil {
		return productFileManualCleanupAPIError(event, err, "authorize")
	}
	request := authorizedProductFileManualCleanupRequest{
		ActorID: user.Id, WorkspaceID: workspaceID,
		RequestID: input.RequestID, NowMillis: time.Now().UTC().UnixMilli(),
	}
	// A key's call asks its person only for a cleanup that would start
	// (user_key_acting_ask.go).
	if err := rehearseBeforeAsking(event, func(txApp core.App) error {
		_, _, err := admitProductFileManualCleanup(event.Request.Context(), txApp, request)
		return err
	}); err != nil {
		return productFileManualCleanupAPIError(event, err, "admit")
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	job, duplicate, err := admitProductFileManualCleanup(event.Request.Context(), event.App, request)
	if err != nil {
		return productFileManualCleanupAPIError(event, err, "admit")
	}
	notifyProductFileManualCleanupAfterCommit(event.App)
	response, err := productFileManualCleanupAnswer(event.Request.Context(), event.App, job, duplicate)
	if err != nil {
		return productFileManualCleanupAPIError(event, err, "response")
	}
	status := http.StatusAccepted
	if job.Status != fileManualCleanupStatusRunning {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func readProductFileManualCleanup(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	if workspaceID == "" || jobID == "" {
		return productFileManualCleanupAPIError(event, errFileManualCleanupInvalid, "validate-read")
	}
	if _, err := authorizeWorkspace(
		event.App, user.Id, workspaceID, permissionWorkspaceOwner,
	); err != nil {
		return productFileManualCleanupAPIError(event, err, "authorize-read")
	}
	job, err := loadProductFileManualCleanupJob(event.Request.Context(), event.App, jobID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.WorkspaceID != workspaceID) {
		return productFileManualCleanupAPIError(event, errFileManualCleanupNotFound, "read")
	}
	if err != nil {
		return productFileManualCleanupAPIError(event, err, "read")
	}
	if job.Status == fileManualCleanupStatusRunning {
		notifyProductFileManualCleanupAfterCommit(event.App)
	}
	response, err := productFileManualCleanupAnswer(event.Request.Context(), event.App, job, false)
	if err != nil {
		return productFileManualCleanupAPIError(event, err, "response-read")
	}
	return event.JSON(http.StatusOK, response)
}

func productFileManualCleanupAPIError(
	event *core.RequestEvent,
	err error,
	stage string,
) error {
	status := http.StatusServiceUnavailable
	code := errorCodeFileManualCleanupUnavailable
	message := "Couldn't continue the file cleanup job."
	switch {
	case isPermissionDenied(err):
		status, code, message = http.StatusForbidden, errorCodePermissionDenied,
			"Files in this workspace can't be cleaned up."
	case errors.Is(err, errFileManualCleanupInvalid):
		status, code, message = http.StatusBadRequest, errorCodeFileManualCleanupInvalid,
			"Check the file cleanup request."
	case errors.Is(err, errFileManualCleanupNotFound):
		status, code, message = http.StatusNotFound, errorCodeFileManualCleanupNotFound,
			"File cleanup job not found."
	case errors.Is(err, errFileManualCleanupRequestIDReused):
		status, code, message = http.StatusConflict, errorCodeFileManualCleanupRequestIDReused,
			"This request ID was used for a different file cleanup."
	case isSQLiteBusyError(err):
		code, message = errorCodeFileManualCleanupDatabaseBusy,
			"Retry the same file cleanup after the other server operation finishes."
	case isSQLiteStorageUnavailableError(err):
		code, message = errorCodeFileManualCleanupStorageUnavailable,
			"Retry the same file cleanup after server storage recovers."
	case errors.Is(err, errFileManualCleanupUnavailable):
		// Stable default.
	}
	return apiErrorWithCause(
		event, status, code, message, "file_manual_cleanup", stage, code, err,
	)
}
