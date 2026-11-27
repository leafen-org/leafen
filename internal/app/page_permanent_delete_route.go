package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const pagePermanentDeleteMaxBodyBytes = 4096

var (
	errPagePermanentDeleteInvalid         = &pagePermanentDeleteFailure{code: errorCodePagePermanentDeleteInvalid}
	errPagePermanentDeleteNotTrashed      = &pagePermanentDeleteFailure{code: errorCodePagePermanentDeleteNotTrashed}
	errPagePermanentDeleteRequestIDReused = &pagePermanentDeleteFailure{code: errorCodePagePermanentDeleteRequestIDReused}
	errPagePermanentDeleteConflict        = &pagePermanentDeleteFailure{code: errorCodePagePermanentDeleteConflict}
	errPagePermanentDeleteUnavailable     = &pagePermanentDeleteFailure{code: errorCodePagePermanentDeleteUnavailable}
)

type pagePermanentDeleteFailure struct {
	code string
}

func (failure *pagePermanentDeleteFailure) Error() string {
	return failure.code
}

type pagePermanentDeleteAdmission struct {
	Job            storedPagePermanentDeleteJob
	Admitted       bool
	Duplicate      bool
	AlreadyDeleted bool
}

func deletePagePermanently(
	roomInvalidator workspaceRoomInvalidator,
) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		user, ok := authenticatedUser(event)
		if !ok {
			return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
		if answered, err := rowPageDeletedOnItsOwnPath(event, user.Id, pageID); answered {
			return err
		}
		input := pagePermanentDeleteInput{}
		if err := event.BindBody(&input); err != nil {
			return pagePermanentDeleteError(event, errPagePermanentDeleteInvalid, "bind")
		}
		input.RequestID = strings.TrimSpace(input.RequestID)
		if pageID == "" || input.RequestID == "" ||
			len([]rune(input.RequestID)) > maxPagePermanentDeleteRequestIDRunes {
			return pagePermanentDeleteError(event, errPagePermanentDeleteInvalid, "validate")
		}
		// A key's call asks its person only for a page this would delete.
		if err := pagePermanentDeleteReady(event.App, user.Id, pageID); err != nil {
			return pagePermanentDeleteError(event, err, "check")
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}

		// A permanent deletion waits 30 days out of everyone's sight; one
		// already under way from before goes on below.
		hold, duplicate, err := holdPageForPermanentDeletion(event.App, personAccessSubject(user.Id), pageID, input.RequestID)
		if err == nil {
			if err := invalidatePermanentPageRooms(roomInvalidator, hold.WorkspaceID); err != nil {
				return pagePermanentDeleteError(
					event, errors.Join(errPagePermanentDeleteUnavailable, err), "invalidate-workspace",
				)
			}
			return event.JSON(http.StatusOK, pagePermanentDeleteResponseForHold(hold, duplicate))
		}
		if !errors.Is(err, errPageDeletionJobExists) {
			return pagePermanentDeleteError(event, err, "hold")
		}

		admission, err := admitPagePermanentDelete(
			event.App, personAccessSubject(user.Id), pageID, input.RequestID,
		)
		if err != nil {
			return pagePermanentDeleteError(event, err, "admit")
		}
		if admission.Job.Status == pagePermanentDeleteStatusCompleted {
			response, err := pagePermanentDeleteResponseForJob(
				event.App, admission.Job, admission.Duplicate, admission.AlreadyDeleted,
			)
			if err != nil {
				return pagePermanentDeleteError(event, err, "terminal-response")
			}
			return event.JSON(http.StatusOK, response)
		}

		if err := invalidatePermanentPageRooms(roomInvalidator, admission.Job.WorkspaceID); err != nil {
			return pagePermanentDeleteError(
				event, errors.Join(errPagePermanentDeleteUnavailable, err), "invalidate-workspace",
			)
		}
		job, err := advancePagePermanentDelete(event.App, admission.Job.ID)
		if err != nil {
			return pagePermanentDeleteError(event, err, "advance")
		}
		response, err := pagePermanentDeleteResponseForJob(event.App, job, false, false)
		if err != nil {
			return pagePermanentDeleteError(event, err, "response")
		}
		status := http.StatusAccepted
		if response.Status == pagePermanentDeleteStatusCompleted {
			status = http.StatusOK
		}
		return event.JSON(status, response)
	}
}

func pagePermanentDeleteError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusServiceUnavailable,
		errorCodePagePermanentDeleteUnavailable, "Couldn't permanently delete the page."
	switch {
	case errors.Is(err, errPagePermanentDeleteInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodePagePermanentDeleteInvalid, "Check the permanent deletion request."
	case errors.Is(err, errPagePermanentDeleteNotTrashed):
		status, code, message = http.StatusConflict,
			errorCodePagePermanentDeleteNotTrashed, "Move the page to Trash first."
	case errors.Is(err, errPagePermanentDeleteRequestIDReused):
		status, code, message = http.StatusConflict,
			errorCodePagePermanentDeleteRequestIDReused, "This request ID was used for a different permanent deletion."
	case errors.Is(err, errPagePermanentDeleteConflict):
		status, code, message = http.StatusConflict,
			errorCodePagePermanentDeleteConflict, "Another permanent deletion is in progress."
	case errors.Is(err, errLegalHoldPreventsPermanentDelete):
		status, code, message = http.StatusPreconditionFailed,
			errorCodeLegalHoldPreventsPermanentDelete,
			"Release the active legal hold, then permanently delete."
	case errors.Is(err, errPageDeletionInTrash):
		status, code, message = http.StatusConflict, errorCodePageInTrash, pageDeletionInTrashMessage(err)
	case pageAccessHeld(err) != "":
		// A page an operation holds a while, from who may delete it, says so (page_access_hold.go).
		busy := pageAccessHeld(err)
		apiErrorAbout(event, &apiErrorTarget{Kind: "page", ID: strings.TrimSpace(event.Request.PathValue("pageId"))})
		apiErrorBecause(event, "", busy)
		status, code, message = http.StatusLocked, errorCodePageBusy, pageBusyMessage(busy)
	case isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodePageNotFound, "Page not found."
	}
	reference := newPermanentPageDiagnosticReference()
	return writeAPIErrorWithReference(
		event, status, code, message, reference, "page_permanent_delete", stage,
		code, err, diagnosticSourcePosition(1),
	)
}

// pagePermanentDeleteReady checks, without changing anything, what the hold
// checks before it holds a page (holdPageForPermanentDeletion): the person
// owns the page's workspace and the page is in the trash, or one under way
// already goes on.
func pagePermanentDeleteReady(app core.App, actorID, pageID string) error {
	if hold, err := loadPageDeletionHold(app, pageID); err == nil {
		_, err := authorizeWorkspace(app, actorID, hold.WorkspaceID, permissionWorkspaceOwner)
		return err
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := authorizePage(app, actorID, pageID, permissionWorkspaceOwner); err != nil {
		if _, jobErr := loadPagePermanentDeleteByRoot(app, actorID, pageID); jobErr == nil {
			return nil
		}
		return err
	}
	if trashed, err := pageInTrashPath(app, pageID); err != nil {
		return err
	} else if !trashed {
		return errPagePermanentDeleteNotTrashed
	}
	return nil
}
