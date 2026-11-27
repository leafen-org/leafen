package app

import (
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// A chart's format beyond its type, axes, color theme and height, as the
// official view configuration names it and Notion's chart settings set it:
// how its groups are sorted and left out, what its Y axis stacks by and in
// what style, its legend, axis names, grid lines, range, reference lines and
// caption, a line's curve, fill and running total, a donut's labels and a
// number chart's title. A setting left at its default stays unset natively.

var (
	notionViewChartSorts          = []string{"manual", "x_ascending", "x_descending", "y_ascending", "y_descending"}
	notionViewChartSeriesSorts    = []string{"manual", "ascending", "descending"}
	notionViewChartGroupStyles    = []string{"normal", "percent", "side_by_side"}
	notionViewChartLegends        = []string{"off", "bottom", "side"}
	notionViewChartAxisLabels     = []string{"none", "x_axis", "y_axis", "both"}
	notionViewChartGridLines      = []string{"none", "horizontal", "vertical", "both"}
	notionViewChartDonutLabels    = []string{"none", "value", "name", "name_and_value"}
	notionViewChartLineDashStyles = []string{"solid", "dash"}
	// A reference line's colors as Notion's API names them, in its order
	// (seen 2026-11-12): gray is the screen's 검정색, a new line's, and
	// lightgray its 옅은 회색.
	notionViewChartLineColors = []string{"gray", "lightgray", "brown", "yellow", "orange", "green", "blue", "purple", "pink", "red"}
	// notionViewChartLineScreenColors are those colors as the screen keeps
	// them (chartViewSettings.ts): its black is "default", its 옅은 회색 "gray".
	notionViewChartLineScreenColors = []string{"default", "gray", "brown", "yellow", "orange", "green", "blue", "purple", "pink", "red"}
)

// notionViewChartLineMostLines is how many reference lines Notion's API
// takes for a chart (seen 2026-11-12: 100, and 101 refused); its screen
// adds up to 10 (ChartReferenceLines.tsx).
const notionViewChartLineMostLines = 100

// officialChartLineColor is a line's color as the API names it, from the
// screen's; one the screen doesn't know is its black.
func officialChartLineColor(screen string) string {
	if index := slices.Index(notionViewChartLineScreenColors, screen); index >= 0 {
		return notionViewChartLineColors[index]
	}
	return notionViewChartLineColors[0]
}

// screenChartLineColor is a line's color as the screen keeps it, from the
// API's.
func screenChartLineColor(official string) string {
	if index := slices.Index(notionViewChartLineColors, official); index >= 0 {
		return notionViewChartLineScreenColors[index]
	}
	return notionViewChartLineScreenColors[0]
}

// notionViewChartChoices are the official choice fields and their native
// keys, each with its choices and the default left unset: a donut's labels
// none, as Notion's donut draws one never labeled (seen 2026-11-18;
// chartViewSettings.ts chartDonutLabels).
var notionViewChartChoices = map[string]struct {
	key      string
	choices  []string
	fallback string
}{
	"sort":            {"chartSort", notionViewChartSorts, "manual"},
	"group_style":     {"chartGroupStyle", notionViewChartGroupStyles, "normal"},
	"legend_position": {"chartLegendPosition", notionViewChartLegends, "bottom"},
	"axis_labels":     {"chartAxisLabels", notionViewChartAxisLabels, "none"},
	"grid_lines":      {"chartGridLines", notionViewChartGridLines, "horizontal"},
	"donut_labels":    {"chartDonutLabels", notionViewChartDonutLabels, "none"},
}

// notionViewChartFlags are the official switches and their native keys.
var notionViewChartFlags = map[string]string{
	"hide_empty_groups":   "chartHideEmptyGroups",
	"cumulative":          "chartCumulative",
	"smooth_line":         "chartSmoothLine",
	"hide_line_fill_area": "chartHideLineFill",
	"hide_title":          "chartHideTitle",
}

// notionChartKeptSettings are a chart's settings a configuration given
// through the API leaves: what its X axis groups by and the groups its
// screen ordered or hid along it, until it groups by another property.
var notionChartKeptSettings = []string{"chartGroupBy", "chartGroupOrder", "chartHiddenGroups"}

// withoutNotionChartFormat is a chart view's native config as a
// configuration given through the API finds it: Notion's API takes a chart's
// configuration whole, every chart setting it doesn't give back at its
// default — what it counts, stacks by and sorts by, its styles, range,
// reference lines and caption, and its screen's own settings such as a
// number chart's format too — while the groups hidden along its X axis stay
// hidden (seen 2026-11-12). Its filter, sorts and name are not its chart's.
func withoutNotionChartFormat(current string) (string, error) {
	config := map[string]any{}
	if json.Unmarshal([]byte(current), &config) != nil || config == nil {
		return "", errNotionDatabaseRowPageProjectionInvalid
	}
	for key := range config {
		if strings.HasPrefix(key, "chart") && !slices.Contains(notionChartKeptSettings, key) {
			delete(config, key)
		}
	}
	encoded, err := json.Marshal(config)
	return string(encoded), err
}

// notionChartCountsBy is whether a chart's configuration counts by the
// field, y_axis or value: a number chart by its value and any other by its
// y_axis, the other taken and left out, as Notion's API does (seen
// 2026-11-12: a column chart given value and a number chart given y_axis
// read back counting rows). A configuration not naming its chart type takes
// either.
func notionChartCountsBy(field string, input map[string]any) bool {
	chartType, named := input["chart_type"].(string)
	return !named || (field == "value") == (chartType == "number")
}

// notionViewChartFormatField reports whether an official field is one of a chart's format.
func notionViewChartFormatField(field string) bool {
	_, choice := notionViewChartChoices[field]
	_, flag := notionViewChartFlags[field]
	return choice || flag || slices.Contains([]string{"y_axis_min", "y_axis_max", "caption", "reference_lines", "stack_by"}, field)
}

// officialViewChartFormat adds a chart's format to its official configuration.
func (scope notionViewFilterScope) officialViewChartFormat(chartType string, config, configuration map[string]any) {
	choice := func(field string) string {
		spec := notionViewChartChoices[field]
		if value, ok := config[spec.key].(string); ok && slices.Contains(spec.choices, value) {
			return value
		}
		return spec.fallback
	}
	axes := chartType == "bar" || chartType == "horizontal_bar" || chartType == "line"
	configuration["sort"] = choice("sort")
	if propertyID, _ := config["chartGroupBy"].(string); axes && scope.byID[propertyID].NativeType == graphValueTypeNumber && configuration["sort"] == "manual" {
		configuration["sort"] = "x_ascending"
	}
	configuration["hide_empty_groups"] = config["chartHideEmptyGroups"] == true
	if group, ok := configuration["x_axis"].(map[string]any); ok {
		group["hide_empty_groups"] = notionChartAxisHidesEmptyGroups(config)
	}
	configuration["legend_position"] = choice("legend_position")
	// A shown caption reads as its words, "" when it has none, as Notion's
	// gives it (seen 2026-11-12: a chart given caption "" reads ""); a
	// caption not shown reads null.
	configuration["caption"] = nil
	if caption, _ := config["chartCaption"].(string); config["chartShowCaption"] == true {
		configuration["caption"] = caption
	}
	// Its styles, range and reference lines read in any type of chart, a
	// donut's and a number chart's too (Notion's, seen 2026-11-12); what it
	// stacks by and a line's switches in those that draw them.
	configuration["group_style"] = choice("group_style")
	configuration["axis_labels"] = choice("axis_labels")
	configuration["grid_lines"] = choice("grid_lines")
	configuration["y_axis_min"], configuration["y_axis_max"] = notionViewChartNumber(config["chartYMin"]), notionViewChartNumber(config["chartYMax"])
	configuration["reference_lines"] = officialViewChartReferenceLines(config["chartReferenceLines"])
	switch {
	case axes:
		if property, ok := config["chartStackBy"].(string); ok && property != "" {
			if group, ok := scope.officialViewGroupBy(property, config, "chartStackBy").(map[string]any); ok {
				// Stacked groups come ascending unless sorted otherwise, as Notion's (seen 2026-10-05);
				// a formula's or rollup's say so in their group_by alone (seen 2026-11-12).
				order, _ := config["chartStackSort"].(string)
				if !slices.Contains(notionViewChartSeriesSorts, order) {
					order = "ascending"
				}
				if formula, ok := group["group_by"].(map[string]any); ok && group["type"] == "formula" {
					family, _ := formula["type"].(string)
					formula["sort"] = map[string]any{"type": notionViewAxisSort(family, order)}
				} else {
					group["sort"] = map[string]any{"type": order}
				}
				configuration["stack_by"] = group
			}
		}
		if chartType == "line" {
			for _, field := range []string{"cumulative", "smooth_line", "hide_line_fill_area"} {
				configuration[field] = config[notionViewChartFlags[field]] == true
			}
		}
	case chartType == "donut":
		configuration["donut_labels"] = choice("donut_labels")
	case chartType == "number":
		configuration["hide_title"] = config["chartHideTitle"] == true
	}
}

func notionViewChartNumber(value any) any {
	if number, ok := value.(float64); ok && !math.IsNaN(number) && !math.IsInf(number, 0) {
		return number
	}
	return nil
}

// officialViewChartReferenceLines are a chart's reference lines as Notion's
// API reads them, each with its id first: the one it was given or made
// with (nativeViewChartReferenceLines), or for a line kept from before lines
// had one, an id its place and settings make, the same at every read until
// it changes.
func officialViewChartReferenceLines(value any) []any {
	lines := []any{}
	items, _ := value.([]any)
	for index, raw := range items {
		line, _ := raw.(map[string]any)
		at, ok := notionViewChartNumber(line["value"]).(float64)
		if !ok {
			continue
		}
		label, _ := line["label"].(string)
		color, _ := line["color"].(string)
		if !slices.Contains(notionViewChartLineScreenColors, color) {
			color = "default"
		}
		dash, _ := line["dashStyle"].(string)
		if !slices.Contains(notionViewChartLineDashStyles, dash) {
			dash = "dash"
		}
		id, given := line["id"].(string)
		if !given {
			kept, _ := json.Marshal([]any{at, label, color, dash})
			id = uuid.NewSHA1(uuid.NameSpaceOID, []byte("chart-reference-line\x00"+strconv.Itoa(index)+"\x00"+string(kept))).String()
		}
		lines = append(lines, map[string]any{"id": id, "value": at, "label": label, "color": officialChartLineColor(color), "dash_style": dash})
	}
	return lines
}

// applyOfficialViewChartFormat writes one official chart format field into
// a native view config, reporting whether the field is one.
func (scope notionViewFilterScope) applyOfficialViewChartFormat(field string, value any, config map[string]any) (bool, error) {
	set := func(key string, next any, clear bool) {
		if clear {
			delete(config, key)
		} else {
			config[key] = next
		}
	}
	if spec, ok := notionViewChartChoices[field]; ok {
		choice, _ := value.(string)
		if value != nil && !slices.Contains(spec.choices, choice) {
			return true, notionViewFieldCheck(false, field)
		}
		// A chart's own sort given "manual" is kept apart from none given: its
		// settings show it over its X axis's own (chartViewSettings.ts
		// chartShownSort; Notion's, seen 2026-11-18). It reads back the same.
		set(spec.key, choice, value == nil || choice == spec.fallback && field != "sort")
		return true, nil
	}
	if key, ok := notionViewChartFlags[field]; ok {
		flag, isFlag := value.(bool)
		if value != nil && !isFlag {
			return true, notionViewFieldCheck(false, field)
		}
		set(key, true, !flag)
		if field == "hide_empty_groups" {
			delete(config, notionChartAxisHidingKey)
		}
		return true, nil
	}
	switch field {
	case "y_axis_min", "y_axis_max":
		key := map[string]string{"y_axis_min": "chartYMin", "y_axis_max": "chartYMax"}[field]
		number := notionViewChartNumber(value)
		if value != nil && number == nil {
			return true, notionViewFieldCheck(false, field)
		}
		set(key, number, number == nil)
		return true, nil
	case "caption":
		caption, isText := value.(string)
		if value != nil && !isText || len(caption) > 2000 {
			return true, notionViewFieldCheck(false, field)
		}
		set("chartCaption", caption, caption == "")
		set("chartShowCaption", true, value == nil)
		return true, nil
	case "reference_lines":
		lines, err := nativeViewChartReferenceLines(value)
		if err != nil {
			return true, err
		}
		set("chartReferenceLines", lines, len(lines) == 0)
		return true, nil
	case "stack_by":
		delete(config, "chartStackOrder")
		delete(config, "chartHiddenStackGroups")
		delete(config, "chartStackSort")
		delete(config, "chartHideEmptyStackGroups")
		group, _ := value.(map[string]any)
		if sorting, _ := group["sort"].(map[string]any); sorting != nil {
			order, _ := sorting["type"].(string)
			if !slices.Contains(notionViewChartSeriesSorts, order) {
				return true, notionViewFieldCheck(false, field)
			}
			if order != "ascending" {
				config["chartStackSort"] = order
			}
		}
		if err := scope.applyOfficialViewGroup("chartStackBy", value, config); err != nil || group["type"] != "formula" {
			return true, err
		}
		// A formula's or rollup's stacked groups sort as their group_by says, read as its result family's.
		if given, _ := group["group_by"].(map[string]any); given["sort"] != nil {
			formula, _ := config["chartStackFormulaGroup"].(map[string]any)
			sorting, _ := formula["sort"].(map[string]any)
			delete(config, "chartStackSort")
			if order, _ := sorting["type"].(string); order != "ascending" && slices.Contains(notionViewChartSeriesSorts, order) {
				config["chartStackSort"] = order
			}
		}
		return true, nil
	}
	return false, nil
}

func nativeViewChartReferenceLines(value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok || len(items) > notionViewChartLineMostLines {
		return nil, notionViewFieldCheck(false, "reference_lines")
	}
	lines := []any{}
	for _, raw := range items {
		line, _ := raw.(map[string]any)
		at := notionViewChartNumber(line["value"])
		// Its label is any length, as Notion's (seen 2026-11-12: 5000 letters).
		label, labelOK := line["label"].(string)
		color, _ := line["color"].(string)
		dash, _ := line["dash_style"].(string)
		id, idGiven := line["id"].(string)
		if at == nil || !labelOK || !slices.Contains(notionViewChartLineColors, color) ||
			!slices.Contains(notionViewChartLineDashStyles, dash) || line["id"] != nil && !idGiven {
			return nil, notionViewFilterInvalid("a reference line needs a value, a label, a color and a solid or dash style")
		}
		// A line keeps the id it is given, whatever its words, and one given
		// none takes a new one, as Notion's (seen 2026-11-12: "nope", capitals
		// and the same id twice are read back as given).
		if !idGiven {
			id = uuid.NewString()
		}
		lines = append(lines, map[string]any{"id": id, "value": at, "label": label, "color": screenChartLineColor(color), "dashStyle": dash})
	}
	return lines, nil
}

const notionNewChartDirectiveMessage = `A chart view needs a CHART directive in configure, as 'CHART column; GROUP BY "Status"'.`

// notionNewChartRefusal refuses a new chart that doesn't say what it charts,
// as Notion's API does (seen 2026-11-05), and is "" for one that does.
// notionNewMapRefusal refuses a map view made with no location property to
// place its rows by, as Notion's API does (seen 2026-11-06).
func notionNewMapRefusal(viewType string, raw json.RawMessage) string {
	if viewType != graphViewTypeMap {
		return ""
	}
	configuration := struct {
		MapBy string `json:"map_by"`
	}{}
	if len(raw) != 0 && string(raw) != "null" && json.Unmarshal(raw, &configuration) == nil && configuration.MapBy != "" {
		return ""
	}
	return `A map view needs a MAP BY directive in configure, as 'MAP BY "Location"'.`
}

func notionNewChartRefusal(viewType string, raw json.RawMessage) string {
	if viewType != graphViewTypeChart {
		return ""
	}
	if len(raw) == 0 || string(raw) == "null" {
		return notionNewChartDirectiveMessage
	}
	return notionChartGroupedRefusal(raw)
}

// notionChartGroupedRefusal refuses, as Notion's API does, a chart's
// configuration, new or changed, that charts its rows by groups — any but a
// number chart — without its x_axis, whatever x_axis the chart has (seen
// 2026-11-12: a column chart changed with {chart_type: bar, height} alone,
// or with x_axis null, refused); "" for any other.
func notionChartGroupedRefusal(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	configuration := struct {
		Type      string          `json:"type"`
		ChartType string          `json:"chart_type"`
		XAxis     json.RawMessage `json:"x_axis"`
	}{}
	if json.Unmarshal(raw, &configuration) != nil || configuration.Type != graphViewTypeChart {
		return ""
	}
	switch configuration.ChartType {
	case "":
		return notionFaultSaid("body", notionFaultMissing("body.configuration.chart_type"))
	case "number":
		return ""
	}
	if len(configuration.XAxis) != 0 && string(configuration.XAxis) != "null" {
		return ""
	}
	name := configuration.ChartType
	if name == "donut" {
		name = "Donut"
	}
	return "A grouped " + name + " chart needs an x_axis group-by."
}

// notionViewChartUnitKeys are where a chart's X axis (chartGroupBy) and its
// stacked groups (chartStackBy) each keep their own unit, as Notion's chart
// settings pick them beside the property and its API reads them back (seen
// 2026-11-12): a date's day, week, month, year or how near today, month
// until picked; a status's groups, its options until picked. The screen
// reads them too (web/src/components/database/chartViewSettings.ts). Each
// keeps the start_day_of_week given too, which only the API reads
// (notionViewGivenWeekStartKey).
var notionViewChartUnitKeys = map[string]struct{ date, status, start string }{
	"chartGroupBy": {"chartDateBy", "chartStatusBy", "chartGivenStartDayOfWeek"},
	"chartStackBy": {"chartStackDateBy", "chartStackStatusBy", "chartStackGivenStartDayOfWeek"},
}

// notionViewChartAxisKeepsUnit says a chart's X axis or stacked groups by
// the property keep the unit they are given, as applyOfficialViewGroup
// writes it (notionViewChartUnitKeys).
func notionViewChartAxisKeepsUnit(property notionViewProperty, unit string) bool {
	switch {
	case notionViewDateProperty(property):
		return slices.Contains(notionViewDateGroupings, unit)
	case property.OfficialType == "status":
		return unit == "group" || unit == "option"
	}
	return false
}
