package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// myTaskSourceProperties is a picked source's properties as its rows' pages
// show them to the person (loadDatabaseRowPagePropertyWires), their access
// rules applied as a read of its database applies them
// (redactGraphItemsForReader): one the person may not see left out, a
// restricted one marked with the person's own level. A task made in the
// source on the device opens its page with them, before the server has the
// task (web lib/myTaskMadeHere.ts).
func myTaskSourceProperties(app core.App, readerID string, source databaseMyTaskSourceWire) ([]databaseMyTaskSourcePropertyWire, error) {
	properties := []databaseRowPageProperty{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[name]], [[config]], [[position]]
		FROM {{leafen_database_properties}}
		WHERE [[page_id]]={:pageId} AND [[data_source_id]]={:dataSourceId}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"pageId": source.PageID, "dataSourceId": source.DataSourceID}).All(&properties); err != nil {
		return nil, err
	}
	reader, err := newDatabasePropertyAccessReaderOfRows(app, readerID, []string{source.DataSourceID}, nil)
	if err != nil {
		return nil, err
	}
	wires := make([]databaseMyTaskSourcePropertyWire, 0, len(properties))
	for _, property := range properties {
		config := json.RawMessage(property.Config)
		if !json.Valid(config) {
			config = json.RawMessage(`{}`)
		}
		wire := databaseMyTaskSourcePropertyWire{ID: property.ID, Name: property.Name, Type: property.Type, Config: config, Position: property.Position}
		if reader != nil && reader.restricted(source.DataSourceID, property.ID) {
			shown, err := reader.propertyShown(source.DataSourceID, property.ID)
			if err != nil {
				return nil, err
			}
			if !shown {
				continue
			}
			level, err := reader.ownLevel(source.DataSourceID, property.ID, nil)
			if err != nil {
				return nil, err
			}
			wire.Access = &graphPropertyAccess{Restricted: reader.rules[source.DataSourceID][property.ID] != nil, Level: level,
				EditsProperty: level == propertyAccessEdit, VariesByPerson: reader.variesByPerson(source.DataSourceID, property.ID)}
		}
		wires = append(wires, wire)
	}
	return wires, nil
}
