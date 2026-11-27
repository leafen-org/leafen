package app

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type databaseQueryPropertyOption struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

type databaseQueryStatusGroup struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	OptionIDs []string `json:"optionIds"`
}

type databaseQueryPropertyConfig struct {
	Options        []databaseQueryPropertyOption `json:"options"`
	StatusGroups   []databaseQueryStatusGroup    `json:"statusGroups"`
	RollupFunction string                        `json:"rollupFunction"`
	Function       string                        `json:"function"`
	// RelationDatabaseID is the data source a relation's items are rows of.
	RelationDatabaseID string `json:"relationDatabaseId"`
}

var databaseQueryRelativeOperators = map[string]struct{}{
	"this_week": {}, "past_week": {}, "past_month": {}, "past_year": {},
	"next_week": {}, "next_month": {}, "next_year": {}, "relative_to_today": {},
}

func databaseQueryFilterOperatorAllowed(propertyType, operator string) bool {
	allowed := map[string]struct{}{}
	add := func(values ...string) {
		for _, value := range values {
			allowed[value] = struct{}{}
		}
	}
	switch propertyType {
	case graphValueTypeSelect, graphValueTypeMultiSelect, graphValueTypeStatus:
		add("equals", "contains", "does_not_equal", "does_not_contain", "is_empty", "is_not_empty")
	case graphValueTypeCheckbox, graphValueTypeVerification:
		add("equals", "does_not_equal")
	case graphValueTypeRelation, graphValueTypePerson, graphValueTypeCreatedBy, graphValueTypeLastEditedBy:
		add("equals", "contains", "does_not_equal", "does_not_contain", "is_empty", "is_not_empty")
	case graphValueTypeNumber, graphValueTypeUniqueID:
		add("equals", "does_not_equal", "greater_than", "greater_than_or_equal_to", "less_than", "less_than_or_equal_to", "is_empty", "is_not_empty")
	case graphValueTypeDate, graphValueTypeCreatedTime, graphValueTypeLastEditedTime, graphValueTypeLastVisitedTime:
		add("equals", "before", "after", "on_or_after", "on_or_before", "is_between", "is_empty", "is_not_empty")
		for value := range databaseQueryRelativeOperators {
			add(value)
		}
	default:
		add("equals", "does_not_equal", "contains", "does_not_contain", "starts_with", "ends_with", "is_empty", "is_not_empty", "greater_than", "greater_than_or_equal_to", "less_than", "less_than_or_equal_to")
		// A formula's or rollup's date is before, after or on a day.
		for operator := range databaseQueryDayOrderOperators {
			add(operator)
		}
		// A formula's or rollup's date filters as a date does, from today
		// or in a range too.
		if propertyType == graphValueTypeRollup || propertyType == graphValueTypeFormula {
			add("is_between")
			for value := range databaseQueryRelativeOperators {
				add(value)
			}
		}
	}
	_, ok := allowed[operator]
	return ok
}

func databaseQueryFilterMatchesProperty(
	row databaseQueryRow,
	filter databaseQueryFilter,
	property storedDatabaseQueryProperty,
	context databaseQueryContext,
	plan databaseQueryPlan,
	principalID, pageID string,
) bool {
	if property.Type == graphValueTypeRollup {
		if matched, read := databaseQueryRollupLeavesFilter(row, filter, property, context, plan, principalID, pageID); read {
			return matched
		}
	}
	actual := databaseQueryPropertyValue(row, property)
	expected := resolveDatabaseQueryFilterValue(databaseQueryRawValue(filter.Value), principalID, pageID,
		context.relativeSprints(property))
	switch property.Type {
	case graphValueTypeSelect, graphValueTypeMultiSelect, graphValueTypeStatus:
		return databaseQueryOptionFilter(actual, expected, filter.Operator, property)
	case graphValueTypeVerification:
		return databaseQueryVerificationFilter(actual, expected, filter.Operator)
	case graphValueTypeCheckbox:
		checked := databaseQueryBoolean(actual)
		wanted := databaseQueryBoolean(expected)
		if filter.Operator == "equals" {
			return checked == wanted
		}
		return checked != wanted
	case graphValueTypeRelation, graphValueTypePerson, graphValueTypeCreatedBy, graphValueTypeLastEditedBy:
		return databaseQueryIdentityFilter(actual, expected, filter.Operator)
	case graphValueTypeNumber, graphValueTypeUniqueID:
		return databaseQueryNumberFilter(actual, expected, filter.Operator)
	case graphValueTypeDate, graphValueTypeCreatedTime, graphValueTypeLastEditedTime, graphValueTypeLastVisitedTime:
		return databaseQueryDateFilter(actual, expected, filter, plan)
	default:
		// A formula's or rollup's date compares by day, as a date's does, and
		// is on a day by its date too, not by its text, by its start or end,
		// from today or in a range (Notion's, seen 2026-11-11).
		_, ordered := databaseQueryDayOrderOperators[filter.Operator]
		_, relative := databaseQueryRelativeOperators[filter.Operator]
		dated := databaseQueryDateObject(actual)
		if ordered || relative || filter.Operator == "is_between" || dated && filter.DatePart == "end" ||
			filter.Operator == "equals" && dated && databaseQueryDateText(expected) {
			return databaseQueryDateFilter(actual, expected, filter, plan)
		}
		if databaseQueryFormulaNegationWithoutResult(actual, expected, filter.Operator, property) {
			return false
		}
		return databaseQueryTextFilter(actual, expected, filter.Operator, property, context)
	}
}

// databaseQueryFormulaNegationWithoutResult says a formula that gives no
// result meets no "is not" or "does not contain", of a number or a text,
// as Notion's (seen 2026-11-14 through its API: prop("n") does_not_equal 5
// and prop("t") does_not_contain "a" leave out the row with no n or t),
// while a property with no value, a rollup with none and a formula's made
// "" (format(prop("n"))) still meet them. A condition still waiting for
// its value keeps every row. The app's view filter keeps the same rule
// (queryFilterPolicy.ts formulaNegationWithoutResult).
func databaseQueryFormulaNegationWithoutResult(actual, expected any, operator string, property storedDatabaseQueryProperty) bool {
	if property.Type != graphValueTypeFormula || actual != nil {
		return false
	}
	if operator != "does_not_equal" && operator != "does_not_contain" {
		return false
	}
	return strings.TrimSpace(databaseQueryValueText(expected)) != ""
}

// databaseQueryDateObject reports a computed value that is a date: a
// formula's date result, {"start": ...}.
func databaseQueryDateObject(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	_, dated := object["start"].(string)
	return dated
}

// databaseQueryDateText reports a filter value that names a day, with or
// without a time.
func databaseQueryDateText(value any) bool {
	text, _ := value.(string)
	if _, relative := databaseQueryRelativeFilterDays[text]; relative {
		return true
	}
	_, read := databaseQueryMinute(strings.TrimSpace(text), time.UTC)
	return read
}

// databaseQueryDayOrderOperators put a value before, after or on a day.
var databaseQueryDayOrderOperators = map[string]struct{}{
	"before": {}, "after": {}, "on_or_before": {}, "on_or_after": {},
}

// resolveDatabaseQueryFilterValue reads a filter's relative values: the
// person querying, the page it runs on, and a sprint relation's current,
// next or last sprints (sprints, nil where the relation holds no sprints).
func resolveDatabaseQueryFilterValue(value any, principalID, pageID string, sprints func(which string) []string) any {
	if object, ok := value.(map[string]any); ok {
		kind, _ := object["kind"].(string)
		switch kind {
		case "leafen.current_user":
			return principalID
		case "leafen.current_page":
			return pageID
		case databaseQueryRelativeSprintKind:
			which, _ := object["sprint"].(string)
			ids := []any{}
			if sprints != nil {
				for _, id := range sprints(which) {
					ids = append(ids, id)
				}
			}
			return ids
		}
		resolved := make(map[string]any, len(object))
		for key, item := range object {
			resolved[key] = resolveDatabaseQueryFilterValue(item, principalID, pageID, sprints)
		}
		return resolved
	}
	if values, ok := value.([]any); ok {
		resolved := make([]any, 0, len(values))
		for _, item := range values {
			next := resolveDatabaseQueryFilterValue(item, principalID, pageID, sprints)
			// A relative sprint stands for its sprints among the others.
			if object, isObject := item.(map[string]any); isObject && object["kind"] == databaseQueryRelativeSprintKind {
				resolved = append(resolved, next.([]any)...)
				continue
			}
			resolved = append(resolved, next)
		}
		return resolved
	}
	return value
}

// databaseQueryVerificationFilter reads a verification as Notion's filter
// lists it (seen 2026-11-05): verified, expired or empty, none chosen keeping
// every row.
func databaseQueryVerificationFilter(actual, expected any, operator string) bool {
	targets := databaseQueryValueStrings(expected)
	if len(targets) == 0 {
		return true
	}
	state := "empty"
	if value, ok := actual.(map[string]any); ok {
		switch value["state"] {
		case "expired":
			state = "expired"
		case "verified":
			state = "verified"
			if date, ok := notionDateFields(value["date"]); ok {
				if end, ok := date["end"].(string); ok && end != "" {
					if ends, err := time.Parse(time.RFC3339, end); err == nil && ends.Before(time.Now()) {
						state = "expired"
					}
				}
			}
		}
	}
	for _, target := range targets {
		if target == state {
			return operator != "does_not_equal"
		}
	}
	return operator == "does_not_equal"
}

func databaseQueryOptionFilter(actual, expected any, operator string, property storedDatabaseQueryProperty) bool {
	actualIDs := databaseQueryValueStrings(actual)
	targets := databaseQueryOptionTargets(property, expected)
	hasTarget := false
	for _, actualID := range actualIDs {
		if _, ok := targets[actualID]; ok {
			hasTarget = true
			break
		}
	}
	switch operator {
	case "equals", "contains":
		return hasTarget
	case "does_not_equal", "does_not_contain":
		return !hasTarget
	case "is_empty":
		return len(actualIDs) == 0
	case "is_not_empty":
		return len(actualIDs) > 0
	}
	return false
}

func databaseQueryOptionTargets(property storedDatabaseQueryProperty, expected any) map[string]struct{} {
	config := databaseQueryParsedPropertyConfig(property)
	targets := map[string]struct{}{}
	for _, raw := range databaseQueryValueStrings(expected) {
		folded := foldDatabaseNaturalText(raw)
		matched := false
		for _, option := range config.Options {
			if folded == foldDatabaseNaturalText(option.ID) || folded == foldDatabaseNaturalText(option.Name) {
				targets[option.ID] = struct{}{}
				matched = true
				break
			}
		}
		if property.Type == graphValueTypeStatus && !matched {
			for _, group := range config.StatusGroups {
				if folded != foldDatabaseNaturalText(group.ID) && folded != foldDatabaseNaturalText(group.Name) {
					continue
				}
				for _, optionID := range group.OptionIDs {
					targets[optionID] = struct{}{}
				}
				for _, option := range config.Options {
					if option.Group == group.Name {
						targets[option.ID] = struct{}{}
					}
				}
				matched = true
				break
			}
		}
		if !matched {
			targets[raw] = struct{}{}
		}
	}
	return targets
}

func databaseQueryIdentityFilter(actual, expected any, operator string) bool {
	actualIDs := databaseQueryValueStrings(actual)
	targetIDs := databaseQueryValueStrings(expected)
	hasTarget := false
	for _, left := range actualIDs {
		for _, right := range targetIDs {
			if left == right {
				hasTarget = true
			}
		}
	}
	switch operator {
	case "is_empty":
		return len(actualIDs) == 0
	case "is_not_empty":
		return len(actualIDs) > 0
	case "equals", "contains":
		return len(targetIDs) == 0 || hasTarget
	case "does_not_equal", "does_not_contain":
		return len(targetIDs) == 0 || !hasTarget
	}
	return false
}

func databaseQueryNumberFilter(actual, expected any, operator string) bool {
	left, leftOK := databaseQueryStrictNumber(actual)
	if operator == "is_empty" {
		return !leftOK
	}
	if operator == "is_not_empty" {
		return leftOK
	}
	if !leftOK {
		return operator == "does_not_equal"
	}
	right, rightOK := databaseQueryStrictNumber(expected)
	if !rightOK {
		return false
	}
	switch operator {
	case "equals":
		return left == right
	case "does_not_equal":
		return left != right
	case "greater_than":
		return left > right
	case "greater_than_or_equal_to":
		return left >= right
	case "less_than":
		return left < right
	case "less_than_or_equal_to":
		return left <= right
	}
	return false
}

func databaseQueryTextFilter(actual, expected any, operator string, property storedDatabaseQueryProperty, context databaseQueryContext) bool {
	left := foldDatabaseNaturalText(databaseQueryPropertyDisplayText(actual, property, context))
	right := foldDatabaseNaturalText(databaseQueryValueText(expected))
	if right == "" && (operator == "does_not_contain" || operator == "does_not_equal") {
		return true
	}
	switch operator {
	case "equals":
		return left == right
	case "does_not_equal":
		return left != right
	case "contains":
		return strings.Contains(left, right)
	case "does_not_contain":
		return !strings.Contains(left, right)
	case "starts_with":
		return strings.HasPrefix(left, right)
	case "ends_with":
		return strings.HasSuffix(left, right)
	case "is_empty":
		return left == ""
	case "is_not_empty":
		return left != ""
	case "greater_than", "greater_than_or_equal_to", "less_than", "less_than_or_equal_to":
		return databaseQueryNumberFilter(actual, expected, operator)
	}
	return false
}

// databaseQueryUnorderedSortKey is the key of a formula's number that is no
// number, which orders with no other: it comes after every number in either
// direction and before the rows with no value (databaseQuerySortKey). Notion's
// places it as its sort meets it (seen 2026-11-14: -3, NaN, 0 rising, 0, -3,
// NaN falling).
const databaseQueryUnorderedSortKey = "\x00unordered"

// databaseQueryPropertySortKey is a row's key for sorting by a property, and
// whether the row has no value to sort by.
func databaseQueryPropertySortKey(row databaseQueryRow, property storedDatabaseQueryProperty, context databaseQueryContext) (string, bool) {
	value := databaseQueryPropertyValue(row, property)
	switch property.Type {
	case graphValueTypeSelect, graphValueTypeStatus, graphValueTypeMultiSelect:
		ids := databaseQueryValueStrings(value)
		if len(ids) == 0 {
			return "", true
		}
		config := databaseQueryParsedPropertyConfig(property)
		for index, option := range config.Options {
			if option.ID == ids[0] {
				return databaseQueryNumberOrderKey(float64(index)), false
			}
		}
		return "", true
	case graphValueTypeNumber, graphValueTypeUniqueID:
		if number, ok := databaseQueryStrictNumber(value); ok {
			return databaseQueryNumberOrderKey(number), false
		}
		return "", true
	case graphValueTypeCheckbox:
		if databaseQueryBoolean(value) {
			return databaseQueryNumberOrderKey(1), false
		}
		return databaseQueryNumberOrderKey(0), false
	case graphValueTypeDate, graphValueTypeCreatedTime, graphValueTypeLastEditedTime, graphValueTypeLastVisitedTime:
		key := databaseQueryDateSortKey(value)
		return databaseNaturalOrderKey(key), key == ""
	case graphValueTypeVerification:
		key := databaseQueryVerificationSortKey(value)
		return databaseNaturalOrderKey(key), key == ""
	case graphValueTypeFormula, graphValueTypeRollup:
		// A number sorts as one, not as its text, an infinite one too, as
		// Notion's (seen 2026-11-14: -18, -3, 7 and 1.25, 1.3, 1.5; -Infinity,
		// 0, Infinity, then the empty one), as the app's sort (querySortPolicy.ts);
		// one that is no number has no order (databaseQueryUnorderedSortKey).
		if number, ok := value.(float64); ok {
			if math.IsNaN(number) {
				return databaseQueryUnorderedSortKey, false
			}
			return databaseQueryNumberOrderKey(number), false
		}
	}
	text := databaseQueryPropertyDisplayText(value, property, context)
	return databaseNaturalOrderKey(text), strings.TrimSpace(text) == ""
}

func databaseQueryPropertyDisplayText(value any, property storedDatabaseQueryProperty, _ databaseQueryContext) string {
	if property.Type == graphValueTypeSelect || property.Type == graphValueTypeStatus || property.Type == graphValueTypeMultiSelect {
		config := databaseQueryParsedPropertyConfig(property)
		parts := []string{}
		for _, id := range databaseQueryValueStrings(value) {
			label := id
			for _, option := range config.Options {
				if option.ID == id {
					label = option.Name
					break
				}
			}
			parts = append(parts, label)
		}
		return strings.Join(parts, " ")
	}
	return databaseQueryValueText(value)
}

func databaseQueryParsedPropertyConfig(property storedDatabaseQueryProperty) databaseQueryPropertyConfig {
	config := databaseQueryPropertyConfig{}
	_ = json.Unmarshal([]byte(property.Config), &config)
	return config
}

func databaseQueryValueStrings(value any) []string {
	if values, ok := value.([]any); ok {
		result := make([]string, 0, len(values))
		for _, item := range values {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				result = append(result, text)
			}
		}
		return result
	}
	text := strings.TrimSpace(databaseQueryValueText(value))
	if text == "" {
		return nil
	}
	return []string{text}
}

func databaseQueryStrictNumber(value any) (float64, bool) {
	if value == nil || value == "" {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return typed, !math.IsNaN(typed)
	case int:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil && !math.IsNaN(parsed)
	}
	return 0, false
}

func databaseQueryBoolean(value any) bool {
	return value == true || value == "true"
}

func databaseQueryNumberOrderKey(value float64) string {
	bits := math.Float64bits(value)
	if bits&(uint64(1)<<63) != 0 {
		bits = ^bits
	} else {
		bits ^= uint64(1) << 63
	}
	return fmt.Sprintf("%016x", bits)
}

// databaseQueryVerificationSortKey orders verifications by when they run
// out, one verified indefinitely after every dated one; an unverified page
// has no key, so it comes last either way, as Notion's (seen 2026-11-11).
func databaseQueryVerificationSortKey(value any) string {
	object, _ := value.(map[string]any)
	if state, _ := object["state"].(string); state != "verified" && state != "expired" {
		return ""
	}
	date, _ := object["date"].(map[string]any)
	if end := databaseQueryDateSortKey(date["end"]); end != "" {
		return end
	}
	return "9999-12-31T23:59:59.999999999Z"
}

func databaseQueryDateSortKey(value any) string {
	raw := strings.TrimSpace(databaseQueryDateString(value))
	if raw == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC().Format(time.RFC3339Nano)
	}
	return raw
}
