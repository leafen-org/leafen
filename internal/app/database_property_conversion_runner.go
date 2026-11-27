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

// The runner converts pending conversions one turn at a time and removes
// recovery records whose retention has passed.
type databasePropertyConversionRunner struct {
	app  core.App
	ygo  *pocketBaseYgoPersistence
	wake chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newDatabasePropertyConversionRunner(app core.App, ygo *pocketBaseYgoPersistence) *databasePropertyConversionRunner {
	return &databasePropertyConversionRunner{app: app, ygo: ygo, wake: make(chan struct{}, 1)}
}

func (runner *databasePropertyConversionRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel, runner.done = cancel, make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *databasePropertyConversionRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	cancel, done := runner.cancel, runner.done
	runner.cancel, runner.done = nil, nil
	runner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("database property conversion shutdown: %w", ctx.Err())
	}
}

func (runner *databasePropertyConversionRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *databasePropertyConversionRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	failures := 0
	for {
		wait, err := runner.runTurn(ctx)
		if err == nil {
			failures = 0
		} else {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeJobRunnerFailed, "database-property-conversion", err)
			wait = min(databasePropertyConversionFailureDelay<<min(failures, 8), time.Minute)
			failures++
		}
		if wait == 0 {
			continue
		}
		var timer <-chan time.Time
		if wait > 0 {
			timer = time.After(wait)
		}
		select {
		case <-ctx.Done():
			return
		case <-runner.wake:
		case <-timer:
		}
	}
}

// runTurn advances the oldest pending conversion; it waits for a wake-up or
// the next recovery expiry when nothing is pending.
func (runner *databasePropertyConversionRunner) runTurn(ctx context.Context) (time.Duration, error) {
	now := time.Now().UTC().UnixMilli()
	if _, err := runner.app.DB().NewQuery(`DELETE FROM {{leafen_database_property_conversions}}
		WHERE [[status]]!='pending' AND [[expires_at]]<={:now}`).Bind(dbx.Params{"now": now}).Execute(); err != nil {
		return -1, err
	}
	var id string
	err := runner.app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_property_conversions}}
		WHERE [[status]]='pending' ORDER BY [[created_at]],[[id]] LIMIT 1`).Row(&id)
	if errors.Is(err, sql.ErrNoRows) {
		next := sql.NullInt64{}
		if err := runner.app.DB().NewQuery(`SELECT MIN([[expires_at]]) FROM {{leafen_database_property_conversions}}
			WHERE [[status]]!='pending'`).Row(&next); err != nil || !next.Valid {
			return -1, err
		}
		return max(time.Duration(next.Int64-now)*time.Millisecond, time.Second), nil
	}
	if err != nil {
		return -1, err
	}
	if err := runner.ygo.advanceDatabasePropertyConversion(ctx, id); err != nil {
		return -1, err
	}
	return 0, nil
}

func notifyDatabasePropertyConversionAfterCommit(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(databasePropertyConversionRunnerStoreKey).(*databasePropertyConversionRunner); ok {
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
