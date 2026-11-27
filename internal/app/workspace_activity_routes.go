package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func registerWorkspaceActivityRoutes(event *core.ServeEvent) {
	event.Router.Route(workspaceActivityListMethod, apiRoot+workspaceActivityListPath, listWorkspaceActivitiesRoute)
	event.Router.Route(workspaceActivityChangesMethod, apiRoot+workspaceActivityChangesPath, readWorkspaceActivityChangesRoute)
	// The feed lets go of its old groups at the start and every hour, behind
	// both and stopped with the server (server_background.go).
	trim := func() {
		serverBackgroundOf(event.App).run("workspace activity trim", func(context.Context) {
			if err := trimWorkspaceActivity(event.App, time.Now().UTC()); err != nil {
				event.App.Logger().Warn("workspace activity: couldn't let go of the oldest groups", "error", err)
			}
		})
	}
	trim()
	event.App.Cron().MustAdd("leafenWorkspaceActivityTrim", "41 * * * *", trim)
}

func listWorkspaceActivitiesRoute(event *core.RequestEvent) error {
	event.Response.Header().Set("Cache-Control", "no-store")
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	query := event.Request.URL.Query()
	response, err := readWorkspaceSourceActivities(event.Request.Context(), event.App, user, strings.TrimSpace(event.Request.PathValue("workspaceId")), query.Get("limit"), query.Get("cursor"),
		strings.TrimSpace(query.Get("sourceId")), strings.TrimSpace(query.Get("trashOf")))
	if err == nil {
		return event.JSON(http.StatusOK, response)
	}
	if isPermissionDenied(err) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have access to this workspace.")
	}
	if errors.Is(err, errWorkspaceActivityInvalid) {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceActivityInvalid, "The activity cursor or limit is invalid. Refresh the list.")
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceActivityFailed, "The activity list could not be read.", "workspace_activity", "list", errorCodeWorkspaceActivityFailed, err)
}

func readWorkspaceActivityChangesRoute(event *core.RequestEvent) error {
	event.Response.Header().Set("Cache-Control", "no-store")
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	response, err := readWorkspaceActivityChangesFor(event.Request.Context(), event.App, user, strings.TrimSpace(event.Request.PathValue("workspaceId")),
		strings.TrimSpace(event.Request.PathValue("activityId")))
	if err == nil {
		return event.JSON(http.StatusOK, response)
	}
	if isPermissionDenied(err) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have access to this workspace.")
	}
	if errors.Is(err, errWorkspaceActivityMissing) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceActivityMissing, "This update is no longer in the list.")
	}
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceActivityFailed, "The update's changes could not be read.", "workspace_activity", "changes", errorCodeWorkspaceActivityFailed, err)
}
