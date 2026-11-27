package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const notionAPIDataSourcesPath = notionAPIRoot + "/data_sources"

func serveNotionDataSourceCompatibility(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
) (bool, error) {
	if event.Request.URL.Path == notionAPIDataSourcesPath {
		if event.Request.Method != http.MethodPost {
			return false, nil
		}
		return true, serveNotionDataSourceCreate(event, ygo)
	}
	if !strings.HasPrefix(event.Request.URL.Path, notionAPIDataSourcesPath+"/") {
		return false, nil
	}
	remainder := strings.TrimPrefix(event.Request.URL.Path, notionAPIDataSourcesPath+"/")
	if publicID, templates := strings.CutSuffix(remainder, "/templates"); templates && publicID != "" && !strings.Contains(publicID, "/") {
		if event.Request.Method != http.MethodGet {
			return false, nil
		}
		return true, serveNotionDataSourceTemplates(event, publicID)
	}
	if publicID, query := strings.CutSuffix(remainder, "/query"); query && publicID != "" && !strings.Contains(publicID, "/") {
		if event.Request.Method != http.MethodPost {
			return false, nil
		}
		return true, serveNotionDataSourceQuery(event, publicID)
	}
	if remainder == "" || strings.Contains(remainder, "/") {
		return false, nil
	}
	switch event.Request.Method {
	case http.MethodGet:
		return true, serveNotionDataSourceRetrieve(event, remainder)
	case http.MethodPatch:
		return true, serveNotionDataSourceUpdate(event, ygo, remainder)
	default:
		return false, nil
	}
}

func serveNotionDataSourceRetrieve(event *core.RequestEvent, publicID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return err
	}
	identity, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDataSource, publicID)
	if errors.Is(err, errInvalidNotionObjectIdentity) ||
		err == nil && identity.Workspace != workspace.Identity.WorkspaceID {
		return notionPageNotFound(event)
	}
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	response, err := loadNotionDataSourceResponse(
		event.App, event.Request, workspace.Identity, identity.InternalID, permissionWorkspaceRead,
	)
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func loadNotionDataSourceResponse(
	app core.App,
	request *http.Request,
	identity notionOAuthBearerIdentity,
	dataSourceID string,
	requirement permissionRequirement,
) (map[string]any, error) {
	var response map[string]any
	fileIssuer := newNotionBlockFileCapabilityIssuer(request, identity)
	err := runNotionReadSnapshot(request.Context(), app, func(txApp core.App) error {
		current, state, err := authorizeNotionDataSourceAnyState(txApp, identity, dataSourceID, requirement)
		if err != nil {
			return err
		}
		origin, err := notionDataSourceTrashOrigin(txApp, state)
		if err != nil {
			return err
		}
		if origin != "" {
			current.Container.ContainerID = origin
		}
		response, err = notionDataSourceObject(txApp, current, fileIssuer, request)
		if err != nil {
			return err
		}
		// In the trash with its database, or under a page in it.
		place, err := notionDataSourceTrashPlace(txApp, state.PageID, state.trashed())
		response["in_trash"] = place.inTrash()
		return err
	})
	return response, err
}

func notionDataSourceObject(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	fileIssuer *notionBlockFileCapabilityIssuer,
	request *http.Request,
) (map[string]any, error) {
	container := authorization.Container
	publicDataSourceID, err := ensureNotionObjectIdentity(
		app, notionObjectIdentityDataSource, container.WorkspaceID, container.DataSource,
	)
	if err != nil {
		return nil, err
	}
	publicDatabaseID, err := ensureNotionObjectIdentity(
		app, notionObjectIdentityDatabase, container.WorkspaceID, container.ContainerID,
	)
	if err != nil {
		return nil, err
	}
	// The data source's database sits where its database does.
	databaseContainer, err := loadNotionDatabaseContainerRecord(app, container.WorkspaceID, container.ContainerID)
	if err != nil {
		return nil, err
	}
	databasePage, err := app.FindRecordById(pagesCollection, databaseContainer.PageID)
	if err != nil {
		return nil, err
	}
	databaseParent, err := notionDatabaseParent(app, databaseContainer, databasePage)
	if err != nil {
		return nil, err
	}
	properties, err := loadNotionDataSourceProperties(app, authorization)
	if err != nil {
		return nil, err
	}
	createdTime, err := notionDatabaseRowPageTime(container.CreatedAt)
	if err != nil {
		return nil, err
	}
	updatedTime, err := notionDatabaseRowPageTime(container.UpdatedAt)
	if err != nil {
		return nil, err
	}
	presentation := struct {
		Icon        string `db:"icon"`
		IconType    string `db:"icon_type"`
		Description string `db:"description"`
		Spans       string `db:"description_spans"`
		TitleSpans  string `db:"title_spans"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[icon]], [[icon_type]], [[description]], [[description_spans]], [[title_spans]] FROM {{leafen_data_sources}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId}
	`).Bind(dbx.Params{"id": container.DataSource, "workspaceId": container.WorkspaceID}).One(&presentation); err != nil {
		return nil, err
	}
	icon, err := notionPageIconWithManagedFile(
		app, fileIssuer, container.WorkspaceID, presentation.IconType, presentation.Icon,
	)
	if err != nil {
		return nil, err
	}
	// Its title and description read with their formatting and mentions, as
	// Notion's, for their reader.
	reader := notionBlockAuthorization{Identity: authorization.Identity, Page: authorization.Page}
	title, err := notionFormattedTitle(app, reader, fileIssuer, container.Title, decodeStoredTextSpans(presentation.TitleSpans))
	if err != nil {
		return nil, err
	}
	description, err := notionFormattedTitle(
		app, reader, fileIssuer, presentation.Description, decodeStoredTextSpans(presentation.Spans),
	)
	if err != nil {
		return nil, err
	}
	// Where it stands is its database's, and who made it: its page's, as a
	// database's own object says (notion_database_api.go).
	origin, err := outwardServerOrigin(request)
	if err != nil {
		return nil, err
	}
	publicURL, err := notionPagePublicURL(app, origin, container.WorkspaceID, databaseContainer.PageID)
	if err != nil {
		return nil, err
	}
	createdBy, lastEditedBy, err := connectionAuthorIDs(
		app, connectionAuthoredDatabase, databaseContainer.ID, notionBlockEditMark(databaseContainer.Updated),
		databasePage.GetString("created_by"), databasePage.GetString("last_edited_by"),
	)
	if err != nil {
		return nil, err
	}
	var cover any
	if databaseContainer.fullPage() {
		if cover, err = notionPageCoverWithManagedFile(app, fileIssuer, container.WorkspaceID, databasePage.GetString("cover")); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"object":           "data_source",
		"id":               publicDataSourceID,
		"properties":       notionPropertyObjects(properties),
		"parent":           map[string]any{"type": "database_id", "database_id": publicDatabaseID},
		"database_parent":  databaseParent,
		"database_type":    notionStoredDatabaseType(app, container.DataSource),
		"created_time":     createdTime,
		"last_edited_time": updatedTime,
		"created_by":       map[string]any{"object": "user", "id": createdBy},
		"last_edited_by":   map[string]any{"object": "user", "id": lastEditedBy},
		"title":            title,
		"description":      description,
		"icon":             icon,
		"cover":            cover,
		"in_trash":         false,
		"is_inline":        !databaseContainer.fullPage(),
		"url":              notionDatabasePublicURL(origin, publicDatabaseID),
		"public_url":       publicURL,
	}, nil
}

func notionDataSourceReadError(event *core.RequestEvent, err error) error {
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return notionPageNotFound(event)
	}
	var propertyError *notionDatabaseRowPagePropertyTypeError
	if errors.As(err, &propertyError) || errors.Is(err, errNotionDatabaseRowPageProjectionInvalid) {
		return notionPageValidationError(event, "The data source has a property this API version can't show.")
	}
	return notionRequestNotCompleted(event, err)
}
