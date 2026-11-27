package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Sub-items are a relation within one data source, as in Notion: a row's
// Parent item names the row it sits under and its Sub-items the rows under
// it. A data source's sub-item settings name the two properties. The row's
// Parent item decides; the sub-item parent that views nest by and a wiki's
// pages sit under follows it in the same change, whichever of the two a
// change writes, and a change to a row's Sub-items moves the rows it adds or
// takes away. A row has one parent and is never under itself.

var (
	errDatabaseSubitemParents = graphInputError(errorCodeGraphReferenceInvalid, "A page has one parent item.")
	errDatabaseSubitemCycle   = graphInputError(errorCodeGraphReferenceInvalid, "A page can't be a sub-item of its own sub-item.")
)

// databaseSubitemPair names a data source's Parent item and Sub-items.
type databaseSubitemPair struct {
	Parent   string
	Children string
}

// loadDatabaseSubitemPairs reads the sub-item relation pairs of the data
// sources whose two properties are there, as relations to the source. A
// wiki's Parent page and Sub-pages are none: its pages sit where they are in
// the wiki by their sub-item parent alone, which its pair leaves as it is
// (wiki_subpage_pair.go).
func loadDatabaseSubitemPairs(app core.App, sourceIDs []string) (map[string]databaseSubitemPair, error) {
	pairs := map[string]databaseSubitemPair{}
	if len(sourceIDs) == 0 {
		return pairs, nil
	}
	encoded, err := json.Marshal(sourceIDs)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		Source   string `db:"data_source_id"`
		Parent   string `db:"parent"`
		Children string `db:"children"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[features]].[[data_source_id]],
			json_extract([[features]].[[subitems_json]], '$.parentPropertyId') AS [[parent]],
			json_extract([[features]].[[subitems_json]], '$.subitemsPropertyId') AS [[children]]
		FROM {{leafen_database_task_features}} AS [[features]]
		WHERE [[features]].[[data_source_id]] IN (SELECT [[value]] FROM json_each({:sources}))
			AND NOT EXISTS (SELECT 1 FROM {{leafen_data_sources}} AS [[wiki_source]]
				WHERE [[wiki_source]].[[id]] = [[features]].[[data_source_id]] AND [[wiki_source]].[[wiki]] = 1)
			AND json_type([[features]].[[subitems_json]], '$.parentPropertyId') = 'text'
			AND json_type([[features]].[[subitems_json]], '$.subitemsPropertyId') = 'text'
			AND (SELECT COUNT(*) FROM {{leafen_database_properties}} AS [[property]]
				WHERE [[property]].[[data_source_id]] = [[features]].[[data_source_id]] AND [[property]].[[type]] = 'relation'
					AND [[property]].[[id]] IN (json_extract([[features]].[[subitems_json]], '$.parentPropertyId'),
						json_extract([[features]].[[subitems_json]], '$.subitemsPropertyId'))) = 2
	`).Bind(dbx.Params{"sources": string(encoded)}).All(&rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.Parent != row.Children {
			pairs[row.Source] = databaseSubitemPair{Parent: row.Parent, Children: row.Children}
		}
	}
	return pairs, nil
}

// normalizeDatabaseSubitemParents writes each row's Parent item and sub-item
// parent as one: a change to a row's Sub-items becomes the move of the rows
// it adds or takes away, and a row whose change names either takes the
// other along.
func normalizeDatabaseSubitemParents(app core.App, ownerID string, operations []graphOperation) ([]graphOperation, error) {
	sourceIDs := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil {
			sourceIDs[operation.Row.DataSourceID] = struct{}{}
		}
	}
	pairs, err := loadDatabaseSubitemPairs(app, workspaceExportSortedKeys(sourceIDs))
	if err != nil {
		return operations, err
	}
	if len(pairs) == 0 {
		// Rows nested by their sub-item parent alone stay out of a loop.
		parents := map[string]string{}
		for _, operation := range operations {
			if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil && operation.Row.SubitemParentID != nil {
				parents[operation.Row.ID] = *operation.Row.SubitemParentID
			}
		}
		for rowID, parentID := range parents {
			if err := refuseDatabaseSubitemCycle(app, ownerID, rowID, parentID, parents); err != nil {
				return nil, err
			}
		}
		return operations, nil
	}
	// A row the change deletes or turns into a page leaves where it is.
	writes := newDatabaseRowWrites(app, ownerID, operations)
	moving := func(sourceID, rowID string) (*graphDatabaseRow, error) {
		row, found, err := writes.row(sourceID, rowID)
		if err == nil && !found {
			err = errDatabaseSubitemParents
		}
		return row, err
	}
	setParent := func(row *graphDatabaseRow, pair databaseSubitemPair, parentID string) {
		value := json.RawMessage(`[]`)
		row.SubitemParentID = nil
		if parentID != "" {
			parent := parentID
			row.SubitemParentID = &parent
			value, _ = json.Marshal([]string{parentID})
		}
		if row.Values == nil {
			row.Values = map[string]graphRowValue{}
		}
		row.Values[pair.Parent] = graphRowValue{Type: graphValueTypeRelation, Value: value}
	}

	// Sub-items name the rows to move under a row or out from under it.
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		row := operation.Row
		pair, paired := pairs[row.DataSourceID]
		value, written := row.Values[pair.Children]
		if !paired || !written {
			continue
		}
		next := []string{}
		if !graphRowValueIsCleared(value.Value) && json.Unmarshal(value.Value, &next) != nil {
			return nil, errDatabaseSubitemParents
		}
		current := []string{}
		if err := app.DB().NewQuery(`SELECT [[row]].[[id]] FROM {{leafen_database_rows}} AS [[row]]
			WHERE ` + databaseSourceRowsSQL("[[row]]", "{:source}") + ` AND [[row]].[[subitem_parent_id]] = {:parent}`).
			Bind(dbx.Params{"source": row.DataSourceID, "parent": row.ID}).Column(&current); err != nil {
			return nil, err
		}
		add, remove := databaseRelationPairDiff(current, next)
		for _, childID := range add {
			if _, gone := writes.deleted[childID]; gone {
				continue
			}
			child, err := moving(row.DataSourceID, childID)
			if err != nil {
				return nil, err
			}
			setParent(child, pair, row.ID)
		}
		for _, childID := range remove {
			if _, gone := writes.deleted[childID]; gone {
				continue
			}
			child, err := moving(row.DataSourceID, childID)
			if err != nil {
				return nil, err
			}
			if child.SubitemParentID != nil && *child.SubitemParentID == row.ID {
				setParent(child, pair, "")
			}
		}
		// The rows moved relate back to this row in the same change.
		delete(row.Values, pair.Children)
	}

	// Each row's Parent item and sub-item parent say the same, where the
	// Parent item names a row the row can sit under.
	parents := map[string]string{}
	named := []databaseSubitemParentWrite{}
	for _, operation := range writes.operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		row := operation.Row
		pair, paired := pairs[row.DataSourceID]
		if !paired {
			// A row nested by its sub-item parent alone is kept out of a loop
			// the same way.
			if row.SubitemParentID != nil {
				parents[row.ID] = *row.SubitemParentID
			}
			continue
		}
		write := databaseSubitemParentWrite{row: row, pair: pair}
		if value, written := row.Values[pair.Parent]; written {
			targets := []string{}
			if !graphRowValueIsCleared(value.Value) && json.Unmarshal(value.Value, &targets) != nil || len(targets) > 1 {
				return nil, errDatabaseSubitemParents
			}
			if len(targets) == 1 {
				write.parent = targets[0]
			}
			write.fromValue = true
		} else if row.SubitemParentID != nil {
			write.parent = *row.SubitemParentID
		}
		named = append(named, write)
	}
	canNest, err := databaseSubitemNestable(app, ownerID, writes, named)
	if err != nil {
		return nil, err
	}
	for _, write := range named {
		row, parentID := write.row, write.parent
		parents[row.ID] = ""
		switch {
		case parentID != "" && canNest(row, parentID):
			setParent(row, write.pair, parentID)
			parents[row.ID] = parentID
		case parentID != "":
			// A Parent item naming the row itself or a row in the trash stays as
			// it is, the row at the top: the relation is the person's, and comes
			// back with the row it names.
			row.SubitemParentID = nil
		case write.fromValue || write.stored != "" && canNest(row, write.stored):
			// Out from under its parent: the Parent item goes with it. One that
			// names no row it could sit under is left as it is.
			setParent(row, write.pair, "")
		}
	}
	for rowID, parentID := range parents {
		if err := refuseDatabaseSubitemCycle(app, ownerID, rowID, parentID, parents); err != nil {
			return nil, err
		}
	}
	return writes.operations, nil
}

// refuseDatabaseSubitemCycle keeps a row from sitting under a row that sits
// under it, as the change leaves the rows' parents: the parents are followed
// to the top however deep, and one met twice (a loop above) is refused too.
func refuseDatabaseSubitemCycle(app core.App, ownerID, rowID, parentID string, changed map[string]string) error {
	seen := map[string]bool{}
	for parentID != "" {
		if parentID == rowID || seen[parentID] {
			return errDatabaseSubitemCycle
		}
		seen[parentID] = true
		if next, written := changed[parentID]; written {
			parentID = next
			continue
		}
		next := ""
		err := app.DB().NewQuery(`SELECT COALESCE([[subitem_parent_id]], '') FROM {{leafen_database_rows}}
			WHERE [[owner]] = {:owner} AND [[id]] = {:id}`).Bind(dbx.Params{"owner": ownerID, "id": parentID}).Row(&next)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		parentID = next
	}
	return nil
}

// databaseSubitemParentWrite is a row of the change in a source with
// sub-items: the parent its Parent item, or else its sub-item parent, names;
// and, where the change leaves its Parent item, the row that one names now.
type databaseSubitemParentWrite struct {
	row       *graphDatabaseRow
	pair      databaseSubitemPair
	parent    string
	fromValue bool
	stored    string
}

// databaseSubitemNestable reads what tells whether a row of the change can
// sit under a row: one of its own data source, not itself, written by the
// change or stored, and out of the trash unless the row is in the trash with
// it (a sub-tree in the trash keeps its shape). It reads too the Parent item
// each row the change leaves it on names.
func databaseSubitemNestable(app core.App, ownerID string, writes *databaseRowWrites, named []databaseSubitemParentWrite) (func(*graphDatabaseRow, string) bool, error) {
	ids := map[string]struct{}{}
	keeping := map[string][]string{}
	for _, write := range named {
		ids[write.row.ID] = struct{}{}
		if write.parent != "" {
			ids[write.parent] = struct{}{}
		} else if !write.fromValue {
			keeping[write.pair.Parent] = append(keeping[write.pair.Parent], write.row.ID)
		}
	}
	kept := map[string]string{}
	for _, propertyID := range workspaceExportSortedKeys(keeping) {
		encoded, err := json.Marshal(keeping[propertyID])
		if err != nil {
			return nil, err
		}
		values := []struct {
			Row   string `db:"row_id"`
			Value string `db:"value"`
		}{}
		// The cells are found by row and property; the owner is read after
		// (+), or SQLite, which keeps no statistics, reads the owner's every
		// cell.
		if err := app.DB().NewQuery(`SELECT [[row_id]], [[value]] FROM {{leafen_database_row_values}}
			WHERE +[[owner]] = {:owner} AND [[property_id]] = {:property} AND [[row_id]] IN (SELECT [[value]] FROM json_each({:rows}))`).
			Bind(dbx.Params{"owner": ownerID, "property": propertyID, "rows": string(encoded)}).All(&values); err != nil {
			return nil, err
		}
		for _, value := range values {
			targets := []string{}
			if json.Unmarshal([]byte(value.Value), &targets) == nil && len(targets) == 1 {
				kept[value.Row] = targets[0]
				ids[targets[0]] = struct{}{}
			}
		}
	}
	for index := range named {
		named[index].stored = kept[named[index].row.ID]
	}
	type state struct {
		source  string
		trashed bool
	}
	known := map[string]state{}
	if len(ids) > 0 {
		encoded, err := json.Marshal(workspaceExportSortedKeys(ids))
		if err != nil {
			return nil, err
		}
		stored := []struct {
			ID      string `db:"id"`
			Source  string `db:"data_source_id"`
			Trashed bool   `db:"trashed"`
		}{}
		// The rows are found by id, the owner read after (+), as above.
		if err := app.DB().NewQuery(`SELECT [[row]].[[id]], [[row]].[[data_source_id]],
				EXISTS (SELECT 1 FROM {{leafen_database_row_trash}} WHERE [[row_id]] = [[row]].[[id]]) AS [[trashed]]
			FROM {{leafen_database_rows}} AS [[row]]
			WHERE +[[row]].[[owner]] = {:owner} AND [[row]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))`).
			Bind(dbx.Params{"owner": ownerID, "ids": string(encoded)}).All(&stored); err != nil {
			return nil, err
		}
		for _, row := range stored {
			known[row.ID] = state{row.Source, row.Trashed}
		}
	}
	for _, row := range writes.rows {
		if _, gone := writes.deleted[row.ID]; !gone {
			known[row.ID] = state{row.DataSourceID, known[row.ID].trashed}
		}
	}
	return func(row *graphDatabaseRow, parentID string) bool {
		parent, found := known[parentID]
		return parentID != row.ID && found && parent.source == row.DataSourceID && (!parent.trashed || known[row.ID].trashed)
	}, nil
}
