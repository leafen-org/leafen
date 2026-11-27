package app

import (
	"errors"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// An internal connection's token made again can leave the one it replaces
// working seven more days, for the integrations that hold it to be given the
// new one, as Notion's "7일 후에 철회" (seen 2026-11-21); "즉시 철회" stops
// it at once. The old token's hash and until when it works sit beside the
// new one's; a later refresh drops it.

const internalConnectionRevokeLaterMillis = int64(7 * 24 * time.Hour / time.Millisecond)

// Migration 838 adds them, empty for every connection.

func ensureNativeInternalConnectionTokenGraceSchema(app core.App) error {
	for _, statement := range []string{
		`ALTER TABLE {{leafen_notion_internal_connections}} ADD COLUMN [[previous_token_hash]] TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE {{leafen_notion_internal_connections}} ADD COLUMN [[previous_token_until]] INTEGER NOT NULL DEFAULT 0
			CHECK ([[previous_token_until]] >= 0)`,
		`CREATE INDEX [[idx_leafen_notion_internal_connections_previous_token]]
			ON {{leafen_notion_internal_connections}} ([[previous_token_hash]]) WHERE [[previous_token_hash]] <> ''`,
	} {
		if _, err := app.DB().NewQuery(statement).Execute(); err != nil {
			return err
		}
	}
	return validateNativeInternalConnectionTokenGraceSchema(app)
}

func validateNativeInternalConnectionTokenGraceSchema(app core.App) error {
	var columns, indexes int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_notion_internal_connections')
		WHERE [[name]] IN ('previous_token_hash', 'previous_token_until') AND [[notnull]] = 1`).Row(&columns); err != nil {
		return err
	}
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM [[sqlite_master]]
		WHERE [[type]] = 'index' AND [[name]] = 'idx_leafen_notion_internal_connections_previous_token'`).Row(&indexes); err != nil {
		return err
	}
	if columns != 2 || indexes != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
