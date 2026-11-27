package app

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// A view's chart and filter read over MCP as Notion's fetch gives them (seen
// 2026-11-09): chartConfig, Notion's own chart record, and advancedFilter, its
// filter record, each naming properties by name with their kind. The chart
// words here are the ones configure's CHART clauses set through the API's
// configuration (mcp_view_configure_display.go), so what fetch shows an agent
// is what it can write back.

// mcpChartFormatNames are a chart's format settings in chartFormat: the
// native setting and the name Notion's record gives it. A setting at its
// default stays out, as Notion's record leaves it.
var mcpChartFormatNames = []struct{ native, fetch string }{
	{"chartHeight", "height"}, {"chartColorTheme", "colorTheme"}, {"chartLegendPosition", "legendPosition"},
	{"chartAxisLabels", "axisLabel"}, {"chartGridLines", "axisGridLine"}, {"chartGroupStyle", "axisGroupStyle"},
	{"chartDonutLabels", "donutDataLabels"}, {"chartYMin", "yAxisMin"}, {"chartYMax", "yAxisMax"},
	{"chartSmoothLine", "smoothLine"}, {"chartCumulative", "axisCumulative"}, {"chartHideLineFill", "hideLineFillArea"},
	{"chartHideTitle", "hideTitle"}, {"chartColorByValue", "weightColorValue"},
	{"chartHideCenterValue", "donutHideValue"}, {"chartHideNegative", "hideNegativeValues"},
	{"chartTextColor", "numberColor"},
}

// mcpChartRuleOperators are a number chart's color rules' comparisons as
// Notion's record names them (greater_than seen 2026-11-09).
var mcpChartRuleOperators = map[string]string{
	">": "greater_than", ">=": "greater_than_or_equal_to", "<": "less_than", "<=": "less_than_or_equal_to",
	"=": "equals", "!=": "does_not_equal",
}

// mcpChartDateBy is how a chart's date axis groups its days when it was never
// picked: by month, as the screen draws it (chartViewSettings).
const mcpChartDateBy = "month"

// mcpViewChartConfig is a chart view's chartConfig: its type, what it groups
// and counts, and its format.
func mcpViewChartConfig(config map[string]any, byID map[string]notionMCPDataSourceProperty) map[string]any {
	chartType := "column"
	for official, native := range notionViewChartTypes {
		if config["chartType"] == native {
			chartType = official
		}
	}
	aggregation := map[string]any{"aggregator": "count"}
	if aggregator, ok := config["chartAggregate"].(string); ok && aggregator != "" {
		aggregation["aggregator"] = aggregator
	}
	if id, _ := config["chartAggregateBy"].(string); byID[id].Name != "" {
		aggregation["property"] = byID[id].Name
	}
	aggregationConfig := map[string]any{"aggregation": aggregation}
	data := map[string]any{"type": "number_reducer", "aggregationConfig": aggregationConfig}
	if chartType != "number" {
		data["type"] = "groups_reducer"
		if chartType != "donut" {
			aggregationConfig["seriesFormat"] = map[string]any{"displayType": chartType}
		}
		if group := mcpChartGroupBy(config, "chartGroupBy", byID); group != nil {
			group["hideEmptyGroups"] = notionChartAxisHidesEmptyGroups(config)
			data["groupBy"] = group
		}
		if group := mcpChartGroupBy(config, "chartStackBy", byID); group != nil {
			order, _ := config["chartStackSort"].(string)
			if !slices.Contains(notionViewChartSeriesSorts, order) {
				order = "ascending"
			}
			group["sort"], group["hideEmptyGroups"] = map[string]any{"type": order}, false
			aggregationConfig["stackOptions"] = map[string]any{"groupBy": group}
		}
	}
	chart := map[string]any{"type": chartType, "dataConfig": data}
	if format := mcpChartFormat(config); len(format) > 0 {
		chart["chartFormat"] = format
	}
	return chart
}

// mcpChartGroupBy is what a chart groups or stacks by: the property, its
// kind, its groups' order — an X axis's own, as the views API reads it back
// (officialChartAxisSort) — and how a date groups its days, along the X axis
// or stacked.
func mcpChartGroupBy(config map[string]any, key string, byID map[string]notionMCPDataSourceProperty) map[string]any {
	id, _ := config[key].(string)
	property, found := byID[id]
	if !found {
		return nil
	}
	sort := "manual"
	if axis := officialChartAxisSort(config); key == "chartGroupBy" && property.Type != graphValueTypeRollup && axis != "" {
		sort = axis
	}
	sort = notionViewAxisSort(property.Type, sort)
	group := map[string]any{
		"property": property.Name, "propertyType": mcpNotionPropertyType(property.Type), "sort": map[string]any{"type": sort},
	}
	// Its dates by their unit, along the X axis or stacked (notionViewChartUnitKeys).
	dated := property.Type == graphValueTypeDate || property.Type == graphValueTypeCreatedTime || property.Type == graphValueTypeLastEditedTime
	if units, charted := notionViewChartUnitKeys[key]; dated && charted {
		by, _ := config[units.date].(string)
		if !slices.Contains(notionViewDateGroupings, by) {
			by = mcpChartDateBy
		}
		group["groupBy"] = by
	}
	return group
}

// mcpChartFormat is a chart's format as Notion's record names it.
func mcpChartFormat(config map[string]any) map[string]any {
	format := map[string]any{}
	for _, name := range mcpChartFormatNames {
		switch value := config[name.native].(type) {
		case string:
			if value != "" {
				format[name.fetch] = value
			}
		case bool:
			if value {
				format[name.fetch] = true
			}
		case float64:
			format[name.fetch] = value
		}
	}
	if sort, _ := config["chartSort"].(string); sort != "" && sort != "manual" {
		format["mainSort"] = strings.ReplaceAll(sort, "_", "-")
	}
	if caption, _ := config["chartCaption"].(string); caption != "" && config["chartShowCaption"] == true {
		format["caption"], format["showCaption"] = caption, true
	}
	if config["chartShowDataLabels"] == false {
		format["axisShowDataLabels"] = false
	}
	if format["numberColor"] == "default" {
		delete(format, "numberColor")
	}
	// A number chart's title and a donut's center label are one setting in Notion's record.
	for _, key := range []string{"chartTitle", "chartCenterLabel"} {
		if label, _ := config[key].(string); label != "" && (key == "chartTitle") == (config["chartType"] == "number") {
			format["valueLabel"] = label
		}
	}
	if config["chartHideCenterLabel"] == true {
		format["showValueLabel"] = false
	}
	if number, _ := config["chartNumberFormat"].(string); number != "" {
		if number == "comma" {
			number = "number_with_commas"
		}
		format["numberFormatOverride"] = number
	}
	if decimals, ok := config["chartDecimals"].(float64); ok {
		format["numberPrecisionOverride"] = "precision_" + strconv.Itoa(int(decimals))
	}
	if rules, _ := config["chartColorRules"].([]any); config["chartDynamicColor"] == true && len(rules) > 0 {
		named := make([]any, 0, len(rules))
		for _, raw := range rules {
			rule, _ := raw.(map[string]any)
			operator, _ := rule["operator"].(string)
			named = append(named, map[string]any{"operator": mcpChartRuleOperators[operator], "value": rule["value"], "color": rule["color"]})
		}
		format["numberConditionalColor"] = map[string]any{"rules": named}
	}
	if lines := officialViewChartReferenceLines(config["chartReferenceLines"]); len(lines) > 0 {
		named := make([]any, 0, len(lines))
		for _, raw := range lines {
			line, _ := raw.(map[string]any)
			dash, _ := line["dash_style"].(string)
			if dash != "" {
				dash = strings.ToUpper(dash[:1]) + dash[1:]
			}
			named = append(named, map[string]any{
				"value": line["value"], "label": line["label"], "color": line["color"], "dashStyle": dash,
			})
		}
		format["referenceLines"] = named
	}
	return format
}

// mcpViewAdvancedFilter is a view's filter as Notion's record gives it: a
// group of property conditions and groups, in the view's order.
func mcpViewAdvancedFilter(raw any, byID map[string]notionMCPDataSourceProperty) map[string]any {
	encoded, err := json.Marshal(raw)
	group := databaseQueryFilterGroup{}
	if err != nil || json.Unmarshal(encoded, &group) != nil {
		return nil
	}
	return mcpFilterGroupTerm(group, byID)
}

func mcpFilterGroupTerm(group databaseQueryFilterGroup, byID map[string]notionMCPDataSourceProperty) map[string]any {
	terms := []any{}
	for _, term := range databaseQueryFilterTerms(&group) {
		if term.group {
			terms = append(terms, mcpFilterGroupTerm(group.Groups[term.index], byID))
			continue
		}
		if condition := mcpFilterCondition(group.Filters[term.index], byID); condition != nil {
			terms = append(terms, condition)
		}
	}
	conjunction := group.Conjunction
	if conjunction != databaseQueryConjunctionOr {
		conjunction = databaseQueryConjunctionAnd
	}
	return map[string]any{"type": "group", "operator": conjunction, "filters": terms}
}

// mcpFilterOperators are Notion's operators for a native one, by the family
// of value the property holds (seen 2026-11-09).
var mcpFilterOperators = map[string]map[string]string{
	"text": {
		"equals": "string_is", "does_not_equal": "string_is_not", "contains": "string_contains",
		"does_not_contain": "string_does_not_contain", "starts_with": "string_starts_with", "ends_with": "string_ends_with",
	},
	"number": {
		"equals": "number_equals", "does_not_equal": "number_does_not_equal", "greater_than": "number_greater_than",
		"less_than": "number_less_than", "greater_than_or_equal_to": "number_greater_than_or_equal_to",
		"less_than_or_equal_to": "number_less_than_or_equal_to",
	},
	"select":   {"equals": "enum_is", "contains": "enum_is", "does_not_equal": "enum_is_not", "does_not_contain": "enum_is_not"},
	"multi":    {"equals": "enum_contains", "contains": "enum_contains", "does_not_equal": "enum_does_not_contain", "does_not_contain": "enum_does_not_contain"},
	"status":   {"equals": "status_is", "contains": "status_is", "does_not_equal": "status_is_not", "does_not_contain": "status_is_not"},
	"checkbox": {"equals": "checkbox_is", "does_not_equal": "checkbox_is_not"},
	"people":   {"equals": "person_contains", "contains": "person_contains", "does_not_equal": "person_does_not_contain", "does_not_contain": "person_does_not_contain"},
	"relation": {"equals": "relation_contains", "contains": "relation_contains", "does_not_equal": "relation_does_not_contain", "does_not_contain": "relation_does_not_contain"},
	"date": {
		"equals": "date_is", "before": "date_is_before", "after": "date_is_after",
		"on_or_before": "date_is_on_or_before", "on_or_after": "date_is_on_or_after",
	},
}

// mcpFilterRelativeWindows are the native windows that read as Notion's past
// or future count of a unit.
var mcpFilterRelativeWindows = map[string][2]string{
	"past_week": {"past", "week"}, "past_month": {"past", "month"}, "past_year": {"past", "year"},
	"next_week": {"future", "week"}, "next_month": {"future", "month"}, "next_year": {"future", "year"},
}

// mcpFilterCondition is one native condition as Notion's record gives it; a
// condition on several options or people reads as Notion's group of one each.
func mcpFilterCondition(filter databaseQueryFilter, byID map[string]notionMCPDataSourceProperty) map[string]any {
	property, found := byID[filter.PropertyID]
	if !found {
		return nil
	}
	condition := func(operator string, value any) map[string]any {
		term := map[string]any{
			"type": "property", "property": property.Name, "propertyType": mcpNotionPropertyType(property.Type), "operator": operator,
		}
		if value != nil {
			term["value"] = value
		}
		return term
	}
	if filter.Operator == "is_empty" || filter.Operator == "is_not_empty" {
		return condition(filter.Operator, nil)
	}
	var value any
	_ = json.Unmarshal(filter.Value, &value)
	exact := func(value any) map[string]any { return map[string]any{"type": "exact", "value": value} }
	family := mcpFilterFamily(property, filter)
	operator, known := mcpFilterOperators[family][filter.Operator]
	if !known {
		operator = filter.Operator
	}
	// A filter bar's condition waiting for its value has none, as Notion's
	// record keeps one (seen 2026-11-09: {"type": "exact"}).
	if value == nil && family != "date" {
		return condition(operator, map[string]any{"type": "exact"})
	}
	switch family {
	case "date":
		return mcpDateCondition(filter, value, condition, exact)
	case "select", "multi", "status", "people", "relation":
		if !known {
			break
		}
		values, listed := value.([]any)
		if !listed {
			values = []any{value}
		}
		if len(values) == 0 {
			return condition(operator, map[string]any{"type": "exact"})
		}
		terms := []any{}
		for _, item := range values {
			terms = append(terms, condition(operator, mcpFilterValue(property, family, item)))
		}
		if len(terms) == 1 {
			return terms[0].(map[string]any)
		}
		// One of several reads as Notion's "or" group, none of them as its "and".
		conjunction := databaseQueryConjunctionOr
		if strings.Contains(operator, "_not") {
			conjunction = databaseQueryConjunctionAnd
		}
		return map[string]any{"type": "group", "operator": conjunction, "filters": terms}
	}
	return condition(operator, exact(value))
}

// mcpFilterFamily is the family of value a condition compares, a formula's
// or rollup's by what it compares with.
func mcpFilterFamily(property notionMCPDataSourceProperty, filter databaseQueryFilter) string {
	switch property.Type {
	case graphValueTypeStatus:
		return "status"
	case graphValueTypeFormula, graphValueTypeRollup:
		_, family := notionViewComputedKind(property.Type, filter)
		return family
	}
	return notionViewConditionFamily(notionDatabaseRowPageOfficialPropertyType(property.Type))
}

// mcpFilterValue is an option, person or page a condition names, as
// Notion's record names it: an option by its name, a status's group as a
// group, a person as a user and a page by its ID.
func mcpFilterValue(property notionMCPDataSourceProperty, family string, value any) any {
	text, _ := value.(string)
	switch family {
	case "select", "multi", "status":
		options, _ := property.Config["options"].([]any)
		for _, raw := range options {
			option, _ := raw.(map[string]any)
			if option["id"] == text || option["name"] == text {
				name, _ := option["name"].(string)
				text = name
				if family == "status" {
					return map[string]any{"type": "is_option", "value": name}
				}
				break
			}
		}
		if family == "status" {
			groups, _ := property.Config["statusGroups"].([]any)
			for _, raw := range groups {
				group, _ := raw.(map[string]any)
				if group["id"] == text || group["name"] == text {
					return map[string]any{"type": "is_group", "value": group["name"]}
				}
			}
			return map[string]any{"type": "is_option", "value": text}
		}
		return map[string]any{"type": "exact", "value": text}
	case "relation":
		// The page the view is on, or each page its template makes.
		if object, ok := value.(map[string]any); ok && object["kind"] == "leafen.current_page" {
			return map[string]any{"type": "relative", "value": "this_page"}
		}
	case "people":
		if object, ok := value.(map[string]any); ok && object["kind"] == "leafen.current_user" {
			return map[string]any{"type": "relative", "value": "me"}
		}
		// By the ID notion-get-users gives (notionPersonID).
		return map[string]any{"type": "exact", "value": map[string]any{"id": notionPersonID(text), "table": "notion_user"}}
	}
	return map[string]any{"type": "exact", "value": value}
}

// mcpDateCondition is a date condition: a day as Notion's exact date, today
// and the windows around it as its relative values.
func mcpDateCondition(
	filter databaseQueryFilter, value any, condition func(string, any) map[string]any, exact func(any) map[string]any,
) map[string]any {
	day := func(raw any) map[string]any {
		text, _ := raw.(string)
		return map[string]any{"type": "date", "start_date": text}
	}
	if window, relative := mcpFilterRelativeWindows[filter.Operator]; relative {
		return condition("date_is_within", map[string]any{
			"type": "relative", "value": "custom", "direction": window[0], "unit": window[1], "count": 1,
		})
	}
	switch filter.Operator {
	case "this_week":
		return condition("date_is_within", map[string]any{"type": "relative", "value": "this_week"})
	case "relative_to_today":
		window, _ := value.(map[string]any)
		direction, _ := window["direction"].(string)
		unit, _ := window["unit"].(string)
		if direction == "this" {
			if unit == "day" {
				return condition("date_is", map[string]any{"type": "relative", "value": "today"})
			}
			return condition("date_is_within", map[string]any{"type": "relative", "value": "this_" + unit})
		}
		if direction == "next" {
			direction = "future"
		}
		return condition("date_is_within", map[string]any{
			"type": "relative", "value": "custom", "direction": direction, "unit": unit, "count": window["count"],
		})
	case "is_between":
		bounds, _ := value.(map[string]any)
		return condition("date_is_within", exact(map[string]any{
			"type": "daterange", "start_date": bounds["start"], "end_date": bounds["end"],
		}))
	}
	if operator, known := mcpFilterOperators["date"][filter.Operator]; known {
		// A day named from today stays so, as Notion's reads it back.
		word, _ := value.(string)
		if _, relative := databaseQueryRelativeFilterDays[word]; relative {
			return condition(operator, map[string]any{"type": "relative", "value": word})
		}
		return condition(operator, exact(day(value)))
	}
	return condition(filter.Operator, exact(value))
}
