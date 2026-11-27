package app

import "slices"

// A timeline's table panel shows the properties chosen for it, apart from
// the properties its bars show; the view API reads them as table_properties.
// A property the settings do not list stays out of the panel. Its title hides
// and moves as any of them, as Notion's (seen 2026-11-11: "Owner | Name");
// settings that don't list it show it first, as the screen does
// (web/src/components/database/timelineTableModel.ts). Set through the API or
// the screen, the settings list every property the source has then, as
// Notion's panel keeps them, and the API reads those alone, not a property
// made since (seen 2026-11-12).

// officialTimelineTableProperties projects the table panel's properties: the
// listed ones in their order, the title first where they don't list it; with
// none listed, the title and then the rest hidden, by name
// (database_view_property_order.go).
func (scope notionViewFilterScope) officialTimelineTableProperties(config map[string]any) []any {
	listed := map[string]map[string]any{}
	ids := []string{}
	items, _ := config["timelineTableProperties"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		id, _ := item["id"].(string)
		if _, known := scope.byID[id]; known && !slices.Contains(ids, id) {
			ids = append(ids, id)
			listed[id] = item
		}
	}
	kept := len(ids) > 0
	for _, property := range scope.ordered {
		if property.OfficialType == "title" && !slices.Contains(ids, property.ID) {
			ids = append([]string{property.ID}, ids...)
			listed[property.ID] = map[string]any{"visible": true}
		}
	}
	for _, property := range scope.viewOrdered() {
		if !slices.Contains(ids, property.ID) && !kept {
			ids = append(ids, property.ID)
		}
	}
	properties := []any{}
	for _, id := range ids {
		entry := map[string]any{"property_id": scope.viewID(id), "property_name": scope.byID[id].Name, "visible": listed[id]["visible"] == true}
		if width, ok := listed[id]["width"].(float64); ok && width >= 0 {
			entry["width"] = int(width)
		}
		if listed[id]["wrap"] == true {
			entry["wrap"] = true
		}
		properties = append(properties, entry)
	}
	return properties
}

// applyOfficialTimelineTableProperties writes the table panel's properties,
// the title's among them; null clears them, and the title shows first again.
func (scope notionViewFilterScope) applyOfficialTimelineTableProperties(value any, config map[string]any) error {
	if value == nil {
		delete(config, "timelineTableProperties")
		return nil
	}
	items, ok := value.([]any)
	if !ok || !scope.propertyListFits(len(items)) {
		return notionViewFilterInvalid("the timeline table_properties are invalid")
	}
	entries := []any{}
	seen := map[string]bool{}
	for index, raw := range items {
		item, _ := raw.(map[string]any)
		id, err := scope.viewListedProperty(item, "table_properties", index)
		if err != nil {
			return err
		}
		visible, visibleOK := item["visible"].(bool)
		if item["visible"] != nil && !visibleOK {
			return notionViewFilterInvalid("the timeline table property %q is invalid", scope.viewID(id))
		}
		entry := map[string]any{"id": id, "visible": !visibleOK || visible}
		if width, ok := item["width"].(float64); ok && width >= 0 && width <= 2000 {
			entry["width"] = width
		} else if item["width"] != nil {
			return notionViewFilterInvalid("the timeline table property %q width is invalid", scope.viewID(id))
		}
		if wrap, ok := item["wrap"].(bool); ok && wrap {
			entry["wrap"] = true
		} else if item["wrap"] != nil && !ok {
			return notionViewFilterInvalid("the timeline table property %q wrap is invalid", scope.viewID(id))
		}
		// A property listed again keeps its first entry, as a view's
		// properties do (applyOfficialViewProperties; seen 2026-11-12: the
		// panel shows "Beta" listed shown and then hidden).
		if seen[id] {
			continue
		}
		seen[id] = true
		entries = append(entries, entry)
	}
	// A property the list leaves out follows the listed ones, hidden — the
	// title too — in the order the API lists the source's properties, as
	// Notion's panel reads back (seen 2026-11-12); one the reader can't see
	// stays as it was, after them.
	for _, property := range scope.publicOrdered() {
		if _, withheld := scope.withheld[property.ID]; !withheld && !seen[property.ID] {
			seen[property.ID] = true
			entries = append(entries, map[string]any{"id": property.ID, "visible": false})
		}
	}
	before, _ := config["timelineTableProperties"].([]any)
	for _, raw := range before {
		item, _ := raw.(map[string]any)
		id, _ := item["id"].(string)
		if _, withheld := scope.withheld[id]; withheld && !seen[id] {
			seen[id] = true
			entries = append(entries, item)
		}
	}
	config["timelineTableProperties"] = entries
	return nil
}
