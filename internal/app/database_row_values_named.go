package app

import "github.com/pocketbase/pocketbase/core"

// A row's values are written by property as a caller knows it: by its ID —
// Leafen's own, as GET /pages/{id}/content-graph names it, or the public one
// the MCP tools and the Notion-compatible API give (notion_property_public_id.go)
// — or by its name where only one of the database's properties has it, as
// the MCP tools take a row's properties (2026-11-26: an agent attaching a
// file to a row's "첨부" through leafen_call had to look up the column's ID
// first). A value's type left out is its property's. Each write that takes a
// row's values by property — POST /database-rows/{rowId}/page/values, its
// values, the values it read and its new options, and the rows of POST
// /pages/{pageId}/database-row-values — reads its keys so, before checking
// them (planDatabaseRowPageValues).

// databaseRowPropertyKeys names the row's database's properties by every
// key a write may give one.
type databaseRowPropertyKeys struct {
	app          core.App
	dataSourceID string
	byID         map[string]databaseRowPageProperty
	byName       map[string][]databaseRowPageProperty
	public       *notionPropertyIDs
}

func newDatabaseRowPropertyKeys(app core.App, dataSourceID string, properties []databaseRowPageProperty) *databaseRowPropertyKeys {
	keys := &databaseRowPropertyKeys{
		app: app, dataSourceID: dataSourceID,
		byID:   make(map[string]databaseRowPageProperty, len(properties)),
		byName: make(map[string][]databaseRowPageProperty, len(properties)),
	}
	for _, property := range properties {
		keys.byID[property.ID] = property
		keys.byName[property.Name] = append(keys.byName[property.Name], property)
	}
	return keys
}

// loadDatabaseRowPropertyKeys reads the row's database's properties for the keys.
func loadDatabaseRowPropertyKeys(app core.App, authority databaseRowPageAuthority) (*databaseRowPropertyKeys, error) {
	properties, err := loadDatabaseRowPageProperties(app, authority, authority.ContainerPageID, authority.DataSourceID)
	if err != nil {
		return nil, err
	}
	return newDatabaseRowPropertyKeys(app, authority.DataSourceID, properties), nil
}

// property is the one a key names, by its own ID, its public ID, or its name.
func (keys *databaseRowPropertyKeys) property(field, key string) (databaseRowPageProperty, error) {
	if property, found := keys.byID[key]; found {
		return property, nil
	}
	if keys.public == nil {
		public, err := loadNotionPropertyIDs(keys.app, keys.dataSourceID)
		if err != nil {
			return databaseRowPageProperty{}, err
		}
		keys.public = &public
	}
	if internalID, found := keys.public.internalID(key); found {
		if property, held := keys.byID[internalID]; held {
			return property, nil
		}
	}
	switch named := keys.byName[key]; len(named) {
	case 1:
		return named[0], nil
	case 0:
		return databaseRowPageProperty{}, refuse(errDatabaseRowPageValuesInvalid,
			"%s.%s names no property of the row's database by its name or ID: GET /pages/{id}/content-graph names them", field, key)
	default:
		return databaseRowPageProperty{}, refuse(errDatabaseRowPageValuesInvalid,
			"%s.%s is the name of %d of the row's database's properties: name it by its ID, as GET /pages/{id}/content-graph names them",
			field, key, len(named))
	}
}

// databaseRowValuesByID is a write's map by property as its ID keys it.
func databaseRowValuesByID[V any](keys *databaseRowPropertyKeys, field string, given map[string]V) (map[string]V, error) {
	if len(given) == 0 {
		return given, nil
	}
	byID := make(map[string]V, len(given))
	keyed := make(map[string]string, len(given))
	for key, value := range given {
		property, err := keys.property(field, key)
		if err != nil {
			return nil, err
		}
		if first, twice := keyed[property.ID]; twice {
			return nil, refuse(errDatabaseRowPageValuesInvalid, "%s.%s and %s.%s name the same property, %q: give it once",
				field, first, field, key, property.Name)
		}
		keyed[property.ID] = key
		byID[property.ID] = value
	}
	return byID, nil
}

// databaseRowPageReadByID is a row's write's values read and new options
// keyed by property ID; its values are keyed so where they are checked
// (planDatabaseRowPageValues).
func databaseRowPageReadByID(app core.App, authority databaseRowPageAuthority, input *databaseRowPageValuesInput) error {
	if len(input.Expected) == 0 && len(input.Options) == 0 {
		return nil
	}
	keys, err := loadDatabaseRowPropertyKeys(app, authority)
	if err != nil {
		return err
	}
	expected, err := databaseRowValuesByID(keys, "expected", input.Expected)
	if err != nil {
		return err
	}
	options, err := databaseRowValuesByID(keys, "options", input.Options)
	if err != nil {
		return err
	}
	input.Expected, input.Options = expected, options
	return nil
}

// databaseRowValueTyped is a value with its property's type where it left
// its own out.
func databaseRowValueTyped(value graphRowValue, propertyType string) graphRowValue {
	if value.Type == "" {
		value.Type = propertyType
	}
	return value
}
