package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func registerNotionImportRoutes(event *core.ServeEvent, jobs *durableJobRunner) {
	event.Router.GET(
		apiRoot+"/workspaces/{workspaceId}/notion-import/capabilities",
		notionImportCapabilitiesHandler,
	)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/notion-import/connections", jobs.createNotionImportConnection)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/notion-import/connections", jobs.listNotionImportConnections)
	event.Router.DELETE(apiRoot+"/workspaces/{workspaceId}/notion-import/connections/{connectionId}", jobs.revokeNotionImportConnection)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/notion-import/connections/{connectionId}/scope", jobs.readNotionImportScope)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/notion-import/jobs", jobs.createNotionImportJob)
	event.Router.POST(
		apiRoot+"/workspaces/{workspaceId}/notion-import/jobs/{jobId}/retry",
		jobs.retryNotionImportJob,
	)
	event.Router.POST(
		apiRoot+"/workspaces/{workspaceId}/notion-import/jobs/{jobId}/cancel",
		jobs.cancelOrStopNotionImport,
	)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/notion-import/jobs/{jobId}", jobs.getNotionImportJob)
	event.Router.GET(
		apiRoot+"/workspaces/{workspaceId}/notion-import/jobs/{jobId}/file-outcomes",
		readNotionImportFileOutcomes,
	)
	event.Router.POST(
		apiRoot+"/workspaces/{workspaceId}/notion-import/jobs/{jobId}/file-outcomes/{sourceId}/retry",
		jobs.retryNotionImportFileOutcome,
	)
	event.Router.GET(
		apiRoot+"/workspaces/{workspaceId}/notion-import/jobs/{jobId}/block-issues",
		readNotionImportBlockIssues,
	)
	registerNotionMigrationRoutes(event, jobs)
	registerNotionMigrationFormatsRoutes(event)
}

func notionImportCapabilitiesHandler(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if workspaceID == "" {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the workspace.")
	}
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	} else if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportConnectionFailed, "Couldn't check the import capability.", err)
	}
	return event.JSON(http.StatusOK, notionImportCapabilities{
		ExecutionOwner: "server",
		Steps:          []string{notionImportPhaseConnect, notionImportPhaseScope, notionImportPhaseApply},
		CopyFiles:      true,
	})
}
