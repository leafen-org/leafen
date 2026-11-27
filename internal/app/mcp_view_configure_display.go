package app

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// What a configure string says of what a view shows: its properties, cover,
// icon, widths, chart and form (mcp_view_configure.go).

// shownIDs are the properties a view shows now, in its order.
func (change *mcpViewConfigure) shownIDs() []string {
	order, _ := change.config["propertyOrder"].([]any)
	ids := []string{}
	for _, raw := range order {
		if id, ok := raw.(string); ok && change.scope.lists(id) && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, property := range change.scope.viewOrdered() {
		if !slices.Contains(ids, property.ID) {
			ids = append(ids, property.ID)
		}
	}
	hidden := notionViewStringSet(change.config["hiddenProperties"])
	listed := notionViewStringSet(change.config["visibleProperties"])
	_, listing := change.config["visibleProperties"]
	shown := []string{}
	for _, id := range ids {
		_, isHidden := hidden[id]
		_, isListed := listed[id]
		if !isHidden && (!listing || isListed || change.scope.byID[id].NativeType == graphValueTypeTitle) {
			shown = append(shown, id)
		}
	}
	return shown
}

// setShown writes which properties a view shows, as the API's properties do.
func (change *mcpViewConfigure) setShown(shown []string) error {
	items := make([]any, 0, len(change.scope.ordered))
	for _, id := range shown {
		items = append(items, map[string]any{"property_id": id, "visible": true})
	}
	for _, property := range change.scope.viewOrdered() {
		if !slices.Contains(shown, property.ID) {
			items = append(items, map[string]any{"property_id": property.ID, "visible": false})
		}
	}
	if err := change.scope.applyOfficialViewProperties(items, change.config); err != nil {
		return err
	}
	change.listShown(shown)
	return nil
}

func (change *mcpViewConfigure) showProperties(names []string) error {
	shown := []string{}
	for _, name := range names {
		property, err := change.property(name)
		if err != nil {
			return err
		}
		shown = append(shown, property.ID)
	}
	// The title stays shown where the names leave it out.
	for _, property := range change.scope.ordered {
		if property.OfficialType == notionTitlePropertyID && !slices.Contains(shown, property.ID) {
			shown = append([]string{property.ID}, shown...)
		}
	}
	return change.setShown(shown)
}

func (change *mcpViewConfigure) hideProperties(names []string) error {
	shown := change.shownIDs()
	for _, name := range names {
		property, err := change.property(name)
		if err != nil {
			return err
		}
		shown = slices.DeleteFunc(shown, func(id string) bool { return id == property.ID })
	}
	return change.setShown(shown)
}

func (change *mcpViewConfigure) cover(words *mcpViewWords) error {
	if err := change.only("COVER", graphViewTypeBoard, graphViewTypeGallery); err != nil {
		return err
	}
	name, ok := words.quoted()
	if !ok {
		return change.unreadable("COVER", `COVER "Property" [SIZE small|medium|large] [ASPECT cover|contain]`)
	}
	property, err := change.property(name)
	if err != nil {
		return err
	}
	change.config["coverProperty"] = property.ID
	for !words.done() {
		switch {
		case words.keyword("SIZE"):
			size, ok := words.oneOf("small", "medium", "large")
			if !ok {
				return change.unreadable("COVER … SIZE", "SIZE small, medium or large")
			}
			change.config["cardSize"] = size
			change.say(mcpViewSaidCoverSize)
		case words.keyword("ASPECT"):
			aspect, ok := words.oneOf("cover", "contain")
			if !ok {
				return change.unreadable("COVER … ASPECT", "ASPECT cover or contain")
			}
			change.config["fitImage"] = aspect == "contain"
		default:
			return nil
		}
	}
	return nil
}

// icon takes a view's built-in icon as fetch gives one, icons/rocket_blue,
// or an emoji, as Notion's MCP takes "🚀" (2026-11-05: Leafen took only its
// own icons).
func (change *mcpViewConfigure) icon(words *mcpViewWords) error {
	raw, ok := words.quoted()
	raw = strings.TrimSpace(raw)
	if ok && notionMarkdownCalloutEmoji(raw) {
		change.config["icon"] = raw
		return nil
	}
	name, isIcon := strings.CutPrefix(raw, "icons/")
	if !ok || !isIcon || name == "" {
		return mcpInvalid("configure: a view's icon is an emoji or one of Leafen's icons, as ICON \"🚀\" or ICON \"icons/rocket_blue\"")
	}
	glyph, color, colored := strings.Cut(name, "_")
	if !colored {
		color = "gray"
	}
	change.config["icon"] = glyph + ":" + color
	return nil
}

func (change *mcpViewConfigure) widths(words *mcpViewWords) error {
	if err := change.only("WIDTH", graphViewTypeTable); err != nil {
		return err
	}
	widths, _ := change.config["propertyWidths"].(map[string]any)
	if widths == nil {
		widths = map[string]any{}
	}
	for {
		name, ok := words.quoted()
		if !ok {
			return change.unreadable("WIDTH", `WIDTH "Property" 320, "Property" 120`)
		}
		property, err := change.property(name)
		if err != nil {
			return err
		}
		raw, _ := words.value()
		width, err := strconv.Atoi(raw)
		if err != nil || width < 32 {
			return mcpInvalid("configure: a column's width is a whole number of at least 32; %q has %q", name, raw)
		}
		widths[property.ID] = width
		if !words.mark(",") {
			break
		}
	}
	change.config["propertyWidths"] = widths
	return nil
}

func (change *mcpViewConfigure) chart(words *mcpViewWords) error {
	config := change.config
	official, ok := words.oneOf("column", "bar", "line", "donut", "number")
	if !ok {
		return change.unreadable("CHART", "CHART column, bar, line, donut or number")
	}
	config["chartType"] = notionViewChartTypes[official]
	var lines, rules []any
	for !words.done() {
		// A format clause first, so COLOR BY VALUE isn't read as a COLOR.
		clause, found := change.chartFormatClause(words)
		if !found {
			clause, found = change.chartNativeClause(words)
		}
		if found {
			if err := clause(); err != nil {
				return err
			}
			continue
		}
		switch {
		case words.keyword("AGGREGATE"):
			aggregator, ok := words.value()
			if !ok {
				return change.unreadable("CHART … AGGREGATE", `AGGREGATE count, or sum ON "Property"`)
			}
			axis := map[string]any{"aggregator": strings.ToLower(aggregator)}
			if words.keyword("ON") {
				name, ok := words.quoted()
				if !ok {
					return change.unreadable("CHART … AGGREGATE", `AGGREGATE sum ON "Property"`)
				}
				property, err := change.property(name)
				if err != nil {
					return err
				}
				axis["property_id"] = property.ID
			}
			if err := change.scope.applyOfficialViewChartAxis(axis, config); err != nil {
				return err
			}
		case words.keyword("COLOR", "RULE"):
			rule, err := change.colorRule(words)
			if err != nil {
				return err
			}
			rules = append(rules, rule)
		case words.keyword("COLOR"):
			color, ok := words.value()
			if !ok || !slices.Contains(notionViewChartColorThemes, strings.ToLower(color)) {
				return change.unreadable("CHART … COLOR", "COLOR "+strings.Join(notionViewChartColorThemes, ", "))
			}
			delete(config, "chartColorTheme")
			if color = strings.ToLower(color); color != "auto" {
				config["chartColorTheme"] = color
			}
		case words.keyword("HEIGHT"):
			height, ok := words.value()
			if !ok || !slices.Contains(notionViewHeights, strings.ToLower(height)) {
				return change.unreadable("CHART … HEIGHT", "HEIGHT "+strings.Join(notionViewHeights, ", "))
			}
			delete(config, "chartHeight")
			if height = strings.ToLower(height); height != "medium" {
				config["chartHeight"] = height
			}
		case words.keyword("SORT"):
			sort, ok := words.value()
			if !ok {
				return change.unreadable("CHART … SORT", "SORT x_ascending, x_descending, y_ascending or y_descending")
			}
			// As in Notion (seen 2026-11-05), manual is where a chart starts, not a sort to ask for;
			// the refusal is in Leafen's words.
			if strings.EqualFold(sort, "manual") || !slices.Contains(notionViewChartSorts, strings.ToLower(sort)) {
				return mcpInvalid("A chart sorts by x_ascending, x_descending, y_ascending or y_descending, not \"%s\".", sort)
			}
			config["chartSort"] = strings.ToLower(sort)
			delete(config, "chartGroupSort")
		case words.keyword("STACK", "BY"):
			name, ok := words.quoted()
			if !ok {
				return change.unreadable("CHART … STACK BY", `STACK BY "Property"`)
			}
			property, err := change.property(name)
			if err != nil {
				return err
			}
			if config["chartStackBy"] != property.ID {
				delete(config, "chartStackOrder")
				delete(config, "chartHiddenStackGroups")
				delete(config, "chartStackSort")
				// Another property's units start afresh (notionViewChartUnitKeys).
				delete(config, notionViewChartUnitKeys["chartStackBy"].date)
				delete(config, notionViewChartUnitKeys["chartStackBy"].status)
			}
			config["chartStackBy"] = property.ID
		case words.keyword("CAPTION"):
			caption, ok := words.quoted()
			if !ok {
				return change.unreadable("CHART … CAPTION", `CAPTION "text"`)
			}
			delete(config, "chartCaption")
			delete(config, "chartShowCaption")
			if caption != "" {
				config["chartCaption"], config["chartShowCaption"] = caption, true
			}
		case words.keyword("REFERENCE", "LINE"):
			line, err := change.referenceLine(words)
			if err != nil {
				return err
			}
			lines = append(lines, line)
		default:
			return mcpInvalid("configure: %q doesn't read in CHART", words.tokens[words.at].text)
		}
	}
	// The color rules a CHART names replace the number chart's, coloring it by them.
	if rules != nil {
		config["chartColorRules"], config["chartDynamicColor"] = rules, true
	}
	if lines != nil {
		return change.officialChartField("reference_lines", lines)
	}
	return nil
}

// colorRule reads COLOR RULE > 0 green: a comparison, a number and a color.
func (change *mcpViewConfigure) colorRule(words *mcpViewWords) (any, error) {
	form := `COLOR RULE > 0 green`
	if words.done() || words.tokens[words.at].kind != mcpViewTokenMark {
		return nil, change.unreadable("CHART … COLOR RULE", form)
	}
	operator := words.tokens[words.at].text
	words.at++
	raw, _ := words.value()
	at, err := strconv.ParseFloat(raw, 64)
	color, colored := words.oneOf(mcpChartTextColors...)
	if _, known := mcpChartRuleOperators[operator]; !known || err != nil || !colored {
		return nil, change.unreadable("CHART … COLOR RULE", form+" (>, >=, <, <=, = or !=, and "+strings.Join(mcpChartTextColors, ", ")+")")
	}
	return map[string]any{"operator": operator, "value": at, "color": color}, nil
}

// mcpChartTextColors are a number chart's value colors, a number property's.
var mcpChartTextColors = []string{"default", "gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"}

// chartNativeClause reads one of CHART's clauses for what the screen sets
// beyond the API's configuration, each named as fetch's chartFormat names
// it: a number chart's or a donut's value label, a donut's center value,
// negative values, and a number chart's format, decimals and color.
func (change *mcpViewConfigure) chartNativeClause(words *mcpViewWords) (func() error, bool) {
	config := change.config
	flag := func(name, key string, inverted bool) func() error {
		value, ok := words.oneOf("true", "false")
		return func() error {
			if !ok {
				return change.unreadable(name, name+" true or false")
			}
			delete(config, key)
			if (value == "true") != inverted {
				config[key] = true
			}
			return nil
		}
	}
	switch {
	case words.keyword("SHOW", "VALUE", "LABEL"):
		return flag("CHART … SHOW VALUE LABEL", "chartHideCenterLabel", true), true
	case words.keyword("VALUE", "LABEL"):
		label, ok := words.quoted()
		return func() error {
			if !ok {
				return change.unreadable("CHART … VALUE LABEL", `VALUE LABEL "text"`)
			}
			key := "chartCenterLabel"
			if config["chartType"] == "number" {
				key = "chartTitle"
			}
			delete(config, key)
			if label != "" {
				config[key] = label
			}
			return nil
		}, true
	case words.keyword("HIDE", "VALUE"):
		return flag("CHART … HIDE VALUE", "chartHideCenterValue", false), true
	case words.keyword("HIDE", "NEGATIVE"):
		return flag("CHART … HIDE NEGATIVE", "chartHideNegative", false), true
	case words.keyword("NUMBER", "FORMAT"):
		format, _ := words.value()
		format = strings.ToLower(format)
		return func() error {
			if format == "number_with_commas" {
				format = "comma"
			}
			if _, currency := databaseFormulaCurrencyFormats[format]; !currency && !slices.Contains([]string{"number", "comma", "percent"}, format) {
				return change.unreadable("CHART … NUMBER FORMAT", "NUMBER FORMAT number, number_with_commas, percent or a currency such as won or dollar")
			}
			config["chartNumberFormat"] = format
			return nil
		}, true
	case words.keyword("DECIMALS"):
		raw, _ := words.value()
		return func() error {
			decimals, err := strconv.Atoi(raw)
			if err != nil || decimals < 0 || decimals > 5 {
				return change.unreadable("CHART … DECIMALS", "DECIMALS and a whole number from 0 to 5")
			}
			config["chartDecimals"] = decimals
			return nil
		}, true
	case words.keyword("VALUE", "COLOR"):
		color, ok := words.oneOf(mcpChartTextColors...)
		return func() error {
			if !ok {
				return change.unreadable("CHART … VALUE COLOR", "VALUE COLOR "+strings.Join(mcpChartTextColors, ", "))
			}
			delete(config, "chartTextColor")
			if color != "default" {
				config["chartTextColor"] = color
			}
			return nil
		}, true
	}
	return nil, false
}

// mcpChartClauses are the words CHART takes for a chart's format, each the
// API configuration's field it sets, so a clause reads as the API's field
// does and fetch's chartFormat shows it back (mcp_view_fetch_vocabulary.go).
// A choice takes one of the field's choices, a flag true or false, a number
// a number or auto.
var mcpChartClauses = []struct {
	words       []string
	field, kind string
}{
	{[]string{"COLOR", "BY", "VALUE"}, "color_by_value", "flag"},
	{[]string{"LEGEND"}, "legend_position", "choice"},
	{[]string{"AXIS", "LABELS"}, "axis_labels", "choice"},
	{[]string{"GRID", "LINES"}, "grid_lines", "choice"},
	{[]string{"GROUP", "STYLE"}, "group_style", "choice"},
	{[]string{"DATA", "LABELS"}, "show_data_labels", "flag"},
	{[]string{"LABELS"}, "donut_labels", "choice"},
	{[]string{"CUMULATIVE"}, "cumulative", "flag"},
	{[]string{"SMOOTH", "LINE"}, "smooth_line", "flag"},
	{[]string{"HIDE", "FILL"}, "hide_line_fill_area", "flag"},
	{[]string{"HIDE", "TITLE"}, "hide_title", "flag"},
	{[]string{"Y", "MIN"}, "y_axis_min", "number"},
	{[]string{"Y", "MAX"}, "y_axis_max", "number"},
}

// chartFormatClause reads one of CHART's format clauses, ready to write; one
// that sets the chart's default says so in the answer's warnings.
func (change *mcpViewConfigure) chartFormatClause(words *mcpViewWords) (func() error, bool) {
	for _, clause := range mcpChartClauses {
		if !words.keyword(clause.words...) {
			continue
		}
		said := strings.Join(clause.words, " ")
		name := "CHART … " + said
		switch clause.kind {
		case "flag":
			value, ok := words.oneOf("true", "false")
			return func() error {
				if !ok {
					return change.unreadable(name, name+" true or false")
				}
				return change.chartFormatField(said, clause.field, value == "true")
			}, true
		case "number":
			raw, _ := words.value()
			return func() error {
				if strings.EqualFold(raw, "auto") {
					return change.chartFormatField(said, clause.field, nil)
				}
				number, err := strconv.ParseFloat(raw, 64)
				if err != nil {
					return change.unreadable(name, name+" and a number, or auto")
				}
				return change.chartFormatField(said, clause.field, number)
			}, true
		}
		choices := notionViewChartChoices[clause.field].choices
		value, ok := words.oneOf(choices...)
		return func() error {
			if !ok {
				return change.unreadable(name, name+" "+strings.Join(choices, ", "))
			}
			return change.chartFormatField(said, clause.field, value)
		}, true
	}
	return nil, false
}

// referenceLine reads REFERENCE LINE 5 "label" [COLOR red] [SOLID | DASH].
func (change *mcpViewConfigure) referenceLine(words *mcpViewWords) (map[string]any, error) {
	form := `REFERENCE LINE 5 "label" [COLOR red] [SOLID | DASH]`
	raw, _ := words.value()
	at, err := strconv.ParseFloat(raw, 64)
	label, labeled := words.quoted()
	if err != nil || !labeled {
		return nil, change.unreadable("CHART … REFERENCE LINE", form)
	}
	line := map[string]any{"value": at, "label": label, "color": notionViewChartLineColors[0], "dash_style": "dash"}
	if words.keyword("COLOR") {
		color, ok := words.oneOf(notionViewChartLineColors...)
		if !ok {
			return nil, change.unreadable("CHART … REFERENCE LINE … COLOR", "COLOR "+strings.Join(notionViewChartLineColors, ", "))
		}
		line["color"] = color
	}
	if style, ok := words.oneOf("solid", "dash"); ok {
		line["dash_style"] = style
	}
	return line, nil
}

// officialChartField writes one field of a chart's API configuration.
func (change *mcpViewConfigure) officialChartField(field string, value any) error {
	encoded, err := json.Marshal(map[string]any{"type": graphViewTypeChart, field: value})
	if err != nil {
		return err
	}
	return change.scope.applyOfficialViewConfiguration(graphViewTypeChart, encoded, change.config)
}
