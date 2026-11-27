package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAPIViewsPath    = "/v1/views"
	notionUnnamedViewName = "Untitled"
)

// notionViewLocation is a saved view with the database that shows it.
type notionViewLocation struct {
	ID            string `db:"id"`
	OwnerID       string `db:"owner"`
	WorkspaceID   string `db:"workspace_id"`
	PageID        string `db:"page_id"`
	DataSourceID  string `db:"data_source_id"`
	OwnContainer  string `db:"own_container_id"`
	ContainerID   string `db:"container_id"`
	ContainerKind string `db:"container_kind"`
	SourcePageID  string `db:"source_page_id"`
	Type          string `db:"type"`
	Name          string `db:"name"`
	Position      string `db:"position"`
	TabPosition   string `db:"tab_position"`
	Config        string `db:"config"`
	Created       int64  `db:"created"`
	Updated       int64  `db:"updated"`
	CreatedBy     string `db:"created_by"`
	LastEditedBy  string `db:"last_edited_by"`
}

// notionViewLocationSelect reads the views the API knows: every tab but a
// feed, which the official API has no type for; notionViewTabSelect reads
// every tab, as a database keeps them.
const notionViewLocationSelect = notionViewTabSelect + ` AND [[view]].[[type]] != 'feed'`

const notionViewTabSelect = `
	SELECT [[view]].[[id]], [[view]].[[owner]], [[view]].[[workspace_id]], [[view]].[[page_id]],
		[[view]].[[data_source_id]], [[view]].[[container_id]] AS [[own_container_id]],
		[[membership]].[[container_id]], [[container]].[[kind]] AS [[container_kind]],
		[[source]].[[page_id]] AS [[source_page_id]], [[view]].[[type]], [[view]].[[name]],
		[[view]].[[position]], [[membership]].[[position]] AS [[tab_position]], [[view]].[[config]],
		[[view]].[[created]], [[view]].[[updated]], [[view]].[[created_by]], [[view]].[[last_edited_by]]
	FROM {{leafen_database_views}} AS [[view]]
	JOIN {{leafen_database_container_views}} AS [[membership]] ON [[membership]].[[view_id]] = [[view]].[[id]]
	JOIN {{leafen_database_containers}} AS [[container]] ON [[container]].[[id]] = [[membership]].[[container_id]]
	JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[view]].[[data_source_id]]
	WHERE [[view]].[[workspace_id]] = {:workspaceId}
		AND NOT EXISTS (
			SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
			WHERE [[trash]].[[container_id]] = [[container]].[[id]]
		)`

func serveNotionViewCompatibility(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) (bool, error) {
	path := event.Request.URL.Path
	if path == notionAPIViewsPath {
		switch event.Request.Method {
		case http.MethodGet:
			return true, serveNotionViewList(event)
		case http.MethodPost:
			return true, serveNotionViewCreate(event, ygo)
		}
		return false, nil
	}
	viewID, found := strings.CutPrefix(path, notionAPIViewsPath+"/")
	if found && strings.Contains(viewID, "/") {
		return serveNotionViewQueryRoute(event, viewID)
	}
	if !found || viewID == "" {
		return false, nil
	}
	switch event.Request.Method {
	case http.MethodGet:
		return true, serveNotionViewRetrieve(event, viewID)
	case http.MethodPatch:
		return true, serveNotionViewUpdate(event, ygo, viewID)
	case http.MethodDelete:
		return true, serveNotionViewDelete(event, ygo, viewID)
	}
	return false, nil
}

// authorizeNotionViewPages reads (or edits) the database page of a view and
// reads the source page a linked database shows.
func authorizeNotionViewPages(
	app core.App, identity notionOAuthBearerIdentity, pageID, sourcePageID string, requirement permissionRequirement,
) (pageAuthorization, error) {
	page, err := authorizePageBody(app, identity.PrincipalID, pageID, requirement)
	if isPermissionDenied(err) || err == nil && (page.Page.GetBool("in_trash") ||
		page.Workspace.WorkspaceID != identity.WorkspaceID || !notionOAuthIdentityAllowsPage(app, identity, page)) {
		return pageAuthorization{}, errDatabaseRowPageNotFound
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	if sourcePageID != pageID {
		source, err := authorizePageBody(app, identity.PrincipalID, sourcePageID, permissionWorkspaceRead)
		if err != nil || source.Page.GetBool("in_trash") || source.Workspace.WorkspaceID != identity.WorkspaceID ||
			!notionOAuthIdentityAllowsPage(app, identity, source) {
			return pageAuthorization{}, refusedUnlessStorage(errDatabaseRowPageNotFound, err)
		}
	}
	return page, nil
}

func loadNotionViewLocation(app core.App, workspaceID, publicID string) (notionViewLocation, error) {
	// An ID as MCP gives it names the same view, as Notion's one ID does.
	internalID := strings.TrimSpace(publicID)
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityView, publicID)
	if err == nil && identity.Workspace != workspaceID {
		return notionViewLocation{}, errDatabaseRowPageNotFound
	}
	if err != nil && !errors.Is(err, errInvalidNotionObjectIdentity) {
		return notionViewLocation{}, err
	}
	if err == nil {
		internalID = identity.InternalID
	}
	location := notionViewLocation{}
	err = app.DB().NewQuery(notionViewLocationSelect + ` AND [[view]].[[id]] = {:id}`).
		Bind(dbx.Params{"workspaceId": workspaceID, "id": internalID}).One(&location)
	if errors.Is(err, sql.ErrNoRows) {
		return notionViewLocation{}, errDatabaseRowPageNotFound
	}
	return location, err
}

// notionViewScopeProperties loads the properties of a source that a view may
// name, in their order.
func notionViewScopeProperties(app core.App, dataSourceID, pageID string) ([]notionViewProperty, error) {
	rows := []struct {
		ID     string `db:"id"`
		Name   string `db:"name"`
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:dataSourceId} AND [[page_id]] = {:pageId}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"dataSourceId": dataSourceID, "pageId": pageID}).All(&rows); err != nil {
		return nil, err
	}
	properties := make([]notionViewProperty, 0, len(rows))
	expressions := make([]string, 0, len(rows))
	for _, row := range rows {
		nativeType := normalizeNotionDatabaseRowPagePropertyType(row.Type)
		if !notionDatabaseRowPagePropertyVisible(row.Type) {
			continue
		}
		options, groups := notionViewPropertyOptions(nativeType, row.Config)
		rolledType, rolledOptions := notionViewRolledType(app, nativeType, row.Config)
		properties = append(properties, notionViewProperty{
			ID: row.ID, Name: row.Name, NativeType: nativeType,
			OfficialType: notionDatabaseRowPageOfficialPropertyType(nativeType),
			RolledType:   rolledType, RolledOptions: rolledOptions,
			RollupResult: notionViewRollupResult(nativeType, row.Config),
			Options:      options, Groups: groups,
		})
		expressions = append(expressions, notionViewFormulaExpression(nativeType, row.Config))
	}
	notionViewFormulaResults(properties, expressions)
	return properties, nil
}

// notionViewFormulaExpression is a formula's expression, "" for any other.
func notionViewFormulaExpression(nativeType, rawConfig string) string {
	config, err := decodeDatabaseComputedConfig(rawConfig)
	if nativeType != graphValueTypeFormula || err != nil {
		return ""
	}
	if config.Formula != "" {
		return config.Formula
	}
	return config.Expression
}

// notionViewFormulaResults sets each formula's result kind from its
// expression, a property it names read as the kind of filter its values take
// (a formula's by its expression, empty() alone of none: notionViewFormulaKind).
func notionViewFormulaResults(properties []notionViewProperty, expressions []string) {
	visiting := map[int]bool{}
	var result func(index int) string
	kindOf := func(name string) string {
		for index, property := range properties {
			if property.Name != name && property.ID != name {
				continue
			}
			switch property.OfficialType {
			case "formula":
				return result(index)
			case "select", "status":
				return "text"
			case "checkbox", "number", "title", "rich_text", "url", "email", "phone_number",
				"date", "created_time", "last_edited_time":
				return notionViewKindOfFilter(property.OfficialType, "")
			}
			return ""
		}
		return ""
	}
	result = func(index int) string {
		if visiting[index] {
			return ""
		}
		visiting[index] = true
		defer delete(visiting, index)
		return notionViewFormulaKind(expressions[index], kindOf)
	}
	for index := range properties {
		if properties[index].OfficialType == "formula" {
			properties[index].FormulaResult = notionViewFormulaKnownKind(result(index))
		}
	}
}

// notionViewRolledType is the official type of the property a rollup rolls
// up, when it shows the related values themselves or counts them per group,
// with that property's options; "" for any other, and for one over a
// rollup, whose values Leafen can't compare one by one.
func notionViewRolledType(app core.App, nativeType, rawConfig string) (string, []notionViewOption) {
	if nativeType != graphValueTypeRollup {
		return "", nil
	}
	config, err := decodeDatabaseComputedConfig(rawConfig)
	if err != nil || !databaseRollupKeepsLeaves(config.rollupCalculation()) {
		return "", nil
	}
	_, targetID, _ := config.rollupFields()
	target := struct {
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	if targetID == "" || app.DB().NewQuery(`SELECT [[type]], [[config]] FROM {{leafen_database_properties}} WHERE [[id]] = {:id} LIMIT 1`).
		Bind(dbx.Params{"id": targetID}).One(&target) != nil {
		return "", nil
	}
	targetType := normalizeNotionDatabaseRowPagePropertyType(target.Type)
	if targetType == graphValueTypeRollup {
		return "", nil
	}
	options, _ := notionViewPropertyOptions(targetType, target.Config)
	return notionDatabaseRowPageOfficialPropertyType(targetType), options
}

// notionViewFilterScopeFor loads the source properties a view filter names.
func notionViewFilterScopeFor(app core.App, location notionViewLocation) (notionViewFilterScope, error) {
	properties, err := notionViewScopeProperties(app, location.DataSourceID, location.SourcePageID)
	if err != nil {
		return notionViewFilterScope{}, err
	}
	scope := newNotionViewFilterScope(properties).withPageTimestamps()
	scope.refusesUnknownOptions = true
	if scope.ids, err = loadNotionPropertyIDs(app, location.DataSourceID); err != nil {
		return notionViewFilterScope{}, err
	}
	// A person is named with or without the ID's dashes, as Notion's filter
	// takes them; a bot (notion_people_value_bots.go) and an invitee
	// (people_value_invitees.go) by their own IDs, as a people value keeps them.
	scope.userToNative = func(publicID string) (string, bool) {
		publicID, _ = normalizeNotionUUIDRequest(publicID)
		if notionBotUserIDShape(publicID) || peopleValueInviteeIDShape(publicID) {
			return publicID, true
		}
		id, err := resolveNotionCompatibleUserID(publicID)
		return id, err == nil
	}
	scope.userToPublic = func(nativeID string) string {
		if notionBotUserIDShape(nativeID) || peopleValueInviteeIDShape(nativeID) {
			return nativeID
		}
		id, _ := notionCompatibleUserID(nativeID)
		return id
	}
	scope.rowToNative = func(publicID string) (string, bool) {
		identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityRowPage, publicID)
		return identity.InternalID, err == nil && identity.Workspace == location.WorkspaceID
	}
	scope.rowToPublic = func(nativeID string) string {
		id, _ := ensureNotionObjectIdentity(app, notionObjectIdentityRowPage, location.WorkspaceID, nativeID)
		return id
	}
	return scope, nil
}

// notionViewReaderScope is a view's scope as its reader sees it: without the
// properties they see in no row.
func notionViewReaderScope(app core.App, location notionViewLocation, readerID string) (notionViewFilterScope, error) {
	scope, err := notionViewFilterScopeFor(app, location)
	if err != nil {
		return scope, err
	}
	return scope.withoutWithheld(app, readerID, location.DataSourceID)
}

type notionViewConfig struct {
	FilterGroup  *databaseQueryFilterGroup `json:"filterGroup"`
	Sorts        []databaseQuerySort       `json:"sorts"`
	QuickFilters []json.RawMessage         `json:"quickFilters"`
}

func notionViewReference(app core.App, location notionViewLocation, full bool, request *http.Request, readerID string) (map[string]any, error) {
	viewID, err := ensureNotionObjectIdentity(app, notionObjectIdentityView, location.WorkspaceID, location.ID)
	if err != nil {
		return nil, err
	}
	databaseID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDatabase, location.WorkspaceID, location.ContainerID)
	if err != nil {
		return nil, err
	}
	object := map[string]any{
		"object": "view", "id": viewID, "type": location.Type,
		"parent": map[string]any{"type": "database_id", "database_id": databaseID},
	}
	if !full {
		return object, nil
	}
	dataSourceID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDataSource, location.WorkspaceID, location.DataSourceID)
	if err != nil {
		return nil, err
	}
	scope, err := notionViewReaderScope(app, location, readerID)
	if err != nil {
		return nil, err
	}
	config, settings := notionViewConfig{}, map[string]any{}
	if json.Unmarshal([]byte(location.Config), &config) != nil || json.Unmarshal([]byte(location.Config), &settings) != nil {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	origin, err := outwardServerOrigin(request)
	if err != nil {
		return nil, err
	}
	// One no one named, as a view added in the app is, is "Untitled" in
	// Notion's API, and one given "" is "" (database_view_name.go).
	object["name"] = location.Name
	if viewNeverNamed(location.Name, settings) {
		object["name"] = notionUnnamedViewName
	}
	object["data_source_id"] = dataSourceID
	object["created_time"] = time.UnixMilli(location.Created).UTC().Format(time.RFC3339Nano)
	object["last_edited_time"] = time.UnixMilli(location.Updated).UTC().Format(time.RFC3339Nano)
	// Its database's address with the view, both IDs without dashes, as
	// Notion's (seen 2026-11-11: https://app.notion.com/p/<database>?v=<view>);
	// the app opens that view there (page_links.go).
	object["url"] = notionMentionURL(origin, databaseID) + "?v=" + strings.ReplaceAll(viewID, "-", "")
	if object["created_by"], object["last_edited_by"], err = notionViewAuthors(app, location); err != nil {
		return nil, err
	}
	object["filter"] = scope.officialViewFilter(config.FilterGroup)
	object["sorts"] = scope.officialViewSorts(config.Sorts)
	object["quick_filters"] = scope.officialQuickFilters(config.QuickFilters)
	if err := notionViewDashboardFields(app, location, object); err != nil {
		return nil, err
	}
	configuration := scope.officialViewConfiguration(location.Type, settings)
	if location.Type == graphViewTypeForm {
		if err := notionViewFormConfiguration(app, location, configuration); err != nil {
			return nil, err
		}
	}
	if location.Type == graphViewTypeDashboard {
		if configuration["rows"], err = notionViewDashboardRows(app, location); err != nil {
			return nil, err
		}
	}
	object["configuration"] = notionViewGivenConfiguration(location.Type, settings, configuration)
	return object, nil
}

func notionViewReadError(event *core.RequestEvent, err error) error {
	if errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) {
		return notionCompatibilityError(event, http.StatusNotFound, "object_not_found", "There's no view with the requested ID.")
	}
	return notionDataSourceMutationError(event, err)
}

func serveNotionViewRetrieve(event *core.RequestEvent, publicID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return err
	}
	location, err := loadNotionViewLocation(event.App, workspace.Identity.WorkspaceID, publicID)
	if err == nil {
		_, err = authorizeNotionViewPages(event.App, workspace.Identity, location.PageID, location.SourcePageID, permissionWorkspaceRead)
	}
	if err != nil {
		return notionViewReadError(event, err)
	}
	object, err := notionViewReference(event.App, location, true, event.Request, workspace.Identity.PrincipalID)
	if err != nil {
		return notionViewReadError(event, err)
	}
	return writeNotionJSON(event, http.StatusOK, object)
}

// serveNotionViewList pages through the views of one database, or of one data
// source across the databases the caller can read, in tab order (notionViewListScope).
func serveNotionViewList(event *core.RequestEvent) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return err
	}
	query := event.Request.URL.Query()
	databaseID, dataSourceID, given := notionViewListScope(query)
	if !given {
		return notionPageValidationError(event, notionViewListNeedsID)
	}
	pageSize := 100
	if raw := notionQueryDigits(query.Get("page_size")); raw != "" {
		if pageSize, err = strconv.Atoi(raw); err != nil || pageSize < 1 || pageSize > 100 {
			return notionPageValidationError(event, "page_size must be between 1 and 100.")
		}
	}
	predicate, kind, public := ` AND [[membership]].[[container_id]] = {:scope}`, notionObjectIdentityDatabase, databaseID
	noteNotionAskedObject(event, "database", databaseID)
	if dataSourceID != "" {
		predicate, kind, public = ` AND [[view]].[[data_source_id]] = {:scope}`, notionObjectIdentityDataSource, dataSourceID
		noteNotionAskedObject(event, "data_source", dataSourceID)
	}
	identity, err := resolveNotionObjectIdentity(event.App, kind, public)
	if err != nil || identity.Workspace != workspace.Identity.WorkspaceID {
		return notionCompatibilityError(event, http.StatusNotFound, "object_not_found", "There's no database or data source with the requested ID.")
	}
	if dataSourceID != "" {
		if trashed, err := databaseSourceIsTrashed(event.App, identity.InternalID); err != nil || trashed {
			if err != nil {
				return notionViewReadError(event, err)
			}
			return notionTrashedDataSourceNotFound(event, dataSourceID)
		}
	}
	locations := []notionViewLocation{}
	if err := event.App.DB().NewQuery(notionViewLocationSelect + predicate + `
		ORDER BY [[container]].[[id]], [[membership]].[[position]], [[view]].[[id]] LIMIT 1001
	`).Bind(dbx.Params{"workspaceId": workspace.Identity.WorkspaceID, "scope": identity.InternalID}).All(&locations); err != nil {
		return notionViewReadError(event, err)
	}
	results := []any{}
	cursor, started := strings.TrimSpace(query.Get("start_cursor")), strings.TrimSpace(query.Get("start_cursor")) == ""
	nextCursor := any(nil)
	allowed := map[string]bool{}
	for _, location := range locations {
		// A database's views are its tabs: its dashboards' widgets aren't, as
		// Notion's list shows them only under their data source (seen 2026-11-05).
		if databaseID != "" && dashboardWidgetOwner([]byte(location.Config)) != "" {
			continue
		}
		key := location.PageID + "\x00" + location.SourcePageID
		if _, checked := allowed[key]; !checked {
			_, err := authorizeNotionViewPages(event.App, workspace.Identity, location.PageID, location.SourcePageID, permissionWorkspaceRead)
			allowed[key] = err == nil
		}
		if !allowed[key] {
			continue
		}
		reference, err := notionViewReference(event.App, location, false, event.Request, workspace.Identity.PrincipalID)
		if err != nil {
			return notionViewReadError(event, err)
		}
		if !started {
			started = reference["id"] == cursor
			if !started {
				continue
			}
		}
		if len(results) == pageSize {
			nextCursor = reference["id"]
			break
		}
		results = append(results, map[string]any{"object": "view", "id": reference["id"]})
	}
	if !started {
		return notionStartCursorRefused(event, query.Get("start_cursor"))
	}
	return writeNotionJSON(event, http.StatusOK, map[string]any{
		"object": "list", "results": results, "next_cursor": nextCursor, "has_more": nextCursor != nil,
		"type": "view", "view": map[string]any{},
	})
}

func writeNotionJSON(event *core.RequestEvent, status int, body any) error {
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(status, body)
}

// notionViewAuthors are who made a view and who last changed it, as user
// references: a person, a connection's bot (connectionAuthorIDs), or null
// for no one (database_view_authors_schema.go).
func notionViewAuthors(app core.App, location notionViewLocation) (any, any, error) {
	createdBy, lastEditedBy, err := connectionAuthorGrants(
		app, connectionAuthoredView, location.ID, notionBlockEditMark(location.Updated),
	)
	if err != nil {
		return nil, nil, err
	}
	reference := func(grant, person string) (any, error) {
		if grant != "" {
			return map[string]any{"object": "user", "id": notionOAuthBotID(grant)}, nil
		}
		if strings.TrimSpace(person) == "" {
			return nil, nil
		}
		id, err := notionCompatibleUserID(person)
		if err != nil {
			return nil, err
		}
		return map[string]any{"object": "user", "id": id}, nil
	}
	created, err := reference(createdBy, location.CreatedBy)
	if err != nil {
		return nil, nil, err
	}
	edited, err := reference(lastEditedBy, location.LastEditedBy)
	return created, edited, err
}
