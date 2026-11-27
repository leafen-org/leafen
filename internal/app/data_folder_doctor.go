package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	sqlite3 "modernc.org/sqlite/lib"
)

// `leafen doctor` says of its data folder what the server's start would: the
// reason it refuses the folder, in the start's own words — one Hanji made
// (retired_product_data.go), data from before the schema baseline
// (schema_history_refusal.go), or data a newer Leafen brought up to date
// (schema_migrations.go) — read without writing anything to the folder
// (2026-11-11: doctor said "ok" of a Hanji folder `serve` refused). The
// start refuses the folder the same way before PocketBase opens it.
func dataFolderRefusal(dataDir string) error {
	if err := retiredProductDataRefusal(dataDir); err != nil {
		return err
	}
	if err := schemaHistoryRefusal(dataDir); err != nil {
		return err
	}
	return appliedMigrationsRefusal(dataDir)
}

// appliedMigrationsRefusal is the start's refusal of data whose migrations
// this program doesn't have, as the database file holds them: a migration
// still only in the write-ahead log, which the start would see, isn't, nor
// is a file a stopped checkpoint left torn, which the start mends first.
func appliedMigrationsRefusal(dataDir string) error {
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
	var tables int
	err = database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
		productMigrationsTable).Scan(&tables)
	if isSQLiteBaseCode(err, sqlite3.SQLITE_CORRUPT, sqlite3.SQLITE_NOTADB) || err == nil && tables == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	rows, err := database.Query(`SELECT version, name, checksum FROM ` + productMigrationsTable + ` ORDER BY version`)
	if err != nil {
		return err
	}
	defer rows.Close()
	applied := []appliedProductMigration{}
	for rows.Next() {
		var row appliedProductMigration
		if err := rows.Scan(&row.Version, &row.Name, &row.Checksum); err != nil {
			return err
		}
		applied = append(applied, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := validateAppliedProductMigrations(applied, productMigrations); err != nil {
		return fmt.Errorf("%s: %w", errorCodeSchemaMigrationHistoryInvalid, err)
	}
	return nil
}
