package app

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"slices"
	"strings"
)

// Quick filters are a view's filter-bar pills: one condition per property,
// kept beside the advanced filter and applied together with it. Official
// requests key them by property name or ID with a property filter condition
// that leaves out "property". Older imported entries that hold a whole group
// keep applying but are not pills, so they are not listed.

// applyOfficialQuickFilters merges official quick filters into a native view
// config: a key with a condition adds or replaces that property's pill, a key
// set to null removes it, a null field removes every pill, and pills the
// request does not mention stay.
func (scope notionViewFilterScope) applyOfficialQuickFilters(raw json.RawMessage, config map[string]any) error {
	if len(raw) == 0 {
		return nil
	}
	current, _ := config["quickFilters"].([]any)
	if string(raw) == "null" {
		current = slices.DeleteFunc(current, func(item any) bool {
			entry, _ := item.(map[string]any)
			_, single := entry["propertyId"]
			return single
		})
		return setQuickFilters(config, current)
	}
	entries := map[string]json.RawMessage{}
	ordered, err := parseNotionValue(raw)
	if json.Unmarshal(raw, &entries) != nil || entries == nil || err != nil {
		return notionViewFilterInvalid("the view quick filters are invalid")
	}
	// Each in the order the request names them, as Notion's API checks them
	// (seen 2026-11-11); their form alone, before any property is looked
	// up, where the scope checks only that.
	for _, key := range ordered.keys {
		property, found := scope.named(key)
		if scope.checksFormOnly {
			property, found = notionViewProperty{ID: key, Name: key}, true
		}
		if !found {
			// Refused as Notion's API refuses it (seen 2026-11-11), in Leafen's words.
			return notionViewFilterInvalid("The data source has no property %q to quick filter by.", key)
		}
		// An empty condition for a property that has a pill leaves that pill
		// as it is, as Notion's (seen 2026-11-12: {"N": {}} over "N > 1").
		if notionQuickFilterAwaits(entries[key]) && slices.ContainsFunc(current, func(item any) bool {
			entry, _ := item.(map[string]any)
			return entry["propertyId"] == property.ID
		}) {
			continue
		}
		current = slices.DeleteFunc(current, func(item any) bool {
			entry, _ := item.(map[string]any)
			return entry["propertyId"] == property.ID
		})
		if string(entries[key]) == "null" {
			continue
		}
		filter, err := scope.nativeQuickFilter(property, key, entries[key])
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(filter)
		if err != nil {
			return err
		}
		var entry map[string]any
		if err := json.Unmarshal(encoded, &entry); err != nil {
			return err
		}
		current = append(current, entry)
	}
	return setQuickFilters(config, current)
}

func setQuickFilters(config map[string]any, filters []any) error {
	if len(filters) == 0 {
		delete(config, "quickFilters")
	} else {
		config["quickFilters"] = filters
	}
	return nil
}

// nativeQuickFilter reads one quick filter condition, given under key,
// through the view filter translation; it must hold exactly one condition.
func (scope notionViewFilterScope) nativeQuickFilter(property notionViewProperty, key string, raw json.RawMessage) (databaseQueryFilter, error) {
	if err := notionViewFilterShapeRefusal(notionQuickFilterShape, raw, "body.quick_filters."+key); err != nil {
		return databaseQueryFilter{}, err
	}
	object := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &object) != nil {
		return databaseQueryFilter{}, notionViewFilterInvalid("the quick filter for property %q is invalid", property.Name)
	}
	// An empty condition is a pill waiting for its value, as Notion's (seen
	// 2026-11-12: {"Name": {}} reads back {} and keeps every row).
	if notionQuickFilterAwaits(raw) {
		return notionAwaitingQuickFilter(property), nil
	}
	reference, err := json.Marshal(property.ID)
	if err != nil {
		return databaseQueryFilter{}, err
	}
	object["property"] = reference
	encoded, err := json.Marshal(object)
	if err != nil {
		return databaseQueryFilter{}, err
	}
	group := databaseQueryFilterGroup{Conjunction: databaseQueryConjunctionAnd}
	if err := scope.addNativeViewFilter(&group, encoded, 2, "body.quick_filters."+key); err != nil {
		return databaseQueryFilter{}, notionViewQuickFilterRefusal(key, err)
	}
	if scope.checksFormOnly {
		return databaseQueryFilter{PropertyID: property.ID}, nil
	}
	if len(group.Filters) != 1 || len(group.Groups) != 0 {
		return databaseQueryFilter{}, notionViewFilterInvalid("a quick filter holds one condition")
	}
	return group.Filters[0], nil
}

// notionQuickFilterAwaits reports an empty quick filter condition, {}.
func notionQuickFilterAwaits(raw json.RawMessage) bool {
	object := map[string]json.RawMessage{}
	return json.Unmarshal(raw, &object) == nil && object != nil && len(object) == 0
}

// notionAwaitingQuickFilter is a pill waiting for its value, with the
// condition Notion's opens on (seen 2026-11-12): "contains" for a text, a
// relation, people or a multi-select, a text formula and a rollup showing
// such values, "equals" for any other — a number, a date, a checkbox, a
// select or status, a number or date rollup or formula.
func notionAwaitingQuickFilter(property notionViewProperty) databaseQueryFilter {
	kind := property.OfficialType
	switch property.NativeType {
	case graphValueTypeFormula:
		kind = notionFormulaResultType(property.FormulaResult)
	case graphValueTypeRollup:
		kind = property.RollupResult
		if kind == notionViewRollupEach {
			kind = notionViewRollupLeafKind(property.RolledType)
		}
	}
	operator := "equals"
	switch kind {
	case "title", "rich_text", "string", "url", "email", "phone_number", "multi_select", "people",
		"created_by", "last_edited_by", "relation":
		operator = "contains"
	}
	return databaseQueryFilter{PropertyID: property.ID, Operator: operator}
}

// notionViewQuickFilterRefusal names the quick filter a refusal is about, as
// Notion's API does for any refusal but one of the request's form (seen
// 2026-11-11: 'Invalid quick filter for property "Sum": ' and then why).
func notionViewQuickFilterRefusal(key string, err error) error {
	var invalid *notionDatabaseRowPageRequestError
	if !errors.As(err, &invalid) || strings.HasPrefix(invalid.message, notionFaultBodyHead) {
		return err
	}
	return notionViewFilterInvalid("Invalid quick filter for property %q: %s", key, invalid.message)
}

// officialQuickFilters lists the view's pills keyed by property ID in the
// order the view keeps them, as Notion's API lists them in the order they
// were given (seen 2026-11-12), or null when it has none. A pill still
// waiting for its value reads as an empty condition, as Notion's (seen
// 2026-11-11: {"<id>": {}}, a range of days without its days too); one with
// a value the official format has no words for, a range of days or a span
// from today set on the screen, is left out (notionViewUnnamedCondition).
func (scope notionViewFilterScope) officialQuickFilters(raw []json.RawMessage) any {
	pills := notionOrderedObject{}
	for _, item := range raw {
		filter := databaseQueryFilter{}
		if json.Unmarshal(item, &filter) != nil || filter.PropertyID == "" {
			continue
		}
		projected := scope.officialViewCondition(filter)
		if projected != nil && databaseQueryFilterAwaitsValue(filter) {
			projected = map[string]any{}
		}
		condition, ok := projected.(map[string]any)
		if !ok {
			continue
		}
		delete(condition, "property")
		pills = pills.with(scope.viewID(filter.PropertyID), condition)
	}
	if len(pills) == 0 {
		return nil
	}
	return pills
}

// notionOrderedObject is an object written with its keys in the order they
// were added, where a map alone is written in no set order.
type notionOrderedObject []notionOrderedMember

type notionOrderedMember struct {
	key   string
	value any
}

// with is the object with key's value set: in its place if it has one, last
// otherwise.
func (object notionOrderedObject) with(key string, value any) notionOrderedObject {
	for index := range object {
		if object[index].key == key {
			object[index].value = value
			return object
		}
	}
	return append(object, notionOrderedMember{key: key, value: value})
}

func (object notionOrderedObject) MarshalJSON() ([]byte, error) {
	written := []byte{'{'}
	for index, member := range object {
		if index > 0 {
			written = append(written, ',')
		}
		key, err := jsonv2.Marshal(member.key, jsontext.AllowInvalidUTF8(true))
		if err != nil {
			return nil, err
		}
		value, err := jsonv2.Marshal(member.value, jsontext.AllowInvalidUTF8(true))
		if err != nil {
			return nil, err
		}
		written = append(append(append(written, key...), ':'), value...)
	}
	return append(written, '}'), nil
}
