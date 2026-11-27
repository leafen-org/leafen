package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// A request's writes stop with the server, as the collaboration's do
// (ygo_persistence.go runOperationTransaction). Most of its statements bring
// no context of their own, and one still running when the server stopped held
// the write connection through the parts' stop — their last writes found
// the database busy — and went on until the process ended with it, its
// client told nothing but a cut connection (2026-11-17: a profile save held
// across a stop was answered by no one after the stop's 3.6 s, and the
// sign-in sessions' last uses were lost).
//
// A request that may write runs against an app whose transactions the
// server's stop can end (requestWriteApp), and its handler writes through
// that app rather than a part's own (workspace_import_batch.go, as the rest
// do). Once the server begins to stop, those under way get until half its
// time to end (stopRequestWrites), as the collaboration's do, and those
// still running are then stopped, before the server stops its parts: SQLite
// interrupts the statement running, the transaction is rolled back and none
// of it is kept. The request is told busy, as one the server had no turn for
// (serverBusy), when none of its transactions had kept a change before — one
// that changed no row keeps nothing (transactionRowChanges) — the app sends it
// again once the server answers, and is noted as stopped, not as a failure.
//
// A transaction begun after that — a request let in before the stop whose
// call finished arriving after it — runs until the closing writes' time
// (serverStop.closing, shutdown.go), as the collaboration's begun then do, so
// a slow one is stopped before the parts' last writes queued behind it give
// up; and once the parts have stopped, those still running are stopped and
// waited for, and any begun later refused, before PocketBase closes its
// listener (endRequestWrites). Run as any other write, one held by a slow
// write went on past the listener's close, and the process ended with it,
// its client told nothing but a cut connection (2026-11-18: each of 8 calls
// whose last bytes came within 80 ms of the stop).
//
// A transaction is the request's while its handler runs: the app may be
// kept by work the request leaves behind (product_afterwork.go), whose
// writes after the request was answered run as any other. A part first made
// during a request keeps the server's own app (serverAppOf,
// server_own_app.go), for its last writes come once the requests' are ended.

const (
	requestWritesStoreKey = "leafen.requestWrites"
	requestWriteNoteKey   = "leafen.requestWriteNote"
)

// errRequestWriteStopped is a request's statement the server's stop ended.
var errRequestWriteStopped = fmt.Errorf("leafen: the server stopped the request's write: %w", context.Canceled)

// requestWriteStop is what the requests' transactions run under: once it
// ends, the statement running is interrupted and the next refused, each as
// stopped by the server (errRequestWriteStopped) — told apart from one that
// brings its own context and was ended by it, as an administrator's change
// is, whose request keeps its own answer (administrator_mutation_lifecycle.go).
type requestWriteStop struct{ context.Context }

func (stop requestWriteStop) Err() error {
	if stop.Context.Err() != nil {
		return errRequestWriteStopped
	}
	return nil
}

// requestWrites are the transactions of requests under way.
type requestWrites struct {
	mu     sync.Mutex
	active int
	idle   chan struct{}    // closed while none runs
	ended  bool             // the server ended them: one begun now is refused
	stop   requestWriteStop // what one begun now runs under
	cancel context.CancelFunc
}

func requestWritesOf(app core.App) *requestWrites {
	writes, _ := app.Store().GetOrSet(requestWritesStoreKey, func() any {
		stop, cancel := context.WithCancel(context.Background())
		idle := make(chan struct{})
		close(idle)
		return &requestWrites{idle: idle, stop: requestWriteStop{stop}, cancel: cancel}
	}).(*requestWrites)
	return writes
}

// begin counts a request's transaction under way and gives the stop its
// statements run under; false once the server ended the requests' writes.
func (writes *requestWrites) begin() (context.Context, func(), bool) {
	writes.mu.Lock()
	defer writes.mu.Unlock()
	if writes.ended {
		return nil, nil, false
	}
	if writes.active == 0 {
		writes.idle = make(chan struct{})
	}
	writes.active++
	return writes.stop, writes.end, true
}

func (writes *requestWrites) end() {
	writes.mu.Lock()
	defer writes.mu.Unlock()
	writes.active--
	if writes.active == 0 {
		close(writes.idle)
	}
}

// stopRequestWrites lets the requests' transactions under way end until a
// closing server's stop.underWay (zero, not at all), then stops those still
// running; those begun from then on run until stop.closing (zero, until
// endRequestWrites).
func stopRequestWrites(ctx context.Context, app core.App, stop serverStop) {
	writes := requestWritesOf(app)
	writes.mu.Lock()
	idle := writes.idle
	writes.mu.Unlock()
	if !stop.underWay.IsZero() {
		timer := time.NewTimer(time.Until(stop.underWay))
		select {
		case <-idle:
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
	}
	closing, cancel := context.WithCancel(context.Background())
	if !stop.closing.IsZero() {
		closing, cancel = context.WithDeadline(context.Background(), stop.closing)
	}
	writes.mu.Lock()
	underWay := writes.cancel
	writes.stop, writes.cancel = requestWriteStop{closing}, cancel
	writes.mu.Unlock()
	underWay()
}

// endRequestWrites stops the requests' transactions still running, refuses
// those begun from now on, and waits until those running have ended or ctx
// does: once a closing server's parts have stopped, before the read log and
// the replica write their last and PocketBase closes its listener, which
// gives the requests still under way a second to be answered.
func endRequestWrites(ctx context.Context, app core.App) {
	writes := requestWritesOf(app)
	writes.mu.Lock()
	writes.ended = true
	cancel, idle := writes.cancel, writes.idle
	writes.mu.Unlock()
	cancel()
	select {
	case <-idle:
	case <-ctx.Done():
	}
}

// requestWriteNote is how a request's transactions went.
type requestWriteNote struct {
	answered  atomic.Bool // its handler returned
	committed atomic.Bool // one of its transactions ended having changed a row
	stopped   atomic.Bool // the server stopped one, or the request before it answered
}

// requestWriteApp is the app a request that may write runs against.
type requestWriteApp struct {
	core.App
	writes *requestWrites
	note   *requestWriteNote
}

func (app requestWriteApp) RunInTransaction(fn func(txApp core.App) error) error {
	if app.note.answered.Load() {
		return app.App.RunInTransaction(fn)
	}
	stop, end, ok := app.writes.begin()
	if !ok {
		// Begun once the server ended the requests' writes: none of it runs.
		app.note.stopped.Store(true)
		return errRequestWriteStopped
	}
	defer end()
	changed := true
	err := app.App.RunInTransaction(func(txApp core.App) error {
		// Its statements run under the stop; its commit, after fn, does not.
		defer interruptWriteStatementsOn(app.App, stop)()
		before, counted := transactionRowChanges(txApp)
		if err := fn(txApp); err != nil {
			return err
		}
		if after, ok := transactionRowChanges(txApp); counted && ok && after == before {
			changed = false
		}
		// One the stop ended part way is rolled back whole, though fn went on.
		return stop.Err()
	})
	switch {
	case err == nil && changed:
		app.note.committed.Store(true)
	case requestWriteStoppedBy(err):
		app.note.stopped.Store(true)
	}
	return err
}

// transactionRowChanges is how many rows the connection of a transaction has
// changed since it opened, triggers' changes among them; false when it could
// not be read. A transaction that changed none kept nothing, though it ended:
// a read in one, as a read snapshot reads once the server's stop has closed
// their pool (runReadSnapshot). Counted as kept, an MCP call the stop ended
// before it made its page, its client check read so, was told its answer was
// lost after part of it was saved rather than busy (2026-11-18).
func transactionRowChanges(txApp core.App) (int64, bool) {
	var changes int64
	if err := txApp.NonconcurrentDB().NewQuery(`SELECT total_changes()`).Row(&changes); err != nil {
		return 0, false
	}
	return changes, true
}

// requestWriteStoppedBy is whether err is a request's transaction the
// server's stop ended — with its rollback's failure beside it when SQLite
// had already rolled the interrupted transaction back.
func requestWriteStoppedBy(err error) bool {
	if errors.Is(err, errRequestWriteStopped) {
		return true
	}
	var combined dbx.Errors
	if errors.As(err, &combined) {
		for _, child := range combined {
			if requestWriteStoppedBy(child) {
				return true
			}
		}
	}
	return false
}

// requestMayWrite is whether a request is one that may write.
func requestMayWrite(request *http.Request) bool {
	switch request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// registerRequestWriteStop has the requests that may write run against an
// app whose transactions the server's stop ends, and a request whose write
// it ended told busy when its handler answered nothing.
func registerRequestWriteStop(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		// Before the sign-in with a key, whose app keeps this one (user_key_auth.go).
		Id: "leafenRequestWriteStop", Priority: apis.DefaultPanicRecoverMiddlewarePriority + 2,
		Func: func(request *core.RequestEvent) error {
			if !requestMayWrite(request.Request) {
				return request.Next()
			}
			note := &requestWriteNote{}
			defer note.answered.Store(true)
			request.Set(requestWriteNoteKey, note)
			request.App = requestWriteApp{App: request.App, writes: requestWritesOf(request.App), note: note}
			err := request.Next()
			if !request.Written() && requestWriteStopped(request) {
				return serverBusy(request)
			}
			return err
		},
	})
}

// requestWriteStopped is whether the server's stop ended a request's
// transaction with none of its transactions kept before: none of it kept.
func requestWriteStopped(event *core.RequestEvent) bool {
	if event == nil {
		return false
	}
	note, _ := event.Get(requestWriteNoteKey).(*requestWriteNote)
	return note != nil && note.stopped.Load() && !note.committed.Load()
}

// stoppedRequestWriteAnswer is what a failed request is answered: busy when
// the server's stop ended its write with none of it kept, else as it was.
func stoppedRequestWriteAnswer(event *core.RequestEvent, status int, code, message string) (int, string, string) {
	if status < http.StatusBadRequest || !requestWriteStopped(event) {
		return status, code, message
	}
	header := event.Response.Header()
	header.Set("Retry-After", requestAdmissionRetryAfter)
	header.Set("Connection", "close")
	return http.StatusServiceUnavailable, errorCodeServerBusy, serverBusyMessage
}
