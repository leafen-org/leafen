package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	gopath "path"
	"time"
)

// A snapshot's database was copied partway through the backup that made
// it, so its schedule names the backup before as the last one (2026-11-22:
// right after a restore "마지막 백업", lastRun and the number kept pointed
// one backup back). Before it comes in, a restored database is told the
// backup it came from: its name and list's SHA-256, when it was made, its
// size and files, and how many snapshots its place keeps now.

// noteRestoredBackup writes the backup a snapshot is into the schedule of
// its put-together database.
func noteRestoredBackup(database string, source scheduledBackupSnapshotSource, name string) error {
	made, err := time.Parse(time.RFC3339, source.manifest.CreatedAt)
	if err != nil {
		return nil
	}
	bytes := source.manifest.Database.Bytes
	for _, file := range source.manifest.Files {
		bytes += file.Bytes
	}
	files := len(source.manifest.Files) + 1
	// What it sent and how long it took went with the run that made it.
	run, err := json.Marshal(administratorBackupProgress{
		Kind: backupProgressKindBackup, Step: backupProgressStepDone, FinishedAt: made.UTC().Format(time.RFC3339),
		BytesDone: bytes, BytesTotal: bytes, FilesDone: files, FilesTotal: files,
	})
	if err != nil {
		return err
	}
	kept := restoredBackupPlaceKeeps(source)
	db, err := sql.Open("sqlite", "file:"+database+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE leafen_scheduled_backups SET last_snapshot = ?, last_snapshot_sha256 = ?,
		last_success_at = ?, last_finished_at = ?, last_added_bytes = 0, last_error_code = '',
		snapshot_count = CASE WHEN ? > 0 THEN ? ELSE snapshot_count END WHERE id = 'default'`,
		name, source.manifestSHA256, made.UnixMilli(), made.UnixMilli(), kept, kept)
	if err == nil {
		// A database from before the last run was kept with the schedule
		// (schema 849) gains the column as it starts.
		var columns int
		if err = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('leafen_scheduled_backups') WHERE name = 'last_run_json'`).Scan(&columns); err == nil && columns == 1 {
			_, err = db.Exec(`UPDATE leafen_scheduled_backups SET last_run_json = ? WHERE id = 'default'`, string(run))
		}
	}
	return errors.Join(err, db.Close())
}

// restoredBackupPlaceKeeps is how many snapshots the place keeps, 0 where
// it can't be told.
func restoredBackupPlaceKeeps(source scheduledBackupSnapshotSource) int {
	entries, err := source.store.List(gopath.Dir(source.snapshot))
	if err != nil {
		return 0
	}
	kept := 0
	for _, entry := range entries {
		if entry.Dir && restoreSnapshotNameValid(entry.Name) {
			kept++
		}
	}
	return kept
}
