package app

import (
	"bytes"
	"encoding/json"
)

// A rollup filter names what Notion's API reads the rollup as: its related
// values (any, every or none, then the kind of value), a number or a date.

// notionViewRollupMatches are a rollup filter's words for which of its
// related values it asks about, and the native match each stands for.
var notionViewRollupMatches = map[string]string{"any": "", "every": databaseRollupMatchEvery, "none": databaseRollupMatchNone}

// notionViewRollupValueKinds are the kinds of value Notion's API takes under
// any, every or none, in the order its refusal lists them (seen 2026-11-11).
var notionViewRollupValueKinds = []string{
	"rich_text", "number", "checkbox", "select", "multi_select", "relation", "date", "people", "files", "status",
}

// notionViewRollupEach is the kind a filter over each related value names, as
// Notion's API reads a rollup showing them: an array.
const notionViewRollupEach = "array"

// notionViewRollupResult is what Notion's API reads a rollup as: its related
// values, shown as they are or counted per group (an array of the values,
// "show_original", seen 2026-11-11), a date for its earliest, latest or
// range of dates, else a number. One not set up yet shows its related
// values, as a new one does.
func notionViewRollupResult(nativeType, rawConfig string) string {
	if nativeType != graphValueTypeRollup {
		return ""
	}
	config, err := decodeDatabaseComputedConfig(rawConfig)
	if err != nil {
		return ""
	}
	_, _, function := config.rollupFields()
	switch function {
	case "", "show_original", "show_unique", "count_per_group", "percent_per_group":
		return notionViewRollupEach
	case "earliest_date", "latest_date", "date_range":
		return "date"
	}
	return "number"
}

// notionViewRollupLeafKind is the kind of value Notion's API names a rollup's
// related values by under any, every or none, from the type of the property
// rolled up (seen 2026-11-11): a title's, URL's, email's or phone's as
// rich_text, an ID's as a number, a page timestamp's as a date, its author's
// as people; "" for a formula's, whose kind is each value's own
// (databaseRollupLeafKind).
func notionViewRollupLeafKind(rolledType string) string {
	switch rolledType {
	case "title", "rich_text", "url", "email", "phone_number":
		return "rich_text"
	case "number", "unique_id":
		return "number"
	case "date", "created_time", "last_edited_time":
		return "date"
	case "people", "created_by", "last_edited_by":
		return "people"
	case "formula":
		return ""
	}
	return rolledType
}

// notionViewRollupKind is the kind a filter over each related value keeps
// (databaseQueryFilter.RollupKind): the kind it names where that isn't the
// values' own, or the values' own are a formula's. Empty or not reads back
// as rich_text whatever it named, as Notion's (seen 2026-11-11), so it keeps
// none.
func notionViewRollupKind(property notionViewProperty, kind, condition string) string {
	leaf := notionViewRollupLeafKind(property.RolledType)
	if condition == "is_empty" || condition == "is_not_empty" || kind == leaf {
		return ""
	}
	return kind
}

// notionViewRollupMatchCondition reads a filter over each of a rollup's
// related values at path (body.filter.rollup), {"any": {"select": {"equals":
// "Done"}}}, as Notion's API takes it: the kind it names, its condition,
// which of the values it asks about and where in the filter its condition
// is: "any.select". Its form is checked here, as Notion's API checks it
// first (seen 2026-11-11); whether it fits the rollup, after its condition's
// (rollupFits). Of several words or kinds the first in Notion's order whose
// form is right is taken, as Notion's (seen 2026-11-11).
func (scope notionViewFilterScope) notionViewRollupMatchCondition(raw json.RawMessage, path string) (string, json.RawMessage, string, string, bool, error) {
	outer := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &outer) != nil {
		return "", nil, "", "", false, nil
	}
	word := notionViewFirstFitting(notionViewComputedKindLists["rollup"], outer, func(word string) bool {
		_, _, _, _, isMatch, err := scope.notionViewRollupMatchWord(word, outer[word], path)
		return isMatch && err == nil || !isMatch && notionViewComputedFamilies["rollup"][word] != "" &&
			scope.computedFormFits("rollup", word, outer[word], path)
	})
	if _, isMatch := notionViewRollupMatches[word]; !isMatch {
		return "", nil, "", "", false, nil
	}
	return scope.notionViewRollupMatchWord(word, outer[word], path)
}

// notionViewRollupMatchWord reads the kinds under one of any, every or none.
func (scope notionViewFilterScope) notionViewRollupMatchWord(word string, inner json.RawMessage, path string) (string, json.RawMessage, string, string, bool, error) {
	match, isMatch := notionViewRollupMatches[word]
	if !isMatch {
		return "", nil, "", "", false, nil
	}
	if !notionViewJSONObject(inner) {
		return "", nil, "", "", true, notionViewFixOne(path, notionViewComputedKindLists["rollup"], word, "an object", inner)
	}
	wordPath := path + "." + word
	typed := map[string]json.RawMessage{}
	_ = json.Unmarshal(inner, &typed)
	kind := notionViewFirstFitting(notionViewRollupValueKinds, typed, func(kind string) bool {
		return notionViewJSONObject(typed[kind]) && scope.conditionFormFits(notionViewConditionFamily(kind), typed[kind], wordPath+"."+kind)
	})
	if kind == "" {
		return "", nil, "", "", true, notionViewFixOne(wordPath, notionViewRollupValueKinds, "", "", nil)
	}
	if !notionViewJSONObject(typed[kind]) {
		return "", nil, "", "", true, notionViewFixOne(wordPath, notionViewRollupValueKinds, kind, "an object", typed[kind])
	}
	return kind, typed[kind], match, word + "." + kind, true, nil
}

// rollupFits refuses a rollup filter that names something other than what
// its rollup reads as (kind: notionViewRollupEach for any, every or none,
// else the kind it names), as Notion's API refuses it (seen 2026-11-11), in
// Leafen's words, and one over
// each related value that Leafen can't compare as them (a rollup's). Any
// kind of value may be named for the related values: one of another kind
// than theirs matches no condition but empty or not (seen 2026-11-11).
// Either is refused only once the condition's form is right, as Notion's API
// checks it first.
func (scope notionViewFilterScope) rollupFits(
	property notionViewProperty, kind, family, condition string, value json.RawMessage, path string,
) error {
	if property.OfficialType != "rollup" || property.RollupResult == "" || kind == "" || kind == "string" {
		return nil
	}
	var refusal error
	switch {
	case kind == notionViewRollupEach && property.RollupResult == kind && property.RolledType == "":
		refusal = notionViewFilterInvalid("the rollup filter for property %q can't compare each value it rolls up", property.Name)
	case kind == property.RollupResult:
	default:
		refusal = notionViewFilterInvalid("The filter's value doesn't fit the rollup filter")
	}
	if refusal == nil {
		return nil
	}
	if err := scope.formOnly().conditionForm(property, family, condition, value, path); err != nil {
		return err
	}
	return refusal
}

// notionViewJSONObject says whether raw is a JSON object.
func notionViewJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// notionViewRollupReadKind is the kind a stored filter over each of a
// rollup's related values reads back under: rich_text for empty or not, as
// Notion's (seen 2026-11-11), else the kind it keeps or its values' own, a
// formula's by its condition.
func notionViewRollupReadKind(property notionViewProperty, filter databaseQueryFilter) string {
	switch {
	case filter.Operator == "is_empty" || filter.Operator == "is_not_empty":
		return "rich_text"
	case filter.RollupKind != "":
		return filter.RollupKind
	}
	if leaf := notionViewRollupLeafKind(property.RolledType); leaf != "" {
		return leaf
	}
	if kind, family := notionViewComputedKind("formula", filter); family != "" && kind != "string" {
		return kind
	}
	return "rich_text"
}
