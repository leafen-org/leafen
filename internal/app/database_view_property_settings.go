package app

// A view's settings name properties in lists and keys as well as in the
// single references a copy already renames (pageDuplicationDatabaseReferenceKind):
// the columns a view shows, hides, orders, wraps or ticks, a row page's
// property order, a column's width and a table's totals keyed by property, a
// timeline's dates and its table's columns, a board's sub-groups, a chart's
// axis, a map's place and a card's cover. A copy — a duplicated page or a
// restored archive — gives its properties new IDs, so these name the copy's
// properties too. A setting may still name a property since deleted, or a
// word ("page_cover"), so a name with no copy stays as it is.

// databaseViewPropertyFields are the settings that hold a property's ID, or
// a list of them.
var databaseViewPropertyFields = map[string]bool{
	"visibleProperties": true, "hiddenProperties": true, "propertyOrder": true, "rowPagePropertyOrder": true,
	"wrappedColumns": true, "statusCheckboxColumns": true, "statusCheckboxPropertyId": true,
	"timelineBy": true, "timelineEndBy": true, "dependencyProperty": true, "coverProperty": true,
	"subGroupBy": true, "chartGroupBy": true, "chartAggregateBy": true, "mapBy": true,
}

// databaseViewPropertyKeyedFields are the settings keyed by property IDs.
var databaseViewPropertyKeyedFields = map[string]bool{"propertyWidths": true, "tableCalculations": true}

// databaseViewTimelineColumns lists a timeline's table columns, each naming
// its property by "id".
const databaseViewTimelineColumns = "timelineTableProperties"

// databaseViewSettingProperties are the property IDs a setting's map names:
// its keys, or a timeline column's "id".
func databaseViewSettingProperties(field string, value map[string]any) []string {
	names := []string{}
	if databaseViewPropertyKeyedFields[field] {
		for key := range value {
			names = append(names, key)
		}
	}
	if field == databaseViewTimelineColumns {
		if id, ok := value["id"].(string); ok && id != "" {
			names = append(names, id)
		}
	}
	return names
}

// renameDatabaseViewSettingProperties names a setting map's properties by
// their copies; copyOf gives a property's copy, and false keeps the name.
func renameDatabaseViewSettingProperties(field string, value map[string]any, copyOf func(string) (string, bool)) {
	if databaseViewPropertyKeyedFields[field] {
		renamed := map[string]any{}
		for key, child := range value {
			if target, ok := copyOf(key); ok && target != key {
				renamed[target] = child
				delete(value, key)
			}
		}
		for key, child := range renamed {
			value[key] = child
		}
	}
	if field == databaseViewTimelineColumns {
		if id, ok := value["id"].(string); ok && id != "" {
			if target, found := copyOf(id); found {
				value["id"] = target
			}
		}
	}
}
