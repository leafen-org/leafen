package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

// notion-update-data-source adds one option to a select, multi-select or
// status, or deletes one, without listing the others — Leafen's own
// statements beside Notion's DDL (user decision 2026-11-26, "추천대로"):
// ALTER COLUMN "Name" ADD OPTION 'New':color (a status's 'New':color:group)
// and ALTER COLUMN "Name" DROP OPTION 'Old', which clears it from its pages,
// as Notion clears an option it removes. ALTER COLUMN … SET still lists the
// options to keep, and is refused when it leaves out options pages hold
// (database_option_removal.go).

// mcpDataSourceOptionStatement is an ADD OPTION's or DROP OPTION's option.
type mcpDataSourceOptionStatement struct {
	Drop  bool   `json:"drop,omitempty"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
	Group string `json:"group,omitempty"`
}

// parseMCPDataSourceOptionStatement reads ALTER COLUMN "column" ADD OPTION
// or DROP OPTION from what follows the column's name (body); ok is whether
// body is one, reason why it doesn't read.
func parseMCPDataSourceOptionStatement(column, body string) (mcpDataSourceUpdateStatement, string, bool) {
	drop := hasMCPDatabaseStatementPrefix(body, "DROP OPTION")
	if !drop && !hasMCPDatabaseStatementPrefix(body, "ADD OPTION") {
		return mcpDataSourceUpdateStatement{}, "", false
	}
	form := fmt.Sprintf(`written ALTER COLUMN %q ADD OPTION 'Name':color (a status's 'Name':color:group, group to_do, in_progress or complete), `+
		`or ALTER COLUMN %q DROP OPTION 'Name'`, column, column)
	prefix := "ADD OPTION"
	if drop {
		prefix = "DROP OPTION"
	}
	rest := strings.TrimSpace(body[len(prefix):])
	name, next, ok := readMCPDatabaseSchemaQuoted(rest, 0)
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return mcpDataSourceUpdateStatement{}, form, true
	}
	option := mcpDataSourceOptionStatement{Drop: drop, Name: name, Color: "default"}
	rest = strings.TrimSpace(rest[next:])
	if !drop && strings.HasPrefix(rest, ":") {
		color, group, _ := strings.Cut(strings.TrimSpace(rest[1:]), ":")
		option.Color, option.Group, rest = strings.TrimSpace(color), strings.TrimSpace(group), ""
		if !knownDatabaseOptionColor(option.Color) {
			return mcpDataSourceUpdateStatement{}, form + "; colors: default, gray, brown, orange, yellow, green, blue, purple, pink, red", true
		}
		if _, known := notionStatusSchemaGroups[option.Group]; !known {
			return mcpDataSourceUpdateStatement{}, form, true
		}
	}
	if rest != "" {
		return mcpDataSourceUpdateStatement{}, form, true
	}
	return mcpDataSourceUpdateStatement{Kind: mcpDataSourceStatementAlter, Name: column, Option: &option}, "", true
}

// mcpDataSourceOptionApplied is a column with an ADD OPTION's option added
// to its options, or a DROP OPTION's marked deleted; seedID gives the new
// option's ID.
func mcpDataSourceOptionApplied(property storedMCPDataSourceProperty, seedID string, change mcpDataSourceOptionStatement) (storedMCPDataSourceProperty, error) {
	if _, options := databasePropertyConversionOptionTypes[property.Type]; !options {
		return property, mcpInvalid("column %q is a %s column, which has no options: ADD OPTION and DROP OPTION are for a select, multi-select or status",
			property.Name, property.Type)
	}
	config := map[string]any{}
	if json.Unmarshal([]byte(property.Config), &config) != nil {
		return property, graphStructureAuthorityError()
	}
	options, _ := config["options"].([]any)
	named := -1
	for index, raw := range options {
		option, _ := raw.(map[string]any)
		name, _ := option["name"].(string)
		if name = strings.TrimSpace(name); name == change.Name || named < 0 && strings.EqualFold(name, change.Name) {
			named = index
		}
	}
	switch {
	case change.Drop && named < 0:
		return property, mcpInvalid("column %q has no option %s to drop", property.Name, strconvQuoteJS(change.Name))
	case change.Drop:
		options[named].(map[string]any)[databaseOptionDeleteKey] = true
	case named >= 0:
		return property, mcpInvalid("column %q has option %s already", property.Name, strconvQuoteJS(change.Name))
	default:
		id := mcpDatabaseSchemaOptionID(seedID, "option", 0)
		option := map[string]any{"id": id, "name": change.Name, "color": change.Color}
		if property.Type == graphValueTypeStatus {
			groups, _ := config["statusGroups"].([]any)
			at := notionStatusSchemaGroups[change.Group]
			if at >= len(groups) {
				return property, mcpInvalid("column %q has no group %s", property.Name, strconvQuoteJS(change.Group))
			}
			group, _ := groups[at].(map[string]any)
			if group == nil {
				return property, graphStructureAuthorityError()
			}
			ids, _ := group["optionIds"].([]any)
			group["optionIds"], option["group"] = append(ids, id), group["name"]
		}
		options = append(options, option)
	}
	config["options"] = options
	encoded, err := json.Marshal(config)
	if err != nil {
		return property, err
	}
	property.Config = string(encoded)
	return property, nil
}
