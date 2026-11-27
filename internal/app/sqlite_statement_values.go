package app

import (
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
)

// Each statement of a query binds its own values (2026-11-14). dbx writes a
// query's named values as SQLite's anonymous "?", and SQLite numbers those
// within each statement: a query of several statements bound each later
// statement's values from the start of the query's list (the second of
// "INSERT … ({:x}); INSERT … ({:y}, {:z})" took X and Y), so a new page
// verification cancelled none of the last one's email deliveries, and a
// schema step restarted the page file reference pass with the row page icon
// pass's name. A query that goes on after a semicolon and binds a value
// after it writes its values numbered instead ("?3": the value's place in the
// whole query, in every statement). The rest keep the anonymous "?": SQLite's
// driver finds a numbered value by writing out each value's place, which
// costs a long list of values more. A semicolon in a quote, a comment or a
// trigger's body only numbers a query that needn't be; a numbered query binds
// the same.
//
// Every SQLite database the server opens is built by dbx under this name —
// the data database's read pool, its write connection and read snapshots
// (sqlite_statement_cache.go), and PocketBase's others — and so are their
// transactions. scripts/check-statement-values.mjs keeps the product's
// queries binding right without the numbering as well.
func init() {
	for _, driver := range []string{"sqlite", "sqlite3"} {
		base := dbx.BuilderFuncMap[driver]
		dbx.BuilderFuncMap[driver] = func(db *dbx.DB, executor dbx.Executor) dbx.Builder {
			return &statementValuesBuilder{Builder: base(db, executor), db: db, executor: executor}
		}
	}
}

// statementValuesBuilder is dbx's SQLite builder for a database or a
// transaction (executor) of it.
type statementValuesBuilder struct {
	dbx.Builder
	db       *dbx.DB
	executor dbx.Executor
}

func (builder *statementValuesBuilder) NewQuery(sql string) *dbx.Query {
	end := strings.IndexByte(sql, ';')
	if end < 0 || !strings.Contains(sql[end:], "{:") {
		return builder.Builder.NewQuery(sql)
	}
	// The database as it stands, writing this query's values numbered.
	numbered := *builder.db
	numbered.Builder = numberedValues{builder.db.Builder}
	return dbx.NewQuery(&numbered, builder.executor, sql)
}

// numberedValues writes the n-th value of a query as "?n".
type numberedValues struct{ dbx.Builder }

func (numberedValues) GeneratePlaceholder(position int) string {
	return "?" + strconv.Itoa(position)
}
