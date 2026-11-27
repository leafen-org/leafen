package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A name taken before the limit became Notion's can still be typed to delete its workspace.
const maxWorkspaceStoredNameRunes = 255

var (
	errWorkspaceDeleteInvalid              = errors.New(errorCodeWorkspaceDeleteInvalid)
	errWorkspaceDeleteConfirmationMismatch = errors.New(errorCodeWorkspaceDeleteConfirmationMismatch)
	errWorkspaceDeleteRequestIDReused      = errors.New(errorCodeWorkspaceDeleteRequestIDReused)
	errWorkspaceDeleteConflict             = errors.New(errorCodeWorkspaceDeleteConflict)
	errWorkspaceDeleteUnavailable          = errors.New(errorCodeWorkspaceDeleteUnavailable)
)

func registerWorkspaceDeletionRoutes(
	event *core.ServeEvent,
	roomInvalidator workspaceRoomInvalidator,
) {
	event.Router.Route(
		workspaceDeleteMethod,
		apiRoot+workspaceDeletePath,
		deleteWorkspaceDurably(roomInvalidator),
	).Bind(apis.BodyLimit(workspaceDeletionMaxBodyBytes))
	event.Router.GET(apiRoot+workspaceDeletionPreviewPath, previewWorkspaceDeletion)
}

// previewWorkspaceDeletion says, as the delete window opens, what would
// refuse it — an active legal hold — so the owner reads it before typing
// the name, as the account deletion preview says it (2026-11-06).
func previewWorkspaceDeletion(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner)
	if err != nil {
		return workspaceDeletionError(event, err, "preview")
	}
	preview := workspaceDeletionPreviewResponse{}
	err = assertNoActiveLegalHoldForPermanentDelete(event.App, authorization.Organization.OrganizationID,
		legalHoldDeletionTarget{WorkspaceID: workspaceID})
	if errors.Is(err, errLegalHoldPreventsPermanentDelete) {
		preview.LegalHold, err = true, nil
	}
	if err != nil {
		return workspaceDeletionError(event, err, "preview")
	}
	return event.JSON(http.StatusOK, preview)
}

func deleteWorkspaceDurably(
	roomInvalidator workspaceRoomInvalidator,
) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		user, ok := authenticatedUser(event)
		if !ok {
			return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
		input := workspaceDeletionInput{}
		if err := event.BindBody(&input); err != nil {
			return workspaceDeletionError(event, errWorkspaceDeleteInvalid, "bind")
		}
		input.RequestID = strings.TrimSpace(input.RequestID)
		if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength ||
			input.RequestID == "" || len(input.RequestID) > maxWorkspaceDeletionRequestIDLength ||
			input.ConfirmationName == "" ||
			len([]rune(input.ConfirmationName)) > maxWorkspaceStoredNameRunes {
			return workspaceDeletionError(event, errWorkspaceDeleteInvalid, "validate")
		}

		release := acquireProductOperationLock("permanent-workspace-delete", workspaceID)
		defer release()
		// A key's call asks its person only for a deletion that would start
		// (user_key_acting_ask.go).
		if err := rehearseBeforeAsking(event, func(txApp core.App) error {
			_, err := admitWorkspaceDeletion(txApp, user.Id, workspaceID, input.RequestID, input.ConfirmationName)
			return err
		}); err != nil {
			return workspaceDeletionError(event, err, "admit")
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}
		admission, err := admitWorkspaceDeletion(
			event.App, user.Id, workspaceID, input.RequestID, input.ConfirmationName,
		)
		if err != nil {
			return workspaceDeletionError(event, err, "admit")
		}
		if admission.Job.Status == workspaceDeletionStatusCompleted {
			return event.JSON(http.StatusOK, workspaceDeletionResponseForJob(
				admission.Job, admission.Duplicate, admission.AlreadyDeleted,
			))
		}
		if err := roomInvalidator.InvalidateWorkspace(admission.Job.WorkspaceID); err != nil {
			return workspaceDeletionError(
				event, errors.Join(errWorkspaceDeleteUnavailable, err), "invalidate-workspace",
			)
		}
		job, err := advanceWorkspaceDeletion(event.App, admission.Job.ID)
		if err != nil {
			return workspaceDeletionError(event, err, "advance")
		}
		status := http.StatusAccepted
		if job.Status == workspaceDeletionStatusCompleted {
			status = http.StatusOK
		}
		return event.JSON(status, workspaceDeletionResponseForJob(job, false, false))
	}
}

func workspaceDeletionError(event *core.RequestEvent, err error, stage string) error {
	if quotaError, ok := organizationStorageAPIError(
		event, err, "workspace_delete", stage,
	); ok {
		return quotaError
	}
	if storageError, ok := persistentWriteStorageAPIError(
		event, err, persistentWriteWorkspaceDelete, stage,
	); ok {
		return storageError
	}
	status, code, message := http.StatusServiceUnavailable,
		errorCodeWorkspaceDeleteUnavailable, "Couldn't delete the workspace."
	switch {
	case errors.Is(err, errWorkspaceDeleteInvalid):
		status, code, message = http.StatusBadRequest,
			errorCodeWorkspaceDeleteInvalid, "Check the workspace deletion request."
	case errors.Is(err, errWorkspaceDeleteConfirmationMismatch):
		status, code, message = http.StatusConflict,
			errorCodeWorkspaceDeleteConfirmationMismatch, "The workspace name doesn't match."
	case errors.Is(err, errWorkspaceDeleteRequestIDReused):
		status, code, message = http.StatusConflict,
			errorCodeWorkspaceDeleteRequestIDReused, "This request ID was used for a different deletion request."
	case errors.Is(err, errWorkspaceDeleteConflict):
		status, code, message = http.StatusConflict,
			errorCodeWorkspaceDeleteConflict, "Another workspace operation is in progress."
	case errors.Is(err, errLegalHoldPreventsPermanentDelete):
		status, code, message = http.StatusPreconditionFailed,
			errorCodeLegalHoldPreventsPermanentDelete, "Release the active legal hold, then delete."
	case isPermissionDenied(err):
		status, code, message = http.StatusNotFound,
			errorCodeWorkspaceNotFound, "Workspace not found."
	}
	return writeAPIError(
		event, status, code, message, "workspace_delete", stage, code, err,
		diagnosticSourcePosition(1),
	)
}
