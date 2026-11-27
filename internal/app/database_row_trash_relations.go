package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A relation keeps the rows in trash it names. Reads leave them out, as
// Notion hides trashed pages from relations, and a person's write keeps
// them, so restoring a row brings its relations back. So it does the rows on
// a page deleted for good in its 30 days, and reads leave out the rows
// deleted for good (database_relation_gone_targets.go).

// hideTrashedRelationTargets removes from the relation cells a client reads
// the rows in trash and the rows deleted for good.
func hideTrashedRelationTargets(app core.App, values []storedGraphRowValue) error {
	ids := map[string]struct{}{}
	for _, value := range values {
		if value.Type != graphValueTypeRelation {
			continue
		}
		for _, id := range graphRelationRowIDs(json.RawMessage(value.Value)) {
			ids[id] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	trashed, gone, err := relationTargetsOutOfSight(app, workspaceExportSortedKeys(ids))
	if err != nil || len(trashed)+len(gone) == 0 {
		return err
	}
	for id := range gone {
		trashed[id] = struct{}{}
	}
	for index, value := range values {
		if value.Type != graphValueTypeRelation {
			continue
		}
		kept := []string{}
		for _, id := range graphRelationRowIDs(json.RawMessage(value.Value)) {
			if _, inTrash := trashed[id]; !inTrash {
				kept = append(kept, id)
			}
		}
		encoded, err := json.Marshal(kept)
		if err != nil {
			return err
		}
		values[index].Value = string(encoded)
	}
	return nil
}

// keepTrashedRelationTargets adds back to each written relation cell the rows
// in trash, or on a page deleted for good in its 30 days, that its stored
// cell names, which the writer could not see.
func keepTrashedRelationTargets(app core.App, operations []graphOperation) error {
	rowIDs := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		for _, value := range operation.Row.Values {
			if value.Type == graphValueTypeRelation {
				rowIDs[operation.Row.ID] = struct{}{}
				break
			}
		}
	}
	if len(rowIDs) == 0 {
		return nil
	}
	encodedRows, err := json.Marshal(workspaceExportSortedKeys(rowIDs))
	if err != nil {
		return err
	}
	stored := []storedGraphRowValue{}
	if err := app.DB().NewQuery(`
		SELECT [[row_id]], [[property_id]], [[type]], [[value]] FROM {{leafen_database_row_values}}
		WHERE [[type]] = 'relation' AND [[row_id]] IN (SELECT [[value]] FROM json_each({:rowIds}))
	`).Bind(dbx.Params{"rowIds": string(encodedRows)}).All(&stored); err != nil || len(stored) == 0 {
		return err
	}
	storedCells := map[[2]string][]string{}
	ids := map[string]struct{}{}
	for _, cell := range stored {
		targets := graphRelationRowIDs(json.RawMessage(cell.Value))
		storedCells[[2]string{cell.RowID, cell.PropertyID}] = targets
		for _, id := range targets {
			ids[id] = struct{}{}
		}
	}
	trashed, _, err := relationTargetsOutOfSight(app, workspaceExportSortedKeys(ids))
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		for propertyID, value := range operation.Row.Values {
			targets, found := storedCells[[2]string{operation.Row.ID, propertyID}]
			if !found || value.Type != graphValueTypeRelation {
				continue
			}
			next := []string{}
			if !graphRowValueIsCleared(value.Value) {
				next = graphRelationRowIDs(value.Value)
			}
			present := map[string]struct{}{}
			for _, id := range next {
				present[id] = struct{}{}
			}
			changed := false
			for _, id := range targets {
				_, inTrash := trashed[id]
				if _, kept := present[id]; inTrash && !kept {
					next = append(next, id)
					present[id] = struct{}{}
					changed = true
				}
			}
			if !changed {
				continue
			}
			encoded, err := json.Marshal(next)
			if err != nil {
				return err
			}
			operation.Row.Values[propertyID] = graphRowValue{Type: graphValueTypeRelation, Value: encoded}
		}
	}
	return nil
}
