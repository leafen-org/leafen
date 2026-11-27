package app

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	sqlite3 "modernc.org/sqlite/lib"
)

// Data from before the schema baseline (schema_baseline.go) is not opened
// (user decision 2026-11-13): a data folder, backup, folder snapshot, replica
// or server moved in whose database kept the history of migrations 1 to 831
// one by one — or none at all, from before that history began — is refused
// with errSchemaHistoryBeforeBaseline and left as it is, rather than brought
// up to date by migrations this Leafen no longer holds. What a workspace
// move, a workspace export or a Notion export carries is unaffected: those
// are read by their own formats, not as a database.
var errSchemaHistoryBeforeBaseline = errors.New("made by an earlier Leafen, whose schema history runs migration by migration " +
	"from version 1; this Leafen keeps that history as one baseline step and does not open such data")

// schemaHistoryBringOver is what a person does with such data instead.
const schemaHistoryBringOver = "bring the pages over by moving or exporting each workspace from the earlier Leafen"

// errSchemaHistoryNewer is data a newer Leafen brought up to date: its
// ledger holds migrations this Leafen doesn't have.
var errSchemaHistoryNewer = errors.New("brought up to date by a newer Leafen than this one")

// schemaHistoryBeforeBaseline says whether a migration ledger holds the
// history from before the baseline.
func schemaHistoryBeforeBaseline(applied []appliedProductMigration) bool {
	for _, row := range applied {
		if row.Version < schemaBaselineVersion {
			return true
		}
	}
	return false
}

// schemaHistoryBeforeBaselineRecord is how the first migration of that
// history sits in a database page: its name and checksum side by side in the
// row. A ledger still only in a write-ahead log is found by it.
var schemaHistoryBeforeBaselineRecord = regexp.MustCompile("initial_native_schema" +
	"6f7055a75ea8049fae3a0ae8b93b80ede2c9b955da1cc4237e964541865c1af8")

// schemaHistoryRefusal is why the database in a folder — a data folder, or
// one a restore put together — is refused as data from before the baseline,
// or nil. It reads without writing anything, not even the index SQLite keeps
// beside a write-ahead log.
func schemaHistoryRefusal(dataDir string) error {
	path := filepath.Join(dataDir, "data.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	database, err := openDatabaseUnchanged(path)
	if err != nil {
		return err
	}
	defer database.Close()
	before, settled, err := databaseHistoryBeforeBaseline(database)
	// A torn database file says nothing until SQLite mends it, and a new
	// one may hold its ledger only in the write-ahead log: the log is read
	// for the old history alone (retired_product_data.go).
	if isSQLiteBaseCode(err, sqlite3.SQLITE_CORRUPT, sqlite3.SQLITE_NOTADB) || err == nil && !settled {
		before, err = logHoldsPattern(path+"-wal", schemaHistoryBeforeBaselineRecord)
	}
	if err != nil {
		return fmt.Errorf("reading which Leafen made the data in %s: %w", dataDir, err)
	}
	if before {
		return fmt.Errorf("the data folder %s is left as it is: it was %w. Start this Leafen with a new data folder, and %s",
			dataDir, errSchemaHistoryBeforeBaseline, schemaHistoryBringOver)
	}
	return nil
}

// databaseHistoryBeforeBaseline reads a database file's ledger; settled is
// false when the file holds neither a ledger nor product tables.
func databaseHistoryBeforeBaseline(database *sql.DB) (before bool, settled bool, err error) {
	var ledger, pages int
	if err := database.QueryRow(`SELECT
		(SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?),
		(SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?)`,
		productMigrationsTable, pagesCollection).Scan(&ledger, &pages); err != nil {
		return false, false, err
	}
	if ledger == 0 {
		// Product tables without a ledger are from before the ledger began.
		return pages > 0, pages > 0, nil
	}
	var older int
	err = database.QueryRow(`SELECT COUNT(*) FROM `+productMigrationsTable+` WHERE version < ?`, schemaBaselineVersion).Scan(&older)
	return older > 0, true, err
}

// restoreRefused is a restore put together that holds what the start would
// refuse (dataFolderRefusal): it isn't left for the next start to move in,
// and the person is told why in words of their own — by the screens through
// code, which each restore's screen says in its own words, and in message.
// A refusal with no code of its own is said by the code of the place it came
// from (a backup that can't be opened, a replica, a move).
type restoreRefused struct {
	code    string
	message string
	cause   error
}

func (refused restoreRefused) Error() string { return refused.cause.Error() }
func (refused restoreRefused) Unwrap() error { return refused.cause }

// codeOr is the refusal's own error code, or otherwise the place's.
func (refused restoreRefused) codeOr(place string) string {
	if refused.code != "" {
		return refused.code
	}
	return place
}

// stagedRestoreRefusal refuses what a restore put together in staged holds,
// as the start would: data from before the schema baseline and data a newer
// Leafen brought up to date each have their own code, as what a person does
// about them differs (move the workspaces over from the earlier Leafen, or
// update this one first).
func stagedRestoreRefusal(staged string) error {
	err := dataFolderRefusal(staged)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errSchemaHistoryBeforeBaseline):
		return restoreRefused{errorCodeDataBeforeSchemaBaseline,
			"it holds data from an earlier Leafen, which this version doesn't open; " + schemaHistoryBringOver, err}
	case errors.Is(err, errSchemaHistoryNewer):
		return restoreRefused{errorCodeDataFromNewerLeafen,
			"it holds data a newer Leafen brought up to date; update this Leafen first", err}
	default:
		return restoreRefused{"", "it holds data this Leafen doesn't open", err}
	}
}

// checkRestoredDataRoot refuses a restore put together for the command line
// that holds no database, or what the start would refuse — data from before
// the schema baseline, say — before it takes the data folder's place.
func checkRestoredDataRoot(extractedRoot string) error {
	if info, err := os.Stat(filepath.Join(extractedRoot, "data.db")); err != nil || !info.Mode().IsRegular() {
		return errors.New("backup root data.db is missing")
	}
	err := dataFolderRefusal(extractedRoot)
	if errors.Is(err, errSchemaHistoryBeforeBaseline) {
		return fmt.Errorf("the backup is not restored, and this folder is left as it was: the backup was %w; %s",
			errSchemaHistoryBeforeBaseline, schemaHistoryBringOver)
	}
	return err
}
