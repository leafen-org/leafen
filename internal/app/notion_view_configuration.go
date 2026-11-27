package app

import (
	"encoding/json"
	"maps"
	"slices"
)

// Official view configuration maps onto the native view settings that carry
// the same meaning; fields without a native setting are refused on input and
// left out of responses.

var notionViewChartTypes = map[string]string{"column": "bar", "bar": "horizontal_bar", "line": "line", "donut": "donut", "number": "number"}

// notionViewChartColorThemes are the chart color themes Leafen draws: option
// colors, a colorful cycle, or one color — Notion's (teal among them, seen
// 2026-10-05) and brown, which charts made before kept.
var notionViewChartColorThemes = []string{"auto", "colorful", "gray", "brown", "orange", "yellow", "green", "blue", "purple", "teal", "pink", "red"}

// notionViewCardPreviews are the official card cover types a native board or
// gallery shows from the page itself rather than from a property.
var notionViewCardPreviews = map[string]string{"page_cover": "__page_cover", "page_content": "__page_content"}

func notionViewGroupType(officialType string) (string, map[string]any) {
	switch officialType {
	case "select", "multi_select", "relation", "checkbox", "title", "url", "email", "phone_number":
		if notionViewConditionFamily(officialType) == "text" {
			return officialType, map[string]any{"group_by": "exact"}
		}
		return officialType, nil
	case "rich_text":
		return "text", map[string]any{"group_by": "exact"}
	case "status":
		return "status", map[string]any{"group_by": "group"}
	case "people":
		return "person", nil
	case "created_by", "last_edited_by":
		return officialType, nil
	case "date", "created_time", "last_edited_time":
		return officialType, map[string]any{"group_by": "relative"}
	case "number":
		return "number", nil
	case "formula":
		return "formula", nil
	}
	return "", nil
}

func (scope notionViewFilterScope) officialViewGroupBy(propertyID string, config map[string]any, groupingKey ...string) any {
	property, found := scope.byID[propertyID]
	key := ""
	if len(groupingKey) > 0 {
		key = groupingKey[0]
	}
	groupType, extra := notionViewGroupType(property.OfficialType)
	if groupType = notionViewAxisGroupType(property.OfficialType); !found || groupType == "" {
		return nil
	}
	// A board's sub-groups keep their own settings; a chart's axes sort by their own.
	settings, sorting := config, config
	if len(groupingKey) > 0 {
		sorting = nil
		if groupingKey[0] == "subGroupBy" {
			settings, _ = config["subGroup"].(map[string]any)
			sorting = settings
		}
		if key == "chartGroupBy" {
			sorting = map[string]any{"groupSort": officialChartAxisSort(config)}
		}
	}
	// A table's or board's group given as another type reads back as given,
	// as a formula's too (notion_view_table_formula_group.go).
	asGiven := len(groupingKey) == 0 || groupingKey[0] == "subGroupBy"
	if group := scope.officialViewTableFormulaGroup(property, config, settings, key == "subGroupBy"); asGiven && group != nil {
		return group
	}
	if group := scope.officialViewChartGivenGroup(property, config, key); !asGiven && group != nil {
		return group
	}
	if groupType == "formula" && (!asGiven || officialViewGivenGroupType(settings, property.ID) == "") {
		return officialNotionViewFormulaGroup(property, scope.viewID(property.ID), config, key)
	}
	if typeName := officialViewGivenGroupType(settings, property.ID); asGiven && typeName != "" {
		groupType, extra = typeName, notionViewGroupExtra(typeName)
	}
	group := map[string]any{
		"type": groupType, "property_id": scope.viewID(property.ID), "property_name": property.Name,
		"sort": map[string]any{"type": officialViewGroupSort(groupType, sorting)},
	}
	for key, value := range extra {
		group[key] = value
	}
	if asGiven && notionViewTextGroupType(groupType) && settings["textGroupBy"] == "letter" {
		group["group_by"] = "alphabet_prefix"
	}
	if asGiven && groupType == "number" {
		withNumberRanges(group, settings)
	} else if groupType == "number" {
		withChartNumberRanges(group, config, key, property.ID)
	}
	// A chart's dates, along its X axis or stacked, group by month until
	// picked otherwise, as the screen draws them, and say so as Notion's do
	// (seen 2026-11-09, 2026-11-12).
	units, charted := notionViewChartUnitKeys[key]
	if notionViewConditionFamily(property.OfficialType) == "date" && charted {
		group["group_by"] = mcpChartDateBy
		if by, ok := config[units.date].(string); ok && slices.Contains(notionViewDateGroupings, by) {
			group["group_by"] = by
		}
		withGivenWeekStart(group, config, units.start)
	} else if notionViewDateGroupType(groupType) && (key == "subGroupBy" || config["groupBy"] == propertyID) {
		withDateUnit(group, config, settings, key == "subGroupBy")
	}
	// A status shows its options until shown by its groups, as Notion's (seen
	// 2026-10-24); a chart's X axis and stacked groups by their own "그룹 ·
	// 옵션" (seen 2026-11-12).
	byGroups := settings["groupByMode"] == "group"
	if charted {
		byGroups = config[units.status] == "group"
	}
	if groupType == "status" && !byGroups {
		group["group_by"] = "option"
	}
	return group
}

func notionViewStringSet(value any) map[string]struct{} {
	set := map[string]struct{}{}
	items, _ := value.([]any)
	for _, item := range items {
		if text, ok := item.(string); ok {
			set[text] = struct{}{}
		}
	}
	return set
}

// notionViewFrozenColumnIndex is the index of a table's last frozen column,
// as Notion's API and MCP give it (0 freezes the first column, -1 none), from
// how many columns the view freezes.
func notionViewFrozenColumnIndex(config map[string]any) int {
	switch count := config["frozenColumns"].(type) {
	case float64:
		return max(0, int(count)) - 1
	case int:
		return max(0, count) - 1
	}
	return -1
}

// Timeline zoom levels, as the official view API names them.
var notionViewTimelineZooms = []string{"hours", "day", "week", "bi_week", "month", "quarter", "year", "5_years"}

// A table grouped by a date groups its rows by how far the date is from
// today, or by the day, week, month or year it falls in.
var notionViewDateGroupings = []string{"relative", "day", "week", "month", "year"}

// notionViewHeights are the official view heights.
var notionViewHeights = []string{"small", "medium", "large", "extra_large"}

// officialViewConfiguration projects the native settings of one view type,
// a setting left unset at its default; the API gives what the view has set
// (notionViewGivenConfiguration).
func (scope notionViewFilterScope) officialViewConfiguration(viewType string, config map[string]any) map[string]any {
	configuration := map[string]any{"type": viewType}
	text := func(key string) (string, bool) {
		value, ok := config[key].(string)
		return value, ok && value != ""
	}
	withProperties := viewType != "form" && viewType != "chart" && viewType != "dashboard"
	if withProperties {
		configuration["properties"] = scope.officialViewProperties(viewType, config)
		if subtasks := scope.officialViewSubtasks(config); subtasks != nil && viewType == "table" {
			configuration["subtasks"] = subtasks
		}
	}
	if groupBy, ok := text("groupBy"); ok && (viewType == "table" || viewType == "board") {
		if group := scope.officialViewGroupBy(groupBy, config); group != nil {
			configuration["group_by"] = scope.withEmptyGroupsHidden(group, viewType, groupBy, config, false)
		}
	}
	switch viewType {
	case "table":
		configuration["wrap_cells"] = config["wrap"] == true
		configuration["frozen_column_index"] = notionViewFrozenColumnIndex(config)
		configuration["show_vertical_lines"] = config["showVerticalLines"] != false
	case "board", "gallery":
		if subGroup, ok := text("subGroupBy"); ok && viewType == "board" {
			if group := scope.officialViewGroupBy(subGroup, config, "subGroupBy"); group != nil {
				settings, _ := config["subGroup"].(map[string]any)
				configuration["sub_group_by"] = scope.withEmptyGroupsHidden(group, viewType, subGroup, settings, true)
			}
		}
		if cover, ok := text("coverProperty"); ok {
			if scope.lists(cover) {
				configuration["cover"] = map[string]any{"type": "property", "property_id": scope.viewID(cover)}
			}
			for official, native := range notionViewCardPreviews {
				if cover == native {
					configuration["cover"] = map[string]any{"type": official}
				}
			}
			if cover == "__none" {
				delete(configuration, "cover")
			}
		}
		if size, ok := text("cardSize"); ok {
			configuration["cover_size"] = size
		}
		configuration["cover_aspect"] = map[bool]string{true: "contain", false: "cover"}[config["fitImage"] == true]
		configuration["card_layout"] = "list"
		if config["cardLayout"] == "compact" {
			configuration["card_layout"] = "compact"
		}
	case "calendar":
		if property, ok := text("calendarBy"); ok && scope.lists(property) {
			configuration["date_property_id"], configuration["date_property_name"] = scope.viewID(property), scope.byID[property].Name
		}
		if layout, ok := text("calendarLayout"); ok {
			configuration["view_range"] = layout
		}
		configuration["show_weekends"] = config["showWeekends"] != false
	case "timeline":
		if property, ok := text("timelineBy"); ok && scope.lists(property) {
			configuration["date_property_id"], configuration["date_property_name"] = scope.viewID(property), scope.byID[property].Name
		}
		if property, ok := text("timelineEndBy"); ok && scope.lists(property) {
			configuration["end_date_property_id"], configuration["end_date_property_name"] = scope.viewID(property), scope.byID[property].Name
		}
		configuration["show_table"] = config["timelineShowTable"] == true
		configuration["table_properties"] = scope.officialTimelineTableProperties(config)
		configuration["color_by"] = config["timelineColorBy"] == true
		if zoom, ok := text("timelineZoom"); ok {
			center, _ := config["timelineCenter"].(float64)
			configuration["preference"] = map[string]any{"zoom_level": zoom, "center_timestamp": center}
		}
		if property, ok := text("dependencyProperty"); ok && scope.lists(property) {
			configuration["arrows_by"] = map[string]any{"property_id": scope.viewID(property)}
		}
	case "map":
		if property, ok := text("mapBy"); ok && scope.lists(property) {
			configuration["map_by"], configuration["map_by_property_name"] = scope.viewID(property), scope.byID[property].Name
		}
		configuration["height"] = "medium"
		if height, ok := text("mapHeight"); ok {
			configuration["height"] = height
		}
	case "chart":
		configuration["chart_type"] = officialChartType(config)
		axisKey := "y_axis"
		if config["chartType"] == "number" {
			// A number chart shows one aggregate, its value: the count of its
			// rows until another is chosen, as Notion's gives it.
			axisKey = "value"
			configuration["value"] = map[string]any{"aggregator": "count"}
		} else if property, ok := text("chartGroupBy"); ok {
			if group := scope.officialViewGroupBy(property, config, "chartGroupBy"); group != nil {
				configuration["x_axis"] = group
			}
		}
		if aggregator, ok := text("chartAggregate"); ok {
			axis := map[string]any{"aggregator": aggregator}
			if property, ok := text("chartAggregateBy"); ok && scope.lists(property) {
				axis["property_id"] = scope.viewID(property)
			}
			configuration[axisKey] = axis
		}
		configuration["color_theme"] = "auto"
		if theme, ok := text("chartColorTheme"); ok {
			configuration["color_theme"] = theme
		}
		configuration["height"] = "medium"
		if height, ok := text("chartHeight"); ok {
			configuration["height"] = height
		}
		// Data labels show unless turned off, as a new Notion chart's do.
		configuration["show_data_labels"] = config["chartShowDataLabels"] != false
		configuration["color_by_value"] = config["chartColorByValue"] == true
		chartType, _ := config["chartType"].(string)
		if chartType == "" {
			chartType = "bar"
		}
		scope.officialViewChartFormat(chartType, config, configuration)
	case "dashboard":
		configuration["rows"] = []any{}
	}
	return configuration
}

// lists is whether the scope names a property: one the reader can't see, or
// one since deleted, leaves the view's configuration.
func (scope notionViewFilterScope) lists(propertyID string) bool {
	_, found := scope.byID[propertyID]
	return found
}

func notionViewDateProperty(property notionViewProperty) bool {
	return notionViewConditionFamily(property.OfficialType) == "date"
}

// applyOfficialViewConfiguration writes the supported official configuration
// fields into a native view config. A field the view's type has no use for —
// another type's, or one no type has — is taken and left out, as Notion's
// API takes it (seen 2026-11-12: a gallery's wrap_properties, a table's
// cover, a map's properties, any type's foo_bar answer 200 and read back
// without it); so are fields its objects don't have, as a property's,
// cover's or zoom preference's.
func (scope notionViewFilterScope) applyOfficialViewConfiguration(viewType string, raw json.RawMessage, config map[string]any) error {
	if len(raw) == 0 {
		return nil
	}
	input := map[string]any{}
	if json.Unmarshal(raw, &input) != nil {
		return notionViewFilterInvalid("the view configuration must be an object")
	}
	if input["type"] != viewType {
		return notionViewFilterInvalid("A \"%v\" configuration doesn't fit a \"%s\" view.", input["type"], viewType)
	}
	set := func(key string, value any, clear bool) {
		if clear {
			delete(config, key)
		} else {
			config[key] = value
		}
	}
	// A group goes before the fields beside it: a chart's own
	// hide_empty_groups says it over its x_axis's.
	fields := slices.Sorted(maps.Keys(input))
	slices.SortStableFunc(fields, func(left, right string) int {
		return notionViewFieldStage(left) - notionViewFieldStage(right)
	})
	for _, field := range fields {
		value := input[field]
		if !notionViewTakesField(viewType, field) {
			continue
		}
		if value == nil && applyOfficialViewNull(viewType, field, config) {
			continue
		}
		var err error
		switch {
		case field == "type":
		case field == "properties" && viewType != "form" && viewType != "chart" && viewType != "map":
			err = scope.applyOfficialViewProperties(value, config)
		case field == "group_by" && (viewType == "table" || viewType == "board") || field == "sub_group_by" && viewType == "board" ||
			field == "x_axis" && viewType == "chart":
			key := map[string]string{"group_by": "groupBy", "sub_group_by": "subGroupBy", "x_axis": "chartGroupBy"}[field]
			err = scope.applyOfficialViewGrouping(viewType, key, value, config)
		case field == "wrap_cells" && viewType == "table":
			flag, ok := value.(bool)
			err = notionViewFieldCheck(ok, field)
			set("wrap", true, !flag)
		case (field == "cover_size" || field == "cover_aspect" || field == "cover") && (viewType == "board" || viewType == "gallery"):
			err = applyOfficialViewCover(scope, field, value, config)
		case (field == "date_property_id" && (viewType == "calendar" || viewType == "timeline")) ||
			(field == "end_date_property_id" && viewType == "timeline"):
			key := map[string]string{"calendar": "calendarBy", "timeline": "timelineBy"}[viewType]
			if field == "end_date_property_id" {
				key = "timelineEndBy"
			}
			var id string
			if id, err = scope.viewDateReference(field, viewType, value); err == nil {
				config[key] = id
			}
		case field == "view_range" && viewType == "calendar":
			err = notionViewFieldCheck(value == "week" || value == "month", field)
			config["calendarLayout"] = value
		case field == "show_table" && viewType == "timeline":
			flag, ok := value.(bool)
			err = notionViewFieldCheck(ok, field)
			config["timelineShowTable"] = flag
		case field == "color_by" && viewType == "timeline":
			err = applyOfficialTimelineColorBy(value, config)
		case field == "table_properties" && viewType == "timeline":
			err = scope.applyOfficialTimelineTableProperties(value, config)
		case field == "preference" && viewType == "timeline":
			// The zoom, and where the timeline was centered (a time in
			// milliseconds, 0 unless given), as Notion's keeps them.
			preference, _ := value.(map[string]any)
			zoom, _ := preference["zoom_level"].(string)
			err = notionViewFieldCheck(slices.Contains(notionViewTimelineZooms, zoom), field)
			config["timelineZoom"] = zoom
			center, given := preference["center_timestamp"].(float64)
			set("timelineCenter", center, !given)
		case field == "frozen_column_index" && viewType == "table":
			// The index of the last column it freezes, as Notion's: 0 is the
			// first column; Notion takes no -1 (seen 2026-11-10), and any
			// count past it (seen 2026-11-12: 150).
			index, ok := value.(float64)
			err = notionViewFieldCheck(ok && index >= 0 && index == float64(int64(index)), field)
			set("frozenColumns", int(index)+1, false)
		case field == "show_vertical_lines" && viewType == "table":
			flag, ok := value.(bool)
			err = notionViewFieldCheck(ok, field)
			set("showVerticalLines", false, flag)
		case field == "card_layout" && (viewType == "board" || viewType == "gallery"):
			err = notionViewFieldCheck(value == "list" || value == "compact", field)
			set("cardLayout", "compact", value != "compact")
		case field == "show_weekends" && viewType == "calendar":
			flag, ok := value.(bool)
			err = notionViewFieldCheck(ok, field)
			set("showWeekends", false, flag)
		case field == "height" && viewType == "map":
			height, _ := value.(string)
			err = notionViewFieldCheck(slices.Contains(notionViewHeights, height), field)
			set("mapHeight", height, height == "medium")
		case field == "arrows_by" && viewType == "timeline":
			arrows, _ := value.(map[string]any)
			if arrows["property_id"] == nil {
				delete(config, "dependencyProperty")
				break
			}
			var id string
			if id, err = scope.viewPropertyReference(arrows["property_id"], func(p notionViewProperty) bool { return p.OfficialType == "relation" }); err == nil {
				config["dependencyProperty"] = id
			}
		case field == "map_by" && viewType == "map":
			var id string
			if id, err = scope.viewPropertyReference(value, nil); err == nil {
				config["mapBy"] = id
			}
		case field == "chart_type" && viewType == "chart":
			chartType, _ := value.(string)
			native, ok := notionViewChartTypes[chartType]
			err = notionViewFieldCheck(ok, field)
			config["chartType"] = native
		case (field == "y_axis" || field == "value") && viewType == "chart":
			if notionChartCountsBy(field, input) {
				err = scope.applyOfficialViewChartAxis(value, config)
			}
		case field == "color_theme" && viewType == "chart":
			theme, _ := value.(string)
			err = notionViewFieldCheck(slices.Contains(notionViewChartColorThemes, theme), field)
			set("chartColorTheme", theme, theme == "auto")
		case field == "height" && viewType == "chart":
			height, _ := value.(string)
			err = notionViewFieldCheck(slices.Contains(notionViewHeights, height), field)
			set("chartHeight", height, height == "medium")
		case field == "show_data_labels" && viewType == "chart":
			flag, ok := value.(bool)
			err = notionViewFieldCheck(ok, field)
			set("chartShowDataLabels", false, flag)
		case field == "color_by_value" && viewType == "chart":
			flag, ok := value.(bool)
			err = notionViewFieldCheck(ok, field)
			set("chartColorByValue", true, !flag)
		case viewType == "chart" && notionViewChartFormatField(field):
			_, err = scope.applyOfficialViewChartFormat(field, value, config)
		case field == "subtasks" && viewType == "table":
			err = scope.applyOfficialViewSubtasks(value, config)
		default:
			err = notionViewFilterInvalid("the %s view configuration field %q is not supported", viewType, field)
		}
		if err != nil {
			return err
		}
	}
	if viewType == graphViewTypeChart {
		applyOfficialChartAxisGroupHiding(input, config)
	}
	return nil
}

// notionViewTakesField is whether a view of the type takes the field of an
// API configuration (applyOfficialViewConfiguration); Notion's API leaves
// out every other. Only a table keeps how it shows sub-items (seen
// 2026-11-12: a board's subtasks read back without them, its sub-items
// shown on the screen too), and a map no property list.
func notionViewTakesField(viewType, field string) bool {
	in := func(types ...string) bool { return slices.Contains(types, viewType) }
	switch field {
	case "type":
		return true
	case "properties":
		return !in(graphViewTypeForm, graphViewTypeChart, graphViewTypeMap, graphViewTypeDashboard)
	case "group_by":
		return in(graphViewTypeTable, graphViewTypeBoard)
	case "sub_group_by":
		return in(graphViewTypeBoard)
	case "wrap_cells", "frozen_column_index", "show_vertical_lines", "subtasks":
		return in(graphViewTypeTable)
	case "cover", "cover_size", "cover_aspect", "card_layout":
		return in(graphViewTypeBoard, graphViewTypeGallery)
	case "date_property_id":
		return in(graphViewTypeCalendar, graphViewTypeTimeline)
	case "view_range", "show_weekends":
		return in(graphViewTypeCalendar)
	case "end_date_property_id", "show_table", "color_by", "table_properties", "preference", "arrows_by":
		return in(graphViewTypeTimeline)
	case "map_by":
		return in(graphViewTypeMap)
	case "height":
		return in(graphViewTypeMap, graphViewTypeChart)
	case "is_form_closed", "anonymous_submissions", "submission_permissions":
		return in(graphViewTypeForm)
	case "x_axis", "chart_type", "y_axis", "value", "color_theme", "show_data_labels", "color_by_value":
		return in(graphViewTypeChart)
	}
	return viewType == graphViewTypeChart && notionViewChartFormatField(field)
}

// notionViewFieldStage is 0 for a field that groups a view, 1 for any other.
func notionViewFieldStage(field string) int {
	if slices.Contains([]string{"group_by", "sub_group_by", "x_axis", "stack_by"}, field) {
		return 0
	}
	return 1
}

func notionViewFieldCheck(ok bool, field string) error {
	if ok {
		return nil
	}
	return notionViewFilterInvalid("the view configuration field %q is invalid", field)
}

func (scope notionViewFilterScope) applyOfficialViewGroup(key string, value any, config map[string]any) error {
	if value == nil {
		delete(config, key)
		delete(config, notionViewFormulaGroupKey(key))
		return nil
	}
	group, _ := value.(map[string]any)
	id, err := scope.viewGroupReference(group["property_id"])
	if err != nil {
		return err
	}
	groupType := notionViewAxisGroupType(scope.byID[id].OfficialType)
	// A table's and a board's groups take any of their types (notionViewGroupShapeRefusal), and a chart's
	// too, which it draws as its property's own (notion_view_chart_given_group.go).
	given := key == "groupBy" || key == "subGroupBy"
	if groupType == "" {
		return notionViewFilterInvalid("the view group must use the %q property type", groupType)
	}
	hiding := group
	if !given {
		otherType, err := scope.applyOfficialChartGivenGroup(key, id, group, config)
		if err != nil {
			return err
		}
		if otherType {
			group = map[string]any{}
		}
	}
	if refusal := officialViewGroupShapeRefusal(key, group); given && refusal != "" {
		return notionViewFilterInvalid("%s", refusal)
	}
	formulaKey := notionViewFormulaGroupKey(key)
	if given && group["type"] == "formula" {
		// The screen groups by the family it names, as by a group of that type (notion_view_table_formula_group.go).
		field := map[string]string{"groupBy": "group_by", "subGroupBy": "sub_group_by"}[key]
		if _, group, err = notionViewTableFormulaGroup(field, scope.byID[id], group); err != nil {
			return err
		}
		if group["type"] != "date" && key == "groupBy" {
			delete(config, "dateGroupBy")
			delete(config, "startDayOfWeek")
		}
	} else if given {
		group = notionViewRollupUnits(scope.byID[id], group)
	} else if groupType == "formula" && !given && group["type"] == "formula" {
		formula, err := normalizeNotionViewFormulaGroup(group["group_by"])
		if err != nil {
			return err
		}
		config[formulaKey] = formula
		// A date result's axis or stacked groups go by its unit, as a date
		// property's (Notion's, seen 2026-11-12: a formula's dates stacked by
		// week); a rollup's axis by how near today whatever unit it was given,
		// as Notion's chart draws it (seen 2026-11-12), until one is picked on
		// the screen.
		if by, _ := formula["group_by"].(string); formula["type"] == "date" {
			units := notionViewChartUnitKeys[key]
			config[units.date] = by
			if scope.byID[id].OfficialType == "rollup" {
				delete(config, units.date)
			}
		}
	} else if formulaKey != "" {
		delete(config, formulaKey)
	}
	if key == "chartGroupBy" && config[key] != id {
		delete(config, "chartGroupOrder")
		delete(config, "chartHiddenGroups")
	}
	units, charted := notionViewChartUnitKeys[key]
	// A chart's dates group by the unit given, along its X axis or stacked.
	if by, given := group["group_by"].(string); charted && given && notionViewDateProperty(scope.byID[id]) {
		if !slices.Contains(notionViewDateGroupings, by) {
			return notionViewFilterInvalid("a date group is relative, day, week, month or year")
		}
		config[units.date] = by
	}
	config[key] = id
	if charted {
		applyOfficialChartGroupHiding(key, hiding, config)
		keepGivenWeekStart(hiding, config, units.start)
		// A status by its groups or options, as given.
		delete(config, units.status)
		if groupType == "status" && group["group_by"] == "group" {
			config[units.status] = "group"
		}
	}
	if key == "groupBy" {
		applyOfficialViewGroupUnit(groupType, group, config)
		keepGivenWeekStart(group, config, notionViewGivenWeekStartKey)
	}
	if typeName, _ := group["type"].(string); notionViewDateGroupType(typeName) && key == "groupBy" {
		by, _ := group["group_by"].(string)
		if by == "" {
			by = "relative"
		}
		start, startSet := group["start_day_of_week"]
		startValue, startOK := start.(float64)
		if !slices.Contains(notionViewDateGroupings, by) || startSet && !(startOK && (startValue == 0 || startValue == 1)) {
			return notionViewFilterInvalid("a date group is relative, day, week, month or year, with the week starting on 0 or 1")
		}
		delete(config, "dateGroupBy")
		if by != "relative" {
			config["dateGroupBy"] = by
		}
	}
	return nil
}

func (scope notionViewFilterScope) applyOfficialViewProperties(value any, config map[string]any) error {
	if value == nil {
		for _, key := range []string{"propertyOrder", "hiddenProperties", "visibleProperties", "propertyWidths", "wrappedColumns", "statusCheckboxColumns"} {
			delete(config, key)
		}
		return nil
	}
	items, ok := value.([]any)
	if !ok || !scope.propertyListFits(len(items)) {
		return notionViewFilterInvalid("the view configuration properties are invalid")
	}
	order, hidden, wrapped, checkboxes := []any{}, []any{}, []any{}, []any{}
	widths, _ := config["propertyWidths"].(map[string]any)
	if widths == nil {
		widths = map[string]any{}
	}
	for index, raw := range items {
		item, _ := raw.(map[string]any)
		id, err := scope.viewListedProperty(item, "properties", index)
		if err != nil {
			return err
		}
		show, showGiven := item["status_show_as"]
		if showGiven && (scope.byID[id].NativeType != graphValueTypeStatus || (show != "checkbox" && show != "select")) {
			return notionViewFilterInvalid("status_show_as is \"select\" or \"checkbox\" on a status property")
		}
		// A property listed again keeps its first entry, as Notion's screen
		// shows it: its view takes the list and echoes the repeat, but its
		// table and timeline show a property listed shown and then hidden, at
		// the first entry's width (seen 2026-11-12: "Beta" 120 shown, then
		// 300 hidden).
		if slices.Contains(order, any(id)) {
			continue
		}
		order = append(order, id)
		if item["visible"] == false {
			hidden = append(hidden, id)
		}
		if item["wrap"] == true {
			wrapped = append(wrapped, id)
		}
		if width, ok := item["width"].(float64); ok && width >= 0 {
			widths[id] = width
		}
		if show == "checkbox" {
			checkboxes = append(checkboxes, id)
		}
	}
	// A property the list leaves out follows the listed ones, hidden — the
	// title too — in the order the API lists the source's properties, as
	// Notion's view reads back (seen 2026-11-09; 2026-11-12, made or changed).
	for _, property := range scope.publicOrdered() {
		if _, withheld := scope.withheld[property.ID]; withheld || slices.Contains(order, any(property.ID)) {
			continue
		}
		order = append(order, property.ID)
		hidden = append(hidden, property.ID)
	}
	// A property the reader can't see stays as it showed, after the others.
	before, hiddenBefore := config["propertyOrder"], notionViewStringSet(config["hiddenProperties"])
	wrappedBefore, checkboxesBefore := notionViewStringSet(config["wrappedColumns"]), notionViewStringSet(config["statusCheckboxColumns"])
	previous, _ := before.([]any)
	for _, id := range slices.Sorted(maps.Keys(scope.withheld)) {
		previous = append(previous, id)
	}
	for _, raw := range previous {
		id, _ := raw.(string)
		if _, withheld := scope.withheld[id]; !withheld || slices.Contains(order, any(id)) {
			continue
		}
		order = append(order, id)
		if _, was := hiddenBefore[id]; was {
			hidden = append(hidden, id)
		}
		if _, was := wrappedBefore[id]; was {
			wrapped = append(wrapped, id)
		}
		if _, was := checkboxesBefore[id]; was {
			checkboxes = append(checkboxes, id)
		}
	}
	config["propertyOrder"], config["hiddenProperties"], config["wrappedColumns"] = order, hidden, wrapped
	config["statusCheckboxColumns"] = checkboxes
	if len(checkboxes) == 0 {
		delete(config, "statusCheckboxColumns")
	}
	config["propertyWidths"] = widths
	delete(config, "visibleProperties")
	return nil
}

func applyOfficialViewCover(scope notionViewFilterScope, field string, value any, config map[string]any) error {
	switch field {
	case "cover_size":
		if value != "small" && value != "medium" && value != "large" {
			return notionViewFieldCheck(false, field)
		}
		config["cardSize"] = value
	case "cover_aspect":
		if value != "contain" && value != "cover" {
			return notionViewFieldCheck(false, field)
		}
		config["fitImage"] = value == "contain"
	case "cover":
		cover, _ := value.(map[string]any)
		coverType, _ := cover["type"].(string)
		// A property cover naming no property shows the page's content, as
		// Notion's API reads it back (seen 2026-11-12: a board's and a
		// gallery's {type: property}, and one with property_id "").
		if id, _ := cover["property_id"].(string); coverType == "property" && id == "" {
			coverType = "page_content"
		}
		if native, page := notionViewCardPreviews[coverType]; page {
			config["coverProperty"] = native
			return nil
		}
		if coverType != "property" {
			return notionViewFilterInvalid("only page_cover, page_content and property covers are supported")
		}
		id, err := scope.viewPropertyReference(cover["property_id"], nil)
		if err != nil {
			return err
		}
		config["coverProperty"] = id
	}
	return nil
}
