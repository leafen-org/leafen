package app

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	authSessionCleanupTurnLimit      = 128
	authSessionCleanupTurnDelay      = time.Millisecond
	authSessionCleanupFailureDelay   = 100 * time.Millisecond
	authSessionCleanupRunnerStoreKey = "leafenAuthSessionCleanupRunner"
)

type authSessionCleanupTurn struct {
	Deleted int64
	HasMore bool
	NextDue int64
}

type authSessionCleanupRunner struct {
	app  core.App
	wake chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newAuthSessionCleanupRunner(app core.App) *authSessionCleanupRunner {
	return &authSessionCleanupRunner{app: app, wake: make(chan struct{}, 1)}
}

func (runner *authSessionCleanupRunner) Start() {
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

func (runner *authSessionCleanupRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%s: %w", errorCodeAuthSessionUnavailable, ctx.Err())
	}
}

func (runner *authSessionCleanupRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *authSessionCleanupRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := time.Now().UTC()
		turn, err := runner.runTurn(ctx, now)
		wait := time.Duration(-1)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeAuthSessionUnavailable, "auth-session.cleanup.continue", err)
			wait = authSessionCleanupFailureDelay
		} else if turn.HasMore {
			wait = authSessionCleanupTurnDelay
		} else if turn.NextDue > 0 {
			wait = time.Duration(turn.NextDue-now.UnixMilli()) * time.Millisecond
			if wait < 0 {
				wait = 0
			}
		}

		var clock *time.Timer
		var timer <-chan time.Time
		if wait >= 0 {
			clock = time.NewTimer(wait)
			timer = clock.C
		}
		select {
		case <-ctx.Done():
			if clock != nil {
				clock.Stop()
			}
			return
		case <-runner.wake:
		case <-timer:
		}
		if clock != nil {
			clock.Stop()
		}
	}
}

func (runner *authSessionCleanupRunner) runTurn(
	ctx context.Context,
	now time.Time,
) (authSessionCleanupTurn, error) {
	turn := authSessionCleanupTurn{}
	if err := ctx.Err(); err != nil {
		return turn, err
	}
	nowMillis := now.UTC().UnixMilli()
	result, err := runner.app.DB().NewQuery(`
		DELETE FROM {{leafen_auth_sessions}} WHERE [[id]] IN (
			SELECT [[id]] FROM {{leafen_auth_sessions}}
			WHERE ` + passkeySessionCleanupTimeSQL + ` <= {:now}
			ORDER BY ` + passkeySessionCleanupTimeSQL + `, [[id]] LIMIT {:limit}
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"now": nowMillis, "limit": authSessionCleanupTurnLimit,
	}).Execute()
	if err != nil {
		return turn, err
	}
	turn.Deleted, err = result.RowsAffected()
	if err != nil {
		return authSessionCleanupTurn{}, err
	}

	var hasMore int
	if err := runner.app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_auth_sessions}}
			WHERE ` + passkeySessionCleanupTimeSQL + ` <= {:now} LIMIT 1
		)
	`).WithContext(ctx).Bind(dbx.Params{"now": nowMillis}).Row(&hasMore); err != nil {
		return authSessionCleanupTurn{}, err
	}
	turn.HasMore = hasMore == 1
	if turn.HasMore {
		return turn, nil
	}

	var nextDue sql.NullInt64
	if err := runner.app.DB().NewQuery(`
		SELECT MIN(` + passkeySessionCleanupTimeSQL + `) FROM {{leafen_auth_sessions}}
	`).WithContext(ctx).Row(&nextDue); err != nil {
		return authSessionCleanupTurn{}, err
	}
	if nextDue.Valid {
		turn.NextDue = nextDue.Int64
	}
	return turn, nil
}

func notifyAuthSessionCleanupAfterCommit(app core.App) {
	notify := func() {
		runner, ok := app.Store().Get(authSessionCleanupRunnerStoreKey).(*authSessionCleanupRunner)
		if ok {
			runner.notify()
		}
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				notify()
			}
			return nil
		})
		return
	}
	notify()
}
