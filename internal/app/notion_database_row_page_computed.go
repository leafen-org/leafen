package app

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// loadNotionDatabaseRowPageComputedValues computes a row page's formulas and
// rollups as the API caller reads them: related rows on pages both the
// caller and their grant can read.
func loadNotionDatabaseRowPageComputedValues(
	app core.App,
	identity notionOAuthBearerIdentity,
	authority databaseRowPageAuthority,
	properties []storedNotionDatabaseRowPageProperty,
) (map[string]databaseComputedValue, bool, error) {
	return loadNotionDatabaseRowPageComputedValuesWith(app, identity, authority, properties, nil)
}

// loadNotionDatabaseRowPageComputedValuesWith reads the query context through
// contexts, which the rows of one data source a rollup shows share
// (notionRollupRelatedTargets); a nil memo loads it for the row.
func loadNotionDatabaseRowPageComputedValuesWith(
	app core.App,
	identity notionOAuthBearerIdentity,
	authority databaseRowPageAuthority,
	properties []storedNotionDatabaseRowPageProperty,
	contexts *databaseQueryContextMemo,
) (map[string]databaseComputedValue, bool, error) {
	// A mention of a page the caller can't reach reads "Untitled", and a
	// related page its grant doesn't reach reads no rows, as its queries
	// read them (database_query_reader_mentions.go).
	return loadDatabaseRowPageComputedValuesFor(
		app, identity.PrincipalID, notionIdentityQueryMentions(identity), authority, properties, contexts, nil,
	)
}

// loadDatabaseRowPageComputedValuesFor computes a row page's formulas and
// rollups for a reader: principalID's, with mentions its reading of the
// pages they name and the related rows they read, as its queries read them
// — an API connection's (notionIdentityQueryMentions) or an agent's
// (mcpQueryMentions) — and their dates (today(), formatDate) on clock, as
// its queries read them too; nil reads UTC, as the Public API does. True,
// with no values, when they read more related rows than a row read on its
// own works out (databaseRowDependencyClosureRows).
func loadDatabaseRowPageComputedValuesFor(
	app core.App,
	principalID string,
	mentions databaseQueryMentions,
	authority databaseRowPageAuthority,
	properties []storedNotionDatabaseRowPageProperty,
	contexts *databaseQueryContextMemo,
	clock *time.Location,
) (map[string]databaseComputedValue, bool, error) {
	hasComputed := false
	for _, property := range properties {
		if property.Type == graphValueTypeFormula || property.Type == graphValueTypeRollup {
			hasComputed = true
			break
		}
	}
	if !hasComputed {
		return map[string]databaseComputedValue{}, false, nil
	}
	context, err := contexts.load(app, storedDatabaseQueryJob{
		PrincipalID: principalID, WorkspaceID: authority.WorkspaceID,
		ContentOwnerID: authority.OwnerID, PageID: authority.ContainerPageID,
		DataSourceID: authority.DataSourceID, mentions: mentions,
	})
	if err != nil {
		return nil, false, err
	}
	context.FormulaTime.Location = clock
	// The row itself in the trash too, as its page is read (notion_trash_reads.go).
	rows, err := loadDatabaseQueryRowsByIDReach(
		app, authority.OwnerID, []string{authority.ContainerPageID}, []string{authority.RowID}, true,
	)
	if err != nil {
		return nil, false, err
	}
	row, found := rows[authority.RowID]
	if !found {
		return nil, false, errNotionDatabaseRowPageProjectionInvalid
	}
	// The related rows its values read, through relations at every remove,
	// in one go (database_row_dependency_closure.go).
	dependencies, within, err := loadDatabaseRowDependencyClosure(
		app, authority.OwnerID, authority.ContainerPageID, row, context, nil,
	)
	if err != nil {
		return nil, false, err
	}
	if !within {
		return map[string]databaseComputedValue{}, true, nil
	}
	computed, err := computeDatabaseRowValues(row, dependencies, context)
	if err != nil {
		return nil, false, err
	}
	return computed, false, nil
}

func notionDatabaseRowPageFormulaValue(
	stored storedNotionDatabaseRowPageProperty,
	value databaseComputedValue,
	unsupported bool,
) (map[string]any, error) {
	config, err := decodeDatabaseComputedConfig(stored.Config)
	if err != nil {
		return nil, err
	}
	// A formula not written yet has no value, as Notion's (seen 2026-10-04).
	if strings.TrimSpace(config.Formula) == "" && strings.TrimSpace(config.Expression) == "" {
		return map[string]any{"type": "string", "string": nil}, nil
	}
	if unsupported {
		return notionDatabaseRowPageUnsupportedComputed(), nil
	}
	decoded, err := notionDatabaseRowPageComputedRaw(value)
	if err != nil {
		return nil, err
	}
	switch typed := decoded.(type) {
	case nil:
		// An empty result is its kind with no value, as Notion's (seen
		// 2026-11-12: prop() of an empty text, select, number or date).
		kind := notionFormulaResultType(stored.FormulaResult)
		return map[string]any{"type": kind, kind: nil}, nil
	case string:
		// A number or date formula's value that is none (1 / 0, 1 / an empty
		// number, a range of no dates) is kept as "" and goes out as its
		// kind with no value, as Notion's (seen 2026-11-14:
		// {"type":"number","number":null}, {"type":"date","date":null}).
		if kind := notionFormulaResultType(stored.FormulaResult); typed == "" && (kind == "number" || kind == "date") {
			return map[string]any{"type": kind, kind: nil}, nil
		}
		// Notion serves a formula's text without the spaces around it, though
		// the formula still counts them (seen 2026-11-04).
		return map[string]any{"type": "string", "string": strings.TrimSpace(typed)}, nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return notionDatabaseRowPageUnsupportedComputed(), nil
		}
		return map[string]any{"type": "number", "number": typed}, nil
	case bool:
		return map[string]any{"type": "boolean", "boolean": typed}, nil
	case map[string]any:
		if date, ok := notionDatabaseRowPageComputedDate(typed); ok {
			return map[string]any{"type": "date", "date": date}, nil
		}
		if _, reference := typed["kind"]; reference {
			return map[string]any{"type": "string", "string": value.Text}, nil
		}
		// Rich text reads as the text it shows, mentions with their "@".
		if _, rich := typed["rich_text"]; rich {
			return map[string]any{"type": "string", "string": strings.TrimSpace(value.Text)}, nil
		}
		return notionDatabaseRowPageUnsupportedComputed(), nil
	case []any:
		text, ok := notionFormulaListText(databaseFormulaInput(typed).([]any))
		if !ok {
			return map[string]any{"type": "string", "string": nil}, nil
		}
		return map[string]any{"type": "string", "string": text}, nil
	default:
		return notionDatabaseRowPageUnsupportedComputed(), nil
	}
}

// withNotionFormulaResults sets each formula's result kind from its
// expression, as a view's filters read it (notionViewFormulaResults).
func withNotionFormulaResults(rows []storedNotionDatabaseRowPageProperty) {
	properties := make([]notionViewProperty, len(rows))
	expressions := make([]string, len(rows))
	formulas := false
	for index, row := range rows {
		nativeType := normalizeNotionDatabaseRowPagePropertyType(row.Type)
		properties[index] = notionViewProperty{
			ID: row.ID, Name: row.Name, NativeType: nativeType,
			OfficialType: notionDatabaseRowPageOfficialPropertyType(nativeType),
		}
		expressions[index] = notionViewFormulaExpression(nativeType, row.Config)
		formulas = formulas || nativeType == graphValueTypeFormula
	}
	if !formulas {
		return
	}
	notionViewFormulaResults(properties, expressions)
	for index := range rows {
		rows[index].FormulaResult = properties[index].FormulaResult
	}
}

// notionFormulaResultType is the type a formula's value goes out as for the
// kind of its result; text for one unknown.
func notionFormulaResultType(kind string) string {
	switch kind {
	case "number", "date":
		return kind
	case "checkbox":
		return "boolean"
	}
	return "string"
}

func notionDatabaseRowPageRollupValue(
	stored storedNotionDatabaseRowPageProperty,
	value databaseComputedValue,
	unsupported bool,
) (map[string]any, error) {
	config, err := decodeDatabaseComputedConfig(stored.Config)
	if err != nil {
		return nil, err
	}
	function := notionRollupReadFunction(config)
	if unsupported {
		result := notionDatabaseRowPageUnsupportedComputed()
		result["function"] = function
		return result, nil
	}
	decoded, err := notionDatabaseRowPageComputedRaw(value)
	if err != nil {
		return nil, err
	}
	if function == "show_original" || function == "show_unique" {
		// One not set up shows nothing, an empty list as Notion's (seen
		// 2026-10-04); the related values of one that is are read from the
		// related pages (notionRollupShownPageValue).
		if decoded == nil {
			return map[string]any{"type": "array", "array": []any{}, "function": function}, nil
		}
		result := notionDatabaseRowPageUnsupportedComputed()
		result["function"] = function
		return result, nil
	}
	result := map[string]any{"function": function}
	dated := function == "earliest_date" || function == "latest_date" || function == "date_range"
	switch typed := decoded.(type) {
	case nil:
		// A date one over no dates is a date of none (seen 2026-11-11).
		if dated {
			result["type"], result["date"] = "date", nil
			break
		}
		result["type"], result["number"] = "number", nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			result = notionDatabaseRowPageUnsupportedComputed()
			result["function"] = function
			return result, nil
		}
		result["type"], result["number"] = "number", typed
	case map[string]any:
		date, ok := notionDatabaseRowPageComputedDate(typed)
		if !ok {
			result = notionDatabaseRowPageUnsupportedComputed()
			result["function"] = function
			return result, nil
		}
		// A date range's ends are times in UTC, a day's its midnight, as
		// Notion's (seen 2026-11-11).
		if function == "date_range" {
			date = notionRollupRangeEnds(date)
		}
		result["type"], result["date"] = "date", date
	default:
		result = notionDatabaseRowPageUnsupportedComputed()
		result["function"] = function
	}
	return result, nil
}

func notionDatabaseRowPageComputedRaw(value databaseComputedValue) (any, error) {
	var decoded any
	if len(value.Value) == 0 || json.Unmarshal(value.Value, &decoded) != nil {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	return decoded, nil
}

func notionDatabaseRowPageComputedDate(value map[string]any) (notionDateObject, bool) {
	if !graphDateValue(value) {
		return nil, false
	}
	start, _ := value["start"].(string)
	end, _ := value["end"].(string)
	zone, _ := value["timeZone"].(string)
	return notionDatePropertyObject(start, end, zone), true
}

func notionDatabaseRowPageUnsupportedComputed() map[string]any {
	return map[string]any{"type": "unsupported", "unsupported": map[string]any{}}
}

// notionRollupRangeEnds is a date range with its ends as UTC times.
func notionRollupRangeEnds(date notionDateObject) notionDateObject {
	for _, key := range []string{"start", "end"} {
		text, _ := date[key].(string)
		if text == "" {
			continue
		}
		layout := time.RFC3339Nano
		if len(text) == len("2006-01-02") {
			layout = time.DateOnly
		}
		if parsed, err := time.Parse(layout, text); err == nil {
			date[key] = parsed.UTC().Format("2006-01-02T15:04:05.000+00:00")
		}
	}
	return date
}

// notionRollupReadFunction is the function a rollup reads with through the
// API: one not set up yet shows its related values, as a new one does, and
// one counting them per group reads as them too, as Notion's (seen
// 2026-11-11: an array of the values, "show_original"); a percent or count
// per group naming a status group reads as that group's share or count
// (seen 2026-11-12: "number": 0.5, "percent_per_group"; "number": 2,
// "count_per_group").
func notionRollupReadFunction(config databaseComputedConfig) string {
	_, _, function := config.rollupFields()
	switch {
	case function == "":
		return "show_original"
	case config.rollupCalculation() == databaseRollupPercentInGroup,
		config.rollupCalculation() == databaseRollupCountInGroup:
		return function
	case function == "count_per_group" || function == "percent_per_group":
		return "show_original"
	}
	return function
}

// notionFormulaListText is a formula's list as Notion's API serves it (seen
// 2026-11-12 under 한지테스트페이지): its items with lists within it flat and
// its empty items left out, nothing left being no value. People alone, or
// pages alone, are their names between spaces, an untitled page left out;
// other items are between commas, each as it is joined to text: a person or
// a page as its mention ("@" and its name, an untitled page "@New page"), a
// date as its mention written in UTC, the API's formulas having no viewer's
// time zone ("@January 2, 2026 10:30 AM (GMT+9) → 12:00 PM").
func notionFormulaListText(list []any) (string, bool) {
	items := databaseFormulaFlatItems(list)
	if len(items) == 0 {
		return "", false
	}
	kind := ""
	for _, item := range items {
		reference, ok := item.(databaseFormulaReference)
		if !ok || (kind != "" && reference.Kind != kind) {
			kind = ""
			break
		}
		kind = reference.Kind
	}
	if kind != "" {
		names := make([]string, 0, len(items))
		for _, item := range items {
			if name := item.(databaseFormulaReference).text(); name != "" {
				names = append(names, name)
			}
		}
		return strings.Join(names, " "), true
	}
	parts := make([]string, len(items))
	for index, item := range items {
		switch typed := item.(type) {
		case databaseFormulaReference:
			name := typed.text()
			if typed.Kind == "page" && name == "" {
				name = notionUntitledPageMentionTitle
			}
			parts[index] = "@" + name
		case databaseFormulaRichText:
			parts[index] = typed.shown()
		case databaseFormulaDate:
			parts[index] = "@" + notionFormulaAPIEnvironment.dateText(typed)
		default:
			parts[index] = databaseFormulaText(item)
		}
	}
	return strings.Join(parts, ","), true
}

// notionFormulaAPIEnvironment reads dates as the API's formulas do, in UTC
// (its formulas are evaluated without a viewer's zone).
var notionFormulaAPIEnvironment = &databaseFormulaEnvironment{location: time.UTC}

// databaseFormulaFlatItems is a list's items with lists within it flat and
// empty items (nil) left out.
func databaseFormulaFlatItems(list []any) []any {
	items := make([]any, 0, len(list))
	for _, item := range list {
		switch typed := item.(type) {
		case nil:
		case []any:
			items = append(items, databaseFormulaFlatItems(typed)...)
		default:
			items = append(items, item)
		}
	}
	return items
}
