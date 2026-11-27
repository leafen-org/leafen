package app

import (
	"sync"

	"github.com/pocketbase/pocketbase/core"
)

// tableKnown says whether a table is in the database, asking SQLite only
// until it is: a table these paths name, once made, stays, while PocketBase's
// HasTable runs a schema query on every call — on an access check that ran
// tens of times a request, most of the server's time under load (2026-10-21).
// Migrations, and tables a later migration retires, keep asking HasTable.
func tableKnown(app core.App, table string) bool {
	known := app.Store().GetOrSet(tableKnownStoreKey, func() any { return &sync.Map{} }).(*sync.Map)
	if _, found := known.Load(table); found {
		return true
	}
	if !app.HasTable(table) {
		return false
	}
	known.Store(table, true)
	return true
}

const tableKnownStoreKey = "leafenTablesKnown"
