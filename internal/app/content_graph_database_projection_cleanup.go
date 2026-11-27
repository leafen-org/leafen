package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Database record cleanup removes one source, property or row from the
// database records, which are its authority, with the references that
// depend on it.

func deleteDatabaseSourceProjection(
	app core.App,
	owner, workspaceID, pageID, sourceID string,
) error {
	if err := deleteProductFileSourceReferences(app, workspaceID, pageID, sourceID); err != nil {
		return err
	}
	if err := deleteProductFileDataSourceIconReference(app, workspaceID, pageID, sourceID); err != nil {
		return err
	}
	if err := deleteProductFileDataSourceCoverReference(app, workspaceID, pageID, sourceID); err != nil {
		return err
	}
	// One statement per query: the driver binds each statement of a
	// multi-statement query from the first parameter on.
	params := dbx.Params{"owner": owner, "pageId": pageID, "id": sourceID}
	for _, statement := range []string{
		`DELETE FROM {{leafen_database_view_property_refs}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[view_id]] IN (SELECT [[id]] FROM {{leafen_database_views}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:id})`,
		`DELETE FROM {{leafen_database_relation_property_targets}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[source_id]] = {:id}`,
		`DELETE FROM {{leafen_database_relation_items}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[data_source_id]] = {:id}`,
		`DELETE FROM {{leafen_database_row_values}}
		WHERE ([[owner]], [[page_id]], [[row_id]]) IN (
			SELECT [[owner]], [[page_id]], [[id]] FROM {{leafen_database_rows}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:id}
		)`,
		`DELETE FROM {{leafen_reminders}} WHERE [[page_id]] = {:pageId} AND substr([[reminder_key]], 1, 4) = 'row:'
			AND [[block_id]] IN (
				SELECT [[id]] FROM {{leafen_database_rows}}
				WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:id}
			)`,
		`DELETE FROM {{leafen_deadline_notices}} WHERE [[page_id]] = {:pageId}
			AND [[row_id]] IN (
				SELECT [[id]] FROM {{leafen_database_rows}}
				WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:id}
			)`,
		`DELETE FROM {{leafen_database_rows}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:id}`,
		`DELETE FROM {{leafen_database_views}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:id}`,
		`DELETE FROM {{leafen_database_properties}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:id}`,
		// The source's templates go with it, their pages with them, their
		// sub-pages to the trash on their own. They are found by their
		// source; the owner is read after (+), or SQLite, which keeps no
		// statistics, reads the owner's every template.
		databaseTemplateSubPagesToTrashSQL(`SELECT [[id]] FROM {{leafen_database_templates}}
			WHERE +[[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:id}`),
		`DELETE FROM {{leafen_database_toolbar_template_state}} WHERE [[template_id]] IN (
			SELECT [[id]] FROM {{leafen_database_templates}}
			WHERE +[[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:id})`,
		`DELETE FROM {{leafen_database_templates}} WHERE +[[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:id}`,
		`DELETE FROM {{leafen_data_sources}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] = {:id}`,
	} {
		if _, err := app.DB().NewQuery(statement).Bind(params).Execute(); err != nil {
			return err
		}
	}
	// Database pages holding databases were queued for cleanup.
	notifyRecoverableBlockTrashRetentionAfterCommit(app)
	return nil
}

func deleteDatabaseEntityProjection(app core.App, table, owner, pageID, id string) error {
	_, err := app.DB().NewQuery(
		"DELETE FROM {{" + table + "}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] = {:id}",
	).Bind(dbx.Params{"owner": owner, "pageId": pageID, "id": id}).Execute()
	return err
}

func deleteDatabasePropertyProjection(
	app core.App,
	owner, workspaceID, pageID, id string,
) error {
	if err := deleteProductFileRowReferences(app, workspaceID, pageID, "", id); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_relation_property_targets}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[property_id]] = {:id};
		DELETE FROM {{leafen_database_relation_items}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[property_id]] = {:id};
		DELETE FROM {{leafen_database_row_values}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[property_id]] = {:id}
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "id": id}).Execute(); err != nil {
		return err
	}
	if err := forgetDatabaseTemplatePropertyValues(app, pageID, id); err != nil {
		return err
	}
	return deleteDatabaseEntityProjection(app, "leafen_database_properties", owner, pageID, id)
}

func deleteDatabaseRowProjection(
	app core.App,
	owner, workspaceID, pageID, actorID, id string,
) error {
	// Other pages' relations let go of it; this page's below.
	if err := admitGoneRelationTargetWork(
		app, owner, workspaceID, actorID, "gone:"+pageID+":"+id, []string{id}, nil,
	); err != nil {
		return err
	}
	if err := deleteProductFileDatabaseRowPageIconReference(
		app, workspaceID, pageID, id,
	); err != nil {
		return err
	}
	if err := deleteProductFileRowReferences(app, workspaceID, pageID, id, ""); err != nil {
		return err
	}
	parentID := ""
	if err := app.DB().NewQuery(`
		SELECT [[subitem_parent_id]] FROM {{leafen_database_rows}}
		WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"id": id, "owner": owner, "pageId": pageID}).Row(&parentID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	updated := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_row_values}}
		SET [[value]] = COALESCE((
			SELECT json_group_array([[entry]].[[value]])
			FROM json_each([[leafen_database_row_values]].[[value]]) AS [[entry]]
			WHERE [[entry]].[[value]] != {:id}
		), '[]'), [[updated]] = {:updated}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND ([[row_id]], [[property_id]]) IN (
				SELECT [[row_id]], [[property_id]] FROM {{leafen_database_relation_items}}
				WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
					AND [[target_row_id]] = {:id}
			)
			AND [[type]] = {:relationType} AND json_valid([[value]])
	`).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "id": id,
		"relationType": graphValueTypeRelation, "updated": updated,
	}).Execute(); err != nil {
		return err
	}
	rowParams := dbx.Params{"owner": owner, "pageId": pageID, "id": id}
	for _, statement := range []string{
		`DELETE FROM {{leafen_reminders}}
		WHERE [[page_id]] = {:pageId} AND substr([[reminder_key]], 1, 4) = 'row:' AND [[block_id]] = {:id}`,
		`DELETE FROM {{leafen_deadline_notices}} WHERE [[page_id]] = {:pageId} AND [[row_id]] = {:id}`,
		`DELETE FROM {{leafen_database_relation_items}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND ([[row_id]] = {:id} OR [[target_row_id]] = {:id})`,
	} {
		if _, err := app.DB().NewQuery(statement).Bind(rowParams).Execute(); err != nil {
			return err
		}
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_row_values}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[row_id]] = {:id}
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "id": id}).Execute(); err != nil {
		return err
	}
	notifyRecoverableBlockTrashRetentionAfterCommit(app)
	if err := deleteDatabaseEntityProjection(app, "leafen_database_rows", owner, pageID, id); err != nil {
		return err
	}
	if parentID != "" {
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_database_rows}}
			SET [[subitem_child_count]] = MAX(0, [[subitem_child_count]] - 1), [[updated]] = {:updated}
			WHERE [[id]] = {:parentId} AND [[owner]] = {:owner} AND [[page_id]] = {:pageId}
		`).Bind(dbx.Params{
			"parentId": parentID, "owner": owner, "pageId": pageID,
			"updated": updated,
		}).Execute()
		return err
	}
	return nil
}
