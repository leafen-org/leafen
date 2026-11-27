package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// applyDatabaseDependencyRecord writes a dependency change to the page's
// dependency records. A binding's revision guards every change against a
// writer who saw an older configuration.
func applyDatabaseDependencyRecord(
	app core.App,
	owner, workspaceID, pageID string,
	operation graphOperation,
	now int64,
) error {
	switch operation.Kind {
	case graphOperationKindConfigureDependency:
		binding := operation.DependencyBinding
		current, exists, err := loadDatabaseDependencyRecord(app, pageID, binding.DataSourceID)
		if err != nil {
			return err
		}
		currentRevision := 0
		if exists {
			currentRevision = current.Revision
		}
		if binding.ExpectedRevision != currentRevision {
			return graphConflictError(errorCodeDatabaseDependencyStale, "The dependency configuration changed. Try again.")
		}
		// Only this page's own configuration of the source changes: one
		// another owner's or page's record holds is left as it is and the
		// change refused (content_graph_written_ids.go).
		result, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_dependency_bindings}}
				([[data_source_id]], [[owner]], [[workspace_id]], [[page_id]],
				 [[predecessor_property_id]], [[successor_property_id]], [[data_key]],
				 [[revision]], [[created]], [[updated]])
			VALUES
				({:dataSourceId}, {:owner}, {:workspaceId}, {:pageId},
				 {:predecessorPropertyId}, {:successorPropertyId}, {:dataKey},
				 {:revision}, {:now}, {:now})
			ON CONFLICT ([[data_source_id]]) DO UPDATE SET
				[[predecessor_property_id]] = excluded.[[predecessor_property_id]],
				[[successor_property_id]] = excluded.[[successor_property_id]],
				[[data_key]] = excluded.[[data_key]], [[revision]] = excluded.[[revision]],
				[[updated]] = excluded.[[updated]]
			WHERE [[owner]] = excluded.[[owner]] AND [[page_id]] = excluded.[[page_id]]
		`).Bind(dbx.Params{
			"dataSourceId": binding.DataSourceID, "owner": owner,
			"workspaceId": workspaceID, "pageId": pageID,
			"predecessorPropertyId": binding.PredecessorPropertyID,
			"successorPropertyId":   binding.SuccessorPropertyID,
			"dataKey":               binding.DataKey, "revision": currentRevision + 1, "now": now,
		}).Execute()
		return refuseUnwrittenGraphRecord(result, err)
	case graphOperationKindUpsertDependencyEdge:
		edge := operation.DependencyEdge
		binding, err := requireDatabaseDependencyRecordRevision(app, pageID, edge.DataSourceID, edge.ExpectedRevision)
		if err != nil {
			return err
		}
		// A link written under an ID changes only the link this page's source
		// holds under it: one another owner's, page's or source's link holds
		// is left as it is and the change refused (content_graph_written_ids.go).
		result, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_dependency_edges}}
				([[id]], [[owner]], [[workspace_id]], [[page_id]], [[data_source_id]],
				 [[data_key]], [[predecessor_row_id]], [[successor_row_id]], [[created]], [[updated]])
			VALUES
				({:id}, {:owner}, {:workspaceId}, {:pageId}, {:dataSourceId},
				 {:dataKey}, {:predecessorRowId}, {:successorRowId}, {:now}, {:now})
			ON CONFLICT ([[id]]) DO UPDATE SET
				[[data_key]] = excluded.[[data_key]],
				[[predecessor_row_id]] = excluded.[[predecessor_row_id]],
				[[successor_row_id]] = excluded.[[successor_row_id]], [[updated]] = excluded.[[updated]]
			WHERE [[owner]] = excluded.[[owner]] AND [[page_id]] = excluded.[[page_id]]
				AND [[data_source_id]] = excluded.[[data_source_id]]
		`).Bind(dbx.Params{
			"id": edge.ID, "owner": owner,
			"workspaceId": workspaceID, "pageId": pageID,
			"dataSourceId": edge.DataSourceID, "dataKey": binding.DataKey,
			"predecessorRowId": edge.PredecessorRowID,
			"successorRowId":   edge.SuccessorRowID, "now": now,
		}).Execute()
		return refuseUnwrittenGraphRecord(result, err)
	case graphOperationKindDeleteDependencyEdge:
		deletion := operation.DependencyEdgeDeletion
		if _, err := requireDatabaseDependencyRecordRevision(
			app, pageID, deletion.DataSourceID, deletion.ExpectedRevision,
		); err != nil {
			return err
		}
		result, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_database_dependency_edges}}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[page_id]] = {:pageId}
				AND [[data_source_id]] = {:dataSourceId}
		`).Bind(dbx.Params{
			"id": deletion.ID, "owner": owner, "pageId": pageID, "dataSourceId": deletion.DataSourceID,
		}).Execute()
		if err != nil {
			return err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if deleted == 0 {
			return graphInputError(errorCodeGraphReferenceInvalid, "The dependency link to delete was not found.")
		}
	}
	return nil
}

func requireDatabaseDependencyRecordRevision(
	app core.App,
	pageID, dataSourceID string,
	expectedRevision int,
) (databaseDependencyBindingAuthority, error) {
	binding, exists, err := loadDatabaseDependencyRecord(app, pageID, dataSourceID)
	if err != nil {
		return binding, err
	}
	if !exists {
		return binding, graphInputError(errorCodeGraphReferenceInvalid, "Dependency configuration not found.")
	}
	if binding.Revision != expectedRevision {
		return binding, graphConflictError(errorCodeDatabaseDependencyStale, "The dependency configuration changed. Try again.")
	}
	return binding, nil
}

// loadDatabaseDependencyRecord reads a source's dependency configuration
// on the page that holds it.
func loadDatabaseDependencyRecord(
	app core.App,
	pageID, dataSourceID string,
) (databaseDependencyBindingAuthority, bool, error) {
	binding := databaseDependencyBindingAuthority{}
	err := app.DB().NewQuery(`
		SELECT [[data_source_id]], [[predecessor_property_id]], [[successor_property_id]], [[data_key]], [[revision]]
		FROM {{leafen_database_dependency_bindings}}
		WHERE [[data_source_id]] = {:dataSourceId} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"dataSourceId": dataSourceID, "pageId": pageID}).Row(
		&binding.DataSourceID, &binding.PredecessorPropertyID, &binding.SuccessorPropertyID,
		&binding.DataKey, &binding.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return databaseDependencyBindingAuthority{}, false, nil
	}
	if err != nil {
		return binding, false, err
	}
	if binding.Revision < 1 {
		return binding, true, graphStructureAuthorityError()
	}
	return binding, true, nil
}
