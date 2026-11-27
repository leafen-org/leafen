package app

import (
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// schemaIndexReadPaths says which reads the schema's indexes serve: an index
// serves a read by the columns it leads with, if it isn't partial and those
// columns compare as written (BINARY), as an equality on the column does.
// Both deletions hold to it: a workspace's for the tables it reads by
// workspace (validateWorkspaceDeletionAccessPaths), an account's for the
// tables its cleanup reads by person (validateAccountDeletionLaneIndexes);
// and so does SQLite's look-up of a foreign key's rows when the row it
// points at goes or is saved (validateForeignKeyChildIndexes).
type schemaIndexReadPaths struct {
	// led holds "table:a" and "table:a,b" … for each run of leading columns.
	led map[string]bool
	// ledInAnyOrder holds the same runs with their columns sorted: a read by
	// all of them, whichever order the index has them in.
	ledInAnyOrder map[string]bool
	// alone holds "table:a" for an index on that one column.
	alone map[string]bool
}

// readSchemaIndexReadPaths reads the indexes of the named tables, or of
// every table when none is named.
func readSchemaIndexReadPaths(app core.App, tables []string) (schemaIndexReadPaths, error) {
	indexes := []struct {
		Table   string `db:"table_name"`
		Columns string `db:"key_columns"`
	}{}
	only := ""
	if len(tables) > 0 {
		only = ` AND [[schema]].[[name]] IN ('` + strings.Join(tables, "', '") + `')`
	}
	if err := app.DB().NewQuery(`
		SELECT [[schema]].[[name]] AS [[table_name]], COALESCE((
			SELECT group_concat(COALESCE([[part]].[[name]], '') || ':' || [[part]].[[coll]], ',') FROM (
				SELECT [[name]], [[coll]] FROM pragma_index_xinfo([[list]].[[name]])
				WHERE [[key]] = 1 ORDER BY [[seqno]]
			) [[part]]
		), '') AS [[key_columns]]
		FROM [[sqlite_master]] [[schema]]
		JOIN pragma_index_list([[schema]].[[name]]) [[list]]
		WHERE [[schema]].[[type]] = 'table' AND [[list]].[[partial]] = 0` + only + `
	`).All(&indexes); err != nil {
		return schemaIndexReadPaths{}, err
	}
	paths := schemaIndexReadPaths{led: map[string]bool{}, ledInAnyOrder: map[string]bool{}, alone: map[string]bool{}}
	for _, index := range indexes {
		columns := strings.Split(index.Columns, ",")
		lead, run := "", []string{}
		for position, column := range columns {
			// An expression keeps no column name.
			name, written := strings.CutSuffix(column, ":BINARY")
			if !written || name == "" {
				break
			}
			if position > 0 {
				lead += ","
			}
			lead += name
			run = append(run, name)
			paths.led[index.Table+":"+lead] = true
			paths.ledInAnyOrder[index.Table+":"+strings.Join(slices.Sorted(slices.Values(run)), ",")] = true
			if len(columns) == 1 {
				paths.alone[index.Table+":"+name] = true
			}
		}
	}
	return paths, nil
}

// ledBy reports whether an index of the table leads with these columns.
func (paths schemaIndexReadPaths) ledBy(table string, columns ...string) bool {
	return paths.led[table+":"+strings.Join(columns, ",")]
}

// ledByAll reports whether an index of the table leads with these columns,
// in any order, and no others before them.
func (paths schemaIndexReadPaths) ledByAll(table string, columns ...string) bool {
	return paths.ledInAnyOrder[table+":"+strings.Join(slices.Sorted(slices.Values(columns)), ",")]
}

// aloneOn reports whether an index of the table is on this column alone.
func (paths schemaIndexReadPaths) aloneOn(table, column string) bool {
	return paths.alone[table+":"+column]
}
