package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Row-level triggers (docs/app-platform.md 11): an app's data-change event
// may name one of its data sources, the properties whose change it waits
// for, and conditions on the row's values; a change outside them doesn't
// queue its worker.

// appEventRowMatches says the change is to a row of the event's data
// source that meets its conditions.
func appEventRowMatches(app core.App, installID, tenancy string, event appManifestEvent, input eventWebhookEventInput) bool {
	if event.DataSource == "" {
		return true
	}
	var sourceID string
	if app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:row} AND [[workspace_id]] = {:workspace}`).
		Bind(dbx.Params{"row": input.EntityID, "workspace": input.WorkspaceID}).Row(&sourceID) != nil || sourceID == "" {
		return false
	}
	spec, known := appDataSourceSpec(app, installID, tenancy, event.DataSource, sourceID)
	if !known {
		return false
	}
	// The event names columns and options as the app wrote them.
	written := appDataWritten(spec)
	properties := map[string]struct{ id, kind, config string }{}
	rows := []struct {
		ID     string `db:"id"`
		Name   string `db:"name"`
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	if app.DB().NewQuery(`SELECT [[id]], [[name]], [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} AND `+databasePropertyNotTrashedSQL("[[id]]")).
		Bind(dbx.Params{"source": sourceID}).All(&rows) != nil {
		return false
	}
	for _, row := range rows {
		properties[appDataWrittenName(written, row.Name)] = struct{ id, kind, config string }{row.ID, row.Type, appDataWrittenConfig(row.Config, written)}
	}
	if len(event.Properties) > 0 && input.Type == eventWebhookPropertiesUpdated {
		changed, _ := input.Data["updated_properties"].([]any)
		touched := false
		for _, name := range event.Properties {
			if property, ok := properties[name]; ok {
				for _, field := range changed {
					touched = touched || field == "values."+property.id
				}
			}
		}
		if !touched {
			return false
		}
	}
	for _, condition := range event.Where {
		property, ok := properties[condition.Property]
		if !ok {
			return false
		}
		value := appEventRowValueText(app, input.EntityID, property.id, property.kind, property.config)
		switch {
		case condition.Equals != nil && value != strings.TrimSpace(*condition.Equals),
			condition.NotEquals != nil && value == strings.TrimSpace(*condition.NotEquals),
			condition.Empty != nil && (value == "") != *condition.Empty:
			return false
		}
	}
	return true
}

// appEventRowValueText is a row's value as it reads: options by name.
func appEventRowValueText(app core.App, rowID, propertyID, kind, config string) string {
	var raw string
	if app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:row} AND [[property_id]] = {:property}`).
		Bind(dbx.Params{"row": rowID, "property": propertyID}).Row(&raw) != nil {
		if kind == graphValueTypeCheckbox {
			return "false"
		}
		return ""
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return ""
	}
	options := struct {
		Options []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"options"`
	}{}
	_ = json.Unmarshal([]byte(config), &options)
	named := func(id any) any {
		for _, option := range options.Options {
			if option.ID == id {
				return option.Name
			}
		}
		return id
	}
	switch typed := value.(type) {
	case string:
		value = named(typed)
	case []any:
		for index := range typed {
			typed[index] = named(typed[index])
		}
	case map[string]any:
		if len(typed) == 0 {
			return ""
		}
	}
	return strings.TrimSpace(databaseQueryValueText(value))
}

// appWorkspaceTenancy names the organization of one of the app's workspaces.
func appWorkspaceTenancy(app core.App, installID, workspaceID string) string {
	tenancy := ""
	_ = app.DB().NewQuery(`SELECT [[tenancy_key]] FROM {{leafen_app_workspaces}} WHERE [[install_id]] = {:install} AND [[workspace_id]] = {:workspace}`).
		Bind(dbx.Params{"install": installID, "workspace": workspaceID}).Row(&tenancy)
	return tenancy
}
