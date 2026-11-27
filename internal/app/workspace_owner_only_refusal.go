package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A member refused a change only the workspace's owners make is told so —
// 403, saying what only they do — and only someone outside the workspace is
// told it isn't found, as before (2026-11-26, the wider scorecard: a member
// asking their agent to remove another member was answered 404 "Workspace
// not found.", and the agent told them so).

// workspaceOwnersOnly answers a permission refusal of a change only the
// workspace's owners make: to one who reads the workspace, what only the
// owners do; to anyone else, the workspace's own not-found answer.
func workspaceOwnersOnly(event *core.RequestEvent, actorID, workspaceID, only string, notFound func() error) error {
	if _, err := authorizeWorkspace(event.App, actorID, workspaceID, permissionWorkspaceRead); err != nil {
		return notFound()
	}
	return apiError(event, http.StatusForbidden, errorCodePermissionDenied, only)
}

// workspaceNotFound is the answer for a workspace someone can't read.
func workspaceNotFound(event *core.RequestEvent) func() error {
	return func() error {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
}
