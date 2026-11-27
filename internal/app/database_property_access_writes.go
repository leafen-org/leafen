package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Changes under the databases' property access rules
// (database_property_access.go): a row's value changes only for whoever may
// edit that property's values in that row, and a property's name, kind or
// settings, or the property itself, only for whoever may edit the property.

func propertyAccessDeniedError() error {
	return &graphRequestError{
		status: http.StatusForbidden, code: errorCodeGraphPropertyAccessDenied,
		message: "You can't change this property.",
	}
}

type storedPropertyAccessCell struct {
	PropertyID string `db:"property_id"`
	Value      string `db:"value"`
}

// refusePropertyAccessChanges refuses the operations that change what the
// person may not change.
func refusePropertyAccessChanges(app core.App, principalID string, operations []graphOperation) error {
	sources := []string{}
	seen := map[string]bool{}
	add := func(source string) {
		if source != "" && !seen[source] {
			seen[source] = true
			sources = append(sources, source)
		}
	}
	deleted := map[string]string{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil:
			add(operation.Row.DataSourceID)
		case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil:
			add(operation.Property.DataSourceID)
		case operation.Kind == graphOperationKindDeleteProperty && operation.PropertyDeletion != nil:
			var source string
			if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_properties}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": operation.PropertyDeletion.ID}).Row(&source); err == nil {
				deleted[operation.PropertyDeletion.ID] = source
				add(source)
			}
		}
	}
	reader, err := newDatabasePropertyAccessReader(app, principalID, sources)
	if err != nil || reader == nil {
		return err
	}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil:
			if err := reader.refuseRowChange(operation.Row); err != nil {
				return err
			}
		case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil:
			if err := reader.refusePropertyChange(operation.Property); err != nil {
				return err
			}
		case operation.Kind == graphOperationKindDeleteProperty && operation.PropertyDeletion != nil &&
			deleted[operation.PropertyDeletion.ID] != "":
			if err := reader.refusePropertyEdit(deleted[operation.PropertyDeletion.ID], operation.PropertyDeletion.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (reader *databasePropertyAccessReader) refusePropertyEdit(dataSourceID, propertyID string) error {
	if !reader.restricted(dataSourceID, propertyID) {
		return nil
	}
	level, err := reader.level(dataSourceID, propertyID, nil)
	if err != nil {
		return err
	}
	if level != propertyAccessEdit {
		return propertyAccessDeniedError()
	}
	return nil
}

// refusePropertyChange refuses a change to a restricted property's name,
// kind or settings by whoever may not edit it.
func (reader *databasePropertyAccessReader) refusePropertyChange(property *graphProperty) error {
	if !reader.restricted(property.DataSourceID, property.ID) {
		return nil
	}
	stored := struct {
		Type   string `db:"type"`
		Name   string `db:"name"`
		Config string `db:"config"`
	}{}
	if err := reader.app.DB().NewQuery(`
		SELECT [[type]], [[name]], [[config]] FROM {{leafen_database_properties}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": property.ID}).One(&stored); err != nil {
		return reader.refusePropertyEdit(property.DataSourceID, property.ID)
	}
	if stored.Type == property.Type && stored.Name == property.Name && jsonEquivalent([]byte(stored.Config), property.Config) {
		return nil
	}
	return reader.refusePropertyEdit(property.DataSourceID, property.ID)
}

// refuseRowChange refuses a change to a restricted value of the row by
// whoever may not edit that value there.
func (reader *databasePropertyAccessReader) refuseRowChange(row *graphDatabaseRow) error {
	restricted := false
	for propertyID := range row.Values {
		if reader.restricted(row.DataSourceID, propertyID) {
			restricted = true
		}
	}
	if !restricted {
		return nil
	}
	cells := []storedPropertyAccessCell{}
	if err := reader.app.DB().NewQuery(`
		SELECT [[property_id]], [[value]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:rowId}
	`).Bind(dbx.Params{"rowId": row.ID}).All(&cells); err != nil {
		return err
	}
	storedValues := make(map[string]string, len(cells))
	for _, cell := range cells {
		storedValues[cell.PropertyID] = cell.Value
	}
	var createdBy string
	if err := reader.app.DB().NewQuery(`SELECT [[created_by]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": row.ID}).Row(&createdBy); err != nil {
		// A row made by this change is the person's own.
		createdBy = reader.readerID
	}
	accessRow := &databasePropertyAccessRow{ID: row.ID, CreatedBy: createdBy, People: func(propertyID string) []string {
		if value, ok := storedValues[propertyID]; ok {
			return peopleInValue([]byte(value))
		}
		if value, ok := row.Values[propertyID]; ok {
			return peopleInValue(value.Value)
		}
		return nil
	}}
	for propertyID, value := range row.Values {
		if !reader.restricted(row.DataSourceID, propertyID) {
			continue
		}
		before, had := storedValues[propertyID]
		if graphRowValueIsCleared(value.Value) && !had || had && jsonEquivalent([]byte(before), value.Value) {
			continue
		}
		level, err := reader.level(row.DataSourceID, propertyID, accessRow)
		if err != nil {
			return err
		}
		if !propertyAccessEditsValues(level) {
			return propertyAccessDeniedError()
		}
	}
	return nil
}

// jsonEquivalent is whether two JSON texts hold the same value.
func jsonEquivalent(left, right []byte) bool {
	if bytes.Equal(bytes.TrimSpace(left), bytes.TrimSpace(right)) {
		return true
	}
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	leftText, _ := json.Marshal(leftValue)
	rightText, _ := json.Marshal(rightValue)
	return bytes.Equal(leftText, rightText)
}

// refuseRestrictedPropertyEdit refuses moving a restricted property to the
// trash, restoring or deleting it by whoever may not edit it.
func refuseRestrictedPropertyEdit(app core.App, principalID, propertyID string) error {
	var source string
	if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_properties}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": propertyID}).Row(&source); err != nil {
		return nil
	}
	reader, err := newDatabasePropertyAccessReader(app, principalID, []string{source})
	if err != nil || reader == nil {
		return err
	}
	return reader.refusePropertyEdit(source, propertyID)
}

// withoutHiddenPageDuplicationCells leaves out of a copy of a page or a
// database block the values its maker may not see under the properties'
// access rules: as themselves, or as a visitor where they copy a published
// page they can't otherwise open.
func withoutHiddenPageDuplicationCells(app core.App, actorID string, rows []pageDuplicationSourceDatabaseRow,
	cells []pageDuplicationSourceDatabaseCell) ([]pageDuplicationSourceDatabaseCell, error) {
	sourceOf := make(map[string]string, len(rows))
	sources := []string{}
	for _, row := range rows {
		if _, known := sourceOf[row.ID]; !known && !slices.Contains(sources, row.DataSourceID) {
			sources = append(sources, row.DataSourceID)
		}
		sourceOf[row.ID] = row.DataSourceID
	}
	actor, err := newDatabasePropertyAccessReader(app, actorID, sources)
	if err != nil || actor == nil {
		return cells, err
	}
	var visitor *databasePropertyAccessReader
	values := map[string]map[string][]byte{}
	for _, cell := range cells {
		if values[cell.RowID] == nil {
			values[cell.RowID] = map[string][]byte{}
		}
		values[cell.RowID][cell.PropertyID] = []byte(cell.Value)
	}
	kept := make([]pageDuplicationSourceDatabaseCell, 0, len(cells))
	for _, cell := range cells {
		source := sourceOf[cell.RowID]
		reader := actor
		role, err := actor.role(source, cell.RowID)
		if err != nil {
			return nil, err
		}
		if role == pageAccessRoleNone {
			if visitor == nil {
				if visitor, err = newDatabasePropertyAccessReader(app, "", sources); err != nil {
					return nil, err
				}
			}
			reader = visitor
		}
		if !reader.restricted(source, cell.PropertyID) {
			kept = append(kept, cell)
			continue
		}
		rowValues := values[cell.RowID]
		level, err := reader.level(source, cell.PropertyID, &databasePropertyAccessRow{ID: cell.RowID,
			People: func(propertyID string) []string { return peopleInValue(rowValues[propertyID]) }})
		if err != nil {
			return nil, err
		}
		if propertyAccessShowsValues(level) {
			kept = append(kept, cell)
		}
	}
	return kept, nil
}

// withoutUneditableValues leaves out of new database items — a duplicate,
// or one made from a template — the values their maker may not set there,
// which writing them would refuse.
func withoutUneditableValues(app core.App, principalID, dataSourceID string, values map[string]map[string]graphRowValue) error {
	reader, err := newDatabasePropertyAccessReader(app, principalID, []string{dataSourceID})
	if err != nil || reader == nil {
		return err
	}
	for rowID, rowValues := range values {
		accessRow := &databasePropertyAccessRow{ID: rowID, People: func(propertyID string) []string {
			return peopleInValue(rowValues[propertyID].Value)
		}}
		hidden := []string{}
		for propertyID := range rowValues {
			if !reader.restricted(dataSourceID, propertyID) {
				continue
			}
			level, err := reader.level(dataSourceID, propertyID, accessRow)
			if err != nil {
				return err
			}
			if !propertyAccessEditsValues(level) {
				hidden = append(hidden, propertyID)
			}
		}
		for _, propertyID := range hidden {
			delete(rowValues, propertyID)
		}
	}
	return nil
}
