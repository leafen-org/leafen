package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// databaseRowWrites holds the row writes of a change for the steps that
// change a row's values because another change asks it: the write the
// change makes to a row, or one added that keeps the row where it is.
type databaseRowWrites struct {
	app        core.App
	ownerID    string
	rows       map[string]*graphDatabaseRow
	deleted    map[string]struct{}
	operations []graphOperation
}

func newDatabaseRowWrites(app core.App, ownerID string, operations []graphOperation) *databaseRowWrites {
	writes := &databaseRowWrites{
		app: app, ownerID: ownerID, rows: map[string]*graphDatabaseRow{}, deleted: map[string]struct{}{},
		operations: append([]graphOperation{}, operations...),
	}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil {
			writes.rows[operation.Row.ID] = operation.Row
		}
		if operation.Kind == graphOperationKindDeleteRow && operation.RowDeletion != nil {
			writes.deleted[operation.RowDeletion.ID] = struct{}{}
		}
	}
	return writes
}

// row returns the change's write to a row of the data source, adding one
// that keeps the row's place and parent when the change has none. A row the
// change deletes, a row of another source and a missing row have none.
func (w *databaseRowWrites) row(sourceID, rowID string) (*graphDatabaseRow, bool, error) {
	if _, gone := w.deleted[rowID]; gone {
		return nil, false, nil
	}
	if row, written := w.rows[rowID]; written {
		return row, row.DataSourceID == sourceID, nil
	}
	stored := struct {
		Source   string `db:"data_source_id"`
		Position string `db:"position"`
		Parent   string `db:"parent"`
	}{}
	err := w.app.DB().NewQuery(`SELECT [[data_source_id]], [[position]], COALESCE([[subitem_parent_id]], '') AS [[parent]]
		FROM {{leafen_database_rows}} WHERE [[owner]] = {:owner} AND [[id]] = {:id}`).
		Bind(dbx.Params{"owner": w.ownerID, "id": rowID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) || err == nil && stored.Source != sourceID {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	row := &graphDatabaseRow{ID: rowID, DataSourceID: sourceID, Position: stored.Position, Values: map[string]graphRowValue{}}
	if stored.Parent != "" {
		parent := stored.Parent
		row.SubitemParentID = &parent
	}
	w.rows[rowID] = row
	w.operations = append(w.operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: row})
	return row, true, nil
}
