package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	ygoCompactionTurnDelay  = time.Millisecond
	ygoCompactionRetryDelay = 100 * time.Millisecond
)

var errYgoCompactionRetryConflict = errors.New("ygo compaction retry state conflict")

type ygoCompactionCandidate struct {
	Kind          string `db:"kind"`
	Room          string `db:"room"`
	Updated       string `db:"updated"`
	NextAttemptAt int64  `db:"next_attempt_at"`
}

type ygoCompactionRunner struct {
	app         core.App
	persistence *pocketBaseYgoPersistence
	clock       durableJobClock
	wake        chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newYgoCompactionRunner(
	app core.App,
	persistence *pocketBaseYgoPersistence,
) *ygoCompactionRunner {
	return newYgoCompactionRunnerWithClock(app, persistence, realDurableJobClock{})
}

func newYgoCompactionRunnerWithClock(
	app core.App,
	persistence *pocketBaseYgoPersistence,
	clock durableJobClock,
) *ygoCompactionRunner {
	runner := &ygoCompactionRunner{
		app: app, persistence: persistence, clock: clock, wake: make(chan struct{}, 1),
	}
	persistence.setCompactionNotifier(runner.notify)
	return runner
}

func (runner *ygoCompactionRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel = cancel
	runner.done = make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *ygoCompactionRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	cancel := runner.cancel
	done := runner.done
	runner.cancel = nil
	runner.done = nil
	runner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", errorCodeYgoShutdown, ctx.Err())
	}
}

// notifyYgoCompactionAfterCommit wakes the compaction runner once the change
// that appended an update to a room's stream commits — every append, the
// one place a stream comes due: the runner sleeps until woken or until the
// next attempt it knows of. Appends the server makes outside a room's own
// writes (a page made under a page, blocks moved or copied, a table or a tab
// rebuilt) woke it never, so a page's structure document filled its backlog
// to ygoMaximumPendingUpdates and every page made under it after failed
// until something else woke the runner, minutes later (2026-11-18: page
// 4,097 under one page).
func notifyYgoCompactionAfterCommit(app core.App) {
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok {
		return
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				persistence.notifyCompaction()
			}
			return nil
		})
		return
	}
	persistence.notifyCompaction()
}

func (runner *ygoCompactionRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *ygoCompactionRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait, err := runner.runTurnWait(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeYgoPersistence, "ygo.compaction.continue", err)
		}
		if wait == 0 {
			continue
		}

		var clock durableJobTimer
		var timer <-chan time.Time
		if wait > 0 {
			clock = runner.clock.newTimer(wait)
			timer = clock.channelValue()
		}
		select {
		case <-ctx.Done():
			if clock != nil {
				clock.stop()
			}
			return
		case <-runner.wake:
		case <-timer:
		}
		if clock != nil {
			clock.stop()
		}
	}
}

func (runner *ygoCompactionRunner) runTurn(ctx context.Context) (bool, error) {
	wait, err := runner.runTurnWait(ctx)
	return wait >= 0, err
}

func (runner *ygoCompactionRunner) runTurnWait(ctx context.Context) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	now := runner.clock.now().UTC().UnixMilli()
	ready, found, err := runner.nextCompactionCandidate(now)
	if err != nil {
		return ygoCompactionRetryDelay, err
	}
	if !found {
		return runner.nextWait(ctx)
	}
	stream := ygoCollaborationStream
	if ready.Kind == "structural" {
		stream = ygoStructuralStream
	}
	// People's edits first (write_lane.go).
	yieldToPeople(ctx)
	remaining, err := runner.persistence.compactRoomTurn(ctx, stream, ready.Room)
	if err != nil {
		if ctx.Err() != nil {
			return -1, err
		}
		scheduleErr := runner.scheduleCompactionRetry(ctx, stream, ready.Room, now)
		wait, waitErr := runner.nextWait(ctx)
		return ygoCompactionPostTurnWait(wait), errors.Join(err, scheduleErr, waitErr)
	}
	if !remaining {
		if err := runner.clearCompactionRetry(ctx, stream, ready.Room); err != nil {
			return ygoCompactionRetryDelay, err
		}
	}
	wait, err := runner.nextWait(ctx)
	return ygoCompactionPostTurnWait(wait), err
}

func (runner *ygoCompactionRunner) scheduleCompactionRetry(
	ctx context.Context,
	stream ygoStreamTables,
	room string,
	now int64,
) error {
	result, err := runner.app.DB().NewQuery(`
		UPDATE {{` + stream.head + `}} SET [[next_attempt_at]] = {:nextAttemptAt}
		WHERE [[room]] = {:room} AND [[compaction_status]] = 'pending'
	`).WithContext(ctx).Bind(dbx.Params{
		"room":          room,
		"nextAttemptAt": now + ygoCompactionRetryDelay.Milliseconds(),
	}).Execute()
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errYgoCompactionRetryConflict
	}
	return nil
}

func (runner *ygoCompactionRunner) clearCompactionRetry(
	ctx context.Context,
	stream ygoStreamTables,
	room string,
) error {
	_, err := runner.app.DB().NewQuery(`
		UPDATE {{` + stream.head + `}} SET [[next_attempt_at]] = 0
		WHERE [[room]] = {:room} AND [[compaction_status]] = 'idle'
	`).WithContext(ctx).Bind(dbx.Params{"room": room}).Execute()
	return err
}

func (runner *ygoCompactionRunner) nextCompactionCandidate(
	now int64,
) (ygoCompactionCandidate, bool, error) {
	ready := []ygoCompactionCandidate{}
	if err := runner.app.DB().NewQuery(`
		SELECT 'collaboration' AS [[kind]], [[room]], [[updated]], [[next_attempt_at]] FROM (
			SELECT [[room]], [[updated]], [[next_attempt_at]] FROM {{leafen_ygo_documents}}
			WHERE [[compaction_status]] = 'pending' AND [[next_attempt_at]] <= {:now}
			ORDER BY [[next_attempt_at]], [[updated]], [[room]] LIMIT 1
		)
		UNION ALL
		SELECT 'structural' AS [[kind]], [[room]], [[updated]], [[next_attempt_at]] FROM (
			SELECT [[room]], [[updated]], [[next_attempt_at]] FROM {{leafen_ygo_structural_documents}}
			WHERE [[compaction_status]] = 'pending' AND [[next_attempt_at]] <= {:now}
			ORDER BY [[next_attempt_at]], [[updated]], [[room]] LIMIT 1
		)
	`).Bind(dbx.Params{"now": now}).All(&ready); err != nil {
		return ygoCompactionCandidate{}, false, err
	}
	if len(ready) == 0 {
		return ygoCompactionCandidate{}, false, nil
	}
	if len(ready) == 1 {
		return ready[0], true, nil
	}
	if len(ready) != 2 {
		return ygoCompactionCandidate{}, false, fmt.Errorf(
			"%s: compaction candidate count %d", errorCodeYgoPersistence, len(ready),
		)
	}
	if ygoCompactionCandidateBefore(ready[0], ready[1]) {
		return ready[0], true, nil
	}
	return ready[1], true, nil
}

func (runner *ygoCompactionRunner) nextWait(ctx context.Context) (time.Duration, error) {
	ready := []struct {
		NextAttemptAt int64 `db:"next_attempt_at"`
	}{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[next_attempt_at]] FROM (
			SELECT [[next_attempt_at]], [[updated]], [[room]] FROM {{leafen_ygo_documents}}
			WHERE [[compaction_status]] = 'pending'
			ORDER BY [[next_attempt_at]], [[updated]], [[room]] LIMIT 1
		)
		UNION ALL
		SELECT [[next_attempt_at]] FROM (
			SELECT [[next_attempt_at]], [[updated]], [[room]] FROM {{leafen_ygo_structural_documents}}
			WHERE [[compaction_status]] = 'pending'
			ORDER BY [[next_attempt_at]], [[updated]], [[room]] LIMIT 1
		)
	`).WithContext(ctx).All(&ready); err != nil {
		return ygoCompactionRetryDelay, err
	}
	if len(ready) == 0 {
		return -1, nil
	}
	next := sql.NullInt64{}
	for _, candidate := range ready {
		if !next.Valid || candidate.NextAttemptAt < next.Int64 {
			next.Int64 = candidate.NextAttemptAt
			next.Valid = true
		}
	}
	wait := time.Duration(next.Int64-runner.clock.now().UTC().UnixMilli()) * time.Millisecond
	if wait <= 0 {
		return 0, nil
	}
	return wait, nil
}

func ygoCompactionPostTurnWait(wait time.Duration) time.Duration {
	if wait == 0 {
		return ygoCompactionTurnDelay
	}
	return wait
}

func ygoCompactionCandidateBefore(left, right ygoCompactionCandidate) bool {
	if left.NextAttemptAt != right.NextAttemptAt {
		return left.NextAttemptAt < right.NextAttemptAt
	}
	if left.Updated != right.Updated {
		return left.Updated < right.Updated
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	return left.Room < right.Room
}
