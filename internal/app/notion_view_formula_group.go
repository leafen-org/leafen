package app

import "slices"

// Formula chart groups retain the official nested result-family setting.
// Current computed values remain the rendering authority, as in Notion even
// when an expression changes after its group setting was selected. A date
// result groups by the same units as a date property's axis, relative, day,
// week, month or year, month until another is picked (Notion's, seen
// 2026-11-11).
func notionViewFormulaGroupKey(key string) string {
	switch key {
	case "chartGroupBy":
		return "chartFormulaGroup"
	case "chartStackBy":
		return "chartStackFormulaGroup"
	}
	return ""
}

// notionViewAxisGroupType is the group type a property's chart axis,
// stacked groups, or table's or board's groups take: a rollup's a formula's,
// by the family of what it gives, as Notion's API takes it — its "rollup"
// refused (seen 2026-11-12). A rollup's groups set on the screen have no
// official form and are left out of the configuration read back, as
// Notion's are.
func notionViewAxisGroupType(officialType string) string {
	if officialType == "rollup" {
		return "formula"
	}
	groupType, _ := notionViewGroupType(officialType)
	return groupType
}

func normalizeNotionViewFormulaGroup(value any) (map[string]any, error) {
	group, ok := value.(map[string]any)
	typeName, _ := group["type"].(string)
	if !ok || !slices.Contains([]string{"number", "text", "date", "checkbox"}, typeName) {
		return nil, notionViewFilterInvalid("a formula chart group needs its number, text, date or checkbox result family")
	}
	result := map[string]any{"type": typeName}
	order := "ascending"
	if typeName == "checkbox" {
		order = "manual"
	}
	for key, value := range group {
		switch key {
		case "type":
		case "sort":
			sorting, ok := value.(map[string]any)
			nextOrder, _ := sorting["type"].(string)
			if !ok || len(sorting) != 1 || !slices.Contains(notionViewChartSeriesSorts, nextOrder) {
				return nil, notionViewFilterInvalid("a formula chart group's sort is manual, ascending or descending")
			}
			order = nextOrder
		case "range_start", "range_end", "range_size":
			// A number's ranges read back as given (notionViewChartRangeRefusal checks them), another family's are passed over.
			if number, ok := notionViewChartNumber(value).(float64); typeName == "number" && ok {
				result[key] = number
			}
		case "group_by":
			unit, _ := value.(string)
			if typeName == "text" && value == "exact" || typeName == "date" && slices.Contains(notionViewDateGroupings, unit) {
				result[key] = value
			} else {
				return nil, notionViewFilterInvalid("formula chart groups use exact text or a date's relative, day, week, month or year")
			}
		default:
			return nil, notionViewFilterInvalid("the formula chart group field %q is unsupported", key)
		}
	}
	// A number's and a date's go by value and a box's by hand, as Notion's API reads them back (seen 2026-11-12).
	result["sort"] = map[string]any{"type": notionViewAxisSort(typeName, order)}
	// Ranges not given are 0, 0 and 1, as Notion's API reads them back (seen 2026-11-12); a size of -1, which a
	// chart's axis kept for each value before ranges were taken, reads as none given.
	if typeName == "number" {
		kept := result["range_size"] == -1.0
		for index, key := range notionViewNumberRangeFields {
			if _, given := result[key]; !given || kept {
				result[key] = []float64{0, 0, 1}[index]
			}
		}
	}
	if typeName == "text" {
		result["group_by"] = "exact"
	}
	if _, given := result["group_by"]; typeName == "date" && !given {
		result["group_by"] = "month"
	}
	return result, nil
}

func officialNotionViewFormulaGroup(property notionViewProperty, viewID string, config map[string]any, key string) any {
	if notionViewFormulaGroupKey(key) == "" || config[key] != property.ID {
		return nil
	}
	group, err := normalizeNotionViewFormulaGroup(config[notionViewFormulaGroupKey(key)])
	// The unit the axis or stacked groups show by, as a date property's.
	if by, _ := config[notionViewChartUnitKeys[key].date].(string); err == nil && group["type"] == "date" && slices.Contains(notionViewDateGroupings, by) {
		group["group_by"] = by
	}
	// A formula's axis sorts as the chart's settings last sorted it; a rollup's as it was given.
	if sort := officialChartAxisSort(config); err == nil && key == "chartGroupBy" && property.OfficialType == "formula" && sort != "" {
		typeName, _ := group["type"].(string)
		group["sort"] = map[string]any{"type": notionViewAxisSort(typeName, sort)}
	}
	if err == nil {
		return map[string]any{"type": "formula", "property_id": viewID, "property_name": property.Name, "group_by": group}
	}
	return nil
}
