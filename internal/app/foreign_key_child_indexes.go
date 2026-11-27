package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Deleting a row other tables' foreign keys point at, or PocketBase saving
// it (a save writes the primary key back), makes SQLite look up the row's
// children by the key, inside the server's one write. So each foreign key's
// table has an index serving that look-up: one led by the whole key, its
// columns in any order (schemaIndexReadPaths: full, compared as written),
// which SQLite searches by rather than reading the whole child table each
// time — the query-plan audit's rule of 2026-10-04, which only a gate test
// held to. Migration 752 added a key without one, and page deletes and saves
// read the whole of leafen_database_notification_preferences until migration
// 818 indexed it (2026-11-17). An index led by one column of a key of two
// is not enough: SQLite may search by it and read every row sharing that
// column, as a legal hold's scopes were read by their organization until
// migration 820 (2026-11-18). Each start checks it now, from the schema
// alone: a development build stops at the start over a schema that breaks
// it, so the change that adds such a key indexes it before it ships; a
// release build writes it to its log and starts, as the start's other schema
// rules do (deletion_inventories_start.go).

var errForeignKeyChildUnindexed = errors.New("a foreign key's table has no index to find its rows by")

type schemaForeignKeyColumn struct {
	Table  string `db:"table_name"`
	Key    int    `db:"key_id"`
	Column string `db:"from_column"`
	Parent string `db:"parent_name"`
}

// validateForeignKeyChildIndexes names each foreign key no index of its
// table serves.
func validateForeignKeyChildIndexes(app core.App) error {
	columns := []schemaForeignKeyColumn{}
	if err := app.DB().NewQuery(`
		SELECT [[schema]].[[name]] AS [[table_name]], [[reference]].[[id]] AS [[key_id]],
			[[reference]].[[from]] AS [[from_column]], [[reference]].[[table]] AS [[parent_name]]
		FROM [[sqlite_master]] [[schema]]
		JOIN pragma_foreign_key_list([[schema]].[[name]]) [[reference]]
		WHERE [[schema]].[[type]] = 'table'
		ORDER BY [[schema]].[[name]], [[reference]].[[id]], [[reference]].[[seq]]
	`).All(&columns); err != nil {
		return err
	}
	paths, err := readSchemaIndexReadPaths(app, nil)
	if err != nil {
		return err
	}
	unserved := []string{}
	for start := 0; start < len(columns); {
		table, key := columns[start].Table, []string{}
		end := start
		for ; end < len(columns) && columns[end].Table == table && columns[end].Key == columns[start].Key; end++ {
			key = append(key, columns[end].Column)
		}
		if !paths.ledByAll(table, key...) {
			unserved = append(unserved, fmt.Sprintf("%s (%s) → %s", table, strings.Join(key, ", "), columns[start].Parent))
		}
		start = end
	}
	if len(unserved) > 0 {
		return fmt.Errorf("%w: %s", errForeignKeyChildUnindexed, strings.Join(unserved, "; "))
	}
	return nil
}

// checkForeignKeyChildIndexesAtStart holds the start to the rule: a
// development build stops, a release build logs and starts.
func checkForeignKeyChildIndexesAtStart(app core.App) error {
	err := validateForeignKeyChildIndexes(app)
	if err == nil {
		return nil
	}
	if unclassifiedTablesStopStart {
		return err
	}
	const message = "leafen schema has a foreign key no index serves; deleting or saving a row it points at reads the whole of its table until an update indexes it"
	app.Logger().Error(message, "error", err.Error())
	if !app.IsDev() {
		productionDiagnosticLogger.Error(message, "error", err.Error())
	}
	return nil
}
