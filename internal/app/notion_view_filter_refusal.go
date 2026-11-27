package app

import (
	"bytes"
	"encoding/json"
	"sort"
)

// notionViewCondition is a condition Notion's API names for a family of
// property and the kind of value it takes.
type notionViewCondition struct{ name, kind string }

// notionViewConditionLists are the conditions Notion's API names for each
// family of property, in the order its refusal lists them (seen 2026-11-11).
// A view's other conditions, a range of days or a span from today set on the
// screen, it refuses in a request and leaves out of a view it reads (seen
// 2026-11-11, officialViewCondition).
var notionViewConditionLists = map[string][]notionViewCondition{
	"text": {{"equals", "s"}, {"does_not_equal", "s"}, {"contains", "s"}, {"does_not_contain", "s"},
		{"starts_with", "s"}, {"ends_with", "s"}, {"is_empty", "t"}, {"is_not_empty", "t"}},
	"number": {{"equals", "n"}, {"does_not_equal", "n"}, {"greater_than", "n"}, {"less_than", "n"},
		{"greater_than_or_equal_to", "n"}, {"less_than_or_equal_to", "n"}, {"is_empty", "t"}, {"is_not_empty", "t"}},
	"checkbox": {{"equals", "b"}, {"does_not_equal", "b"}},
	"select":   {{"equals", "o"}, {"does_not_equal", "o"}, {"is_empty", "t"}, {"is_not_empty", "t"}},
	"multi":    {{"contains", "o"}, {"does_not_contain", "o"}, {"is_empty", "t"}, {"is_not_empty", "t"}},
	"date": {{"equals", "d"}, {"before", "d"}, {"after", "d"}, {"on_or_before", "d"}, {"on_or_after", "d"},
		{"this_week", "e"}, {"past_week", "e"}, {"past_month", "e"}, {"past_year", "e"},
		{"next_week", "e"}, {"next_month", "e"}, {"next_year", "e"}, {"is_empty", "t"}, {"is_not_empty", "t"}},
	"people":   {{"contains", "u"}, {"does_not_contain", "u"}, {"is_empty", "t"}, {"is_not_empty", "t"}},
	"relation": {{"contains", "p"}, {"does_not_contain", "p"}, {"is_empty", "t"}, {"is_not_empty", "t"}},
	"files":    {{"is_empty", "t"}, {"is_not_empty", "t"}},
}

// notionViewConditionsByType are each family's conditions by name, with the
// kind of value each takes.
var notionViewConditionsByType = func() map[string]map[string]string {
	families := map[string]map[string]string{}
	for family, conditions := range notionViewConditionLists {
		families[family] = map[string]string{}
		for _, condition := range conditions {
			families[family][condition.name] = condition.kind
		}
	}
	return families
}()

// notionViewComputedKindLists are the kinds of value a formula's or a
// rollup's filter may name, in the order Notion's refusal lists them (seen
// 2026-11-11).
var notionViewComputedKindLists = map[string][]string{
	"formula": {"string", "checkbox", "number", "date"},
	"rollup":  {"any", "none", "every", "date", "number"},
}

// notionViewRelativeDays are the days a date condition names from today, as
// a refusal lists them.
const notionViewRelativeDays = "`\"today\"`, `\"tomorrow\"`, `\"yesterday\"`, `\"one_week_ago\"`, " +
	"`\"one_week_from_now\"`, `\"one_month_ago\"`, or `\"one_month_from_now\"`"

// What each kind of condition value is, as a refusal names it (seen
// 2026-11-11); a date's depends on whether a string was given
// (notionViewConditionValueShape).
var notionViewConditionValueShapes = map[string]string{
	"t": "`true`", "s": "a string", "n": "a number", "b": "a boolean",
	"o": "a string or an array", "u": "a string or `\"me\"`", "p": "a string", "e": "an object",
}

func notionViewConditionValueShape(kind string, raw json.RawMessage) string {
	var text string
	given := json.Unmarshal(raw, &text) == nil
	switch {
	case kind == "d" && given:
		return "a valid ISO 8601 date string, " + notionViewRelativeDays
	case kind == "d":
		return "a string, " + notionViewRelativeDays
	// A page's or a person's string that isn't a UUID (seen 2026-11-12).
	case kind == "p" && given:
		return "a valid uuid"
	case kind == "u" && given:
		return "a valid uuid or `\"me\"`"
	}
	return notionViewConditionValueShapes[kind]
}

// notionViewConditionRefusal is Notion's refusal of a condition at path —
// body.filter.date, body.filter.and[0].rich_text, body.quick_filters.Due.date
// — that the family doesn't name, or whose value isn't the kind the
// condition takes: each condition the family names, in Notion's order, on a
// line of its own, the one given saying what its value should be.
func notionViewConditionRefusal(path, family, condition string, raw json.RawMessage) error {
	conditions := notionViewConditionLists[family]
	names := make([]string, len(conditions))
	shape := ""
	for index, item := range conditions {
		names[index] = item.name
		if item.name == condition {
			shape = notionViewConditionValueShape(item.kind, raw)
		}
	}
	// An empty object's condition given a field says that field shouldn't
	// be there, alone.
	if notionViewConditionsByType[family][condition] == "e" {
		fields := map[string]json.RawMessage{}
		if json.Unmarshal(raw, &fields) == nil && len(fields) > 0 {
			keys := make([]string, 0, len(fields))
			for key := range fields {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			return notionViewFilterInvalid("%s", notionFaultSaid("body", notionFaultLine(
				path+"."+condition+"."+keys[0], []string{"not present"}, notionViewRefusedValue(fields[keys[0]]))))
		}
	}
	return notionViewFixOne(path, names, condition, shape, raw)
}

// notionViewFixOne is Notion's refusal of what is at path when it is none of
// the alternatives its API takes there, any one of which fixes it
// (notionFaultChoicesSaid): a line for each alternative, the one given
// saying what it takes, the others that they are missing.
func notionViewFixOne(path string, alternatives []string, given, shape string, raw json.RawMessage) error {
	lines := []string{}
	for _, alternative := range alternatives {
		if alternative == given && shape != "" {
			lines = append(lines, notionFaultLine(path+"."+alternative, []string{shape}, notionViewRefusedValue(raw)))
			continue
		}
		lines = append(lines, notionFaultMissing(path+"."+alternative))
	}
	return notionViewFilterInvalid("%s", notionFaultChoicesSaid("body", lines...))
}

// notionViewRefusedValue is a refused value as JSON written without spaces.
func notionViewRefusedValue(raw json.RawMessage) string {
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		return string(raw)
	}
	return compact.String()
}
