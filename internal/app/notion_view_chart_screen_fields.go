package app

import "slices"

// A chart's format the screen made — a new chart from "+", or a view made a
// chart from another layout — reads in Notion's views API with the fields
// its screen writes there, each at the chart's setting (seen 2026-11-18 in
// Chrome, a table made a chart, two charts from "+", their type left alone
// or picked as column, line or number): its type, "column" until another is
// picked, its own sort ("manual" until sorted), its own hide_empty_groups
// and its X axis's (false until "0 값 생략" is on; none along dates or a
// status picked, notionScreenChartQuietAxes, nor in a chart started along
// dates or numbers, notionScreenChartUnsaidKey). A format the API gave —
// a chart made or changed with a configuration, which always names its
// chart_type (notionViewConfigurationRefusal) — reads only what it gave
// (notionViewGivenConfiguration), and goes on so through the screen (seen
// 2026-11-18: an API chart's type picked on the screen, then made a table
// and a chart again, still read without them; a screen chart given a
// configuration read without them, one given a name alone kept them).

// notionScreenChartFields are what Notion's screen writes into the format
// of a chart it makes, as given fields (notionViewGivenGroupHiding names an
// X axis's hide_empty_groups so).
var notionScreenChartFields = []string{"chart_type", "sort", "hide_empty_groups", "x_axis.hide_empty_groups"}

// notionScreenChartQuietAxes are the X axes whose hide_empty_groups Notion's
// screen leaves out once picked, until its "0 값 생략" switch is used (seen
// 2026-11-18 in Chrome, two databases: days by each unit and a status by its
// options or groups, in column, bar, line and donut charts; a select's and a
// person's say it). The screen keeps what it says apart then
// (notionChartAxisHidingKey: ChartSettingsPanel.tsx's switch, and a status a
// table made a chart starts along, viewLayoutDefaults.ts).
var notionScreenChartQuietAxes = []string{"date", "status"}

// notionScreenChartUnsaidKey marks a chart the screen started along dates or
// numbers, whose format says nothing of its empty groups, its own or its X
// axis's, as Notion's (seen 2026-11-18 in Chrome, a table made a chart and
// one from "+" in five databases): its own stays unsaid until "0 값 생략" is
// used, an axis picked later saying its own (viewLayoutDefaults.ts
// withChartAxis, ChartSettingsPanel.tsx).
const notionScreenChartUnsaidKey = "chartEmptyGroupsUnsaid"

// withNotionScreenChartFields adds to a chart's given fields those its
// screen writes, where the API never gave its format.
func withNotionScreenChartFields(viewType string, settings, configuration map[string]any, given map[string]bool) {
	if viewType != graphViewTypeChart || given["chart_type"] {
		return
	}
	axis, _ := configuration["x_axis"].(map[string]any)
	axisType, _ := axis["type"].(string)
	_, said := settings[notionChartAxisHidingKey].(bool)
	unsaid := settings[notionScreenChartUnsaidKey] == true
	quiet := slices.Contains(notionScreenChartQuietAxes, axisType) || unsaid && axisType == "number"
	for _, field := range notionScreenChartFields {
		if field == "x_axis.hide_empty_groups" && !said && quiet || field == "hide_empty_groups" && unsaid {
			continue
		}
		given[field] = true
	}
}

// officialChartType is a chart's type as the API names it: a column chart
// until another is picked, as the screen draws it.
func officialChartType(config map[string]any) string {
	for official, native := range notionViewChartTypes {
		if config["chartType"] == native {
			return official
		}
	}
	return "column"
}
