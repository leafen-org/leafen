package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// The agents connected to a page — the workspace's apps installed as guests,
// each its own account shared with the page with edit access, as a person is,
// and its Notion-compatible connections (page_notion_connections.go) — read
// and changed from the page's "연결" menu. Who may change them is the
// page's "에이전트 사용 권한을 부여할 수 있는 사람" (page_agent_grant_schema.go):
// people with full access, or, when the page allows it, everyone with edit
// access; the app's account is all such a change touches, its other shares
// stay with full access. Each change moves the page's sharing revision on.

func registerPageAppConnectionRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+publicMutationAppConnectionsPath, readPageAppConnections)
	event.Router.POST(apiRoot+publicMutationAppConnectionChangePath, changePageAppConnection).
		Bind(apis.BodyLimit(publicMutationMaxBodyBytes))
	event.Router.POST(apiRoot+publicMutationAgentGrantScopePath, changePageAgentGrantScope).
		Bind(apis.BodyLimit(publicMutationMaxBodyBytes))
}

// authorizePageAppConnections opens a page's connections to those who may
// edit it; manageable says whether they may change them. A database
// template's are opened to who may edit the template, and changed by who may
// change its database page's, as Notion's template "연결" lists the
// connections reaching it from above with "연결 추가하기" (seen 2026-11-16):
// a Notion-compatible connection may take the template into its pages, while
// the workspace's apps, shared as people are, stay off it, as a template
// keeps no shares of its own (its access is its database page's,
// authorizeDatabaseTemplateBody).
func authorizePageAppConnections(app core.App, actorID, pageID string) (pageAuthorization, bool, error) {
	if template, err := isDatabaseTemplatePageID(app, pageID); err != nil {
		return pageAuthorization{}, false, err
	} else if template {
		authorization, err := authorizeDatabaseTemplateBody(app, actorID, pageID, pageAccessRoleEdit)
		if isPermissionDenied(err) {
			return pageAuthorization{}, false, errPublicMutationNotFound
		}
		if err != nil {
			return pageAuthorization{}, false, err
		}
		containerID := ""
		if err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_database_templates}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": pageID}).Row(&containerID); err != nil {
			return pageAuthorization{}, false, err
		}
		_, manageable, err := authorizePageAppConnections(app, actorID, containerID)
		if errors.Is(err, errPublicMutationNotFound) {
			return authorization, false, nil
		}
		return authorization, manageable, err
	}
	manager, err := authorizePublicMutationManager(app, actorID, pageID)
	if err == nil {
		return manager, true, nil
	}
	if !errors.Is(err, errPublicMutationNotFound) {
		return pageAuthorization{}, false, err
	}
	editor, err := authorizePageAccessRole(app, actorID, pageID, pageAccessRoleEdit)
	if err != nil {
		if isPermissionDenied(err) {
			return pageAuthorization{}, false, errPublicMutationNotFound
		}
		return pageAuthorization{}, false, err
	}
	stored, err := loadStoredPublicMutationState(app, pageID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return pageAuthorization{}, false, err
	}
	return editor, stored.AgentGrantScope == agentGrantScopeAccess, nil
}

func loadPageAppConnections(app core.App, workspaceID, pageID string) ([]publicMutationAppConnection, error) {
	rows := []struct {
		ID          string `db:"id"`
		PrincipalID string `db:"principal_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[permission]].[[id]],[[permission]].[[principal_id]]
		FROM {{leafen_page_permissions}} [[permission]]
		JOIN {{leafen_integration_accounts}} [[account]] ON [[account]].[[user_id]]=[[permission]].[[principal_id]]
			AND [[account]].[[workspace_id]]=[[permission]].[[workspace_id]] AND [[account]].[[removed_at]]=0
		WHERE [[permission]].[[workspace_id]]={:workspaceId} AND [[permission]].[[page_id]]={:pageId}
			AND [[permission]].[[principal_type]]='user'
		ORDER BY [[permission]].[[created_at]],[[permission]].[[id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "pageId": pageID}).All(&rows)
	connections := make([]publicMutationAppConnection, 0, len(rows))
	for _, row := range rows {
		connections = append(connections, publicMutationAppConnection{ShareID: row.ID, PrincipalID: row.PrincipalID})
	}
	return connections, err
}

// loadConnectableApps are the apps installed in the workspace as guests, the
// ones a page is connected to: an editor who may connect them can't read the
// workspace's app list, so the page's connections name them.
func loadConnectableApps(app core.App, workspaceID string) ([]publicMutationConnectableApp, error) {
	rows := []struct {
		AppID  string `db:"app_id"`
		Name   string `db:"name"`
		UserID string `db:"user_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[account]].[[app_id]],[[user]].[[name]],[[account]].[[user_id]]
		FROM {{leafen_integration_accounts}} [[account]] JOIN {{users}} [[user]] ON [[user]].[[id]]=[[account]].[[user_id]]
		WHERE [[account]].[[workspace_id]]={:workspaceId} AND [[account]].[[removed_at]]=0 AND [[account]].[[role]]={:guest}
		ORDER BY [[account]].[[created]],[[account]].[[user_id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "guest": integrationRoleGuest}).All(&rows)
	apps := make([]publicMutationConnectableApp, 0, len(rows))
	for _, row := range rows {
		apps = append(apps, publicMutationConnectableApp{AppID: row.AppID, Name: row.Name, UserID: row.UserID})
	}
	return apps, err
}

func pageAppConnectionsResponse(app core.App, authorization pageAuthorization, manageable bool) (publicMutationAppConnections, error) {
	stored, err := loadStoredPublicMutationState(app, authorization.Page.Id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return publicMutationAppConnections{}, err
	}
	apps, err := loadConnectableApps(app, authorization.Workspace.WorkspaceID)
	if err != nil {
		return publicMutationAppConnections{}, err
	}
	if isDatabaseTemplatePageRecord(authorization.Page) {
		apps = []publicMutationConnectableApp{}
	}
	connections, err := loadPageAppConnections(app, authorization.Workspace.WorkspaceID, authorization.Page.Id)
	if err != nil {
		return publicMutationAppConnections{}, err
	}
	notion, err := loadPageNotionConnections(app, authorization.Workspace.WorkspaceID, authorization.Page)
	return publicMutationAppConnections{
		Revision: stored.Revision, Manageable: manageable, Apps: apps, Connections: connections, NotionConnections: notion,
	}, err
}

func readPageAppConnections(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	authorization, manageable, err := authorizePageAppConnections(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("pageId")))
	if err != nil {
		return publicMutationRouteError(event, err, "app-connections-authorization")
	}
	response, err := pageAppConnectionsResponse(event.App, authorization, manageable)
	if err != nil {
		return publicMutationRouteError(event, err, "app-connections-read")
	}
	return event.JSON(http.StatusOK, response)
}

func changePageAppConnection(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := publicMutationAppConnectionInput{}
	if err := event.BindBody(&input); err != nil {
		return publicMutationRouteError(event, bodyRefusal(errPublicMutationInvalid, "app connection change", err), "app-connection-input")
	}
	if err := pageAppConnectionInputRefusal(input); err != nil {
		return publicMutationRouteError(event, err, "app-connection-input")
	}
	// A Notion-compatible connection is named by its grant either way.
	notion := input.GrantID != ""
	authorization, _, err := authorizePageAppConnections(event.App, user.Id, pageID)
	if err != nil {
		return publicMutationRouteError(event, err, "app-connection-authorization")
	}
	release := acquireProductOperationLock(publicMutationLockNamespace, authorization.Workspace.WorkspaceID+"\x00"+pageID)
	defer release()
	response := publicMutationAppConnections{}
	change := func(txApp core.App) error {
		current, manageable, err := authorizePageAppConnections(txApp, user.Id, pageID)
		if err != nil {
			return err
		}
		if !manageable {
			return errPublicMutationNotFound
		}
		stored, err := loadStoredPublicMutationState(txApp, pageID)
		if errors.Is(err, sql.ErrNoRows) {
			stored = storedPublicMutationState{WorkspaceID: current.Workspace.WorkspaceID, PageID: pageID}
		} else if err != nil {
			return err
		}
		if stored.Revision != input.ExpectedRevision {
			return errPublicMutationRevisionConflict
		}
		now := time.Now().UTC()
		if notion {
			err = changePageNotionConnection(txApp, current, input.GrantID, input.Action == "connect")
		} else if isDatabaseTemplatePageRecord(current.Page) {
			err = publicMutationRefused("a database template takes no app's share; connect the app on its database's page")
		} else if input.Action == "connect" {
			err = connectPageApp(txApp, current, input.PrincipalID, now)
		} else {
			err = disconnectPageApp(txApp, current, input.ShareID)
		}
		if err != nil {
			return err
		}
		next := stored
		next.Revision, next.UpdatedBy, next.UpdatedAt = stored.Revision+1, user.Id, now.UnixMilli()
		if err := storePublicMutationState(txApp, stored.Revision, next); err != nil {
			return err
		}
		response, err = pageAppConnectionsResponse(txApp, current, true)
		return err
	}
	// A key's call asks its person only for a change that would be made
	// (user_key_acting_ask.go).
	if err := rehearseBeforeAsking(event, change); err != nil {
		return publicMutationRouteError(event, err, "app-connection")
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	err = event.App.RunInTransaction(change)
	if err != nil {
		return publicMutationRouteError(event, err, "app-connection")
	}
	return event.JSON(http.StatusOK, response)
}

// connectPageApp shares the page with an app the workspace has installed as
// a guest, with edit access, under the same policies as a share.
func connectPageApp(app core.App, authorization pageAuthorization, principalID string, now time.Time) error {
	var installed bool
	if err := app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_integration_accounts}}
			WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:userId} AND [[removed_at]]=0 AND [[role]]={:guest})
	`).Bind(dbx.Params{
		"workspaceId": authorization.Workspace.WorkspaceID, "userId": principalID, "guest": integrationRoleGuest,
	}).Row(&installed); err != nil {
		return err
	}
	if !installed {
		return publicMutationRefused("principalId names no app connected to this workspace; its apps list their ids")
	}
	if err := enforcePublicMutationPolicies(app, authorization, publicMutationInput{
		Action: publicMutationActionGrantPermission, PrincipalType: publicMutationPrincipalUser, PrincipalID: principalID,
		Role: publicMutationRoleEdit,
	}); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_permissions}}
			([[id]],[[workspace_id]],[[page_id]],[[principal_type]],[[principal_id]],[[role]],
			 [[created_by]],[[created_at]],[[updated_at]],[[invite_message]],[[notify_on_access]])
		VALUES ({:id},{:workspaceId},{:pageId},'user',{:principalId},'edit',{:actorId},{:now},{:now},'',0)
		ON CONFLICT ([[page_id]],[[principal_type]],[[principal_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"id": securityRandomID(), "workspaceId": authorization.Workspace.WorkspaceID, "pageId": authorization.Page.Id,
		"principalId": principalID, "actorId": authorization.Workspace.PrincipalID, "now": now.UnixMilli(),
	}).Execute()
	return err
}

// disconnectPageApp takes an app's share off the page; a share with anyone
// else is not an app connection and stays.
func disconnectPageApp(app core.App, authorization pageAuthorization, shareID string) error {
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_page_permissions}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId} AND [[principal_type]]='user'
			AND [[principal_id]] IN (SELECT [[user_id]] FROM {{leafen_integration_accounts}}
				WHERE [[workspace_id]]={:workspaceId} AND [[removed_at]]=0)
	`).Bind(dbx.Params{
		"id": shareID, "workspaceId": authorization.Workspace.WorkspaceID, "pageId": authorization.Page.Id,
	}).Execute()
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return publicMutationRefused("shareId names no app's share of this page")
	}
	return nil
}

// pageAppConnectionInputRefusal is why a page's app connection change can't
// be made as sent, or nil. A Notion-compatible connection is named by its
// grant either way.
func pageAppConnectionInputRefusal(input publicMutationAppConnectionInput) error {
	notion := input.GrantID != ""
	switch {
	case input.ExpectedRevision < 0:
		return publicMutationRefused("expectedRevision is the sharing settings' revision as GET reads it, 0 or more")
	case input.Action != "connect" && input.Action != "disconnect":
		return publicMutationRefused("action is connect or disconnect, not %q", input.Action)
	case len(input.GrantID) > 255:
		return publicMutationRefused("grantId is %d bytes, over 255", len(input.GrantID))
	case !notion && input.Action == "connect" && input.PrincipalID == "":
		return publicMutationRefused("principalId is empty: it names the app to connect")
	case !notion && input.Action == "connect" && len(input.PrincipalID) > 255:
		return publicMutationRefused("principalId is %d bytes, over 255", len(input.PrincipalID))
	case !notion && input.Action == "disconnect" && input.ShareID == "":
		return publicMutationRefused("shareId is empty: it names the app's share to take off")
	case !notion && input.Action == "disconnect" && len(input.ShareID) > 255:
		return publicMutationRefused("shareId is %d bytes, over 255", len(input.ShareID))
	}
	return nil
}

// changePageAgentGrantScope sets who may connect agents to the page; only
// people with full access change it.
func changePageAgentGrantScope(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := publicMutationAgentGrantScopeInput{}
	if err := event.BindBody(&input); err != nil {
		return publicMutationRouteError(event, bodyRefusal(errPublicMutationInvalid, "agent connection scope", err), "agent-grant-scope-input")
	}
	switch {
	case input.ExpectedRevision < 0:
		return publicMutationRouteError(event, publicMutationRefused(
			"expectedRevision is the sharing settings' revision as GET reads it, 0 or more"), "agent-grant-scope-input")
	case input.Scope != agentGrantScopeFullAccess && input.Scope != agentGrantScopeAccess:
		return publicMutationRouteError(event, publicMutationRefused("scope is %s or %s, not %q",
			agentGrantScopeFullAccess, agentGrantScopeAccess, input.Scope), "agent-grant-scope-input")
	}
	authorization, err := authorizePublicMutationManager(event.App, user.Id, pageID)
	if err != nil {
		return publicMutationRouteError(event, err, "agent-grant-scope-authorization")
	}
	release := acquireProductOperationLock(publicMutationLockNamespace, authorization.Workspace.WorkspaceID+"\x00"+pageID)
	defer release()
	response := publicMutationStateResponse{}
	change := func(txApp core.App) error {
		current, err := authorizePublicMutationManager(txApp, user.Id, pageID)
		if err != nil {
			return err
		}
		stored, err := loadStoredPublicMutationState(txApp, pageID)
		if errors.Is(err, sql.ErrNoRows) {
			stored = storedPublicMutationState{WorkspaceID: current.Workspace.WorkspaceID, PageID: pageID}
		} else if err != nil {
			return err
		}
		if stored.Revision != input.ExpectedRevision {
			return errPublicMutationRevisionConflict
		}
		next := stored
		next.Revision, next.UpdatedBy, next.UpdatedAt = stored.Revision+1, user.Id, time.Now().UTC().UnixMilli()
		if err := storePublicMutationState(txApp, stored.Revision, next); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_public_mutation_states}} SET [[agent_grant_scope]]={:scope} WHERE [[page_id]]={:pageId}
		`).Bind(dbx.Params{"scope": input.Scope, "pageId": pageID}).Execute(); err != nil {
			return err
		}
		response.State, err = readPublicMutationState(event.Request.Context(), txApp, current, "", "", true, true)
		return err
	}
	// A key's call asks its person only for a change that would be made
	// (user_key_acting_ask.go).
	if err := rehearseBeforeAsking(event, change); err != nil {
		return publicMutationRouteError(event, err, "agent-grant-scope")
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	err = event.App.RunInTransaction(change)
	if err != nil {
		return publicMutationRouteError(event, err, "agent-grant-scope")
	}
	return event.JSON(http.StatusOK, response)
}
