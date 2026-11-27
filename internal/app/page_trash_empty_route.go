package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const pageTrashEmptyMaxBodyBytes = 4096

var (
	errTrashEmptyInvalid         = &pageTrashEmptyFailure{code: errorCodeTrashEmptyInvalid}
	errTrashEmptyRequestIDReused = &pageTrashEmptyFailure{code: errorCodeTrashEmptyRequestIDReused}
	errTrashEmptyConflict        = &pageTrashEmptyFailure{code: errorCodeTrashEmptyConflict}
	errTrashEmptyUnavailable     = &pageTrashEmptyFailure{code: errorCodeTrashEmptyUnavailable}
)

type pageTrashEmptyFailure struct {
	code string
}

func (failure *pageTrashEmptyFailure) Error() string {
	return failure.code
}

func emptyWorkspaceTrash(
	roomInvalidator workspaceRoomInvalidator,
) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		user, ok := authenticatedUser(event)
		if !ok {
			return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
		input := trashEmptyInput{}
		if err := event.BindBody(&input); err != nil {
			return pageTrashEmptyError(event, errTrashEmptyInvalid, "bind")
		}
		input.RequestID = strings.TrimSpace(input.RequestID)
		if workspaceID == "" || input.RequestID == "" ||
			len([]rune(input.RequestID)) > maxPagePermanentDeleteRequestIDRunes {
			return pageTrashEmptyError(event, errTrashEmptyInvalid, "validate")
		}

		// A key's call asks its person only for an emptying that would start
		// (user_key_acting_ask.go).
		if err := rehearseBeforeAsking(event, func(txApp core.App) error {
			_, err := admitPageTrashEmpty(txApp, user.Id, workspaceID, input.RequestID)
			return err
		}); err != nil {
			return pageTrashEmptyError(event, err, "admit")
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}
		admission, err := admitPageTrashEmpty(
			event.App, user.Id, workspaceID, input.RequestID,
		)
		if err != nil {
			return pageTrashEmptyError(event, err, "admit")
		}
		if admission.Job.Status == trashEmptyStatusCompleted {
			response, err := pageTrashEmptyResponseForJob(
				event.App, admission.Job, admission.Duplicate,
			)
			if err != nil {
				return pageTrashEmptyError(event, err, "terminal-response")
			}
			return event.JSON(http.StatusOK, response)
		}

		if err := invalidatePermanentPageRooms(
			roomInvalidator, admission.Job.WorkspaceID,
		); err != nil {
			return pageTrashEmptyError(
				event, errors.Join(errTrashEmptyUnavailable, err), "invalidate-workspace",
			)
		}
		job, err := advancePageTrashEmpty(event.App, admission.Job.ID)
		if err != nil {
			return pageTrashEmptyError(event, err, "advance")
		}
		response, err := pageTrashEmptyResponseForJob(event.App, job, false)
		if err != nil {
			return pageTrashEmptyError(event, err, "response")
		}
		status := http.StatusAccepted
		if response.Status == trashEmptyStatusCompleted {
			status = http.StatusOK
		}
		return event.JSON(status, response)
	}
}

func pageTrashEmptyError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusServiceUnavailable,
		errorCodeTrashEmptyUnavailable, "Couldn't empty the trash."
	switch {
	case errors.Is(err, errTrashEmptyInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodeTrashEmptyInvalid, "Check the empty trash request."
	case errors.Is(err, errTrashEmptyRequestIDReused):
		status, code, message = http.StatusConflict,
			errorCodeTrashEmptyRequestIDReused, "This request ID was used for a different trash emptying."
	case errors.Is(err, errTrashEmptyConflict),
		errors.Is(err, errPagePermanentDeleteConflict),
		errors.Is(err, errPagePermanentDeleteNotTrashed),
		errors.Is(err, errPagePermanentDeleteRequestIDReused):
		status, code, message = http.StatusConflict,
			errorCodeTrashEmptyConflict, "Another trash change or deletion is in progress."
	case errors.Is(err, errLegalHoldPreventsPermanentDelete):
		status, code, message = http.StatusPreconditionFailed,
			errorCodeLegalHoldPreventsPermanentDelete,
			"Release the active legal hold, then empty the trash."
	case isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodeWorkspaceNotFound, "Workspace not found."
	}
	reference := newPermanentPageDiagnosticReference()
	return writeAPIErrorWithReference(
		event, status, code, message, reference, "page_trash_empty", stage,
		code, err, diagnosticSourcePosition(1),
	)
}
