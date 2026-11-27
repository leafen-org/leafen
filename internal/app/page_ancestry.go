package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's ancestry runs through its parent pages up to the workspace. A
// database page's parent is its data source, so its ancestry continues at
// the page holding its database, or for a page of a wiki at the page it sits
// under in the wiki; a sub-page of a database page reaches that page through
// the database page. A database page is in the trash while its row, or its
// data source, is.

// pageAncestryParentSQL names the page above the page aliased: its parent
// page, for a page of a wiki the page it sits under in the wiki, or for
// another database page the page holding its database.
func pageAncestryParentSQL(alias string) string {
	return `(CASE WHEN ` + alias + `.[[data_source_id]] = '' THEN ` + alias + `.[[parent_id]]
		ELSE COALESCE((SELECT CASE WHEN [[ancestry_source]].[[wiki]] = 1
				AND COALESCE([[ancestry_row]].[[subitem_parent_id]], '') != ''
				THEN [[ancestry_row]].[[subitem_parent_id]] ELSE [[ancestry_source]].[[page_id]] END
			FROM {{leafen_data_sources}} AS [[ancestry_source]]
			LEFT JOIN {{leafen_database_rows}} AS [[ancestry_row]] ON [[ancestry_row]].[[id]] = ` + alias + `.[[id]]
			WHERE [[ancestry_source]].[[id]] = ` + alias + `.[[data_source_id]]), '') END)`
}

// pageInTrashSQL reports whether the page aliased is in the trash itself:
// a page by its own trash state, a database page by its row's or its data
// source's, a deleted database's source too (notion_deleted_database_reads.go),
// and a template's page while its template is deleted, which its sub-pages
// go to the trash with (database_template_trash.go).
func pageInTrashSQL(alias string) string {
	return `(CASE WHEN ` + alias + `.[[data_source_id]] = '' THEN ` + alias + `.[[in_trash]] = 1
		ELSE NOT ` + databaseRowNotTrashedSQL(alias+`.[[id]]`) + `
			OR NOT ` + databaseSourceNotTrashedSQL(alias+`.[[data_source_id]]`) + `
			OR ` + databaseSourceDeletedSQL(alias+`.[[data_source_id]]`) + `
			OR (` + alias + `.[[` + databaseTemplatePageField + `]] AND EXISTS (
				SELECT 1 FROM {{leafen_database_toolbar_template_state}} AS [[template_trash]]
				WHERE [[template_trash]].[[template_id]] = ` + alias + `.[[id]] AND [[template_trash]].[[deleted_at]] != '')) END)`
}

// pageTrashedAtSQL names when the page aliased went to the trash itself.
func pageTrashedAtSQL(alias string) string {
	return `(CASE WHEN ` + alias + `.[[data_source_id]] = '' THEN ` + alias + `.[[trashed_at]]
		ELSE COALESCE((SELECT strftime('%Y-%m-%dT%H:%M:%fZ', [[row_trash_time]].[[trashed_at]] / 1000.0, 'unixepoch')
			FROM {{leafen_database_row_trash}} AS [[row_trash_time]]
			WHERE [[row_trash_time]].[[row_id]] = ` + alias + `.[[id]]), '') END)`
}

// syncDatabaseRowSubPageTeamspaces puts the sub-pages under the given
// database pages, at any depth, in their database page's teamspace or
// private pages, as when a row moves to a page in another teamspace.
func syncDatabaseRowSubPageTeamspaces(app core.App, rowIDs []string) error {
	for _, rowID := range rowIDs {
		if _, err := app.DB().NewQuery(`
			WITH RECURSIVE [[descendants]] ([[id]]) AS (
				SELECT [[id]] FROM {{leafen_pages}} WHERE [[parent_id]] = {:rowId} AND [[data_source_id]] = ''
				UNION
				-- From each found page to its children by key: SQLite's own
				-- order read every page, given the nightly statistics (2026-10-18).
				SELECT [[page]].[[id]] FROM [[descendants]]
				CROSS JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[parent_id]] = [[descendants]].[[id]]
				WHERE [[page]].[[data_source_id]] = ''
			)
			UPDATE {{leafen_pages}} SET
				[[teamspace_id]] = (SELECT [[row_page]].[[teamspace_id]] FROM {{leafen_pages}} AS [[row_page]] WHERE [[row_page]].[[id]] = {:rowId}),
				[[private_owner_id]] = (SELECT [[row_page]].[[private_owner_id]] FROM {{leafen_pages}} AS [[row_page]] WHERE [[row_page]].[[id]] = {:rowId})
			WHERE [[id]] IN (SELECT [[id]] FROM [[descendants]])
		`).Bind(dbx.Params{"rowId": rowID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
