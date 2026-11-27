package app

import (
	"cmp"
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// notionRollupRequestField holds a rollup's request in its planned config
// until the whole request is planned: the relation it names may come in the
// same request, by name or by ID.
const notionRollupRequestField = "notionRollupRequest"

// notionRollupFunctionOrder is Notion's list of rollup calculations, in the
// order its API names them when refusing another (seen 2026-10-08).
var notionRollupFunctionOrder = []string{
	"count", "count_values", "empty", "not_empty", "unique", "show_unique", "percent_empty", "percent_not_empty",
	"sum", "average", "median", "min", "max", "range", "earliest_date", "latest_date", "date_range", "checked",
	"unchecked", "percent_checked", "percent_unchecked", "count_per_group", "percent_per_group", "show_original",
}

type notionRollupRequest struct {
	Key          string `json:"key"`
	RelationID   string `json:"relationId,omitempty"`
	RelationName string `json:"relationName,omitempty"`
	RollupID     string `json:"rollupId,omitempty"`
	RollupName   string `json:"rollupName,omitempty"`
	Function     string `json:"function"`
}

// notionDataSourceRollupRequest reads a rollup's settings as Notion's API
// takes them: the relation and the property it rolls up, each by name or ID
// (a name wins over an ID), and a calculation; other fields are left out.
// The request's shape is checked first (notionRollupSettingsShape), so
// settings that don't fit it come here only beside a property's "name", as
// Notion's API reads them then (seen 2026-11-12): no calculation, or one it
// doesn't have, is show_original (Notion keeps an unknown one as given), and
// a relation or property left out is refused once the request is planned.
func notionDataSourceRollupRequest(key string, object map[string]json.RawMessage) notionRollupRequest {
	request := notionRollupRequest{Key: key}
	for field, into := range map[string]*string{
		"relation_property_id": &request.RelationID, "relation_property_name": &request.RelationName,
		"rollup_property_id": &request.RollupID, "rollup_property_name": &request.RollupName, "function": &request.Function,
	} {
		if encoded, present := object[field]; present && json.Unmarshal(encoded, into) != nil {
			*into = ""
		}
	}
	if !slices.Contains(notionRollupFunctionOrder, request.Function) {
		request.Function = "show_original"
	}
	return request
}

type notionRollupSchemaProperty struct {
	ID     string `db:"id"`
	Name   string `db:"name"`
	Type   string `db:"type"`
	Config string `db:"config"`
}

// resolveNotionRollupRequests turns each rollup the request writes into the
// relation and property IDs it stores, reading the data source as the
// request leaves it. A relation or property left out (or given as "") is
// refused as Notion's API refuses it, in Leafen's words, and so is one it
// can't find by the name or ID
// as given, and a rollup of a rollup, as the app's list of properties to
// roll up leaves rollups out (seen 2026-11-12: a related data source's
// rollup, or this one's through a relation to itself, by name or ID; a
// button or a formula is taken). The refusal names a property the data
// source has by its name before the request, a new one by its key, as
// Notion's does.
func resolveNotionRollupRequests(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	dataSourceID string,
	operations []graphOperation,
	stored []storedNotionDataSourceProperty,
) error {
	own := make(map[string]notionRollupSchemaProperty, len(stored)+len(operations))
	storedNames := make(map[string]string, len(stored))
	for _, property := range stored {
		own[property.ID] = notionRollupSchemaProperty{ID: property.ID, Name: property.Name, Type: property.Type, Config: property.Config}
		storedNames[property.ID] = property.Name
	}
	for _, operation := range operations {
		switch {
		case operation.Property != nil:
			own[operation.Property.ID] = notionRollupSchemaProperty{
				ID: operation.Property.ID, Name: operation.Property.Name,
				Type: operation.Property.Type, Config: string(operation.Property.Config),
			}
		case operation.PropertyDeletion != nil:
			delete(own, operation.PropertyDeletion.ID)
		}
	}
	ownIDs, err := loadNotionPropertyIDs(app, dataSourceID)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if operation.Property == nil || normalizeNotionDatabaseRowPagePropertyType(operation.Property.Type) != graphValueTypeRollup {
			continue
		}
		config := map[string]any{}
		if json.Unmarshal(operation.Property.Config, &config) != nil {
			return errNotionDatabaseRowPageProjectionInvalid
		}
		encoded, pending := config[notionRollupRequestField]
		if !pending {
			continue
		}
		raw, err := json.Marshal(encoded)
		if err != nil {
			return err
		}
		request := notionRollupRequest{}
		if json.Unmarshal(raw, &request) != nil {
			return errNotionDatabaseRowPageProjectionInvalid
		}
		named := request.Key
		if storedName, has := storedNames[operation.Property.ID]; has {
			named = storedName
		}
		invalid := func(kind, given string) error {
			return notionDatabaseRowPageRequestInvalid(
				"Rollup property '%s' can't roll up through %s property '%s'", named, kind, given,
			)
		}
		left := notionDatabaseRowPageRequestInvalid("Rollup property '%s' isn't set up right", named)
		if cmp.Or(request.RelationName, request.RelationID) == "" {
			return left
		}
		relation, found := findNotionRollupProperty(own, ownIDs, request.RelationID, request.RelationName)
		if !found || normalizeNotionDatabaseRowPagePropertyType(relation.Type) != graphValueTypeRelation {
			return invalid("relation", cmp.Or(request.RelationName, request.RelationID))
		}
		relationConfig := map[string]any{}
		if json.Unmarshal([]byte(relation.Config), &relationConfig) != nil {
			return errNotionDatabaseRowPageProjectionInvalid
		}
		targets, targetIDs := own, ownIDs
		if targetID, _ := relationConfig["relationDatabaseId"].(string); targetID != dataSourceID {
			target, visible, err := notionDataSourceRelationTarget(app, authorization, relationConfig)
			if err != nil {
				return err
			}
			if !visible {
				return invalid("relation", cmp.Or(request.RelationName, request.RelationID))
			}
			if targets, err = loadNotionRollupTargetProperties(app, target); err != nil {
				return err
			}
			if targetIDs, err = loadNotionPropertyIDs(app, target.DataSource); err != nil {
				return err
			}
		}
		if cmp.Or(request.RollupName, request.RollupID) == "" {
			return left
		}
		rolled, found := findNotionRollupProperty(targets, targetIDs, request.RollupID, request.RollupName)
		if !found {
			return invalid("rollup", cmp.Or(request.RollupName, request.RollupID))
		}
		if normalizeNotionDatabaseRowPagePropertyType(rolled.Type) == graphValueTypeRollup {
			return notionDatabaseRowPageRequestInvalid(
				"Rollup property %s can't roll up a rollup of the related database.", named,
			)
		}
		delete(config, notionRollupRequestField)
		config["rollupRelationPropertyId"] = relation.ID
		config["rollupTargetPropertyId"] = rolled.ID
		config["rollupFunction"] = request.Function
		resolved, err := json.Marshal(config)
		if err != nil {
			return err
		}
		operation.Property.Config = resolved
	}
	return nil
}

// findNotionRollupProperty finds a property by name first, as Notion's API
// does when both are given, then by its public ID, URL-encoded or not, or
// Leafen's.
func findNotionRollupProperty(
	properties map[string]notionRollupSchemaProperty, ids notionPropertyIDs, id, name string,
) (notionRollupSchemaProperty, bool) {
	if name != "" {
		for _, property := range properties {
			if property.Name == name {
				return property, true
			}
		}
		return notionRollupSchemaProperty{}, false
	}
	if internalID, named := ids.internalID(id); named {
		id = internalID
	}
	property, found := properties[id]
	return property, found
}

func loadNotionRollupTargetProperties(app core.App, target notionDataSourceParentAuthority) (map[string]notionRollupSchemaProperty, error) {
	rows := []notionRollupSchemaProperty{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[owner]]={:owner} AND [[data_source_id]]={:dataSourceId}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
	`).Bind(dbx.Params{"owner": target.OwnerID, "dataSourceId": target.DataSource}).All(&rows); err != nil {
		return nil, err
	}
	properties := make(map[string]notionRollupSchemaProperty, len(rows))
	for _, row := range rows {
		properties[row.ID] = row
	}
	return properties, nil
}
