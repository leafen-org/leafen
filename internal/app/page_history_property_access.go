package app

import (
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's history shows what its reader may see now under the databases'
// property access rules (database_property_access.go), whatever the rules
// were when a version was made, as Notion's version history keeps the
// current rules: a row page's values, a database's rows as they stood and
// the lines naming a changed value leave out what the reader may not see,
// and a property shown to nobody in their place leaves a database's columns.

type pageHistoryPropertyAccess struct {
	reader  *databasePropertyAccessReader
	sources map[string]string
}

// newPageHistoryPropertyAccess reads the rules of the properties' databases
// for one reader; nil where none has a rule.
func newPageHistoryPropertyAccess(app core.App, readerID string, propertyIDs []string) (*pageHistoryPropertyAccess, error) {
	if len(propertyIDs) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(propertyIDs)
	if err != nil {
		return nil, err
	}
	stored := []struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]] FROM {{leafen_database_properties}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&stored); err != nil {
		return nil, err
	}
	sources := map[string]string{}
	distinct := []string{}
	seen := map[string]bool{}
	for _, property := range stored {
		sources[property.ID] = property.DataSourceID
		if !seen[property.DataSourceID] {
			seen[property.DataSourceID] = true
			distinct = append(distinct, property.DataSourceID)
		}
	}
	reader, err := newDatabasePropertyAccessReader(app, readerID, distinct)
	if err != nil || reader == nil {
		return nil, err
	}
	return &pageHistoryPropertyAccess{reader: reader, sources: sources}, nil
}

// hidesValue is whether the reader may not see the property's value in the
// row (nil values: in no row in particular).
func (access *pageHistoryPropertyAccess) hidesValue(rowID, propertyID string, values map[string]pageHistoryRowValue) (bool, error) {
	if access == nil {
		return false, nil
	}
	source := access.sources[propertyID]
	if !access.reader.restricted(source, propertyID) {
		return false, nil
	}
	var row *databasePropertyAccessRow
	if values != nil {
		row = &databasePropertyAccessRow{ID: rowID, People: func(id string) []string { return peopleInValue(values[id].Value) }}
	}
	level, err := access.reader.level(source, propertyID, row)
	return !propertyAccessShowsValues(level), err
}

// gone is whether the property shows the reader in no row.
func (access *pageHistoryPropertyAccess) gone(propertyID string) (bool, error) {
	source := access.sources[propertyID]
	if !access.reader.restricted(source, propertyID) {
		return false, nil
	}
	shown, err := access.reader.propertyShown(source, propertyID)
	return !shown, err
}

func (access *pageHistoryPropertyAccess) redactValues(rowID string, values map[string]pageHistoryRowValue) error {
	hidden := []string{}
	for propertyID := range values {
		hides, err := access.hidesValue(rowID, propertyID, values)
		if err != nil {
			return err
		}
		if hides {
			hidden = append(hidden, propertyID)
		}
	}
	for _, propertyID := range hidden {
		delete(values, propertyID)
	}
	return nil
}

// redactPageHistorySnapshot applies the rules to a version as it stood, and
// leaves out of its databases the unlinked items the reader has no role in
// now (database_row_unlinked_access.go).
func redactPageHistorySnapshot(app core.App, readerID, pageID string, response *pageHistorySnapshotResponse) error {
	rowIDs := []string{}
	for _, database := range response.Databases {
		for _, row := range database.Rows {
			rowIDs = append(rowIDs, row.ID)
		}
	}
	items, err := unlinkedRowAccessFor(app, readerID, rowIDs)
	if err != nil {
		return err
	}
	if items != nil {
		for index := range response.Databases {
			rows := response.Databases[index].Rows[:0]
			for _, row := range response.Databases[index].Rows {
				if !items.hides(row.ID) {
					rows = append(rows, row)
				}
			}
			response.Databases[index].Rows = rows
		}
	}
	if response.RelationRowTitles, err = pageHistoryRelationRowTitles(app, readerID, response); err != nil {
		return err
	}
	propertyIDs := []string{}
	for propertyID := range response.Values {
		propertyIDs = append(propertyIDs, propertyID)
	}
	for _, database := range response.Databases {
		for _, property := range database.Properties {
			propertyIDs = append(propertyIDs, property.ID)
		}
	}
	access, err := newPageHistoryPropertyAccess(app, readerID, propertyIDs)
	if err != nil || access == nil {
		return err
	}
	if err := access.redactValues(pageID, response.Values); err != nil {
		return err
	}
	for index := range response.Databases {
		database := &response.Databases[index]
		for _, row := range database.Rows {
			if err := access.redactValues(row.ID, row.Values); err != nil {
				return err
			}
		}
		kept := database.Properties[:0]
		for _, property := range database.Properties {
			if gone, err := access.gone(property.ID); err != nil {
				return err
			} else if !gone {
				kept = append(kept, property)
			}
		}
		database.Properties = kept
	}
	return nil
}

// pageHistoryRelationRowTitles names the rows a version's relations point at
// for its reader, those they can open; a version shows them by name, as a cell
// does, never by their ids.
func pageHistoryRelationRowTitles(
	app core.App, readerID string, response *pageHistorySnapshotResponse,
) (map[string]string, error) {
	const limit = 500
	titles := map[string]string{}
	name := func(values map[string]pageHistoryRowValue) error {
		for _, value := range values {
			if value.Type != graphValueTypeRelation {
				continue
			}
			ids := []string{}
			if json.Unmarshal(value.Value, &ids) != nil {
				continue
			}
			for _, id := range ids {
				if _, known := titles[id]; known || len(titles) >= limit {
					continue
				}
				row, err := loadDatabaseRowPageAuthority(app, id)
				if errors.Is(err, errDatabaseRowPageNotFound) {
					continue
				}
				if err != nil {
					return err
				}
				if _, err := authorizeDatabaseRowPageRecord(app, readerID, row, pageAccessRoleView); isPermissionDenied(err) {
					continue
				} else if err != nil {
					return err
				}
				titles[id] = databaseRowPageTitle(app, id)
			}
		}
		return nil
	}
	if err := name(response.Values); err != nil {
		return nil, err
	}
	for _, database := range response.Databases {
		for _, row := range database.Rows {
			if err := name(row.Values); err != nil {
				return nil, err
			}
		}
	}
	return titles, nil
}

// redactPageHistoryChanges leaves out the lines naming a value the reader
// may not see, and the changes to an unlinked item they have no role in.
func redactPageHistoryChanges(app core.App, readerID string, changes []pageHistoryChangeWire) ([]pageHistoryChangeWire, error) {
	itemIDs := []string{}
	for _, change := range changes {
		if change.ItemID != "" {
			itemIDs = append(itemIDs, change.ItemID)
		}
	}
	items, err := unlinkedRowAccessFor(app, readerID, itemIDs)
	if err != nil {
		return nil, err
	}
	if items != nil {
		shown := changes[:0]
		for _, change := range changes {
			if !items.hides(change.ItemID) {
				shown = append(shown, change)
			}
		}
		changes = shown
	}
	propertyIDs := []string{}
	for _, change := range changes {
		for _, line := range change.RowValues {
			propertyIDs = append(propertyIDs, line.PropertyID)
		}
	}
	access, err := newPageHistoryPropertyAccess(app, readerID, propertyIDs)
	if err != nil || access == nil {
		return changes, err
	}
	for index := range changes {
		kept := changes[index].RowValues[:0]
		for _, line := range changes[index].RowValues {
			if hides, err := access.hidesValue(changes[index].ItemID, line.PropertyID, nil); err != nil {
				return nil, err
			} else if !hides {
				kept = append(kept, line)
			}
		}
		changes[index].RowValues = kept
	}
	return changes, nil
}
