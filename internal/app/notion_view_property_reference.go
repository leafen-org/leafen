package app

import (
	"fmt"
	"strings"
)

// How a view's configuration names its properties, as Notion's views API
// reads them (seen 2026-11-12): a group's, a sub-group's, a chart axis's and
// the date a calendar or timeline goes by name a property by its ID alone —
// by its name, it is a property the API can't find — while a view's
// properties and a timeline's table properties take an ID or a name. Its
// sorts, filters and quick filters take either too (named).

// byViewID is the property a view's setting names by its ID: Leafen's, or its
// public ID, encoded or not.
func (scope notionViewFilterScope) byViewID(reference string) (notionViewProperty, bool) {
	if property, found := scope.byID[reference]; found {
		return property, true
	}
	if internalID, found := scope.ids.internalID(reference); found {
		if property, found := scope.byID[internalID]; found {
			return property, true
		}
	}
	return notionViewProperty{}, false
}

// viewPropertyReference is the property one of a view's other settings names
// by its ID (a cover's, arrows', map's, chart value's or sub-items'), of the
// kind familyOK takes when given. Notion's API keeps such a reference as
// written, unchecked, and draws one that names nothing as none (seen
// 2026-11-12: a cover by "파일" kept as "파일"); Leafen refuses it instead of
// keeping a setting that names nothing.
func (scope notionViewFilterScope) viewPropertyReference(raw any, familyOK func(notionViewProperty) bool) (string, error) {
	reference, _ := raw.(string)
	property, found := scope.byViewID(reference)
	if !found || familyOK != nil && !familyOK(property) {
		return "", notionViewFilterInvalid("the view configuration property %q is invalid", reference)
	}
	return property.ID, nil
}

// viewGroupReference is the property a group, a sub-group or a chart's axis
// or stack goes by, refused as Notion's API refuses it, in Leafen's words.
func (scope notionViewFilterScope) viewGroupReference(raw any) (string, error) {
	reference, _ := raw.(string)
	property, found := scope.byViewID(reference)
	if !found {
		return "", notionViewFilterInvalid("Group-by property \"%s\" not found.", reference)
	}
	return property.ID, nil
}

// viewDateReference is the date a calendar or a timeline goes by, or a
// timeline ends by (field end_date_property_id), refused as Notion's API
// refuses it, in Leafen's words.
func (scope notionViewFilterScope) viewDateReference(field, viewType string, raw any) (string, error) {
	reference, _ := raw.(string)
	property, found := scope.byViewID(reference)
	end := field == "end_date_property_id"
	switch {
	case !found && end:
		return "", notionViewFilterInvalid("End date property \"%s\" not found.", reference)
	case !found:
		return "", notionViewFilterInvalid("Property \"%s\" not found.", reference)
	case !notionViewDateProperty(property) && end:
		return "", notionViewFilterInvalid("End date property \"%s\" isn't a date.", reference)
	case !notionViewDateProperty(property):
		return "", notionViewFilterInvalid(
			"Property \"%s\" isn't a date: a %s view needs a date, created_time, last_edited_time or date formula property.",
			reference, strings.ToUpper(viewType[:1])+viewType[1:])
	}
	return property.ID, nil
}

// viewListedProperty is the property item index of a view's properties or a
// timeline's table_properties (field) names by its property_id, an ID or a
// name; a property_name alone names none there, as Notion's API reads it.
func (scope notionViewFilterScope) viewListedProperty(item map[string]any, field string, index int) (string, error) {
	raw, given := item["property_id"]
	reference, text := raw.(string)
	switch {
	case !given:
		return "", notionViewFilterInvalid("%s", notionFaultSaid("body",
			notionFaultMissing(fmt.Sprintf("body.configuration.%s[%d].property_id", field, index))))
	case !text:
		return "", notionViewFilterInvalid("%s", notionBodyFault(
			fmt.Sprintf("body.configuration.%s[%d].property_id", field, index), "a string", fmt.Sprint(raw)))
	}
	property, found := scope.named(reference)
	if !found {
		return "", notionViewFilterInvalid(
			"The database has no property \"%s\": give one of its properties' ID or name.", reference)
	}
	return property.ID, nil
}
