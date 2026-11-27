package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	scimOperationTurnDelay  = time.Millisecond
	scimOperationRetryDelay = 100 * time.Millisecond
)

var errSCIMOperationRetryConflict = errors.New("scim operation retry state conflict")

type scimOperationRunner struct {
	app   core.App
	clock leafen.DurableJobClock
	wake  chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newSCIMOperationRunner(app core.App) *scimOperationRunner {
	return newSCIMOperationRunnerWithClock(app, leafen.RealDurableJobClock{})
}

func newSCIMOperationRunnerWithClock(
	app core.App,
	clock leafen.DurableJobClock,
) *scimOperationRunner {
	return &scimOperationRunner{app: app, clock: clock, wake: make(chan struct{}, 1)}
}

func (runner *scimOperationRunner) Start() {
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

func (runner *scimOperationRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%s: %w", leafen.ErrorCodeSCIMUnavailable, ctx.Err())
	}
}

func (runner *scimOperationRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *scimOperationRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait, err := runner.runTurn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			leafen.LogDurableJobDiagnostic(
				runner.app, leafen.ErrorCodeSCIMRequestFailed, leafen.NewDiagnosticReference(), "",
				"scim.group.continue", "runner-turn", err,
			)
		}
		if wait == 0 {
			continue
		}

		var clock leafen.DurableJobTimer
		var timer <-chan time.Time
		if wait > 0 {
			clock = leafen.DurableJobClockNewTimer(runner.clock, wait)
			timer = leafen.DurableJobTimerChannelValue(clock)
		}
		select {
		case <-ctx.Done():
			if clock != nil {
				leafen.DurableJobTimerStop(clock)
			}
			return
		case <-runner.wake:
		case <-timer:
		}
		if clock != nil {
			leafen.DurableJobTimerStop(clock)
		}
	}
}

func (runner *scimOperationRunner) runTurn(ctx context.Context) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	now := leafen.DurableJobClockNow(runner.clock).UTC().UnixMilli()
	ready := []struct {
		ID string `db:"id"`
	}{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_scim_operations}}
		WHERE [[status]] = 'pending' AND [[next_attempt_at]] <= {:now}
		ORDER BY [[next_attempt_at]], [[updated_at]], [[created_at]], [[id]]
		LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": now, "limit": 1}).All(&ready); err != nil {
		return scimOperationRetryDelay, err
	}
	if len(ready) == 0 {
		return runner.nextWait(ctx)
	}
	if _, err := continueEnterpriseSCIMOperationContext(ctx, runner.app, ready[0].ID); err != nil {
		if ctx.Err() != nil {
			return -1, err
		}
		if scheduleErr := runner.scheduleSCIMOperationRetry(ctx, ready[0].ID, now); scheduleErr != nil {
			return scimOperationRetryDelay, errors.Join(err, scheduleErr)
		}
		wait, waitErr := runner.nextWait(ctx)
		return scimOperationPostTurnWait(wait), errors.Join(err, waitErr)
	}
	wait, err := runner.nextWait(ctx)
	return scimOperationPostTurnWait(wait), err
}

func (runner *scimOperationRunner) scheduleSCIMOperationRetry(
	ctx context.Context,
	operationID string,
	now int64,
) error {
	result, err := runner.app.DB().NewQuery(`
		UPDATE {{leafen_scim_operations}}
		SET [[next_attempt_at]] = {:nextAttemptAt}, [[updated_at]] = {:now}
		WHERE [[id]] = {:id} AND [[status]] = 'pending'
	`).WithContext(ctx).Bind(dbx.Params{
		"id": operationID, "now": now,
		"nextAttemptAt": now + scimOperationRetryDelay.Milliseconds(),
	}).Execute()
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errSCIMOperationRetryConflict
	}
	return nil
}

func scimOperationPostTurnWait(wait time.Duration) time.Duration {
	if wait == 0 {
		return scimOperationTurnDelay
	}
	return wait
}

func (runner *scimOperationRunner) nextWait(ctx context.Context) (time.Duration, error) {
	var next sql.NullInt64
	err := runner.app.DB().NewQuery(`
		SELECT [[next_attempt_at]] FROM {{leafen_scim_operations}}
		WHERE [[status]] = 'pending'
		ORDER BY [[next_attempt_at]], [[updated_at]], [[created_at]], [[id]]
		LIMIT 1
	`).WithContext(ctx).Row(&next)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, nil
	}
	if err != nil {
		return scimOperationRetryDelay, err
	}
	if !next.Valid {
		return -1, nil
	}
	wait := time.Duration(next.Int64-leafen.DurableJobClockNow(runner.clock).UTC().UnixMilli()) * time.Millisecond
	if wait <= 0 {
		return 0, nil
	}
	return wait, nil
}
