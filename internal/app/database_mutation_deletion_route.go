package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const databaseMutationMaxBodyBytes = 4096

func registerDatabaseMutationDeletionRoutes(
	event *core.ServeEvent,
) {
	event.Router.POST(apiRoot+databaseMutationStartPath, startDatabaseMutationDeletion).
		Bind(apis.BodyLimit(databaseMutationMaxBodyBytes))
	event.Router.GET(apiRoot+databaseMutationReadPath, readDatabaseMutationDeletion)
}

func startDatabaseMutationDeletion(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := databaseMutationInput{}
	if err := event.BindBody(&input); err != nil {
		return writeDatabaseMutationDeletionError(event, errDatabaseMutationInvalid, "bind")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.MutationID = strings.TrimSpace(input.MutationID)
	input.TargetKind = strings.TrimSpace(input.TargetKind)
	input.TargetID = strings.TrimSpace(input.TargetID)
	if pageID == "" || input.RequestID == "" || input.MutationID == "" || input.TargetID == "" ||
		utf8.RuneCountInString(input.RequestID) > maxDatabaseMutationRequestIDRunes ||
		len(input.MutationID) > maxGraphIdentifierLength || len(input.TargetID) > maxGraphIdentifierLength ||
		databaseMutationInitialPhase(input.TargetKind) == "" {
		return writeDatabaseMutationDeletionError(event, errDatabaseMutationInvalid, "validate")
	}
	if input.TargetKind == databaseMutationTargetProperty {
		// A restricted property goes only for whoever may edit it.
		if err := refuseRestrictedPropertyEdit(event.App, user.Id, input.TargetID); err != nil {
			return writeGraphError(event, err, "property-access", errorCodeGraphPropertyAccessDenied, "You can't change this property.")
		}
	}
	if input.TargetKind == databaseMutationTargetRow {
		// An unlinked item goes only for whoever may edit it.
		if err := refuseUnlinkedRowDeletion(event.App, user.Id, input.TargetID); err != nil {
			return writeGraphError(event, err, "row-access", errorCodeGraphRowAccessDenied, "You can't change this item.")
		}
	}
	admission, err := admitDatabaseMutationDeletion(event.App, user.Id, pageID, input)
	if err != nil {
		return writeDatabaseMutationDeletionError(event, err, "admit")
	}
	notifyDatabaseMutationDeletionAfterCommit(event.App)
	response, err := databaseMutationDeletionResponse(admission.Job, admission.Duplicate)
	if err != nil {
		return writeDatabaseMutationDeletionError(event, err, "response")
	}
	status := http.StatusAccepted
	if response.Status == databaseMutationStatusCompleted {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func readDatabaseMutationDeletion(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	if pageID == "" || jobID == "" {
		return writeDatabaseMutationDeletionError(event, errDatabaseMutationInvalid, "validate-read")
	}
	job, err := loadDatabaseMutationDeletionByID(event.App, jobID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.PageID != pageID) {
		return writeDatabaseMutationDeletionError(event, errDatabaseMutationNotFound, "read")
	}
	if err != nil {
		return writeDatabaseMutationDeletionError(event, err, "read")
	}
	if _, err := authorizePageBody(event.App, user.Id, pageID, permissionWorkspaceWrite); err != nil {
		return writeDatabaseMutationDeletionError(event, errDatabaseMutationNotFound, "authorize-read")
	}
	if job.Status == databaseMutationStatusPending {
		notifyDatabaseMutationDeletionAfterCommit(event.App)
	}
	response, err := databaseMutationDeletionResponse(job, false)
	if err != nil {
		return writeDatabaseMutationDeletionError(event, err, "response-read")
	}
	return event.JSON(http.StatusOK, response)
}

func writeDatabaseMutationDeletionError(event *core.RequestEvent, err error, stage string) error {
	if isSQLiteBusyError(err) {
		return apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeDatabaseMutationDatabaseBusy,
			"The database change store is locked by another operation.",
			"database_mutation", stage, errorCodeDatabaseMutationDatabaseBusy, err,
		)
	}
	if isSQLiteStorageUnavailableError(err) {
		return apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeDatabaseMutationStorageUnavailable,
			"The database change store is temporarily unavailable.",
			"database_mutation", stage, errorCodeDatabaseMutationStorageUnavailable, err,
		)
	}
	status, code, message := http.StatusServiceUnavailable,
		errorCodeDatabaseMutationUnavailable, "Couldn't continue the database deletion."
	switch {
	case errors.Is(err, errDatabaseMutationInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodeDatabaseMutationInvalid, "Check the database deletion request."
	case errors.Is(err, errDatabaseMutationNotFound), isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodeDatabaseMutationNotFound, "Database deletion job not found."
	case errors.Is(err, errDatabaseMutationRequestIDReused):
		status, code, message = http.StatusConflict,
			errorCodeDatabaseMutationRequestIDReused, "This request ID was used for a different deletion."
	case errors.Is(err, errDatabaseMutationConflict):
		status, code, message = http.StatusConflict,
			errorCodeDatabaseMutationConflict, "Clean up the linked items, then try again."
	case errors.Is(err, errWikiDatabaseKept):
		status, code, message = http.StatusConflict, errorCodeDatabaseMutationConflict, wikiDatabaseKeptMessage
	case errors.Is(err, errWikiPropertyKept):
		status, code, message = http.StatusConflict, errorCodeDatabaseMutationConflict, wikiPropertyKeptMessage
	}
	return apiErrorWithCause(event, status, code, message, "database_mutation", stage, code, err)
}
