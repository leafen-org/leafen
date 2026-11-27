package app

import (
	"context"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func deleteDatabaseMutationPropertyFinal(app core.App, job storedDatabaseMutationDeletion) (bool, int, string, bool, error) {
	sourceID, err := databaseMutationDeletionTargetSource(app, job.OwnerID, job.PageID, job.TargetKind, job.TargetID)
	if err != nil {
		return false, 0, "", false, err
	}
	var bindingCount int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_dependency_bindings}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[data_source_id]]={:sourceId}
			AND ([[predecessor_property_id]]={:targetId} OR [[successor_property_id]]={:targetId})`,
	).Bind(dbx.Params{
		"owner": job.OwnerID, "pageId": job.PageID, "sourceId": sourceID, "targetId": job.TargetID,
	}).Row(&bindingCount); err != nil {
		return false, 0, "", false, err
	}
	if bindingCount > 0 {
		processed, err := deleteDatabaseMutationDependencyEdges(app, job, sourceID, "")
		if err != nil || processed > 0 {
			return processed > 0, processed, job.Phase, false, err
		}
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_dependency_bindings}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[data_source_id]]={:sourceId}`,
		).Bind(dbx.Params{"owner": job.OwnerID, "pageId": job.PageID, "sourceId": sourceID}).Execute(); err != nil {
			return false, 0, "", false, err
		}
	}
	return deleteDatabaseMutationTargetEntity(app, job, "leafen_database_properties")
}

func deleteDatabaseMutationRowFinal(app core.App, job storedDatabaseMutationDeletion) (bool, int, string, bool, error) {
	processed, err := deleteDatabaseMutationDependencyEdges(app, job, "", job.TargetID)
	if err != nil || processed > 0 {
		return processed > 0, processed, job.Phase, false, err
	}
	// The row's page content is queued as the row goes (row_page_content_cleanup.go): clear it now.
	notifyRecoverableBlockTrashRetentionAfterCommit(app)
	return deleteDatabaseMutationTargetEntity(app, job, "leafen_database_rows")
}

func deleteDatabaseMutationSourceFinal(app core.App, job storedDatabaseMutationDeletion) (bool, int, string, bool, error) {
	processed, err := deleteDatabaseSQLQueryJobsForSource(app, job)
	if err != nil || processed > 0 {
		return false, processed, job.Phase, false, err
	}
	if err := deleteProductFileDataSourceIconReference(app, job.WorkspaceID, job.PageID, job.TargetID); err != nil {
		return false, 0, job.Phase, false, err
	}
	if err := deleteProductFileDataSourceCoverReference(app, job.WorkspaceID, job.PageID, job.TargetID); err != nil {
		return false, 0, job.Phase, false, err
	}
	return deleteDatabaseMutationTargetEntity(app, job, "leafen_data_sources")
}

func deleteDatabaseSQLQueryJobsForSource(
	app core.App,
	job storedDatabaseMutationDeletion,
) (int, error) {
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_sql_query_jobs}} WHERE [[rowid]] IN (
			SELECT [[query_job]].[[rowid]]
			FROM {{leafen_database_sql_query_jobs}} AS [[query_job]]
			JOIN {{leafen_database_sql_query_sources}} AS [[source]]
				INDEXED BY [[idx_leafen_database_sql_query_sources_page]]
				ON [[source]].[[job_id]] = [[query_job]].[[id]]
			WHERE [[query_job]].[[content_owner_id]] = {:owner}
				AND [[query_job]].[[workspace_id]] = {:workspaceId}
				AND [[source]].[[page_id]] = {:pageId}
				AND [[source]].[[data_source_id]] = {:dataSourceId}
			GROUP BY [[query_job]].[[rowid]]
			ORDER BY [[query_job]].[[rowid]] LIMIT {:limit}
		)
	`).Bind(dbx.Params{
		"owner": job.OwnerID, "workspaceId": job.WorkspaceID,
		"pageId": job.PageID, "dataSourceId": job.TargetID,
		"limit": databaseMutationItemsPerTurn,
	}).Execute()
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}

func deleteDatabaseMutationTargetEntity(
	app core.App, job storedDatabaseMutationDeletion, table string,
) (bool, int, string, bool, error) {
	parentID := ""
	if job.TargetKind == databaseMutationTargetRow {
		if err := app.DB().NewQuery(`SELECT [[subitem_parent_id]] FROM {{leafen_database_rows}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:targetId}`,
		).Bind(dbx.Params{
			"owner": job.OwnerID, "pageId": job.PageID, "targetId": job.TargetID,
		}).Row(&parentID); err != nil {
			return false, 0, "", false, err
		}
	}
	if job.TargetKind == databaseMutationTargetProperty {
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_relation_property_targets}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[property_id]]={:targetId}`,
		).Bind(dbx.Params{
			"owner": job.OwnerID, "pageId": job.PageID, "targetId": job.TargetID,
		}).Execute(); err != nil {
			return false, 0, "", false, err
		}
		if err := forgetDatabaseTemplatePropertyValues(app, job.PageID, job.TargetID); err != nil {
			return false, 0, "", false, err
		}
	}
	if job.TargetKind == databaseMutationTargetSource {
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_dependency_bindings}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[data_source_id]]={:targetId}`,
		).Bind(dbx.Params{
			"owner": job.OwnerID, "pageId": job.PageID, "targetId": job.TargetID,
		}).Execute(); err != nil {
			return false, 0, "", false, err
		}
	}
	if err := deleteDatabaseEntityProjection(app, table, job.OwnerID, job.PageID, job.TargetID); err != nil {
		return false, 0, "", false, err
	}
	if job.TargetKind == databaseMutationTargetRow {
		if err := deleteDatabaseRowBodyRoom(app, job.TargetID); err != nil {
			return false, 0, "", false, err
		}
		// The index lets go of the deleted item.
		if err := markDatabaseRowsSearchDirty(
			context.Background(), app, job.WorkspaceID, []string{job.TargetID},
		); err != nil {
			return false, 0, "", false, err
		}
	}
	if parentID != "" {
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_database_rows}}
			SET [[subitem_child_count]]=MAX(0,[[subitem_child_count]]-1),[[updated]]={:now}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:parentId}`,
		).Bind(dbx.Params{
			"owner": job.OwnerID, "pageId": job.PageID, "parentId": parentID,
			"now": time.Now().UTC().UnixMilli(),
		}).Execute(); err != nil {
			return false, 0, "", false, err
		}
	}
	return true, 1, databaseMutationPhaseCompleted, true, nil
}

// recordDatabaseMutationDeletionHistory makes a finished deletion a version
// of its page, as the same change made in the graph is: a row is named as
// taken away, and a property or data source leaves its database's state
// after it. Its turns changed no blocks, so its last update stands for them.
func recordDatabaseMutationDeletionHistory(
	app core.App, job storedDatabaseMutationDeletion, sourceID string, update []byte,
) error {
	if started, err := pageHistoryStarted(app, job.PageID); err != nil || !started {
		return err
	}
	var rows []pageHistoryRowChange
	var databases pageHistoryDatabaseChanges
	switch job.TargetKind {
	case databaseMutationTargetRow:
		rows = []pageHistoryRowChange{{ID: job.TargetID, Change: pageHistoryChangeRowRemoved}}
	case databaseMutationTargetProperty, databaseMutationTargetSource:
		owner, err := pageHistoryDatabaseOwner(app, job.PageID)
		if err != nil {
			return err
		}
		states, err := loadPageHistoryDatabaseStates(app, owner, job.PageID)
		if err != nil {
			return err
		}
		databases = pageHistoryDatabaseChanges{sourceID: nil}
		if state, held := states[sourceID]; held {
			databases[sourceID] = &state
		}
	}
	return recordPageHistoryGraphMutation(app, job.PageID, job.ActorID, update, nil, databases, rows)
}
