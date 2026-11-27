package app

import (
	"encoding/json"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// A scheduled backup's last run as it ended — the step it got to, the bytes
// and files it went through of all it had, what it sent, and when it
// started and ended — stays with the schedule, so the status shows it after
// the server starts again too (user 2026-11-22: a first backup ran 46
// minutes and left only its time).
func ensureNativeScheduledBackupLastRunSchema(app core.App) error {
	if _, err := app.DB().NewQuery(`ALTER TABLE {{leafen_scheduled_backups}}
		ADD COLUMN [[last_run_json]] TEXT NOT NULL DEFAULT ''`).Execute(); err != nil {
		return err
	}
	return validateNativeScheduledBackupLastRunSchema(app)
}

func validateNativeScheduledBackupLastRunSchema(app core.App) error {
	var columns int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_scheduled_backups')
		WHERE [[name]] = 'last_run_json' AND [[notnull]] = 1`).Row(&columns); err != nil {
		return err
	}
	if columns != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}

// scheduledBackupLastRunJSON is a run's record as it is kept.
func scheduledBackupLastRunJSON(run administratorBackupProgress) string {
	raw, err := json.Marshal(run)
	if err != nil {
		return ""
	}
	return string(raw)
}

// projectScheduledBackupLastRun is the kept record, nil for none.
func projectScheduledBackupLastRun(raw string) *administratorBackupProgress {
	if raw == "" {
		return nil
	}
	run := administratorBackupProgress{}
	if json.Unmarshal([]byte(raw), &run) != nil || run.Kind != backupProgressKindBackup {
		return nil
	}
	return &run
}
