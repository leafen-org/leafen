package app

import (
	"encoding/json"
	"slices"
	"sort"
)

// Readable exports show a database through one of its views, as Notion's
// export offers: the view its block shows (the current view) or its first
// view (the default view). The view's filter, search and sorts choose and
// order the table's rows through the database query rules, and its visible
// properties choose and order the columns. Row pages are still written for
// every row. A database whose view is outside the export keeps every row and
// property in database order; a linked source outside the export shows no
// rows without its view.

type workspaceExportViewTable struct {
	Properties []*workspaceExportDocProperty
	Rows       []*workspaceExportDocRow
	// Widths are the columns' widths on screen, in the properties' order.
	Widths []float64
}

// workspaceExportColumnWidth is a column's width on screen: the one it was
// given, else the title 280 and any other 200, as the table draws it.
func workspaceExportColumnWidth(view *workspaceExportDocView, property *workspaceExportDocProperty) float64 {
	if view != nil {
		if widths, ok := view.Config["propertyWidths"].(map[string]any); ok {
			if width, ok := widths[property.ID].(float64); ok && width > 0 {
				return width
			}
		}
	}
	if property.Type == graphValueTypeTitle {
		return 280
	}
	return 200
}

func workspaceExportColumnWidths(view *workspaceExportDocView, properties []*workspaceExportDocProperty) []float64 {
	widths := make([]float64, len(properties))
	for index, property := range properties {
		widths[index] = workspaceExportColumnWidth(view, property)
	}
	return widths
}

func workspaceExportDashboardWidget(view *workspaceExportDocView) bool {
	id, _ := view.Config["dashboardViewId"].(string)
	return id != ""
}

// containerView returns the view a database block shows for one of its
// sources: the block's current view when it shows that source, otherwise
// the source's first view in the block.
func (document *workspaceExportDocument) containerView(
	container *workspaceExportDocContainer, dataSourceID string,
) *workspaceExportDocView {
	current := ""
	if block := document.BlockByID[container.BlockID]; block != nil && !document.Payload.Options.DefaultDatabaseViews {
		current = mapString(block.Content, "viewId")
	}
	var first *workspaceExportDocView
	for _, id := range container.ViewIDs {
		view := document.Views[id]
		if view == nil || view.DataSourceID != dataSourceID || workspaceExportDashboardWidget(view) {
			continue
		}
		if id == current {
			return view
		}
		if first == nil {
			first = view
		}
	}
	return first
}

// sourceView returns the view a database's own table and CSV use: the one
// its own database block shows, or its first own view. A source outside
// the export shows as the linked database that placed it shows it.
func (document *workspaceExportDocument) sourceView(dataSourceID string) *workspaceExportDocView {
	if source := document.DataSources[dataSourceID]; source != nil && source.External {
		if source.Container == nil {
			return nil
		}
		return document.containerView(source.Container, dataSourceID)
	}
	for _, id := range workspaceExportSortedKeys(document.Containers) {
		container := document.Containers[id]
		if container.Kind == "source" && slices.Contains(container.DataSourceIDs, dataSourceID) {
			if view := document.containerView(container, dataSourceID); view != nil {
				return view
			}
		}
	}
	var first *workspaceExportDocView
	for _, view := range document.Views {
		if view.DataSourceID != dataSourceID || view.ContainerID != "" || workspaceExportDashboardWidget(view) {
			continue
		}
		if first == nil || workspaceExportPositionLess(view.Position, view.ID, first.Position, first.ID) {
			first = view
		}
	}
	return first
}

// viewTable returns the columns and rows a view shows, computed once per
// view.
func (document *workspaceExportDocument) viewTable(dataSourceID string, view *workspaceExportDocView) workspaceExportViewTable {
	if view == nil {
		// A source outside the export shows only through a view.
		if source := document.DataSources[dataSourceID]; source != nil && source.External {
			return workspaceExportViewTable{Properties: document.orderedProperties(dataSourceID)}
		}
		return workspaceExportViewTable{Properties: document.orderedProperties(dataSourceID), Rows: document.Rows[dataSourceID]}
	}
	if table, ok := document.ViewTables[view.ID]; ok {
		return table
	}
	if document.ViewTables == nil {
		document.ViewTables = map[string]workspaceExportViewTable{}
	}
	properties := workspaceExportViewProperties(document.Properties[dataSourceID], view)
	table := workspaceExportViewTable{
		Properties: properties,
		Rows:       document.viewRows(dataSourceID, view),
		Widths:     workspaceExportColumnWidths(view, properties),
	}
	document.ViewTables[view.ID] = table
	return table
}

// workspaceExportViewProperties orders and filters properties as the view
// shows them: the view's property order first, the title always shown, and
// then its visible list or hidden set.
func workspaceExportViewProperties(properties []*workspaceExportDocProperty, view *workspaceExportDocView) []*workspaceExportDocProperty {
	byID := map[string]*workspaceExportDocProperty{}
	for _, property := range properties {
		byID[property.ID] = property
	}
	ordered := workspaceExportViewOrder(properties, view)
	// Imported Notion views list their visible properties in Notion's
	// order, and an imported gallery without the list shows only titles.
	visible, hasVisible := view.Config["visibleProperties"].([]any)
	_, listsVisible := view.Config["visibleProperties"]
	_, hasSettings := view.Config["notionPropertySettings"]
	if !listsVisible && hasSettings && view.Type == "gallery" {
		visible, hasVisible = []any{}, true
	}
	visibleIDs := notionViewStringSet(visible)
	hidden := notionViewStringSet(view.Config["hiddenProperties"])
	_, settingsList := view.Config["notionPropertySettings"].([]any)
	_, notionViewID := view.Config["notionViewId"].(string)
	if hasVisible && (settingsList || notionViewID) {
		result := []*workspaceExportDocProperty{}
		seen := map[string]bool{}
		for _, raw := range visible {
			id, _ := raw.(string)
			if property := byID[id]; property != nil && !seen[id] {
				result = append(result, property)
				seen[id] = true
			}
		}
		for _, property := range ordered {
			if property.Type == graphValueTypeTitle && !seen[property.ID] {
				result = append(result, property)
				seen[property.ID] = true
			}
		}
		return workspaceExportTitleFirst(result)
	}
	result := []*workspaceExportDocProperty{}
	for _, property := range ordered {
		_, listed := visibleIDs[property.ID]
		_, isHidden := hidden[property.ID]
		if property.Type == graphValueTypeTitle || (hasVisible && listed) || (!hasVisible && !isHidden) {
			result = append(result, property)
		}
	}
	return result
}

// workspaceExportViewOrder is every property in the view's order, those it
// hides included: the view's property order first, the title and the rest
// after by name, as the view shows them (database_view_property_order.go).
func workspaceExportViewOrder(properties []*workspaceExportDocProperty, view *workspaceExportDocView) []*workspaceExportDocProperty {
	order := []any(nil)
	if view != nil {
		order, _ = view.Config["propertyOrder"].([]any)
	}
	properties = databaseViewDefaultOrder(properties,
		func(property *workspaceExportDocProperty) string { return property.Name },
		func(property *workspaceExportDocProperty) bool { return property.Type == graphValueTypeTitle })
	byID := map[string]*workspaceExportDocProperty{}
	for _, property := range properties {
		byID[property.ID] = property
	}
	ordered := []*workspaceExportDocProperty{}
	seen := map[string]bool{}
	for _, raw := range order {
		id, _ := raw.(string)
		if property := byID[id]; property != nil && !seen[id] {
			ordered = append(ordered, property)
			seen[id] = true
		}
	}
	for _, property := range properties {
		if !seen[property.ID] {
			ordered = append(ordered, property)
		}
	}
	return ordered
}

func workspaceExportTitleFirst(properties []*workspaceExportDocProperty) []*workspaceExportDocProperty {
	ordered := make([]*workspaceExportDocProperty, 0, len(properties))
	for _, property := range properties {
		if property.Type == graphValueTypeTitle {
			ordered = append([]*workspaceExportDocProperty{property}, ordered...)
		} else {
			ordered = append(ordered, property)
		}
	}
	return ordered
}

// viewRows keeps the source's active rows that the view's filter and search
// accept, in the view's sort order. Formulas and rollups are the values the
// export computed; a view the query rules reject shows every active row.
func (document *workspaceExportDocument) viewRows(dataSourceID string, view *workspaceExportDocView) []*workspaceExportDocRow {
	active := []*workspaceExportDocRow{}
	for _, row := range document.Rows[dataSourceID] {
		if !row.Archived {
			active = append(active, row)
		}
	}
	context, ok := document.queryContext(dataSourceID)
	if !ok {
		return active
	}
	config, err := json.Marshal(view.Config)
	if err != nil {
		return active
	}
	context.ViewConfig = config
	// A view's days ("오늘", "지난 주") are the exporter's, as its cells' are
	// their viewer's in the app.
	plan, err := compileDatabaseQueryPlan(databaseQueryRequest{DataSourceID: dataSourceID, TimeZone: document.exportLocation().String()}, context)
	if err != nil {
		return active
	}
	pageID := ""
	if source := document.DataSources[dataSourceID]; source != nil {
		pageID = source.PageID
	}
	dependencies := document.queryRows()
	type ranked struct {
		row *workspaceExportDocRow
		key string
	}
	accepted := []ranked{}
	for _, row := range active {
		evaluated, ok, err := evaluateDatabaseQueryRow(
			dependencies[row.ID], dependencies, document.computedValues(row), context, plan, document.Principal, pageID,
		)
		if err != nil {
			return active
		}
		if ok {
			accepted = append(accepted, ranked{row: row, key: evaluated.SortKey})
		}
	}
	sort.SliceStable(accepted, func(left, right int) bool {
		if accepted[left].key != accepted[right].key {
			return accepted[left].key < accepted[right].key
		}
		return accepted[left].row.ID < accepted[right].row.ID
	})
	rows := make([]*workspaceExportDocRow, len(accepted))
	for index, item := range accepted {
		rows[index] = item.row
	}
	return rows
}

// queryRows gives every exported row in the shape the query rules read,
// built once.
func (document *workspaceExportDocument) queryRows() map[string]storedDatabaseQueryRow {
	if document.QueryRows != nil {
		return document.QueryRows
	}
	document.QueryRows = make(map[string]storedDatabaseQueryRow, len(document.RowByID))
	for id, row := range document.RowByID {
		values := make(map[string]graphRowValue, len(row.Values))
		for propertyID, raw := range row.Values {
			wrapper, _ := raw.(map[string]any)
			encoded, err := json.Marshal(wrapper["value"])
			if err != nil {
				continue
			}
			valueType, _ := wrapper["type"].(string)
			values[propertyID] = graphRowValue{Type: valueType, Value: encoded}
		}
		document.QueryRows[id] = storedDatabaseQueryRow{
			ID: id, Position: row.Position, Values: values, Computed: map[string]databaseComputedValue{},
			Created: row.Created, Updated: row.Updated,
			CreatedBy: row.CreatedBy, LastEditedBy: row.LastEditedBy,
		}
	}
	return document.QueryRows
}
