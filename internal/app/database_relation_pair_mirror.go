package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A two-way relation within one data source changes both sides in the change
// that makes it, as in Notion: relating a row to another relates the other
// back, and unrelating it unrelates it back, so no one reads one side
// without the other. A relation to another data source keeps the runner,
// which changes the other page once this change commits. The other side
// changes even where the writer may not change that row themselves — a page
// they only view, in this database or another — and says nothing of it, as
// Notion's does (seen 2026-11-09 with a guest: plain two-way relations,
// dependencies and sub-items alike): what the writer may change is asked of
// the row they edit alone.

// mirrorSameSourceRelationPairs adds to the operations the other side of
// each two-way relation they change within one data source.
func mirrorSameSourceRelationPairs(app core.App, ownerID string, operations []graphOperation) ([]graphOperation, error) {
	propertyIDs := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		for propertyID, value := range operation.Row.Values {
			if value.Type == graphValueTypeRelation {
				propertyIDs[propertyID] = struct{}{}
			}
		}
	}
	if len(propertyIDs) == 0 {
		return operations, nil
	}
	properties, err := loadDatabaseRelationPairProperties(app, ownerID, workspaceExportSortedKeys(propertyIDs))
	if err != nil {
		return nil, err
	}
	// A property the change writes is paired as the change writes it.
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertProperty || operation.Property == nil {
			continue
		}
		config := map[string]any{}
		if len(operation.Property.Config) > 0 && json.Unmarshal(operation.Property.Config, &config) != nil {
			config = map[string]any{}
		}
		properties[operation.Property.ID] = databaseRelationPairProperty{
			ID: operation.Property.ID, DataSourceID: operation.Property.DataSourceID,
			Type: operation.Property.Type, Config: config,
		}
	}

	// changes[pair property][row on the other side] holds the rows to add to
	// and remove from that row's value.
	changes := map[string]map[string][2][]string{}
	sources := map[string]string{}
	// origins[pair property] is the property whose change it mirrors.
	origins := map[string]string{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		row := operation.Row
		for _, propertyID := range workspaceExportSortedKeys(row.Values) {
			property, known := properties[propertyID]
			if !known || property.DataSourceID != row.DataSourceID {
				continue
			}
			pair := databaseRelationPairOf(propertyID, property.DataSourceID, property.Type, property.Config)
			if pair == nil || pair.TargetDataSourceID != property.DataSourceID {
				continue
			}
			next := []string{}
			if value := row.Values[propertyID].Value; !graphRowValueIsCleared(value) && json.Unmarshal(value, &next) != nil {
				continue
			}
			previous, err := loadDatabaseRelationPairTargets(app, ownerID, row.ID, propertyID)
			if err != nil {
				return nil, err
			}
			add, remove := databaseRelationPairDiff(previous, next)
			if len(add) == 0 && len(remove) == 0 {
				continue
			}
			byTarget := changes[pair.PairPropertyID]
			if byTarget == nil {
				byTarget = map[string][2][]string{}
				changes[pair.PairPropertyID] = byTarget
			}
			for _, targetID := range add {
				entry := byTarget[targetID]
				entry[0] = append(entry[0], row.ID)
				byTarget[targetID] = entry
			}
			for _, targetID := range remove {
				entry := byTarget[targetID]
				entry[1] = append(entry[1], row.ID)
				byTarget[targetID] = entry
			}
			sources[pair.PairPropertyID] = property.DataSourceID
			origins[pair.PairPropertyID] = propertyID
		}
	}
	if len(changes) == 0 {
		return operations, nil
	}

	// A row the change already writes takes its other side in that write;
	// a row it deletes, or moves to another data source, has none.
	written := map[string]*graphDatabaseRow{}
	deleted := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil {
			written[operation.Row.ID] = operation.Row
		}
		if operation.Kind == graphOperationKindDeleteRow && operation.RowDeletion != nil {
			deleted[operation.RowDeletion.ID] = struct{}{}
		}
	}
	mirrored := append([]graphOperation{}, operations...)
	// apply writes the rows to add to and remove from each row's value of
	// propertyID, kept to its limit (keepDatabaseRelationLimit) when taken
	// is given, which then names the pages each row let go.
	apply := func(propertyID, source string, byTarget map[string][2][]string, taken map[string][]string) error {
		if _, known := properties[propertyID]; !known && taken != nil {
			loaded, err := loadDatabaseRelationPairProperties(app, ownerID, []string{propertyID})
			if err != nil {
				return err
			}
			properties[propertyID] = loaded[propertyID]
		}
		stored := map[string][2][]string{}
		for _, targetID := range workspaceExportSortedKeys(byTarget) {
			if _, gone := deleted[targetID]; gone {
				continue
			}
			change := byTarget[targetID]
			row, inChange := written[targetID]
			if !inChange {
				stored[targetID] = change
				continue
			}
			if row.DataSourceID != source {
				continue
			}
			current := []string{}
			if value, set := row.Values[propertyID]; set {
				if !graphRowValueIsCleared(value.Value) && json.Unmarshal(value.Value, &current) != nil {
					continue
				}
			} else if loaded, err := loadDatabaseRelationPairTargets(app, ownerID, targetID, propertyID); err != nil {
				return err
			} else {
				current = loaded
			}
			next := mergeDatabaseRelationPairTargets(current, change)
			if taken != nil {
				var letGo []string
				if next, letGo = keepDatabaseRelationLimit(current, next, databaseRelationLimit(properties[propertyID].Config)); len(letGo) > 0 {
					taken[targetID] = append(taken[targetID], letGo...)
				}
			}
			if add, remove := databaseRelationPairDiff(current, next); len(add) == 0 && len(remove) == 0 {
				continue
			}
			encoded, err := json.Marshal(next)
			if err != nil {
				return err
			}
			if row.Values == nil {
				row.Values = map[string]graphRowValue{}
			}
			row.Values[propertyID] = graphRowValue{Type: graphValueTypeRelation, Value: encoded}
		}
		if len(stored) == 0 {
			return nil
		}
		others, err := databaseRelationPairRowOperationsTaking(app, ownerID, databaseRelationPairPayload{
			SourceDataSourceID: source, TargetDataSourceID: source, PairPropertyID: propertyID,
		}, stored, taken)
		if err != nil {
			return err
		}
		// A row both sides of a pair relate back to takes the other side in
		// the same write (one change writes a row once).
		for _, other := range others {
			if other.Kind == graphOperationKindUpsertRow && other.Row != nil {
				written[other.Row.ID] = other.Row
			}
		}
		mirrored = append(mirrored, others...)
		return nil
	}
	for _, pairPropertyID := range workspaceExportSortedKeys(changes) {
		taken := map[string][]string{}
		if err := apply(pairPropertyID, sources[pairPropertyID], changes[pairPropertyID], taken); err != nil {
			return nil, err
		}
		if len(taken) == 0 {
			continue
		}
		// The pages a side kept to one page let go leave it on their own side.
		letGo := map[string][2][]string{}
		for _, rowID := range workspaceExportSortedKeys(taken) {
			for _, pageID := range taken[rowID] {
				entry := letGo[pageID]
				entry[1] = append(entry[1], rowID)
				letGo[pageID] = entry
			}
		}
		if err := apply(origins[pairPropertyID], sources[pairPropertyID], letGo, nil); err != nil {
			return nil, err
		}
	}
	return mirrored, nil
}

// graphOperationOwnRows names the rows a change names itself: those it
// writes or deletes, and the two ends of each dependency link it draws or
// takes away.
func graphOperationOwnRows(app core.App, ownerID string, operations []graphOperation) (map[string]struct{}, error) {
	own := map[string]struct{}{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil:
			own[operation.Row.ID] = struct{}{}
		case operation.Kind == graphOperationKindDeleteRow && operation.RowDeletion != nil:
			own[operation.RowDeletion.ID] = struct{}{}
		case operation.Kind == graphOperationKindUpsertDependencyEdge && operation.DependencyEdge != nil:
			own[operation.DependencyEdge.PredecessorRowID] = struct{}{}
			own[operation.DependencyEdge.SuccessorRowID] = struct{}{}
		case operation.Kind == graphOperationKindDeleteDependencyEdge && operation.DependencyEdgeDeletion != nil:
			ends := struct {
				Predecessor string `db:"predecessor_row_id"`
				Successor   string `db:"successor_row_id"`
			}{}
			err := app.DB().NewQuery(`SELECT [[predecessor_row_id]], [[successor_row_id]] FROM {{leafen_database_dependency_edges}}
				WHERE [[id]] = {:id} AND [[owner]] = {:owner}`).
				Bind(dbx.Params{"id": operation.DependencyEdgeDeletion.ID, "owner": ownerID}).One(&ends)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			own[ends.Predecessor], own[ends.Successor] = struct{}{}, struct{}{}
		}
	}
	return own, nil
}

// relationMirrorRows names the rows a change writes only as the other ends
// of its relation pairs — a Blocked by from a Blocking, a Parent item from
// Sub-items, a two-way relation's other side — rows the writer may not even
// edit themselves; they keep when and by whom they were last edited, as
// Notion's do (seen 2026-11-09: a guest's link, or its removal, left the
// other page's last edited time, on a page they could only view too). Nil
// where the change takes no pair's other end itself (own is nil).
func relationMirrorRows(own map[string]struct{}, operations []graphOperation) map[string]struct{} {
	if own == nil {
		return nil
	}
	mirrored := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		if _, named := own[operation.Row.ID]; !named {
			mirrored[operation.Row.ID] = struct{}{}
		}
	}
	return mirrored
}

// mergeDatabaseRelationPairTargets applies the rows to add and remove to a
// relation value, keeping its order.
func mergeDatabaseRelationPairTargets(current []string, change [2][]string) []string {
	removed := map[string]struct{}{}
	for _, id := range change[1] {
		removed[id] = struct{}{}
	}
	next := []string{}
	seen := map[string]struct{}{}
	for _, id := range append(append([]string{}, current...), change[0]...) {
		if _, drop := removed[id]; drop {
			continue
		}
		if _, duplicate := seen[id]; !duplicate {
			seen[id] = struct{}{}
			next = append(next, id)
		}
	}
	return next
}
