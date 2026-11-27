package app

import (
	"encoding/json"
	"maps"
	"time"
)

// A row page's created and last edited times are query properties of their
// own, so the public data source query can sort and filter on them as Notion
// does, without a property of that type. They take part in evaluation only:
// result rows never carry them, and nothing in the product graph names them.

const (
	databaseQueryRowCreatedTimeID = "leafen:row-created-time"
	databaseQueryRowEditedTimeID  = "leafen:row-last-edited-time"
)

func addDatabaseQueryRowTimestampProperties(context *databaseQueryContext, dataSourceID string) {
	for id, valueType := range map[string]string{
		databaseQueryRowCreatedTimeID: graphValueTypeCreatedTime,
		databaseQueryRowEditedTimeID:  graphValueTypeLastEditedTime,
	} {
		context.PropertiesByID[id] = storedDatabaseQueryProperty{
			ID: id, DataSourceID: dataSourceID, Type: valueType, Config: "{}",
		}
	}
}

// databaseQueryRowWithTimestamps is the row as filters and sorts see it:
// its page's created and edited times and people, under the synthetic
// timestamp properties and under each property of those types, which shows
// them as Notion's does.
func databaseQueryRowWithTimestamps(
	row databaseQueryRow, stored storedDatabaseQueryRow, context databaseQueryContext,
) databaseQueryRow {
	times := map[string]int64{
		graphValueTypeCreatedTime: stored.Created, graphValueTypeLastEditedTime: stored.Updated,
	}
	people := map[string]string{
		graphValueTypeCreatedBy: stored.CreatedBy, graphValueTypeLastEditedBy: stored.LastEditedBy,
	}
	values := make(map[string]graphRowValue, len(row.Values)+2)
	maps.Copy(values, row.Values)
	for id, property := range context.PropertiesByID {
		if at := times[property.Type]; at > 0 {
			encoded, _ := json.Marshal(time.UnixMilli(at).UTC().Format(time.RFC3339Nano))
			values[id] = graphRowValue{Type: graphValueTypeDate, Value: encoded}
		} else if person := people[property.Type]; person != "" {
			encoded, _ := json.Marshal([]string{person})
			values[id] = graphRowValue{Type: property.Type, Value: encoded}
		}
	}
	row.Values = values
	return row
}
