package app

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
	sqlite3 "modernc.org/sqlite/lib"
)

// runNotionReadSnapshot reads a public API answer in a snapshot of its own
// (read_snapshot.go), not on the one write connection, so a read waits for
// no write (2026-11-14: page, block, database and data source reads each
// took the write connection, and with twenty readers at once one block list
// waited out a long write and answered 500 after ten seconds). The public
// identities it names that weren't stored yet are stored after it
// (runReadSnapshot); a read that writes something else — a server's first
// file secret, say — reads again in a transaction, as before, so fn sets
// what it answers afresh each time it runs.
func runNotionReadSnapshot(ctx context.Context, app core.App, fn func(core.App) error) error {
	err := runReadSnapshot(ctx, app, fn)
	if isSQLiteBaseCode(err, sqlite3.SQLITE_READONLY) {
		return app.RunInTransaction(fn)
	}
	return err
}
