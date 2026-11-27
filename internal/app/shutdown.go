package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// serverStop is when a closing server ends each kind of its writes, as
// shares of the time from when it began to stop to its deadline, in the
// order that lets a write queued behind one it stops have the write
// connection once that one is stopped (2026-11-18: a room's last flush a
// slow write held kept the connection until it was stopped at three
// quarters, and the parts' last writes queued behind it — the apps' usage
// and the search index's settling — gave up waiting at half and were lost):
//   - the writes under way when it began, the requests' and the
//     collaboration's, end by half of it and are then stopped (underWay:
//     stopRequestWrites, beginShutdown);
//   - those begun once it is closing, each room's last flush and the
//     collaboration's follow-ups among them and the requests' let in before,
//     by three quarters, and are then stopped and rolled back (closing:
//     closingStop, stopRequestWrites);
//   - the waits for the write connection end at seven eighths (waits:
//     closeWriteConnectionWaits), after both: a part's last write queued
//     behind a write that was stopped has the connection then, and an eighth
//     is left for what is written once the parts have stopped, the read log
//     and the replica's last sending.
//
// The requests' writes still running once the parts have stopped — sooner
// than three quarters when they stop fast — are stopped then, and any begun
// later refused, before the read log, the replica and PocketBase's listener
// close (endRequestWrites).
//
// Its times are zero when ctx has no deadline: nothing waits.
type serverStop struct {
	underWay, closing, waits time.Time
}

func serverStopOf(ctx context.Context) serverStop {
	deadline, ok := ctx.Deadline()
	if !ok {
		return serverStop{}
	}
	now := time.Now()
	left := deadline.Sub(now)
	return serverStop{underWay: now.Add(left / 2), closing: now.Add(left * 3 / 4), waits: now.Add(left * 7 / 8)}
}

// The server's parts stop together, and a part that failed to stop fails
// the stop: the process ends as failed (exit 1). But not for a last write
// the server can do without. A write under way since before the server
// began to stop can hold the write connection past the time the parts'
// writes are given (closeWriteConnectionWaits, sqlite_write_gate.go), and a
// part's last write then finds the database busy (2026-11-17: a profile save
// held across a stop kept the sessions' last uses from being written, and
// the process ended as failed in every stop; a held page write, which the
// collaboration's stop ends first, did not).
//
// A part whose last write keeps only what can be lost — when something was
// last used, which its next use writes again — is named with
// lastWriteMayBeLost: when that write finds the database busy, it is noted
// as lost and the stop is not failed for it. Any other failure of such a
// part, and every failure of the other parts, still fails the stop: the
// collaboration's last appends and the read log (app.go) keep what people
// made or what an administrator chose to keep, and are not given up so.

func shutdownProductComponents(ctx context.Context, shutdowns ...func(context.Context) error) error {
	results := make([]error, len(shutdowns))
	var wait sync.WaitGroup
	wait.Add(len(shutdowns))
	for index, shutdown := range shutdowns {
		go func(index int, shutdown func(context.Context) error) {
			defer wait.Done()
			results[index] = shutdown(ctx)
		}(index, shutdown)
	}
	wait.Wait()
	for index, err := range results {
		var lost *lostLastWrite
		if errors.As(err, &lost) && isSQLiteBusyError(lost.err) {
			lost.app.Logger().Warn("leafen last write lost as the server stopped", "what", lost.what, "error", lost.err)
			results[index] = nil
		}
	}
	return errors.Join(results...)
}

// lostLastWrite is the failure of a part whose last write may be lost.
type lostLastWrite struct {
	app  core.App
	what string
	err  error
}

func (lost *lostLastWrite) Error() string { return lost.what + ": " + lost.err.Error() }

func (lost *lostLastWrite) Unwrap() error { return lost.err }

// lastWriteMayBeLost names a part whose last write, what, may be lost as the
// server stops.
func lastWriteMayBeLost(app core.App, what string, shutdown func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := shutdown(ctx); err != nil {
			return &lostLastWrite{app: app, what: what, err: err}
		}
		return nil
	}
}
