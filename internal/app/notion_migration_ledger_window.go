package app

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A run's ledger takes what its job made a window of the job's mappings at a
// time, each window a statement of its own, so between them the one write
// connection is free for people's presses and agents' calls; a mapping the
// ledger already keeps as it is is passed over (2026-11-16: the ledger was
// one statement over every mapping of the run, and the run's end wrote each
// of a 31,000-block run's rows again inside its last transaction; agents'
// calls and people's saves waited 1.8 and 2 to 4.6 s behind them on a
// NAS-class server. The run's end now leaves it to the ledger step,
// finishNotionMigrationRun). A run that takes over a stopped one records the
// stopped job's ledger and preloads its own job from the ledger the same
// way, in its job's first turn (notion_migration_run_ledger_start.go).

// Each window is sized by how long the one before held the write connection
// (notionImportTurnHold), from notionMigrationLedgerFirstWindow up to
// notionMigrationLedgerRecordWindow entries: a fixed window of 200 held it 0.5
// to 0.7 s on a NAS-class server; sized, windows of a stopped 15,000-block
// run held it at most 0.12 s there (2026-11-16 — a cap of 1,000 held it up to
// 0.25 s and didn't start the run sooner).
const (
	notionMigrationLedgerRecordWindow = 200
	notionMigrationLedgerFirstWindow  = 50
)

// recordNotionMigrationLedgerMappings keeps in the ledger what each of the
// job's mappings became. A block keeps what the ledger knew of it; one new to
// it, or made anew, waits for the run's end to say how the run left it.
func recordNotionMigrationLedgerMappings(app core.App, migrationID, jobID string) error {
	after, window := "", notionMigrationLedgerFirstWindow
	for {
		var last sql.NullString
		if err := app.DB().NewQuery(`
			SELECT MAX([[source_id]]) FROM (
				SELECT [[source_id]] FROM {{leafen_notion_import_mappings}}
				WHERE [[job_id]]={:jobId} AND [[source_id]]>{:after}
				ORDER BY [[source_id]] LIMIT {:window})
		`).Bind(dbx.Params{"jobId": jobID, "after": after, "window": window}).Row(&last); err != nil {
			return err
		}
		if !last.Valid || last.String == "" {
			return nil
		}
		start := time.Now()
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_notion_migration_ledger}}
				([[migration_id]],[[workspace_id]],[[source_id]],[[kind]],[[native_id]],[[page_id]],[[state]])
			SELECT {:migrationId}, [[migration]].[[workspace_id]], [[mapping]].[[source_id]], [[mapping]].[[kind]],
				[[mapping]].[[native_id]],
				CASE WHEN [[mapping]].[[kind]]='block' THEN COALESCE(
					(SELECT [[block]].[[page_id]] FROM {{leafen_blocks}} AS [[block]] WHERE [[block]].[[id]]=[[mapping]].[[native_id]]), '')
				ELSE '' END, ''
			FROM {{leafen_notion_import_mappings}} AS [[mapping]]
			JOIN {{leafen_notion_migrations}} AS [[migration]] ON [[migration]].[[id]]={:migrationId}
			WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[source_id]]>{:after} AND [[mapping]].[[source_id]]<={:last}
				AND NOT EXISTS (SELECT 1 FROM {{leafen_notion_migration_ledger}} AS [[kept]]
					WHERE [[kept]].[[migration_id]]={:migrationId} AND [[kept]].[[source_id]]=[[mapping]].[[source_id]]
						AND [[kept]].[[kind]]=[[mapping]].[[kind]] AND [[kept]].[[native_id]]=[[mapping]].[[native_id]])
			ON CONFLICT ([[migration_id]],[[source_id]],[[kind]]) DO UPDATE SET
				[[page_id]]=excluded.[[page_id]], [[state]]='', [[native_id]]=excluded.[[native_id]]
		`).Bind(dbx.Params{"migrationId": migrationID, "jobId": jobID, "after": after, "last": last.String}).Execute(); err != nil {
			return err
		}
		after, window = last.String, notionHoldSizedWindow(window, notionMigrationLedgerRecordWindow, time.Since(start))
	}
}

// eachNotionMigrationLedgerWindow hands each window of the migration's
// ledger in turn, by the Notion ids after and up to which it runs.
func eachNotionMigrationLedgerWindow(app core.App, migrationID string, each func(after, last string) error) error {
	after, window := "", notionMigrationLedgerFirstWindow
	for {
		var last sql.NullString
		if err := app.DB().NewQuery(`
			SELECT MAX([[source_id]]) FROM (
				SELECT [[source_id]] FROM {{leafen_notion_migration_ledger}}
				WHERE [[migration_id]]={:migrationId} AND [[source_id]]>{:after}
				ORDER BY [[source_id]] LIMIT {:window})
		`).Bind(dbx.Params{"migrationId": migrationID, "after": after, "window": window}).Row(&last); err != nil {
			return err
		}
		if !last.Valid || last.String == "" {
			return nil
		}
		start := time.Now()
		if err := each(after, last.String); err != nil {
			return err
		}
		after, window = last.String, notionHoldSizedWindow(window, notionMigrationLedgerRecordWindow, time.Since(start))
	}
}

// notionMigrationLedgerEntriesSQL keeps, of the migration's ledger, the
// entries of a window: {:entries} is a JSON list of [Notion id, kind].
const notionMigrationLedgerEntriesSQL = `([[source_id]],[[kind]]) IN (
	SELECT json_extract([[value]],'$[0]'), json_extract([[value]],'$[1]') FROM json_each({:entries}))`

// eachNotionMigrationLedgerEntries hands, a window at a time, the migration's
// ledger entries of the kinds given (read first, off the write connection),
// as notionMigrationLedgerEntriesSQL binds them: a statement over every
// entry of those kinds went through the whole ledger, its blocks too, at once.
func eachNotionMigrationLedgerEntries(app core.App, migrationID string, kinds []string, each func(entries string) error) error {
	encodedKinds, err := json.Marshal(kinds)
	if err != nil {
		return err
	}
	entries := []struct {
		SourceID string `db:"source_id"`
		Kind     string `db:"kind"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]],[[kind]] FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]] IN (SELECT [[value]] FROM json_each({:kinds}))
		ORDER BY [[source_id]],[[kind]]
	`).Bind(dbx.Params{"migrationId": migrationID, "kinds": string(encodedKinds)}).All(&entries); err != nil {
		return err
	}
	for window := notionMigrationLedgerFirstWindow; len(entries) > 0; {
		count := min(window, len(entries))
		keys := make([][2]string, count)
		for index, entry := range entries[:count] {
			keys[index] = [2]string{entry.SourceID, entry.Kind}
		}
		encoded, err := json.Marshal(keys)
		if err != nil {
			return err
		}
		start := time.Now()
		if err := each(string(encoded)); err != nil {
			return err
		}
		entries, window = entries[count:], notionHoldSizedWindow(count, notionMigrationLedgerRecordWindow, time.Since(start))
	}
	return nil
}
