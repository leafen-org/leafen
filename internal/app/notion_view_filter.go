package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Official view filters and sorts use the data-source query format. Native
// views keep the same meaning as a filter group of property operators, so
// this file translates both ways. Dates compare by day like native filters.

type notionViewProperty struct {
	ID, Name, NativeType, OfficialType string
	// RolledType is, for a rollup that shows its related values or counts
	// them per group, the official type of the property it rolls up: its
	// any, every and none filters compare each value as that type, as
	// Notion's do (notionViewRollupLeafKind).
	RolledType string
	// RollupResult is, for a rollup, what Notion's API reads it as, which its
	// filters must name (notionViewRollupResult).
	RollupResult string
	// FormulaResult is, for a formula, the kind of filter its result takes
	// (notionViewFormulaResult), "" where unknown.
	FormulaResult string
	// Options are a select's, multi-select's or status's options and Groups a
	// status's groups (notion_view_filter_options.go); nil for any other.
	// RolledOptions are those of the property a rollup rolls up.
	Options, Groups, RolledOptions []notionViewOption
}

type notionViewFilterScope struct {
	byID, byName map[string]notionViewProperty
	ordered      []notionViewProperty
	// timestamps are the page timestamps a filter may name without a
	// property, {"timestamp": "created_time", ...}: a data source query's and
	// a view's, which Notion's views keep and apply (seen 2026-11-11). Only a
	// data source query sorts on them (sortsTimestamps); a view's timestamp
	// sorts are left out (notionViewCreateSorts).
	timestamps      map[string]notionViewProperty
	sortsTimestamps bool
	// ids are the data source's properties' public IDs, which a view names
	// them by (notion_property_public_id.go).
	ids notionPropertyIDs
	// withheld are the properties the reader sees in no row; a view change
	// keeps them as they showed.
	withheld     map[string]struct{}
	userToNative func(string) (string, bool)
	rowToNative  func(string) (string, bool)
	userToPublic func(string) string
	rowToPublic  func(string) string
	// wholePropertyLists takes a view's list of properties as long as its data
	// source's, as a Notion import brings a view over; the official API's
	// request takes at most 100.
	wholePropertyLists bool
	// refusesUnknownOptions refuses a filter naming an option its property
	// doesn't have, as Notion's API does (nativeOptionValues).
	refusesUnknownOptions bool
	// checksFormOnly checks a filter's form alone (formOnly).
	checksFormOnly bool
	// readsNotionForm reads a filter as Notion's own views give it (a Notion
	// import), nested deeper than its API's requests take, without checking
	// it as a request's (notionDataSourceFilterShape).
	readsNotionForm bool
}

// notionViewPropertyListItems is how many properties the official API's
// view configuration lists at once.
const notionViewPropertyListItems = 100

// propertyListFits says whether a view's list of properties is one to take.
func (scope notionViewFilterScope) propertyListFits(items int) bool {
	return scope.wholePropertyLists || items <= notionViewPropertyListItems
}

// withPageTimestamps lets a filter name when a page was created or last
// edited, under the query's own timestamp properties
// (database_query_row_timestamps.go), which a view keeps as it does a
// property's.
func (scope notionViewFilterScope) withPageTimestamps() notionViewFilterScope {
	scope.timestamps = map[string]notionViewProperty{
		"created_time": {
			ID: databaseQueryRowCreatedTimeID, Name: "created_time",
			NativeType: graphValueTypeCreatedTime, OfficialType: "created_time",
		},
		"last_edited_time": {
			ID: databaseQueryRowEditedTimeID, Name: "last_edited_time",
			NativeType: graphValueTypeLastEditedTime, OfficialType: "last_edited_time",
		},
	}
	return scope
}

// withTimestampSorts lets a data source query sort on its page timestamps too.
func (scope notionViewFilterScope) withTimestampSorts() notionViewFilterScope {
	scope = scope.withPageTimestamps()
	scope.sortsTimestamps = true
	return scope
}

// timestampNamed is the page timestamp a stored filter names by its ID.
func (scope notionViewFilterScope) timestampNamed(id string) (notionViewProperty, bool) {
	for _, property := range scope.timestamps {
		if property.ID == id {
			return property, true
		}
	}
	return notionViewProperty{}, false
}

func newNotionViewFilterScope(properties []notionViewProperty) notionViewFilterScope {
	scope := notionViewFilterScope{
		byID: map[string]notionViewProperty{}, byName: map[string]notionViewProperty{}, ordered: properties,
	}
	for _, property := range properties {
		scope.byID[property.ID] = property
		scope.byName[property.Name] = property
		if property.OfficialType == notionTitlePropertyID {
			scope.byID[notionTitlePropertyID] = property
		}
	}
	return scope
}

func notionViewConditionFamily(officialType string) string {
	switch officialType {
	case "title", "rich_text", "url", "email", "phone_number":
		return "text"
	case "number", "unique_id":
		return "number"
	case "select", "status":
		return "select"
	case "multi_select":
		return "multi"
	case "date", "created_time", "last_edited_time":
		return "date"
	case "people", "created_by", "last_edited_by":
		return "people"
	case "checkbox", "relation", "files":
		return officialType
	}
	return ""
}

// notionViewComputedFamilies are the kinds of value a formula's or rollup's
// filter names inside it, as Notion's: {"formula": {"number": {...}}}.
var notionViewComputedFamilies = map[string]map[string]string{
	"formula": {"string": "text", "number": "number", "checkbox": "checkbox", "date": "date"},
	"rollup":  {"number": "number", "date": "date"},
}

// notionViewFilterDay is a day, alone or with a time of day.
var notionViewFilterDay = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}.*)?$`)

// notionViewComputedCondition reads the kind a formula's or rollup's filter
// names and the condition inside it (2026-10-16: neither shape was taken):
// of several, the first in Notion's order whose form is right. A kind it
// doesn't name, or one given something other than an object, is refused as
// Notion's API refuses it, at path (body.filter.formula), with the kinds it
// names.
func (scope notionViewFilterScope) notionViewComputedCondition(officialType string, raw json.RawMessage, path string) (string, string, json.RawMessage, error) {
	kinds := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &kinds)
	kind := notionViewFirstFitting(notionViewComputedKindLists[officialType], kinds, func(kind string) bool {
		return scope.computedFormFits(officialType, kind, kinds[kind], path)
	})
	family := notionViewComputedFamilies[officialType][kind]
	switch {
	case family == "":
		return "", "", nil, notionViewFixOne(path, notionViewComputedKindLists[officialType], "", "", nil)
	case !notionViewJSONObject(kinds[kind]):
		return "", "", nil, notionViewFixOne(path, notionViewComputedKindLists[officialType], kind, "an object", kinds[kind])
	}
	return family, kind, kinds[kind], nil
}

// notionViewComputedKind is the kind a stored formula or rollup filter reads
// back as, from its condition: a date, a yes or no, a number, else text.
func notionViewComputedKind(officialType string, filter databaseQueryFilter) (string, string) {
	var value any
	_ = json.Unmarshal(filter.Value, &value)
	text, _ := value.(string)
	_, number := value.(float64)
	_, flag := value.(bool)
	dateOnly := notionViewConditionsByType["date"][filter.Operator] != "" &&
		notionViewConditionsByType["text"][filter.Operator] == "" && notionViewConditionsByType["number"][filter.Operator] == ""
	numberOnly := notionViewConditionsByType["number"][filter.Operator] != "" && notionViewConditionsByType["text"][filter.Operator] == ""
	kind := ""
	switch {
	case dateOnly || notionViewFilterDay.MatchString(text):
		kind = "date"
	case flag && officialType == "formula":
		kind = "checkbox"
	case number || numberOnly:
		kind = "number"
	case officialType == "formula" || officialType == "rollup":
		kind = "string"
	}
	return kind, notionViewComputedFamilies[officialType][kind]
}

func notionViewFilterInvalid(format string, arguments ...any) error {
	return notionDatabaseRowPageRequestInvalid(format, arguments...)
}

// nativeViewFilterGroup reads an official filter into a native filter group:
// the whole filter's form first, then what it names, as Notion's API checks
// it (seen 2026-11-11: a filter's second rule refused for its form before
// its first for a property the source doesn't have).
func (scope notionViewFilterScope) nativeViewFilterGroup(raw json.RawMessage) (*databaseQueryFilterGroup, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if !scope.readsNotionForm {
		if err := notionViewFilterShapeRefusal(notionDataSourceFilterShape, raw, "body.filter"); err != nil {
			return nil, err
		}
	}
	if !scope.checksFormOnly {
		if _, err := scope.formOnly().nativeViewFilterGroup(raw); err != nil {
			return nil, err
		}
	}
	group := &databaseQueryFilterGroup{Conjunction: databaseQueryConjunctionAnd}
	if err := scope.addNativeViewFilter(group, raw, 1, "body.filter"); err != nil {
		return nil, err
	}
	plainDatabaseQueryFilterOrder(group)
	return group, nil
}

// A filter group's rules and groups keep the order the official filter
// lists them in (Order), as Notion's views show them; where that is the
// rules first, it says nothing.
func plainDatabaseQueryFilterOrder(group *databaseQueryFilterGroup) {
	plain := true
	for index, term := range group.Order {
		if (index < len(group.Filters)) != (term == databaseQueryFilterTermFilter) {
			plain = false
		}
	}
	if plain {
		group.Order = nil
	}
	for index := range group.Groups {
		plainDatabaseQueryFilterOrder(&group.Groups[index])
	}
}

// databaseQueryFilterTerm is one of a group's rules or groups, by its index
// in Filters or Groups.
type databaseQueryFilterTerm struct {
	group bool
	index int
}

// databaseQueryFilterTerms are a group's rules and groups in Order, the
// rules first where it has none or doesn't count them all.
func databaseQueryFilterTerms(group *databaseQueryFilterGroup) []databaseQueryFilterTerm {
	terms := []databaseQueryFilterTerm{}
	filters, groups := 0, 0
	for _, term := range group.Order {
		switch {
		case term == databaseQueryFilterTermFilter && filters < len(group.Filters):
			terms = append(terms, databaseQueryFilterTerm{index: filters})
			filters++
		case term == databaseQueryFilterTermGroup && groups < len(group.Groups):
			terms = append(terms, databaseQueryFilterTerm{group: true, index: groups})
			groups++
		}
	}
	for ; filters < len(group.Filters); filters++ {
		terms = append(terms, databaseQueryFilterTerm{index: filters})
	}
	for ; groups < len(group.Groups); groups++ {
		terms = append(terms, databaseQueryFilterTerm{group: true, index: groups})
	}
	return terms
}

// addNativeViewFilter reads the official filter at path (body.filter,
// body.filter.and[0], body.quick_filters.Due) into group; a refusal names
// where in the request it refuses, as Notion's API does.
func (scope notionViewFilterScope) addNativeViewFilter(group *databaseQueryFilterGroup, raw json.RawMessage, depth int, path string) error {
	object := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &object) != nil || len(object) == 0 {
		return notionViewFilterInvalid("the view filter is invalid")
	}
	// Its form is one Notion's API takes (notionDataSourceFilterShape),
	// checked before anything is read. A property's condition is read beside
	// a compound's key, the first kind it names, and a timestamp's beside
	// one; a property naming no kind, or a timestamp not its own, leaves the
	// compound to be read, "or" before "and" (seen 2026-11-11 and 11-12).
	var name, reference string
	_, named := object["property"]
	_ = json.Unmarshal(object["timestamp"], &name)
	_ = json.Unmarshal(object["property"], &reference)
	key := notionViewFirstFilterKey(object)
	timestamp := !named && (name == "created_time" || name == "last_edited_time") && object[name] != nil
	if timestamp {
		key = name
	}
	for _, conjunction := range []string{"or", "and"} {
		children, compound := object[conjunction]
		if !compound || named && key != "" || timestamp {
			continue
		}
		items := []json.RawMessage{}
		if json.Unmarshal(children, &items) != nil || depth > notionAPIFilterDepth {
			return notionViewFilterInvalid("the view filter %q is invalid", conjunction)
		}
		nested := databaseQueryFilterGroup{Conjunction: conjunction}
		for index, item := range items {
			if err := scope.addNativeViewFilter(&nested, item, depth+1, fmt.Sprintf("%s.%s[%d]", path, conjunction, index)); err != nil {
				return err
			}
		}
		if depth == 1 && len(group.Filters) == 0 && len(group.Groups) == 0 {
			*group = nested
		} else {
			group.Groups = append(group.Groups, nested)
			group.Order = append(group.Order, databaseQueryFilterTermGroup)
		}
		return nil
	}
	if key == "" || !timestamp && !named {
		// Only a verification's, which Leafen's views don't filter by.
		return notionViewFilterInvalid("the view property filter is invalid")
	}
	// Its form first, from its key alone, as Notion's API checks it (seen
	// 2026-11-11: one on a property the source doesn't have is refused for
	// its form first).
	form, err := scope.viewFilterForm(key, object[key], path)
	if err != nil || scope.checksFormOnly {
		return err
	}
	property, found := scope.timestamps[key]
	if !timestamp {
		if property, found = scope.named(reference); !found {
			// A property the source doesn't have, refused as Notion's API refuses it (seen 2026-11-09).
			return notionViewFilterInvalid("No property has the name or ID: %s", reference)
		}
		if err := notionViewFilterKindFits(property, key, form.computedKind, form.condition); err != nil {
			return err
		}
	} else if !found {
		return notionViewFilterInvalid("the timestamp filter is invalid")
	}
	return scope.addViewFilterForm(group, property, form)
}

// nativeViewCondition reads one condition at path (body.filter.date). One
// the family doesn't name, or a value the condition can't take, is refused
// with what Notion's API lists there (notionViewConditionRefusal; 2026-10-29:
// a day that wasn't one read as an unsupported condition).
func (scope notionViewFilterScope) nativeViewCondition(
	property notionViewProperty, family, condition string, raw json.RawMessage, path string,
) (databaseQueryFilter, error) {
	kind, supported := notionViewConditionsByType[family][condition]
	invalid := notionViewConditionRefusal(path, family, condition, raw)
	// No value is none of the kinds a condition takes (seen 2026-11-12:
	// "contains": null refused as no string).
	if !supported || strings.TrimSpace(string(raw)) == "null" {
		return databaseQueryFilter{}, invalid
	}
	operator, value := condition, any(nil)
	switch kind {
	case "t":
		var flag bool
		if json.Unmarshal(raw, &flag) != nil || !flag {
			return databaseQueryFilter{}, invalid
		}
	case "s":
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return databaseQueryFilter{}, invalid
		}
		value = text
	case "n":
		var number float64
		if json.Unmarshal(raw, &number) != nil {
			return databaseQueryFilter{}, invalid
		}
		value = number
	case "b":
		var flag bool
		if json.Unmarshal(raw, &flag) != nil {
			return databaseQueryFilter{}, invalid
		}
		value = flag
	case "o":
		values, err := notionViewOptionValues(raw, path+"."+condition)
		if errors.Is(err, errNotionViewOptionShape) {
			return databaseQueryFilter{}, invalid
		}
		if err != nil {
			return databaseQueryFilter{}, err
		}
		ids, err := scope.nativeOptionValues(property, values)
		if err != nil {
			return databaseQueryFilter{}, err
		}
		value = ids
	case "u", "p":
		// One page or person, as Notion's API takes it: a list is refused
		// (seen 2026-11-12, "should be a string").
		values, ok := notionViewSingleStringValue(raw)
		if !ok {
			if object := map[string]any{}; json.Unmarshal(raw, &object) == nil && object["kind"] == "leafen.current_user" {
				return databaseQueryFilter{PropertyID: property.ID, Operator: condition, Value: raw}, nil
			}
			return databaseQueryFilter{}, invalid
		}
		resolve := scope.userToNative
		if kind == "p" {
			resolve = scope.rowToNative
		}
		// Each names a page or a person by a UUID as Notion's bodies take
		// one (notionBodyUUID), or someone as "me" (seen 2026-11-12).
		for _, item := range values {
			text := item.(string)
			if !notionBodyUUID.MatchString(text) && !leafenOwnID.MatchString(text) && (kind == "p" || text != "me") {
				return databaseQueryFilter{}, invalid
			}
		}
		ids := []any{}
		for _, item := range values {
			id, found := resolve(item.(string))
			if !found {
				// Someone or a page unknown is simply in no row, as Notion's
				// (seen 2026-11-05, a page 2026-11-12: one named in capitals too).
				ids = append(ids, item)
				continue
			}
			ids = append(ids, id)
		}
		value = ids
	case "d":
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return databaseQueryFilter{}, invalid
		}
		// A day keeps its condition as written, before and after too, as
		// Notion's view reads it back (seen 2026-11-11). A time of day is
		// kept as written: the filter compares moments to the minute while
		// it reads back as its day (officialViewCondition), as Notion's.
		text = strings.TrimSpace(text)
		// A day named from today ("one_week_ago") stays so, as Notion's
		// "사용자 지정 날짜 ⌄" choices keep it (database_query_date_policy.go).
		if _, relative := databaseQueryRelativeFilterDays[text]; relative {
			value = text
			break
		}
		day, err := time.Parse("2006-01-02", strings.SplitN(text, "T", 2)[0])
		if _, timed := databaseQueryFilterMinute(text); err != nil || strings.Contains(text, "T") && !timed {
			return databaseQueryFilter{}, invalid
		}
		value = day.Format("2006-01-02")
		if strings.Contains(text, "T") {
			value = text
		}
	case "e":
		object := map[string]any{}
		if json.Unmarshal(raw, &object) != nil || len(object) != 0 {
			return databaseQueryFilter{}, invalid
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return databaseQueryFilter{}, err
	}
	return databaseQueryFilter{PropertyID: property.ID, Operator: operator, Value: encoded}, nil
}

// notionViewSingleStringValue is the one string a condition names, which
// must not be empty.
func notionViewSingleStringValue(raw json.RawMessage) ([]any, bool) {
	var single string
	if json.Unmarshal(raw, &single) != nil {
		return nil, false
	}
	return []any{single}, single != ""
}

var errNotionViewOptionShape = errors.New("option condition value is neither a string nor an array")

// notionViewOptionValues are the options a condition names, as a string or
// a list of strings; an empty name, or none, names none, the condition then
// waiting for its value and filtering nothing, as Notion's API takes it
// (seen 2026-11-12: "", [] and [""] each kept every row). A list holding
// something else is refused at that item, as Notion's API refuses it, in
// Leafen's words.
func notionViewOptionValues(raw json.RawMessage, path string) ([]any, error) {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		if single == "" {
			return []any{}, nil
		}
		return []any{single}, nil
	}
	many := []json.RawMessage{}
	if json.Unmarshal(raw, &many) != nil {
		return nil, errNotionViewOptionShape
	}
	values := make([]any, 0, len(many))
	for index, item := range many {
		var name string
		if json.Unmarshal(item, &name) != nil {
			return nil, notionViewFilterInvalid("%s", notionBodyFault(
				fmt.Sprintf("%s[%d]", path, index), "a string", notionViewRefusedValue(item)))
		}
		if name != "" {
			values = append(values, name)
		}
	}
	return values, nil
}

// officialViewFilter projects a native filter group in the official format.
// A rule still waiting for its value is left out, as it filters nothing, and
// so is one the official format has no words for (notionViewUnnamedCondition);
// a group left with none reads as no filter; a group reads as its one rule
// only when it holds that rule alone, as Notion's view reads them (seen
// 2026-11-11: six rules, five without a value, read as an "and" of the
// sixth; one rule alone, as itself; all six without, as null; a span from
// today or a range of days beside a text rule, as an "and" of the text rule).
func (scope notionViewFilterScope) officialViewFilter(group *databaseQueryFilterGroup) any {
	if group == nil || len(group.Filters)+len(group.Groups) == 0 {
		return nil
	}
	children := []any{}
	// held counts the rules and groups the view shows, those waiting for a
	// value among them.
	held := 0
	for _, term := range databaseQueryFilterTerms(group) {
		var projected any
		if term.group {
			held++
			projected = scope.officialViewFilter(&group.Groups[term.index])
		} else {
			filter := group.Filters[term.index]
			if projected = scope.officialViewCondition(filter); projected != nil {
				held++
			}
			if _, unnamed := projected.(notionViewUnnamedCondition); unnamed || databaseQueryFilterAwaitsValue(filter) {
				projected = nil
			}
		}
		if projected != nil {
			children = append(children, projected)
		}
	}
	if len(children) == 0 {
		return nil
	}
	if held == 1 && len(group.Groups) == 0 {
		return children[0]
	}
	conjunction := group.Conjunction
	if conjunction != "or" {
		conjunction = "and"
	}
	return map[string]any{conjunction: children}
}

// notionViewUnnamedCondition is a rule whose condition Notion's API has no
// words for, a range of days or a span from today ("오늘 기준 지난 1주") set
// on the screen: a view's filter and quick filters read without it, the rule
// still counting among its group's rules, as Notion's (seen 2026-11-11).
type notionViewUnnamedCondition struct{}

// officialViewCondition projects one rule in the official format, or nil for
// a property the reader can't see.
func (scope notionViewFilterScope) officialViewCondition(filter databaseQueryFilter) any {
	property, found := scope.byID[filter.PropertyID]
	stamp, stamped := scope.timestampNamed(filter.PropertyID)
	if stamped {
		property = stamp
	} else if !found {
		return nil
	}
	family := notionViewConditionFamily(property.OfficialType)
	computedKind, rollupKind := "", ""
	// A rollup showing its related values filters each of them as the kind
	// it names, which Notion's API names under any, every or none.
	rollupWord := ""
	if property.OfficialType == "rollup" && property.RolledType != "" {
		rollupKind = notionViewRollupReadKind(property, filter)
		family = notionViewConditionFamily(rollupKind)
		rollupWord = "any"
		for word, match := range notionViewRollupMatches {
			if match != "" && match == filter.RollupMatch {
				rollupWord = word
			}
		}
	} else if notionViewComputedFamilies[property.OfficialType] != nil {
		computedKind, family = notionViewComputedKind(property.OfficialType, filter)
		if family == "" {
			// A rollup's text, which Notion's API has no words for.
			return notionViewUnnamedCondition{}
		}
	}
	operator := filter.Operator
	switch {
	case family == "select" && operator == "contains":
		operator = "equals"
	case family == "select" && operator == "does_not_contain":
		operator = "does_not_equal"
	case (family == "multi" || family == "people" || family == "relation") && operator == "equals":
		operator = "contains"
	case (family == "multi" || family == "people" || family == "relation") && operator == "does_not_equal":
		operator = "does_not_contain"
	}
	if _, named := notionViewConditionsByType[family][operator]; family != "" && !named {
		return notionViewUnnamedCondition{}
	}
	var value any = true
	if kind := notionViewConditionsByType[family][operator]; kind != "t" {
		if json.Unmarshal(filter.Value, &value) != nil {
			value = nil
		}
		if kind == "e" {
			value = map[string]any{}
		}
		if ids, ok := value.([]any); ok && (kind == "u" || kind == "p") {
			convert := scope.userToPublic
			if kind == "p" {
				convert = scope.rowToPublic
			}
			for index, id := range ids {
				ids[index] = convert(fmt.Sprint(id))
			}
			if len(ids) == 1 {
				value = ids[0]
			}
		}
		if kind == "o" {
			value = property.officialOptionValues(value)
		}
		if text, _ := value.(string); kind == "d" && len(text) > 10 && text[10] == 'T' {
			// A time of day reads back as the day it was written on.
			value = text[:10]
		}
		if values, ok := value.([]any); ok && kind == "o" && len(values) == 1 {
			value = values[0]
		}
	}
	if rollupWord != "" {
		return map[string]any{"property": scope.viewID(property.ID), "rollup": map[string]any{rollupWord: map[string]any{rollupKind: map[string]any{operator: value}}}}
	}
	if computedKind != "" {
		return map[string]any{"property": scope.viewID(property.ID), property.OfficialType: map[string]any{computedKind: map[string]any{operator: value}}}
	}
	if stamped {
		// A page timestamp's reads back by its name, as Notion's view gives it.
		return map[string]any{"timestamp": property.Name, property.Name: map[string]any{operator: value}}
	}
	return map[string]any{"property": scope.viewID(property.ID), property.OfficialType: map[string]any{operator: value}}
}

// nativeViewSorts reads official property sorts.
func (scope notionViewFilterScope) nativeViewSorts(raw json.RawMessage) ([]databaseQuerySort, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []databaseQuerySort{}, nil
	}
	items := []struct {
		Property  *string `json:"property"`
		Timestamp *string `json:"timestamp"`
		Direction string  `json:"direction"`
	}{}
	if json.Unmarshal(raw, &items) != nil {
		return nil, notionViewFilterInvalid("the view sorts are invalid")
	}
	sorts := make([]databaseQuerySort, 0, len(items))
	for index, item := range items {
		var property notionViewProperty
		found := false
		switch {
		case item.Timestamp != nil && item.Property == nil && scope.sortsTimestamps:
			property, found = scope.timestamps[*item.Timestamp]
		case item.Property == nil:
			// A view keeps property sorts only, and Notion's API says so of
			// a view's change in these words (seen 2026-11-11); a new view
			// leaves its timestamp sorts out first (notionViewCreateSorts).
			return nil, notionViewFilterInvalid("%s", notionFaultSaid("body", notionFaultMissing(fmt.Sprintf("body.sorts[%d].property", index))))
		case item.Timestamp != nil && scope.sortsTimestamps:
			return nil, notionViewFilterInvalid("a sort names a timestamp or a property, not both")
		default:
			// A view's sort naming a timestamp too sorts by its property, as
			// Notion's (seen 2026-11-11).
			property, found = scope.named(*item.Property)
		}
		if !found && item.Property != nil {
			return nil, notionViewFilterInvalid("No property to sort by has the name or ID: %s", *item.Property)
		}
		if !found || item.Direction != databaseQuerySortAscending && item.Direction != databaseQuerySortDescending {
			return nil, notionViewFilterInvalid("the view sort is invalid")
		}
		sorts = append(sorts, databaseQuerySort{PropertyID: property.ID, Direction: storedViewSortDirection(item.Direction)})
	}
	return sorts, nil
}

// notionViewCreateSorts are a new view's sorts as Notion's API takes them
// (seen 2026-11-11): a timestamp sort, which a view can't keep, is checked
// and left out, the view made without it, as Notion's is.
func notionViewCreateSorts(raw json.RawMessage) (json.RawMessage, error) {
	items := []map[string]json.RawMessage{}
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &items) != nil {
		return raw, nil
	}
	kept := make([]map[string]json.RawMessage, 0, len(items))
	for _, item := range items {
		stamp, timed := item["timestamp"]
		if _, named := item["property"]; named || !timed {
			kept = append(kept, item)
			continue
		}
		var name, direction string
		if json.Unmarshal(stamp, &name) != nil || name != "created_time" && name != "last_edited_time" ||
			json.Unmarshal(item["direction"], &direction) != nil ||
			direction != databaseQuerySortAscending && direction != databaseQuerySortDescending {
			return nil, notionViewFilterInvalid("the view sort is invalid")
		}
	}
	return json.Marshal(kept)
}

// storedViewSortDirection is a sort's direction as a view keeps it, the same
// whether the app or Notion's words wrote it: asc or desc.
func storedViewSortDirection(direction string) string {
	if direction == databaseQuerySortDescending || direction == "desc" {
		return "desc"
	}
	return "asc"
}

func (scope notionViewFilterScope) officialViewSorts(sorts []databaseQuerySort) any {
	if len(sorts) == 0 {
		return nil
	}
	projected := make([]any, 0, len(sorts))
	for _, sort := range sorts {
		if _, found := scope.byID[sort.PropertyID]; !found {
			continue
		}
		direction := databaseQuerySortAscending
		if sort.Direction == "desc" || sort.Direction == databaseQuerySortDescending {
			direction = databaseQuerySortDescending
		}
		projected = append(projected, map[string]any{"property": scope.viewID(sort.PropertyID), "direction": direction})
	}
	return projected
}
