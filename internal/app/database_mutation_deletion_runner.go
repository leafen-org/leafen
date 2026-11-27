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
	databaseMutationDeletionRunnerStoreKey = "leafenDatabaseMutationDeletionRunner"
	databaseMutationTurnDelay              = time.Millisecond
	databaseMutationFailureDelay           = 100 * time.Millisecond
	databaseMutationMaximumFailureDelay    = 30 * time.Second
)

type databaseMutationDeletionRunner struct {
	app         core.App
	persistence *pocketBaseYgoPersistence
	wake        chan struct{}

	mu     sync.Mutex
	runMu  sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newDatabaseMutationDeletionRunner(
	app core.App, persistence *pocketBaseYgoPersistence,
) *databaseMutationDeletionRunner {
	return &databaseMutationDeletionRunner{
		app: app, persistence: persistence, wake: make(chan struct{}, 1),
	}
}

func (runner *databaseMutationDeletionRunner) Start() {
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

func (runner *databaseMutationDeletionRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%s: %w", errorCodeDatabaseMutationUnavailable, ctx.Err())
	}
}

func (runner *databaseMutationDeletionRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *databaseMutationDeletionRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait, err := runner.advanceBurst(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeDatabaseMutationUnavailable, "database_mutation.delete", err)
			wait = databaseMutationFailureDelay
		}
		var timer *time.Timer
		var timerC <-chan time.Time
		if wait >= 0 {
			timer = time.NewTimer(wait)
			timerC = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-runner.wake:
		case <-timerC:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func (runner *databaseMutationDeletionRunner) advanceBurst(ctx context.Context) (time.Duration, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	now := time.Now().UTC().UnixMilli()
	jobs := []struct {
		ID string `db:"id"`
	}{}
	if err := runner.app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_mutation_jobs}}
		INDEXED BY [[idx_leafen_database_mutation_jobs_ready]]
		WHERE [[status]]='pending' AND [[not_before]]<={:now}
		ORDER BY [[not_before]],[[created_at]],[[id]] LIMIT {:limit}`,
	).Bind(dbx.Params{"now": now, "limit": databaseMutationJobsPerBurst}).WithContext(ctx).All(&jobs); err != nil {
		return -1, err
	}
	for _, job := range jobs {
		if err := runner.persistence.advanceDatabaseMutationDeletion(ctx, job.ID); err != nil {
			if ctx.Err() != nil {
				return -1, ctx.Err()
			}
			if scheduleErr := runner.scheduleRetry(ctx, job.ID, now); scheduleErr != nil {
				return -1, fmt.Errorf("%v; retry schedule: %w", err, scheduleErr)
			}
			logDurableJobDiagnostic(
				runner.app, errorCodeDatabaseMutationUnavailable, newDiagnosticReference(), "",
				"database_mutation.delete", "job-turn", err,
			)
		}
	}
	var next sql.NullInt64
	err := runner.app.DB().NewQuery(`SELECT MIN([[not_before]]) FROM {{leafen_database_mutation_jobs}}
		WHERE [[status]]='pending'`,
	).WithContext(ctx).Row(&next)
	if err != nil || !next.Valid {
		return -1, err
	}
	delay := time.Duration(next.Int64-time.Now().UTC().UnixMilli()) * time.Millisecond
	if delay < databaseMutationTurnDelay {
		delay = databaseMutationTurnDelay
	}
	return delay, nil
}

func databaseMutationRetryDelay(attempt int) time.Duration {
	delay := databaseMutationFailureDelay
	for current := 1; current < attempt && delay < databaseMutationMaximumFailureDelay; current++ {
		delay *= 2
		if delay > databaseMutationMaximumFailureDelay {
			return databaseMutationMaximumFailureDelay
		}
	}
	return delay
}

func (runner *databaseMutationDeletionRunner) scheduleRetry(
	ctx context.Context, jobID string, now int64,
) error {
	job, err := loadDatabaseMutationDeletionByID(runner.app, jobID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != databaseMutationStatusPending) {
		return nil
	}
	if err != nil {
		return err
	}
	attempt := job.CleanupLane + 1
	result, err := runner.app.DB().NewQuery(`UPDATE {{leafen_database_mutation_jobs}}
		SET [[cleanup_lane]]={:attempt},[[not_before]]={:notBefore},[[updated_at]]={:now}
		WHERE [[id]]={:jobId} AND [[status]]='pending' AND [[revision]]={:revision}`,
	).Bind(dbx.Params{
		"attempt": attempt, "notBefore": now + databaseMutationRetryDelay(attempt).Milliseconds(),
		"now": now, "jobId": job.ID, "revision": job.Revision,
	}).WithContext(ctx).Execute()
	return requireDatabaseMutationJobUpdate(result, err)
}

func notifyDatabaseMutationDeletionAfterCommit(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(databaseMutationDeletionRunnerStoreKey).(*databaseMutationDeletionRunner); ok {
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
