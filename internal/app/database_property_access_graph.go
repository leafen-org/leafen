package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A content graph read as its reader may see it under the databases'
// property access rules (database_property_access.go): a property shown to
// nobody in the reader's place leaves the read; a restricted property
// carries how it shows the reader ("access"), so its column shows a lock for
// everyone; and each row leaves out the values the reader may not see,
// naming them in "hiddenValues" where the column still shows ("사용 권한
// 없음" in the cell), and names in "readOnlyValues" those they may see but
// not change.

type graphPropertyAccess struct {
	Restricted     bool   `json:"restricted"`
	Level          string `json:"level"`
	EditsProperty  bool   `json:"editsProperty"`
	VariesByPerson bool   `json:"variesByPerson,omitzero"`
}

// redactGraphItemsForReader applies the rules of the read's databases to
// it, and their unlinked items' sharing (database_row_unlinked_access.go):
// an item the reader has no role in leaves the read, one they may not
// change says so ("readOnlyForMe").
func redactGraphItemsForReader(app core.App, readerID string, items []graphReadItem) ([]graphReadItem, error) {
	sources := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		if item.Kind != graphReadKindProperty && item.Kind != graphReadKindRow {
			continue
		}
		if source, _ := item.Value["dataSourceId"].(string); source != "" && !seen[source] {
			seen[source] = true
			sources = append(sources, source)
		}
	}
	// The read's own items alone are asked about (database_row_window_access.go).
	rowIDs := []string{}
	for _, item := range items {
		if id, _ := item.Value["id"].(string); item.Kind == graphReadKindRow && id != "" {
			rowIDs = append(rowIDs, id)
		}
	}
	rows, err := loadDatabaseWindowRowAccess(app, readerID, sources, rowIDs)
	if err != nil {
		return nil, err
	}
	if rows != nil {
		shown := items[:0]
		for _, item := range items {
			if item.Kind == graphReadKindRow {
				id, _ := item.Value["id"].(string)
				if rows.hides(id) {
					continue
				}
				if rows.readOnly(id) {
					item.Value["readOnlyForMe"] = true
				}
			}
			shown = append(shown, item)
		}
		items = shown
	}
	reader, err := newDatabasePropertyAccessReaderOfRows(app, readerID, sources, rowIDs)
	if err != nil || reader == nil {
		return items, err
	}
	kept := make([]graphReadItem, 0, len(items))
	for _, item := range items {
		source, _ := item.Value["dataSourceId"].(string)
		switch item.Kind {
		case graphReadKindProperty:
			propertyID, _ := item.Value["id"].(string)
			if !reader.restricted(source, propertyID) {
				break
			}
			shown, err := reader.propertyShown(source, propertyID)
			if err != nil {
				return nil, err
			}
			if !shown {
				continue
			}
			// A formula or rollup shows as its own level says; what it reads
			// hidden only empties its values (redactRowItem).
			level, err := reader.ownLevel(source, propertyID, nil)
			if err != nil {
				return nil, err
			}
			item.Value["access"] = graphPropertyAccess{Restricted: reader.rules[source][propertyID] != nil, Level: level, EditsProperty: level == propertyAccessEdit,
				VariesByPerson: reader.variesByPerson(source, propertyID)}
		case graphReadKindRow:
			if err := reader.redactRowItem(source, item.Value); err != nil {
				return nil, err
			}
		}
		kept = append(kept, item)
	}
	return kept, nil
}

// variesByPerson is whether a person property's exception may show the
// property otherwise in some rows; a formula reading the row's own varying
// property, however far, varies with it.
func (reader *databasePropertyAccessReader) variesByPerson(dataSourceID, propertyID string) bool {
	start := databasePropertyAccessKey{dataSourceID, propertyID}
	seen := map[databasePropertyAccessKey]bool{start: true}
	for pending := []databasePropertyAccessKey{start}; len(pending) > 0; {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if rule := reader.rules[current.DataSourceID][current.PropertyID]; rule != nil {
			for _, exception := range rule.Exceptions {
				if exception.PrincipalType == propertyAccessPrincipalProperty {
					return true
				}
			}
		}
		for _, read := range reader.reads[current.DataSourceID][current.PropertyID] {
			key := databasePropertyAccessKey{read.DataSourceID, read.PropertyID}
			if read.SameRow && !seen[key] && reader.restricted(read.DataSourceID, read.PropertyID) {
				seen[key] = true
				pending = append(pending, key)
			}
		}
	}
	return false
}

// redactRowItem takes from a row's values what the reader may not see.
func (reader *databasePropertyAccessReader) redactRowItem(dataSourceID string, row map[string]any) error {
	values, _ := row["values"].(map[string]any)
	restricted := reader.restrictedProperties(dataSourceID)
	if values == nil || len(restricted) == 0 {
		return nil
	}
	rowID, _ := row["id"].(string)
	createdBy, _ := row["createdBy"].(string)
	accessRow := &databasePropertyAccessRow{ID: rowID, CreatedBy: createdBy, People: func(propertyID string) []string {
		return peopleInGraphValue(values[propertyID])
	}}
	hidden := []string{}
	readOnly := []string{}
	decisions := map[string]string{}
	for _, propertyID := range restricted {
		level, err := reader.level(dataSourceID, propertyID, accessRow)
		if err != nil {
			return err
		}
		decisions[propertyID] = level
	}
	for propertyID, level := range decisions {
		if !propertyAccessShowsValues(level) {
			delete(values, propertyID)
			// A formula or rollup that reads a value the reader may not see
			// shows empty, as Notion's does (seen 2026-10-12), not as hidden.
			own, err := reader.ownLevel(dataSourceID, propertyID, accessRow)
			if err != nil {
				return err
			}
			if propertyAccessShowsValues(own) {
				continue
			}
			shown, err := reader.propertyShown(dataSourceID, propertyID)
			if err != nil {
				return err
			}
			if shown {
				hidden = append(hidden, propertyID)
			}
			continue
		}
		if !propertyAccessEditsValues(level) {
			readOnly = append(readOnly, propertyID)
		}
	}
	if len(hidden) > 0 {
		sort.Strings(hidden)
		row["hiddenValues"] = hidden
	}
	if len(readOnly) > 0 {
		sort.Strings(readOnly)
		row["readOnlyValues"] = readOnly
	}
	return nil
}

// peopleInGraphValue reads the people a graph row value names.
func peopleInGraphValue(value any) []string {
	cell, _ := value.(map[string]any)
	if cell == nil {
		return nil
	}
	raw, err := json.Marshal(cell["value"])
	if err != nil {
		return nil
	}
	return peopleInValue(raw)
}

// redactDatabaseRowPage applies the rules of the page's database to a
// database page read: its properties and its values.
func redactDatabaseRowPage(app core.App, readerID string, snapshot *databaseRowPageSnapshot) error {
	source := snapshot.Row.DataSourceID
	reader, err := newDatabasePropertyAccessReaderOfRows(app, readerID, []string{source}, []string{snapshot.Row.ID})
	if err != nil || reader == nil {
		return err
	}
	values := map[string]any{}
	if len(snapshot.Row.Values) > 0 {
		if err := json.Unmarshal(snapshot.Row.Values, &values); err != nil {
			return err
		}
	}
	row := map[string]any{"id": snapshot.Row.ID, "createdBy": snapshot.Row.CreatedBy, "values": values}
	if err := reader.redactRowItem(source, row); err != nil {
		return err
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	snapshot.Row.Values = encoded
	snapshot.Row.HiddenValues, _ = row["hiddenValues"].([]string)
	snapshot.Row.ReadOnlyValues, _ = row["readOnlyValues"].([]string)
	properties := snapshot.Properties[:0]
	for _, property := range snapshot.Properties {
		if reader.restricted(source, property.ID) {
			shown, err := reader.propertyShown(source, property.ID)
			if err != nil {
				return err
			}
			if !shown {
				continue
			}
			level, err := reader.ownLevel(source, property.ID, nil)
			if err != nil {
				return err
			}
			property.Access = &graphPropertyAccess{Restricted: reader.rules[source][property.ID] != nil, Level: level, EditsProperty: level == propertyAccessEdit,
				VariesByPerson: reader.variesByPerson(source, property.ID)}
		}
		properties = append(properties, property)
	}
	snapshot.Properties = properties
	return nil
}

// markPropertyAccessManagers marks the read's data sources whose property
// access the reader may change (propertyAccessManager).
func markPropertyAccessManagers(app core.App, readerID string, items []graphReadItem) error {
	roles := map[string]bool{}
	for _, item := range items {
		if item.Kind != graphReadKindDataSource {
			continue
		}
		source, _ := item.Value["id"].(string)
		if source == "" {
			continue
		}
		full, err := propertyAccessManager(app, readerID, source, roles)
		if err != nil {
			return err
		}
		if full {
			item.Value["manageAccess"] = true
		}
	}
	return nil
}

// propertyAccessManager says whether a reader may change who reads a data
// source's properties: one of a database they have full access to, other
// than a wiki's — the one rule for its database's read and a row's page read
// alike (2026-11-06: a row opened as a full page had no "속성 사용 권한").
// roles keeps the answers by database page for a read that asks again.
func propertyAccessManager(app core.App, readerID, dataSourceID string, roles map[string]bool) (bool, error) {
	stored := struct {
		PageID string `db:"page_id"`
		Wiki   bool   `db:"wiki"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[page_id]], [[wiki]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": dataSourceID}).One(&stored); err != nil || stored.Wiki {
		return false, nil
	}
	if full, known := roles[stored.PageID]; known {
		return full, nil
	}
	authorization, err := authorizePageAccessRole(app, readerID, stored.PageID, pageAccessRoleFullAccess)
	if err != nil && !isPermissionDenied(err) {
		return false, err
	}
	full := err == nil && authorization.AccessRole >= pageAccessRoleFullAccess
	if roles != nil {
		roles[stored.PageID] = full
	}
	return full, nil
}
