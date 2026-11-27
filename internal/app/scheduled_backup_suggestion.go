package app

import (
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Scheduled backups are off until an administrator turns them on, so a
// server whose data is worth keeping suggests it once (2026-10-17: Leafen
// keeps no backups for anyone; they go to the person's own storage, such as
// a cloud drive's folder): the app shows its administrators a band to turn
// them on, until one of them turns backups on or dismisses it. Worth keeping
// is a page of one's own a week after the first workspace, pages brought in
// from Notion among them: the first week is for finding one's way, without a
// band over every page (user decision 2026-11-09; until then a day after ten
// pages, so an import showed it the next day). Migration 544 keeps when the
// suggestion was dismissed.

const scheduledBackupSuggestionAge = 7 * 24 * time.Hour

// scheduledBackupTurnOnSuggested reports whether the band suggesting
// backups shows: they are off, the screen can turn them on, nobody dismissed
// it, and the data is worth keeping.
func scheduledBackupTurnOnSuggested(app core.App, stored storedScheduledBackup, now time.Time) bool {
	if stored.Enabled || stored.SetByEnvironment || stored.SuggestionDismissedAt > 0 {
		return false
	}
	// Two pages are the welcome page and one of one's own, counted as the
	// server's pages are shown (serverHoldsCounts): People's pages, which
	// the server makes with every workspace, are none of them.
	var oldWorkspaces int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspaces}} WHERE [[created]] <= {:cutoff}`).Bind(dbx.Params{
		"cutoff": now.Add(-scheduledBackupSuggestionAge).UTC().Format("2006-01-02 15:04:05.000Z"),
	}).Row(&oldWorkspaces)
	if err != nil || oldWorkspaces == 0 {
		return false
	}
	pages, _ := serverHoldsCounts(app)
	return pages >= 2
}

// dismissScheduledBackupSuggestion keeps the band from showing again, for
// every administrator; the backup settings still turn backups on.
func dismissScheduledBackupSuggestion(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	if _, err := event.App.DB().NewQuery(`
		UPDATE {{leafen_scheduled_backups}} SET [[suggestion_dismissed_at]] = {:now}
		WHERE [[id]] = 'default' AND [[suggestion_dismissed_at]] = 0
	`).Bind(dbx.Params{"now": time.Now().UnixMilli()}).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the backup schedule.", err)
	}
	stored, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	return event.JSON(http.StatusOK, administratorScheduledBackupResult{Schedule: projectScheduledBackupWithRestore(event.App, stored)})
}
