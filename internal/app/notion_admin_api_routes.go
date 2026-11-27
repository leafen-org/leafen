package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAdminAPIRoot                         = "/admin/v1"
	notionAdminAPILegalHoldsPath               = notionAdminAPIRoot + "/legal_holds"
	notionAdminAPIMCPConnectionsPath           = notionAdminAPIRoot + "/mcp_client_connections"
	notionAdminAPISpacesPath                   = notionAdminAPIRoot + "/spaces"
	notionAdminAPIManagedUserSessionRevokePath = notionAdminAPIRoot + "/managed_users/revoke_session"
)

func registerNotionAdminAPIRoutes(event *core.ServeEvent, jobs *durableJobRunner) {
	// Its answers come through Notion's gateway as the public API's do: the
	// request's id in their header and JSON in UTF-8 (serveNotionAnswer).
	// Their bodies are the Admin API's own, a request_id only where one is
	// written.
	serve := func(event *core.RequestEvent) error {
		return serveNotionAnswer(event, func() error {
			notionAnswerWithoutRequestID(event)
			return serveNotionAdminAPI(event, jobs)
		})
	}
	for _, method := range notionCompatibilityMethods {
		event.Router.Route(method, notionAdminAPIRoot, serve)
		event.Router.Route(method, notionAdminAPIRoot+"/{path...}", serve)
	}
}

func serveNotionAdminAPI(event *core.RequestEvent, jobs *durableJobRunner) error {
	for _, serve := range notionAdminAPIHandlers {
		if handled, err := serve(event); handled {
			return err
		}
	}
	if workspaceID, groupID, userID, route, ok := notionAdminSpaceGroupRoute(event.Request.URL.Path); ok {
		if handled, err := serveNotionAdminSpaceGroups(
			event, workspaceID, groupID, userID, route,
		); handled {
			return err
		}
	}
	if workspaceID, targetID, route, ok := notionAdminCreditAccessRoute(event.Request.URL.Path); ok {
		if handled, err := serveNotionAdminCreditAccess(event, workspaceID, targetID, route); handled {
			return err
		}
	}
	if workspaceID, tokenID, ok := notionAdminSpacePersonalAccessTokenIDs(event.Request.URL.Path); ok {
		if handled, err := serveNotionAdminPersonalAccessTokens(event, workspaceID, tokenID); handled {
			return err
		}
	}
	if event.Request.URL.Path == notionAdminAPISpacesPath && event.Request.Method == http.MethodGet {
		return serveNotionAdminSpacesList(event)
	}
	if workspaceID, userID, ok := notionAdminUserCreditUsageRoute(event.Request.URL.Path); ok &&
		event.Request.Method == http.MethodGet {
		return serveNotionAdminUserCreditUsage(event, workspaceID, userID)
	}
	if workspaceID, ok := notionAdminSpaceUsersID(event.Request.URL.Path); ok &&
		event.Request.Method == http.MethodGet {
		return serveNotionAdminSpaceUsersList(event, workspaceID)
	}
	if workspaceID, exportID, download, ok := notionAdminSpaceExportIDs(event.Request.URL.Path); ok {
		if exportID == "" && event.Request.Method == http.MethodPost {
			return serveNotionAdminSpaceExportEnqueue(event, jobs, workspaceID)
		}
		if exportID != "" && !download && event.Request.Method == http.MethodGet {
			return serveNotionAdminSpaceExportStatus(event, workspaceID, exportID)
		}
		if exportID != "" && download && event.Request.Method == http.MethodGet {
			return serveNotionAdminSpaceExportDownload(event, workspaceID, exportID)
		}
	}
	return notionCompatibilityError(
		event,
		http.StatusBadRequest,
		notionErrorInvalidRequestURL,
		notionInvalidRequestURLMessage,
	)
}

func notionAdminSpaceUsersID(path string) (string, bool) {
	value, found := strings.CutPrefix(path, notionAdminAPISpacesPath+"/")
	parts := strings.Split(value, "/")
	if !found || len(parts) != 2 || parts[0] == "" || parts[1] != "users" {
		return "", false
	}
	return parts[0], true
}

func notionAdminSpaceExportIDs(path string) (string, string, bool, bool) {
	value, found := strings.CutPrefix(path, notionAdminAPISpacesPath+"/")
	parts := strings.Split(value, "/")
	if !found || len(parts) < 2 || len(parts) > 4 || parts[0] == "" || parts[1] != "exports" {
		return "", "", false, false
	}
	if len(parts) == 2 {
		return parts[0], "", false, true
	}
	if parts[2] == "" {
		return "", "", false, false
	}
	if len(parts) == 3 {
		return parts[0], parts[2], false, true
	}
	return parts[0], parts[2], parts[3] == "download", parts[3] == "download"
}
