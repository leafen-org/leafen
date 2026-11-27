package app

import (
	"encoding/json"
	"maps"
	"slices"
)

// A table's or board's group, or a board's sub-group, given as a formula's
// (seen 2026-11-12 in Notion's views API on formulas, rollups and a text):
// any property takes it, read back as given with what its group_by says —
// a result family, date, text, number or box, its sort, a date's unit and
// week start, words exactly or by their first letter, a number's ranges (0,
// 0 and 1 until given, whole, at least one wide). A number's and a date's
// sort go by value, up unless down, a box's by hand; the group's own sort is
// passed over, and its hide_empty_groups reads back only where given.
// Notion's screen groups a formula by what group_by says where it names the
// family the formula gives; any other group so given stays the property's
// own — dates by how near today, numbers and words by each value, a
// rollup's too — and hides its empty groups. A rollup grouped on the screen
// has no official form and is left out, as Notion's is; a formula's reads
// back by the family of what it gives.

// notionViewFormulaFamilies are the result families a formula group names.
var notionViewFormulaFamilies = []string{"date", "text", "number", "checkbox"}

// notionViewTableFormulaGroup is a group given as a formula's (field the
// group's path, group_by or sub_group_by) as it reads back, and the group of
// its family the screen goes by for the property: none for a rollup's.
func notionViewTableFormulaGroup(field string, property notionViewProperty, group map[string]any) (nested, flat map[string]any, err error) {
	given, _ := group["group_by"].(map[string]any)
	family, _ := given["type"].(string)
	if !slices.Contains(notionViewFormulaFamilies, family) {
		return nil, nil, notionViewFilterInvalid("a formula group needs its date, text, number or checkbox result family")
	}
	fields := map[string]json.RawMessage{}
	if raw, err := json.Marshal(given); err == nil && json.Unmarshal(raw, &fields) == nil {
		if refusal := notionViewGroupShapeRefusal(field+".group_by", fields); refusal != "" {
			return nil, nil, notionViewFilterInvalid("%s", refusal)
		}
	}
	sorting, _ := given["sort"].(map[string]any)
	order, _ := sorting["type"].(string)
	nested = map[string]any{"type": family, "sort": map[string]any{"type": notionViewAxisSort(family, order)}}
	flat = map[string]any{}
	switch family {
	case "text", "date":
		nested["group_by"] = given["group_by"]
		if start, ok := given["start_day_of_week"].(float64); ok && family == "date" {
			nested["start_day_of_week"] = start
		}
		flat = map[string]any{"type": family, "group_by": given["group_by"]}
	case "number":
		flat["type"] = family
		for index, key := range notionViewNumberRangeFields {
			nested[key] = []float64{0, 0, 1}[index]
			if value, ok := given[key].(float64); ok {
				nested[key] = value
			}
			flat[key] = nested[key]
		}
	}
	if !notionViewFormulaGroupApplies(property, family) {
		flat = map[string]any{}
	}
	return nested, flat, nil
}

// notionViewFormulaGroupApplies is whether the screen groups the property by
// a formula group of the family: a formula's that gives it, or whose result
// isn't known.
func notionViewFormulaGroupApplies(property notionViewProperty, family any) bool {
	return property.OfficialType == "formula" && (property.FormulaResult == "" || property.FormulaResult == family)
}

// notionViewRollupUnits is how a group of the property splits its values on
// the screen: as given, but a rollup's groups are its own whatever it was
// given as — only a number's ranges are kept, which its groups by each value
// pass over, so that they read back as given.
func notionViewRollupUnits(property notionViewProperty, group map[string]any) map[string]any {
	if property.OfficialType != "rollup" || group["type"] == "number" {
		return group
	}
	return map[string]any{}
}

// officialViewGivenFormulaGroup is the formula group a table's or board's
// group by the property was given (applyOfficialViewGrouping), nil for none.
func officialViewGivenFormulaGroup(settings map[string]any, propertyID string) map[string]any {
	given, _ := settings["officialGroupType"].(map[string]any)
	nested, _ := given["group_by"].(map[string]any)
	if given["property"] != propertyID || given["type"] != "formula" || nested == nil {
		return nil
	}
	return maps.Clone(nested)
}

// officialViewTableFormulaGroup is a table's or board's group by the
// property (settings: the view's own, or a board's sub-groups' when sub) as
// a formula's, with the screen's settings as they stand; nil where it has
// none: neither given as one nor a formula's set on the screen, or a
// formula's whose result family isn't known.
func (scope notionViewFilterScope) officialViewTableFormulaGroup(property notionViewProperty, config, settings map[string]any, sub bool) map[string]any {
	nested := officialViewGivenFormulaGroup(settings, property.ID)
	if nested == nil {
		if property.OfficialType != "formula" || officialViewGivenGroupType(settings, property.ID) != "" ||
			!slices.Contains(notionViewFormulaFamilies, property.FormulaResult) {
			return nil
		}
		nested = map[string]any{"type": property.FormulaResult}
		if property.FormulaResult == "date" {
			nested["group_by"] = "relative"
		}
	}
	family, _ := nested["type"].(string)
	if notionViewFormulaGroupApplies(property, family) {
		order, _ := settings["groupSort"].(string)
		if !slices.Contains(notionViewGroupSorts, order) {
			order = map[bool]string{true: "manual", false: "ascending"}[settings["groupOrder"] != nil]
		}
		nested["sort"] = map[string]any{"type": notionViewAxisSort(family, order)}
		switch family {
		case "text":
			nested["group_by"] = map[bool]string{true: "alphabet_prefix", false: "exact"}[settings["textGroupBy"] == "letter"]
		case "number":
			withNumberRanges(nested, settings)
		case "date":
			// A board's sub-group's unit is its own (applyOfficialViewSubGroupDates).
			units := config
			if sub {
				units = settings
			}
			nested["group_by"] = "relative"
			if by, ok := units["dateGroupBy"].(string); ok && slices.Contains(notionViewDateGroupings, by) {
				nested["group_by"] = by
			}
		}
	}
	return map[string]any{"type": "formula", "property_id": scope.viewID(property.ID), "property_name": property.Name, "group_by": nested}
}

// notionViewGroupsByDates is whether a view's groups by the property go by
// dates, with a unit to pick: a date's, a formula's that gives dates, or a
// rollup's earliest, latest or range of dates or the dates it shows as they
// are, as the screen's group menu offers them (computedDateValue.ts).
func notionViewGroupsByDates(property notionViewProperty) bool {
	return notionViewDateProperty(property) || property.FormulaResult == "date" || property.RollupResult == "date" ||
		property.RollupResult == notionViewRollupEach && notionViewDateGroupType(property.RolledType)
}
