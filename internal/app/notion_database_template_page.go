package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database template answers the Notion API as a page of its data source,
// as Notion's templates do ("Templates are also Notion pages"): the template
// list gives the ID its page answers with, a page read of that ID gives the
// template's values as the page's properties, and the block endpoints read
// and write its body as a database page's body.

// notionTemplatePagePublicID is the public ID a template's page answers with.
func notionTemplatePagePublicID(app core.App, workspaceID, templateID string) (string, error) {
	return ensureNotionObjectIdentity(app, notionObjectIdentityRowPage, workspaceID, templateID)
}

// resolveNotionTemplatePageID names the template whose page a public ID in
// the workspace answers for.
func resolveNotionTemplatePageID(app core.App, workspaceID, publicID string) (string, bool, error) {
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityRowPage, strings.TrimSpace(publicID))
	if errors.Is(err, errInvalidNotionObjectIdentity) || err == nil && identity.Workspace != workspaceID {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	template, err := isDatabaseTemplatePageID(app, identity.InternalID)
	return identity.InternalID, template && err == nil, err
}

// serveNotionDatabaseTemplatePageRetrieve answers a page read of a
// template's page, and reports whether the ID named one.
func serveNotionDatabaseTemplatePageRetrieve(event *core.RequestEvent, publicPageID string) (bool, error) {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return true, err
	}
	templateID, template, err := resolveNotionTemplatePageID(event.App, workspace.Identity.WorkspaceID, publicPageID)
	if err != nil {
		return true, notionRequestNotCompleted(event, err)
	}
	if !template {
		return false, nil
	}
	response, err := notionDatabaseTemplatePageObject(event.App, event.Request, workspace.Identity, templateID)
	if isPermissionDenied(err) || errors.Is(err, sql.ErrNoRows) {
		return true, notionPageNotFound(event)
	}
	if err != nil {
		return true, notionRequestNotCompleted(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return true, event.JSON(http.StatusOK, response)
}

// notionDatabaseTemplatePageObject is a template's page as a page of its
// data source: its title and values as the page's properties. Formulas and
// rollups are not worked out on a template.
func notionDatabaseTemplatePageObject(
	app core.App, request *http.Request, identity notionOAuthBearerIdentity, templateID string,
) (map[string]any, error) {
	page, err := authorizeDatabaseTemplateBody(app, identity.PrincipalID, templateID, pageAccessRoleView)
	if err != nil {
		return nil, err
	}
	if page.Workspace.WorkspaceID != identity.WorkspaceID || !notionOAuthIdentityAllowsPage(app, identity, page) {
		return nil, permissionDenied()
	}
	template := struct {
		DataSourceID string `db:"data_source_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_templates}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": templateID}).One(&template); err != nil {
		return nil, err
	}
	container, err := loadNotionDataSourceParentAuthority(app, identity.WorkspaceID, template.DataSourceID)
	if err != nil {
		return nil, err
	}
	publicID, err := notionTemplatePagePublicID(app, identity.WorkspaceID, templateID)
	if err != nil {
		return nil, err
	}
	databaseID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDatabase, identity.WorkspaceID, container.ContainerID)
	if err != nil {
		return nil, err
	}
	dataSourceID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDataSource, identity.WorkspaceID, container.DataSource)
	if err != nil {
		return nil, err
	}
	stored := []storedNotionDatabaseRowPageProperty{}
	if err := app.DB().NewQuery(`
		SELECT [[property]].[[id]], [[property]].[[type]], [[property]].[[name]], [[property]].[[config]]
		FROM {{leafen_database_properties}} AS [[property]]
		WHERE [[property]].[[owner]] = {:ownerId} AND [[property]].[[workspace_id]] = {:workspaceId}
			AND [[property]].[[page_id]] = {:pageId} AND [[property]].[[data_source_id]] = {:dataSourceId}
			AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
		ORDER BY [[property]].[[position]], [[property]].[[id]]
	`).Bind(dbx.Params{
		"ownerId": container.OwnerID, "workspaceId": identity.WorkspaceID,
		"pageId": container.PageID, "dataSourceId": container.DataSource,
	}).All(&stored); err != nil {
		return nil, err
	}
	if err := withNotionPublicPropertyIDs(app, container.DataSource, stored); err != nil {
		return nil, err
	}
	if err := withDatabaseTemplateValues(app, templateID, stored); err != nil {
		return nil, err
	}
	record := page.Page
	authorization := notionDatabaseRowPageAuthorization{
		Identity: identity, Container: container, Page: page,
		Authority: databaseRowPageAuthority{
			RowID: templateID, OwnerID: container.OwnerID, WorkspaceID: identity.WorkspaceID,
			ContainerPageID: container.PageID, DataSourceID: container.DataSource,
			Icon: record.GetString("icon"), IconType: record.GetString("icon_type"),
			Cover: record.GetString("cover"), CoverPosition: record.GetInt("cover_position"),
			CreatedBy: record.GetString("created_by"), LastEditedBy: record.GetString("last_edited_by"),
			CreatedAt: record.GetDateTime("created").Time().UnixMilli(),
			UpdatedAt: record.GetDateTime("updated").Time().UnixMilli(),
		},
	}
	fileIssuer := newNotionBlockFileCapabilityIssuer(request, identity)
	projected := make([]notionDatabaseRowPageProjectedProperty, 0, len(stored))
	for _, property := range stored {
		if !notionDatabaseRowPagePropertyVisible(property.Type) {
			continue
		}
		result, err := projectNotionDatabaseRowPageProperty(app, authorization, property, nil, true, fileIssuer)
		if err != nil && property.Value.Valid {
			// A value the property no longer takes reads as an empty one.
			property.Value, property.ValueType = sql.NullString{}, sql.NullString{}
			result, err = projectNotionDatabaseRowPageProperty(app, authorization, property, nil, true, fileIssuer)
		}
		if err != nil {
			return nil, err
		}
		projected = append(projected, result)
	}
	origin, err := outwardServerOrigin(request)
	if err != nil {
		return nil, err
	}
	return notionDatabaseRowPageObject(app, origin, authorization, notionDatabaseRowPagePublicIdentities{
		PageID: publicID, DatabaseID: databaseID, DataSourceID: dataSourceID,
	}, projected, fileIssuer)
}

// withDatabaseTemplateValues gives a data source's properties a template's
// values, as a page of the data source holds them: its title the template's
// title, the rest those the template keeps, a text with its formatting and
// template mentions.
func withDatabaseTemplateValues(app core.App, templateID string, stored []storedNotionDatabaseRowPageProperty) error {
	template := struct {
		Title      string `db:"title"`
		Properties string `db:"properties_json"`
		Spans      string `db:"property_spans_json"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[title]], [[properties_json]], [[property_spans_json]] FROM {{leafen_database_templates}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": templateID}).One(&template); err != nil {
		return err
	}
	values := map[string]json.RawMessage{}
	_ = json.Unmarshal([]byte(template.Properties), &values)
	spans := map[string]json.RawMessage{}
	_ = json.Unmarshal([]byte(template.Spans), &spans)
	title, err := json.Marshal(template.Title)
	if err != nil {
		return err
	}
	for index := range stored {
		value, present := values[stored[index].ID]
		if stored[index].Type == graphValueTypeTitle {
			value, present = title, true
		}
		if present && databaseToolbarTemplateWritableProperty(stored[index].Type) {
			stored[index].Value = sql.NullString{String: string(value), Valid: true}
			stored[index].ValueType = sql.NullString{String: stored[index].Type, Valid: true}
			if formatted, ok := spans[stored[index].ID]; ok && stored[index].Type == graphValueTypeRichText {
				stored[index].Spans = sql.NullString{String: string(formatted), Valid: true}
			}
		}
	}
	return nil
}

// serveNotionDatabaseTemplatePageUpdate answers a page update of a
// template's page — its properties, icon and cover, as Notion's API updates a
// template page — and reports whether the ID named one. Its trash, archive,
// lock and template stay its database's to change.
func serveNotionDatabaseTemplatePageUpdate(event *core.RequestEvent, ygo *pocketBaseYgoPersistence, publicPageID string) (bool, error) {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:write", permissionWorkspaceWrite)
	if err != nil || !ok {
		return true, err
	}
	templateID, template, err := resolveNotionTemplatePageID(event.App, workspace.Identity.WorkspaceID, publicPageID)
	if err != nil {
		return true, notionRequestNotCompleted(event, err)
	}
	if !template {
		return false, nil
	}
	input := notionPageUpdateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return true, notionBindRefused(event, err)
	}
	if len(input.Template) > 0 && string(input.Template) != "null" || input.Erase != nil || input.InTrash != nil ||
		input.IsArchived != nil || input.IsLocked != nil {
		return true, notionPageValidationError(event,
			"A database template's page takes properties, icon and cover; its database's templates menu trashes, archives and locks it.")
	}
	presentation, err := notionPagePresentation(input.Icon, input.Cover)
	if err != nil {
		return true, notionPageValidationError(event, err.Error())
	}
	write := databaseTemplatePageWrite{Presentation: presentation}
	if input.Properties.count() > 0 {
		write.Properties = func(txApp core.App, current notionDatabaseRowPageAuthorization) (notionRequestProperties, error) {
			return notionRowRequestValues(txApp, current, input.Properties)
		}
	}
	err = writeDatabaseTemplatePage(event.Request.Context(), event.App, ygo, workspace.Identity, templateID, write)
	if isDatabaseTemplateWriteRefusal(err) {
		return true, notionCompatibilityError(event, http.StatusConflict, "conflict_error", "The template changed. Try again.")
	}
	if err != nil {
		return true, notionDatabaseRowPageMutationError(event, err)
	}
	response, err := notionDatabaseTemplatePageObject(event.App, event.Request, workspace.Identity, templateID)
	if err != nil {
		return true, notionRequestNotCompleted(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return true, event.JSON(http.StatusOK, response)
}
