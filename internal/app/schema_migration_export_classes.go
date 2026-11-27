package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Every table on a server has its class in the instance product export
// policy (instance_product_export_policy.go): carried along with the
// server's product data, or left out and why. The export refuses a server
// holding a table the policy doesn't name, and a migration that made a
// table without adding it there showed only when someone exported a whole
// server, which then failed on every server (2026-11-09: three tables;
// 2026-11-17: the two agent sign-in tables).
//
// So the migrations being applied note the tables they make, and a table
// left without a class is named, with the migration that made it, as soon
// as that migration runs. A development build (made with the tag
// leafen_devweb, as every check of a change on screen is) stops there: the
// migrations don't count as applied and the start stops, the first time
// the build that added the table starts. It stops as well at any start
// over a database holding a table without a class, however it got there.
// A release build writes the same line to its log and starts as usual: a
// server that won't start after an update costs its people far more than
// an export that won't run, and the export names every such table itself.
// The workspace and account deletion inventories, the other lists a new
// table needs a place in, keep the same rule (deletion_inventories_start.go).

// applyProductMigrations applies the pending migrations in the transaction
// it's given: on a new database the schema baseline first, which makes
// nearly every table at once.
func applyProductMigrations(txApp core.App, pending []productMigration, fresh bool) error {
	made, err := newMigrationMadeTables(txApp, fresh)
	if err != nil {
		return err
	}
	if err := ensureProductMigrationsTable(txApp); err != nil {
		return err
	}
	if err := made.note(txApp, "the migration ledger"); err != nil {
		return err
	}
	for _, migration := range pending {
		if migration.up != nil {
			if err := migration.up(txApp); err != nil {
				return fmt.Errorf("migration %d %s: %w", migration.version, migration.name, err)
			}
			if err := made.noteMigration(txApp, migration); err != nil {
				return err
			}
		}
		if migration.content == nil {
			if err := finishProductMigration(txApp, migration); err != nil {
				return err
			}
		}
	}
	// Content goes through today's code, so it runs on the schema the
	// last up leaves (productMigration).
	for _, migration := range pending {
		if migration.content == nil {
			continue
		}
		if err := migration.content(txApp); err != nil {
			return fmt.Errorf("migration %d %s: %w", migration.version, migration.name, err)
		}
		if err := made.noteMigration(txApp, migration); err != nil {
			return err
		}
		if err := finishProductMigration(txApp, migration); err != nil {
			return err
		}
	}
	return made.classified(txApp)
}

// migrationMadeTables are the tables the migrations being applied made,
// each with what made it. A table one migration makes and a later one
// drops needs no class.
type migrationMadeTables struct {
	before map[string]bool
	madeBy map[string]string
}

// newMigrationMadeTables starts from the tables a database has. On a fresh
// one every table counts, PocketBase's own among them.
func newMigrationMadeTables(app core.App, fresh bool) (*migrationMadeTables, error) {
	made := &migrationMadeTables{before: map[string]bool{}, madeBy: map[string]string{}}
	if fresh {
		return made, made.note(app, "PocketBase")
	}
	tables, err := schemaTableNames(app)
	if err != nil {
		return nil, err
	}
	for _, table := range tables {
		made.before[table] = true
	}
	return made, nil
}

func (made *migrationMadeTables) noteMigration(app core.App, migration productMigration) error {
	return made.note(app, fmt.Sprintf("migration %d %s", migration.version, migration.name))
}

func (made *migrationMadeTables) note(app core.App, by string) error {
	tables, err := schemaTableNames(app)
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(tables))
	for _, table := range tables {
		present[table] = true
		if _, known := made.madeBy[table]; !known && !made.before[table] {
			made.madeBy[table] = by
		}
	}
	for table := range made.madeBy {
		if !present[table] {
			delete(made.madeBy, table)
		}
	}
	return nil
}

// classified names every table the migrations made that the export policy
// has no class for: a development build stops on it, a release build logs it.
func (made *migrationMadeTables) classified(app core.App) error {
	tables, err := schemaTableNames(app)
	if err != nil {
		return err
	}
	missing := []string{}
	for _, table := range instanceProductExportUnclassified(instanceProductExportPolicyDefinitions(), tables) {
		if !made.before[table] {
			missing = append(missing, fmt.Sprintf("%s (%s)", table, made.madeBy[table]))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if unclassifiedTablesStopStart {
		return fmt.Errorf("%w: give each table a class in instance_product_export_policy.go: %s",
			errInstanceProductExportPolicyIncomplete, strings.Join(missing, ", "))
	}
	logUnclassifiedExportTables(app, "leafen schema migration made tables the instance product export policy has no class for; a whole-server product export fails until each has one", missing)
	return nil
}

// logUnclassifiedExportTables writes one line naming the tables to the
// server's log, and to its error output as other failures go there.
func logUnclassifiedExportTables(app core.App, message string, tables []string) {
	app.Logger().Error(message, "tables", strings.Join(tables, ", "))
	if !app.IsDev() {
		productionDiagnosticLogger.Error(message, "tables", strings.Join(tables, ", "))
	}
}

// checkProductTableExportClasses stops a development build's start over a
// database holding a table the export policy has no class for, however it
// got there; a release build leaves that to the export.
func checkProductTableExportClasses(app core.App) error {
	if !unclassifiedTablesStopStart {
		return nil
	}
	tables, err := schemaTableNames(app)
	if err != nil {
		return err
	}
	if missing := instanceProductExportUnclassified(instanceProductExportPolicyDefinitions(), tables); len(missing) > 0 {
		return fmt.Errorf("%w: give each table a class in instance_product_export_policy.go: %s, already in this database",
			errInstanceProductExportPolicyIncomplete, strings.Join(missing, ", "))
	}
	return nil
}

// schemaTableNames are a database's tables in name order, without SQLite's own.
func schemaTableNames(app core.App) ([]string, error) {
	tables := []string{}
	err := app.DB().NewQuery(`
		SELECT [[name]] FROM [[sqlite_master]]
		WHERE [[type]] = 'table' AND [[name]] NOT LIKE 'sqlite_%'
		ORDER BY [[name]]
	`).Column(&tables)
	return tables, err
}
