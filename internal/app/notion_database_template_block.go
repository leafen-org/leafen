package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// serveNotionDatabaseTemplateBlockRetrieve answers a block read of a
// template's page as a database page's (serveNotionRowPageBlockRetrieve): a
// child page whose parent is its data source, as Notion's API reads a
// template's page (seen 2026-11-16: 200, child_page, has_children with its
// body). It reports whether the ID named a template's page.
func serveNotionDatabaseTemplateBlockRetrieve(event *core.RequestEvent, publicID string) (bool, error) {
	named, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityRowPage, strings.TrimSpace(publicID))
	if err != nil {
		return false, nil
	}
	if template, err := isDatabaseTemplatePageID(event.App, named.InternalID); err != nil || !template {
		return err != nil, err
	}
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return true, err
	}
	if named.Workspace != workspace.Identity.WorkspaceID {
		return true, notionBlockNotFound(event)
	}
	response, err := notionDatabaseTemplateBlockObject(event.App, workspace.Identity, named.InternalID)
	if isPermissionDenied(err) || errors.Is(err, sql.ErrNoRows) {
		return true, notionBlockNotFound(event)
	}
	if err != nil {
		return true, notionRequestNotCompleted(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return true, event.JSON(http.StatusOK, response)
}

// notionDatabaseTemplateBlockObject is a template's page as a block, for one
// who may read its database, as its page read admits them
// (notionDatabaseTemplatePageObject).
func notionDatabaseTemplateBlockObject(
	app core.App, identity notionOAuthBearerIdentity, templateID string,
) (map[string]any, error) {
	page, err := authorizeDatabaseTemplateBody(app, identity.PrincipalID, templateID, pageAccessRoleView)
	if err != nil {
		return nil, err
	}
	if page.Workspace.WorkspaceID != identity.WorkspaceID || !notionOAuthIdentityAllowsPage(app, identity, page) {
		return nil, permissionDenied()
	}
	var dataSourceID string
	if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_templates}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": templateID}).Row(&dataSourceID); err != nil {
		return nil, err
	}
	container, err := loadNotionDataSourceParentAuthority(app, identity.WorkspaceID, dataSourceID)
	if err != nil {
		return nil, err
	}
	databaseID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDatabase, identity.WorkspaceID, container.ContainerID)
	if err != nil {
		return nil, err
	}
	publicSourceID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDataSource, identity.WorkspaceID, container.DataSource)
	if err != nil {
		return nil, err
	}
	hasChildren, err := loadNotionPageHasChildren(app, []string{templateID})
	if err != nil {
		return nil, err
	}
	return notionChildPageBlockObject(app, page.Page, map[string]any{
		"type": "data_source_id", "data_source_id": publicSourceID, "database_id": databaseID,
	}, hasChildren[templateID])
}
