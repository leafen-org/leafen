package app

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

type databaseComputedConfig struct {
	Expression               string `json:"expression"`
	Formula                  string `json:"formula"`
	RelationPropertyID       string `json:"relationPropertyId"`
	TargetPropertyID         string `json:"targetPropertyId"`
	Function                 string `json:"function"`
	RollupRelationPropertyID string `json:"rollupRelationPropertyId"`
	RollupTargetPropertyID   string `json:"rollupTargetPropertyId"`
	RollupFunction           string `json:"rollupFunction"`
	RollupStatusGroup        string `json:"rollupStatusGroup"`
}

func decodeDatabaseComputedConfig(raw string) (databaseComputedConfig, error) {
	config := databaseComputedConfig{}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return config, errors.New(errorCodeDatabaseQueryInvalid)
	}
	return config, nil
}

func (config databaseComputedConfig) rollupFields() (string, string, string) {
	relationID := config.RollupRelationPropertyID
	if relationID == "" {
		relationID = config.RelationPropertyID
	}
	targetID := config.RollupTargetPropertyID
	if targetID == "" {
		targetID = config.TargetPropertyID
	}
	function := config.RollupFunction
	if function == "" {
		function = config.Function
	}
	// A calculation's older name is its documented one (database_property_config_keys.go).
	if documented := databaseRollupFunctionAliases[function]; documented != "" {
		function = documented
	}
	return relationID, targetID, function
}

// rollupCalculation is what a rollup works out from its related rows: as
// rollupFields names it, but a percent per group naming a status group is
// that group's share, and one naming none, as Notion's API brings them, shows
// the values.
func (config databaseComputedConfig) rollupCalculation() string {
	_, _, function := config.rollupFields()
	if function == "percent_per_group" && config.RollupStatusGroup != "" {
		return databaseRollupPercentInGroup
	}
	if function == "count_per_group" && config.RollupStatusGroup != "" {
		return databaseRollupCountInGroup
	}
	return function
}

// rollupSetUp reads a rollup's relation, rolled-up property and calculation,
// and whether it can be computed. A rollup not set up yet, or whose relation
// or rolled-up property is gone, is empty on every row, as Notion's is (seen
// 2026-10-04); a count needs no property.
func (context databaseQueryContext) rollupSetUp(config databaseComputedConfig) (string, string, string, bool) {
	relationID, targetID, _ := config.rollupFields()
	function := config.rollupCalculation()
	if relationID == "" || targetID == "" || function == "" {
		return relationID, targetID, function, false
	}
	if relation, ok := context.PropertiesByID[relationID]; !ok || relation.Type != graphValueTypeRelation {
		return relationID, targetID, function, false
	}
	_, targetExists := context.PropertiesByID[targetID]
	if function != "count" && !targetExists && !context.UnavailableRelations[relationID] {
		return relationID, targetID, function, false
	}
	return relationID, targetID, function, true
}

type databaseComputedResolver struct {
	context databaseQueryContext
	// rows are the rows this evaluation has read; base the caller's, read
	// from where rows has none and left as they are.
	rows     map[string]storedDatabaseQueryRow
	base     map[string]storedDatabaseQueryRow
	computed map[string]map[string]databaseComputedValue
	visiting map[string]bool
	cyclic   map[string]bool
	// read are the rows already read as whoever computes reads them.
	read map[string]bool
	// now is read once, so every formula of an evaluation reads one time.
	now time.Time
	// leaves are the related values, one by one, of the rollup just evaluated.
	leaves []any
	// unstored are the formula results that hold a number that is not finite,
	// which their stored values can't (databaseFormulaOutput), by row and
	// property: another formula reads them as they are (prop() of
	// 1e308 * 10 is Infinity, as Notion's).
	unstored map[string]any
	// failed are the formulas that failed, by row and property, with what
	// failed them: another formula reading one fails too, as Notion's (seen
	// 2026-11-14: empty(prop("round(2, 0.5)")) is no value).
	failed map[string]error
}

func (resolver *databaseComputedResolver) formulaEnvironment(row storedDatabaseQueryRow, property storedDatabaseQueryProperty) *databaseFormulaEnvironment {
	if resolver.now.IsZero() {
		resolver.now = resolver.context.FormulaTime.Now
		if resolver.now.IsZero() {
			resolver.now = time.Now().UTC()
		}
	}
	return &databaseFormulaEnvironment{
		location: resolver.context.FormulaTime.Location, now: resolver.now, language: resolver.context.FormulaTime.Language, pageID: row.ID,
		property: func(name string) (any, error) {
			reference, ok := resolver.context.formulaPropertyNamed(property.DataSourceID, name)
			if !ok {
				return "", nil
			}
			return resolver.formulaProperty(row, reference)
		},
		related: func(page databaseFormulaReference, name string) (any, error) {
			if page.DataSourceID == "" {
				return "", nil
			}
			related, loaded := resolver.row(page.ID)
			property, found := resolver.context.formulaPropertyNamed(page.DataSourceID, name)
			if !loaded || !found {
				return "", nil
			}
			return resolver.formulaProperty(related, property)
		},
	}
}

// row is a related row the resolver reads, read as whoever computes reads
// it (database_query_reader_mentions.go), once.
func (resolver *databaseComputedResolver) row(id string) (storedDatabaseQueryRow, bool) {
	row, ok := resolver.rows[id]
	if !ok {
		row, ok = resolver.base[id]
	}
	if ok && !resolver.read[id] {
		if resolver.read == nil {
			resolver.read = map[string]bool{}
		}
		row = resolver.context.rowAsRead(row)
		resolver.rows[id], resolver.read[id] = row, true
	}
	return row, ok
}

func computeDatabaseRowValues(
	row storedDatabaseQueryRow,
	dependencyValues map[string]storedDatabaseQueryRow,
	context databaseQueryContext,
) (map[string]databaseComputedValue, error) {
	return computeDatabaseRowValuesWithSeed(row, dependencyValues, context, nil)
}

func computeDatabaseRowValuesWithSeed(
	row storedDatabaseQueryRow,
	dependencyValues map[string]storedDatabaseQueryRow,
	context databaseQueryContext,
	seed map[string]databaseComputedValue,
) (map[string]databaseComputedValue, error) {
	// The row and the related rows its values reach are read as whoever
	// computes them reads them (database_query_reader_mentions.go). The
	// caller's rows stay as they are, read through rather than copied: a copy
	// of them for each row had an export of a table of 100,000 rows still
	// writing nothing of its files after 8 minutes, some 40 seconds read
	// through (2026-11-11).
	row = context.rowAsRead(row)
	rows := map[string]storedDatabaseQueryRow{row.ID: row}
	computed := map[string]map[string]databaseComputedValue{}
	if len(seed) > 0 {
		computed[row.ID] = make(map[string]databaseComputedValue, len(seed))
		for id, value := range seed {
			computed[row.ID][id] = value
		}
	}
	resolver := &databaseComputedResolver{
		context: context, rows: rows, base: dependencyValues, read: map[string]bool{row.ID: true},
		computed: computed,
		visiting: map[string]bool{}, cyclic: map[string]bool{},
	}
	if err := resolver.compute(row); err != nil {
		return nil, err
	}
	return resolver.computed[row.ID], nil
}

func (resolver *databaseComputedResolver) compute(row storedDatabaseQueryRow) error {
	if resolver.computed[row.ID] == nil {
		resolver.computed[row.ID] = map[string]databaseComputedValue{}
	}
	for _, property := range resolver.context.Properties {
		if property.Type != graphValueTypeFormula && property.Type != graphValueTypeRollup {
			continue
		}
		if _, err := resolver.evaluate(row, property); err != nil {
			return err
		}
	}
	return nil
}

func (resolver *databaseComputedResolver) evaluate(
	row storedDatabaseQueryRow,
	property storedDatabaseQueryProperty,
) (any, error) {
	if existing, ok := resolver.computed[row.ID][property.ID]; ok {
		return databaseQueryRawValue(existing.Value), nil
	}
	key := row.ID + "\x00" + property.ID
	if resolver.visiting[key] {
		for active := range resolver.visiting {
			resolver.cyclic[active] = true
		}
		return "", nil
	}
	resolver.visiting[key] = true
	defer delete(resolver.visiting, key)
	config, err := decodeDatabaseComputedConfig(property.Config)
	if err != nil {
		return nil, err
	}
	var value any
	var leaves json.RawMessage
	text := ""
	switch property.Type {
	case graphValueTypeFormula:
		expression := config.Formula
		if expression == "" {
			expression = config.Expression
		}
		// A formula that cannot be evaluated has no value for its row only,
		// kept as none, not as empty text, so its API value is its type with
		// none, as Notion's (seen 2026-11-14: format(round(3.14, 13)) is
		// {"type":"string","string":null}), where text it makes empty is "".
		result, formulaErr := evaluateDatabaseFormulaIn(expression, resolver.formulaEnvironment(row, property))
		switch {
		case resolver.cyclic[key]:
			result = ""
		case formulaErr != nil:
			result = nil
			if resolver.failed == nil {
				resolver.failed = map[string]error{}
			}
			resolver.failed[key] = formulaErr
		}
		result = databaseFormulaKept(result)
		value, text = databaseFormulaOutput(result, true), databaseFormulaFormat(result)
		// A number that is no number keeps "NaN" as its text, which its cell
		// leaves out (formula.ts) but its export writes and a query reads
		// (databaseComputedNonFinite), as Notion's.
		if number, ok := result.(float64); ok && math.IsNaN(number) {
			text = "NaN"
		}
		if databaseFormulaHoldsNonFinite(result) {
			if resolver.unstored == nil {
				resolver.unstored = map[string]any{}
			}
			resolver.unstored[key] = result
		}
	case graphValueTypeRollup:
		value, err = resolver.evaluateDatabaseRollup(row, config)
		if resolver.cyclic[key] {
			value = ""
		}
		text = databaseRollupText(config.rollupCalculation(), value)
		leaves = databaseRollupEncodedLeaves(config.rollupCalculation(), resolver.leaves)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if resolver.computed[row.ID] == nil {
		resolver.computed[row.ID] = map[string]databaseComputedValue{}
	}
	resolver.computed[row.ID][property.ID] = databaseComputedValue{
		Type: property.Type, Value: encoded, Text: text, Leaves: leaves,
	}
	return value, nil
}

// databaseComputedNonFinite is the number that is not finite a formula's
// computed value holds, which its stored value can't (databaseFormulaOutput
// keeps it empty and its text "Infinity", "-Infinity" or "NaN"): an export
// writes it as that text, and a query's conditions and sorts read it as the
// number it is, as Notion's (seen 2026-11-14: a sort puts -Infinity before 0
// and Infinity after every other number; greater_than 1 finds Infinity; none
// of them is empty; one that is no number matches no comparison but
// does_not_equal).
func databaseComputedNonFinite(computed databaseComputedValue) (float64, bool) {
	if computed.Type != graphValueTypeFormula || databaseQueryRawValue(computed.Value) != "" {
		return 0, false
	}
	switch computed.Text {
	case "Infinity":
		return math.Inf(1), true
	case "-Infinity":
		return math.Inf(-1), true
	case "NaN":
		return math.NaN(), true
	}
	return 0, false
}

func (resolver *databaseComputedResolver) evaluateDatabaseRollup(
	row storedDatabaseQueryRow,
	config databaseComputedConfig,
) (any, error) {
	relationID, targetID, function, setUp := resolver.context.rollupSetUp(config)
	if !setUp {
		return nil, nil
	}
	unavailable := resolver.context.UnavailableRelations[relationID]
	targetProperty := resolver.context.PropertiesByID[targetID]
	ids := resolver.context.visibleRowIDs(databaseQueryStringSlice(row.Values[relationID].Value))
	if unavailable {
		ids = nil
	}
	values := make([]any, 0, len(ids))
	leaves := make([]any, 0, len(ids))
	// The related rows read; the shares are of them.
	found := 0
	for _, id := range ids {
		target, ok := resolver.row(id)
		if !ok {
			continue
		}
		found++
		if function == "count" {
			values = append(values, true)
			continue
		}
		value := databaseRollupTargetValue(targetProperty, target)
		if targetProperty.Type == graphValueTypeFormula || targetProperty.Type == graphValueTypeRollup {
			var err error
			value, err = resolver.evaluate(target, targetProperty)
			if err != nil {
				return nil, err
			}
		}
		leaves = append(leaves, value)
		if value != nil && value != "" {
			values = append(values, value)
		}
	}
	resolver.leaves = leaves
	// The counts of values leave checkboxes out (databaseRollupCountsValue),
	// and count a list's items each (databaseRollupValueCount).
	counted, items := 0, 0
	for _, value := range values {
		if databaseRollupCountsValue(value) {
			counted++
		}
		items += databaseRollupValueCount(value)
	}
	booleans := len(values) - counted
	switch function {
	case "count":
		return len(values), nil
	case "count_values":
		return items, nil
	case "not_empty":
		return counted, nil
	case "empty":
		return len(ids) - len(values), nil
	case "unique":
		seen := map[string]struct{}{}
		for _, value := range values {
			if !databaseRollupCountsValue(value) {
				continue
			}
			for _, piece := range databaseRollupPieces(value) {
				seen[piece] = struct{}{}
			}
		}
		return len(seen), nil
	case "percent_empty":
		return databaseRollupPercent(found-len(values), found-booleans), nil
	case "percent_not_empty":
		return databaseRollupPercent(counted, found-booleans), nil
	case databaseRollupPercentInGroup, databaseRollupCountInGroup:
		options := databaseRollupStatusGroupOptions(targetProperty, config.RollupStatusGroup)
		inGroup := 0
		for _, value := range values {
			if databaseRollupInGroup(value, options) {
				inGroup++
			}
		}
		if function == databaseRollupCountInGroup {
			return inGroup, nil
		}
		return databaseRollupPercent(inGroup, found), nil
	case "checked", "unchecked", "percent_checked", "percent_unchecked":
		checked := 0
		for _, value := range values {
			if databaseRollupChecked(value) {
				checked++
			}
		}
		switch function {
		case "checked":
			return checked, nil
		case "unchecked":
			return found - checked, nil
		case "percent_checked":
			return databaseRollupPercent(checked, found), nil
		default:
			return databaseRollupPercent(found-checked, found), nil
		}
	case "earliest_date", "latest_date", "date_range":
		earliest, latest := "", ""
		for _, value := range values {
			for _, date := range databaseRollupDates(value) {
				earliest, latest = databaseRollupDateSpan(earliest, latest, date)
			}
		}
		return databaseRollupDateValue(function, earliest, latest), nil
	case "show_original", "show_unique", "count_per_group", "percent_per_group":
		for index, value := range values {
			if targetProperty.Type == graphValueTypeRelation {
				values[index] = resolver.relationTitles(value)
				continue
			}
			values[index] = databaseRollupShownValue(targetProperty, value, resolver.context.People)
		}
		if function == "show_unique" {
			seen := map[string]struct{}{}
			unique := make([]string, 0, len(values))
			for _, value := range values {
				key := databaseQueryValueText(value)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				unique = append(unique, key)
			}
			return strings.Join(unique, ", "), nil
		}
		parts := make([]string, 0, len(values))
		for _, value := range values {
			parts = append(parts, databaseQueryValueText(value))
		}
		return strings.Join(parts, ", "), nil
	case "sum", "average", "median", "min", "max", "range":
		numbers := make([]float64, 0, len(values))
		for _, value := range values {
			if number, ok := databaseQueryNumber(value); ok {
				numbers = append(numbers, number)
			}
		}
		if len(numbers) == 0 {
			// Nothing to add up is 0 for a sum and a median and empty for
			// the others, as Notion shows and its API answers (seen
			// 2026-10-03 over a relation to nothing).
			if function == "sum" || function == "median" {
				return 0.0, nil
			}
			return nil, nil
		}
		result := numbers[0]
		switch function {
		case "sum", "average":
			result = 0
			for _, number := range numbers {
				result += number
			}
			if function == "average" {
				result /= float64(len(numbers))
			}
		case "median":
			sort.Float64s(numbers)
			middle := len(numbers) / 2
			result = numbers[middle]
			if len(numbers)%2 == 0 {
				result = (numbers[middle-1] + numbers[middle]) / 2
			}
		case "min":
			for _, number := range numbers[1:] {
				result = math.Min(result, number)
			}
		case "max":
			for _, number := range numbers[1:] {
				result = math.Max(result, number)
			}
		case "range":
			minimum, maximum := numbers[0], numbers[0]
			for _, number := range numbers[1:] {
				minimum, maximum = math.Min(minimum, number), math.Max(maximum, number)
			}
			result = maximum - minimum
		}
		return result, nil
	default:
		return nil, errors.New(errorCodeDatabaseQueryInvalid)
	}
}
