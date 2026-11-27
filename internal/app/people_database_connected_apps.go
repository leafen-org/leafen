package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An app connected to People's page from its "⋯ › 연결" (page_app_connections.go)
// reads People and its people's pages as a member does, as a connection added
// to Notion's People page reaches it (Notion's People "⋯" offers "연결 ›" as
// any page's, seen 2026-11-16 in Chrome at 1440). The app's account is a guest
// of the workspace, which People's own rule otherwise lets read none of it
// (authorizePeopleDatabasePage); a person's page unlinked from People's rule
// is closed to it as to a member. A Notion-compatible connection acts as its
// person (notion_page_authorization.go), whom People's rule decides already.

// authorizePeopleConnectedApp decides a page of People's database for a
// principal the workspace doesn't let read: handled is false unless they are
// an app's account People's page is connected to.
func authorizePeopleConnectedApp(app core.App, principalID string, page *core.Record, row bool, minimum pageAccessRole) (pageAuthorization, bool, error) {
	database, _, found, err := peopleDatabasePageOf(app, page)
	if err != nil || !found {
		return pageAuthorization{}, false, err
	}
	var connected bool
	if err := app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_page_permissions}} [[permission]]
			JOIN {{leafen_integration_accounts}} [[account]] ON [[account]].[[user_id]]=[[permission]].[[principal_id]]
				AND [[account]].[[workspace_id]]=[[permission]].[[workspace_id]] AND [[account]].[[removed_at]]=0
			WHERE [[permission]].[[workspace_id]]={:workspaceId} AND [[permission]].[[page_id]]={:pageId}
				AND [[permission]].[[principal_type]]='user' AND [[permission]].[[principal_id]]={:principalId})
	`).Bind(dbx.Params{
		"workspaceId": page.GetString("workspace"), "pageId": database.PageID, "principalId": principalID,
	}).Row(&connected); err != nil || !connected {
		return pageAuthorization{}, false, err
	}
	workspace, err := findRecordRemembered(app, workspacesCollection, page.GetString("workspace"))
	if errors.Is(err, sql.ErrNoRows) {
		return pageAuthorization{}, true, permissionDenied()
	}
	if err != nil {
		return pageAuthorization{}, true, err
	}
	authorization, err := authorizePageGrantWorkspace(app, principalID, workspace)
	if err != nil {
		return pageAuthorization{}, true, err
	}
	if row {
		lowered, err := pageShareUnlinked(app, page.Id)
		if err != nil {
			return pageAuthorization{}, true, err
		}
		if lowered {
			return pageAuthorization{}, true, permissionDenied()
		}
	}
	if minimum > pageAccessRoleView {
		return pageAuthorization{}, true, permissionDenied()
	}
	return pageAuthorization{Workspace: authorization, Page: page, AccessRole: pageAccessRoleView}, true, nil
}
