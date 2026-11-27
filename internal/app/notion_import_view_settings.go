package app

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
)

// A Notion view's configuration keeps each setting Leafen has a place for, as
// the view API reads it, pointed at the imported properties; a setting Leafen
// has no place for is reported unless it looks as Leafen shows every view.

// notionImportViewSettingDefaults are the values of settings Leafen has no
// place for that look the same as Leafen shows every view.
var notionImportViewSettingDefaults = map[string]any{
	"frozen_column_index": -1.0, "show_vertical_lines": true, "show_weekends": true, "card_layout": "list",
	"height": "medium", "submission_permissions": "none", "sort": "manual", "color_theme": "auto",
}

// notionImportMeaningful reports whether a setting says something beyond its
// absence.
func notionImportMeaningful(field string, value any) bool {
	if fallback, known := notionImportViewSettingDefaults[field]; known && value != nil {
		return value != fallback
	}
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case float64:
		return typed != 0
	case []any:
		return len(typed) != 0
	case map[string]any:
		return len(typed) != 0
	}
	return true
}

// notionViewTitleOnlyTypes are the views that, with no list of properties from
// the API, show only the title in Notion (seen 2026-11-04); a table shows them all.
var notionViewTitleOnlyTypes = []string{graphViewTypeBoard, graphViewTypeGallery, graphViewTypeList, graphViewTypeCalendar, graphViewTypeTimeline}

// titleOnlyProperties is the list of such a view: its title shown, and so the
// others hidden as a list leaves them. It names the title as Notion's list
// would, by its Notion ID ("title"), which property finds.
func (converter *notionImportViewConverter) titleOnlyProperties() []any {
	for _, property := range converter.scope.ordered {
		if property.OfficialType != "title" {
			continue
		}
		for notionID, id := range converter.notionIDs {
			if id == property.ID {
				return []any{map[string]any{"property_id": notionID, "visible": true}}
			}
		}
		return []any{map[string]any{"property_name": property.Name, "visible": true}}
	}
	return []any{}
}

// configuration splits an official view configuration into one request per
// setting the import keeps.
func (converter *notionImportViewConverter) configuration(
	viewType string, raw json.RawMessage, reasons notionImportViewReasons,
) []json.RawMessage {
	settings := map[string]any{}
	if len(raw) == 0 {
		return nil
	}
	if json.Unmarshal(raw, &settings) != nil || settings["type"] != nil && settings["type"] != viewType {
		reasons.add(notionImportIssueViewSettingOmitted)
		return nil
	}
	if _, listed := settings["properties"]; !listed && slices.Contains(notionViewTitleOnlyTypes, viewType) {
		settings["properties"] = converter.titleOnlyProperties()
	}
	fields := make([]string, 0, len(settings))
	for field := range settings {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	requests := []json.RawMessage{}
	for _, field := range fields {
		value, keep := converter.setting(viewType, field, settings[field], reasons)
		if keep {
			requests = append(requests, notionImportJSON(map[string]any{"type": viewType, field: value}))
		}
	}
	return requests
}

// setting reads one official configuration setting into the request the
// normalization takes, or reports why the import leaves it.
func (converter *notionImportViewConverter) setting(
	viewType, field string, value any, reasons notionImportViewReasons,
) (any, bool) {
	in := func(types ...string) bool { return slices.Contains(types, viewType) }
	switch {
	case field == "type" || strings.HasSuffix(field, "_property_name") || field == "map_by_property_name" ||
		field == "rows" && viewType == graphViewTypeDashboard:
		// Names repeat the property IDs; a dashboard's rows are laid out
		// once its widgets are known.
		return nil, false
	case field == "properties" && !in(graphViewTypeForm, graphViewTypeChart, graphViewTypeDashboard):
		return converter.viewProperties(value, reasons)
	case field == "table_properties" && in(graphViewTypeTimeline):
		return converter.timelineTableProperties(value, reasons)
	case (field == "group_by" || field == "sub_group_by") && notionViewTakesField(viewType, field):
		// As the API takes them: a list's, gallery's and timeline's group too.
		return converter.group(value, reasons, "")
	case field == "x_axis" && in(graphViewTypeChart), field == "stack_by" && in(graphViewTypeChart):
		// A chart's X axis can leave out its groups at zero; the groups it
		// stacks by keep their own order.
		return converter.group(value, reasons, field)
	case field == "cover" && in(graphViewTypeBoard, graphViewTypeGallery):
		return converter.cover(value, reasons)
	case field == "date_property_id" && in(graphViewTypeCalendar, graphViewTypeTimeline),
		field == "end_date_property_id" && in(graphViewTypeTimeline):
		return converter.propertySetting(value, reasons, notionViewDateProperty)
	case field == "map_by" && in(graphViewTypeMap):
		return converter.propertySetting(value, reasons, nil)
	case field == "arrows_by" && in(graphViewTypeTimeline):
		arrows, _ := value.(map[string]any)
		if arrows["property_id"] == nil {
			return nil, false
		}
		id, keep := converter.propertySetting(arrows["property_id"], reasons, func(property notionViewProperty) bool {
			return property.OfficialType == "relation"
		})
		return map[string]any{"property_id": id}, keep
	case field == "preference" && in(graphViewTypeTimeline):
		preference, _ := value.(map[string]any)
		zoom, _ := preference["zoom_level"].(string)
		if slices.Contains(notionViewTimelineZooms, zoom) {
			kept := map[string]any{"zoom_level": zoom}
			if center, ok := preference["center_timestamp"].(float64); ok {
				kept["center_timestamp"] = center
			}
			return kept, true
		}
		if zoom != "" {
			reasons.add(notionImportIssueViewSettingOmitted)
		}
		return nil, false
	case field == "chart_type" && in(graphViewTypeChart):
		chartType, _ := value.(string)
		if _, supported := notionViewChartTypes[chartType]; !supported {
			reasons.add(notionImportIssueViewSettingOmitted)
			return nil, false
		}
		return value, true
	case field == "frozen_column_index" && in(graphViewTypeTable) && notionImportNoFrozenColumn(value):
		return nil, false
	case field == "wrap_cells" && in(graphViewTypeTable), field == "view_range" && in(graphViewTypeCalendar),
		field == "show_table" && in(graphViewTypeTimeline), field == "show_weekends" && in(graphViewTypeCalendar),
		field == "color_by" && in(graphViewTypeTimeline),
		field == "height" && in(graphViewTypeMap),
		(field == "frozen_column_index" || field == "show_vertical_lines") && in(graphViewTypeTable),
		field == "card_layout" && in(graphViewTypeBoard, graphViewTypeGallery),
		(field == "color_theme" || field == "height" || field == "show_data_labels" || field == "color_by_value") && in(graphViewTypeChart),
		field != "stack_by" && notionViewChartFormatField(field) && in(graphViewTypeChart),
		(field == "cover_size" || field == "cover_aspect") && in(graphViewTypeBoard, graphViewTypeGallery):
		return value, value != nil
	case field == "is_form_closed" && in(graphViewTypeForm):
		// An imported form has no link yet, so it takes no responses; an
		// open form is reported.
		if value != true {
			reasons.add(notionImportIssueViewSettingOmitted)
		}
		return nil, false
	case field == "subtasks" && !in(graphViewTypeForm, graphViewTypeChart, graphViewTypeDashboard):
		return converter.subtasks(value, reasons)
	case (field == "y_axis" || field == "value") && in(graphViewTypeChart):
		axis, _ := value.(map[string]any)
		if axis == nil {
			return nil, false
		}
		result := map[string]any{"aggregator": axis["aggregator"]}
		if axis["property_id"] != nil {
			id, keep := converter.propertySetting(axis["property_id"], reasons, nil)
			if !keep {
				return nil, false
			}
			result["property_id"] = id
		}
		return result, true
	}
	if notionImportMeaningful(field, value) {
		reasons.add(notionImportIssueViewSettingOmitted)
	}
	return nil, false
}

// subtasks reads how a view shows sub-items, pointed at the imported
// relation and toggle column.
func (converter *notionImportViewConverter) subtasks(value any, reasons notionImportViewReasons) (any, bool) {
	subtasks, _ := value.(map[string]any)
	if subtasks == nil {
		return nil, false
	}
	// Each field as Notion's API keeps it, alone too (applyOfficialViewSubtasks).
	result := map[string]any{}
	for _, field := range []string{"display_mode", "filter_scope"} {
		if subtasks[field] != nil {
			result[field] = subtasks[field]
		}
	}
	for _, field := range []string{"property_id", "toggle_column_id"} {
		if subtasks[field] == nil {
			continue
		}
		id, keep := converter.propertySetting(subtasks[field], reasons, nil)
		if !keep {
			return nil, false
		}
		result[field] = id
	}
	return result, len(result) > 0
}

// notionImportNoFrozenColumn reads the -1 Notion gives a view that freezes none.
func notionImportNoFrozenColumn(value any) bool {
	number, isNumber := value.(float64)
	return isNumber && number < 0
}

// propertySetting points a setting at the imported property it names.
func (converter *notionImportViewConverter) propertySetting(
	value any, reasons notionImportViewReasons, fits func(notionViewProperty) bool,
) (any, bool) {
	if value == nil {
		return nil, false
	}
	property, found := converter.property(value)
	switch {
	case !found:
		reasons.add(notionImportIssueViewPropertyUnresolved)
		return nil, false
	case fits != nil && !fits(property):
		reasons.add(notionImportIssueViewSettingOmitted)
		return nil, false
	}
	return property.ID, true
}

// viewProperties keeps the shown properties, their order, widths and
// wrapping. A hidden property the import did not bring is reported apart:
// hiding it changes nothing. Notion's views keep settings of properties long
// gone from their database, shown or not; those were never anything to bring.
func (converter *notionImportViewConverter) viewProperties(value any, reasons notionImportViewReasons) (any, bool) {
	items, _ := value.([]any)
	kept, seen := []any{}, map[string]int{}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		property, found := converter.property(item["property_id"])
		if !found {
			property, found = converter.property(item["property_name"])
		}
		if !found {
			switch {
			case !converter.inSource(item["property_id"]) && !converter.inSource(item["property_name"]):
			case item["visible"] == false:
				reasons.add(notionImportIssueViewStaleHiddenProperty)
			default:
				reasons.add(notionImportIssueViewPropertyUnresolved)
			}
			continue
		}
		entry := map[string]any{"property_id": property.ID}
		for key, setting := range item {
			switch key {
			case "property_id", "property_name":
			case "visible":
				if visible, ok := setting.(bool); ok {
					entry[key] = visible
				}
			case "width":
				if width, ok := setting.(float64); ok && width >= 0 {
					entry[key] = width
				}
			case "wrap":
				if setting == true {
					entry[key] = true
				}
			case "status_show_as":
				// A status column shown as a checkbox comes as one; "select" is how it shows anyway.
				if setting == "checkbox" && property.NativeType == graphValueTypeStatus {
					entry[key] = setting
				} else if setting != "select" && notionImportMeaningful(key, setting) {
					reasons.add(notionImportIssueViewSettingOmitted)
				}
			default:
				if notionImportMeaningful(key, setting) {
					reasons.add(notionImportIssueViewSettingOmitted)
				}
			}
		}
		// Two Notion properties one column stands for — a relation gathered
		// into another — show it where the shown one was, as wide.
		if at, duplicate := seen[property.ID]; duplicate {
			if earlier, _ := kept[at].(map[string]any); earlier["visible"] != true && entry["visible"] == true {
				kept[at], seen[property.ID] = nil, len(kept)
				kept = append(kept, entry)
			}
			continue
		}
		seen[property.ID] = len(kept)
		kept = append(kept, entry)
	}
	kept = slices.DeleteFunc(kept, func(entry any) bool { return entry == nil })
	// A view that lists its properties shows only those it lists as visible
	// (seen 2026-11-04: a board and tables of one database); one added since
	// isn't in it. Each property once: the scope also finds the title as
	// "title", which is no property of its own.
	unlisted := []string{}
	for _, property := range converter.scope.ordered {
		if _, listed := seen[property.ID]; !listed && len(items) > 0 {
			unlisted = append(unlisted, property.ID)
		}
	}
	sort.Strings(unlisted)
	for _, id := range unlisted {
		kept = append(kept, map[string]any{"property_id": id, "visible": false})
	}
	return kept, len(kept) > 0
}

// timelineTableProperties keeps a timeline's table panel as viewProperties
// keeps a view's properties, its title among them: hidden or placed after
// another property as Notion's list has it (notion_view_timeline_table.go).
func (converter *notionImportViewConverter) timelineTableProperties(value any, reasons notionImportViewReasons) (any, bool) {
	return converter.viewProperties(value, reasons)
}

// group reads a group, sub-group or chart axis over one property. A table's
// or board's group keeps its unit, sort and whether it hides its empty
// groups; a chart's X axis and stacked groups keep a date's and a status's
// unit, their own sort and whether they hide their empty groups, as the
// views API takes them; other choices are reported. A rollup's group
// is a formula's, and a formula's keeps its group_by, as the views API takes them
// (notion_view_group_sort.go, notion_view_chart_axis_sort.go,
// notion_view_table_formula_group.go).
func (converter *notionImportViewConverter) group(value any, reasons notionImportViewReasons, chartAxis string) (any, bool) {
	if value == nil {
		return nil, false
	}
	group, _ := value.(map[string]any)
	property, found := converter.property(group["property_id"])
	if !found {
		reasons.add(notionImportIssueViewPropertyUnresolved)
		return nil, false
	}
	groupType, extra := notionViewGroupType(property.OfficialType)
	groupType = notionViewAxisGroupType(property.OfficialType)
	if groupType == "" || group["type"] != nil && group["type"] != groupType {
		reasons.add(notionImportIssueViewSettingOmitted)
		return nil, false
	}
	result := map[string]any{"type": groupType, "property_id": property.ID}
	if groupType == "formula" && chartAxis == "" {
		// A table's or board's keeps its group_by as given (notion_view_table_formula_group.go).
		nested, _, err := notionViewTableFormulaGroup("group_by", property, group)
		if err != nil {
			reasons.add(notionImportIssueViewSettingOmitted)
			return nil, false
		}
		result["group_by"] = nested
		if hide, ok := group["hide_empty_groups"].(bool); ok {
			result["hide_empty_groups"] = hide
		}
		return result, true
	}
	if groupType == "formula" {
		formula, err := normalizeNotionViewFormulaGroup(group["group_by"])
		if err != nil {
			reasons.add(notionImportIssueViewSettingOmitted)
			return nil, false
		}
		result["group_by"] = formula
		if group["hide_empty_groups"] == true {
			result["hide_empty_groups"] = true
		}
		if sorting, ok := group["sort"].(map[string]any); ok && chartAxis == "stack_by" {
			order, _ := sorting["type"].(string)
			if !slices.Contains(notionViewChartSeriesSorts, order) {
				reasons.add(notionImportIssueViewSettingOmitted)
				return nil, false
			}
			result["sort"] = map[string]any{"type": order}
		}
		return result, true
	}
	mode, _ := group["group_by"].(string)
	switch want, _ := extra["group_by"].(string); {
	case chartAxis == "" && want != "":
		// A table's or board's group names its unit, as the views API takes
		// it (notionViewGroupUnits): a status's groups or options, a date's
		// and a text's own; without one the API refuses the group.
		if mode == "" {
			mode = want
		}
		if !slices.Contains(notionViewGroupUnits[groupType], mode) {
			reasons.add(notionImportIssueViewSettingOmitted)
			mode = want
		}
		result["group_by"] = mode
	case chartAxis != "" && mode != "" && notionViewChartAxisKeepsUnit(property, mode):
		// A chart's X axis and stacked groups keep a date's and a status's
		// unit as the views API takes it (notionViewChartUnitKeys).
		result["group_by"] = mode
	case mode != "" && mode != want:
		reasons.add(notionImportIssueViewSettingOmitted)
	}
	// A date group's start_day_of_week comes over as given, whatever its
	// unit, which only the views API reads (notionViewGivenWeekStartKey).
	if start, given := group["start_day_of_week"].(float64); given && notionViewDateGroupType(groupType) && (start == 0 || start == 1) {
		result["start_day_of_week"] = start
	}
	sorting, _ := group["sort"].(map[string]any)
	sortType, _ := sorting["type"].(string)
	switch {
	case chartAxis != "" && slices.Contains(notionViewChartSeriesSorts, sortType):
		result["sort"] = map[string]any{"type": sortType}
	case chartAxis == "" && slices.Contains(notionViewGroupSorts, sortType):
		// A table's or board's groups come in the order the views API reads
		// its sort as (applyOfficialViewGrouping): by hand with no order of
		// the view's own, by name, the group for no value as "uncategorized"
		// — as Notion shows a board whose order its API doesn't give (seen
		// 2026-11-12: a select made Zeta, Alpha, Mid shows Alpha, Mid, Zeta,
		// no value).
		result["sort"] = map[string]any{"type": sortType}
	case sortType != "":
		reasons.add(notionImportIssueViewSettingOmitted)
	}
	for _, key := range []string{"hide_empty_groups", "range_start", "range_end", "range_size"} {
		if hide, told := group[key].(bool); key == "hide_empty_groups" && told && chartAxis == "" {
			result[key] = hide
			continue
		}
		if key == "hide_empty_groups" && chartAxis != "" {
			if group[key] == true {
				result[key] = true
			}
			continue
		}
		if notionImportMeaningful(key, group[key]) {
			reasons.add(notionImportIssueViewSettingOmitted)
		}
	}
	return result, true
}

// cover reads a card preview: the page cover, the page's content or a
// property.
func (converter *notionImportViewConverter) cover(value any, reasons notionImportViewReasons) (any, bool) {
	cover, _ := value.(map[string]any)
	switch coverType, _ := cover["type"].(string); coverType {
	case "":
		return nil, false
	case "page_cover", "page_content":
		return map[string]any{"type": coverType}, true
	case "property":
		id, keep := converter.propertySetting(cover["property_id"], reasons, nil)
		return map[string]any{"type": "property", "property_id": id}, keep
	}
	reasons.add(notionImportIssueViewSettingOmitted)
	return nil, false
}
