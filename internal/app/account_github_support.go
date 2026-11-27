package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// The workspace menu's "GitHub에서 리펜 응원하기" line (user decision
// 2026-11-25, where the sponsor credit was) shows to a person until they
// press it once; the account keeps that it was pressed, beside its display
// preferences, so it shows on none of their devices again
// (account_display_preferences.go carries it as githubSupportOpened).
// LEAFEN_SPONSOR_CREDIT=off takes the line away for everyone
// (sponsor_credit.go).

func ensureNativeAccountGitHubSupportSchema(app core.App) error {
	if _, err := app.DB().NewQuery(`ALTER TABLE {{leafen_account_display_preferences}}
		ADD COLUMN [[github_support_opened]] INTEGER NOT NULL DEFAULT 0 CHECK ([[github_support_opened]] IN (0, 1))`).Execute(); err != nil {
		return err
	}
	return validateNativeAccountGitHubSupportSchema(app)
}

func validateNativeAccountGitHubSupportSchema(app core.App) error {
	var columns int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_account_display_preferences')
		WHERE [[name]] = 'github_support_opened' AND [[notnull]] = 1`).Row(&columns); err != nil {
		return err
	}
	if columns != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
