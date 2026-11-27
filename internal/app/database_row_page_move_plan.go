package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The move of a row between databases of one page is a single graph
// mutation, however many sub-items move with it (held only to one change's
// bytes, maxYgoUpdateBytes): the mapped cells, the options it adds, the
// relations within its data source and dependency edges it ends. Two-way relations to other data
// sources leave through the pair runner.

type databaseRowPageMovePlan struct {
	RowID, OwnerID, WorkspaceID, PageID string
	Operations                          []graphOperation
	// Rows are the moved row and its sub-items, parents first.
	Rows     []string
	Sources  map[string][]string
	PairWork []databaseRelationPairWork
	Dropped  []databaseRowPageMoveDroppedProperty
}

// planDatabaseRowPageMove plans the whole move inside the mutation that
// applies it. accepted names the dropped properties the mover agreed to lose.
// The row's sub-items move with it and keep their parents.
func planDatabaseRowPageMove(
	app core.App,
	authority databaseRowPageAuthority,
	targetDataSourceID string,
	accepted func(propertyID string) bool,
) (databaseRowPageMovePlan, error) {
	plan := databaseRowPageMovePlan{
		RowID: authority.RowID, OwnerID: authority.OwnerID,
		WorkspaceID: authority.WorkspaceID, PageID: authority.ContainerPageID,
	}
	tree, err := databaseRowPageMoveTree(app, authority)
	if err != nil {
		return plan, err
	}
	plan.Rows = databaseRowPageMoveTreeIDs(tree)
	values, err := planDatabaseRowPageMoveValues(app, authority, authority.ContainerPageID, targetDataSourceID, plan.Rows)
	if err != nil {
		return plan, err
	}
	for _, dropped := range values.Dropped {
		if !accepted(dropped.PropertyID) {
			return plan, errDatabaseRowPageMoveDropsValues
		}
	}
	plan.Sources, plan.Dropped = values.Sources, values.Dropped
	edges, err := databaseRowPageMoveDependencyDeletions(app, authority, plan.Rows)
	if err != nil {
		return plan, err
	}
	plan.Operations = append(plan.Operations, edges...)
	for index := range values.Added {
		plan.Operations = append(plan.Operations, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &values.Added[index]})
	}
	for _, propertyID := range workspaceExportSortedKeys(values.Options) {
		operation, err := databaseRowPageMovePropertyOperation(app, authority.ContainerPageID, propertyID, values.Options[propertyID])
		if err != nil {
			return plan, err
		}
		plan.Operations = append(plan.Operations, operation)
	}
	tail, err := databaseRowTail(app, authority.OwnerID, authority.ContainerPageID, targetDataSourceID)
	if err != nil {
		return plan, err
	}
	moved, err := databaseRowPageMoveTreeOperations(tree, values, targetDataSourceID, tail)
	if err != nil {
		return plan, err
	}
	plan.Operations = append(plan.Operations, moved...)
	rows, err := databaseRowPageMoveReleasedRows(app, authority, plan.Rows)
	if err != nil {
		return plan, err
	}
	plan.Operations = append(plan.Operations, rows...)
	if plan.PairWork, err = databaseRowPageMovePairWork(app, authority.OwnerID, plan.Rows); err != nil {
		return plan, err
	}
	if len(plan.Rows) > 1 {
		if err := enableDatabaseRowPageMoveSubitems(app, authority, authority.ContainerPageID, targetDataSourceID, values.TitleID); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

// enableDatabaseRowPageMoveSubitems turns on sub-items in the target when
// sub-items move there, as Notion does.
func enableDatabaseRowPageMoveSubitems(
	app core.App, authority databaseRowPageAuthority, targetPageID, targetDataSourceID, titlePropertyID string,
) error {
	stored := "null"
	if err := app.DB().NewQuery(`
		SELECT [[subitems_json]] FROM {{leafen_database_task_features}} WHERE [[data_source_id]]={:sourceId}
	`).Bind(dbx.Params{"sourceId": targetDataSourceID}).Row(&stored); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	subitems := map[string]any{}
	_ = json.Unmarshal([]byte(stored), &subitems)
	if subitems["enabled"] == true {
		return nil
	}
	if subitems["propertyDisposition"] != "keep" {
		subitems = map[string]any{"titlePropertyId": titlePropertyID, "showToggleOnTitle": false}
	}
	subitems["enabled"] = true
	encoded, err := json.Marshal(subitems)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_database_task_features}}
			([[data_source_id]], [[owner]], [[workspace_id]], [[page_id]], [[authority_revision]],
			 [[subitems_json]], [[dependencies_json]], [[tasks_json]], [[created_at]], [[updated_at]])
		VALUES ({:sourceId}, {:owner}, {:workspaceId}, {:pageId}, 1, {:subitems}, 'null', 'null', {:now}, {:now})
		ON CONFLICT ([[data_source_id]]) DO UPDATE SET
			[[authority_revision]]=[[authority_revision]]+1, [[subitems_json]]=excluded.[[subitems_json]],
			[[updated_at]]=excluded.[[updated_at]]
	`).Bind(dbx.Params{
		"sourceId": targetDataSourceID, "owner": authority.OwnerID, "workspaceId": authority.WorkspaceID,
		"pageId": targetPageID, "subitems": string(encoded), "now": now,
	}).Execute()
	return err
}

func databaseRowPageMovePropertyOperation(
	app core.App,
	pageID, propertyID string,
	options []map[string]any,
) (graphOperation, error) {
	stored, exists, err := loadDatabasePropertyRecord(app, pageID, propertyID)
	if err != nil {
		return graphOperation{}, err
	}
	if !exists {
		return graphOperation{}, graphStructureAuthorityError()
	}
	config := stored.Config
	config["options"] = options
	encoded, err := json.Marshal(config)
	if err != nil {
		return graphOperation{}, err
	}
	return graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
		ID: propertyID, DataSourceID: stored.DataSourceID, Type: stored.Type, Name: stored.Name,
		Position: stored.Position, Config: encoded,
	}}, nil
}

func databaseRowPageMoveRow(app core.App, pageID, rowID string) (*graphDatabaseRow, error) {
	row, err := databaseRowRecordOperation(app, pageID, rowID)
	if err != nil {
		return nil, err
	}
	row.Values = map[string]graphRowValue{}
	return &row, nil
}

// databaseRowPageMovePairWork hands the moved rows' two-way relations to
// other data sources to the relation pair runner. Those within the data
// source let go in the move itself, as both their sides change together.
func databaseRowPageMovePairWork(app core.App, ownerID string, rowIDs []string) ([]databaseRelationPairWork, error) {
	result := []databaseRelationPairWork{}
	for _, rowID := range rowIDs {
		work, err := planDatabaseRelationPairDeletion(app, ownerID, databaseMutationTargetRow, rowID)
		if err != nil {
			return nil, err
		}
		for _, item := range work {
			if item.Payload.TargetDataSourceID != item.Payload.SourceDataSourceID {
				result = append(result, item)
			}
		}
	}
	return result, nil
}

// databaseRowPageMoveReleasedRows takes the row out of the relations of the
// page's other rows: one-way ones and two-way ones within their data source.
func databaseRowPageMoveReleasedRows(
	app core.App,
	authority databaseRowPageAuthority,
	movedIDs []string,
) ([]graphOperation, error) {
	moved := make(map[string]struct{}, len(movedIDs))
	for _, id := range movedIDs {
		moved[id] = struct{}{}
	}
	encodedMoved, err := json.Marshal(movedIDs)
	if err != nil {
		return nil, err
	}
	incoming := []struct {
		RowID        string `db:"row_id"`
		PropertyID   string `db:"property_id"`
		DataSourceID string `db:"data_source_id"`
		Config       string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[items]].[[row_id]], [[items]].[[property_id]],
			[[properties]].[[data_source_id]], [[properties]].[[config]]
		FROM {{leafen_database_relation_items}} AS [[items]]
		INDEXED BY [[idx_leafen_database_relation_items_target]]
		JOIN {{leafen_database_properties}} AS [[properties]]
			ON [[properties]].[[id]] = [[items]].[[property_id]]
		WHERE [[items]].[[owner]]={:owner} AND [[items]].[[page_id]]={:pageId}
			AND [[items]].[[target_row_id]] IN (SELECT [[value]] FROM json_each({:moved}))
			AND [[items]].[[row_id]] NOT IN (SELECT [[value]] FROM json_each({:moved}))
		ORDER BY [[items]].[[row_id]], [[items]].[[property_id]]
	`).Bind(dbx.Params{
		"owner": authority.OwnerID, "pageId": authority.ContainerPageID,
		"moved": string(encodedMoved),
	}).All(&incoming); err != nil {
		return nil, err
	}
	rows := map[string]*graphDatabaseRow{}
	order := []string{}
	seenCells := map[string]struct{}{}
	release := func(rowID string) (*graphDatabaseRow, error) {
		if row := rows[rowID]; row != nil {
			return row, nil
		}
		row, err := databaseRowPageMoveRow(app, authority.ContainerPageID, rowID)
		if err != nil {
			return nil, err
		}
		rows[rowID] = row
		order = append(order, rowID)
		return row, nil
	}
	for _, item := range incoming {
		if _, seen := seenCells[item.RowID+"\x00"+item.PropertyID]; seen {
			continue
		}
		seenCells[item.RowID+"\x00"+item.PropertyID] = struct{}{}
		config := map[string]any{}
		_ = json.Unmarshal([]byte(item.Config), &config)
		// A two-way relation to another data source lets go through the
		// relation pair runner.
		if pair := databaseRelationPairOf(item.PropertyID, item.DataSourceID, graphValueTypeRelation, config); pair != nil &&
			pair.TargetDataSourceID != item.DataSourceID {
			continue
		}
		cell, exists, err := loadDatabaseCellRecord(app, authority.ContainerPageID, item.RowID, item.PropertyID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, graphStructureAuthorityError()
		}
		values, isList := cell.Value.([]any)
		if !isList {
			return nil, graphStructureAuthorityError()
		}
		ids, err := databaseRelationStrings(values)
		if err != nil {
			return nil, err
		}
		kept := make([]string, 0, len(ids))
		for _, id := range ids {
			if _, leaving := moved[id]; !leaving {
				kept = append(kept, id)
			}
		}
		value := graphRowValueCleared
		if len(kept) > 0 {
			if value, err = json.Marshal(kept); err != nil {
				return nil, err
			}
		}
		row, err := release(item.RowID)
		if err != nil {
			return nil, err
		}
		row.Values[item.PropertyID] = graphRowValue{Type: graphValueTypeRelation, Value: value}
	}
	operations := make([]graphOperation, 0, len(order))
	for _, rowID := range order {
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: rows[rowID]})
	}
	return operations, nil
}

func databaseRowPageMoveDependencyDeletions(
	app core.App,
	authority databaseRowPageAuthority,
	movedIDs []string,
) ([]graphOperation, error) {
	encodedMoved, err := json.Marshal(movedIDs)
	if err != nil {
		return nil, err
	}
	edges := []struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]] FROM {{leafen_database_dependency_edges}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
			AND ([[predecessor_row_id]] IN (SELECT [[value]] FROM json_each({:moved}))
				OR [[successor_row_id]] IN (SELECT [[value]] FROM json_each({:moved})))
		ORDER BY [[id]]
	`).Bind(dbx.Params{
		"owner": authority.OwnerID, "pageId": authority.ContainerPageID,
		"moved": string(encodedMoved),
	}).All(&edges); err != nil {
		return nil, err
	}
	operations := make([]graphOperation, 0, len(edges))
	revisions := map[string]int{}
	for _, edge := range edges {
		revision, known := revisions[edge.DataSourceID]
		if !known {
			binding, exists, err := loadDatabaseDependencyRecord(app, authority.ContainerPageID, edge.DataSourceID)
			if err != nil || !exists {
				return nil, graphStructureAuthorityError()
			}
			revision = binding.Revision
			revisions[edge.DataSourceID] = revision
		}
		operations = append(operations, graphOperation{
			Kind: graphOperationKindDeleteDependencyEdge,
			DependencyEdgeDeletion: &graphDependencyEdgeDelete{
				ID: edge.ID, DataSourceID: edge.DataSourceID, ExpectedRevision: revision,
			},
		})
	}
	return operations, nil
}

// finish removes the old cells' projections, relation links, file references
// and date reminders, and hands the rows' two-way relations to the pair runner.
func (plan *databaseRowPageMovePlan) finish(app core.App, actorID, mutationID string) error {
	kept := map[string]map[string]struct{}{}
	for _, operation := range plan.Operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil {
			if kept[operation.Row.ID] == nil {
				kept[operation.Row.ID] = map[string]struct{}{}
			}
			for propertyID := range operation.Row.Values {
				kept[operation.Row.ID][propertyID] = struct{}{}
			}
		}
	}
	for _, rowID := range plan.Rows {
		sources := append([]string{}, plan.Sources[rowID]...)
		sort.Strings(sources)
		for _, propertyID := range sources {
			if _, exists := kept[rowID][propertyID]; exists {
				continue
			}
			if err := deleteDatabaseRowValueProjection(
				app, plan.WorkspaceID, plan.PageID, rowID, propertyID, false,
			); err != nil {
				return err
			}
			prefix := databaseDateReminderKeyPrefix + rowID + ":" + propertyID + "|"
			if _, err := app.DB().NewQuery(`
				DELETE FROM {{leafen_reminders}}
				WHERE [[page_id]]={:pageId} AND substr([[reminder_key]], 1, length({:prefix}))={:prefix}
			`).Bind(dbx.Params{"pageId": plan.PageID, "prefix": prefix}).Execute(); err != nil {
				return err
			}
			if _, err := app.DB().NewQuery(`
				DELETE FROM {{leafen_deadline_notices}}
				WHERE [[page_id]]={:pageId} AND [[row_id]]={:rowId} AND [[property_id]]={:propertyId}
			`).Bind(dbx.Params{"pageId": plan.PageID, "rowId": rowID, "propertyId": propertyID}).Execute(); err != nil {
				return err
			}
		}
	}
	return admitDatabaseRelationPairWork(
		app, plan.OwnerID, plan.WorkspaceID, actorID, "row-page-move:"+mutationID, plan.PairWork,
	)
}
