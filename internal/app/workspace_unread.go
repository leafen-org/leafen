package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Which of a person's workspaces hold unread personal notifications, for the
// red dot Notion's workspace switcher puts on those workspaces and on the
// sidebar's workspace mark (seen 2026-10-06). A workspace the person can no
// longer read is left out rather than refusing the others.

const workspaceUnreadLimit = 64

type workspaceUnreadResponse struct {
	Unread []string `json:"unread"`
}

func registerWorkspaceUnreadRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/account/workspace-unread", listWorkspaceUnreadRoute)
}

func listWorkspaceUnreadRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceIDs := event.Request.URL.Query()["workspaceId"]
	if len(workspaceIDs) == 0 || len(workspaceIDs) > workspaceUnreadLimit {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Name up to 64 workspaces.")
	}
	response, err := readWorkspaceUnread(event.App, user.Id, workspaceIDs)
	if err != nil {
		return notificationInboxAPIError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func readWorkspaceUnread(app core.App, userID string, workspaceIDs []string) (workspaceUnreadResponse, error) {
	response := workspaceUnreadResponse{Unread: []string{}}
	seen := map[string]bool{}
	for _, raw := range workspaceIDs {
		workspaceID := strings.TrimSpace(raw)
		if workspaceID == "" || seen[workspaceID] {
			continue
		}
		seen[workspaceID] = true
		// The unread count the inbox shows, personal notifications only.
		unread, err := notificationUnreadCount(app, userID, workspaceID, true)
		if isPermissionDenied(err) {
			continue
		}
		if err != nil {
			return response, err
		}
		if unread > 0 {
			response.Unread = append(response.Unread, workspaceID)
		}
	}
	return response, nil
}
