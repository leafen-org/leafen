package app

import "slices"

// What fetch reads back of a view's settings Notion's fetch has no word for,
// which Leafen's configure sets (mcp_view_configure_groups.go): the order
// of the groups it was given, the groups it hides, whether they show their
// counts and whether a board colors its columns, each named as the
// directive that sets it; and the properties a gallery wraps, as Notion's
// fetch lists them once WRAP PROPERTIES is on (seen 2026-11-16: every
// property of its list, the shown ones first). A view never given them reads
// as Notion's does (2026-11-16: an agent's GROUPS, HIDE GROUPS, GROUP COUNTS
// and COLOR COLUMNS answered success and read back nothing).

// mcpViewFetchGroupSettings adds a grouped view's group settings to fetched.
func mcpViewFetchGroupSettings(
	viewType string, config, fetched map[string]any, byID map[string]notionMCPDataSourceProperty,
) {
	id, _ := config["groupBy"].(string)
	property, grouped := byID[id]
	if !grouped {
		return
	}
	names := mcpGroupNames(property)
	named := func(raw any) []string {
		listed := []string{}
		for _, item := range mcpAnySlice(raw) {
			key, _ := item.(string)
			if name, known := names[key]; known && !slices.Contains(listed, name) {
				listed = append(listed, name)
			}
		}
		return listed
	}
	if config["groupSort"] == "manual" {
		if order := named(config["groupOrder"]); len(order) > 0 {
			fetched["groups"] = order
		}
	}
	if hidden := named(config["hiddenGroups"]); len(hidden) > 0 {
		fetched["hiddenGroups"] = hidden
	}
	switch {
	case viewType == graphViewTypeBoard && config["groupCountsHidden"] == true:
		fetched["groupCounts"] = false
	case viewType != graphViewTypeBoard && config["showGroupCounts"] == true:
		fetched["groupCounts"] = true
	}
	if viewType == graphViewTypeBoard && config["boardColumnColors"] == false {
		fetched["colorColumns"] = false
	}
}

// mcpGroupNames names a property's groups by their keys, as GROUPS takes
// them: its options, a status's groups too, and NO VALUE.
func mcpGroupNames(property notionMCPDataSourceProperty) map[string]string {
	names := map[string]string{mcpBoardNoValueGroup: "NO VALUE", mcpTableNoValueGroup: "NO VALUE"}
	for _, key := range []string{"options", "statusGroups"} {
		for _, raw := range mcpAnySlice(property.Config[key]) {
			entry, _ := raw.(map[string]any)
			id, _ := entry["id"].(string)
			name, _ := entry["name"].(string)
			if id != "" && name != "" {
				names[id] = name
			}
		}
	}
	return names
}

// mcpGalleryWrappedProperties are the properties a gallery wraps: every one
// it lists, the shown ones first, once WRAP PROPERTIES is on.
func mcpGalleryWrappedProperties(config map[string]any, shown []string, ids []string, byID map[string]notionMCPDataSourceProperty) []string {
	wrapped := []string{}
	if config["wrap"] != true {
		return wrapped
	}
	wrapped = append(wrapped, shown...)
	for _, id := range ids {
		property := byID[id]
		if mcpDatabasePropertyShown(property.Type) && !slices.Contains(wrapped, property.Name) {
			wrapped = append(wrapped, property.Name)
		}
	}
	return wrapped
}
