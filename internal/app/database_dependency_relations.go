package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Dependencies are a relation within one data source, as in Notion: a row's
// Blocked by lists the rows it waits on and its Blocking the rows waiting on
// it. Where a data source's dependency binding names the two, a row's
// Blocked by decides: the dependency links a timeline draws and dates move
// by follow it in the same change; a link drawn or taken away on a timeline
// writes the row's Blocked by along; and a change to a row's Blocking
// becomes the change of the Blocked by of the rows it adds or takes away.
// A row may name itself in either, as Notion's takes (seen 2026-11-04: Np XY
// became its own Blocking and Blocked by); that names no link to draw.

// databaseDependencyEdgeID names the link from a predecessor to a successor
// the same way wherever a Blocked by value makes it.
func databaseDependencyEdgeID(sourceID, predecessorID, successorID string) string {
	digest := sha256.Sum256([]byte("leafen:dependency-edge\x00" + sourceID + "\x00" + predecessorID + "\x00" + successorID))
	return "depends-edge-" + hex.EncodeToString(digest[:12])
}

// normalizeDatabaseDependencies writes each row's Blocked by and the links
// it makes as one.
func normalizeDatabaseDependencies(app core.App, ownerID, pageID string, operations []graphOperation) ([]graphOperation, error) {
	sourceIDs := map[string]struct{}{}
	// A binding the change makes itself counts for the links it draws.
	configured := map[string]databaseDependencyBindingAuthority{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindConfigureDependency && operation.DependencyBinding != nil:
			binding := operation.DependencyBinding
			configured[binding.DataSourceID] = databaseDependencyBindingAuthority{
				DataSourceID: binding.DataSourceID, PredecessorPropertyID: binding.PredecessorPropertyID,
				SuccessorPropertyID: binding.SuccessorPropertyID, DataKey: binding.DataKey,
				Revision: binding.ExpectedRevision + 1,
			}
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil:
			sourceIDs[operation.Row.DataSourceID] = struct{}{}
		case operation.Kind == graphOperationKindUpsertDependencyEdge && operation.DependencyEdge != nil:
			sourceIDs[operation.DependencyEdge.DataSourceID] = struct{}{}
		case operation.Kind == graphOperationKindDeleteDependencyEdge && operation.DependencyEdgeDeletion != nil:
			sourceIDs[operation.DependencyEdgeDeletion.DataSourceID] = struct{}{}
		}
	}
	bindings := map[string]databaseDependencyBindingAuthority{}
	for _, sourceID := range workspaceExportSortedKeys(sourceIDs) {
		binding, bound, err := loadDatabaseDependencyRecord(app, pageID, sourceID)
		if err != nil {
			return nil, err
		}
		if bound {
			bindings[sourceID] = binding
		}
	}
	for sourceID, binding := range configured {
		bindings[sourceID] = binding
	}
	if len(bindings) == 0 {
		return operations, nil
	}
	stored := func(binding databaseDependencyBindingAuthority, column, rowID, other string) (map[string][]string, error) {
		links := []struct {
			ID    string `db:"id"`
			Other string `db:"other"`
		}{}
		err := app.DB().NewQuery(`
			SELECT [[id]], [[` + other + `]] AS [[other]] FROM {{leafen_database_dependency_edges}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:page} AND [[data_source_id]] = {:source}
				AND [[data_key]] = {:key} AND [[` + column + `]] = {:row}
			ORDER BY [[created]], [[id]]
		`).Bind(dbx.Params{
			"owner": ownerID, "page": pageID, "source": binding.DataSourceID, "key": binding.DataKey, "row": rowID,
		}).All(&links)
		byRow := map[string][]string{}
		for _, link := range links {
			byRow[link.Other] = append(byRow[link.Other], link.ID)
		}
		return byRow, err
	}
	// A row naming itself draws no link, so its kept Blocked by tells it.
	namesItself := func(binding databaseDependencyBindingAuthority, rowID string) (bool, error) {
		kept := ""
		err := app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_database_row_values}}
			WHERE [[owner]] = {:owner} AND [[row_id]] = {:row} AND [[property_id]] = {:property}`).
			Bind(dbx.Params{"owner": ownerID, "row": rowID, "property": binding.PredecessorPropertyID}).Row(&kept)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		ids := []string{}
		if err != nil || json.Unmarshal([]byte(kept), &ids) != nil {
			return false, err
		}
		return slices.Contains(ids, rowID), nil
	}
	predecessorsOf := func(binding databaseDependencyBindingAuthority, rowID string) (map[string][]string, error) {
		return stored(binding, "successor_row_id", rowID, "predecessor_row_id")
	}
	blockedBy := func(binding databaseDependencyBindingAuthority, row *graphDatabaseRow) ([]string, error) {
		if value, written := row.Values[binding.PredecessorPropertyID]; written {
			ids := []string{}
			if !graphRowValueIsCleared(value.Value) && json.Unmarshal(value.Value, &ids) != nil {
				return nil, graphInputError(errorCodeGraphInvalid, "Check the page's Blocked by.")
			}
			return ids, nil
		}
		links, err := predecessorsOf(binding, row.ID)
		return workspaceExportSortedKeys(links), err
	}
	setBlockedBy := func(binding databaseDependencyBindingAuthority, row *graphDatabaseRow, ids []string) error {
		encoded, err := json.Marshal(ids)
		if err != nil {
			return err
		}
		if row.Values == nil {
			row.Values = map[string]graphRowValue{}
		}
		row.Values[binding.PredecessorPropertyID] = graphRowValue{Type: graphValueTypeRelation, Value: encoded}
		return nil
	}
	writes := newDatabaseRowWrites(app, ownerID, operations)
	// Links a change draws or takes away itself stay as it writes them.
	drawn := map[[3]string]struct{}{}
	relink := func(binding databaseDependencyBindingAuthority, successorID, predecessorID string, add bool) error {
		row, found, err := writes.row(binding.DataSourceID, successorID)
		if err != nil || !found {
			return err
		}
		ids, err := blockedBy(binding, row)
		if err != nil {
			return err
		}
		next := slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == predecessorID })
		if add {
			next = append(next, predecessorID)
		}
		return setBlockedBy(binding, row, next)
	}

	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertDependencyEdge && operation.DependencyEdge != nil:
			edge := operation.DependencyEdge
			binding, bound := bindings[edge.DataSourceID]
			if !bound {
				continue
			}
			if err := relink(binding, edge.SuccessorRowID, edge.PredecessorRowID, true); err != nil {
				return nil, err
			}
			drawn[[3]string{edge.DataSourceID, edge.PredecessorRowID, edge.SuccessorRowID}] = struct{}{}
		case operation.Kind == graphOperationKindDeleteDependencyEdge && operation.DependencyEdgeDeletion != nil:
			deletion := operation.DependencyEdgeDeletion
			binding, bound := bindings[deletion.DataSourceID]
			if !bound {
				continue
			}
			link := struct {
				Predecessor string `db:"predecessor_row_id"`
				Successor   string `db:"successor_row_id"`
			}{}
			if err := app.DB().NewQuery(`SELECT [[predecessor_row_id]], [[successor_row_id]] FROM {{leafen_database_dependency_edges}}
				WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[data_source_id]] = {:source}`).
				Bind(dbx.Params{"id": deletion.ID, "owner": ownerID, "source": deletion.DataSourceID}).One(&link); err != nil {
				continue
			}
			if err := relink(binding, link.Successor, link.Predecessor, false); err != nil {
				return nil, err
			}
			drawn[[3]string{deletion.DataSourceID, link.Predecessor, link.Successor}] = struct{}{}
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil:
			// Blocking names the rows to block or to let go.
			row := operation.Row
			binding, bound := bindings[row.DataSourceID]
			value, written := row.Values[binding.SuccessorPropertyID]
			if !bound || !written {
				continue
			}
			next := []string{}
			if !graphRowValueIsCleared(value.Value) && json.Unmarshal(value.Value, &next) != nil {
				return nil, graphInputError(errorCodeGraphInvalid, "Check the page's Blocking.")
			}
			current, err := stored(binding, "predecessor_row_id", row.ID, "successor_row_id")
			if err != nil {
				return nil, err
			}
			if self, err := namesItself(binding, row.ID); err != nil {
				return nil, err
			} else if self {
				current[row.ID] = nil
			}
			add, remove := databaseRelationPairDiff(workspaceExportSortedKeys(current), next)
			for _, successorID := range add {
				if err := relink(binding, successorID, row.ID, true); err != nil {
					return nil, err
				}
			}
			for _, successorID := range remove {
				if err := relink(binding, successorID, row.ID, false); err != nil {
					return nil, err
				}
			}
			// The rows blocked relate back to this row in the same change.
			delete(row.Values, binding.SuccessorPropertyID)
		}
	}

	// Each row's Blocked by makes its links.
	links := []graphOperation{}
	for _, operation := range writes.operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		row := operation.Row
		binding, bound := bindings[row.DataSourceID]
		if _, written := row.Values[binding.PredecessorPropertyID]; !bound || !written {
			continue
		}
		next, err := blockedBy(binding, row)
		if err != nil {
			return nil, err
		}
		current, err := predecessorsOf(binding, row.ID)
		if err != nil {
			return nil, err
		}
		add, remove := databaseRelationPairDiff(workspaceExportSortedKeys(current), next)
		for _, predecessorID := range add {
			if _, explicit := drawn[[3]string{row.DataSourceID, predecessorID, row.ID}]; explicit || predecessorID == row.ID {
				continue
			}
			links = append(links, graphOperation{Kind: graphOperationKindUpsertDependencyEdge, DependencyEdge: &graphDependencyEdge{
				ID: databaseDependencyEdgeID(row.DataSourceID, predecessorID, row.ID), DataSourceID: row.DataSourceID,
				PredecessorRowID: predecessorID, SuccessorRowID: row.ID, ExpectedRevision: binding.Revision,
			}})
		}
		for _, predecessorID := range remove {
			if _, explicit := drawn[[3]string{row.DataSourceID, predecessorID, row.ID}]; explicit {
				continue
			}
			for _, edgeID := range current[predecessorID] {
				links = append(links, graphOperation{Kind: graphOperationKindDeleteDependencyEdge, DependencyEdgeDeletion: &graphDependencyEdgeDelete{
					ID: edgeID, DataSourceID: row.DataSourceID, ExpectedRevision: binding.Revision,
				}})
			}
		}
	}
	return append(writes.operations, links...), nil
}
