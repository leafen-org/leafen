package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// rebuildWorkspaceSearchAsking is the search rebuild's route: a key's call
// asks its person once the rebuild would be done — the workspace one they
// own — and not before (user_key_acting_ask.go); the rebuild then checks
// it again and starts.
func rebuildWorkspaceSearchAsking(event *core.RequestEvent) error {
	if !userKeyAsksWhereItActs(event) {
		return rebuildWorkspaceSearch(event)
	}
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner); isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	} else if err != nil {
		return workspaceSearchInternalError(
			event, err, persistentWriteWorkspaceSearchRebuild, "authorize",
			errorCodeWorkspaceSearchRebuildFailed, "Couldn't start the search rebuild.",
		)
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	return rebuildWorkspaceSearch(event)
}
