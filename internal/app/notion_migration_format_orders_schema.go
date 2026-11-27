package app

import (
	"errors"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Migration 852 lets a migration's kept reading hold each rows' order once,
// for the views that have it (kind order, takeNotionMigrationFormats), not
// once a view (2026-11-23: 10,000 rows with four linked views each, a
// hundred rows in order, kept 165 MB of the same order). The kinds the table
// takes are SQLite's own check, so the table is made again with 'order'
// among them, what it held copied over, and its index made again.

const nativeNotionMigrationFormatOrdersChecksum = "ad2c6ca5869ba058cef6cbdcc2e9b3a4c9cb3cdb60ccc50b8c15ce24b49ad76f"

func ensureNativeNotionMigrationFormatOrdersSchema(app core.App) error {
	for _, statement := range []string{
		"CREATE TABLE `leafen_notion_migration_formats_next` (" +
			"`migration_id` TEXT NOT NULL REFERENCES `leafen_notion_migrations` (`id`) ON DELETE CASCADE, " +
			"`workspace_id` TEXT NOT NULL REFERENCES `leafen_workspaces` (`id`) ON DELETE CASCADE, " +
			"`kind` TEXT NOT NULL CHECK (`kind` IN ('reader', 'page', 'database', 'view', 'order')), " +
			"`notion_id` TEXT NOT NULL, " +
			"`value` TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(`value`)), " +
			"PRIMARY KEY (`migration_id`, `kind`, `notion_id`))",
		"INSERT INTO `leafen_notion_migration_formats_next` (`migration_id`, `workspace_id`, `kind`, `notion_id`, `value`) " +
			"SELECT `migration_id`, `workspace_id`, `kind`, `notion_id`, `value` FROM `leafen_notion_migration_formats`",
		"DROP TABLE `leafen_notion_migration_formats`",
		"ALTER TABLE `leafen_notion_migration_formats_next` RENAME TO `leafen_notion_migration_formats`",
		"CREATE INDEX `idx_leafen_workspace_deletion_notion_migration_formats` ON `leafen_notion_migration_formats` (`workspace_id`)",
	} {
		if _, err := app.DB().NewQuery(statement).Execute(); err != nil {
			return err
		}
	}
	return validateNativeNotionMigrationFormatOrdersSchema(app)
}

func validateNativeNotionMigrationFormatOrdersSchema(app core.App) error {
	var table string
	var indexes int
	if err := app.DB().NewQuery("SELECT COALESCE((SELECT `sql` FROM `sqlite_master` WHERE `type` = 'table' AND `name` = 'leafen_notion_migration_formats'), '')").
		Row(&table); err != nil {
		return err
	}
	if err := app.DB().NewQuery("SELECT COUNT(*) FROM `sqlite_master` WHERE `type` = 'index' AND `name` = 'idx_leafen_workspace_deletion_notion_migration_formats'").
		Row(&indexes); err != nil {
		return err
	}
	if !strings.Contains(table, "'order'") || indexes != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
