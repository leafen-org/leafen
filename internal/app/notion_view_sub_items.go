package app

import "slices"

// A view's sub-items and a chart's y-axis in the official configuration map
// onto the native view settings that carry the same meaning.

// notionViewChartAggregators are the chart y-axis aggregations; the native
// chart keeps each under the same name.
var notionViewChartAggregators = map[string]struct{}{
	"count": {}, "count_values": {}, "sum": {}, "average": {}, "median": {}, "min": {}, "max": {}, "range": {},
	"unique": {}, "empty": {}, "not_empty": {}, "percent_empty": {}, "percent_not_empty": {}, "checked": {},
	"unchecked": {}, "percent_checked": {}, "percent_unchecked": {}, "earliest_date": {}, "latest_date": {},
	"date_range": {},
}

// A view shows sub-items nested under their parents, hidden, flattened or
// not at all, and filters parents, sub-items or both.
var (
	notionViewSubtaskDisplayModes = []string{"show", "hidden", "flattened", "disabled"}
	notionViewSubtaskFilterScopes = []string{"parents", "parents_and_subitems", "subitems"}
)

// notionViewSubtaskReferences are a table's sub-item fields that name a
// property, each with the native key of the property it names and the key
// that keeps a name Notion's API takes as given (see applyOfficialViewSubtasks).
var notionViewSubtaskReferences = map[string][2]string{
	"property_id": {"propertyId", "propertyReference"}, "toggle_column_id": {"toggleColumnId", "toggleColumnReference"},
}

// officialViewSubtasks projects how a table shows sub-items: what it was
// given and kept, as Notion's API reads it back (seen 2026-11-12): a mode, a
// filter scope or a property alone when only that was given, nothing for
// none.
func (scope notionViewFilterScope) officialViewSubtasks(config map[string]any) map[string]any {
	subtasks, _ := config["subtasks"].(map[string]any)
	official := map[string]any{}
	if mode, _ := subtasks["displayMode"].(string); slices.Contains(notionViewSubtaskDisplayModes, mode) {
		official["display_mode"] = mode
	}
	if scopeName, _ := subtasks["filterScope"].(string); slices.Contains(notionViewSubtaskFilterScopes, scopeName) {
		official["filter_scope"] = scopeName
	}
	for field, keys := range notionViewSubtaskReferences {
		if id, _ := subtasks[keys[0]].(string); id != "" && scope.lists(id) {
			official[field] = scope.viewID(id)
		} else if reference, _ := subtasks[keys[1]].(string); reference != "" {
			official[field] = reference
		}
	}
	if len(official) == 0 {
		return nil
	}
	return official
}

// applyOfficialViewSubtasks sets how a table shows sub-items, field by field
// over what it had, as Notion's API does (seen 2026-11-12): a mode, a filter
// scope, the relation and the toggle's column each optional, the rest kept;
// "" takes a property away and null takes them all. Notion keeps a property
// field as the words given, a property of another type or none at all too:
// one of the source's properties by its ID is kept as that property
// (subtaskPropertyID), any other words as they are. Their form is checked before
// (notionViewConfigurationRefusal).
func (scope notionViewFilterScope) applyOfficialViewSubtasks(value any, config map[string]any) error {
	if value == nil {
		delete(config, "subtasks")
		return nil
	}
	input, isObject := value.(map[string]any)
	if !isObject {
		return notionViewFieldCheck(false, "subtasks")
	}
	subtasks := map[string]any{}
	if current, ok := config["subtasks"].(map[string]any); ok {
		for key, kept := range current {
			subtasks[key] = kept
		}
	}
	for field, choices := range map[string][]string{"display_mode": notionViewSubtaskDisplayModes, "filter_scope": notionViewSubtaskFilterScopes} {
		raw, given := input[field]
		if !given {
			continue
		}
		choice, _ := raw.(string)
		if !slices.Contains(choices, choice) {
			return notionViewFieldCheck(false, "subtasks")
		}
		subtasks[map[string]string{"display_mode": "displayMode", "filter_scope": "filterScope"}[field]] = choice
	}
	for field, keys := range notionViewSubtaskReferences {
		raw, given := input[field]
		if !given {
			continue
		}
		reference, isText := raw.(string)
		if !isText {
			return notionViewFieldCheck(false, "subtasks")
		}
		subtasks[keys[0]] = ""
		delete(subtasks, keys[1])
		if id := scope.subtaskPropertyID(reference); id != "" {
			subtasks[keys[0]] = id
		} else if reference != "" {
			subtasks[keys[1]] = reference
		}
	}
	if _, had := config["subtasks"]; had || notionViewSubtasksGiven(input) {
		config["subtasks"] = subtasks
	}
	return nil
}

// subtaskPropertyID is the source's property a sub-item field names by its
// ID — Leafen's, as an import gives it, or its public one — and "" for other
// words, a name too, which Notion keeps as given.
func (scope notionViewFilterScope) subtaskPropertyID(reference string) string {
	if _, native := scope.byID[reference]; native {
		return reference
	}
	if internalID, public := scope.ids.internalID(reference); public && scope.lists(internalID) {
		return internalID
	}
	return ""
}

// notionViewSubtasksGiven is whether a sub-item setting names any of its fields.
func notionViewSubtasksGiven(input map[string]any) bool {
	for _, field := range []string{"display_mode", "filter_scope", "property_id", "toggle_column_id"} {
		if _, given := input[field]; given {
			return true
		}
	}
	return false
}

// applyOfficialViewChartAxis sets what a chart counts or aggregates: a count
// of rows, or an aggregation of one property.
func (scope notionViewFilterScope) applyOfficialViewChartAxis(value any, config map[string]any) error {
	if value == nil {
		delete(config, "chartAggregate")
		delete(config, "chartAggregateBy")
		return nil
	}
	axis, _ := value.(map[string]any)
	aggregator, _ := axis["aggregator"].(string)
	if _, known := notionViewChartAggregators[aggregator]; !known {
		return notionViewFieldCheck(false, "y_axis")
	}
	config["chartAggregate"] = aggregator
	if axis["property_id"] == nil {
		if aggregator != "count" {
			return notionViewFilterInvalid("the chart y_axis aggregator %q needs a property_id", aggregator)
		}
		delete(config, "chartAggregateBy")
		return nil
	}
	id, err := scope.viewPropertyReference(axis["property_id"], nil)
	if err != nil {
		return err
	}
	config["chartAggregateBy"] = id
	return nil
}
