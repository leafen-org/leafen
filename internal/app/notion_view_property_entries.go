package app

import "slices"

// officialViewProperties lists a view's properties as Notion's API gives
// them. A view that keeps an order of its properties (propertyOrder: every
// property it had when the order was set, through the API or the screen)
// lists those alone, its title too, and not a property made since, as
// Notion's view lists what it stored (seen 2026-11-12, a table, board,
// gallery, list, calendar and timeline alike); one keeping no order but the
// properties it shows, as a new list one beside its title, lists its title
// and those (seen 2026-11-12: "Name · 0선택"); any other lists every
// property, its title first and the rest by name
// (database_view_property_order.go). A view that lists its shown properties
// (visibleProperties, even empty, as a new board's or gallery's title alone)
// shows those and its title, as the screen reads it
// (propertyVisibilityModel.ts); any other all but its hidden ones. A
// gallery's and a list's entries don't name their property, as Notion's
// (seen 2026-11-12); a table's, board's, calendar's and timeline's do.
func (scope notionViewFilterScope) officialViewProperties(viewType string, config map[string]any) []any {
	order := config["propertyOrder"]
	hidden := notionViewStringSet(config["hiddenProperties"])
	visibleOnly := notionViewStringSet(config["visibleProperties"])
	_, listsShown := config["visibleProperties"].([]any)
	wrapped := notionViewStringSet(config["wrappedColumns"])
	checkboxes := notionViewStringSet(config["statusCheckboxColumns"])
	widths, _ := config["propertyWidths"].(map[string]any)
	ids := scope.knownViewProperties(order)
	kept := len(ids) > 0
	if shown := scope.knownViewProperties(config["visibleProperties"]); !kept && len(shown) > 0 {
		for _, property := range scope.viewOrdered() {
			if property.OfficialType == notionTitlePropertyID && !slices.Contains(shown, property.ID) {
				ids = append(ids, property.ID)
			}
		}
		ids, kept = append(ids, shown...), true
	}
	for _, property := range scope.viewOrdered() {
		if !slices.Contains(ids, property.ID) && (!kept || property.OfficialType == notionTitlePropertyID) {
			ids = append(ids, property.ID)
		}
	}
	named := viewType != graphViewTypeGallery && viewType != graphViewTypeList
	properties := make([]any, 0, len(ids))
	for _, id := range ids {
		_, isHidden := hidden[id]
		_, isListed := visibleOnly[id]
		entry := map[string]any{
			"property_id": scope.viewID(id),
			"visible":     !isHidden && (!listsShown || isListed || scope.byID[id].OfficialType == "title"),
		}
		if named {
			entry["property_name"] = scope.byID[id].Name
		}
		if width, ok := widths[id].(float64); ok && width >= 0 {
			entry["width"] = int(width)
		}
		if _, wrap := wrapped[id]; wrap {
			entry["wrap"] = true
		}
		// A status column shown as a checkbox, Notion's "다음과 같이 표시".
		if _, checkbox := checkboxes[id]; checkbox && scope.byID[id].NativeType == graphValueTypeStatus {
			entry["status_show_as"] = "checkbox"
		}
		properties = append(properties, entry)
	}
	return properties
}

// knownViewProperties is a list of property IDs a view keeps, without those
// its source doesn't have and repeats.
func (scope notionViewFilterScope) knownViewProperties(value any) []string {
	items, _ := value.([]any)
	ids := []string{}
	for _, raw := range items {
		if id, ok := raw.(string); ok && !slices.Contains(ids, id) {
			if _, known := scope.byID[id]; known {
				ids = append(ids, id)
			}
		}
	}
	return ids
}
