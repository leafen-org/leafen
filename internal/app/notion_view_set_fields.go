package app

import (
	"bytes"
	"encoding/json"
	"slices"
)

// A view's configuration gives what the view has set, as Notion's does (seen
// 2026-11-10): a table made through the API or with its database reads
// {"type":"table"}; turning its lines off on the screen adds
// show_vertical_lines, its "wrap all" switch wrap_cells, hiding a column
// properties, wrapping one column properties and wrap_cells; a switch turned
// back stays given at its value; a board, gallery, calendar, map or chart
// made on the screen reads only what making it set, as its grouping, cover,
// date property, place property or axes (a chart its type, sort and empty
// groups too: notion_view_chart_screen_fields.go), and a board, calendar or timeline
// with nothing set reads null — until it is shown, when the screen keeps its
// grouping or date (databaseViewShownConfig). A setting is set when the view
// keeps it (the screen keeps a switch it turned back), or when the API gave
// it (a default the API gave is kept natively as no setting at all).

// notionViewAPIFieldsKey keeps, in a view made through the API, which of the
// API's configuration fields it was given.
const notionViewAPIFieldsKey = "apiFields"

// notionViewAPIGivenFieldsKey keeps, in a view made on the screen, which of
// the API's configuration fields a later request gave: Notion gives a field
// once given, its default too (seen 2026-11-10: a database's first view
// given show_vertical_lines true reads it).
const notionViewAPIGivenFieldsKey = "apiGivenFields"

// withNotionViewAPIFields records the fields a request's configuration gave
// that a view of its type takes (notionViewTakesField): the first, for a view
// the request made; added to those it had, for one it changes, kept apart for
// a view made on the screen.
func withNotionViewAPIFields(viewType string, config json.RawMessage, requested json.RawMessage, made bool) (json.RawMessage, error) {
	settings := map[string]any{}
	if err := json.Unmarshal(config, &settings); err != nil {
		return nil, err
	}
	key := notionViewAPIFieldsKey
	if _, viaAPI := settings[notionViewAPIFieldsKey]; !viaAPI && !made {
		key = notionViewAPIGivenFieldsKey
	}
	known := map[string]bool{}
	if fields, ok := settings[key].([]any); ok {
		for _, field := range fields {
			if name, ok := field.(string); ok {
				known[name] = true
			}
		}
	}
	given := map[string]json.RawMessage{}
	if len(requested) != 0 && string(requested) != "null" {
		_ = json.Unmarshal(requested, &given)
		// A chart's configuration given again is all it has given since
		// (withoutNotionChartFormat).
		if viewType == graphViewTypeChart {
			clear(known)
		}
	}
	// A field given null is unset, as Notion's API reads it back (seen
	// 2026-11-12: a chart's color_theme, a board's cover, a calendar's
	// view_range, a form's is_form_closed given null read back without it).
	for name, raw := range given {
		switch {
		case name == "type" || !notionViewTakesField(viewType, name):
		case string(bytes.TrimSpace(raw)) == "null":
			delete(known, name)
		default:
			known[name] = true
		}
	}
	notionViewGivenGroupHiding(viewType, given, known)
	names := make([]string, 0, len(known))
	for name := range known {
		names = append(names, name)
	}
	if len(names) == 0 && key == notionViewAPIGivenFieldsKey {
		return config, nil
	}
	slices.Sort(names)
	settings[key] = names
	return json.Marshal(settings)
}

// notionViewPropertyListKeys are the native settings that make a view's
// property list: its order, what it hides, widths and a status shown as a
// checkbox. A list of shown properties (visibleProperties) makes it once it
// names one: left empty it is a new board's, gallery's or list's first
// setting, its title alone, which Notion's new views don't give (seen
// 2026-11-10). A column's own wrap (wrappedColumns) makes it too, unless
// "wrap all" is on: that switch keeps every column's wrap itself.
var notionViewPropertyListKeys = []string{"propertyOrder", "hiddenProperties", "propertyWidths", "statusCheckboxColumns"}

// notionViewDefaultedFields are the fields the configuration fills in with a
// default, each with the native settings that hold it; every other field is
// there only when its setting is.
func notionViewDefaultedFields(viewType string) map[string][]string {
	switch viewType {
	case graphViewTypeTable:
		return map[string][]string{"wrap_cells": {"wrap"}, "frozen_column_index": {"frozenColumns"}, "show_vertical_lines": {"showVerticalLines"}}
	case graphViewTypeBoard, graphViewTypeGallery:
		return map[string][]string{"cover_aspect": {"fitImage"}, "card_layout": {"cardLayout"}}
	case graphViewTypeCalendar:
		return map[string][]string{"show_weekends": {"showWeekends"}}
	case graphViewTypeTimeline:
		return map[string][]string{"show_table": {"timelineShowTable"}, "table_properties": {"timelineTableProperties"}, "color_by": {"timelineColorBy"}}
	case graphViewTypeMap:
		return map[string][]string{"height": {"mapHeight"}}
	case graphViewTypeChart:
		fields := map[string][]string{
			"color_theme": {"chartColorTheme"}, "height": {"chartHeight"}, "show_data_labels": {"chartShowDataLabels"},
			"color_by_value": {"chartColorByValue"}, "y_axis_min": {"chartYMin"}, "y_axis_max": {"chartYMax"},
			"reference_lines": {"chartReferenceLines"}, "caption": {"chartShowCaption"},
		}
		for field, choice := range notionViewChartChoices {
			fields[field] = []string{choice.key}
		}
		for field, key := range notionViewChartFlags {
			fields[field] = []string{key}
		}
		return fields
	}
	return nil
}

// notionViewGivenConfiguration is a view's configuration as the API gives it:
// what the view has set, a number chart's value, a dashboard's rows and what
// the screen writes into a chart's format (withNotionScreenChartFields). A
// form made through the API gives the fields it was given.
func notionViewGivenConfiguration(viewType string, settings map[string]any, configuration map[string]any) any {
	fields, made := settings[notionViewAPIFieldsKey].([]any)
	later, _ := settings[notionViewAPIGivenFieldsKey].([]any)
	given := map[string]bool{"type": true}
	for _, field := range append(slices.Clone(fields), later...) {
		if name, ok := field.(string); ok {
			given[name] = true
		}
	}
	withNotionScreenChartFields(viewType, settings, configuration, given)
	kept := func(keys ...string) bool {
		for _, key := range keys {
			if _, ok := settings[key]; ok {
				return true
			}
		}
		return false
	}
	paired := map[string]string{
		"date_property_name": "date_property_id", "end_date_property_name": "end_date_property_id",
		"map_by_property_name": "map_by",
	}
	defaulted := notionViewDefaultedFields(viewType)
	shown, _ := settings["visibleProperties"].([]any)
	for key := range configuration {
		native, isDefaulted := defaulted[key]
		switch {
		case key == "hide_empty_groups" && viewType == graphViewTypeChart && !given[key] && (made || given["x_axis.hide_empty_groups"]):
			// Said in the x_axis, or not said in a chart the API made.
			delete(configuration, key)
		case given[key] || given[paired[key]]:
		case key == "rows" && viewType == graphViewTypeDashboard:
		case key == "value" && configuration["chart_type"] == "number":
		case viewType == graphViewTypeForm:
			if made {
				delete(configuration, key)
			}
		case key == "caption" && viewType == graphViewTypeChart && (configuration[key] == nil || configuration[key] == ""):
			// The screen's caption switch writes nothing until words are
			// typed under it (seen 2026-11-18): a caption given shows as given.
			delete(configuration, key)
		case key == "properties":
			if !kept(notionViewPropertyListKeys...) && len(shown) == 0 && (settings["wrap"] == true || !kept("wrappedColumns")) {
				delete(configuration, key)
			}
		case isDefaulted:
			if !kept(native...) {
				delete(configuration, key)
			}
		}
	}
	notionViewGroupHidingGiven(viewType, settings, configuration, given)
	notionViewScreenCompanions(viewType, settings, configuration, given)
	if len(configuration) == 1 && (viewType == graphViewTypeBoard || viewType == graphViewTypeCalendar || viewType == graphViewTypeTimeline) {
		return nil
	}
	return configuration
}

// notionViewScreenCompanions adds what Notion's screen writes beside a change
// of its own, as its API reads it then (seen 2026-11-10): a table's columns
// changed on the screen (hidden, sized or wrapped) give every column's width,
// a sub-item scope and no frozen column with them, and a column's own wrap
// gives every column's wrap and the table's; a group the screen picked says
// whether it hides empty groups, false once its switch is off (seen
// 2026-11-12 on a table, 2026-11-27 on a list).
func notionViewScreenCompanions(viewType string, settings, configuration map[string]any, given map[string]bool) {
	if notionViewTakesField(viewType, "group_by") {
		// It reads true while it hides them (withEmptyGroupsHidden), a sub-group by its own setting.
		for _, field := range []string{"group_by", "sub_group_by"} {
			if group, ok := configuration[field].(map[string]any); ok && !given[field] {
				if _, set := group["hide_empty_groups"]; !set {
					group["hide_empty_groups"] = false
				}
			}
		}
	}
	switch viewType {
	case graphViewTypeTable:
		properties, listed := configuration["properties"].([]any)
		if !listed || given["properties"] {
			return
		}
		wrapped := len(notionViewStringSet(settings["wrappedColumns"])) > 0
		for _, raw := range properties {
			entry, _ := raw.(map[string]any)
			if _, sized := entry["width"]; !sized && entry != nil {
				// The screen's own widths (tableViewModel.ts), Notion's too.
				entry["width"] = 200
				if entry["property_id"] == "title" {
					entry["width"] = 280
				}
			}
			if _, set := entry["wrap"]; wrapped && !set && entry != nil {
				entry["wrap"] = false
			}
		}
		companions := map[string]any{"subtasks": map[string]any{"filter_scope": "parents_and_subitems"}, "frozen_column_index": -1}
		if wrapped {
			companions["wrap_cells"] = false
		}
		for key, value := range companions {
			if _, set := configuration[key]; !set {
				configuration[key] = value
			}
		}
	}
}
