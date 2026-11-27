package app

import (
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A deleted template waits in the trash as a page does, as Notion's (seen
// 2026-10-30): the trash lists it under its database with the rows there,
// its restore and permanent deletion are a row's (its id in a row's place),
// and it goes by itself after the trash's 30 days. The template menu no
// longer shows it.

// databaseTemplateTrashedAtSQL is when the template of state went to the
// trash, in milliseconds: the expression the trash expiry index of deleted
// templates holds (schema_baseline.json), written alike
// so the runner's reads go along it.
const databaseTemplateTrashedAtSQL = `CAST(ROUND((julianday([[state]].[[deleted_at]]) - 2440587.5) * 86400000) AS INTEGER)`

// listDatabaseTemplateTrash reads up to limit deleted templates of the
// workspace still in the trash, in a trashed row's shape, newest first,
// after the cursor.
func listDatabaseTemplateTrash(app core.App, workspaceID string, after timeIDCursor, limit int) ([]storedDatabaseRowTrash, error) {
	stored := []storedDatabaseRowTrash{}
	err := app.DB().NewQuery(`
		SELECT [[template]].[[id]] AS [[row_id]], [[template]].[[page_id]], [[template]].[[data_source_id]],
			` + databaseTemplateTrashedAtSQL + ` AS [[trashed_at]], [[state]].[[deleted_by]] AS [[trashed_by]],
			COALESCE([[record]].[[icon]], '') AS [[icon]], COALESCE([[record]].[[icon_type]], 'none') AS [[icon_type]],
			[[state]].[[authority_revision]],
			json_quote(` + databaseTemplateNameSQL("[[template]]") + `) AS [[title]],
			TRUE AS [[template]]
		FROM {{leafen_database_toolbar_template_state}} [[state]]
		JOIN {{leafen_database_templates}} [[template]] ON [[template]].[[id]] = [[state]].[[template_id]]
		LEFT JOIN {{leafen_pages}} [[record]] ON [[record]].[[id]] = [[template]].[[id]]
		WHERE [[state]].[[workspace_id]] = {:workspaceId} AND [[state]].[[deleted_at]] != ''
			AND ` + databaseTemplateTrashedAtSQL + ` > {:since}
			AND ` + databaseSourceNotTrashedSQL("[[template]].[[data_source_id]]") + `
			AND ({:afterId} = '' OR ` + databaseTemplateTrashedAtSQL + ` < {:afterAt}
				OR (` + databaseTemplateTrashedAtSQL + ` = {:afterAt} AND [[template]].[[id]] > {:afterId}))
		ORDER BY [[trashed_at]] DESC, [[row_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "limit": limit, "afterAt": after.At, "afterId": after.ID,
		"since": time.Now().UTC().Add(-databaseRowTrashRetention).UnixMilli(),
	}).All(&stored)
	return stored, err
}

// changeDatabaseTemplateTrash restores a deleted template to its database's
// menu, or deletes it for good — which, like deleting a page from the trash,
// needs the workspace owner's authority.
func changeDatabaseTemplateTrash(
	app core.App, principalID, templateID string, expectedRevision int, purge bool,
) (databaseRowPageActionResponse, error) {
	response := databaseRowPageActionResponse{RowID: templateID}
	err := app.RunInTransaction(func(txApp core.App) error {
		place := struct {
			PageID       string `db:"page_id"`
			DataSourceID string `db:"data_source_id"`
		}{}
		err := txApp.DB().NewQuery(`SELECT [[page_id]], [[data_source_id]] FROM {{leafen_database_templates}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": templateID}).One(&place)
		if errors.Is(err, sql.ErrNoRows) {
			return errDatabaseRowPageNotFound
		} else if err != nil {
			return err
		}
		authority, err := authorizeDatabaseToolbarConfig(txApp, principalID, place.PageID, place.DataSourceID, permissionWorkspaceWrite)
		if errors.Is(err, errDatabaseToolbarConfigNotFound) {
			return errDatabaseRowPageNotFound
		} else if err != nil {
			return err
		}
		template, err := databaseToolbarTemplateForMutation(txApp, authority, templateID)
		if err != nil {
			return err
		}
		if template.DeletedAt == "" && purge {
			return errDatabaseRowPageNotTrashed
		}
		if template.DeletedAt == "" {
			return errDatabaseRowPageNotInTrash
		}
		if template.AuthorityRevision != expectedRevision {
			return errDatabaseRowPageConflict
		}
		response.ContainerPageID = authority.PageID
		if !purge {
			state := databaseToolbarMutationState{ActorID: principalID}
			err := setDatabaseToolbarTemplateDeleted(txApp, authority, databaseToolbarConfigMutationOperation{
				Kind: "restoreTemplate", TemplateID: &templateID, ExpectedRevision: &expectedRevision,
			}, false, &state)
			response.AuthorityRevision = state.TemplateAuthorityRevision
			if errors.Is(err, errDatabaseToolbarConfigConflict) {
				return errDatabaseRowPageConflict
			}
			return err
		}
		if _, err := authorizeWorkspace(txApp, principalID, authority.WorkspaceID, permissionWorkspaceOwner); err != nil {
			return err
		}
		response.AuthorityRevision = expectedRevision
		return deleteDatabaseTemplatesForGood(txApp, []string{templateID})
	})
	return response, err
}

// databaseTemplateSubPagesToTrashSQL sends the sub-pages of the templates
// templates selects to the trash on their own, at the top level, as a
// removed row's go (the trigger on leafen_database_rows): the template's
// page they sat under is going.
func databaseTemplateSubPagesToTrashSQL(templates string) string {
	return `UPDATE {{leafen_pages}}
		SET [[parent_id]] = '', [[in_trash]] = 1,
			[[trashed_at]] = CASE WHEN [[in_trash]] = 1 AND trim([[trashed_at]]) != '' THEN [[trashed_at]]
				ELSE strftime('%Y-%m-%dT%H:%M:%fZ', 'now') END
		WHERE [[data_source_id]] = '' AND [[parent_id]] IN (` + templates + `)`
}

// deleteDatabaseTemplatesForGood removes templates and, by its trigger, their
// pages and bodies; their sub-pages go to the trash on their own.
func deleteDatabaseTemplatesForGood(app core.App, templateIDs []string) error {
	for _, id := range templateIDs {
		for _, statement := range []string{
			databaseTemplateSubPagesToTrashSQL(`{:id}`),
			`DELETE FROM {{leafen_database_toolbar_template_state}} WHERE [[template_id]] = {:id}`,
			`DELETE FROM {{leafen_database_templates}} WHERE [[id]] = {:id}`,
		} {
			if _, err := app.DB().NewQuery(statement).Bind(dbx.Params{"id": id}).Execute(); err != nil {
				return err
			}
		}
	}
	if len(templateIDs) == 0 {
		return nil
	}
	return wakeRowPageContentCleanup(app)
}

// deleteExpiredDatabaseTemplateTrashSlice deletes the templates whose time in
// the trash ended, a legal hold on one keeping it.
func deleteExpiredDatabaseTemplateTrashSlice(app core.App, now int64) (int64, error) {
	ids := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[state]].[[template_id]] FROM {{leafen_database_toolbar_template_state}} [[state]]
		INDEXED BY [[idx_leafen_database_toolbar_template_state_trash_expiry]]
		WHERE [[state]].[[deleted_at]] != '' AND ` + databaseTemplateTrashedAtSQL + ` <= {:before}
			AND NOT ` + legalHoldRetainsPageSQL("[[state]].[[template_id]]", "[[state]].[[workspace_id]]") + `
		ORDER BY ` + databaseTemplateTrashedAtSQL + `, [[state]].[[template_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"before": now - databaseRowTrashRetention.Milliseconds(), "limit": databaseRowTrashPurgeSize,
	}).Column(&ids); err != nil {
		return 0, err
	}
	return int64(len(ids)), deleteDatabaseTemplatesForGood(app, ids)
}

// databaseTemplateTrashDueSQL is when the next deleted template's time in the
// trash ends, in milliseconds: the first deleted template along the expiry
// index that no legal hold keeps, a hold judged for each template up to it,
// not for every deleted template (2026-11-18).
var databaseTemplateTrashDueSQL = `SELECT * FROM (
	SELECT ` + databaseTemplateTrashedAtSQL + ` + ` + strconv.FormatInt(databaseRowTrashRetention.Milliseconds(), 10) + `
	FROM {{leafen_database_toolbar_template_state}} [[state]]
	INDEXED BY [[idx_leafen_database_toolbar_template_state_trash_expiry]]
	WHERE [[state]].[[deleted_at]] != '' AND ` + databaseTemplateTrashedAtSQL + ` IS NOT NULL
		AND NOT ` + legalHoldRetainsPageSQL("[[state]].[[template_id]]", "[[state]].[[workspace_id]]") + `
	ORDER BY ` + databaseTemplateTrashedAtSQL + `, [[state]].[[template_id]] LIMIT 1)`
