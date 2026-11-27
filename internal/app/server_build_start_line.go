package app

import (
	"fmt"
	"path/filepath"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// As it starts, the server says in its output — a container's log among
// them — which build it runs on which schema of its data, and when this
// start brought the data up from an earlier schema, from which one and where
// the data from before is kept (2026-11-17: after a Docker upgrade nothing
// in the log told an operator whether the new image ran and the data moved
// on; only an agent's leafen_server_version did). The line starts with
// "leafen: build".

// sayServerBuildStart prints the line; previous is the build this data ran
// before, "" for none.
func sayServerBuildStart(app core.App, build serverBuild, previous string) {
	line := fmt.Sprintf("leafen: build %s, data schema %d", build.Build, build.SchemaVersion)
	if from, moved := serverSchemaMovedAtStart(app); moved && from == 0 {
		line += ", a new data folder"
	} else if moved {
		line += fmt.Sprintf(", brought up from schema %d as it started", from)
		// The copy ensureMigrationRecovery keeps before migrating, the
		// pair's latest: a data folder, or a zip an earlier build made.
		root := filepath.Join(app.DataDir(), core.LocalBackupsDirName, "migrations")
		if kept, err := newestMigrationRecoveryPoint(root, migrationRecoveryName(from, build.SchemaVersion)); err == nil && kept.name != "" {
			path := filepath.Join(root, kept.name)
			if kept.zip {
				path += ".zip"
			}
			line += " (the data from before is kept in " + path + ")"
		}
	}
	if previous != "" {
		line += "; it ran " + previous + " before"
	}
	app.Logger().Info(line)
	fmt.Println(line)
}

// serverSchemaMovedAtStart is the schema the data had before this start
// applied migrations to it, if it did: the migrations recorded since the
// process began (a new data folder takes them all, from 0, the schema
// baseline first).
func serverSchemaMovedAtStart(app core.App) (from int, moved bool) {
	// applied_at is RFC 3339 in UTC, and the start's second without its zone
	// sorts before every time within that second.
	since := serverBuildState.startedAt.UTC().Format("2006-01-02T15:04:05")
	var lowest, count int
	if err := app.DB().NewQuery(`SELECT COALESCE(MIN([[version]]), 0), COUNT(*) FROM {{leafen_schema_migrations}}
		WHERE [[applied_at]] >= {:since}`).Bind(dbx.Params{"since": since}).Row(&lowest, &count); err != nil || count == 0 {
		return 0, false
	}
	if lowest <= schemaBaselineVersion {
		return 0, true
	}
	return lowest - 1, true
}
