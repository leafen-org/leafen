package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func enforceDatabaseMutationDeletionBoundary(
	app core.App, owner, pageID string, operations []graphOperation,
) error {
	for _, operation := range operations {
		if operation.Kind == graphOperationKindDeleteDataSource ||
			operation.Kind == graphOperationKindDeleteProperty ||
			operation.Kind == graphOperationKindDeleteRow {
			return graphPreconditionError(
				errorCodeDatabaseMutationLifecycleRequired,
				"Use the database deletion lifecycle.",
			)
		}
	}
	fenced, err := databaseMutationDeletionOperationsFenced(app, owner, pageID, operations)
	if err != nil {
		return err
	}
	if fenced {
		return graphConflictError(
			errorCodeDatabaseMutationConflict,
			"A database item being deleted can't be changed.",
		)
	}
	return nil
}

func databaseMutationDeletionOperationsFenced(
	app core.App, owner, pageID string, operations []graphOperation,
) (bool, error) {
	sourceIDs := map[string]struct{}{}
	propertyIDs := map[string]struct{}{}
	rowIDs := map[string]struct{}{}
	viewIDs := map[string]struct{}{}
	existingPropertyIDs := map[string]struct{}{}
	existingRowIDs := map[string]struct{}{}
	add := func(target map[string]struct{}, id string) {
		if id != "" {
			target[id] = struct{}{}
		}
	}
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertDataSource:
			add(sourceIDs, operation.DataSource.ID)
		case graphOperationKindUpsertView:
			add(sourceIDs, operation.View.DataSourceID)
			add(viewIDs, operation.View.ID)
			for _, id := range graphViewPropertyReferences(operation.View.Config) {
				add(propertyIDs, id)
			}
		case graphOperationKindDeleteView:
			add(viewIDs, operation.ViewDeletion.ID)
		case graphOperationKindUpsertProperty:
			add(propertyIDs, operation.Property.ID)
			add(sourceIDs, operation.Property.DataSourceID)
			add(sourceIDs, graphRelationTarget(operation.Property.Type, operation.Property.Config))
			add(existingPropertyIDs, operation.Property.ID)
		case graphOperationKindUpsertRow:
			add(rowIDs, operation.Row.ID)
			add(sourceIDs, operation.Row.DataSourceID)
			add(existingRowIDs, operation.Row.ID)
			if operation.Row.SubitemParentID != nil {
				add(rowIDs, *operation.Row.SubitemParentID)
			}
			for propertyID, value := range operation.Row.Values {
				add(propertyIDs, propertyID)
				if value.Type == graphValueTypeRelation {
					for _, rowID := range graphRelationRowIDs(value.Value) {
						add(rowIDs, rowID)
					}
				}
			}
		case graphOperationKindConfigureDependency:
			add(sourceIDs, operation.DependencyBinding.DataSourceID)
			add(propertyIDs, operation.DependencyBinding.PredecessorPropertyID)
			add(propertyIDs, operation.DependencyBinding.SuccessorPropertyID)
		case graphOperationKindUpsertDependencyEdge:
			add(sourceIDs, operation.DependencyEdge.DataSourceID)
			add(rowIDs, operation.DependencyEdge.PredecessorRowID)
			add(rowIDs, operation.DependencyEdge.SuccessorRowID)
		case graphOperationKindDeleteDependencyEdge:
			add(sourceIDs, operation.DependencyEdgeDeletion.DataSourceID)
		}
	}
	if len(sourceIDs)+len(propertyIDs)+len(rowIDs)+len(viewIDs)+len(existingPropertyIDs)+len(existingRowIDs) == 0 {
		return false, nil
	}
	if err := addExistingDatabaseMutationSources(
		app, owner, pageID, viewIDs, existingPropertyIDs, existingRowIDs, sourceIDs,
	); err != nil {
		return false, err
	}
	if len(sourceIDs)+len(propertyIDs)+len(rowIDs) == 0 {
		return false, nil
	}
	encodedSources, err := databaseMutationIDJSON(sourceIDs)
	if err != nil {
		return false, err
	}
	encodedProperties, err := databaseMutationIDJSON(propertyIDs)
	if err != nil {
		return false, err
	}
	encodedRows, err := databaseMutationIDJSON(rowIDs)
	if err != nil {
		return false, err
	}
	var count int
	err = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_mutation_jobs}}
		INDEXED BY [[idx_leafen_database_mutation_jobs_page_target]]
		WHERE [[owner_id]]={:owner} AND [[page_id]]={:pageId} AND [[status]]='pending' AND (
			([[target_kind]]='source' AND [[target_id]] IN (SELECT [[value]] FROM json_each({:sourceIds}))) OR
			([[target_kind]]='property' AND [[target_id]] IN (SELECT [[value]] FROM json_each({:propertyIds}))) OR
			([[target_kind]]='row' AND [[target_id]] IN (SELECT [[value]] FROM json_each({:rowIds})))
		)`,
	).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "sourceIds": encodedSources,
		"propertyIds": encodedProperties, "rowIds": encodedRows,
	}).Row(&count)
	return count != 0, err
}

func databaseMutationIDJSON(ids map[string]struct{}) (string, error) {
	values := make([]string, 0, len(ids))
	for id := range ids {
		values = append(values, id)
	}
	sort.Strings(values)
	encoded, err := json.Marshal(values)
	return string(encoded), err
}

func addExistingDatabaseMutationSources(
	app core.App, owner, pageID string,
	viewIDs, propertyIDs, rowIDs, sourceIDs map[string]struct{},
) error {
	views, err := databaseMutationIDJSON(viewIDs)
	if err != nil {
		return err
	}
	properties, err := databaseMutationIDJSON(propertyIDs)
	if err != nil {
		return err
	}
	rows, err := databaseMutationIDJSON(rowIDs)
	if err != nil {
		return err
	}
	existing := []struct {
		SourceID string `db:"data_source_id"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[entity]].[[data_source_id]] FROM json_each({:viewIds}) AS [[ids]]
		JOIN {{leafen_database_views}} AS [[entity]]
		INDEXED BY [[sqlite_autoindex_leafen_database_views_1]]
			ON [[entity]].[[id]]=CAST([[ids]].[[value]] AS TEXT)
		WHERE [[entity]].[[owner]]={:owner} AND [[entity]].[[page_id]]={:pageId}
		UNION ALL
		SELECT [[entity]].[[data_source_id]] FROM json_each({:propertyIds}) AS [[ids]]
		JOIN {{leafen_database_properties}} AS [[entity]]
		INDEXED BY [[sqlite_autoindex_leafen_database_properties_1]]
			ON [[entity]].[[id]]=CAST([[ids]].[[value]] AS TEXT)
		WHERE [[entity]].[[owner]]={:owner} AND [[entity]].[[page_id]]={:pageId}
		UNION ALL
		SELECT [[entity]].[[data_source_id]] FROM json_each({:rowIds}) AS [[ids]]
		JOIN {{leafen_database_rows}} AS [[entity]]
		INDEXED BY [[sqlite_autoindex_leafen_database_rows_1]]
			ON [[entity]].[[id]]=CAST([[ids]].[[value]] AS TEXT)
		WHERE [[entity]].[[owner]]={:owner} AND [[entity]].[[page_id]]={:pageId}
	`).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "viewIds": views,
		"propertyIds": properties, "rowIds": rows,
	}).All(&existing)
	if err != nil {
		return err
	}
	for _, row := range existing {
		if row.SourceID != "" {
			sourceIDs[row.SourceID] = struct{}{}
		}
	}
	return nil
}
