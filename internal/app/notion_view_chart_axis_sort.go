package app

import "slices"

// The order a chart's X axis goes in. Notion's views API keeps the axis's
// own sort (x_axis.sort, a formula's in its group_by) apart from the chart's
// "sort", and the screen draws by the axis's unless the chart sorts by value
// (seen 2026-11-12: an axis sorted descending under "sort" x_ascending draws
// descending, under y_ascending by value), while its settings show the
// chart's own "sort" where one was given (seen 2026-11-18); its settings
// write both. A rollup's axis draws by the chart's "sort" alone, whatever
// its group_by says. The native settings are the screen's: chartGroupSort,
// the axis's own sort, which the screen's sort menu clears
// (chartViewSettings.ts), and chartSort.

// applyOfficialViewChartAxisSort keeps a chart's X axis's own sort, as its
// type reads it back: a person's and a box's by hand, a number's and a
// date's never.
func (scope notionViewFilterScope) applyOfficialViewChartAxisSort(value any, config map[string]any) {
	delete(config, "chartGroupSort")
	group, _ := value.(map[string]any)
	id, _ := config["chartGroupBy"].(string)
	property := scope.byID[id]
	sorting, _ := group["sort"].(map[string]any)
	groupType, _ := notionViewGroupType(property.OfficialType)
	switch property.OfficialType {
	case "rollup":
		return
	case "formula":
		// One given as another type sorts its values as its result's (notion_view_chart_given_group.go).
		groupType = property.FormulaResult
		if formula, ok := config["chartFormulaGroup"].(map[string]any); ok {
			sorting, _ = formula["sort"].(map[string]any)
			groupType, _ = formula["type"].(string)
		}
	}
	sort, _ := sorting["type"].(string)
	if slices.Contains(notionViewGroupSorts, sort) {
		config["chartGroupSort"] = notionViewAxisSort(groupType, sort)
	}
}

// notionViewAxisSort is a group's sort as Notion's API reads it back for its
// type or a formula's result family: a person's and a box's by hand, a
// number's and a date's by value.
func notionViewAxisSort(groupType, sort string) string {
	switch groupType {
	case "person", "created_by", "last_edited_by", "checkbox":
		return "manual"
	case "number", "date", "created_time", "last_edited_time":
		if sort != "descending" {
			return "ascending"
		}
	}
	return sort
}

// officialChartAxisSort is a chart's X axis's own sort as it reads back: as
// kept, else as the chart's settings last sorted it along the axis, else ""
// for the type's own.
func officialChartAxisSort(config map[string]any) string {
	if sort, _ := config["chartGroupSort"].(string); slices.Contains(notionViewGroupSorts, sort) {
		return sort
	}
	switch config["chartSort"] {
	case "x_ascending":
		return "ascending"
	case "x_descending":
		return "descending"
	case nil, "manual":
		return "manual"
	}
	return ""
}
