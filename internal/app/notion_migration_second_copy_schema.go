package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// Migration 865 keeps whether the person started a Notion move as a second,
// separate copy of roots an earlier move already brought into the workspace
// (notion_migration_moved_before.go), so its first run isn't refused as
// bringing them twice. Moves started before it read as no copy.

func ensureNativeNotionMigrationSecondCopySchema(app core.App) error {
	if _, err := app.DB().NewQuery(`ALTER TABLE {{leafen_notion_migrations}}
		ADD COLUMN [[second_copy]] INTEGER NOT NULL DEFAULT 0 CHECK ([[second_copy]] IN (0, 1))`).Execute(); err != nil {
		return err
	}
	return validateNativeNotionMigrationSecondCopySchema(app)
}

func validateNativeNotionMigrationSecondCopySchema(app core.App) error {
	var columns int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_notion_migrations')
		WHERE [[name]] = 'second_copy' AND [[notnull]] = 1 AND [[dflt_value]] = '0'`).Row(&columns); err != nil {
		return err
	}
	if columns != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
