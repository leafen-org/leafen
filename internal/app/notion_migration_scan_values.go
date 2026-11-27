package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The scan's readers of Notion's objects (notion_migration_scan.go): each
// property as a plan reads it — with a formula's expression and what a
// rollup reads — the properties in a database's order, and a value's text.

// notionMigrationPropertyOf is a property as the scan lists it.
func notionMigrationPropertyOf(key string, property notionProviderProperty) notionMigrationProperty {
	entry := notionMigrationProperty{
		ID: property.ID, Name: notionImportPropertyName(key, property), Type: property.Type,
		LeafenType: notionGraphValueType(property.Type),
	}
	settings := map[string]json.RawMessage{}
	_ = json.Unmarshal(property.Settings, &settings)
	switch property.Type {
	case "select", "multi_select", "status":
		options := []struct {
			Name string `json:"name"`
		}{}
		_ = json.Unmarshal(settings["options"], &options)
		for _, option := range options {
			entry.Options = append(entry.Options, option.Name)
		}
	case "relation":
		var databaseID string
		_ = json.Unmarshal(settings["database_id"], &databaseID)
		entry.RelationDatabaseID = databaseID
	case "formula":
		entry.Expression = notionMigrationFormulaExpression(property)
	case "rollup":
		// What it reads, for a merge to tell whether it computes as its
		// target's of its name (notion_migration_merge_computed_plan.go).
		rollup := notionMigrationRollup{}
		_ = json.Unmarshal(settings["relation_property_id"], &rollup.RelationID)
		_ = json.Unmarshal(settings["rollup_property_id"], &rollup.PropertyID)
		_ = json.Unmarshal(settings["function"], &rollup.Function)
		entry.Rollup = &rollup
	}
	return entry
}

func notionMigrationFormulaExpression(property notionProviderProperty) string {
	settings := struct {
		Expression string `json:"expression"`
	}{}
	_ = json.Unmarshal(property.Settings, &settings)
	return settings.Expression
}

// sortNotionMigrationProperties puts the title first, then the rest by name,
// as a database lists them.
func sortNotionMigrationProperties(properties []notionMigrationProperty) {
	less := func(a, b notionMigrationProperty) bool {
		if (a.Type == "title") != (b.Type == "title") {
			return a.Type == "title"
		}
		return a.Name < b.Name
	}
	for i := 1; i < len(properties); i++ {
		for j := i; j > 0 && less(properties[j], properties[j-1]); j-- {
			properties[j], properties[j-1] = properties[j-1], properties[j]
		}
	}
}

func notionMigrationValueText(property notionProviderProperty) string {
	raw := property.Settings
	text := ""
	switch property.Type {
	case "title", "rich_text":
		parts, _ := notionProviderRichTextParts(raw)
		pieces := make([]string, 0, len(parts))
		for _, part := range parts {
			pieces = append(pieces, part.PlainText)
		}
		text = strings.Join(pieces, "")
	case "select", "status":
		value := struct {
			Name string `json:"name"`
		}{}
		_ = json.Unmarshal(raw, &value)
		text = value.Name
	case "multi_select":
		values := []struct {
			Name string `json:"name"`
		}{}
		_ = json.Unmarshal(raw, &values)
		names := make([]string, 0, len(values))
		for _, value := range values {
			names = append(names, value.Name)
		}
		text = strings.Join(names, ", ")
	case "date":
		value := struct {
			Start string  `json:"start"`
			End   *string `json:"end"`
		}{}
		_ = json.Unmarshal(raw, &value)
		text = value.Start
		if value.End != nil && *value.End != "" {
			text += " → " + *value.End
		}
	case "people", "relation", "files":
		values := []json.RawMessage{}
		_ = json.Unmarshal(raw, &values)
		if len(values) > 0 {
			text = fmt.Sprintf("%d", len(values))
		}
	default:
		var value any
		if json.Unmarshal(raw, &value) == nil && value != nil {
			switch typed := value.(type) {
			case string:
				text = typed
			case map[string]any:
				for _, field := range []string{"string", "number", "boolean", "date", "name", "prefix"} {
					if inner, ok := typed[field]; ok && inner != nil {
						text = fmt.Sprint(inner)
						break
					}
				}
				if prefix, ok := typed["prefix"].(string); ok {
					text = fmt.Sprintf("%s-%v", prefix, typed["number"])
				}
			default:
				text = fmt.Sprint(typed)
			}
		}
	}
	if utf8.RuneCountInString(text) > notionMigrationValueRunes {
		text = string([]rune(text)[:notionMigrationValueRunes]) + "…"
	}
	return text
}
