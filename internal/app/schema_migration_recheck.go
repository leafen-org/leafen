package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A start whose migrations or schema changed checks every applied migration
// again. Many checks read rows too — that no private teamspace lets outsiders
// in, say — and a row a restored backup, an older version or a past defect
// left behind would then stop the server for good, with nothing a person
// could do about it. So a re-check that fails is tried once more over a
// schema-only twin of the database: its tables, indexes, triggers, views and
// collection records, without the rows — but for a table that holds one row
// by its own definition, a setting its migration put there. Failing there too, the schema itself
// is off and the start stops as before; passing there, only the data is
// unexpected, and the start goes on with that written to the log. A
// migration being applied now still checks its rows: its own up is what
// makes them right.

// migrationSchemaTwin is the schema-only copy of a database, made on the
// first re-check that fails.
type migrationSchemaTwin struct {
	app *core.BaseApp
	dir string
}

// migrationsTwinCopiedRows are the tables whose rows are schema too: the
// collections PocketBase reads its own schema from, and the migration
// ledgers.
var migrationsTwinCopiedRows = []string{"_collections", "_migrations", productMigrationsTable}

// migrationTwinSingleRowKey finds a table that holds one row by its own
// definition — its key checked against a constant, as the server's settings
// and the authority revisions are. That row is schema too: the migration
// put it there and the checks expect it.
var migrationTwinSingleRowKey = regexp.MustCompile("`(\\w+)`\\s+(?:TEXT|INTEGER)\\s+(?:NOT NULL\\s+)?PRIMARY KEY(?:\\s+NOT NULL)?\\s+CHECK\\s*\\(\\s*`(\\w+)`\\s*=\\s*(?:'[^']*'|\\d+)\\s*\\)")

func migrationTwinSingleRowTable(sql string) bool {
	for _, match := range migrationTwinSingleRowKey.FindAllStringSubmatch(sql, -1) {
		if match[1] == match[2] {
			return true
		}
	}
	return false
}

func newMigrationSchemaTwin(source core.App) (*migrationSchemaTwin, error) {
	// Under the data folder, as the settings rows it copies are the data's;
	// one a stopped start left behind goes first.
	parent := filepath.Join(source.DataDir(), "tmp")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	if stale, err := filepath.Glob(filepath.Join(parent, "schema-twin-*")); err == nil {
		for _, path := range stale {
			_ = os.RemoveAll(path)
		}
	}
	dir, err := os.MkdirTemp(parent, "schema-twin-")
	if err != nil {
		return nil, err
	}
	twin := &migrationSchemaTwin{app: core.NewBaseApp(core.BaseAppConfig{DataDir: dir}), dir: dir}
	if err := twin.app.Bootstrap(); err != nil {
		twin.close()
		return nil, err
	}
	if err := twin.copySchema(source); err != nil {
		twin.close()
		return nil, err
	}
	return twin, nil
}

func (twin *migrationSchemaTwin) copySchema(source core.App) error {
	objects := []struct {
		Type string `db:"type"`
		Name string `db:"name"`
		SQL  string `db:"sql"`
	}{}
	// In the order they were made, so a virtual table comes before its own tables.
	if err := source.DB().NewQuery(`
		SELECT [[type]], [[name]], COALESCE([[sql]], '') AS [[sql]] FROM [[sqlite_master]]
		WHERE [[name]] NOT LIKE 'sqlite_%' ORDER BY [[rowid]]
	`).All(&objects); err != nil {
		return err
	}
	tables := append([]string{}, migrationsTwinCopiedRows...)
	for _, object := range objects {
		if object.Type == "table" && migrationTwinSingleRowTable(object.SQL) {
			tables = append(tables, object.Name)
		}
	}
	rows := map[string][]dbx.NullStringMap{}
	for _, table := range tables {
		if !source.HasTable(table) {
			continue
		}
		copied := []dbx.NullStringMap{}
		if err := source.DB().NewQuery("SELECT * FROM {{" + table + "}}").All(&copied); err != nil {
			return err
		}
		rows[table] = copied
	}
	err := twin.app.RunInTransaction(func(tx core.App) error {
		// The twin's own schema goes, but for its settings.
		own := []string{}
		if err := tx.DB().NewQuery(`
			SELECT [[name]] FROM [[sqlite_master]]
			WHERE [[type]] IN ('table', 'view') AND [[name]] NOT LIKE 'sqlite_%' AND [[name]] != '_params' ORDER BY [[rowid]] DESC
		`).Column(&own); err != nil {
			return err
		}
		for _, name := range own {
			var kind string
			if err := tx.DB().NewQuery(`SELECT [[type]] FROM [[sqlite_master]] WHERE [[name]] = {:name}`).
				Bind(dbx.Params{"name": name}).Row(&kind); err != nil {
				continue // a virtual table's own table, gone with it
			}
			if _, err := tx.DB().NewQuery(fmt.Sprintf("DROP %s IF EXISTS [[%s]]", map[bool]string{true: "VIEW", false: "TABLE"}[kind == "view"], name)).Execute(); err != nil {
				return err
			}
		}
		for _, object := range objects {
			if object.SQL == "" || object.Name == "_params" {
				continue
			}
			var count int
			if err := tx.DB().NewQuery(`SELECT COUNT(*) FROM [[sqlite_master]] WHERE [[name]] = {:name}`).
				Bind(dbx.Params{"name": object.Name}).Row(&count); err != nil {
				return err
			}
			if count > 0 {
				continue
			}
			if _, err := tx.DB().NewQuery(object.SQL).Execute(); err != nil {
				return fmt.Errorf("%s %s: %w", object.Type, object.Name, err)
			}
		}
		for table, copied := range rows {
			for _, row := range copied {
				params := dbx.Params{}
				for column, value := range row {
					if value.Valid {
						params[column] = value.String
					} else {
						params[column] = nil
					}
				}
				if _, err := tx.DB().Insert(table, params).Execute(); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return twin.app.ReloadCachedCollections()
}

func (twin *migrationSchemaTwin) close() {
	if twin == nil {
		return
	}
	_ = twin.app.ClearBootstrap()
	_ = os.RemoveAll(twin.dir)
}

// recheckAppliedMigration checks an applied migration again, telling a
// schema that's off (an error) from rows the check didn't expect (logged).
func recheckAppliedMigration(app core.App, migration productMigration, twin **migrationSchemaTwin) error {
	failure := migration.validate(app)
	if failure == nil {
		return nil
	}
	if *twin == nil {
		made, err := newMigrationSchemaTwin(app)
		if err != nil {
			app.Logger().Warn("leafen schema migration twin failed", "error", err)
			return failure
		}
		*twin = made
	}
	if err := migration.validate((*twin).app); err != nil {
		return failure
	}
	app.Logger().Warn("leafen schema migration check found rows it didn't expect; the schema is as it should be",
		"migration", migration.version, "name", migration.name, "check", failure.Error())
	return nil
}
