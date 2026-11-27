package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// productMigrationChecksTable holds the migrations and the schema the last
// check of every applied migration passed over. A start with the same
// migrations over the same schema leaves the checks out: many of them read
// whole tables, so a start took longer as data grew. A changed schema, such
// as a dropped index, or a changed set of migrations checks everything again
// — and there rows a check didn't expect are logged rather than stopping the
// start (schema_migration_recheck.go).
const productMigrationChecksTable = "leafen_schema_migration_checks"

func ensureProductMigrationChecksTable(app core.App) error {
	_, err := app.DB().NewQuery(`
		CREATE TABLE IF NOT EXISTS {{leafen_schema_migration_checks}} (
			[[id]] TEXT PRIMARY KEY NOT NULL,
			[[migrations]] TEXT NOT NULL,
			[[schema]] TEXT NOT NULL,
			[[checked_at]] TEXT NOT NULL
		)
	`).Execute()
	return err
}

// productSchemaDigest names the database's schema objects, apart from the
// record of the checks itself and what another tool keeps in the database
// (dataDatabaseToolTables): turning continuous replication on checked every
// migration again at the next start, as if the schema had changed.
func productSchemaDigest(app core.App) (string, error) {
	objects := []struct {
		Type  string `db:"type"`
		Name  string `db:"name"`
		Table string `db:"tbl_name"`
		SQL   string `db:"sql"`
	}{}
	tools := make([]string, 0, len(dataDatabaseToolTables))
	for _, table := range dataDatabaseToolTables {
		tools = append(tools, sqlStringLiteral(table))
	}
	if err := app.DB().NewQuery(`
		SELECT [[type]], [[name]], [[tbl_name]], COALESCE([[sql]], '') AS [[sql]] FROM [[sqlite_master]]
		WHERE [[name]] NOT LIKE 'sqlite_%' AND [[name]] != {:checks}
			AND [[tbl_name]] NOT IN (` + strings.Join(tools, ", ") + `)
		ORDER BY [[type]], [[name]]
	`).Bind(dbx.Params{"checks": productMigrationChecksTable}).All(&objects); err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, object := range objects {
		fmt.Fprintf(hash, "%s\t%s\t%s\t%s\n", object.Type, object.Name, object.Table, object.SQL)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// productMigrationsDigest names a list of migrations by their versions,
// names and checksums.
func productMigrationsDigest(migrations []productMigration) string {
	hash := sha256.New()
	for _, migration := range migrations {
		fmt.Fprintf(hash, "%d\t%s\t%s\n", migration.version, migration.name, migration.checksum)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// productMigrationsChecked reports whether the applied migrations were all
// checked before over this same schema by these same migrations.
func productMigrationsChecked(app core.App, migrations string) (bool, error) {
	if !app.HasTable(productMigrationChecksTable) {
		return false, nil
	}
	var storedMigrations, storedSchema string
	err := app.DB().NewQuery(`
		SELECT [[migrations]], [[schema]] FROM {{leafen_schema_migration_checks}} WHERE [[id]] = 'default'
	`).Row(&storedMigrations, &storedSchema)
	if errors.Is(err, sql.ErrNoRows) || err == nil && storedMigrations != migrations {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	schema, err := productSchemaDigest(app)
	return err == nil && schema == storedSchema, err
}

// recordProductMigrationsChecked notes that every applied migration passed
// its check over the schema as it is now.
func recordProductMigrationsChecked(app core.App, migrations string) error {
	if err := ensureProductMigrationChecksTable(app); err != nil {
		return err
	}
	schema, err := productSchemaDigest(app)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_schema_migration_checks}} ([[id]], [[migrations]], [[schema]], [[checked_at]])
		VALUES ('default', {:migrations}, {:schema}, {:now})
		ON CONFLICT ([[id]]) DO UPDATE SET [[migrations]] = excluded.[[migrations]],
			[[schema]] = excluded.[[schema]], [[checked_at]] = excluded.[[checked_at]]
	`).Bind(dbx.Params{
		"migrations": migrations, "schema": schema, "now": time.Now().UTC().Format(time.RFC3339Nano),
	}).Execute()
	return err
}
