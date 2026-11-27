package app

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

const (
	productMigrationsTable                 = "leafen_schema_migrations"
	errorCodeSchemaMigrationFailed         = "LEAFEN_SCHEMA_MIGRATION_FAILED"
	errorCodeSchemaMigrationHistoryInvalid = "LEAFEN_SCHEMA_MIGRATION_HISTORY_INVALID"
	errorCodeSchemaMigrationRecoveryFailed = "LEAFEN_SCHEMA_MIGRATION_RECOVERY_FAILED"
)

var initialNativeSchemaTables = []string{
	"leafen_workspaces",
	"leafen_user_state",
	"leafen_pages",
	"leafen_mutation_receipts",
	"leafen_files",
	"leafen_ygo_documents",
	"leafen_jobs",
	"leafen_job_effects",
	"leafen_blocks",
	"leafen_data_sources",
	"leafen_database_views",
	"leafen_database_properties",
	"leafen_database_rows",
	"leafen_database_row_values",
	"leafen_graph_mutations",
}

type appliedProductMigration struct {
	Version  int    `db:"version"`
	Name     string `db:"name"`
	Checksum string `db:"checksum"`
}

// migrationTurnLockName is in the backups folder, which isn't backed up or
// counted against a folder being fresh for a restore.
const (
	migrationTurnLockName = ".leafen-migration.lock"
	migrationTurnWait     = 30 * time.Minute
	migrationTurnSaidEach = 15 * time.Second
)

// takeMigrationTurn has processes on one data folder take its migrations in
// turn: a server bringing its data up and a command run meanwhile (`account
// device-code` once the screen says to, say) otherwise both make the recovery
// archive, and one stops on the other's (2026-11-05: a server stopped with a
// disk I/O error that way). The one that waits finds nothing left to do.
func takeMigrationTurn(dataDir string) (func(), error) {
	root := filepath.Join(dataDir, core.LocalBackupsDirName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	file, err := openBackupLockFile(filepath.Join(root, migrationTurnLockName))
	if err != nil {
		return nil, err
	}
	started := time.Now()
	said := started
	for {
		unlock, taken, err := tryExclusiveBackupFileLock(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if taken {
			return func() {
				_ = unlock()
				_ = file.Close()
			}, nil
		}
		if time.Since(started) > migrationTurnWait {
			_ = file.Close()
			return nil, errors.New("another Leafen process is still bringing this data folder up to date")
		}
		if time.Since(said) >= migrationTurnSaidEach {
			said = time.Now()
			fmt.Fprintln(os.Stderr, "leafen: waiting for another Leafen process to bring this data folder up to date")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func runProductMigrations(pb *pocketbase.PocketBase) (err error) {
	release, err := takeMigrationTurn(pb.DataDir())
	if err != nil {
		return fmt.Errorf("%s: %w", errorCodeSchemaMigrationRecoveryFailed, err)
	}
	defer release()
	sqlTraceMigrating.Store(true)
	defer sqlTraceMigrating.Store(false)
	if err := validateProductMigrationDefinitions(productMigrations); err != nil {
		return fmt.Errorf("%s: %w", errorCodeSchemaMigrationHistoryInvalid, err)
	}

	ledgerExists := pb.HasTable(productMigrationsTable)
	applied, err := readAppliedProductMigrations(pb, ledgerExists)
	if err != nil {
		return fmt.Errorf("%s: %w", errorCodeSchemaMigrationHistoryInvalid, err)
	}
	schemaState, err := initialSchemaState(pb)
	if err != nil {
		return fmt.Errorf("%s: %w", errorCodeSchemaMigrationHistoryInvalid, err)
	}
	// Data from before the baseline, with the history of migrations 1 to
	// 831 one by one or with no history at all, isn't opened
	// (schema_history_refusal.go); the start's earlier refusal stops it
	// before anything is written, and this one stops what that couldn't see.
	if len(applied) == 0 && schemaState != "empty" {
		return fmt.Errorf("%s: this data was %w; %s", errorCodeSchemaMigrationHistoryInvalid, errSchemaHistoryBeforeBaseline, schemaHistoryBringOver)
	}
	if err := validateAppliedProductMigrations(applied, productMigrations); err != nil {
		return fmt.Errorf("%s: %w", errorCodeSchemaMigrationHistoryInvalid, err)
	}
	if len(applied) > 0 && schemaState != "complete" {
		return fmt.Errorf("%s: installed migration history does not match the product schema", errorCodeSchemaMigrationHistoryInvalid)
	}
	migrationsDigest := productMigrationsDigest(productMigrations)
	checked, err := productMigrationsChecked(pb, migrationsDigest)
	if err != nil {
		return fmt.Errorf("%s: %w", errorCodeSchemaMigrationHistoryInvalid, err)
	}
	pending := productMigrations[len(applied):]
	// A start that brings data up to date — a new build's check of the
	// schema, new migrations — says so while it lasts (schema_migration_answer.go).
	if schemaState == "complete" && (!checked || len(pending) > 0) {
		step := updateStepChecking
		if checked {
			step = updateStepKeeping
		}
		dataSchema, toSchema := applied[len(applied)-1].Version, productMigrations[len(productMigrations)-1].version
		defer beginUpdateSteps(dataSchema, toSchema, step, &err)()
		// A serve doesn't try again an update that failed it on this data
		// (schema_migration_failure.go); this deferral runs before the
		// answer's, which says the way back.
		if serveCommandLine {
			holdUpdateFailure(nil)
			if failure, found := earlierUpdateFailure(pb, migrationsDigest, toSchema, dataSchema); found {
				holdUpdateFailure(failure)
				return failure.againError()
			}
			defer func() {
				switch {
				case err == nil:
					forgetUpdateFailure(pb.DataDir())
				case len(pending) > 0:
					holdUpdateFailure(noteUpdateFailure(pb, migrationsDigest, dataSchema, toSchema, err))
				}
			}()
		}
	}
	if !checked {
		// Rows a check didn't expect don't stop the start; a schema that's off does (schema_migration_recheck.go).
		var twin *migrationSchemaTwin
		for index := range applied {
			if err := recheckAppliedMigration(pb, productMigrations[index], &twin); err != nil {
				twin.close()
				return fmt.Errorf("%s: migration %d schema mismatch: %w", errorCodeSchemaMigrationHistoryInvalid, productMigrations[index].version, err)
			}
		}
		twin.close()
	}

	if len(pending) == 0 {
		if !checked {
			noteProductMigrationsChecked(pb, migrationsDigest)
		}
		return checkProductTableExportClasses(pb)
	}

	if schemaState != "empty" {
		setUpdateStep(updateStepKeeping)
		fromVersion := applied[len(applied)-1].Version
		if err := ensureMigrationRecovery(pb, fromVersion, productMigrations[len(productMigrations)-1].version); err != nil {
			return fmt.Errorf("%s: %w", errorCodeSchemaMigrationRecoveryFailed, err)
		}
	}

	setUpdateStep(updateStepMigrating)
	err = pb.RunInTransaction(func(txApp core.App) error {
		// A migration is Leafen's own upkeep, no one's act, which no webhook
		// or app hears of, as Leafen's other upkeep (contentGraphMutationHooks.LeafenOwn).
		keepTransactionFromApps(txApp)
		// Each table a migration makes needs its export class (schema_migration_export_classes.go).
		return applyProductMigrations(txApp, pending, schemaState == "empty")
	})
	if err != nil {
		return fmt.Errorf("%s: %w", errorCodeSchemaMigrationFailed, err)
	}
	// The migrations may have written collections the app's cache doesn't
	// hold yet, as the baseline does.
	if err := pb.ReloadCachedCollections(); err != nil {
		return err
	}
	// Every earlier check has to hold on the schema the new migrations
	// leave, as all of them run again once a later build adds one: a check a
	// new migration breaks stops this start, in the build that broke it, and
	// not the next build's (2026-11-11: 746 gave groups a column 125 counted
	// exactly, and installs past 746 couldn't start the build that added 747).
	setUpdateStep(updateStepChecking)
	var twin *migrationSchemaTwin
	for _, migration := range productMigrations {
		if err := recheckAppliedMigration(pb, migration, &twin); err != nil {
			twin.close()
			return fmt.Errorf("%s: migration %d schema mismatch: %w", errorCodeSchemaMigrationHistoryInvalid, migration.version, err)
		}
	}
	twin.close()
	noteProductMigrationsChecked(pb, migrationsDigest)
	return checkProductTableExportClasses(pb)
}

// finishProductMigration checks a migration's result and records it.
func finishProductMigration(app core.App, migration productMigration) error {
	if err := migration.validate(app); err != nil {
		return fmt.Errorf("migration %d %s validation: %w", migration.version, migration.name, err)
	}
	_, err := app.DB().Insert(productMigrationsTable, dbx.Params{
		"version":    migration.version,
		"name":       migration.name,
		"checksum":   migration.checksum,
		"applied_at": time.Now().UTC().Format(time.RFC3339Nano),
	}).Execute()
	return err
}

// noteProductMigrationsChecked records a passed check; a start that can't
// record it, as on read-only storage, checks everything again next time.
func noteProductMigrationsChecked(app core.App, migrations string) {
	if err := recordProductMigrationsChecked(app, migrations); err != nil {
		app.Logger().Warn("schema migration checks not recorded", "error", err)
	}
}

// productMigrationRecorded reports whether a migration is in the ledger; an
// earlier migration's check of storage a later one retired stands down once
// the later one is recorded.
func productMigrationRecorded(app core.App, version int, name string) (bool, error) {
	if !app.HasTable(productMigrationsTable) {
		return false, nil
	}
	var count int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_schema_migrations}}
		WHERE [[version]] = {:version} AND [[name]] = {:name}
	`).Bind(dbx.Params{"version": version, "name": name}).Row(&count); err != nil {
		return false, err
	}
	return count == 1, nil
}

// productMigrationAppliedAt is when a migration ran on this data: its own
// row's time, or the schema baseline's for one the baseline holds — data
// that began with the baseline holds nothing made before any of those. ok
// is false while it hasn't run.
func productMigrationAppliedAt(app core.App, version int) (at time.Time, ok bool, err error) {
	version = max(version, schemaBaselineVersion)
	var appliedAt string
	err = app.DB().NewQuery(`SELECT [[applied_at]] FROM {{leafen_schema_migrations}} WHERE [[version]] = {:version}`).
		Bind(dbx.Params{"version": version}).Row(&appliedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	at, err = time.Parse(time.RFC3339Nano, appliedAt)
	return at, err == nil, err
}

func validateProductMigrationDefinitions(migrations []productMigration) error {
	if len(migrations) == 0 || migrations[0].version != schemaBaselineVersion || migrations[0].checksum != schemaBaselineChecksum {
		return errors.New("the migration list doesn't start with the schema baseline")
	}
	names := map[string]struct{}{}
	for index, migration := range migrations {
		if migration.version != schemaBaselineVersion+index || migration.name == "" || !sha256Pattern.MatchString(migration.checksum) ||
			migration.impact.responsibilityCodes == "" || migration.impact.policyConsumers == "" ||
			(migration.up == nil && migration.content == nil) || migration.validate == nil {
			return fmt.Errorf("invalid migration definition at index %d", index)
		}
		if !strings.HasPrefix(migration.name, "native_") {
			return fmt.Errorf("migration %d name must use native_ prefix", migration.version)
		}
		if _, exists := names[migration.name]; exists {
			return fmt.Errorf("duplicate migration name %s", migration.name)
		}
		names[migration.name] = struct{}{}
	}
	return nil
}

func readAppliedProductMigrations(app core.App, exists bool) ([]appliedProductMigration, error) {
	if !exists {
		return []appliedProductMigration{}, nil
	}
	rows := []appliedProductMigration{}
	err := app.DB().NewQuery(`
		SELECT [[version]], [[name]], [[checksum]]
		FROM {{leafen_schema_migrations}}
		ORDER BY [[version]] ASC
	`).All(&rows)
	return rows, err
}

func validateAppliedProductMigrations(applied []appliedProductMigration, migrations []productMigration) error {
	if schemaHistoryBeforeBaseline(applied) {
		return fmt.Errorf("this data was %w; %s", errSchemaHistoryBeforeBaseline, schemaHistoryBringOver)
	}
	if len(applied) > len(migrations) {
		return fmt.Errorf("this data was %w; run that newer version again", errSchemaHistoryNewer)
	}
	for index, row := range applied {
		expected := migrations[index]
		if row.Version != expected.version || row.Checksum != expected.checksum || row.Name != expected.name {
			return fmt.Errorf("migration history diverges at version %d", expected.version)
		}
	}
	return nil
}

func initialSchemaState(app core.App) (string, error) {
	params := dbx.Params{}
	placeholders := make([]string, 0, len(initialNativeSchemaTables))
	for index, table := range initialNativeSchemaTables {
		key := fmt.Sprintf("table%d", index)
		params[key] = table
		placeholders = append(placeholders, "{:"+key+"}")
	}
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*)
		FROM [[sqlite_master]]
		WHERE [[type]] = 'table' AND [[name]] IN (` + joinSQL(placeholders) + `)
	`).Bind(params).Row(&count)
	if err != nil {
		return "", err
	}
	if count == 0 {
		return "empty", nil
	}
	if count == len(initialNativeSchemaTables) {
		return "complete", nil
	}
	return "partial", nil
}

func ensureProductMigrationsTable(app core.App) error {
	_, err := app.DB().NewQuery(`
		CREATE TABLE IF NOT EXISTS {{leafen_schema_migrations}} (
			[[version]] INTEGER PRIMARY KEY NOT NULL,
			[[name]] TEXT UNIQUE NOT NULL,
			[[checksum]] TEXT NOT NULL,
			[[applied_at]] TEXT NOT NULL
		)
	`).Execute()
	return err
}

func joinSQL(parts []string) string {
	return strings.Join(parts, ",")
}
