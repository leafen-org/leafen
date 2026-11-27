package app

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
)

// What a view's group, or a board's sub-group, says beyond its
// property and sort, as Notion's views API takes it and reads it back (seen
// 2026-11-12 on boards and tables made with and without each):
//   - hide_empty_groups: a group made through the API shows its empty
//     groups, the one for no value too, unless told to hide them or given as
//     another type than its property's, where one picked on the screen hides
//     them; read back true while they hide, unless given as another type.
//   - type: any of its types, whatever type its property is — a status
//     grouped as a select, a text as a number — read back as given; the
//     groups are its property's own (a status's options for any type but
//     status). Each type needs what it says: a status's, a text's and a
//     date's group_by among the values that type takes, a formula's its own.
//   - group_by of a text: its exact values or their first letters.
//   - range_start, range_end and range_size of a number: its values in
//     ranges from start to end, size wide, those outside in one group after
//     them; 0, 0 and 1 until given, so that a number grouped through the API
//     puts its values out of range (seen 2026-11-12: "범위 벗어남").

// notionViewNumberRangeFields are a number group's range fields, in the
// order Notion's refusal reads them.
var notionViewNumberRangeFields = []string{"range_start", "range_end", "range_size"}

// notionViewGroupUnits are the group_by a group of each kind takes.
var notionViewGroupUnits = map[string][]string{
	"status": {"group", "option"},
	"text":   {"exact", "alphabet_prefix"}, "title": {"exact", "alphabet_prefix"}, "url": {"exact", "alphabet_prefix"},
	"email": {"exact", "alphabet_prefix"}, "phone_number": {"exact", "alphabet_prefix"},
	"date": notionViewDateGroupings, "created_time": notionViewDateGroupings, "last_edited_time": notionViewDateGroupings,
}

// notionViewGroupShapeRefusal is Notion's refusal of a view's group (path
// group_by or sub_group_by, fields as given) by its type, in its
// words; "" for one it takes.
func notionViewGroupShapeRefusal(path string, fields map[string]json.RawMessage) string {
	at := "body.configuration." + path + "."
	shouldBe := func(field, kind string, value json.RawMessage) string {
		return notionBodyFault(at+field, kind, notionViewRefusedValue(value))
	}
	var typeName string
	if _, given := fields["type"]; !given {
		return notionFaultSaid("body", notionFaultMissing(at+"type"))
	}
	if json.Unmarshal(fields["type"], &typeName) != nil || !slices.Contains(notionViewGroupTypes, typeName) {
		return shouldBe("type", notionViewChoices(notionViewGroupTypes), fields["type"])
	}
	if units, named := notionViewGroupUnits[typeName]; named || typeName == "formula" {
		by, given := fields["group_by"]
		var unit string
		switch {
		case !given:
			return notionFaultSaid("body", notionFaultMissing(at+"group_by"))
		case named && (json.Unmarshal(by, &unit) != nil || !slices.Contains(units, unit)):
			return shouldBe("group_by", notionViewChoices(units), by)
		}
	}
	if start, given := fields["start_day_of_week"]; given && notionViewDateGroupType(typeName) && string(start) != "0" && string(start) != "1" {
		return shouldBe("start_day_of_week", "`0`, `1`, or `undefined`", start)
	}
	if refusal := notionViewNumberRangeRefusal(at, fields); typeName == "number" && refusal != "" {
		return refusal
	}
	if hide, given := fields["hide_empty_groups"]; given && string(hide) != "true" && string(hide) != "false" {
		return shouldBe("hide_empty_groups", "a boolean or `undefined`", hide)
	}
	return ""
}

// notionViewNumberRangeRefusal is Notion's refusal of a number group's
// ranges (at the path its fields hang on, "…x_axis.", fields as given):
// whole numbers, the size at least 1, each left out or given; "" for ranges
// it takes, start past end too (seen 2026-11-12, on tables', boards' and
// charts' groups alike).
func notionViewNumberRangeRefusal(at string, fields map[string]json.RawMessage) string {
	for _, field := range notionViewNumberRangeFields {
		value, given := fields[field]
		var number float64
		kind := ""
		switch {
		case !given:
		case json.Unmarshal(value, &number) != nil || string(value) == "null":
			kind = "a number or `undefined`"
		case number != math.Trunc(number):
			kind = "an integer or `undefined`"
		case field == "range_size" && number < 1:
			kind = "≥ `1` or `undefined`"
		}
		if kind != "" {
			return notionBodyFault(at+field, kind, notionViewRefusedValue(value))
		}
	}
	return ""
}

// applyOfficialViewGroupUnit writes how a group (settings: the view's own, a
// board's sub-groups' own) by a property of groupType splits its values, as
// given: a status's by its groups or options, a status grouped as another
// type by its options (seen 2026-11-12), a text's by its exact values or
// first letters.
func applyOfficialViewGroupUnit(groupType string, group, settings map[string]any) {
	typeName, _ := group["type"].(string)
	switch {
	case typeName == "status":
		settings["groupByMode"] = map[bool]string{true: "option", false: "group"}[group["group_by"] == "option"]
	case groupType == "status":
		settings["groupByMode"] = "option"
	}
	delete(settings, "textGroupBy")
	if notionViewTextGroupType(typeName) && group["group_by"] == "alphabet_prefix" {
		settings["textGroupBy"] = "letter"
	}
	delete(settings, "numberGroupBy")
	delete(settings, "numberGroupRange")
	if typeName == "number" {
		ends := map[string]any{"start": 0.0, "end": 0.0, "step": 1.0}
		for index, key := range []string{"start", "end", "step"} {
			if value, given := group[notionViewNumberRangeFields[index]].(float64); given {
				ends[key] = value
			}
		}
		settings["numberGroupBy"], settings["numberGroupRange"] = "range", ends
	}
}

// applyOfficialViewSubGroupDates writes how a board's sub-groups
// (settings) by dates split them, as given: by how near today until a unit
// is given, the week starting where given. Notion's API reads a sub-group's
// unit back as given and its screen groups by it (seen 2026-11-12: "month"
// descending, then "주별" picked on the screen read back "week"). A
// sub-group by anything else keeps none.
func applyOfficialViewSubGroupDates(group, settings map[string]any) error {
	delete(settings, "dateGroupBy")
	keepGivenWeekStart(group, settings, notionViewGivenWeekStartKey)
	if typeName, _ := group["type"].(string); !notionViewDateGroupType(typeName) {
		return nil
	}
	by, _ := group["group_by"].(string)
	start, startSet := group["start_day_of_week"]
	startValue, startOK := start.(float64)
	if by != "" && !slices.Contains(notionViewDateGroupings, by) || startSet && !(startOK && (startValue == 0 || startValue == 1)) {
		return notionViewFilterInvalid("a date group is relative, day, week, month or year, with the week starting on 0 or 1")
	}
	if by != "" && by != "relative" {
		settings["dateGroupBy"] = by
	}
	return nil
}

// notionViewGivenWeekStartKey is where a group keeps the start_day_of_week
// given through the views API, apart from the day the screen starts its
// weeks on ("startDayOfWeek", a chart's own, notionViewChartUnitKeys):
// Notion's API reads it back as given, whatever the unit, and neither moves
// the other (seen 2026-11-12: a table by weeks given 1 drew its weeks from
// Sunday and still read 1 once Monday and then Sunday were picked on the
// screen; given 1 by month and 0 by how near today, a board's sub-groups' 1,
// and a chart's X axis' and stacked groups' each read back as given).
const notionViewGivenWeekStartKey = "givenStartDayOfWeek"

// keepGivenWeekStart keeps the group's given start_day_of_week in settings
// under key, or none.
func keepGivenWeekStart(group, settings map[string]any, key string) {
	delete(settings, key)
	if start, ok := group["start_day_of_week"].(float64); ok && (start == 0 || start == 1) {
		settings[key] = start
	}
}

// withGivenWeekStart reads a group's given start_day_of_week back
// (keepGivenWeekStart).
func withGivenWeekStart(group, settings map[string]any, key string) {
	if start, ok := settings[key].(float64); ok && (start == 0 || start == 1) {
		group["start_day_of_week"] = int(start)
	}
}

// withDateUnit is a date group read back with its unit: a board's
// sub-group's from its own settings, a view's group's from the view's; the
// day its weeks start on is the one given through the API, not the screen's
// (notionViewGivenWeekStartKey; seen 2026-11-12: Monday picked on the
// screen, "week" read back alone).
func withDateUnit(group, config, settings map[string]any, sub bool) {
	units := config
	if sub {
		units = settings
	}
	if by, ok := units["dateGroupBy"].(string); ok && slices.Contains(notionViewDateGroupings, by) {
		group["group_by"] = by
	}
	withGivenWeekStart(group, units, notionViewGivenWeekStartKey)
}

// withNumberRanges is a number group read back with its ranges (settings:
// the view's own, a board's sub-groups'), the screen's 0 to 1000 by 100
// until set; one by each value reads its ranges too, as Notion's API has no
// word for it (seen 2026-11-12: "숫자별 고유" and "범위" picked on the screen
// both read 0, 1000, 100).
func withNumberRanges(group map[string]any, settings map[string]any) {
	ends, _ := settings["numberGroupRange"].(map[string]any)
	for index, key := range []string{"start", "end", "step"} {
		value, given := ends[key].(float64)
		if !given {
			value = []float64{0, 1000, 100}[index]
		}
		group[notionViewNumberRangeFields[index]] = value
	}
}

// officialViewGivenGroupType is the type a group by the property was given
// as where its property's is another (applyOfficialViewGrouping); "" for none.
func officialViewGivenGroupType(settings map[string]any, propertyID string) string {
	given, _ := settings["officialGroupType"].(map[string]any)
	typeName, _ := given["type"].(string)
	if given["property"] != propertyID || !slices.Contains(notionViewGroupTypes, typeName) {
		return ""
	}
	return typeName
}

// notionViewGroupExtra is what a group of the type reads back besides its
// type, property and sort until its settings say otherwise.
func notionViewGroupExtra(typeName string) map[string]any {
	switch {
	case typeName == "status":
		return map[string]any{"group_by": "group"}
	case notionViewTextGroupType(typeName):
		return map[string]any{"group_by": "exact"}
	case notionViewDateGroupType(typeName):
		return map[string]any{"group_by": "relative"}
	}
	return nil
}

// notionViewSortingGroupType is the type a group's sort is read by: its
// property's, but a number's or a date's for values (a text, a URL, a
// number) given as a number or a date, which sort by value as those do
// (seen 2026-11-12: a text grouped as a number put its group for no value
// first); a group of options given as another type keeps its options'.
func notionViewSortingGroupType(groupType string, group map[string]any) string {
	typeName, _ := group["type"].(string)
	options := slices.Contains([]string{"status", "select", "multi_select", "person", "created_by", "last_edited_by", "checkbox"}, groupType)
	if !options && (typeName == "number" || notionViewDateGroupType(typeName)) {
		return typeName
	}
	return groupType
}

// notionViewDateGroupType is whether a group of the type groups by dates.
func notionViewDateGroupType(typeName string) bool {
	return typeName == "date" || typeName == "created_time" || typeName == "last_edited_time"
}

// notionViewTextGroupType is whether a group of the type groups by text.
func notionViewTextGroupType(typeName string) bool {
	return slices.Contains([]string{"text", "title", "url", "email", "phone_number"}, typeName)
}

// officialViewGroupShapeRefusal is notionViewGroupShapeRefusal of a group
// read for its key (groupBy, subGroupBy), on every path that writes one.
func officialViewGroupShapeRefusal(key string, group map[string]any) string {
	fields := map[string]json.RawMessage{}
	raw, err := json.Marshal(group)
	if err != nil || json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	return notionViewGroupShapeRefusal(map[string]string{"groupBy": "group_by", "subGroupBy": "sub_group_by"}[key], fields)
}

// notionViewChoices are values as Notion's refusal lists them: `"a"` or
// `"b"`; `"a"`, `"b"`, or `"c"`.
func notionViewChoices(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = "`\"" + value + "\"`"
	}
	if len(quoted) == 2 {
		return quoted[0] + " or " + quoted[1]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + ", or " + quoted[len(quoted)-1]
}

// applyOfficialViewEmptyGroups writes whether a view's groups (settings: the
// view's own, a board's sub-groups' own) by a property whose groups are of
// groupType leave out their empty ones: as given, else shown — but for a
// group given as another type, which shows only the groups its cards make
// (seen 2026-11-12: a select grouped as a status, a person or a text, a
// number as a select, a text as a number; a non-empty '없음' still shows).
func applyOfficialViewEmptyGroups(field, groupType string, group, settings map[string]any) error {
	hide, given := group["hide_empty_groups"]
	if !given {
		settings["hideEmptyGroups"] = group["type"] != groupType
		return nil
	}
	flag, ok := hide.(bool)
	if !ok {
		return notionViewFilterInvalid("%s", notionBodyFault(
			"body.configuration."+field+".hide_empty_groups", "a boolean or `undefined`", notionViewQuoted(hide)))
	}
	settings["hideEmptyGroups"] = flag
	return nil
}

// hidesEmptyGroups is whether a view's groups by a property (settings: the
// view's own, or a board's sub-groups' own when sub) leave out their empty
// ones as the screen draws them (viewHidesEmptyGroups).
func (scope notionViewFilterScope) hidesEmptyGroups(viewType, propertyID string, settings map[string]any, sub bool) bool {
	return viewHidesEmptyGroups(viewType, scope.byID[propertyID].NativeType, settings, sub)
}

// viewHidesEmptyGroups is whether a view's groups by a property of the
// native type leave out their empty ones as the screen draws them: a board's
// by options, people or a box only when told to, any other's unless told
// not to (boardColumns.ts, tableGroupSettingsModel.ts, boardSubgroupModel.ts).
func viewHidesEmptyGroups(viewType, nativeType string, settings map[string]any, sub bool) bool {
	switch nativeType {
	case graphValueTypeSelect, graphValueTypeMultiSelect, graphValueTypeStatus, graphValueTypePerson, graphValueTypeCheckbox:
		if viewType == graphViewTypeBoard && !sub {
			return settings["hideEmptyGroups"] == true
		}
	}
	return settings["hideEmptyGroups"] != false
}

// withEmptyGroupsHidden is a group read back with hide_empty_groups true
// while its empty groups hide.
func (scope notionViewFilterScope) withEmptyGroupsHidden(group any, viewType, propertyID string, settings map[string]any, sub bool) any {
	// One given as another type hides them without saying so, as Notion's reads it back;
	// one given as a formula's says so, either way, where it was told (seen 2026-11-12).
	asGiven := officialViewGivenGroupType(settings, propertyID) != ""
	given, _ := settings["officialGroupType"].(map[string]any)
	_, told := given["hide_empty_groups"]
	read, ok := group.(map[string]any)
	switch {
	case ok && asGiven && told:
		read["hide_empty_groups"] = scope.hidesEmptyGroups(viewType, propertyID, settings, sub)
	case ok && !asGiven && scope.hidesEmptyGroups(viewType, propertyID, settings, sub):
		read["hide_empty_groups"] = true
	}
	return group
}
