package app

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// What a configure string says goes into a view's settings where the screen
// and the API keep the same things, through the same readings of properties,
// groups, covers and chart settings (notion_view_configuration.go).

//go:embed mcp_view_dsl_spec.md
var mcpViewDSLSpec string

const mcpViewDSLSpecURI = "notion://docs/view-dsl-spec"

// mcpViewConfigure carries one configure string's reading of a view.
type mcpViewConfigure struct {
	app      core.App
	scope    notionViewFilterScope
	viewType string
	config   map[string]any
	clock    *time.Location // whose days a date filter's words name (person_clock.go)
	form     notionViewFormSettingsInput
	filters  []*databaseQueryFilterGroup
	// said are the directives its end checks read, warnings what it tells of
	// those it took that fetch doesn't show (mcp_view_configure_checks.go).
	said     map[string]bool
	warnings []string
}

// applyMCPViewConfigure writes a configure string into a view's settings, and
// gives what it says of a form's responses, which live on the form's link,
// and its warnings; clock is the connection's (mcpAuthority.clock).
func applyMCPViewConfigure(
	app core.App, scope notionViewFilterScope, viewType string, config map[string]any, raw string, updating bool, clock *time.Location,
) (notionViewFormSettingsInput, []string, error) {
	directives, err := mcpViewDirectives(raw)
	if err != nil {
		return notionViewFormSettingsInput{}, nil, err
	}
	change := mcpViewConfigure{app: app, scope: scope, viewType: viewType, config: config, clock: clock}
	// A CLEAR comes before what the same string sets.
	for _, tokens := range directives {
		words := &mcpViewWords{tokens: tokens}
		if !words.keyword("CLEAR") {
			continue
		}
		if !updating {
			return change.form, nil, mcpInvalid("configure: CLEAR is for notion-update-view")
		}
		if err := change.clear(words); err != nil {
			return change.form, nil, err
		}
	}
	for _, tokens := range directives {
		words := &mcpViewWords{tokens: tokens}
		if words.keyword("CLEAR") {
			continue
		}
		if err := change.directive(words); err != nil {
			return change.form, nil, err
		}
		if !words.done() {
			return change.form, nil, mcpInvalid("configure: %q doesn't read after %s", words.tokens[words.at].text, mcpViewSaid(words))
		}
	}
	if err := change.settle(); err != nil {
		return change.form, nil, err
	}
	if len(change.filters) > 0 {
		delete(config, "filters")
		delete(config, "filterConjunction")
		config["filterGroup"] = mcpViewFilterGroups(change.filters)
	}
	return change.form, change.warnings, nil
}

// mcpViewFilterGroups joins a configure string's FILTERs as Notion keeps
// them (seen 2026-11-09): one "and" of all their conditions, an "or" among
// them as its own group.
func mcpViewFilterGroups(filters []*databaseQueryFilterGroup) *databaseQueryFilterGroup {
	joined := &databaseQueryFilterGroup{Conjunction: databaseQueryConjunctionAnd}
	for _, filter := range filters {
		terms := databaseQueryFilterTerms(filter)
		if filter.Conjunction != databaseQueryConjunctionAnd && len(terms) > 1 {
			joined.Groups = append(joined.Groups, *filter)
			joined.Order = append(joined.Order, databaseQueryFilterTermGroup)
			continue
		}
		for _, term := range terms {
			if term.group {
				joined.Groups = append(joined.Groups, filter.Groups[term.index])
				joined.Order = append(joined.Order, databaseQueryFilterTermGroup)
			} else {
				joined.Filters = append(joined.Filters, filter.Filters[term.index])
				joined.Order = append(joined.Order, databaseQueryFilterTermFilter)
			}
		}
	}
	plainDatabaseQueryFilterOrder(joined)
	return joined
}

func mcpViewSaid(words *mcpViewWords) string {
	said := make([]string, 0, words.at)
	for _, token := range words.tokens[:words.at] {
		said = append(said, token.text)
	}
	return strconv.Quote(strings.Join(said, " "))
}

func (change *mcpViewConfigure) unreadable(name, form string) error {
	return mcpInvalid("configure: %s is written %s (%s gives the syntax)", name, form, mcpViewDSLSpecURI)
}

func (change *mcpViewConfigure) only(name string, types ...string) error {
	if slices.Contains(types, change.viewType) {
		return nil
	}
	return mcpInvalid("configure: %s is for %s views, not a %s view", name, strings.Join(types, " and "), change.viewType)
}

func (change *mcpViewConfigure) property(name string) (notionViewProperty, error) {
	property, found := change.scope.byName[name]
	if !found {
		property, found = change.scope.named(name)
	}
	if !found {
		names := make([]string, 0, len(change.scope.ordered))
		for _, candidate := range change.scope.ordered {
			names = append(names, candidate.Name)
		}
		return property, mcpInvalid("configure: this database has no property %q; it has %s", name, strings.Join(names, ", "))
	}
	return property, nil
}

func (change *mcpViewConfigure) clear(words *mcpViewWords) error {
	config := change.config
	switch {
	case words.keyword("QUICK", "FILTER"):
		if words.done() {
			delete(config, "quickFilters")
			return nil
		}
		names, ok := words.names()
		if !ok {
			return change.unreadable("CLEAR QUICK FILTER", `CLEAR QUICK FILTER "Property", "Property"`)
		}
		for _, name := range names {
			property, err := change.property(name)
			if err != nil {
				return err
			}
			if err := change.scope.applyOfficialQuickFilters(json.RawMessage(`{`+strconv.Quote(property.ID)+`:null}`), config); err != nil {
				return err
			}
		}
	case words.keyword("FILTER"):
		for _, key := range []string{"filterGroup", "filters", "filterConjunction"} {
			delete(config, key)
		}
	case words.keyword("SORT"):
		delete(config, "sorts")
	case words.keyword("SUB", "GROUP", "BY"):
		delete(config, "subGroupBy")
		delete(config, "subGroup")
	case words.keyword("GROUP", "BY"):
		if change.viewType == graphViewTypeBoard {
			return mcpInvalid("configure: a board always groups; name another property with GROUP BY instead")
		}
		delete(config, "groupBy")
		delete(config, "groupByMode")
		delete(config, "chartGroupBy")
	case words.keyword("ICON"):
		delete(config, "icon")
	case words.keyword("REFERENCE", "LINES"):
		delete(config, "chartReferenceLines")
	case words.keyword("COLOR", "RULES"):
		delete(config, "chartColorRules")
		delete(config, "chartDynamicColor")
	default:
		return change.unreadable("CLEAR", "CLEAR FILTER, QUICK FILTER, SORT, GROUP BY, SUB GROUP BY, ICON, REFERENCE LINES or COLOR RULES")
	}
	if !words.done() {
		return mcpInvalid("configure: %q doesn't read after %s", words.tokens[words.at].text, mcpViewSaid(words))
	}
	return nil
}

func (change *mcpViewConfigure) directive(words *mcpViewWords) error {
	if handled, err := change.groupSetting(words); handled {
		return err
	}
	if words.keyword("ROW", "ORDER") {
		return change.rowOrder(words)
	}
	config := change.config
	switch {
	case words.keyword("QUICK", "FILTER"):
		return change.quickFilters(words)
	case words.keyword("FILTER"):
		expression, ok := mcpViewFilterExpression(words, 1)
		if !ok {
			return change.unreadable("FILTER", `FILTER "Property" operator value`)
		}
		group, err := change.filterGroup(expression)
		if err != nil {
			return err
		}
		plainDatabaseQueryFilterOrder(group)
		change.filters = append(change.filters, group)
	case words.keyword("SORT", "BY"):
		return change.sorts(words)
	case words.keyword("SUB", "GROUP", "BY"):
		if err := change.only("SUB GROUP BY", graphViewTypeBoard); err != nil {
			return err
		}
		return change.grouping(words, "subGroupBy")
	case words.keyword("GROUP", "BY"):
		if change.viewType == graphViewTypeChart {
			return change.grouping(words, "chartGroupBy")
		}
		if err := change.only("GROUP BY", graphViewTypeTable, graphViewTypeBoard, graphViewTypeList, graphViewTypeGallery, graphViewTypeTimeline); err != nil {
			return err
		}
		return change.grouping(words, "groupBy")
	case words.keyword("CARD", "LAYOUT"):
		if err := change.only("CARD LAYOUT", graphViewTypeBoard, graphViewTypeGallery); err != nil {
			return err
		}
		layout, ok := words.oneOf("default", "compact")
		if !ok {
			return change.unreadable("CARD LAYOUT", "CARD LAYOUT default or compact")
		}
		delete(config, "cardLayout")
		if layout == "compact" {
			config["cardLayout"] = layout
		}
	case words.keyword("CARD", "SIZE"):
		if err := change.only("CARD SIZE", graphViewTypeGallery); err != nil {
			return err
		}
		size, ok := words.oneOf("small", "medium", "large")
		if !ok {
			return change.unreadable("CARD SIZE", "CARD SIZE small, medium or large")
		}
		config["cardSize"] = size
		change.say(mcpViewSaidCardSize)
	case words.keyword("CALENDAR", "BY"):
		if err := change.only("CALENDAR BY", graphViewTypeCalendar); err != nil {
			return err
		}
		return change.dateProperty(words, "calendarBy", "CALENDAR BY")
	case words.keyword("TIMELINE", "BY"):
		if err := change.only("TIMELINE BY", graphViewTypeTimeline); err != nil {
			return err
		}
		if err := change.dateProperty(words, "timelineBy", "TIMELINE BY"); err != nil {
			return err
		}
		delete(config, "timelineEndBy")
		if words.keyword("TO") {
			return change.dateProperty(words, "timelineEndBy", "TIMELINE BY … TO")
		}
	case words.keyword("MAP", "BY"):
		if err := change.only("MAP BY", graphViewTypeMap); err != nil {
			return err
		}
		name, ok := words.quoted()
		if !ok {
			return change.unreadable("MAP BY", `MAP BY "Property"`)
		}
		property, err := change.property(name)
		if err != nil {
			return err
		}
		config["mapBy"] = property.ID
	case words.keyword("SHOW"):
		names, ok := words.names()
		if !ok {
			return change.unreadable("SHOW", `SHOW "Property", "Property"`)
		}
		change.say(mcpViewSaidProperties)
		return change.showProperties(names)
	case words.keyword("HIDE"):
		names, ok := words.names()
		if !ok {
			return change.unreadable("HIDE", `HIDE "Property", "Property"`)
		}
		change.say(mcpViewSaidProperties)
		return change.hideProperties(names)
	case words.keyword("COVER"):
		return change.cover(words)
	case words.keyword("WRAP", "CELLS"):
		if err := change.only("WRAP CELLS", graphViewTypeTable); err != nil {
			return err
		}
		return change.flag(words, "WRAP CELLS", "wrap")
	case words.keyword("WRAP", "PROPERTIES"):
		if err := change.only("WRAP PROPERTIES", graphViewTypeGallery); err != nil {
			return err
		}
		change.say(mcpViewSaidWrapProperties)
		return change.flag(words, "WRAP PROPERTIES", "wrap")
	case words.keyword("FREEZE", "COLUMNS"):
		if err := change.only("FREEZE COLUMNS", graphViewTypeTable); err != nil {
			return err
		}
		raw, _ := words.value()
		count, err := strconv.Atoi(raw)
		if err != nil || count < 0 || count > 100 {
			return change.unreadable("FREEZE COLUMNS", "FREEZE COLUMNS and a number")
		}
		delete(config, "frozenColumns")
		if count > 0 {
			config["frozenColumns"] = count
		}
	case words.keyword("ICON"):
		return change.icon(words)
	case words.keyword("WIDTH"):
		return change.widths(words)
	case words.keyword("CHART"):
		if err := change.only("CHART", graphViewTypeChart); err != nil {
			return err
		}
		return change.chart(words)
	case words.keyword("FORM"):
		if err := change.only("FORM", graphViewTypeForm); err != nil {
			return err
		}
		return change.formSetting(words)
	default:
		if words.done() {
			return nil
		}
		return change.unknownDirective(words)
	}
	return nil
}

func (change *mcpViewConfigure) flag(words *mcpViewWords, name, key string) error {
	value, ok := words.oneOf("true", "false")
	if !ok {
		return change.unreadable(name, name+" true or false")
	}
	delete(change.config, key)
	if value == "true" {
		change.config[key] = true
	}
	return nil
}

func (change *mcpViewConfigure) dateProperty(words *mcpViewWords, key, name string) error {
	raw, ok := words.quoted()
	if !ok {
		return change.unreadable(name, name+` "Property"`)
	}
	property, err := change.property(raw)
	if err != nil {
		return err
	}
	if !notionViewDateProperty(property) {
		return mcpInvalid("configure: %s takes a date property; %q is %s", name, raw, property.OfficialType)
	}
	change.config[key] = property.ID
	return nil
}

// grouping reads a property and how it groups into a view's group settings.
func (change *mcpViewConfigure) grouping(words *mcpViewWords, key string) error {
	name, ok := words.quoted()
	if !ok {
		return change.unreadable("GROUP BY", `GROUP BY "Property" [BY OPTION | BY GROUP | BY day…] [SHOW EMPTY | HIDE EMPTY]`)
	}
	property, err := change.property(name)
	if err != nil {
		return err
	}
	byGroup, byOption := words.keyword("BY", "GROUP"), false
	if !byGroup {
		byOption = words.keyword("BY", "OPTION")
	}
	if (byGroup || byOption) && property.NativeType != graphValueTypeStatus && property.NativeType != graphValueTypeRollup {
		return mcpInvalid("configure: BY OPTION and BY GROUP are for status properties; leave them out for %q", name)
	}
	// A date groups its days by a unit, as the API's group_by and fetch's
	// groupBy name it.
	dateBy := ""
	if !byGroup && !byOption && words.keyword("BY") {
		unit, ok := words.oneOf(notionViewDateGroupings...)
		// A formula's or rollup's dates too, as the screen's group menu offers them.
		if !ok || !notionViewGroupsByDates(property) || key == "subGroupBy" {
			return mcpInvalid("configure: a date property groups BY relative, day, week, month or year; %q doesn't", name)
		}
		dateBy = unit
	}
	hide, show := words.keyword("HIDE", "EMPTY"), false
	if !hide {
		show = words.keyword("SHOW", "EMPTY")
	}
	config := change.config
	if config[key] != property.ID {
		switch key {
		case "chartGroupBy":
			delete(config, "chartGroupOrder")
			delete(config, "chartHiddenGroups")
			delete(config, "chartGroupSort")
		case "groupBy":
			delete(config, "groupSort")
			delete(config, "groupOrder")
			delete(config, "hiddenGroups")
		case "subGroupBy":
			// Another property's sub-groups start afresh, as a person's pick does.
			delete(config, "subGroup")
		}
	}
	config[key] = property.ID
	switch key {
	case "groupBy":
		if property.NativeType == graphValueTypeStatus {
			delete(config, "groupByMode")
			if byGroup {
				config["groupByMode"] = "group"
			} else if byOption {
				config["groupByMode"] = "option"
			}
		}
		if hide || show {
			config["hideEmptyGroups"] = hide
		}
	case "subGroupBy":
		sub, _ := config["subGroup"].(map[string]any)
		if sub == nil {
			sub = map[string]any{}
		}
		delete(sub, "hideEmptyGroups")
		if show {
			sub["hideEmptyGroups"] = false
		}
		if len(sub) > 0 {
			config["subGroup"] = sub
		}
	case "chartGroupBy":
		delete(config, "chartHideEmptyGroups")
		delete(config, notionChartAxisHidingKey)
		if hide {
			config["chartHideEmptyGroups"] = true
		}
		// A status axis by its groups or options (notionViewChartUnitKeys).
		delete(config, "chartStatusBy")
		if byGroup && property.NativeType == graphValueTypeStatus {
			config["chartStatusBy"] = "group"
		}
	}
	switch {
	case dateBy == "":
	case key == "chartGroupBy":
		config["chartDateBy"] = dateBy
	case dateBy == "relative":
		delete(config, "dateGroupBy")
	default:
		config["dateGroupBy"] = dateBy
	}
	return nil
}

func (change *mcpViewConfigure) sorts(words *mcpViewWords) error {
	sorts := []databaseQuerySort{}
	for {
		name, ok := words.quoted()
		if !ok {
			return change.unreadable("SORT BY", `SORT BY "Property" [ASC|DESC], "Property" [ASC|DESC]`)
		}
		property, err := change.property(name)
		if err != nil {
			return err
		}
		direction := databaseQuerySortAscending
		if words.keyword("DESC") {
			direction = databaseQuerySortDescending
		} else {
			words.keyword("ASC")
		}
		sorts = append(sorts, databaseQuerySort{PropertyID: property.ID, Direction: direction})
		if !words.mark(",") {
			break
		}
	}
	change.config["sorts"] = sorts
	return nil
}
