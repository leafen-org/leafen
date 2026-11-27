package app

import (
	"encoding/json"
	"slices"
	"strings"
)

// notionViewGroupTypes are the types a view group names, in the order
// Notion's refusal lists them (seen 2026-11-12); a rollup groups as a formula.
var notionViewGroupTypes = []string{"select", "multi_select", "status", "person", "created_by", "last_edited_by", "relation",
	"date", "created_time", "last_edited_time", "text", "title", "url", "email", "phone_number", "number", "checkbox", "formula"}

// notionViewConfigurationTypes are the types a view configuration names, in
// the order Notion's refusal lists them.
var notionViewConfigurationTypes = []string{"table", "board", "calendar", "timeline", "gallery", "list", "map", "form", "chart"}

// notionViewConfigurationRefusal refuses a view configuration out of form
// (notion_view_configuration_form.go) as Notion's API does, in Leafen's
// words, before
// anything the configuration names is read; "" for one in form, or for none
// at all. Notion's API checks it alike on a new view and a changed one (seen
// 2026-11-12): first its type, one of nine — a dashboard takes none — then
// its type's fields in order (notionViewConfigurationFields): a board needs
// its group_by, a calendar and a timeline their date_property_id, a chart
// its chart_type; a map nothing. A group — a view's group_by, a board's
// sub_group_by, a chart's x_axis and stack_by — is in its own form
// (notionViewGroupForm); a group_by but a board's, its sub_group_by and a
// chart's axes may be null, which takes the grouping away, but a board's
// group_by may not.
func notionViewConfigurationRefusal(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	configuration := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &configuration) != nil {
		return ""
	}
	typeValue, typed := configuration["type"]
	var viewType string
	if !typed {
		return notionViewFormUndefined("type")
	}
	if json.Unmarshal(typeValue, &viewType) != nil || !slices.Contains(notionViewConfigurationTypes, viewType) {
		return notionViewFormShouldBe("type", "`\"table\"`, `\"board\"`, `\"calendar\"`, `\"timeline\"`, `\"gallery\"`, `\"list\"`, `\"map\"`, `\"form\"`, or `\"chart\"`", typeValue)
	}
	fields := notionViewConfigurationFields(viewType)
	return notionViewFormFields("", configuration, fields)
}

// notionViewSubtasksRefusal is Notion's refusal of a table's sub-item
// setting in the wrong form (seen 2026-11-12): an object, null or none; its
// mode and filter scope one of theirs or none, null too refused; the
// relation and the toggle's column words or none. Any words are taken
// (applyOfficialViewSubtasks), and a field it doesn't know is passed over.
// Another type's sub-items are left out unread.
func notionViewSubtasksRefusal(value json.RawMessage, shouldBe func(path, kind string, value json.RawMessage) string) string {
	if len(value) == 0 || string(value) == "null" {
		return ""
	}
	fields := map[string]json.RawMessage{}
	if value[0] != '{' || json.Unmarshal(value, &fields) != nil {
		return shouldBe("subtasks", "an object, `null`, or `undefined`", value)
	}
	choices := func(names []string) string {
		quoted := make([]string, 0, len(names)+1)
		for _, name := range names {
			quoted = append(quoted, "`\""+name+"\"`")
		}
		return strings.Join(append(quoted, "or `undefined`"), ", ")
	}
	for _, field := range []struct {
		name    string
		choices []string
	}{{"display_mode", notionViewSubtaskDisplayModes}, {"filter_scope", notionViewSubtaskFilterScopes}} {
		raw, given := fields[field.name]
		var choice string
		if given && (json.Unmarshal(raw, &choice) != nil || string(raw) == "null" || !slices.Contains(field.choices, choice)) {
			return shouldBe("subtasks."+field.name, choices(field.choices), raw)
		}
	}
	for _, name := range []string{"property_id", "toggle_column_id"} {
		raw, given := fields[name]
		var words string
		if given && (json.Unmarshal(raw, &words) != nil || string(raw) == "null") {
			return shouldBe("subtasks."+name, "a string or `undefined`", raw)
		}
	}
	return ""
}
