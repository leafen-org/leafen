package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Official dashboard widgets are views created with the dashboard's view_id
// and a placement: new_row inserts a row (at row_index or last), existing_row
// joins the row at row_index. A widget shows one of the dashboard database's
// sources.

type notionViewPlacementRequest struct {
	Type     string `json:"type"`
	RowIndex *int   `json:"row_index"`
}

func loadNotionViewLocationByID(app core.App, workspaceID, viewID string) (notionViewLocation, error) {
	location := notionViewLocation{}
	err := app.DB().NewQuery(notionViewLocationSelect + ` AND [[view]].[[id]] = {:id}`).
		Bind(dbx.Params{"workspaceId": workspaceID, "id": viewID}).One(&location)
	if errors.Is(err, sql.ErrNoRows) {
		return notionViewLocation{}, errDatabaseRowPageNotFound
	}
	return location, err
}

// dashboardLayoutWithWidget places a widget as the placement asks. A new row
// takes the default height and the widget the default width.
func dashboardLayoutWithWidget(layout dashboardViewConfig, raw json.RawMessage, widgetID string) (dashboardViewConfig, error) {
	placement := notionViewPlacementRequest{Type: "new_row"}
	if len(raw) != 0 && string(raw) != "null" {
		if json.Unmarshal(raw, &placement) != nil {
			return dashboardViewConfig{}, notionViewFilterInvalid("the widget placement is invalid")
		}
	}
	total := 0
	for _, row := range layout.DashboardRows {
		total += len(row)
	}
	if total >= maxDashboardWidgets {
		return dashboardViewConfig{}, notionViewFilterInvalid("a dashboard holds at most 12 widgets")
	}
	next := layout.clone()
	switch placement.Type {
	case "new_row":
		index := len(next.DashboardRows)
		if placement.RowIndex != nil {
			index = *placement.RowIndex
		}
		if index < 0 || index > len(next.DashboardRows) {
			return dashboardViewConfig{}, notionViewFilterInvalid("the widget row_index is out of range")
		}
		next.DashboardRows = slices.Insert(next.DashboardRows, index, []string{widgetID})
		if next.DashboardRowHeights != nil {
			next.DashboardRowHeights = slices.Insert(next.DashboardRowHeights, index, 0)
		}
		if next.DashboardWidgetWidths != nil {
			next.DashboardWidgetWidths = slices.Insert(next.DashboardWidgetWidths, index, []float64{1})
		}
		return next, nil
	case "existing_row":
		if placement.RowIndex == nil || *placement.RowIndex < 0 || *placement.RowIndex >= len(next.DashboardRows) {
			return dashboardViewConfig{}, notionViewFilterInvalid("an existing_row placement needs a row_index of an existing row")
		}
		index := *placement.RowIndex
		if len(next.DashboardRows[index]) >= maxDashboardWidgetsPerRow {
			return dashboardViewConfig{}, notionViewFilterInvalid("a dashboard row holds at most 4 widgets")
		}
		next.DashboardRows[index] = append(next.DashboardRows[index], widgetID)
		if next.DashboardWidgetWidths != nil {
			next.DashboardWidgetWidths[index] = append(next.DashboardWidgetWidths[index], 1)
		}
		return next, nil
	}
	return dashboardViewConfig{}, notionViewFilterInvalid("the widget placement type must be new_row or existing_row")
}

func (layout dashboardViewConfig) clone() dashboardViewConfig {
	next := dashboardViewConfig{DashboardViewID: layout.DashboardViewID}
	for _, row := range layout.DashboardRows {
		next.DashboardRows = append(next.DashboardRows, slices.Clone(row))
	}
	next.DashboardRowHeights = slices.Clone(layout.DashboardRowHeights)
	if layout.DashboardWidgetWidths != nil {
		next.DashboardWidgetWidths = [][]float64{}
		for _, widths := range layout.DashboardWidgetWidths {
			next.DashboardWidgetWidths = append(next.DashboardWidgetWidths, slices.Clone(widths))
		}
	}
	return next
}

// withoutWidget takes a widget out of its row, and an emptied row out of the
// dashboard, keeping the other rows' heights and widths.
func (layout dashboardViewConfig) withoutWidget(widgetID string) dashboardViewConfig {
	next := dashboardViewConfig{}
	sized := layout.validSizes()
	for index, row := range layout.DashboardRows {
		kept, widths := []string{}, []float64{}
		for column, id := range row {
			if id == widgetID {
				continue
			}
			kept = append(kept, id)
			if sized && layout.DashboardWidgetWidths != nil {
				widths = append(widths, layout.DashboardWidgetWidths[index][column])
			}
		}
		if len(kept) == 0 {
			continue
		}
		next.DashboardRows = append(next.DashboardRows, kept)
		if sized && layout.DashboardRowHeights != nil {
			next.DashboardRowHeights = append(next.DashboardRowHeights, layout.DashboardRowHeights[index])
		}
		if sized && layout.DashboardWidgetWidths != nil {
			next.DashboardWidgetWidths = append(next.DashboardWidgetWidths, widths)
		}
	}
	return next
}

func (layout dashboardViewConfig) validSizes() bool {
	return validateDashboardLayoutSizes(layout) == nil
}

// dashboardConfigWithLayout rewrites a dashboard's stored config with new rows
// and their sizes.
func dashboardConfigWithLayout(current string, layout dashboardViewConfig) (json.RawMessage, error) {
	config := map[string]any{}
	if json.Unmarshal([]byte(current), &config) != nil || config == nil {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	set := func(key string, value any, present bool) {
		if present {
			config[key] = value
		} else {
			delete(config, key)
		}
	}
	set("dashboardRows", layout.DashboardRows, len(layout.DashboardRows) > 0)
	set("dashboardRowHeights", layout.DashboardRowHeights, len(layout.DashboardRowHeights) > 0)
	set("dashboardWidgetWidths", layout.DashboardWidgetWidths, len(layout.DashboardWidgetWidths) > 0)
	return json.Marshal(config)
}

// serveNotionViewCreateWidget adds a widget of source, found first
// (findNotionViewCreateTargets), to the dashboard view_id names: one the
// token can't find is a view Notion's finds none of, and one that is no
// dashboard refused as no dashboard view (seen 2026-11-12).
func serveNotionViewCreateWidget(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	input notionViewCreateRequest, name string, source notionObjectIdentity,
) error {
	noteNotionAskedObject(event, "view", notionBodyIDShown(*input.ViewID))
	dashboard, err := loadNotionViewLocation(event.App, identity.WorkspaceID, *input.ViewID)
	if err != nil {
		return notionViewReadError(event, err)
	}
	if dashboard.Type != graphViewTypeDashboard {
		return notionDataSourceMutationError(event, notionViewNotDashboard(notionBodyIDShown(*input.ViewID)))
	}
	// A widget of another database's source links it, as a tab does
	// (notion_view_linked_source.go).
	holder, err := loadNotionViewDatabase(event.App, dashboard.ContainerID)
	member := notionViewMember{}
	if err == nil {
		member, err = findNotionViewMember(event.App, dashboard.ContainerID, source.InternalID)
	}
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return notionPageValidationError(event, "The data source is not part of the dashboard's database.")
	}
	if err != nil {
		return notionViewReadError(event, err)
	}
	created := notionViewLocation{
		ID: uuid.NewString(), WorkspaceID: identity.WorkspaceID, PageID: dashboard.PageID,
		DataSourceID: source.InternalID, ContainerID: dashboard.ContainerID, SourcePageID: member.SourcePageID,
		Type: input.Type, Name: name,
	}
	// A database owns the views of the sources it links.
	if member.Linked != "" {
		if input.Type == graphViewTypeForm {
			return notionPageValidationError(event, notionLinkedFormMessage)
		}
		created.OwnContainer = dashboard.ContainerID
	}
	if _, err := authorizeNotionViewPages(event.App, identity, created.PageID, created.SourcePageID, permissionWorkspaceWrite); err != nil {
		return notionViewReadError(event, err)
	}
	owner, err := json.Marshal(map[string]string{"dashboardViewId": dashboard.ID})
	if err != nil {
		return err
	}
	config, err := notionViewUpdatedConfig(event.App, identity.PrincipalID, created, string(owner), input.Filter, input.Sorts, input.QuickFilters, input.Configuration)
	if err == nil {
		config, err = notionAPICreatedViewConfig(event.App, identity.PrincipalID, created, config)
	}
	if err == nil {
		config, err = withNotionViewAPIFields(created.Type, config, input.Configuration, true)
	}
	if err == nil {
		config, err = withViewNameGiven(config, name)
	}
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	layout, err := dashboardLayoutWithWidget(parseDashboardViewConfig([]byte(dashboard.Config)), input.Placement, created.ID)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	dashboardConfig, err := dashboardConfigWithLayout(dashboard.Config, layout)
	if err != nil {
		return notionViewReadError(event, err)
	}
	siblings, err := notionViewSiblings(event.App, identity.WorkspaceID, dashboard.ContainerID, source.InternalID)
	if err != nil {
		return notionViewReadError(event, err)
	}
	positions := make([]string, 0, len(siblings))
	for _, sibling := range siblings {
		positions = append(positions, sibling.Position)
	}
	position, err := notionDataSourceMoveViewPosition(positions)
	if err != nil {
		return notionViewReadError(event, err)
	}
	operations := []graphOperation{
		notionViewOperation(dashboard, dashboard.Name, dashboard.Position, dashboardConfig),
		notionViewOperation(created, name, position, config),
	}
	if err := applyNotionViewMutationLinking(event, ygo, identity, created.PageID, created.SourcePageID, "create", operations,
		nil, notionViewLinkInput(holder, source.InternalID, member)); err != nil {
		return notionDataSourceMutationError(event, err)
	}
	publicID, err := ensureNotionObjectIdentity(event.App, notionObjectIdentityView, created.WorkspaceID, created.ID)
	if err != nil {
		return notionViewReadError(event, err)
	}
	return serveNotionViewRetrieve(event, publicID)
}

// notionWidgetDeleteOperations removes a widget and takes it out of its
// dashboard's rows.
func notionWidgetDeleteOperations(app core.App, location notionViewLocation, dashboardID string) ([]graphOperation, error) {
	operations := []graphOperation{{Kind: graphOperationKindDeleteView, ViewDeletion: &graphDatabaseDelete{ID: location.ID}}}
	dashboard, err := loadNotionViewLocationByID(app, location.WorkspaceID, dashboardID)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return operations, nil
	}
	if err != nil {
		return nil, err
	}
	config, err := dashboardConfigWithLayout(dashboard.Config, parseDashboardViewConfig([]byte(dashboard.Config)).withoutWidget(location.ID))
	if err != nil {
		return nil, err
	}
	return append(operations, notionViewOperation(dashboard, dashboard.Name, dashboard.Position, config)), nil
}

// dashboardRowPublicID and dashboardWidgetPublicID name a dashboard's row and
// widget where it is read, as Notion's rows and widgets have IDs of their own;
// a row is stored as its widgets, so it goes by its first.
func dashboardRowPublicID(dashboardID string, row []string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("dashboard-row\x00"+dashboardID+"\x00"+row[0])).String()
}

func dashboardWidgetPublicID(dashboardID, widgetID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("dashboard-widget\x00"+dashboardID+"\x00"+widgetID)).String()
}

// notionViewDashboardRows is a dashboard's configuration rows as Notion's API
// gives them (seen 2026-11-05): each widget names its view and its row's index,
// and a row or widget that was sized says so.
func notionViewDashboardRows(app core.App, location notionViewLocation) ([]any, error) {
	layout := parseDashboardViewConfig([]byte(location.Config))
	sized := layout.validSizes()
	rows := make([]any, 0, len(layout.DashboardRows))
	for index, stored := range layout.DashboardRows {
		if len(stored) == 0 {
			continue
		}
		widgets := make([]any, 0, len(stored))
		for column, widgetID := range stored {
			publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityView, location.WorkspaceID, widgetID)
			if err != nil {
				return nil, err
			}
			widget := map[string]any{
				"id": dashboardWidgetPublicID(location.ID, widgetID), "view_id": publicID, "row_index": index,
			}
			if sized && layout.DashboardWidgetWidths != nil {
				widget["width"] = layout.DashboardWidgetWidths[index][column]
			}
			widgets = append(widgets, widget)
		}
		row := map[string]any{"id": dashboardRowPublicID(location.ID, stored), "widgets": widgets}
		if sized && layout.DashboardRowHeights != nil && layout.DashboardRowHeights[index] > 0 {
			row["height"] = layout.DashboardRowHeights[index]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func notionViewDashboardFields(app core.App, location notionViewLocation, object map[string]any) error {
	if location.Type == graphViewTypeDashboard {
		object["data_source_id"] = nil
	}
	dashboardID := dashboardWidgetOwner([]byte(location.Config))
	if dashboardID == "" {
		return nil
	}
	publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityView, location.WorkspaceID, dashboardID)
	if err != nil {
		return err
	}
	object["dashboard_view_id"] = publicID
	return nil
}
