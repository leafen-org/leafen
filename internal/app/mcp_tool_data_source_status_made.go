package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// mcpStatusMadeFromConfig is the settings ALTER COLUMN … SET STATUS gives a
// column of another type, as Notion's MCP gives them (seen 2026-11-14): a new
// status, as ADD COLUMN makes one — the options listed, each in the group it
// names, else in To-do, or with none listed Notion's three in the words of
// the person who asks — whose first is its default. The options it had are
// not kept (a select's, a multi-select's, or a status's before it was made
// something else), but one named as one of them, in any case, takes that
// one's ID, so the rows that set it keep it, as the API's status does
// (notionDataSourceStatusRequestConfig); a row whose option went reads the
// default; its own settings (a description) stay. seedID names the new
// options and groups, current is the column's settings and column what the
// statement names.
func mcpStatusMadeFromConfig(
	app core.App, principalID, propertyID, seedID string, current map[string]any, column mcpDatabaseSchemaProperty,
) (map[string]any, error) {
	column.Type = graphValueTypeStatus
	if len(column.Options) == 0 {
		column.translate = newStatusWords(app, principalID)
	}
	made := map[string]any{}
	if err := json.Unmarshal(mcpDatabaseSchemaConfig(seedID, column), &made); err != nil {
		return nil, err
	}
	options, err := notionStatusConfigMaps(made["options"])
	if err != nil || len(options) == 0 {
		return nil, failedBecause(errMCPToolFailed, err)
	}
	groups, err := notionStatusConfigMaps(made["statusGroups"])
	if err != nil {
		return nil, err
	}
	former := append(databasePropertyConversionOptions(current), databasePropertyFormerStatusOptions(app, propertyID)...)
	lent, renamed := map[string]bool{}, map[string]string{}
	for _, option := range options {
		name, _ := option["name"].(string)
		earlier := notionStatusOptionNamed(former, name, lent)
		if earlier == nil {
			continue
		}
		id, _ := earlier["id"].(string)
		own, _ := option["id"].(string)
		lent[id], renamed[own] = true, id
		option["id"] = id
	}
	for _, group := range groups {
		ids, _ := group["optionIds"].([]any)
		for index, value := range ids {
			if own, ok := value.(string); ok && renamed[own] != "" {
				ids[index] = renamed[own]
			}
		}
	}
	config := map[string]any{}
	keepDatabasePropertyOwnSettings(current, config)
	if column.Description != "" {
		config["description"] = column.Description
	}
	config["options"], config["statusGroups"], config["defaultOptionId"] = options, groups, options[0]["id"]
	return config, nil
}
