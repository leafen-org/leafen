package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Duplicating a database item duplicates its sub-items too, as in Notion,
// those the person may open.
// Every copy gets an ID derived from its source and the request, so a
// retried request makes the same copies. The copies keep their tree under
// the new item, relations between items of the tree point at the copies,
// and values the database numbers or computes itself are not copied.

// A tree of any size is copied a change at a time, parents before their
// sub-items, each change holding at most maxGraphMutationOperations rows.

// databaseRowDuplicateUncopiedTypes are numbered or computed per item.
var databaseRowDuplicateUncopiedTypes = map[string]struct{}{
	graphValueTypeUniqueID: {}, graphValueTypeCreatedTime: {}, graphValueTypeLastEditedTime: {},
	graphValueTypeCreatedBy: {}, graphValueTypeLastEditedBy: {}, graphValueTypeLastVisitedTime: {},
}

type databaseRowDuplicate struct {
	SourceID string
	TargetID string
}

// databaseRowDuplicateTree is the item's tree to copy, out of trash, with
// each row's copy.
type databaseRowDuplicateTree struct {
	Rows    []databaseRowTreeRow
	Targets map[string]string
}

func readDatabaseRowDuplicateTree(
	app core.App, authority databaseRowPageAuthority, principalID, requestID string,
) (databaseRowDuplicateTree, error) {
	rows, err := databaseRowTreeRead(app, authority, true)
	if err == nil {
		// Its sub-items the person can't open stay behind (page_duplication_guest.go).
		rows, err = readableRowDuplicateTree(app, principalID, authority.WorkspaceID, rows)
	}
	if err != nil {
		return databaseRowDuplicateTree{}, err
	}
	targets := make(map[string]string, len(rows))
	for _, row := range rows {
		targets[row.ID] = databaseRowPageDuplicateRowID(principalID, row.ID, requestID)
	}
	return databaseRowDuplicateTree{Rows: rows, Targets: targets}, nil
}

// databaseRowDuplicateChangeBudget is about as much of one change's bytes
// as a batch of copies takes, leaving room for the rest of the change.
const databaseRowDuplicateChangeBudget = maxGraphChangeBytes / 2

// databaseRowDuplicateBatch is the rows [From, To) of the tree one change copies.
type databaseRowDuplicateBatch struct {
	From, To int
}

// partition splits the tree into changes of at most maxGraphMutationOperations
// rows and about databaseRowDuplicateChangeBudget bytes of values; a row is
// never split, so one row past a change's size is refused by its change.
func (tree databaseRowDuplicateTree) partition(app core.App, owner, pageID string) ([]databaseRowDuplicateBatch, error) {
	sizes := map[string]int{}
	for start := 0; start < len(tree.Rows); start += maxGraphMutationOperations {
		window := tree.Rows[start:min(start+maxGraphMutationOperations, len(tree.Rows))]
		ids := make([]string, len(window))
		for index, row := range window {
			ids[index] = row.ID
		}
		encoded, err := json.Marshal(ids)
		if err != nil {
			return nil, err
		}
		measured := []struct {
			RowID string `db:"row_id"`
			Size  int    `db:"size"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[row_id]], SUM(LENGTH([[value]]) + LENGTH([[spans]])) AS [[size]]
			FROM {{leafen_database_row_values}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
				AND [[row_id]] IN (SELECT [[value]] FROM json_each({:ids}))
			GROUP BY [[row_id]]
		`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "ids": string(encoded)}).All(&measured); err != nil {
			return nil, err
		}
		for _, row := range measured {
			sizes[row.RowID] = row.Size
		}
	}
	batches := []databaseRowDuplicateBatch{}
	from, bytes := 0, 0
	for index, row := range tree.Rows {
		size := sizes[row.ID] + 256
		if index > from && (index-from == maxGraphMutationOperations || bytes+size > databaseRowDuplicateChangeBudget) {
			batches = append(batches, databaseRowDuplicateBatch{From: from, To: index})
			from, bytes = index, 0
		}
		bytes += size
	}
	return append(batches, databaseRowDuplicateBatch{From: from, To: len(tree.Rows)}), nil
}

// planDatabaseRowDuplicate writes one change's rows of the tree as new rows;
// the item copied is the first row of the first change. The item's copy goes
// right after it (databaseRowCopyPlace), or at the position the request
// gives: copies of several rows go together after the last, as Notion's
// (seen 2026-11-08). Copies of its sub-items each go right after theirs,
// under its copy. The web client draws the copies so at the press, before
// they come (web/src/lib/rowCopiesHere.ts).
func planDatabaseRowDuplicate(
	app core.App, authority databaseRowPageAuthority, principalID string,
	tree databaseRowDuplicateTree, batch databaseRowDuplicateBatch, position string,
) ([]graphOperation, []databaseRowDuplicate, error) {
	if batch.From >= batch.To || batch.To > len(tree.Rows) {
		return nil, nil, errDatabaseRowPageConflict
	}
	sources := tree.Rows[batch.From:batch.To]
	targets := tree.Targets
	ids := make([]string, len(sources))
	for index, source := range sources {
		ids[index] = source.ID
	}
	stored, err := loadGraphRowValues(app, authority.OwnerID, authority.ContainerPageID, ids)
	if err != nil {
		return nil, nil, err
	}
	types, err := databaseRowDuplicatePropertyTypes(app, authority)
	if err != nil {
		return nil, nil, err
	}
	values := make(map[string]map[string]graphRowValue, len(sources))
	for _, value := range stored {
		if _, uncopied := databaseRowDuplicateUncopiedTypes[value.Type]; uncopied {
			continue
		}
		// A value is copied as its property reads it now: one kept from a
		// type it had before is the property's type when the two store
		// alike (a text that became a link), and stays behind otherwise.
		propertyType, known := types[value.PropertyID]
		if !known || !graphValueTypesShareRepresentation(value.Type, propertyType) {
			continue
		}
		raw := json.RawMessage(value.Value)
		if !json.Valid(raw) {
			return nil, nil, errDatabaseRowPageConflict
		}
		if value.Type == graphValueTypeRelation {
			raw = remapDatabaseRowDuplicateRelation(raw, targets)
		}
		if values[value.RowID] == nil {
			values[value.RowID] = map[string]graphRowValue{}
		}
		copied := graphRowValue{Type: propertyType, Value: raw}
		if propertyType == graphValueTypeTitle || propertyType == graphValueTypeRichText {
			copied.Spans = databaseStoredSpans(value.Spans)
		}
		// The item copied and its sub-items are each named as a copy, as
		// Notion names them ("Child" → "Child (1)", seen 2026-11-03).
		if value.Type == graphValueTypeTitle {
			copied = duplicateCopyTitleValue(copied)
		}
		values[value.RowID][value.PropertyID] = copied
	}
	if err := withoutUneditableValues(app, principalID, authority.DataSourceID, values); err != nil {
		return nil, nil, err
	}
	operations := make([]graphOperation, 0, len(sources))
	duplicates := make([]databaseRowDuplicate, 0, len(sources))
	// Rows sharing a place share their copies' place, read once.
	places := map[string]string{}
	for _, source := range sources {
		target := targets[source.ID]
		parent := source.ParentID
		if source.Depth > 0 {
			parent = targets[source.ParentID]
		}
		rowValues := values[source.ID]
		if rowValues == nil {
			rowValues = map[string]graphRowValue{}
		}
		place := position
		if source.Depth > 0 || position == "" {
			found := false
			if place, found = places[source.Position]; !found {
				if place, err = databaseRowCopyPlace(app, authority, source.Position); err != nil {
					return nil, nil, err
				}
				places[source.Position] = place
			}
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
			ID: target, DataSourceID: authority.DataSourceID, SubitemParentID: optionalDatabaseRowParent(parent),
			Position: place, Values: rowValues,
		}})
		duplicates = append(duplicates, databaseRowDuplicate{SourceID: source.ID, TargetID: target})
	}
	return operations, duplicates, nil
}

// databaseRowDuplicatePropertyTypes are the types of the item's database's
// properties.
func databaseRowDuplicatePropertyTypes(app core.App, authority databaseRowPageAuthority) (map[string]string, error) {
	properties := []struct {
		ID   string `db:"id"`
		Type string `db:"type"`
	}{}
	// The properties are found by their page and source; the owner is read
	// after (+), or SQLite, which keeps no statistics, reads the owner's
	// every property.
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[type]] FROM {{leafen_database_properties}}
		WHERE +[[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
	`).Bind(dbx.Params{
		"owner": authority.OwnerID, "pageId": authority.ContainerPageID, "dataSourceId": authority.DataSourceID,
	}).All(&properties); err != nil {
		return nil, err
	}
	types := make(map[string]string, len(properties))
	for _, property := range properties {
		types[property.ID] = property.Type
	}
	return types, nil
}

// remapDatabaseRowDuplicateRelation points relations to items of the
// duplicated tree at their copies.
func remapDatabaseRowDuplicateRelation(raw json.RawMessage, targets map[string]string) json.RawMessage {
	related := []string{}
	if json.Unmarshal(raw, &related) != nil {
		return raw
	}
	changed := false
	for index, id := range related {
		if target, found := targets[id]; found {
			related[index] = target
			changed = true
		}
	}
	if !changed {
		return raw
	}
	encoded, err := json.Marshal(related)
	if err != nil {
		return raw
	}
	return encoded
}
