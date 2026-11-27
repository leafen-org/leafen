package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The organization's workspaces, for an admin choosing among them — a legal
// hold's or a content search's workspaces, or the one an agent policy is for —
// whether or not the admin is in each one: an organization's holds cover its
// workspaces, not the admin's own (2026-11-06).
func registerOrganizationWorkspaceListRoute(event *core.ServeEvent) {
	event.Router.GET(apiRoot+organizationWorkspaceListPath, listOrganizationWorkspacesRoute)
}

func listOrganizationWorkspacesRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	if _, err := authorizeOrganization(event.App, user.Id, organizationID, permissionOrganizationAdmin); err != nil {
		return organizationSettingsError(event, err, "workspaces")
	}
	summaries, err := OrganizationWorkspaceSummaries(event.App, organizationID)
	if err != nil {
		return organizationSettingsError(event, err, "workspaces")
	}
	list := organizationWorkspaceListResponse{Workspaces: make([]organizationWorkspaceSummary, 0, len(summaries))}
	for _, summary := range summaries {
		list.Workspaces = append(list.Workspaces, organizationWorkspaceSummary{ID: summary.ID, Name: summary.Name})
	}
	return event.JSON(http.StatusOK, list)
}
