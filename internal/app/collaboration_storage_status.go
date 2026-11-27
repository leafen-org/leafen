package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func registerCollaborationStorageStatusRoute(
	event *core.ServeEvent,
	persistence *pocketBaseYgoPersistence,
) {
	event.Router.GET(collaborationStorageStatusPath, func(request *core.RequestEvent) error {
		user, ok := authenticatedUser(request)
		if !ok {
			return apiError(
				request, http.StatusUnauthorized, errorCodeUnauthorized,
				"Login required.",
			)
		}
		room := strings.TrimSpace(request.Request.PathValue("room"))
		if room == "" || len(room) > maxWorkspacePermissionIdentifierLength {
			return apiError(
				request, http.StatusBadRequest, errorCodeInvalidInput,
				"The page ID is invalid.",
			)
		}
		rowPage, err := isDatabaseRowPageRoom(request.App, room)
		if err == nil && rowPage {
			_, _, err = authorizeDatabaseRowBodyRoom(request.App, user.Id, room)
			if err != nil && !isPermissionDenied(err) && errors.Is(err, errDatabaseRowPageNotFound) {
				err = permissionDenied()
			}
		} else if err == nil {
			// Anyone who reads the page reads its storage state, as they
			// join its room.
			_, err = authorizePage(request.App, user.Id, room, permissionWorkspaceRead)
		}
		if isPermissionDenied(err) {
			// Held by an operation, it is busy to a member, who waits for it
			// as for its room (page_busy.go).
			if busy := pageSetBusyRefusal(request.App, user.Id, []string{room}); busy != nil {
				return writeGraphError(request, busy, "status", errorCodeYgoPersistence, "Couldn't check the collaboration storage state.")
			}
			return apiError(request, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
		} else if err != nil {
			return apiErrorWithCause(
				request, http.StatusInternalServerError, errorCodeYgoPersistence,
				"Couldn't check the collaboration storage state.", "ygo.persistence.status",
				"authorization", errorCodeYgoPersistence, err,
			)
		}
		return request.JSON(http.StatusOK, persistence.settlement.status(room))
	})
}
