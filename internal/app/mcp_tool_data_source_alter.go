package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// mcpAlteredDataSourceProperty is a property as ALTER COLUMN … SET leaves it,
// and as ADD COLUMN leaves one already so named: its type and the settings
// the statement names (mcpAlterColumnSettings, mcpStatusMadeFromConfig). A
// type change converts existing cells through the conversion owner, as the
// strict API's does. seedID names what the statement makes.
func mcpAlteredDataSourceProperty(
	app core.App, principalID, seedID string, property storedMCPDataSourceProperty, statement mcpDataSourceUpdateStatement,
) (storedMCPDataSourceProperty, error) {
	if statement.Option != nil {
		return mcpDataSourceOptionApplied(property, seedID, *statement.Option)
	}
	if property.Type == graphValueTypeTitle && statement.Type != graphValueTypeTitle {
		// Refused as Notion's MCP refuses it (seen 2026-11-14, ADD and ALTER alike).
		return property, graphInputError(errorCodeGraphInvalid, "The title property keeps its type.")
	}
	if statement.Type == graphValueTypeRelation || statement.Type == graphValueTypeRollup ||
		property.Type != statement.Type && !databasePropertyTypeChangeAllowed(property.Type, statement.Type) {
		return property, graphInputError(errorCodeGraphInvalid, "The property can't be converted to this type.")
	}
	config := map[string]any{}
	if json.Unmarshal([]byte(property.Config), &config) != nil {
		return property, graphStructureAuthorityError()
	}
	converted := config
	madeStatus := statement.Type == graphValueTypeStatus && property.Type != graphValueTypeStatus
	if madeStatus {
		column := mcpDatabaseSchemaProperty{Name: property.Name}
		if statement.Column != nil {
			column = *statement.Column
		}
		var err error
		if converted, err = mcpStatusMadeFromConfig(app, principalID, property.ID, seedID, config, column); err != nil {
			return property, err
		}
	} else if property.Type != statement.Type {
		converted = notionDataSourceConvertedConfig(property.Type, statement.Type, config)
	}
	if statement.Type == graphValueTypeNumber {
		// SET NUMBER without FORMAT leaves it without one, as
		// Notion's (seen 2026-11-14: a percent column's went).
		delete(converted, "numberFormat")
	}
	if statement.Column != nil && !madeStatus {
		mcpAlterColumnSettings(property.ID, seedID, *statement.Column, converted)
	}
	encoded, err := json.Marshal(converted)
	if err != nil {
		return property, err
	}
	property.Type, property.Config = statement.Type, string(encoded)
	return property, nil
}

// mcpAlterColumnSettings writes what ALTER COLUMN SET names into a column's
// settings. A select's or status's options are all it keeps: one already there
// keeps its ID and, for a status, its group unless the statement names another.
// A new one's ID comes from the statement's seedID, never one an option of the
// column has or had: counted from the options there, a second change's new
// option took the ID of one the first had made, or of one it had removed,
// whose pages would then hold the new one.
func mcpAlterColumnSettings(propertyID, seedID string, column mcpDatabaseSchemaProperty, config map[string]any) {
	if column.NumberFormat != "" {
		config["numberFormat"] = column.NumberFormat
	}
	if column.Description != "" {
		config["description"] = column.Description
	}
	if column.Type == graphValueTypeFormula {
		config["formula"] = column.Formula
	}
	if column.IDPrefix != "" {
		config["idPrefix"] = column.IDPrefix
	}
	if len(column.Options) == 0 {
		return
	}
	current := map[string]map[string]any{}
	existing, _ := config["options"].([]any)
	for _, raw := range existing {
		if option, ok := raw.(map[string]any); ok {
			name, _ := option["name"].(string)
			current[name] = option
		}
	}
	groupOf := map[string]int{}
	groups, _ := config["statusGroups"].([]any)
	if column.Type == graphValueTypeStatus {
		for index, raw := range groups {
			group, _ := raw.(map[string]any)
			ids, _ := group["optionIds"].([]any)
			for _, id := range ids {
				if text, ok := id.(string); ok {
					groupOf[text] = index
				}
			}
		}
		if len(groups) != len(notionStatusGroupNames) {
			_, fresh := notionStatusNewConfig(nil, nil, func(kind string, index int) string {
				return mcpDatabaseSchemaOptionID(propertyID, kind, index)
			})
			groups = make([]any, len(fresh))
			for index, group := range fresh {
				groups[index] = group
			}
		}
		for _, raw := range groups {
			raw.(map[string]any)["optionIds"] = []any{}
		}
	}
	options := make([]any, 0, len(column.Options))
	for index, named := range column.Options {
		option := current[named.Name]
		if option == nil {
			option = map[string]any{"id": mcpDatabaseSchemaOptionID(seedID, "option", index), "name": named.Name}
		}
		option["color"] = named.Color
		if column.Type == graphValueTypeStatus {
			id, _ := option["id"].(string)
			at, known := groupOf[id]
			if named.Group != "" || !known {
				at = notionStatusSchemaGroups[named.Group]
			}
			group := groups[at].(map[string]any)
			group["optionIds"] = append(group["optionIds"].([]any), id)
			option["group"] = group["name"]
		}
		options = append(options, option)
	}
	config["options"] = options
	if column.Type == graphValueTypeStatus {
		config["statusGroups"] = groups
	}
}
