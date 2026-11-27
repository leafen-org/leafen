package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// Migration 854 keeps the page origin a sign-in by an agent was asked from
// (agent_sign_in_requests.go), which tells Leafen's own app from a browser
// when the request is named for the person later (clientDeviceSummary).
// Requests asked before it have none and read as the browser their user
// agent names, as they did.

func ensureNativeAgentSignInRequestOriginSchema(app core.App) error {
	if _, err := app.DB().NewQuery(`ALTER TABLE {{leafen_agent_sign_in_requests}}
		ADD COLUMN [[request_origin]] TEXT NOT NULL DEFAULT ''`).Execute(); err != nil {
		return err
	}
	return validateNativeAgentSignInRequestOriginSchema(app)
}

func validateNativeAgentSignInRequestOriginSchema(app core.App) error {
	var columns int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_agent_sign_in_requests')
		WHERE [[name]] = 'request_origin' AND [[notnull]] = 1`).Row(&columns); err != nil {
		return err
	}
	if columns != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
