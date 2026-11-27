package app

import (
	"encoding/json"
	"strconv"
	"strings"
)

// A formula's or rollup's value as Notion shows it, kept aside among a row's
// staged values: Leafen computes these again from their definition, but a
// migration plan may "preserve" one as text (notion_migration_property_choices.go),
// keeping what Notion showed when the run read it — a formula naming a
// property that is gone, say, which Leafen can't compute again. The run makes
// such a property a text property from the start and gives each row its text.

// notionImportRowShownKey carries, by property source ID, the formulas' and
// rollups' values a row shows, as text.
const notionImportRowShownKey = "leafen:row-shown"

// stageNotionImportRowAside stages what a row carries beside its values: its
// presentation (notion_import_presentation.go) and what its computed values show.
func stageNotionImportRowAside(values map[string]notionDatabaseRowValuePlan, databaseID string, row notionProviderObject) {
	stageNotionImportRowPresentation(values, row)
	shown := map[string]any{}
	for _, property := range row.Properties {
		if !notionMigrationShownAsText[notionGraphValueType(property.Type)] {
			continue
		}
		if text, _, _ := notionImportFittedCell(notionImportShownText(property), nil, false); text != "" {
			shown[notionPropertySourceID(databaseID, property.ID)] = text
		}
	}
	if len(shown) > 0 {
		values[notionImportRowShownKey] = notionDatabaseRowValuePlan{Type: "shown", Value: shown}
	}
}

// notionImportShownText is a property value as text, as Leafen's own
// conversion to text writes it (database_property_conversion_rules.go): a
// checkbox Yes or No, a date "start → end", options and a list's items joined.
func notionImportShownText(property notionProviderProperty) string {
	raw := notionImportPropertySettings(property)
	switch property.Type {
	case "formula", "rollup":
		computed := struct {
			Type string `json:"type"`
		}{}
		fields := map[string]json.RawMessage{}
		if json.Unmarshal(raw, &computed) != nil || json.Unmarshal(raw, &fields) != nil {
			return ""
		}
		if computed.Type == "array" {
			items := []notionProviderProperty{}
			_ = json.Unmarshal(fields["array"], &items)
			texts := make([]string, 0, len(items))
			for _, item := range items {
				if text := notionImportShownText(item); text != "" {
					texts = append(texts, text)
				}
			}
			return strings.Join(texts, ", ")
		}
		return notionImportShownText(notionProviderProperty{Type: computed.Type, Settings: fields[computed.Type]})
	case "title", "rich_text", "text":
		parts, _ := notionProviderRichTextParts(raw)
		text := strings.Builder{}
		for _, part := range parts {
			text.WriteString(part.PlainText)
		}
		return text.String()
	case "string", "url", "email", "phone_number", "created_time", "last_edited_time":
		text := ""
		_ = json.Unmarshal(raw, &text)
		return text
	case "number":
		var number *float64
		if json.Unmarshal(raw, &number) != nil || number == nil {
			return ""
		}
		return strconv.FormatFloat(*number, 'f', -1, 64)
	case "boolean", "checkbox":
		checked := false
		if json.Unmarshal(raw, &checked) != nil {
			return ""
		}
		if checked {
			return "Yes"
		}
		return "No"
	case "date":
		date := struct {
			Start string  `json:"start"`
			End   *string `json:"end"`
		}{}
		if json.Unmarshal(raw, &date) != nil || date.Start == "" {
			return ""
		}
		if date.End != nil && *date.End != "" {
			return date.Start + " → " + *date.End
		}
		return date.Start
	case "select", "status", "created_by", "last_edited_by":
		named := struct {
			Name string `json:"name"`
		}{}
		_ = json.Unmarshal(raw, &named)
		return named.Name
	case "multi_select", "people", "files":
		named := []struct {
			Name string `json:"name"`
		}{}
		_ = json.Unmarshal(raw, &named)
		names := make([]string, 0, len(named))
		for _, item := range named {
			if item.Name != "" {
				names = append(names, item.Name)
			}
		}
		return strings.Join(names, ",")
	case "unique_id":
		unique := struct {
			Prefix *string  `json:"prefix"`
			Number *float64 `json:"number"`
		}{}
		if json.Unmarshal(raw, &unique) != nil || unique.Number == nil {
			return ""
		}
		number := strconv.FormatFloat(*unique.Number, 'f', -1, 64)
		if unique.Prefix != nil && *unique.Prefix != "" {
			return *unique.Prefix + "-" + number
		}
		return number
	}
	return ""
}

// keepShownAsText makes, in a window of staged sources, each formula or
// rollup the plan preserves a text property, and each row's value of it the
// text Notion showed (keepRowShownAsText); the run's later change of it to
// text (notion_migration_plan_sources.go) then finds it so already.
func (run *notionMigrationRunPlan) keepShownAsText(sources []storedNotionSource) []storedNotionSource {
	if run == nil {
		return sources
	}
	for index, source := range sources {
		switch source.Kind {
		case "property":
			plan := notionDatabasePropertyPlan{}
			if !run.preservesShown(source.SourceID) || json.Unmarshal([]byte(source.Text), &plan) != nil || !notionMigrationShownAsText[plan.Type] {
				continue
			}
			config := map[string]any{}
			for _, key := range []string{"notionType", "notionPropertyId", "description"} {
				if value, ok := plan.Config[key]; ok {
					config[key] = value
				}
			}
			if encoded, err := json.Marshal(notionDatabasePropertyPlan{Type: graphValueTypeRichText, Config: config}); err == nil {
				sources[index].Text = string(encoded)
			}
		case "row":
			if !strings.Contains(source.Text, notionImportRowShownKey) {
				continue
			}
			values := map[string]json.RawMessage{}
			if json.Unmarshal([]byte(source.Text), &values) != nil || !run.keepRowShownAsText(values) {
				continue
			}
			if encoded, err := json.Marshal(values); err == nil {
				sources[index].Text = string(encoded)
			}
		}
	}
	return sources
}

// preservesShown says whether the plan keeps a property, by its source, as
// the text Notion shows.
func (run *notionMigrationRunPlan) preservesShown(propertySource string) bool {
	databaseID, propertyID, found := strings.Cut(propertySource, ":property:")
	return found && run.columns[notionMigrationSourceKey(databaseID)][notionMigrationPropertyKey(propertyID)].Action == "preserve"
}

// keepRowShownAsText puts among a row's staged values, as rich text, the
// text Notion showed of each formula or rollup the plan preserves, the rest
// as staged, and says whether it put any. It is the one way a row's staged
// text becomes the row a run takes: the run's own (keepShownAsText), and a
// run again's on both sides of what it compares — the row as the ledger keeps
// it, staged, and as Notion gives it now (notionMigrationRowChanged); taken
// so on one side only, a preserved value was always new (2026-11-17).
func (run *notionMigrationRunPlan) keepRowShownAsText(values map[string]json.RawMessage) bool {
	raw, found := values[notionImportRowShownKey]
	if run == nil || !found {
		return false
	}
	shown := struct {
		Value map[string]any `json:"value"`
	}{}
	if json.Unmarshal(raw, &shown) != nil {
		return false
	}
	kept := false
	for propertySource, value := range shown.Value {
		text, ok := value.(string)
		if !ok || text == "" || !run.preservesShown(propertySource) {
			continue
		}
		if encoded, err := json.Marshal(notionDatabaseRowValuePlan{Type: graphValueTypeRichText, Value: text}); err == nil {
			values[propertySource], kept = encoded, true
		}
	}
	return kept
}
