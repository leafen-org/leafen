package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// mcpViewTarget is where a view change is written: views of a database's own
// source live with their source, views of a linked source on the database's
// page.
type mcpViewTarget struct {
	PageID      string
	ContainerID string
}

func mcpDatabaseViewTarget(
	container mcpDatabaseContainerLocation, source mcpDataSourceLocation, linked bool,
) mcpViewTarget {
	if linked {
		return mcpViewTarget{PageID: container.PageID, ContainerID: container.ID}
	}
	return mcpViewTarget{PageID: source.PageID}
}

// authorizeMCPViewSource checks source access for a view change: changing a
// database's own view edits the source page, while a linked database's view
// only reads it.
func authorizeMCPViewSource(
	app core.App, authority mcpAuthority, location mcpDataSourceLocation, linked bool,
) (workspaceAuthorization, error) {
	if !linked {
		return authorizeMCPViewSourceWrite(app, authority, location)
	}
	page, err := authorizePage(app, authority.PrincipalID, location.PageID, permissionWorkspaceRead)
	if err != nil || page.Workspace.WorkspaceID != location.WorkspaceID ||
		page.Workspace.OwnerID != location.OwnerID {
		return workspaceAuthorization{}, mcpRefusedUnlessStorage(errMCPCantRead, err)
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: location.WorkspaceID,
	}, permissionWorkspaceWrite)
	if err != nil || workspace.WorkspaceID != location.WorkspaceID {
		return workspaceAuthorization{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	return workspace, nil
}

// nextMCPLinkedViewPosition appends after the linked database's last view.
func nextMCPLinkedViewPosition(app core.App, container mcpDatabaseContainerLocation) (string, error) {
	var tail string
	err := app.DB().NewQuery(`
		SELECT [[position]] FROM {{leafen_database_views}}
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
			AND [[page_id]] = {:pageId} AND [[container_id]] = {:containerId}
		ORDER BY [[position]] DESC, [[id]] DESC LIMIT 1
	`).Bind(dbx.Params{
		"owner": container.OwnerID, "workspaceId": container.WorkspaceID,
		"pageId": container.PageID, "containerId": container.ID,
	}).Row(&tail)
	if errors.Is(err, sql.ErrNoRows) {
		return "p0000000000", nil
	}
	if err != nil {
		return "", err
	}
	return databasePositionAfter(tail)
}

// loadMCPDatabaseContainerViews returns the views a database shows, in tab
// order: a linked database's own views or its sources' views.
func loadMCPDatabaseContainerViews(
	app core.App, container mcpDatabaseContainerLocation,
) ([]notionMCPDataSourceView, error) {
	stored := []storedMCPDataSourceView{}
	if err := app.DB().NewQuery(`
		SELECT [[view]].[[id]], [[view]].[[name]], [[view]].[[type]], [[view]].[[position]], [[view]].[[config]],
			[[view]].[[data_source_id]]
		FROM {{leafen_database_container_views}} AS [[membership]]
		JOIN {{leafen_database_views}} AS [[view]] ON [[view]].[[id]] = [[membership]].[[view_id]]
		WHERE [[membership]].[[container_id]] = {:containerId}
			AND [[membership]].[[owner]] = {:owner} AND [[membership]].[[workspace_id]] = {:workspaceId}
		ORDER BY [[membership]].[[position]], [[membership]].[[view_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"containerId": container.ID, "owner": container.OwnerID, "workspaceId": container.WorkspaceID,
		"limit": maxGraphMutationOperations + 1,
	}).All(&stored); err != nil {
		return nil, err
	}
	if len(stored) > maxGraphMutationOperations {
		return nil, errMCPToolFailed
	}
	views := make([]notionMCPDataSourceView, 0, len(stored))
	for _, view := range stored {
		// A widget reads under its dashboard, not as a tab of its own.
		if dashboardWidgetOwner([]byte(view.Config)) != "" {
			continue
		}
		config := map[string]any{}
		if err := json.Unmarshal([]byte(view.Config), &config); err != nil || config == nil {
			return nil, errors.New(errorCodeGraphReadFailed)
		}
		views = append(views, notionMCPDataSourceView{
			ID: view.ID, Name: view.Name, Type: view.Type, Config: config, DataSourceID: view.DataSourceID,
		})
	}
	return views, nil
}

// mcpViewFetchConfig is a view's configuration as Notion's fetch gives it:
// its data source, the properties it shows in order, its name and type.
func mcpViewFetchConfig(
	app core.App, view notionMCPDataSourceView, dataSourceID string, properties []notionMCPDataSourceProperty,
) map[string]any {
	byID := make(map[string]notionMCPDataSourceProperty, len(properties))
	for _, property := range properties {
		byID[property.ID] = property
	}
	ids := []string{}
	order, _ := view.Config["propertyOrder"].([]any)
	for _, raw := range order {
		if id, ok := raw.(string); ok && !slices.Contains(ids, id) {
			if _, known := byID[id]; known {
				ids = append(ids, id)
			}
		}
	}
	// The rest after them by name, as the view shows them (database_view_property_order.go).
	rest := databaseViewDefaultOrder(properties,
		func(property notionMCPDataSourceProperty) string { return property.Name },
		func(property notionMCPDataSourceProperty) bool { return property.Type == "title" })
	for _, property := range rest {
		if !slices.Contains(ids, property.ID) {
			ids = append(ids, property.ID)
		}
	}
	hidden := notionViewStringSet(view.Config["hiddenProperties"])
	shown := []string{}
	for _, id := range ids {
		_, isHidden := hidden[id]
		if !isHidden && mcpViewListed(view.Config, id, byID) && mcpDatabasePropertyShown(byID[id].Type) {
			shown = append(shown, byID[id].Name)
		}
	}
	fetched := mcpViewFetchSettings(view.Config, byID)
	mcpViewFetchNotionNames(view.Type, view.Config, fetched, byID)
	mcpViewFetchGroupSettings(view.Type, view.Config, fetched, byID)
	if view.Type == graphViewTypeGallery {
		fetched["wrapProperties"] = mcpGalleryWrappedProperties(view.Config, shown, ids, byID)
	}
	fetched["dataSourceUrl"] = "{{" + mcpDataSourceURL(app, dataSourceID) + "}}"
	fetched["displayProperties"], fetched["name"], fetched["type"] = shown, view.Name, view.Type
	// A view showing its tab rather than its source's title says so, as
	// create-view's show_data_source_title sets it.
	if view.Config["showDataSourceTitle"] == false {
		fetched["showDataSourceTitle"] = false
	}
	switch view.Type {
	case graphViewTypeForm:
		delete(fetched, "displayProperties")
		fetched["type"], fetched["questions"] = "form_editor", mcpFormViewQuestions(view.Config, properties, byID)
	case graphViewTypeChart:
		// A chart shows what it charts, not properties (Notion's, seen 2026-11-09).
		delete(fetched, "displayProperties")
		fetched["chartConfig"] = mcpViewChartConfig(view.Config, byID)
	}
	return fetched
}

// mcpViewFetchNotionNames names a card or table view's settings as Notion's
// fetch does (seen 2026-11-05): a group's property with its type, sort and
// whether its empty groups hide, as the view draws them (viewHidesEmptyGroups:
// a table's and a board's by values unless told not to, a board's by
// options when told to), a date's unit as a chart's axis names it, and the
// cards' layout.
func mcpViewFetchNotionNames(
	viewType string, config, fetched map[string]any, byID map[string]notionMCPDataSourceProperty,
) {
	if property, ok := fetched["groupBy"].(string); ok {
		id, _ := config["groupBy"].(string)
		hide := viewHidesEmptyGroups(viewType, byID[id].Type, config, false)
		group := map[string]any{
			"property": property, "hideEmptyGroups": hide, "propertyType": mcpNotionPropertyType(byID[id].Type),
			"sort": map[string]any{"type": "manual"},
		}
		if notionViewConditionFamily(notionDatabaseRowPageOfficialPropertyType(byID[id].Type)) == "date" {
			by, _ := config["dateGroupBy"].(string)
			if !slices.Contains(notionViewDateGroupings, by) {
				by = "relative"
			}
			group["groupBy"] = by
		}
		fetched["groupBy"] = group
		delete(fetched, "hideEmptyGroups")
	}
	if widths, ok := fetched["widths"]; ok {
		fetched["columnWidths"] = widths
		delete(fetched, "widths")
	}
	if wrap, ok := fetched["wrap"]; ok {
		delete(fetched, "wrap")
		if viewType == graphViewTypeTable {
			fetched["wrapCells"] = wrap
		}
	}
	if _, frozen := fetched["frozenColumns"]; frozen {
		fetched["frozenColumnIndex"] = notionViewFrozenColumnIndex(fetched)
	}
	delete(fetched, "frozenColumns")
	if viewType == graphViewTypeBoard || viewType == graphViewTypeGallery {
		fetched["fullWidthProperties"] = []string{}
		fetched["cardLayoutMode"] = "default"
		if layout, ok := fetched["cardLayout"].(string); ok && layout != "" {
			fetched["cardLayoutMode"] = layout
		}
		delete(fetched, "cardLayout")
	}
}

// mcpFormViewQuestions is a form view's questions as fetch names them, as
// Notion's (seen 2026-11-05): a form nobody has asked anything of yet asks its
// title. A question's settings beyond its name, as FORM QUESTIONS writes them,
// show where they aren't a new question's.
func mcpFormViewQuestions(
	config map[string]any, properties []notionMCPDataSourceProperty, byID map[string]notionMCPDataSourceProperty,
) []map[string]any {
	questions := []map[string]any{}
	for _, question := range mcpViewFormConfiguration(config).Questions {
		property, known := byID[question.PropertyID]
		if !known {
			continue
		}
		name := question.Label
		if question.SyncWithPropertyName && !formQuestionOwnLabel(&question) || name == "" {
			name = property.Name
		}
		asked := map[string]any{"name": name, "property": property.Name}
		if question.Required {
			asked["required"] = true
		}
		if question.MaxSelections != nil {
			asked["maxSelections"] = *question.MaxSelections
		}
		if question.OptionsDisplay == "dropdown" {
			asked["optionsDisplay"] = "dropdown"
		}
		questions = append(questions, asked)
	}
	if len(questions) > 0 {
		return questions
	}
	for _, property := range properties {
		if property.Type == graphValueTypeTitle {
			return []map[string]any{{"name": "Title", "property": property.Name}}
		}
	}
	return questions
}
