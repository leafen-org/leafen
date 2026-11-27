package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// Migration 855 keeps, on an agent's request to connect, the connection it
// widens when the agent asked with that connection's key
// (mcp_device_connection_widen.go): allowed, that connection gets more and
// no other is made. Requests asked before it widen none and make a
// connection, as they did.

func ensureNativeDeviceConnectionWidensSchema(app core.App) error {
	if _, err := app.DB().NewQuery(`ALTER TABLE {{leafen_mcp_device_connections}}
		ADD COLUMN [[widens_connection]] TEXT NOT NULL DEFAULT ''`).Execute(); err != nil {
		return err
	}
	return validateNativeDeviceConnectionWidensSchema(app)
}

func validateNativeDeviceConnectionWidensSchema(app core.App) error {
	var columns int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_mcp_device_connections')
		WHERE [[name]] = 'widens_connection' AND [[notnull]] = 1`).Row(&columns); err != nil {
		return err
	}
	if columns != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
