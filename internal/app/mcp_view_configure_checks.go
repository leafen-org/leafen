package app

import (
	"fmt"
	"slices"
	"strings"
)

// What a configure string says that its directives alone don't check, and
// what it tells of directives it took that fetch doesn't show
// (mcp_view_configure.go). Notion's create-view and update-view refuse a
// gallery given both CARD SIZE and COVER … SIZE, and WRAP PROPERTIES on a
// gallery that keeps no list of its properties with no SHOW in the same call
// (seen 2026-11-16), and Leafen's do too, in its own words; a setting Leafen's own directives put at
// its default is saved, and the answer's warnings say why fetch leaves it
// out (2026-11-16: an agent read LEGEND bottom, DATA LABELS true and GRID
// LINES horizontal as dropped).

const (
	mcpViewSaidCardSize       = "CARD SIZE"
	mcpViewSaidCoverSize      = "COVER … SIZE"
	mcpViewSaidWrapProperties = "WRAP PROPERTIES"
	mcpViewSaidProperties     = "SHOW"
)

func (change *mcpViewConfigure) say(directive string) {
	if change.said == nil {
		change.said = map[string]bool{}
	}
	change.said[directive] = true
}

// settle checks what the whole string said, once its directives are read.
func (change *mcpViewConfigure) settle() error {
	if change.viewType != graphViewTypeGallery {
		return nil
	}
	if change.said[mcpViewSaidCardSize] && change.said[mcpViewSaidCoverSize] {
		return mcpInvalid("A gallery view takes CARD SIZE or COVER SIZE, not both.")
	}
	if change.said[mcpViewSaidWrapProperties] && !change.said[mcpViewSaidProperties] && !mcpViewKeepsPropertyList(change.config) {
		return mcpInvalid("WRAP PROPERTIES needs the gallery's shown properties saved: name them with SHOW \"Property\" in the same call.")
	}
	return nil
}

// mcpViewKeepsPropertyList tells a view whose properties were set, on the
// screen or by SHOW and HIDE, from one that shows what its layout starts with.
func mcpViewKeepsPropertyList(config map[string]any) bool {
	order, _ := config["propertyOrder"].([]any)
	shown, _ := config["visibleProperties"].([]any)
	return len(order) > 0 || len(shown) > 0
}

// listShown keeps, in a view that lists the properties it shows as a new
// board's or gallery's does (mcpTitleAloneLayouts), the list SHOW and HIDE
// leave, so a property made later stays off its cards as Notion's does
// (seen 2026-11-16: a board given SHOW, then a property added, then CARD
// LAYOUT changed, still showed the two it was given).
func (change *mcpViewConfigure) listShown(shown []string) {
	if !slices.Contains(mcpTitleAloneLayouts, change.viewType) {
		return
	}
	listed := []any{}
	for _, id := range shown {
		if change.scope.byID[id].NativeType != graphValueTypeTitle {
			listed = append(listed, id)
		}
	}
	change.config["visibleProperties"] = listed
}

// mcpChartClauseFetchNames are the names chartFormat gives what CHART's
// format clauses set (mcpChartFormatNames, mcpChartFormat).
var mcpChartClauseFetchNames = map[string]string{
	"color_by_value": "weightColorValue", "legend_position": "legendPosition", "axis_labels": "axisLabel",
	"grid_lines": "axisGridLine", "group_style": "axisGroupStyle", "show_data_labels": "axisShowDataLabels",
	"donut_labels": "donutDataLabels", "cumulative": "axisCumulative", "smooth_line": "smoothLine",
	"hide_line_fill_area": "hideLineFillArea", "hide_title": "hideTitle", "y_axis_min": "yAxisMin", "y_axis_max": "yAxisMax",
}

// chartFormatField writes a format clause's field and warns when it is the
// chart's default.
func (change *mcpViewConfigure) chartFormatField(clause, field string, value any) error {
	if err := change.officialChartField(field, value); err != nil {
		return err
	}
	change.warnDefaultChartFormat(clause, field, value)
	return nil
}

// warnDefaultChartFormat says, of a format clause the chart took, when what it
// set is the chart's default, which chartFormat leaves out as Notion's record
// does.
func (change *mcpViewConfigure) warnDefaultChartFormat(clause string, field string, value any) {
	name, named := mcpChartClauseFetchNames[field]
	if _, shown := mcpChartFormat(change.config)[name]; !named || shown {
		return
	}
	said := fmt.Sprint(value)
	if value == nil {
		said = "auto"
	}
	change.warnings = append(change.warnings, fmt.Sprintf(
		"%s %s is saved; it is the chart's default, so fetch's chartFormat leaves %s out", clause, said, name))
}

// mcpChartOnlyClauses are words that read only after CHART on its line.
var mcpChartOnlyClauses = [][]string{
	{"AGGREGATE"}, {"HEIGHT"}, {"STACK", "BY"}, {"CAPTION"}, {"REFERENCE", "LINE"}, {"COLOR", "RULE"},
	{"SHOW", "VALUE", "LABEL"}, {"VALUE", "LABEL"}, {"HIDE", "VALUE"}, {"HIDE", "NEGATIVE"}, {"NUMBER", "FORMAT"},
	{"DECIMALS"}, {"VALUE", "COLOR"},
}

// misplacedChartClause names a directive that is one of CHART's clauses,
// written as a directive of its own.
func misplacedChartClause(words *mcpViewWords) string {
	candidates := slices.Clone(mcpChartOnlyClauses)
	for _, clause := range mcpChartClauses {
		candidates = append(candidates, clause.words)
	}
	for _, candidate := range candidates {
		probe := &mcpViewWords{tokens: words.tokens, at: words.at}
		if probe.keyword(candidate...) {
			return strings.Join(candidate, " ")
		}
	}
	return ""
}

func (change *mcpViewConfigure) unknownDirective(words *mcpViewWords) error {
	if clause := misplacedChartClause(words); clause != "" {
		return mcpInvalid("configure: %s is one of CHART's clauses: write it after CHART on the same line, as CHART column %s … (%s gives the syntax)", clause, clause, mcpViewDSLSpecURI)
	}
	return mcpInvalid("configure: %q isn't a directive (%s gives the syntax)", words.tokens[0].text, mcpViewDSLSpecURI)
}
