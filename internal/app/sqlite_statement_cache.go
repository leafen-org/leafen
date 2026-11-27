package app

import (
	"container/list"
	"context"
	"database/sql"
	"database/sql/driver"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Statements stay compiled on their connection (2026-10-14 load
// measurement): database/sql prepares, runs and finalizes a statement each
// time it isn't prepared beforehand, and SQLite compiles a statement with
// the triggers of the tables it writes — a block's upsert compiled six
// trigger programs on every change, 1.65 ms against 0.46 ms once compiled,
// and the one write connection every change waits for spent most of its
// time compiling. Each data connection keeps the statements it ran last, by
// their text, and runs them again (the driver resets one after each use); a
// statement whose rows are still being read, or one too long to keep, is
// prepared afresh as before. A compiled statement with triggers is large,
// so the one write connection keeps the most; a read connection keeps as
// many as a request runs (2026-10-14: with 24, a database query compiled a
// sixth of its statements again each time).
//
// The data database is also read through a memory map: SQLite as modernc
// builds it keeps every connection's pages in one cache behind one lock,
// and under 200 concurrent database queries half the server's time went to
// that lock; a read through the map takes no lock. The data database never
// shrinks under a mapping (no auto-vacuum, no VACUUM of it); a disk failing
// under the map stops the server rather than failing a request.

const (
	sqliteWriteStatementCacheSize = 256
	sqliteReadStatementCacheSize  = 128
	sqliteStatementCacheMaxBytes  = 16 << 10
	dataDatabaseFile              = "data.db"
)

// dataDatabaseOpens counts the data database's openings by path:
// PocketBase opens its read pool first and its write connection second
// (core.BaseApp.initDataDB).
var dataDatabaseOpens = struct {
	sync.Mutex
	byPath map[string]int
}{byPath: map[string]int{}}

// pocketBaseDBConnect opens PocketBase's databases as it does
// (core.DefaultDBConnect), the data one keeping its statements compiled.
func pocketBaseDBConnect(dbPath string) (*dbx.DB, error) {
	if filepath.Base(dbPath) != dataDatabaseFile {
		return core.DefaultDBConnect(dbPath)
	}
	dataDatabaseOpens.Lock()
	dataDatabaseOpens.byPath[dbPath]++
	writer := dataDatabaseOpens.byPath[dbPath]%2 == 0
	dataDatabaseOpens.Unlock()
	if writer {
		// The one write connection starts its transactions holding the
		// write lock: another writer — the continuous replica's checkpoints
		// (replica_runner.go) — then makes a transaction that reads first
		// wait for the lock rather than fail when it goes on to write. Its
		// checkpoints are taken apart from its commits (wal_checkpoint.go).
		return leafenDBConnectBeginning(dbPath, sqliteWriteStatementCacheSize, "immediate", "wal_autocheckpoint(0)")
	}
	return leafenDBConnect(dbPath, sqliteReadStatementCacheSize)
}

// leafenDBConnect opens the data database with PocketBase's pragmas and
// any more given, each connection keeping up to size statements compiled.
func leafenDBConnect(dbPath string, size int, pragmas ...string) (*dbx.DB, error) {
	return leafenDBConnectBeginning(dbPath, size, "", pragmas...)
}

// leafenDBConnectBeginning opens it with how its transactions begin as well
// ("immediate"), or SQLite's own way with "".
func leafenDBConnectBeginning(dbPath string, size int, begin string, pragmas ...string) (*dbx.DB, error) {
	// PocketBase's pragmas, as core.DefaultDBConnect gives them.
	// The page cache and the memory map follow the server's memory
	// (memory_plan.go): 32 MB a connection and a 2 GB map on a large machine.
	dsn := dbPath + "?_pragma=busy_timeout(" + strconv.FormatInt(sqliteBusyTimeout.Milliseconds(), 10) + ")" +
		"&_pragma=journal_mode(WAL)&_pragma=journal_size_limit(200000000)" +
		"&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)" +
		"&_pragma=cache_size(-" + strconv.FormatInt(sqlitePageCacheKiB(), 10) + ")&_defensive=1" +
		"&_pragma=mmap_size(" + strconv.FormatInt(sqliteMemoryMap(), 10) + ")"
	for _, pragma := range pragmas {
		dsn += "&_pragma=" + pragma
	}
	if begin != "" {
		dsn += "&_txlock=" + begin
	}
	// The driver core.DefaultDBConnect opens, under the same name.
	probe, err := sql.Open("sqlite", "")
	if err != nil {
		return nil, err
	}
	base := probe.Driver()
	_ = probe.Close()
	if begin == "immediate" {
		// Its transactions take the write lock as they begin: the one write
		// connection, lent to one writer at a time (sqlite_write_gate.go).
		return openWriteConnection(&statementCachingConnector{dsn: dsn, driver: base, size: size}), nil
	}
	return dbx.NewFromDB(sql.OpenDB(&statementCachingConnector{dsn: dsn, driver: base, size: size}), "sqlite"), nil
}

type statementCachingConnector struct {
	dsn    string
	driver driver.Driver
	size   int
}

func (connector *statementCachingConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := connector.open()
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (connector *statementCachingConnector) open() (*statementCachingConn, error) {
	conn, err := connector.driver.Open(connector.dsn)
	if err != nil {
		return nil, err
	}
	return &statementCachingConn{inner: conn, size: connector.size, statements: map[string]*cachedStatement{}, order: list.New()}, nil
}

func (connector *statementCachingConnector) Driver() driver.Driver { return connector.driver }

type cachedStatement struct {
	stmt    driver.Stmt
	busy    bool
	element *list.Element
}

// statementCachingConn is one connection; database/sql uses a connection
// from one goroutine at a time, so its cache needs no lock.
type statementCachingConn struct {
	inner      driver.Conn
	size       int
	statements map[string]*cachedStatement
	order      *list.List // the queries, most recently run first
}

// statement is the connection's compiled statement for a query, nil when it
// is in use or isn't kept.
func (conn *statementCachingConn) statement(ctx context.Context, query string) *cachedStatement {
	if len(query) > sqliteStatementCacheMaxBytes {
		return nil
	}
	if entry, found := conn.statements[query]; found {
		if entry.busy {
			return nil
		}
		conn.order.MoveToFront(entry.element)
		return entry
	}
	preparer, ok := conn.inner.(driver.ConnPrepareContext)
	if !ok {
		return nil
	}
	stmt, err := preparer.PrepareContext(ctx, query)
	if err != nil {
		return nil
	}
	entry := &cachedStatement{stmt: stmt}
	entry.element = conn.order.PushFront(query)
	conn.statements[query] = entry
	for element := conn.order.Back(); conn.order.Len() > conn.size && element != nil; {
		previous := element.Prev()
		if oldest := conn.statements[element.Value.(string)]; !oldest.busy {
			_ = oldest.stmt.Close()
			delete(conn.statements, element.Value.(string))
			conn.order.Remove(element)
		}
		element = previous
	}
	return entry
}

func (conn *statementCachingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	entry := conn.statement(ctx, query)
	if entry == nil {
		if execer, ok := conn.inner.(driver.ExecerContext); ok {
			return execer.ExecContext(ctx, query, args)
		}
		return nil, driver.ErrSkip
	}
	execer, ok := entry.stmt.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	entry.busy = true
	defer func() { entry.busy = false }()
	return execer.ExecContext(ctx, args)
}

func (conn *statementCachingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	entry := conn.statement(ctx, query)
	if entry == nil {
		if queryer, ok := conn.inner.(driver.QueryerContext); ok {
			return queryer.QueryContext(ctx, query, args)
		}
		return nil, driver.ErrSkip
	}
	queryer, ok := entry.stmt.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	entry.busy = true
	rows, err := queryer.QueryContext(ctx, args)
	if err != nil {
		entry.busy = false
		return nil, err
	}
	return &cachedStatementRows{Rows: rows, entry: entry}, nil
}

func (conn *statementCachingConn) Prepare(query string) (driver.Stmt, error) {
	return conn.inner.Prepare(query)
}

func (conn *statementCachingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if preparer, ok := conn.inner.(driver.ConnPrepareContext); ok {
		return preparer.PrepareContext(ctx, query)
	}
	return conn.inner.Prepare(query)
}

func (conn *statementCachingConn) Begin() (driver.Tx, error) {
	return conn.inner.Begin()
}

func (conn *statementCachingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if beginner, ok := conn.inner.(driver.ConnBeginTx); ok {
		return beginner.BeginTx(ctx, opts)
	}
	return conn.inner.Begin()
}

func (conn *statementCachingConn) Close() error {
	for query, entry := range conn.statements {
		_ = entry.stmt.Close()
		delete(conn.statements, query)
	}
	conn.order.Init()
	return conn.inner.Close()
}

func (conn *statementCachingConn) Ping(ctx context.Context) error {
	if pinger, ok := conn.inner.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

func (conn *statementCachingConn) ResetSession(ctx context.Context) error {
	if resetter, ok := conn.inner.(driver.SessionResetter); ok {
		return resetter.ResetSession(ctx)
	}
	return nil
}

func (conn *statementCachingConn) IsValid() bool {
	if validator, ok := conn.inner.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

// cachedStatementRows gives a kept statement back once its rows are read.
type cachedStatementRows struct {
	driver.Rows
	entry *cachedStatement
}

func (rows *cachedStatementRows) Close() error {
	err := rows.Rows.Close()
	rows.entry.busy = false
	return err
}

func (rows *cachedStatementRows) ColumnTypeDatabaseTypeName(index int) string {
	if typed, ok := rows.Rows.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return typed.ColumnTypeDatabaseTypeName(index)
	}
	return ""
}

func (rows *cachedStatementRows) ColumnTypeLength(index int) (int64, bool) {
	if typed, ok := rows.Rows.(driver.RowsColumnTypeLength); ok {
		return typed.ColumnTypeLength(index)
	}
	return 0, false
}

func (rows *cachedStatementRows) ColumnTypeNullable(index int) (bool, bool) {
	if typed, ok := rows.Rows.(driver.RowsColumnTypeNullable); ok {
		return typed.ColumnTypeNullable(index)
	}
	return false, false
}

func (rows *cachedStatementRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	if typed, ok := rows.Rows.(driver.RowsColumnTypePrecisionScale); ok {
		return typed.ColumnTypePrecisionScale(index)
	}
	return 0, 0, false
}

func (rows *cachedStatementRows) ColumnTypeScanType(index int) reflect.Type {
	if typed, ok := rows.Rows.(driver.RowsColumnTypeScanType); ok {
		return typed.ColumnTypeScanType(index)
	}
	return reflect.TypeFor[any]()
}
