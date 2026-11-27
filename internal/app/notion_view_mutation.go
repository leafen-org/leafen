package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

var notionOfficialViewTypes = map[string]struct{}{
	"table": {}, "board": {}, "list": {}, "calendar": {}, "timeline": {},
	"gallery": {}, "form": {}, "chart": {}, "map": {}, "dashboard": {},
}

type notionViewCreateRequest struct {
	DataSourceID   string          `json:"data_source_id"`
	Name           *string         `json:"name"`
	Type           string          `json:"type"`
	DatabaseID     *string         `json:"database_id"`
	ViewID         *string         `json:"view_id"`
	Filter         json.RawMessage `json:"filter"`
	Sorts          json.RawMessage `json:"sorts"`
	QuickFilters   json.RawMessage `json:"quick_filters"`
	CreateDatabase json.RawMessage `json:"create_database"`
	Configuration  json.RawMessage `json:"configuration"`
	Position       json.RawMessage `json:"position"`
	Placement      json.RawMessage `json:"placement"`
}

type notionViewUpdateRequest struct {
	Name          *string         `json:"name"`
	Filter        json.RawMessage `json:"filter"`
	Sorts         json.RawMessage `json:"sorts"`
	QuickFilters  json.RawMessage `json:"quick_filters"`
	Configuration json.RawMessage `json:"configuration"`
}

// Forms live only in the database that holds their source, as in Notion: a
// database that links the source offers no form view, and Notion's API
// refuses one in these words (seen 2026-11-12: a tab or a dashboard widget
// of another database's source, and a new linked database).
const notionLinkedFormMessage = "The request isn't valid: the form question has no form database to belong to"

// notionViewName is the name a request gives a view, as given: "" and
// spaces too, as Notion's API keeps them (database_view_name.go).
func notionViewName(raw *string) (string, error) {
	if raw == nil || !viewNameValid(*raw) {
		return "", notionViewFilterInvalid("the view name is invalid")
	}
	return *raw, nil
}

// notionViewSiblings are the views of one source in one database the API
// knows, in order; notionViewTabSiblings are all its tabs, feeds too.
func notionViewSiblings(app core.App, workspaceID, containerID, dataSourceID string) ([]notionViewLocation, error) {
	return notionViewSiblingsFrom(app, notionViewLocationSelect, workspaceID, containerID, dataSourceID)
}

func notionViewTabSiblings(app core.App, workspaceID, containerID, dataSourceID string) ([]notionViewLocation, error) {
	return notionViewSiblingsFrom(app, notionViewTabSelect, workspaceID, containerID, dataSourceID)
}

func notionViewSiblingsFrom(app core.App, query, workspaceID, containerID, dataSourceID string) ([]notionViewLocation, error) {
	siblings := []notionViewLocation{}
	err := app.DB().NewQuery(query + `
		AND [[membership]].[[container_id]] = {:containerId} AND [[view]].[[data_source_id]] = {:dataSourceId}
		ORDER BY [[membership]].[[position]], [[view]].[[id]]
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "containerId": containerID, "dataSourceId": dataSourceID,
	}).All(&siblings)
	return siblings, err
}

func notionViewOperation(location notionViewLocation, name, position string, config json.RawMessage) graphOperation {
	return graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
		ID: location.ID, DataSourceID: location.DataSourceID, ContainerID: location.OwnContainer,
		Type: location.Type, Name: name, Position: position, Config: config,
	}}
}

func applyNotionViewMutation(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	pageID, sourcePageID, kind string, operations []graphOperation,
) error {
	return applyNotionViewMutationThen(event, ygo, identity, pageID, sourcePageID, kind, operations, nil)
}

// applyNotionViewMutationThen also runs then, a write outside the graph (a form's link), in its transaction.
func applyNotionViewMutationThen(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	pageID, sourcePageID, kind string, operations []graphOperation,
	then func(core.App, pageAuthorization) error,
) error {
	return applyNotionViewMutationLinking(event, ygo, identity, pageID, sourcePageID, kind, operations, then, nil)
}

// applyNotionViewMutationLinking also adds, when link is given, what it adds
// from the page as it stands (notionViewLinkInput).
func applyNotionViewMutationLinking(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	pageID, sourcePageID, kind string, operations []graphOperation,
	then func(core.App, pageAuthorization) error,
	link func(core.App, pageAuthorization, *crdt.Doc, graphMutationInput) (graphMutationInput, error),
) error {
	hooks := contentGraphMutationHooks{Authorize: func(txApp core.App) (pageAuthorization, error) {
		return authorizeNotionViewPages(txApp, identity, pageID, sourcePageID, permissionWorkspaceWrite)
	}, PrepareInput: link}
	if then != nil {
		hooks.AfterApply, hooks.StepsKeepGraph = func(txApp core.App, authorization pageAuthorization, _ *crdt.Doc) error {
			return then(txApp, authorization)
		}, true
	}
	_, err := ygo.applyContentGraphMutationContext(
		event.Request.Context(), identity.PrincipalID, pageID,
		graphMutationInput{MutationID: "notion-public-view-" + kind + ":" + newDiagnosticReference(), Operations: operations},
		hooks,
	)
	return err
}

func serveNotionViewCreate(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:write", permissionWorkspaceWrite)
	if err != nil || !ok {
		return err
	}
	input := notionViewCreateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	if input.Sorts, err = notionViewCreateSorts(input.Sorts); err != nil {
		return notionDataSourceMutationError(event, err)
	}
	// A type the API has no view for is named as Notion's validation names it (seen 2026-11-06: "feed").
	if _, supported := notionOfficialViewTypes[input.Type]; !supported && input.Type != "" {
		return notionPageValidationError(event, notionBodyFault("body.type", "`\"table\"`, `\"board\"`, `\"list\"`, "+
			"`\"calendar\"`, `\"timeline\"`, `\"gallery\"`, `\"form\"`, `\"chart\"`, `\"map\"`, or `\"dashboard\"`",
			strconv.Quote(input.Type)))
	}
	newDatabase := len(input.CreateDatabase) != 0 && string(input.CreateDatabase) != "null"
	targets := notionViewCreateTargets{}
	if !newDatabase {
		found := false
		if targets, found, err = findNotionViewCreateTargets(event, workspace.Identity.WorkspaceID, input); !found {
			return err
		}
	}
	if refusal := notionViewConfigurationRefusal(input.Configuration); refusal != "" {
		return notionPageValidationError(event, refusal)
	}
	if refusal := notionNewChartRefusal(input.Type, input.Configuration); refusal != "" {
		return notionPageValidationError(event, refusal)
	}
	if refusal := notionNewMapRefusal(input.Type, input.Configuration); refusal != "" && input.ViewID == nil {
		return notionPageValidationError(event, refusal)
	}
	if input.ViewID != nil {
		name, err := notionViewName(input.Name)
		if _, supported := notionOfficialViewTypes[input.Type]; err != nil || !supported || input.Type == graphViewTypeDashboard ||
			input.DatabaseID != nil || newDatabase || len(input.Position) != 0 {
			return notionPageValidationError(event, "A dashboard widget needs view_id, data_source_id, a name and a non-dashboard view type, with an optional placement.")
		}
		return serveNotionViewCreateWidget(event, ygo, workspace.Identity, input, name, targets.source)
	}
	if len(input.Placement) != 0 {
		return notionPageValidationError(event, "A placement only applies to a widget created in a dashboard view_id.")
	}
	name, err := notionViewName(input.Name)
	if _, supported := notionOfficialViewTypes[input.Type]; err != nil || !supported || (input.DatabaseID == nil) == !newDatabase ||
		newDatabase && len(input.Position) != 0 {
		return notionPageValidationError(event, "The view request needs one of database_id or create_database, data_source_id, a name and a view type.")
	}
	if newDatabase {
		return serveNotionViewCreateDatabase(event, ygo, workspace.Identity, input, name)
	}
	database, sourceID := targets.database, targets.source.InternalID
	// Another database's source is linked by the view's making
	// (notion_view_linked_source.go); a dashboard takes the database's own.
	holder, err := loadNotionViewDatabase(event.App, database.InternalID)
	member := notionViewMember{}
	if err == nil {
		member, err = findNotionViewMember(event.App, database.InternalID, sourceID)
	}
	if err == nil && member.link && input.Type == graphViewTypeDashboard {
		if sourceID, err = notionViewFirstSource(event.App, database.InternalID); err == nil {
			member, err = findNotionViewMember(event.App, database.InternalID, sourceID)
		}
	}
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return notionPageValidationError(event, "The data source is not part of that database.")
	}
	if err != nil {
		return notionViewReadError(event, err)
	}
	created := notionViewLocation{
		ID: uuid.NewString(), WorkspaceID: workspace.Identity.WorkspaceID, PageID: holder.PageID,
		DataSourceID: sourceID, ContainerID: database.InternalID, SourcePageID: member.SourcePageID,
		Type: input.Type, Name: name, Config: "{}",
	}
	// A database owns the views of the sources it links.
	if member.Linked != "" {
		if input.Type == graphViewTypeForm {
			return notionPageValidationError(event, notionLinkedFormMessage)
		}
		created.OwnContainer = database.InternalID
	}
	if _, err := authorizeNotionViewPages(event.App, workspace.Identity, created.PageID, created.SourcePageID, permissionWorkspaceWrite); err != nil {
		return notionViewReadError(event, err)
	}
	configuration, formSettings, err := splitNotionViewFormSettings(created.Type, input.Configuration)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	// It keeps its own row order from the start, as every view made since
	// does, so its rows show newest first, as Notion's (seen 2026-11-11: a
	// view the API made without sorts showed A1,R4,R3,R2,R1 while the
	// default view held A1 where it was made, R4,R3,A1,R2,R1).
	config, err := notionViewUpdatedConfig(event.App, workspace.Identity.PrincipalID, created, string(notionCreatedViewConfig()), input.Filter, input.Sorts, input.QuickFilters, configuration)
	if err == nil {
		config, err = notionAPICreatedViewConfig(event.App, workspace.Identity.PrincipalID, created, config)
	}
	if err == nil {
		config, err = withNotionViewAPIFields(created.Type, config, input.Configuration, true)
	}
	if err == nil {
		config, err = withViewNameGiven(config, name)
	}
	if err == nil {
		config, err = withNotionAPIFormStart(created.Type, config)
	}
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	formAudience := ""
	if formSettings.present() {
		formAudience = nextNotionViewFormAudience(storedFormLink{}, false, formSettings)
		if config, err = withNotionViewFormAudience(config, formAudience, formSettings.access); err != nil {
			return notionDataSourceMutationError(event, err)
		}
	}
	placement, err := notionViewPlacement(event.App, workspace.Identity.WorkspaceID, input.Position)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	operations, err := notionPlacedTabOperations(event.App, workspace.Identity.WorkspaceID, database.InternalID,
		*notionViewOperation(created, created.Name, "", config).View, placement.notionTabPlacement)
	if errors.Is(err, errNotionTabAnchorMissing) {
		err = notionViewNotInDatabase(placement.anchorShown)
	}
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	if err := applyNotionViewMutationLinking(event, ygo, workspace.Identity, created.PageID, created.SourcePageID, "create", operations,
		notionViewFormAudienceSaver(workspace.Identity.PrincipalID, created, formAudience, formSettings.access),
		notionViewLinkInput(holder, sourceID, member)); err != nil {
		return notionDataSourceMutationError(event, err)
	}
	publicID, err := ensureNotionObjectIdentity(event.App, notionObjectIdentityView, created.WorkspaceID, created.ID)
	if err != nil {
		return notionViewReadError(event, err)
	}
	return serveNotionViewRetrieve(event, publicID)
}

// notionViewPlacement reads a new view's position, its shape already
// checked (notion_request_shape_routes.go): "end" when it names none, and the
// view an "after_view" names as the database's tabs know it, none when it is
// no view the token's workspace holds.
func notionViewPlacement(app core.App, workspaceID string, raw json.RawMessage) (notionViewTabPlacement, error) {
	position := struct {
		Type   string `json:"type"`
		ViewID string `json:"view_id"`
	}{Type: "end"}
	if len(raw) != 0 && json.Unmarshal(raw, &position) != nil {
		return notionViewTabPlacement{}, notionViewFilterInvalid("the view position is invalid")
	}
	placement := notionViewTabPlacement{notionTabPlacement: notionTabPlacement{kind: position.Type}}
	switch position.Type {
	case "start", "end":
		return placement, nil
	case "after_view":
		placement.anchorShown = notionBodyIDShown(position.ViewID)
		anchor, err := loadNotionViewLocation(app, workspaceID, position.ViewID)
		if err != nil && !errors.Is(err, errDatabaseRowPageNotFound) {
			return notionViewTabPlacement{}, err
		}
		placement.anchorID = anchor.ID
		return placement, nil
	}
	return notionViewTabPlacement{}, notionViewFilterInvalid("the view position is invalid")
}

// notionViewTabPlacement is a placement with its anchor as the request named it.
type notionViewTabPlacement struct {
	notionTabPlacement
	anchorShown string
}

// notionViewUpdatedConfig applies an official filter, sorts and configuration
// to a native view config, keeping its other settings.
func notionViewUpdatedConfig(
	app core.App, readerID string, location notionViewLocation, current string, filter, sorts, quickFilters, configuration json.RawMessage,
) (json.RawMessage, error) {
	scope, err := notionViewReaderScope(app, location, readerID)
	if err != nil {
		return nil, err
	}
	return scope.updatedViewConfig(location.Type, current, filter, sorts, quickFilters, configuration)
}

// updatedViewConfig is the one reading of an official view's filter, sorts,
// quick filters and configuration into a native view config: views created
// or changed through the API and views brought over by Notion import both
// take their settings through it.
func (scope notionViewFilterScope) updatedViewConfig(
	viewType, current string, filter, sorts, quickFilters, configuration json.RawMessage,
) (json.RawMessage, error) {
	config := map[string]any{}
	if json.Unmarshal([]byte(current), &config) != nil || config == nil {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	// The form of the filter and every quick filter first, then what they
	// name, as Notion's API checks a request (seen 2026-11-11).
	form := scope.formOnly()
	if _, err := form.nativeViewFilterGroup(filter); err != nil {
		return nil, err
	}
	if err := form.applyOfficialQuickFilters(quickFilters, map[string]any{}); err != nil {
		return nil, err
	}
	if len(filter) != 0 {
		group, err := scope.nativeViewFilterGroup(filter)
		if err != nil {
			return nil, err
		}
		delete(config, "filters")
		delete(config, "filterConjunction")
		if group == nil {
			delete(config, "filterGroup")
		} else {
			config["filterGroup"] = group
		}
	}
	if len(sorts) != 0 {
		native, err := scope.nativeViewSorts(sorts)
		if err != nil {
			return nil, err
		}
		if len(native) == 0 {
			delete(config, "sorts")
		} else {
			config["sorts"] = native
		}
	}
	if err := scope.applyOfficialQuickFilters(quickFilters, config); err != nil {
		return nil, err
	}
	if err := scope.applyOfficialViewConfiguration(viewType, configuration, config); err != nil {
		return nil, err
	}
	return json.Marshal(config)
}

func serveNotionViewUpdate(event *core.RequestEvent, ygo *pocketBaseYgoPersistence, publicID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:write", permissionWorkspaceWrite)
	if err != nil || !ok {
		return err
	}
	location, err := loadNotionViewLocation(event.App, workspace.Identity.WorkspaceID, publicID)
	if err == nil {
		_, err = authorizeNotionViewPages(event.App, workspace.Identity, location.PageID, location.SourcePageID, permissionWorkspaceWrite)
	}
	if err != nil {
		return notionViewReadError(event, err)
	}
	input := notionViewUpdateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	if refusal := notionViewConfigurationRefusal(input.Configuration); refusal != "" {
		return notionPageValidationError(event, refusal)
	}
	if refusal := notionChartGroupedRefusal(input.Configuration); refusal != "" && location.Type == graphViewTypeChart {
		return notionPageValidationError(event, refusal)
	}
	if input.Name == nil && len(input.Filter) == 0 && len(input.Sorts) == 0 && len(input.Configuration) == 0 && len(input.QuickFilters) == 0 {
		return notionPageValidationError(event, "The body needs name, filter, sorts, configuration or quick_filters.")
	}
	name := location.Name
	if input.Name != nil {
		if name, err = notionViewName(input.Name); err != nil {
			return notionDataSourceMutationError(event, err)
		}
	}
	configuration, formSettings, err := splitNotionViewFormSettings(location.Type, input.Configuration)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	current := location.Config
	if location.Type == graphViewTypeChart && len(configuration) != 0 && string(configuration) != "null" {
		if current, err = withoutNotionChartFormat(current); err != nil {
			return notionDataSourceMutationError(event, err)
		}
	}
	config, err := notionViewUpdatedConfig(event.App, workspace.Identity.PrincipalID, location, current, input.Filter, input.Sorts, input.QuickFilters, configuration)
	if err == nil {
		config, err = withNotionViewAPIFields(location.Type, config, input.Configuration, false)
	}
	if err == nil && input.Name != nil {
		config, err = withViewNameGiven(config, name)
	}
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	formAudience := ""
	if formSettings.present() {
		link, found, err := loadStoredFormLinkByView(event.App, location.OwnerID, location.PageID, location.ID)
		if err != nil {
			return notionDataSourceMutationError(event, err)
		}
		formAudience = nextNotionViewFormAudience(link, found, formSettings)
		if err := checkNotionViewFormQuestions(event.App, location, config, formAudience); err != nil {
			return notionDataSourceMutationError(event, err)
		}
		if config, err = withNotionViewFormAudience(config, formAudience, formSettings.access); err != nil {
			return notionDataSourceMutationError(event, err)
		}
	}
	if err := applyNotionViewMutationThen(event, ygo, workspace.Identity, location.PageID, location.SourcePageID, "update",
		[]graphOperation{notionViewOperation(location, name, location.Position, config)},
		notionViewFormAudienceSaver(workspace.Identity.PrincipalID, location, formAudience, formSettings.access)); err != nil {
		return notionDataSourceMutationError(event, err)
	}
	return serveNotionViewRetrieve(event, publicID)
}

// serveNotionViewDelete removes a view, keeping one view of each source and
// moving the database's selected view when it is the one removed.
func serveNotionViewDelete(event *core.RequestEvent, ygo *pocketBaseYgoPersistence, publicID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:write", permissionWorkspaceWrite)
	if err != nil || !ok {
		return err
	}
	location, err := loadNotionViewLocation(event.App, workspace.Identity.WorkspaceID, publicID)
	if err == nil {
		_, err = authorizeNotionViewPages(event.App, workspace.Identity, location.PageID, location.SourcePageID, permissionWorkspaceWrite)
	}
	if err != nil {
		return notionViewReadError(event, err)
	}
	reference, err := notionViewReference(event.App, location, false, event.Request, workspace.Identity.PrincipalID)
	if err != nil {
		return notionViewReadError(event, err)
	}
	operations, err := notionViewDeleteOperations(event.App, location)
	if errors.Is(err, errNotionViewKeepsOne) {
		return notionPageValidationError(event, notionViewKeepsOneMessage)
	}
	if err != nil {
		return notionViewReadError(event, err)
	}
	if err := applyNotionViewMutation(event, ygo, workspace.Identity, location.PageID, location.SourcePageID, "delete", operations); err != nil {
		return notionDataSourceMutationError(event, err)
	}
	return writeNotionJSON(event, http.StatusOK, reference)
}

const notionViewKeepsOneMessage = "A database keeps at least one view of each data source."

var errNotionViewKeepsOne = errors.New(notionViewKeepsOneMessage)

// notionViewDeleteOperations are the changes that remove a view, the API's
// and MCP's alike: a widget leaves its dashboard's rows, not being one of the
// database's tabs; a tab leaves a database that keeps another view of its
// source, which it shows next if it showed this one.
func notionViewDeleteOperations(app core.App, location notionViewLocation) ([]graphOperation, error) {
	if dashboardID := dashboardWidgetOwner([]byte(location.Config)); dashboardID != "" {
		return notionWidgetDeleteOperations(app, location, dashboardID)
	}
	siblings, err := notionViewTabSiblings(app, location.WorkspaceID, location.ContainerID, location.DataSourceID)
	if err != nil {
		return nil, err
	}
	siblings = slices.DeleteFunc(siblings, func(sibling notionViewLocation) bool {
		return dashboardWidgetOwner([]byte(sibling.Config)) != ""
	})
	if len(siblings) < 2 {
		return nil, errNotionViewKeepsOne
	}
	operations := []graphOperation{{Kind: graphOperationKindDeleteView, ViewDeletion: &graphDatabaseDelete{ID: location.ID}}}
	for _, sibling := range siblings {
		if sibling.ID == location.ID {
			continue
		}
		shown, err := notionDatabaseBlockShowing(app, location.ContainerID, sibling.ID, location.ID)
		if err != nil {
			return nil, err
		}
		if shown != nil {
			operations = append(operations, *shown)
		}
		break
	}
	return operations, nil
}

// notionDatabaseBlockShowing points a database block at the view it opens
// on, from the one it opens on now when that is from; nil when it doesn't,
// or when the database is no block. A database opens on its first tab, as
// Notion's does, so a tab put first or taken away moves it.
func notionDatabaseBlockShowing(app core.App, blockID, viewID, from string) (*graphOperation, error) {
	block := struct {
		ParentID string `db:"parent_id"`
		Position string `db:"position"`
		Content  string `db:"content"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[parent_id]], [[position]], [[content]] FROM {{leafen_blocks}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": blockID}).One(&block)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	content := map[string]any{}
	if json.Unmarshal([]byte(block.Content), &content) != nil {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	if from != "" && content["viewId"] != from || content["viewId"] == viewID {
		return nil, nil
	}
	content["viewId"] = viewID
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	var parent *string
	if block.ParentID != "" {
		parent = &block.ParentID
	}
	return &graphOperation{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
		ID: blockID, ParentID: parent, Position: block.Position, Type: graphBlockTypeInlineDatabase, Content: encoded,
	}}, nil
}
