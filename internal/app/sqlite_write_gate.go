package app

import (
	"container/list"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// The one write connection is lent to one holder at a time, in the order
// they asked, and no one waits for it without end (2026-11-10: while another
// program held the database's write lock, each write that had the connection
// waited SQLite's ten seconds for the lock and failed, and the writes after
// it waited for the connection with no end of their own — database/sql
// waits as long as the caller's context, which most writes don't give, and
// hands the connection to a waiter at random, so the background's turns,
// asking again at once, took it before people's writes: these were answered
// after 30 to 100 seconds or not at all, and a stopping server waited for its
// background work past its deadline).
//
// A write waits for the connection, and then for the database's write lock,
// until its context's deadline or, when it gives none, writeConnectionWait —
// the connection's own wait for the lock is shortened to what is left of
// that — and no longer than its context lasts. It is then refused as busy (sqlite_errors.go), before any of it was
// written — each request answers busy in its own words, and the background
// tries again on its next turn, behind those already waiting. Once the server
// is closing, waits end when its stop says (serverStop, shutdown.go): after
// the writes it stops were stopped, so its background work, whose writes
// don't carry its context, stops waiting, while what is written last still can.
//
// The connection itself stays open between holders, keeping its compiled
// statements (sqlite_statement_cache.go); database/sql keeps no idle
// connection of its own and opens as many as are asked for, each a holder's
// turn here, so it never queues writes in front of the gate.

const (
	// sqliteBusyTimeout is how long a data connection waits for another's
	// write lock.
	sqliteBusyTimeout = 10 * time.Second
	// writeConnectionWait is how long a write waits in all, for the write
	// connection and the database's write lock, when its context gives no
	// deadline.
	writeConnectionWait = sqliteBusyTimeout
	// writeConnectionWaitSlack is how much longer than its time a write may
	// wait in all rather than have the connection's wait set for it alone.
	writeConnectionWaitSlack = 100 * time.Millisecond
)

// errWriteConnectionBusy is a write the write connection wasn't free for in
// time; it counts as SQLite's busy (sqlite_errors.go).
var errWriteConnectionBusy = errors.New("leafen: the write connection was not free in time")

// writeConnectionWaitError is a wait for the write connection that ended:
// busy, with the context's error that ended it when one did — canceled for
// a closing server's.
type writeConnectionWaitError struct{ cause error }

func (err *writeConnectionWaitError) Error() string {
	if err.cause != nil {
		return errWriteConnectionBusy.Error() + ": " + err.cause.Error()
	}
	return errWriteConnectionBusy.Error()
}

func (err *writeConnectionWaitError) Is(target error) bool { return target == errWriteConnectionBusy }

func (err *writeConnectionWaitError) Unwrap() error { return err.cause }

// waitedOutForWriteConnection is whether err is a write that waited out its
// time for the write connection.
func waitedOutForWriteConnection(err error) bool {
	if errors.Is(err, errWriteConnectionBusy) {
		return true
	}
	var combined dbx.Errors
	if errors.As(err, &combined) {
		for _, child := range combined {
			if waitedOutForWriteConnection(child) {
				return true
			}
		}
	}
	return false
}

// writeConnectionGates are the gates in front of open write connections.
var writeConnectionGates sync.Map // *sql.DB -> *writeConnectionGate

// writeConnectionGate lends the one write connection.
type writeConnectionGate struct {
	opener *statementCachingConnector
	db     *sql.DB

	mu        sync.Mutex
	held      bool
	waiting   list.List              // *writeConnectionWaiter, first asked first
	conn      *statementCachingConn  // open between holders
	holder    *writeConnectionHolder // who holds it now
	closing   chan struct{}          // closed once waits end by closingBy
	closingBy time.Time
	closed    bool
}

type writeConnectionWaiter struct {
	handed chan struct{}
	given  bool
}

// openWriteConnection is the write database, its connection lent through a gate.
func openWriteConnection(opener *statementCachingConnector) *dbx.DB {
	gate := &writeConnectionGate{opener: opener, closing: make(chan struct{})}
	gate.db = sql.OpenDB(gate)
	writeConnectionGates.Store(gate.db, gate)
	return dbx.NewFromDB(gate.db, "sqlite")
}

// writeConnectionGateOf is the gate in front of an app's write connection.
func writeConnectionGateOf(app core.App) *writeConnectionGate {
	database, ok := app.NonconcurrentDB().(*dbx.DB)
	if !ok {
		return nil
	}
	gate, _ := writeConnectionGates.Load(database.DB())
	held, _ := gate.(*writeConnectionGate)
	return held
}

// registerWriteConnectionGate leaves the waiting to the gate once PocketBase
// has opened the database, which gives its write pool one connection, kept
// idle.
func registerWriteConnectionGate(pb core.App) {
	pb.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
		Id: "leafenWriteConnectionGate",
		Func: func(event *core.BootstrapEvent) error {
			if err := event.Next(); err != nil {
				return err
			}
			if gate := writeConnectionGateOf(event.App); gate != nil {
				gate.db.SetMaxIdleConns(0)
				gate.db.SetMaxOpenConns(0)
			}
			return nil
		},
	})
}

// closeWriteConnectionWaits has the waits for an app's write connection end
// by by, a closing server's (serverStop.waits); a zero by, now.
func closeWriteConnectionWaits(app core.App, by time.Time) {
	if gate := writeConnectionGateOf(app); gate != nil {
		if by.IsZero() {
			by = time.Now()
		}
		gate.closeWaits(by)
	}
}

func (gate *writeConnectionGate) closeWaits(by time.Time) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.closingBy.IsZero() || by.Before(gate.closingBy) {
		gate.closingBy = by
	}
	select {
	case <-gate.closing:
	default:
		close(gate.closing)
	}
}

// waitEnd is when a wait that would end at by ends, and whether a closing
// server ends it.
func (gate *writeConnectionGate) waitEnd(by time.Time) (time.Time, bool) {
	if !gate.closingBy.IsZero() && gate.closingBy.Before(by) {
		return gate.closingBy, true
	}
	return by, false
}

// Connect lends the connection to one who asks, once those who asked before
// have had it, by the end of their wait.
func (gate *writeConnectionGate) Connect(ctx context.Context) (driver.Conn, error) {
	by := time.Now().Add(writeConnectionWait)
	if deadline, ok := ctx.Deadline(); ok {
		by = deadline
	}
	gate.mu.Lock()
	if gate.closed {
		gate.mu.Unlock()
		return nil, sql.ErrConnDone
	}
	end, closing := gate.waitEnd(by)
	if !gate.held && gate.waiting.Len() == 0 {
		gate.held = true
		gate.mu.Unlock()
		return gate.lend(end)
	}
	waiter := &writeConnectionWaiter{handed: make(chan struct{})}
	element := gate.waiting.PushBack(waiter)
	closingSignal := gate.closing
	gate.mu.Unlock()
	timer := time.NewTimer(time.Until(end))
	defer timer.Stop()
	var err error
	for err == nil {
		select {
		case <-waiter.handed:
			return gate.lend(end)
		case <-closingSignal:
			closingSignal = nil
			gate.mu.Lock()
			end, closing = gate.waitEnd(by)
			gate.mu.Unlock()
			timer.Reset(time.Until(end))
		case <-timer.C:
			err = &writeConnectionWaitError{}
			if closing {
				err = &writeConnectionWaitError{cause: context.Canceled}
			}
		case <-ctx.Done():
			err = ctx.Err()
			if errors.Is(err, context.DeadlineExceeded) {
				err = &writeConnectionWaitError{cause: err}
			}
		}
	}
	gate.mu.Lock()
	given := waiter.given
	if !given {
		gate.waiting.Remove(element)
	}
	gate.mu.Unlock()
	if given {
		// Handed over as the wait ended: on to the next.
		gate.giveBack(false)
	}
	return nil, err
}

func (gate *writeConnectionGate) Driver() driver.Driver { return gate.opener.Driver() }

// lend gives the connection to its holder, its wait for the database's write
// lock what is left until end.
func (gate *writeConnectionGate) lend(end time.Time) (driver.Conn, error) {
	gate.mu.Lock()
	conn := gate.conn
	gate.mu.Unlock()
	if conn != nil && !conn.IsValid() {
		_ = conn.Close()
		conn = nil
	}
	if conn == nil {
		opened, err := gate.opener.open()
		if err != nil {
			gate.mu.Lock()
			gate.conn = nil
			gate.mu.Unlock()
			gate.giveBack(false)
			return nil, err
		}
		conn = opened
	}
	holder := &writeConnectionHolder{gate: gate, conn: conn}
	gate.mu.Lock()
	gate.conn = conn
	gate.holder = holder
	gate.mu.Unlock()
	// One that hardly waited keeps the connection's own wait.
	if left := time.Until(end); sqliteBusyTimeout-left > writeConnectionWaitSlack {
		holder.shortened = true
		if err := conn.setBusyTimeout(max(left, 0)); err != nil {
			holder.bad = true
			_ = holder.Close()
			return nil, err
		}
	}
	return holder, nil
}

// giveBack hands the connection to the next waiting, closing it first when
// it broke or the database closed.
func (gate *writeConnectionGate) giveBack(broken bool) {
	gate.mu.Lock()
	var conn *statementCachingConn
	if (broken || gate.closed) && gate.conn != nil {
		conn, gate.conn = gate.conn, nil
	}
	gate.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if front := gate.waiting.Front(); front != nil {
		waiter := gate.waiting.Remove(front).(*writeConnectionWaiter)
		waiter.given = true
		close(waiter.handed)
		return
	}
	gate.held = false
}

// Close closes the connection once no one holds it; database/sql calls it
// when the database closes.
func (gate *writeConnectionGate) Close() error {
	writeConnectionGates.Delete(gate.db)
	gate.mu.Lock()
	gate.closed = true
	var conn *statementCachingConn
	if !gate.held {
		conn, gate.conn = gate.conn, nil
	}
	gate.mu.Unlock()
	gate.closeWaits(time.Now())
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// setBusyTimeout sets how long the connection waits for another's write lock.
func (conn *statementCachingConn) setBusyTimeout(wait time.Duration) error {
	execer, ok := conn.inner.(driver.ExecerContext)
	if !ok {
		return nil
	}
	_, err := execer.ExecContext(context.Background(), "PRAGMA busy_timeout = "+strconv.FormatInt(wait.Milliseconds(), 10), nil)
	return err
}

// writeConnectionHolder is the write connection while one holds it: a
// connection of database/sql's, closed when given back.
type writeConnectionHolder struct {
	gate      *writeConnectionGate
	conn      *statementCachingConn
	shortened bool
	bad       bool
	done      bool
	// stop is what the statements that bring no context of their own run
	// under (interruptWriteStatementsOn).
	stop atomic.Pointer[context.Context]
}

// interruptWriteStatementsOn has the statements of the transaction holding
// app's write connection run under stop until the returned func is called,
// those that bring no context of their own: once stop ends, SQLite
// interrupts the one running, the next is refused, and the transaction is
// rolled back, none of it kept. It is called inside that transaction, which
// holds the connection while it runs; its commit runs after, not stopped.
// (2026-11-11: a page change held by a slow trigger ran on past a closing
// server's signal, and the process ended with it unanswered.)
func interruptWriteStatementsOn(app core.App, stop context.Context) func() {
	gate := writeConnectionGateOf(app)
	if gate == nil || stop == nil || stop.Done() == nil {
		return func() {}
	}
	gate.mu.Lock()
	holder := gate.holder
	gate.mu.Unlock()
	if holder == nil {
		return func() {}
	}
	bound := &stop
	holder.stop.Store(bound)
	return func() { holder.stop.CompareAndSwap(bound, nil) }
}

// statementContext is the context a statement runs under: its own, or the
// stop its transaction was given when it brings none.
func (holder *writeConnectionHolder) statementContext(ctx context.Context) context.Context {
	if ctx != nil && ctx.Done() != nil {
		return ctx
	}
	if stop := holder.stop.Load(); stop != nil {
		return *stop
	}
	return ctx
}

// noted keeps whether the connection broke.
func (holder *writeConnectionHolder) noted(err error) error {
	if errors.Is(err, driver.ErrBadConn) {
		holder.bad = true
	}
	return err
}

func (holder *writeConnectionHolder) Prepare(query string) (driver.Stmt, error) {
	stmt, err := holder.conn.Prepare(query)
	return holder.prepared(stmt, err)
}

func (holder *writeConnectionHolder) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	stmt, err := holder.conn.PrepareContext(ctx, query)
	return holder.prepared(stmt, err)
}

func (holder *writeConnectionHolder) prepared(stmt driver.Stmt, err error) (driver.Stmt, error) {
	if err != nil {
		return nil, holder.noted(err)
	}
	return &writeConnectionStatement{Stmt: stmt, holder: holder}, nil
}

// writeConnectionStatement is a statement prepared on the write connection,
// run as its holder runs the rest (statementContext).
type writeConnectionStatement struct {
	driver.Stmt
	holder *writeConnectionHolder
}

func (stmt *writeConnectionStatement) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := stmt.Stmt.(driver.StmtExecContext)
	if !ok {
		return stmt.Stmt.Exec(statementValues(args)) //nolint:staticcheck // a driver without contexts
	}
	result, err := execer.ExecContext(stmt.holder.statementContext(ctx), args)
	return result, stmt.holder.noted(err)
}

func (stmt *writeConnectionStatement) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := stmt.Stmt.(driver.StmtQueryContext)
	if !ok {
		return stmt.Stmt.Query(statementValues(args)) //nolint:staticcheck // a driver without contexts
	}
	rows, err := queryer.QueryContext(stmt.holder.statementContext(ctx), args)
	return rows, stmt.holder.noted(err)
}

func statementValues(args []driver.NamedValue) []driver.Value {
	values := make([]driver.Value, len(args))
	for index, arg := range args {
		values[index] = arg.Value
	}
	return values
}

func (holder *writeConnectionHolder) Begin() (driver.Tx, error) {
	tx, err := holder.conn.Begin()
	return tx, holder.noted(err)
}

func (holder *writeConnectionHolder) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := holder.conn.BeginTx(ctx, opts)
	return tx, holder.noted(err)
}

func (holder *writeConnectionHolder) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	result, err := holder.conn.ExecContext(holder.statementContext(ctx), query, args)
	return result, holder.noted(err)
}

func (holder *writeConnectionHolder) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := holder.conn.QueryContext(holder.statementContext(ctx), query, args)
	return rows, holder.noted(err)
}

func (holder *writeConnectionHolder) Ping(ctx context.Context) error {
	return holder.noted(holder.conn.Ping(ctx))
}

func (holder *writeConnectionHolder) ResetSession(ctx context.Context) error {
	return holder.noted(holder.conn.ResetSession(ctx))
}

func (holder *writeConnectionHolder) IsValid() bool {
	if !holder.conn.IsValid() {
		holder.bad = true
	}
	return !holder.bad
}

// Close gives the connection back, its wait for the write lock as it was.
func (holder *writeConnectionHolder) Close() error {
	if holder.done {
		return nil
	}
	holder.done = true
	holder.stop.Store(nil)
	holder.gate.mu.Lock()
	if holder.gate.holder == holder {
		holder.gate.holder = nil
	}
	holder.gate.mu.Unlock()
	if holder.shortened && !holder.bad && holder.conn.setBusyTimeout(sqliteBusyTimeout) != nil {
		holder.bad = true
	}
	holder.gate.giveBack(holder.bad)
	return nil
}
