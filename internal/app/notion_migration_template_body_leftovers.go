package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Before 2026-11-24 a template's row forgot, as it went, only its body's
// blocks and the views of its linked databases (clearNotionImportTemplateRowBody):
// a database of the body's own stayed in the migration's ledger — the
// database, its properties and views, its rows — naming what went with the
// row. A run again preloaded them: one stopped at publishing on the row,
// and one that read again a template Notion changed found the database made
// and stopped linking its views. A run's first turn forgets such leftovers
// before it starts from the ledger: of each database and row the ledger
// keeps among what a template's body held (notion_import_counted.go), one
// whose data source or page is gone. What a run makes of a body now leaves
// none (forgetNotionImportTemplateRowDatabases).

// forgetNotionMigrationTemplateBodyLeftovers forgets, in the migration's
// ledger, what the template bodies' own databases left there that is gone.
func forgetNotionMigrationTemplateBodyLeftovers(app core.App, migrationID string) error {
	params := dbx.Params{"migrationId": migrationID, "templateBody": notionMigrationLedgerTemplateBody}
	gone := []struct {
		Key  string `db:"key"`
		Kind string `db:"kind"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[read]].[[key]] AS [[key]], [[made]].[[kind]] AS [[kind]]
		FROM {{leafen_notion_migration_ledger}} AS [[body]]
		JOIN json_each([[body]].[[state]], '$.read') AS [[read]]
		JOIN {{leafen_notion_migration_ledger}} AS [[made]] ON [[made]].[[migration_id]]=[[body]].[[migration_id]]
			AND [[made]].[[source_id]]=[[read]].[[key]] AND [[made]].[[kind]] IN ('data_source','row')
		WHERE [[body]].[[migration_id]]={:migrationId} AND [[body]].[[kind]]={:templateBody} AND json_valid([[body]].[[state]])
			AND CASE [[made]].[[kind]]
				WHEN 'data_source' THEN NOT EXISTS (SELECT 1 FROM {{leafen_data_sources}} WHERE [[id]]=[[made]].[[native_id]])
				ELSE NOT EXISTS (SELECT 1 FROM {{leafen_pages}} WHERE [[id]]=[[made]].[[native_id]]) END
	`).Bind(params).All(&gone); err != nil || len(gone) == 0 {
		return err
	}
	databases, rows := []string{}, []string{}
	for _, item := range gone {
		if item.Kind == "data_source" {
			databases = append(databases, item.Key)
		} else {
			rows = append(rows, item.Key)
		}
	}
	encodedDatabases, err := json.Marshal(databases)
	if err != nil {
		return err
	}
	encodedRows, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	params["databases"], params["rows"] = string(encodedDatabases), string(encodedRows)
	// A database is each key the import staged it by (forgetNotionImportTemplateRowDatabases).
	_, err = app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND (
			([[kind]]='row' AND [[source_id]] IN (SELECT [[value]] FROM json_each({:rows})))
			OR ([[kind]] IN ('data_source','property','view','block') AND EXISTS (
				SELECT 1 FROM json_each({:databases}) AS [[database]]
				WHERE {{leafen_notion_migration_ledger}}.[[source_id]]=[[database]].[[value]]
					OR substr({{leafen_notion_migration_ledger}}.[[source_id]], 1, length([[database]].[[value]])+1)=[[database]].[[value]] || ':')))
	`).Bind(params).Execute()
	return err
}
