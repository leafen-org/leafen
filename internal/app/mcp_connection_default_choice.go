package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// Migration 858 kept a default chosen on each connection and on the one-time
// code that makes one (2026-11-23). Since migration 866 no connection keeps
// a default (connection_person_rights.go): a call that needs a workspace
// nothing of it decides names one (mcpWorkspaceNeeded). The columns stay,
// empty.

func ensureNativeConnectionDefaultWorkspaceSchema(app core.App) error {
	for _, table := range []string{mcpConnectionsTable, mcpConnectionCodesTable} {
		if _, err := app.DB().NewQuery(`ALTER TABLE {{` + table + `}}
			ADD COLUMN [[default_workspace_id]] TEXT NOT NULL DEFAULT ''`).Execute(); err != nil {
			return err
		}
	}
	return validateNativeConnectionDefaultWorkspaceSchema(app)
}

func validateNativeConnectionDefaultWorkspaceSchema(app core.App) error {
	var columns int
	if err := app.DB().NewQuery(`SELECT
		(SELECT COUNT(*) FROM pragma_table_info('leafen_mcp_connections') WHERE [[name]] = 'default_workspace_id' AND [[notnull]] = 1) +
		(SELECT COUNT(*) FROM pragma_table_info('leafen_mcp_connection_codes') WHERE [[name]] = 'default_workspace_id' AND [[notnull]] = 1)`).
		Row(&columns); err != nil {
		return err
	}
	if columns != 2 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
