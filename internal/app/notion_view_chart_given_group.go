package app

import (
	"encoding/json"
	"maps"
	"slices"
)

// A chart's X axis or stacked groups given as another type than their
// property's (seen 2026-11-12 in Notion's views API: a number rollup's axis
// as a select, a number formula's as a date or words, a text's as a number,
// a status or a formula's, a date's as a select, a text's stacked groups as
// a select) are taken and read back as given — a number's with its ranges,
// 0, 0 and 1 until given, a formula's with its group_by read as a formula
// axis's — while the chart draws the property as its own with no unit of
// its own: a date's, or a formula's dates, by how near today; a rollup's
// and a formula's values each. The record keeps the property it was given
// for; another property's group leaves it.

// notionViewChartGivenKey is where a chart's axis (key chartGroupBy) or its
// stacked groups (chartStackBy) keep a group given as another type.
func notionViewChartGivenKey(key string) string {
	return map[string]string{"chartGroupBy": "chartGivenAxis", "chartStackBy": "chartGivenStack"}[key]
}

// notionViewChartGivenGroup is a chart group given as another type as it
// reads back, without its property, whose hiding the chart keeps.
func notionViewChartGivenGroup(group map[string]any) (map[string]any, error) {
	given := maps.Clone(group)
	for _, field := range []string{"property_id", "property_name", "hide_empty_groups"} {
		delete(given, field)
	}
	typeName, _ := given["type"].(string)
	if !slices.Contains(notionViewGroupTypes, typeName) {
		return nil, notionViewFilterInvalid("the view group must use one of the group types, not %q", typeName)
	}
	switch typeName {
	case "number":
		for index, field := range notionViewNumberRangeFields {
			if _, set := given[field]; !set {
				given[field] = []float64{0, 0, 1}[index]
			}
		}
	case "formula":
		// Its group_by sorts within itself, as the form says (notionViewGroupForm).
		if nested, _ := given["group_by"].(map[string]any); nested["sort"] == nil {
			return nil, notionViewFilterInvalid("a formula group's group_by needs its sort")
		}
		formula, err := normalizeNotionViewFormulaGroup(given["group_by"])
		if err != nil {
			return nil, err
		}
		given["group_by"] = formula
		delete(given, "sort")
	}
	return given, nil
}

// officialViewChartGivenGroup is the group a chart's axis or stacked groups
// by the property were given as another type, nil for none.
func (scope notionViewFilterScope) officialViewChartGivenGroup(property notionViewProperty, config map[string]any, key string) map[string]any {
	record, _ := config[notionViewChartGivenKey(key)].(map[string]any)
	group, _ := record["group"].(map[string]any)
	if record["property"] != property.ID || group == nil {
		return nil
	}
	given := maps.Clone(group)
	given["property_id"], given["property_name"] = scope.viewID(property.ID), property.Name
	return given
}

// applyOfficialChartGivenGroup keeps a chart group given as another type than
// its property's (applyOfficialViewGroup), or forgets one given before; it
// is whether the group was so given, which the chart then draws as its
// property's own.
func (scope notionViewFilterScope) applyOfficialChartGivenGroup(key, id string, group, config map[string]any) (bool, error) {
	property := scope.byID[id]
	if err := notionViewChartRangeRefusal(key, group); err != nil {
		return false, err
	}
	applyOfficialChartNumberRanges(key, id, group, config)
	if group["type"] == notionViewAxisGroupType(property.OfficialType) {
		delete(config, notionViewChartGivenKey(key))
		return false, nil
	}
	given, err := notionViewChartGivenGroup(group)
	if err != nil {
		return true, err
	}
	config[notionViewChartGivenKey(key)] = map[string]any{"property": id, "group": given}
	delete(config, notionViewFormulaGroupKey(key))
	units := notionViewChartUnitKeys[key]
	delete(config, units.date)
	if notionViewDateProperty(property) || property.FormulaResult == "date" {
		config[units.date] = "relative"
	}
	return true, nil
}

// A chart's number axis or stacked groups, and a formula's or rollup's of
// numbers, take ranges as a table's number groups do (seen 2026-11-12 in
// Notion's views API): whole numbers, the size at least 1, read back as
// given, 0, 0 and 1 until given, while the chart draws each value apart
// whatever its ranges say. Ranges given to a group of words or dates are
// passed over.

// notionViewChartRangeRefusal is Notion's refusal of a chart group's ranges
// (key chartGroupBy or chartStackBy), at x_axis or stack_by, or their
// group_by of a formula's numbers; nil for ranges it takes.
func notionViewChartRangeRefusal(key string, group map[string]any) error {
	at := "body.configuration." + map[string]string{"chartGroupBy": "x_axis", "chartStackBy": "stack_by"}[key] + "."
	fields := group
	if nested, _ := group["group_by"].(map[string]any); group["type"] == "formula" {
		at, fields = at+"group_by.", nested
	}
	if fields["type"] != "number" {
		return nil
	}
	raw := map[string]json.RawMessage{}
	for _, field := range notionViewNumberRangeFields {
		if value, given := fields[field]; given {
			raw[field], _ = json.Marshal(value)
		}
	}
	if refusal := notionViewNumberRangeRefusal(at, raw); refusal != "" {
		return notionViewFilterInvalid("%s", refusal)
	}
	return nil
}

// notionViewChartRangesKey is where a chart's number axis (key
// chartGroupBy) or stacked groups (chartStackBy) keep the ranges given.
func notionViewChartRangesKey(key string) string {
	return map[string]string{"chartGroupBy": "chartAxisRanges", "chartStackBy": "chartStackRanges"}[key]
}

// applyOfficialChartNumberRanges keeps the ranges a chart's number group
// by the property was given, forgetting any given before.
func applyOfficialChartNumberRanges(key, id string, group, config map[string]any) {
	delete(config, notionViewChartRangesKey(key))
	ranges := map[string]any{"property": id}
	for _, field := range notionViewNumberRangeFields {
		if value, ok := group[field].(float64); ok && group["type"] == "number" {
			ranges[field] = value
		}
	}
	if len(ranges) > 1 {
		config[notionViewChartRangesKey(key)] = ranges
	}
}

// withChartNumberRanges is a chart's number group by the property read back
// with its ranges: as given, 0, 0 and 1 until given.
func withChartNumberRanges(group map[string]any, config map[string]any, key, propertyID string) {
	record, _ := config[notionViewChartRangesKey(key)].(map[string]any)
	for index, field := range notionViewNumberRangeFields {
		group[field] = []float64{0, 0, 1}[index]
		if value, ok := record[field].(float64); ok && record["property"] == propertyID {
			group[field] = value
		}
	}
}
