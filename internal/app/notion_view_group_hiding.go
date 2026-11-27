package app

import "encoding/json"

// Whether a chart's X axis or what it stacks by leaves out its empty groups,
// as Notion's API takes hide_empty_groups on each (seen 2026-11-12): true
// leaves them out, false or none shows them, given again too; a chart's own
// hide_empty_groups says it for its X axis. Natively each is kept as the
// screen keeps it, only while it hides (a table's and a board's groups:
// applyOfficialViewEmptyGroups). Given in one request with a chart's own
// another way, an X axis's is kept apart (notionChartAxisHidingKey), as
// Notion's keeps both (seen 2026-11-12): the chart leaves out its empty
// groups when either does, and its "0 값 생략" switch shows its own and
// sets both.

// notionChartAxisHidingKey keeps an X axis's hide_empty_groups where it is
// not the chart's own (chartHideEmptyGroups); the screen reads it too
// (web/src/components/database/chartViewSettings.ts).
const notionChartAxisHidingKey = "chartAxisHideEmptyGroups"

// notionChartAxisHidesEmptyGroups is whether a chart's X axis says it hides
// its empty groups.
func notionChartAxisHidesEmptyGroups(config map[string]any) bool {
	if hides, apart := config[notionChartAxisHidingKey].(bool); apart {
		return hides
	}
	return config["chartHideEmptyGroups"] == true
}

// applyOfficialChartAxisGroupHiding keeps the X axis's hide_empty_groups
// apart where a request gives it with the chart's own another way.
func applyOfficialChartAxisGroupHiding(input, config map[string]any) {
	axis, _ := input["x_axis"].(map[string]any)
	hides, axisSays := axis["hide_empty_groups"].(bool)
	own, ownSays := input["hide_empty_groups"].(bool)
	if axisSays && ownSays && hides != own {
		config[notionChartAxisHidingKey] = hides
	}
}

// applyOfficialChartGroupHiding writes a chart group's hide_empty_groups
// (key chartGroupBy or chartStackBy).
func applyOfficialChartGroupHiding(key string, group, config map[string]any) {
	setting := map[string]string{"chartGroupBy": "chartHideEmptyGroups", "chartStackBy": "chartHideEmptyStackGroups"}[key]
	delete(config, setting)
	if key == "chartGroupBy" {
		delete(config, notionChartAxisHidingKey)
	}
	if group["hide_empty_groups"] == true {
		config[setting] = true
	}
}

// notionViewGivenGroupHiding keeps where a request last said whether a group
// hides its empty groups, as Notion's API gives it back there alone (seen
// 2026-11-12): in a chart's x_axis or stack_by or a board's group_by or
// sub_group_by ("x_axis.hide_empty_groups"), the group given again without
// it taking it away; or a chart's own hide_empty_groups, which a new x_axis
// takes away unless given with it, and which takes away the x_axis's unless
// given with it.
func notionViewGivenGroupHiding(viewType string, given map[string]json.RawMessage, known map[string]bool) {
	for _, group := range []string{"x_axis", "group_by", "sub_group_by", "stack_by"} {
		raw, gave := given[group]
		if !gave || !notionViewTakesField(viewType, group) {
			continue
		}
		marker := group + ".hide_empty_groups"
		delete(known, marker)
		fields := map[string]json.RawMessage{}
		if json.Unmarshal(raw, &fields) == nil {
			if _, hides := fields["hide_empty_groups"]; hides {
				known[marker] = true
			}
		}
		if _, own := given["hide_empty_groups"]; group == "x_axis" && !own {
			delete(known, "hide_empty_groups")
		}
	}
	axis := map[string]json.RawMessage{}
	_ = json.Unmarshal(given["x_axis"], &axis)
	if _, own := given["hide_empty_groups"]; own && viewType == graphViewTypeChart && axis["hide_empty_groups"] == nil {
		delete(known, "x_axis.hide_empty_groups")
	}
}

// notionViewGroupHidingGiven gives a group's hide_empty_groups where a
// request said it (notionViewGivenGroupHiding) and nowhere else: a chart's
// x_axis reads none by default, its count as its y_axis as Notion's gives it
// when none was given (seen 2026-11-12: a bar, donut or line chart made
// with no y_axis reads {"aggregator":"count"}).
func notionViewGroupHidingGiven(viewType string, settings, configuration map[string]any, given map[string]bool) {
	charted := map[string]string{"x_axis": "chartHideEmptyGroups", "stack_by": "chartHideEmptyStackGroups"}
	for _, field := range []string{"x_axis", "stack_by", "group_by", "sub_group_by"} {
		group, ok := configuration[field].(map[string]any)
		setting, chart := charted[field]
		switch {
		case !ok:
		case given[field+".hide_empty_groups"] && field == "x_axis":
			group["hide_empty_groups"] = notionChartAxisHidesEmptyGroups(settings)
		case given[field+".hide_empty_groups"] && chart:
			group["hide_empty_groups"] = settings[setting] == true
		case given[field+".hide_empty_groups"]:
			// A table's or board's group reads true while it hides (withEmptyGroupsHidden).
			if _, hides := group["hide_empty_groups"]; !hides {
				group["hide_empty_groups"] = false
			}
		case field == "x_axis":
			delete(group, "hide_empty_groups")
		}
	}
	if _, counted := configuration["y_axis"]; viewType == graphViewTypeChart && !counted && configuration["chart_type"] != "number" {
		configuration["y_axis"] = map[string]any{"aggregator": "count"}
	}
}
