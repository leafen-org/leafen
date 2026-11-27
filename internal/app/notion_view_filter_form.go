package app

import (
	"encoding/json"
	"strings"
)

// A filter's form is checked as Notion's API checks it, from the keys it
// names alone: of several conditions, words or kinds in one object the first
// in Notion's order whose form is right is taken (seen 2026-11-11:
// {"equals": 1, "less_than": 3} filters by equals, {"is_empty": true,
// "equals": "A"} by equals too), and only when none is the first one named
// is refused.

// notionViewFilterShapeRefusal refuses a filter at path (body.filter,
// body.quick_filters.Due) whose form isn't one shape takes, in Notion's
// words.
func notionViewFilterShapeRefusal(shape *notionShape, raw json.RawMessage, path string) error {
	value, err := parseNotionValue(raw)
	if err != nil {
		return notionViewFilterInvalid("the view filter is invalid")
	}
	if faults := shape.check(value, strings.Split(path, ".")); len(faults) > 0 {
		return notionViewFilterInvalid("%s", notionFaultsMessage("body", faults))
	}
	return nil
}

// formOnly is the scope checking a filter's form alone: any option, person
// or page it names will do.
func (scope notionViewFilterScope) formOnly() notionViewFilterScope {
	scope.checksFormOnly = true
	scope.refusesUnknownOptions = false
	scope.userToNative = func(id string) (string, bool) { return id, true }
	scope.rowToNative = scope.userToNative
	return scope
}

// notionViewFirstFitting is the first of order's keys present that fits; of
// none that fits, the first present; "" when none is present.
func notionViewFirstFitting(order []string, present map[string]json.RawMessage, fits func(string) bool) string {
	first := ""
	for _, key := range order {
		if _, named := present[key]; !named {
			continue
		}
		if fits(key) {
			return key
		}
		if first == "" {
			first = key
		}
	}
	return first
}

// conditionForm refuses a condition whose form isn't right.
func (scope notionViewFilterScope) conditionForm(
	property notionViewProperty, family, condition string, value json.RawMessage, path string,
) error {
	_, err := scope.formOnly().nativeViewCondition(property, family, condition, value, path)
	return err
}

// conditionFormFits says whether a family's conditions at path hold one
// whose form is right.
func (scope notionViewFilterScope) conditionFormFits(family string, raw json.RawMessage, path string) bool {
	conditions := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &conditions)
	for _, condition := range notionViewConditionLists[family] {
		value, named := conditions[condition.name]
		if named && scope.conditionForm(notionViewProperty{}, family, condition.name, value, path) == nil {
			return true
		}
	}
	return false
}

// computedFormFits says whether a formula's or rollup's kind holds a
// condition whose form is right.
func (scope notionViewFilterScope) computedFormFits(officialType, kind string, raw json.RawMessage, path string) bool {
	family := notionViewComputedFamilies[officialType][kind]
	return family != "" && notionViewJSONObject(raw) && scope.conditionFormFits(family, raw, path+"."+kind)
}

// pickCondition is the condition a family's object at path filters by: the
// first in Notion's order whose form is right, else the first named, which
// nativeViewCondition refuses; an object naming none is refused with what
// Notion lists there.
func (scope notionViewFilterScope) pickCondition(family string, raw json.RawMessage, path string) (string, json.RawMessage, error) {
	conditions := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &conditions)
	order := make([]string, len(notionViewConditionLists[family]))
	for index, condition := range notionViewConditionLists[family] {
		order[index] = condition.name
	}
	picked := notionViewFirstFitting(order, conditions, func(condition string) bool {
		return scope.conditionForm(notionViewProperty{}, family, condition, conditions[condition], path) == nil
	})
	if picked == "" {
		for name, value := range conditions {
			// A condition the family doesn't name is refused as one.
			return name, value, nil
		}
		return "", nil, notionViewConditionRefusal(path, family, "", nil)
	}
	return picked, conditions[picked], nil
}

// notionViewFilterForm is a property filter's form, read from its key: the
// family of its condition, the condition and its value, where it is
// (body.filter.rollup.any.select), and for a formula or rollup the kind it
// names, for a rollup's values which of them it asks about and of what kind.
type notionViewFilterForm struct {
	family, computedKind, rollupMatch, rollupKind, condition, path string
	value                                                          json.RawMessage
}

// viewFilterForm checks the form of the filter key names at filterPath
// (body.filter, body.filter.and[0], body.quick_filters.Due), before any
// property is looked up, as Notion's API checks it.
func (scope notionViewFilterScope) viewFilterForm(key string, raw json.RawMessage, filterPath string) (notionViewFilterForm, error) {
	form := notionViewFilterForm{family: notionViewConditionFamily(key), path: filterPath + "." + key}
	if key == "rollup" || key == "formula" {
		kind, matchCondition, match, matchPath, isMatch, err := scope.notionViewRollupMatchCondition(raw, form.path)
		if key != "rollup" {
			isMatch, err = false, nil
		}
		switch {
		case err != nil:
			return form, err
		case isMatch:
			form.family, raw, form.rollupMatch, form.computedKind, form.rollupKind = notionViewConditionFamily(kind), matchCondition, match, notionViewRollupEach, kind
			form.path += "." + matchPath
		default:
			if form.family, form.computedKind, raw, err = scope.notionViewComputedCondition(key, raw, form.path); err != nil {
				return form, err
			}
			form.path += "." + form.computedKind
		}
	} else if form.family == "date" && notionViewDateConditionCount(raw) > 1 {
		return form, notionViewDateOneOperatorRefusal(filterPath, key, raw)
	}
	if form.family == "" || !notionViewJSONObject(raw) {
		return form, notionViewFilterInvalid("the view filter %q must hold its conditions", key)
	}
	var err error
	if form.condition, form.value, err = scope.pickCondition(form.family, raw, form.path); err != nil {
		return form, err
	}
	return form, scope.conditionForm(notionViewProperty{}, form.family, form.condition, form.value, form.path)
}

// addViewFilterForm adds the rule a filter's form holds on property, once
// it fits the property.
func (scope notionViewFilterScope) addViewFilterForm(group *databaseQueryFilterGroup, property notionViewProperty, form notionViewFilterForm) error {
	if err := scope.rollupFits(property, form.computedKind, form.family, form.condition, form.value, form.path); err != nil {
		return err
	}
	filter, err := scope.nativeViewCondition(property, form.family, form.condition, form.value, form.path)
	if err != nil {
		return err
	}
	filter.RollupMatch = form.rollupMatch
	if form.rollupKind != "" {
		filter.RollupKind = notionViewRollupKind(property, form.rollupKind, form.condition)
	}
	group.Filters = append(group.Filters, filter)
	group.Order = append(group.Order, databaseQueryFilterTermFilter)
	return nil
}
