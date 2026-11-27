package app

import (
	"context"
	"database/sql"
	"path/filepath"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A read that must see one moment of the database — a page's content graph,
// a comment list, a history version, a settings view — reads in a snapshot
// of its own on a read connection: SQLite's WAL gives a reader the database
// as it was when it began, without the one write connection every change
// waits for (2026-10-14 load measurement: graph reads took the write
// connection, and writes waited behind them). app.RunInTransaction is for
// what writes: it opens on that write connection, so a read in it waits out
// any write in progress (2026-11-11: a comment list waited 10 s behind one
// and failed busy). Nothing
// in it writes: its connections refuse writes (query_only), so one that
// slips in fails at once rather than be rolled back with the snapshot. They
// refuse them from when they open: setting query_only expires every
// statement the connection keeps compiled, and setting it for each snapshot
// compiled every statement a snapshot ran again (2026-10-14 load
// measurement: a fifth of the server's time under 200 database queries).
//
// Snapshots take their connections from a pool of their own: what runs
// inside one still reads through PocketBase's own pool (its record lookups
// don't go through the snapshot), and a snapshot holding a connection of
// that pool while waiting for another would, at enough of them at once,
// leave every one waiting (2026-10-14: 300 users, every read timed out).

const readSnapshotPoolSize = 16

const readSnapshotPoolStoreKey = "leafen.readSnapshotPool"

type readSnapshotApp struct {
	core.App
	tx *dbx.Tx
	// Its permission decisions (access_memo.go).
	memo map[any]accessMemoEntry
	// The public identities it names that weren't stored yet, stored once
	// it ends (notionDeferredIdentities).
	identities *notionDeferredIdentities
}

func (app *readSnapshotApp) DB() dbx.Builder              { return app.tx }
func (app *readSnapshotApp) ConcurrentDB() dbx.Builder    { return app.tx }
func (app *readSnapshotApp) NonconcurrentDB() dbx.Builder { return app.tx }
func (app *readSnapshotApp) IsTransactional() bool        { return true }

// RunInTransaction reads on in the snapshot.
func (app *readSnapshotApp) RunInTransaction(fn func(core.App) error) error { return fn(app) }

// openReadSnapshotPool opens the snapshots' pool on the data database. What
// logs the data database's statements (--dev, sql_trace.go) logs the
// snapshots' too; with neither set, nothing is called.
func openReadSnapshotPool(app core.App) (*dbx.DB, error) {
	db, err := leafenDBConnect(filepath.Join(app.DataDir(), dataDatabaseFile), sqliteReadStatementCacheSize, "query_only(1)")
	if err != nil {
		return nil, err
	}
	if logged, ok := app.ConcurrentDB().(*dbx.DB); ok {
		db.QueryLogFunc, db.ExecLogFunc = logged.QueryLogFunc, logged.ExecLogFunc
	}
	db.DB().SetMaxOpenConns(readSnapshotConnections())
	db.DB().SetMaxIdleConns(readSnapshotConnections())
	db.DB().SetConnMaxIdleTime(3 * time.Minute)
	return db, nil
}

// closeReadSnapshotPool closes the snapshots' pool when the server stops.
func closeReadSnapshotPool(app core.App) func(context.Context) error {
	return func(context.Context) error {
		db, ok := app.Store().Get(readSnapshotPoolStoreKey).(*dbx.DB)
		if !ok || db == nil {
			return nil
		}
		app.Store().Remove(readSnapshotPoolStoreKey)
		return db.Close()
	}
}

// runReadSnapshot runs fn in a read snapshot; inside a transaction already,
// or without the snapshots' pool, it reads in a transaction as before. The
// public identities the snapshot names that weren't stored yet are stored
// once it ends, before what it read goes out (notionDeferredIdentities), as
// a transaction stores one as it names it: one a snapshot named failed to
// store before 2026-11-22, and an MCP read then named a data source by its
// own ID rather than its public one.
func runReadSnapshot(ctx context.Context, app core.App, fn func(core.App) error) error {
	db, ok := app.Store().Get(readSnapshotPoolStoreKey).(*dbx.DB)
	if !ok || db == nil || app.IsTransactional() {
		return app.RunInTransaction(fn)
	}
	identities := newNotionDeferredIdentities()
	if err := readInSnapshot(ctx, app, db, identities, fn); err != nil {
		return err
	}
	return identities.store(app)
}

// readInSnapshot runs fn in a snapshot on a connection of the snapshots'
// pool, which it gives back as fn returns.
func readInSnapshot(
	ctx context.Context, app core.App, db *dbx.DB, identities *notionDeferredIdentities, fn func(core.App) error,
) error {
	conn, err := db.DB().Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	sqlTx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer sqlTx.Rollback()
	return fn(&readSnapshotApp{App: app, tx: db.Wrap(sqlTx), identities: identities})
}
