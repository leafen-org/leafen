package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A row an import makes a template of goes once the template has copies of
// its body (makeNotionImportTemplate), and so does what its body held: its
// blocks, the views of its linked databases, and its own databases with
// their properties, views and rows, and what those rows hold in turn. The
// job's mappings and the migration's ledger forget all of it, so a run again
// neither names what went nor passes by what it reads again of a template
// Notion changed — that body is made anew, as the first run made it
// (notion_import_counted.go). The row's sub-pages are the template's copies'
// now (notion_import_template_sub_pages.go). Before 2026-11-24 the own
// databases' rows were kept: a run again of a move whose template held a
// database with a row preloaded that row, gone with the template's row, and
// stopped at publishing every time ("sql: no rows in result set").

// clearNotionImportTemplateRowBody forgets what the import made in the row's
// body, which goes with the row; the template has its own copies of its
// databases and their views. The view of a linked database the import made
// is its copy's in the template now, which what the reader reads of it
// matches (2026-11-23: the reader read a template's linked view, and the run
// knew no view of it).
func clearNotionImportTemplateRowBody(app core.App, job storedJob, payload notionImportJobPayload, rowID, templateID string) error {
	if err := forgetNotionImportTemplateRowDatabases(app, job, payload, rowID); err != nil {
		return err
	}
	sourceID := rowSourceOf(app, job.ID, rowID)
	params := dbx.Params{
		"jobId": job.ID, "rowId": rowID, "sourceId": sourceID, "migrationId": payload.MigrationID, "template": notionMigrationLedgerTemplate,
		"templateBody": notionMigrationLedgerTemplateBody, "templateId": templateID, "copy": templateID + ":",
	}
	// Its row, the blocks of its body and the views of its linked databases;
	// the template's own entries stay (notion_import_counted.go).
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_migration_ledger}} WHERE [[migration_id]]={:migrationId}
			AND [[kind]] NOT IN ({:template}, {:templateBody}) AND [[source_id]] IN (
			SELECT [[source_id]] FROM {{leafen_notion_import_mappings}} WHERE [[job_id]]={:jobId} AND (
				[[source_id]]={:sourceId}
				OR ([[kind]]='block' AND [[native_id]] IN (SELECT [[id]] FROM {{leafen_blocks}} WHERE [[page_id]]={:rowId}))
				OR ([[kind]]='view' AND [[native_id]] IN (SELECT [[id]] FROM {{leafen_database_views}} WHERE [[page_id]]={:rowId}))))
	`).Bind(params).Execute(); err != nil {
		return err
	}
	// The ledger forgets it, as the template is made again from the row a run
	// again makes; this run's matching takes the copy.
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_mappings}} SET [[native_id]]={:copy} || [[native_id]]
		WHERE [[job_id]]={:jobId} AND [[kind]]='view' AND [[native_id]] IN (SELECT [[id]] FROM {{leafen_database_views}} WHERE [[page_id]]={:rowId})
			AND {:copy} || [[native_id]] IN (SELECT [[id]] FROM {{leafen_database_views}} WHERE [[page_id]]={:templateId})
	`).Bind(params).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_import_mappings}} WHERE [[job_id]]={:jobId} AND (
			([[kind]]='block' AND [[native_id]] IN (SELECT [[id]] FROM {{leafen_blocks}} WHERE [[page_id]]={:rowId}))
			OR ([[kind]]='view' AND [[native_id]] IN (SELECT [[id]] FROM {{leafen_database_views}} WHERE [[page_id]]={:rowId})))
	`).Bind(params).Execute(); err != nil {
		return err
	}
	return nil
}

// forgetNotionImportTemplateRowDatabases forgets, in the job's mappings and
// the migration's ledger, what the row's own databases held, at any depth:
// each database (its data sources, its block, its properties and views), the
// pages of its rows and templates, their blocks and views and sub-pages, and
// the databases those hold. The template's copies of them are its own: a
// copy made by the template's body, as one made by a page from it.
func forgetNotionImportTemplateRowDatabases(app core.App, job storedJob, payload notionImportJobPayload, rowID string) error {
	pages := []string{}
	if err := app.DB().NewQuery(`
		WITH RECURSIVE [[going]]([[id]]) AS (
			SELECT [[page]].[[id]] FROM {{leafen_pages}} AS [[page]]
			WHERE [[page]].[[data_source_id]] IN (SELECT [[id]] FROM {{leafen_data_sources}} WHERE [[page_id]]={:rowId})
			UNION
			SELECT [[page]].[[id]] FROM {{leafen_pages}} AS [[page]] JOIN [[going]]
				ON [[page]].[[parent_id]]=[[going]].[[id]]
					OR [[page]].[[data_source_id]] IN (SELECT [[id]] FROM {{leafen_data_sources}} WHERE [[page_id]]=[[going]].[[id]])
		)
		SELECT [[id]] FROM [[going]]
	`).Bind(dbx.Params{"rowId": rowID}).Column(&pages); err != nil {
		return err
	}
	encodedPages, err := json.Marshal(pages)
	if err != nil {
		return err
	}
	places, err := json.Marshal(append([]string{rowID}, pages...))
	if err != nil {
		return err
	}
	params := dbx.Params{"jobId": job.ID, "pages": string(encodedPages), "places": string(places)}
	// A database is every key the import staged it by: its own ID, and its
	// ID before each of its keys' parts (notionImportDataSourceKey, its
	// block, its properties, its views).
	keys := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='data_source' AND [[native_id]] IN (
			SELECT [[id]] FROM {{leafen_data_sources}} WHERE [[page_id]] IN (SELECT [[value]] FROM json_each({:places})))
	`).Bind(params).Column(&keys); err != nil {
		return err
	}
	databases := map[string]bool{}
	for _, key := range keys {
		database, _, _ := strings.Cut(key, notionImportDataSourceKeyMarker)
		databases[database] = true
	}
	if len(pages) == 0 && len(databases) == 0 {
		return nil
	}
	databaseList := make([]string, 0, len(databases))
	for database := range databases {
		databaseList = append(databaseList, database)
	}
	encodedDatabases, err := json.Marshal(databaseList)
	if err != nil {
		return err
	}
	params["databases"] = string(encodedDatabases)
	forgotten := []struct {
		SourceID string `db:"source_id"`
		Kind     string `db:"kind"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]], [[kind]] FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		WHERE [[job_id]]={:jobId} AND (
			([[kind]] IN ('page','row') AND [[native_id]] IN (SELECT [[value]] FROM json_each({:pages})))
			OR ([[kind]]='block' AND [[native_id]] IN (SELECT [[id]] FROM {{leafen_blocks}}
				WHERE [[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))))
			OR ([[kind]]='view' AND [[native_id]] IN (SELECT [[id]] FROM {{leafen_database_views}}
				WHERE [[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))))
			OR EXISTS (SELECT 1 FROM json_each({:databases}) AS [[database]]
				WHERE [[mapping]].[[source_id]]=[[database]].[[value]]
					OR substr([[mapping]].[[source_id]], 1, length([[database]].[[value]])+1)=[[database]].[[value]] || ':'))
	`).Bind(params).All(&forgotten); err != nil {
		return err
	}
	entries := make([][2]string, len(forgotten))
	for index, entry := range forgotten {
		entries[index] = [2]string{entry.SourceID, entry.Kind}
	}
	encodedEntries, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	params["entries"] = string(encodedEntries)
	if payload.MigrationID != "" {
		params["migrationId"] = payload.MigrationID
		params["template"], params["templateBody"] = notionMigrationLedgerTemplate, notionMigrationLedgerTemplateBody
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_notion_migration_ledger}}
			WHERE [[migration_id]]={:migrationId} AND ` + notionMigrationLedgerEntriesSQL + `
		`).Bind(params).Execute(); err != nil {
			return err
		}
		// The templates an earlier run made of these databases went with
		// them, and what their bodies held with them.
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_notion_migration_ledger}}
			WHERE [[migration_id]]={:migrationId} AND [[kind]] IN ({:template}, {:templateBody}) AND [[source_id]] IN (
				SELECT [[source_id]] FROM {{leafen_notion_migration_ledger}}
				WHERE [[migration_id]]={:migrationId} AND [[kind]]={:template}
					AND [[native_id]] IN (SELECT [[value]] FROM json_each({:pages})))
		`).Bind(params).Execute(); err != nil {
			return err
		}
	}
	_, err = app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND ` + notionMigrationLedgerEntriesSQL + `
	`).Bind(params).Execute()
	return err
}

// rowSourceOf is the staged row an imported row came from, or "".
func rowSourceOf(app core.App, jobID, rowID string) string {
	sourceID := ""
	_ = app.DB().NewQuery(`
		SELECT [[source_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='row' AND [[native_id]]={:rowId}
	`).Bind(dbx.Params{"jobId": jobID, "rowId": rowID}).Row(&sourceID)
	return sourceID
}
