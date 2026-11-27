package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a migration's ledger knows of the templates its runs made
// (notion_import_templates.go): the template each Notion template came to,
// and whether a person deleted that one for good since.

// notionMigrationTemplateNative is the template a migration's earlier run
// made of a Notion template, if one did.
func notionMigrationTemplateNative(app core.App, migrationID, sourceID string) (string, error) {
	if migrationID == "" {
		return "", nil
	}
	made := ""
	err := app.DB().NewQuery(`
		SELECT [[native_id]] FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[source_id]]={:sourceId} AND [[kind]]={:kind}
	`).Bind(dbx.Params{"migrationId": migrationID, "sourceId": sourceID, "kind": notionMigrationLedgerTemplate}).Row(&made)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return made, err
}

// notionMigrationTemplateGone says whether the template an earlier run made
// was deleted for good since: a run again doesn't read its Notion template
// again, as nothing in Leafen takes what it holds, and it stays gone as a
// page or row a person deleted does. One in the trash is read and takes
// Notion's change there (updateNotionImportTemplate).
func notionMigrationTemplateGone(app core.App, templateID string) (bool, error) {
	kept := false
	err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_database_templates}} WHERE [[id]]={:id})`).
		Bind(dbx.Params{"id": templateID}).Row(&kept)
	return !kept, err
}
