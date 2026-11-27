package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type databaseQueryContext struct {
	Properties       []storedDatabaseQueryProperty
	PropertiesByID   map[string]storedDatabaseQueryProperty
	PropertiesByName map[string]storedDatabaseQueryProperty
	ViewConfig       json.RawMessage
	// NaturalOrder orders the rows when the query has no sorts.
	NaturalOrder databaseNaturalOrder
	// PropertiesByNameBySource names the properties of each data source the
	// query reads, so a formula resolves names within its own source.
	PropertiesByNameBySource map[string]map[string]storedDatabaseQueryProperty
	// ReadablePages are the pages whose rows a rollup may read: the query's
	// page and the related pages the person querying can read and its
	// reading reaches (database_query_related_sources.go).
	ReadablePages []string
	// UnavailableRelations are relations whose target data source is gone or
	// in the trash; rollups over them read no rows.
	UnavailableRelations map[string]bool
	// ClosedRelations are relations whose target data source lies on a page
	// a connection's or an agent's reading doesn't read: their cells read as
	// empty to it, as its row page shows them (rowAsRead).
	ClosedRelations map[string]bool
	// Hidden are the related sources' unlinked items the person querying has
	// no role in: rollups and formulas read past them, as rows they can't
	// open (Notion's, seen 2026-10-14: a sum left out the hidden item's value).
	Hidden map[string]bool
	// DeletedPropertyIDs are the source's properties in Deleted properties;
	// sorts and filters on them wait until they are restored.
	DeletedPropertyIDs map[string]struct{}
	// FormulaTime is the clock and time zone formulas read dates in, and
	// People names the people they read.
	FormulaTime databaseFormulaTime
	People      *databaseFormulaPeople
	// Access is the source's property access rules for the person
	// querying (database_property_access.go); nil where it has none.
	Access       *databasePropertyAccessReader
	DataSourceID string
	// Sprints reads the sprints a filter's "현재", "다음" and "마지막" stand
	// for (database_query_relative_sprints.go); nil reads none.
	Sprints *databaseRelativeSprints
	// Mentions is how a page or database mention in a cell stands to
	// whoever queries, whose conditions, formulas and rollups read one they
	// can't open as "Untitled" (database_query_reader_mentions.go); nil
	// reads every mention as written.
	Mentions *databaseQueryMentionReading
	// TargetValues are the related rows' computed values a request's steps
	// worked out (database_query_target_window.go); nil keeps none.
	TargetValues *databaseQueryTargetValues
}

// propertyNamed resolves a formula's property name within the data source
// the formula belongs to. A name no property has may be a property ID, which
// a rename writes while the property's name is shared.
func (context databaseQueryContext) propertyNamed(dataSourceID, name string) (storedDatabaseQueryProperty, bool) {
	byName, ok := context.PropertiesByNameBySource[dataSourceID]
	if !ok {
		byName = context.PropertiesByName
	}
	if property, found := byName[name]; found {
		return property, true
	}
	property, found := context.PropertiesByID[name]
	return property, found && property.DataSourceID == dataSourceID && property.Name != ""
}

// databaseFormulaPageFields are a page's own created and edited fields,
// which Notion's formulas name as prop("Created Time") and the rest even
// where no property holds them (seen 2026-09-30).
var databaseFormulaPageFields = map[string]string{
	"Created By": graphValueTypeCreatedBy, "Created Time": graphValueTypeCreatedTime,
	"Last Edited By": graphValueTypeLastEditedBy, "Last Edited Time": graphValueTypeLastEditedTime,
}

// formulaPropertyNamed is the property a formula's prop() names: one of the
// data source's, or else one of the page's own fields.
func (context databaseQueryContext) formulaPropertyNamed(dataSourceID, name string) (storedDatabaseQueryProperty, bool) {
	if property, found := context.propertyNamed(dataSourceID, name); found {
		return property, true
	}
	fieldType, field := databaseFormulaPageFields[name]
	return storedDatabaseQueryProperty{Name: name, Type: fieldType, DataSourceID: dataSourceID}, field
}

// hidden is whether a related item is one the person can't open.
func (context databaseQueryContext) hidden(id string) bool {
	return context.Hidden[id]
}

// visibleRowIDs leaves out the related items the person can't open.
func (context databaseQueryContext) visibleRowIDs(ids []string) []string {
	if len(context.Hidden) == 0 {
		return ids
	}
	visible := make([]string, 0, len(ids))
	for _, id := range ids {
		if !context.hidden(id) {
			visible = append(visible, id)
		}
	}
	return visible
}

// readablePages are the pages dependency rows may come from.
func (context databaseQueryContext) readablePages(pageID string) []string {
	if len(context.ReadablePages) > 0 {
		return context.ReadablePages
	}
	return []string{pageID}
}

type databaseQueryEvaluatedRow struct {
	Row     databaseQueryRow
	SortKey string
}

func boundedDatabaseQueryRows(
	rows []storedDatabaseQueryRow,
	context databaseQueryContext,
) ([]storedDatabaseQueryRow, []string, error) {
	relationProperties := map[string]struct{}{}
	formulaProperties := 0
	// Related rows are read only for formulas and rollups to compute from.
	computes := databaseQueryComputes(context)
	for _, property := range context.Properties {
		if property.Type == graphValueTypeRelation && computes {
			relationProperties[property.ID] = struct{}{}
		}
		if property.Type == graphValueTypeFormula {
			formulaProperties++
		}
	}
	rowLimit := databaseQuerySourceRowsPerTurn
	if formulaProperties > 0 {
		rowLimit = max(1, databaseQueryFormulaEvaluationsPerTurn/formulaProperties)
	}
	dependencies := map[string]struct{}{}
	accepted := make([]storedDatabaseQueryRow, 0, len(rows))
	for _, row := range rows {
		if len(accepted) >= rowLimit {
			break
		}
		candidate := make(map[string]struct{}, len(dependencies))
		for id := range dependencies {
			candidate[id] = struct{}{}
		}
		for propertyID := range relationProperties {
			ids := databaseQueryStringSlice(row.Values[propertyID].Value)
			for _, id := range ids {
				candidate[id] = struct{}{}
			}
		}
		if len(candidate) > databaseQueryDependencyRowsPerTurn {
			if len(accepted) == 0 {
				return nil, nil, errors.New(errorCodeDatabaseQueryDependencyLimit)
			}
			break
		}
		dependencies = candidate
		accepted = append(accepted, row)
	}
	ids := make([]string, 0, len(dependencies))
	for id := range dependencies {
		ids = append(ids, id)
	}
	return accepted, ids, nil
}

// databaseQueryRequiresDurableDependencies reports a row whose computed values
// take the durable dependency path: one with more related rows than a turn
// reads, or one related through a relation whose rollup reads a computed
// value on the related rows. Without formulas or rollups nothing reads the
// related rows, so no row takes it, however many it has.
func databaseQueryRequiresDurableDependencies(shape databaseQueryRelationShape, context databaseQueryContext) bool {
	return databaseQueryComputes(context) && (shape.ExceedsBound != 0 || shape.NestedRelation != 0)
}

// databaseQueryComputes reports a data source with formulas or rollups, the
// only values computed from related rows.
func databaseQueryComputes(context databaseQueryContext) bool {
	for _, property := range context.Properties {
		if property.Type == graphValueTypeFormula || property.Type == graphValueTypeRollup {
			return true
		}
	}
	return false
}

// databaseQueryNestedRelationIDs lists the relations whose rollups read a
// computed value on the related rows. A rollup that can't be read could
// follow any relation, so it lists them all.
func databaseQueryNestedRelationIDs(context databaseQueryContext) []string {
	ids := map[string]struct{}{}
	everyRelation := func() {
		for _, property := range context.Properties {
			if property.Type == graphValueTypeRelation {
				ids[property.ID] = struct{}{}
			}
		}
	}
	for _, property := range context.Properties {
		if property.Type != graphValueTypeRollup {
			continue
		}
		config, err := decodeDatabaseComputedConfig(property.Config)
		if err != nil {
			everyRelation()
			continue
		}
		// A rollup not set up follows no relation (rollupSetUp).
		relationID, targetID, _, setUp := context.rollupSetUp(config)
		if !setUp || context.UnavailableRelations[relationID] {
			continue
		}
		target, ok := context.PropertiesByID[targetID]
		if !ok || target.Type == graphValueTypeFormula || target.Type == graphValueTypeRollup ||
			target.Type == graphValueTypeRelation {
			ids[relationID] = struct{}{}
		}
	}
	// A formula naming a relation and a computed value of its pages that
	// reads rows of their own reads past them, as a rollup of it does
	// (prop("Rel").map(current.prop("F")) where F reads a rollup there).
	for _, property := range context.Properties {
		if property.Type != graphValueTypeFormula {
			continue
		}
		config, err := decodeDatabaseComputedConfig(property.Config)
		if err != nil {
			everyRelation()
			continue
		}
		expression := config.Formula
		if expression == "" {
			expression = config.Expression
		}
		names := databaseFormulaPropertyReferences(expression)
		for _, name := range names {
			relation, found := context.propertyNamed(property.DataSourceID, name)
			if !found || relation.Type != graphValueTypeRelation || context.UnavailableRelations[relation.ID] {
				continue
			}
			source := databaseRelationTargetSource(relation)
			for _, other := range names {
				related, found := context.propertyNamed(source, other)
				if found && related.DataSourceID == source && context.computedReadsRows(related) {
					ids[relation.ID] = struct{}{}
				}
			}
		}
	}
	return workspaceExportSortedKeys(ids)
}

// computedReadsRows is whether a computed value reads rows past its own: a
// rollup, or a formula that names one or a relation.
func (context databaseQueryContext) computedReadsRows(property storedDatabaseQueryProperty) bool {
	switch property.Type {
	case graphValueTypeRollup:
		return true
	case graphValueTypeFormula:
		rollups, err := databaseComputedRollupDependencies(property.ID, context)
		if err != nil || len(rollups) > 0 {
			return true
		}
		config, err := decodeDatabaseComputedConfig(property.Config)
		if err != nil {
			return true
		}
		expression := config.Formula
		if expression == "" {
			expression = config.Expression
		}
		for _, name := range databaseFormulaPropertyReferences(expression) {
			if named, found := context.propertyNamed(property.DataSourceID, name); found && named.Type == graphValueTypeRelation {
				return true
			}
		}
	}
	return false
}

// databaseQueryTurnComputedBytes is how much computed text a query's turn
// holds: past it the turn stops after the row it is on and the next goes on
// from there (a formula's value may be 10,000,000 characters).
const databaseQueryTurnComputedBytes = 64 << 20

// evaluateDatabaseQueryRows evaluates the rows in order and says how many it
// did.
func evaluateDatabaseQueryRows(
	rows []storedDatabaseQueryRow,
	dependencyValues map[string]storedDatabaseQueryRow,
	context databaseQueryContext,
	plan databaseQueryPlan,
	principalID, pageID string,
) ([]databaseQueryEvaluatedRow, int, error) {
	result := make([]databaseQueryEvaluatedRow, 0, len(rows))
	held := 0
	for index, stored := range rows {
		if held >= databaseQueryTurnComputedBytes {
			return result, index, nil
		}
		evaluated, accepted, err := evaluateDatabaseQueryRow(
			stored, dependencyValues, nil, context, plan, principalID, pageID,
		)
		if err != nil {
			return nil, 0, err
		}
		if !accepted {
			continue
		}
		held += len(evaluated.SortKey)
		for _, value := range evaluated.Row.Computed {
			held += len(value.Value) + len(value.Text)
		}
		result = append(result, evaluated)
	}
	return result, len(rows), nil
}

func evaluateDatabaseQueryRow(
	stored storedDatabaseQueryRow,
	dependencyValues map[string]storedDatabaseQueryRow,
	seed map[string]databaseComputedValue,
	context databaseQueryContext,
	plan databaseQueryPlan,
	principalID, pageID string,
) (databaseQueryEvaluatedRow, bool, error) {
	context.FormulaTime = plan.formulaTime(context.FormulaTime.Language)
	// The conditions and the computed values read the row as whoever
	// queries reads it; the row answered keeps its cells as stored
	// (database_query_reader_mentions.go).
	read := context.rowAsRead(stored)
	computed, err := computeDatabaseRowValuesWithSeed(read, dependencyValues, context, seed)
	if err != nil {
		return databaseQueryEvaluatedRow{}, false, err
	}
	row := databaseQueryRow{
		ID: stored.ID, Position: stored.Position,
		SubitemParentID:   databaseQueryOptionalID(stored.SubitemParentID),
		SubitemChildCount: stored.SubitemChildCount,
		Values:            read.Values, Computed: computed,
		CreatedAt: databaseQueryRowTime(stored.Created), CreatedBy: stored.CreatedBy,
		UpdatedAt: databaseQueryRowTime(stored.Updated), LastEditedBy: stored.LastEditedBy,
	}
	visible := databaseQueryRowWithTimestamps(row, stored, context)
	if !matchesDatabaseQueryFilterGroup(visible, plan.FilterGroup, context, plan, principalID, pageID) ||
		!matchesDatabaseQuerySearch(row, plan.Search, context, principalID) {
		return databaseQueryEvaluatedRow{}, false, nil
	}
	row.Values = stored.Values
	return databaseQueryEvaluatedRow{
		Row: row, SortKey: databaseQuerySortKey(visible, plan.Sorts, context),
	}, true, nil
}

// databaseQueryRowTime writes a row page's time as the content graph does.
func databaseQueryRowTime(milliseconds int64) string {
	if milliseconds <= 0 {
		return ""
	}
	return time.UnixMilli(milliseconds).UTC().Format("2006-01-02T15:04:05.000Z")
}

func databaseQueryOptionalID(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func matchesDatabaseQueryFilterGroup(
	row databaseQueryRow,
	group *databaseQueryFilterGroup,
	context databaseQueryContext,
	plan databaseQueryPlan,
	principalID, pageID string,
) bool {
	if group == nil {
		return true
	}
	values := make([]bool, 0, len(group.Filters)+len(group.Groups))
	for _, filter := range group.Filters {
		property, ok := context.PropertiesByID[filter.PropertyID]
		values = append(values, ok && databaseQueryFilterMatchesProperty(
			row, filter, property, context, plan, principalID, pageID,
		))
	}
	for index := range group.Groups {
		values = append(values, matchesDatabaseQueryFilterGroup(
			row, &group.Groups[index], context, plan, principalID, pageID,
		))
	}
	if group.Conjunction == databaseQueryConjunctionOr {
		for _, value := range values {
			if value {
				return true
			}
		}
		return len(values) == 0
	}
	for _, value := range values {
		if !value {
			return false
		}
	}
	return true
}

func databaseQuerySortKey(
	row databaseQueryRow,
	sorts []databaseQuerySort,
	context databaseQueryContext,
) string {
	parts := make([]string, 0, len(sorts)+2)
	for _, sort := range sorts {
		property, ok := context.PropertiesByID[sort.PropertyID]
		if !ok {
			continue
		}
		// A row with no value comes last in either direction, as Notion's (seen 2026-11-04).
		key, empty := databaseQueryPropertySortKey(row, property, context)
		switch {
		case empty:
			key = "1"
		case key == databaseQueryUnorderedSortKey:
			key = "0~"
		case sort.Direction == databaseQuerySortDescending:
			key = "0" + reverseDatabaseSortKey(key)
		default:
			key = "0" + key
		}
		parts = append(parts, key)
	}
	parts = append(parts, context.NaturalOrder.key(row))
	return strings.Join(parts, "|")
}

func databaseQueryRowValue(row databaseQueryRow, propertyID string) any {
	if computed, ok := row.Computed[propertyID]; ok {
		return databaseQueryRawValue(computed.Value)
	}
	return databaseQueryRawValue(row.Values[propertyID].Value)
}

// databaseQueryPropertyValue is a row's value for one of its source's
// properties as it reads, a status's default and a person property's
// default creator included (databaseValueAsRead), and a formula's result
// that is not finite as the number it is (databaseComputedNonFinite).
func databaseQueryPropertyValue(row databaseQueryRow, property storedDatabaseQueryProperty) any {
	if computed, ok := row.Computed[property.ID]; ok {
		if number, ok := databaseComputedNonFinite(computed); ok {
			return number
		}
		return databaseQueryRawValue(computed.Value)
	}
	return databaseQueryRawValue(databaseValueAsRead(property.Type, property.Config, row.Values[property.ID].Value, row.CreatedBy))
}

func databaseQueryRawValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return value
}

func databaseQueryStringSlice(raw json.RawMessage) []string {
	value := databaseQueryRawValue(raw)
	array, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(array))
	for _, item := range array {
		if text, ok := item.(string); ok && text != "" {
			result = append(result, text)
		}
	}
	return result
}

func databaseQueryEqual(left, right any) bool {
	if databaseQueryContains(left, right) && isDatabaseQueryList(left) {
		return true
	}
	return strings.EqualFold(databaseQueryValueText(left), databaseQueryValueText(right))
}

func databaseQueryContains(container, target any) bool {
	if values, ok := container.([]any); ok {
		for _, value := range values {
			if databaseQueryEqual(value, target) {
				return true
			}
		}
		return false
	}
	return strings.Contains(strings.ToLower(databaseQueryValueText(container)), strings.ToLower(databaseQueryValueText(target)))
}

func isDatabaseQueryList(value any) bool {
	_, ok := value.([]any)
	return ok
}

func databaseQueryEmpty(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	}
	return false
}

func compareNaturalDatabaseValues(left, right any) int {
	leftNumber, leftOK := databaseQueryNumber(left)
	rightNumber, rightOK := databaseQueryNumber(right)
	if leftOK && rightOK {
		if math.Abs(leftNumber-rightNumber) < 1e-12 {
			return 0
		}
		if leftNumber < rightNumber {
			return -1
		}
		return 1
	}
	return strings.Compare(databaseNaturalOrderKey(left), databaseNaturalOrderKey(right))
}

func databaseQueryNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, true
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func databaseQueryDateString(value any) string {
	if object, ok := value.(map[string]any); ok {
		value = object["start"]
	}
	return databaseQueryValueText(value)
}

func databaseQueryValueText(value any) string {
	if value == nil {
		return ""
	}
	if object, ok := value.(map[string]any); ok {
		if start, exists := object["start"]; exists {
			return fmt.Sprint(start)
		}
		// A formula's rich text reads as the text it shows.
		if text, rich := databaseFormulaStoredRichText(object); rich {
			return text.shown()
		}
		// A page or person a formula returns reads as its name.
		if kind, _ := object["kind"].(string); kind == "page" || kind == "person" {
			if name, _ := object["name"].(string); name != "" {
				return name
			}
			return fmt.Sprint(object["id"])
		}
		// A place reads as its name and address, as Notion filters and
		// sorts places by that text.
		if _, latitude := object["lat"]; latitude {
			parts := []string{}
			for _, key := range []string{"name", "address"} {
				if text, _ := object[key].(string); strings.TrimSpace(text) != "" {
					parts = append(parts, strings.TrimSpace(text))
				}
			}
			return strings.Join(parts, " ")
		}
	}
	if array, ok := value.([]any); ok {
		parts := make([]string, len(array))
		for index := range array {
			parts[index] = databaseQueryValueText(array[index])
		}
		return strings.Join(parts, ", ")
	}
	// A number that is not finite reads as format() writes it, "Infinity" or "NaN".
	if number, ok := value.(float64); ok && (math.IsInf(number, 0) || math.IsNaN(number)) {
		return databaseFormulaNumberText(number)
	}
	return fmt.Sprint(value)
}
