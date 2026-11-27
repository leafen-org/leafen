package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The developer bar and the page menu's "개발자" that the Developer item's
// "개발자 기능 활성화" shows, as Notion's (seen 2026-10-02), name the person,
// the open workspace and the open page by the IDs the Notion-compatible API
// knows them by, which the server makes from their own IDs
// (notion_*_identity.go). A workspace or page the person can't open is left
// out, as if it weren't there.

const (
	accountDeveloperIDsPath            = "/account/developer-ids"
	errorCodeAccountDeveloperIDsFailed = "LEAFEN_ACCOUNT_DEVELOPER_IDS_FAILED"
)

type accountDeveloperIDs struct {
	User      string `json:"user"`
	Workspace string `json:"workspace,omitempty"`
	Page      string `json:"page,omitempty"`
}

func registerAccountDeveloperIDRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+accountDeveloperIDsPath, readAccountDeveloperIDs)
}

func readAccountDeveloperIDs(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	failed := func(err error) error {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAccountDeveloperIDsFailed,
			"Couldn't read the developer IDs.", "account_developer_ids.read", "identity",
			errorCodeAccountDeveloperIDsFailed, err,
		)
	}
	query := event.Request.URL.Query()
	ids := accountDeveloperIDs{}
	var err error
	if ids.User, err = notionCompatibleUserID(user.Id); err != nil {
		return failed(err)
	}
	if workspaceID := strings.TrimSpace(query.Get("workspaceId")); workspaceID != "" {
		_, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
		if err == nil {
			ids.Workspace, err = notionCompatibleWorkspaceID(workspaceID)
		}
		if err != nil && !isPermissionDenied(err) {
			return failed(err)
		}
	}
	if pageID := strings.TrimSpace(query.Get("pageId")); pageID != "" {
		_, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead)
		if err == nil {
			ids.Page, err = notionPagePublicID(event.App, pageID)
		}
		if err != nil && !isPermissionDenied(err) {
			return failed(err)
		}
	}
	return event.JSON(http.StatusOK, ids)
}
