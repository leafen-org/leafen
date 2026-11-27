package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerTeamspaceRoutes(event *core.ServeEvent, roomInvalidator workspaceRoomInvalidator) {
	event.Router.GET(apiRoot+teamspaceSnapshotPath, readTeamspaceSnapshot)
	event.Router.GET(apiRoot+teamspaceManagementPath, readTeamspaceManagement)
	event.Router.POST(apiRoot+teamspaceMutationPath, func(request *core.RequestEvent) error {
		return mutateTeamspace(request, roomInvalidator)
	}).Bind(apis.BodyLimit(teamspaceMutationMaxBodyBytes))
}

func readTeamspaceSnapshot(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	query := event.Request.URL.Query()
	read := readTeamspaceSnapshotState
	if strings.TrimSpace(query.Get("archived")) == "true" {
		read = readArchivedTeamspaceSnapshotState
	} else if strings.TrimSpace(query.Get("table")) == "true" {
		read = readTeamspaceTableSnapshotState
	}
	response, err := read(event.Request.Context(), event.App, user.Id, workspaceID,
		strings.TrimSpace(query.Get("cursor")))
	if err != nil {
		return teamspaceRouteError(event, err, "snapshot")
	}
	return event.JSON(http.StatusOK, response)
}

func readTeamspaceManagement(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	query := event.Request.URL.Query()
	response, err := readTeamspaceManagementState(
		event.Request.Context(), event.App, user.Id,
		strings.TrimSpace(event.Request.PathValue("workspaceId")),
		strings.TrimSpace(event.Request.PathValue("teamspaceId")),
		strings.TrimSpace(query.Get("memberCursor")), strings.TrimSpace(query.Get("requestCursor")),
		strings.TrimSpace(query.Get("principalCursor")),
	)
	if err != nil {
		return teamspaceRouteError(event, err, "management")
	}
	return event.JSON(http.StatusOK, response)
}

func mutateTeamspace(event *core.RequestEvent, roomInvalidator workspaceRoomInvalidator) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := teamspaceMutationInput{}
	if err := event.BindBody(&input); err != nil {
		return teamspaceRouteError(event, bodyRefusal(errTeamspaceInvalid, "teamspace change", err), "mutation-input")
	}
	response, fresh, err := executeTeamspaceMutation(
		event.Request.Context(), event.App, roomInvalidator, user.Id,
		strings.TrimSpace(event.Request.PathValue("workspaceId")), input,
	)
	if err != nil {
		return teamspaceRouteError(event, err, "mutation")
	}
	status := http.StatusOK
	if response.PublicationPending {
		status = http.StatusAccepted
	} else if fresh && response.Action == teamspaceActionCreate {
		status = http.StatusCreated
	}
	return event.JSON(status, response)
}

func teamspaceRouteError(event *core.RequestEvent, err error, stage string) error {
	switch {
	case errors.Is(err, errTeamspaceInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeTeamspaceInvalid, refusalMessage("Check the teamspace request.", refusalReason(err)))
	case errors.Is(err, errTeamspaceNotFound), isPermissionDenied(err):
		return apiError(event, http.StatusNotFound, errorCodeTeamspaceNotFound, "Teamspace not found.")
	case errors.Is(err, errTeamspaceRequestIDReused):
		return apiError(event, http.StatusConflict, errorCodeTeamspaceRequestIDReused, "This request was already used for a different change.")
	case errors.Is(err, errTeamspacePrivateDefault):
		return apiError(event, http.StatusConflict, errorCodeTeamspacePrivateDefault,
			"An invite-only teamspace can't be the default teamspace, which holds every member.")
	case errors.Is(err, errTeamspaceDefaultMember):
		return apiError(event, http.StatusConflict, errorCodeTeamspaceDefaultMember,
			"Everyone in the workspace is in its default teamspace, so no one is taken out of it.")
	case errors.Is(err, errTeamspaceDefaultRequired):
		return apiError(event, http.StatusConflict, errorCodeTeamspaceDefaultRequired,
			"A workspace keeps at least one default teamspace.")
	case errors.Is(err, errTeamspaceLastOwner):
		return apiError(event, http.StatusConflict, errorCodeTeamspaceLastOwner, "A teamspace keeps at least one owner.")
	case errors.Is(err, errTeamspaceConflict):
		return apiError(event, http.StatusConflict, errorCodeTeamspaceConflict, teamspaceConflictMessage(err))
	case errors.Is(err, errOrganizationSharingSecurityBlocked):
		return writeSharingSecurityBlockedError(event)
	case isSQLiteBusyError(err):
		return apiErrorWithCause(event, http.StatusServiceUnavailable, errorCodeTeamspaceDatabaseBusy,
			"The teamspace store is busy. Try again.", "teamspace", stage, errorCodeTeamspaceDatabaseBusy, err)
	default:
		return apiErrorWithCause(event, http.StatusServiceUnavailable, errorCodeTeamspaceStorageUnavailable,
			"The teamspace store is unavailable.", "teamspace", stage, errorCodeTeamspaceStorageUnavailable, err)
	}
}
