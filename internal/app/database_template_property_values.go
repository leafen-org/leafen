package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database template holds values only for its database's properties, as
// the database's pages do: a property deleted for good takes its value and
// formatting out of the templates as its deletion takes its cells out of the
// rows, a template change keeps no value of a property the database no
// longer has, and a template leaves in an export without one. A property in
// Deleted properties keeps its template values for a restore.

// databaseTemplateProperties is whether a property is one of a database's,
// those in Deleted properties included.
func databaseTemplateProperties(app core.App, pageID, dataSourceID string) (func(string) bool, error) {
	ids := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_properties}}
		WHERE [[page_id]]={:pageId} AND [[data_source_id]]={:dataSourceId}`).
		Bind(dbx.Params{"pageId": pageID, "dataSourceId": dataSourceID}).Column(&ids); err != nil {
		return nil, err
	}
	properties := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		properties[id] = struct{}{}
	}
	return func(propertyID string) bool {
		_, found := properties[propertyID]
		return found
	}, nil
}

// keepDatabaseTemplateValues keeps, of a template's values or their
// formatting by property, those of the properties keep admits.
func keepDatabaseTemplateValues(raw []byte, keep func(string) bool) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return raw, nil
	}
	values := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &values) != nil {
		return nil, errDatabaseToolbarConfigInvalid
	}
	dropped := false
	for propertyID := range values {
		if !keep(propertyID) {
			delete(values, propertyID)
			dropped = true
		}
	}
	if !dropped {
		return raw, nil
	}
	return json.Marshal(values)
}

// keepDatabaseToolbarTemplateValues keeps, of the values and formatting a
// template change gives, those of the template's database's properties, so
// an editor opened before a property was deleted doesn't bring its value
// back.
func keepDatabaseToolbarTemplateValues(
	app core.App, authority databaseToolbarConfigAuthority, properties string, spans []byte,
) (string, []byte, error) {
	keep, err := databaseTemplateProperties(app, authority.PageID, authority.DataSourceID)
	if err != nil {
		return "", nil, err
	}
	kept, err := keepDatabaseTemplateValues([]byte(properties), keep)
	if err != nil {
		return "", nil, err
	}
	keptSpans, err := keepDatabaseTemplateValues(spans, keep)
	if err != nil {
		return "", nil, err
	}
	return string(kept), keptSpans, nil
}

// forgetDatabaseTemplatePropertyValues takes a property deleted for good out
// of the values and formatting of its database's templates.
func forgetDatabaseTemplatePropertyValues(app core.App, pageID, propertyID string) error {
	templates := []struct {
		ID         string `db:"id"`
		Properties string `db:"properties_json"`
		Spans      string `db:"property_spans_json"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[properties_json]], [[property_spans_json]]
		FROM {{leafen_database_templates}} WHERE [[page_id]]={:pageId}`).
		Bind(dbx.Params{"pageId": pageID}).All(&templates); err != nil {
		return err
	}
	keep := func(id string) bool { return id != propertyID }
	for _, template := range templates {
		// Values no one can read hold none to take out.
		properties, propertiesErr := keepDatabaseTemplateValues([]byte(template.Properties), keep)
		spans, spansErr := keepDatabaseTemplateValues([]byte(template.Spans), keep)
		if propertiesErr != nil || spansErr != nil ||
			string(properties) == template.Properties && string(spans) == template.Spans {
			continue
		}
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_database_templates}}
			SET [[properties_json]]={:properties}, [[property_spans_json]]={:spans} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"properties": string(properties), "spans": string(spans), "id": template.ID}).
			Execute(); err != nil {
			return err
		}
	}
	return nil
}

// keepExportedDatabaseTemplateValues leaves out of an exported template the
// values and formatting of properties its database no longer has.
func keepExportedDatabaseTemplateValues(app core.App, template map[string]any) error {
	pageID, _ := template["pageId"].(string)
	dataSourceID, _ := template["dataSourceId"].(string)
	keep, err := databaseTemplateProperties(app, pageID, dataSourceID)
	if err != nil {
		return err
	}
	for _, field := range []string{"properties", "propertySpans"} {
		values, _ := template[field].(map[string]any)
		for propertyID := range values {
			if !keep(propertyID) {
				delete(values, propertyID)
			}
		}
	}
	return nil
}
