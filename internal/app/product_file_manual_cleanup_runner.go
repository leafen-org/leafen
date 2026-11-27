package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const productFileManualCleanupRunnerStoreKey = "leafenProductFileManualCleanupRunner"

type productFileManualCleanupRunner struct {
	app         core.App
	clock       durableJobClock
	maintenance *productFileMaintenanceRunner
	lane        productFileWorkspaceMaintenanceLane
	wake        chan struct{}

	mu     sync.Mutex
	runMu  sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newProductFileManualCleanupRunner(
	app core.App,
	maintenance *productFileMaintenanceRunner,
) *productFileManualCleanupRunner {
	return &productFileManualCleanupRunner{
		app: app, clock: realDurableJobClock{}, maintenance: maintenance,
		wake: make(chan struct{}, 1),
	}
}

func newProductFileManualCleanupRunnerWithClock(
	app core.App,
	clock durableJobClock,
	lane productFileWorkspaceMaintenanceLane,
) *productFileManualCleanupRunner {
	return &productFileManualCleanupRunner{
		app: app, clock: clock, lane: lane, wake: make(chan struct{}, 1),
	}
}

func (runner *productFileManualCleanupRunner) Start() {
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

func (runner *productFileManualCleanupRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%s: %w", errorCodeFileManualCleanupUnavailable, ctx.Err())
	}
}

func (runner *productFileManualCleanupRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *productFileManualCleanupRunner) run(
	ctx context.Context,
	done chan struct{},
) {
	defer close(done)
	for {
		wait, err := runner.advanceBurst(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeFileManualCleanupUnavailable, "file_manual_cleanup.run", err)
			wait = time.Duration(fileMaintenanceRunnerRetrySeconds) * time.Second
		}
		var timer durableJobTimer
		var timerC <-chan time.Time
		if wait >= 0 {
			timer = runner.clock.newTimer(wait)
			timerC = timer.channelValue()
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.stop()
			}
			return
		case <-runner.wake:
		case <-timerC:
		}
		if timer != nil {
			timer.stop()
		}
	}
}

func (runner *productFileManualCleanupRunner) advanceBurst(
	ctx context.Context,
) (time.Duration, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	now := runner.clock.now().UTC().UnixMilli()
	jobs := []struct {
		ID string `db:"id"`
	}{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_file_manual_cleanup_jobs}}
		INDEXED BY [[idx_leafen_file_manual_cleanup_jobs_ready]]
		WHERE [[status]]='running' AND [[not_before]]<={:now}
		ORDER BY [[not_before]],[[created_at]],[[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"now": now, "limit": fileManualCleanupJobsPerTurn,
	}).All(&jobs); err != nil {
		return -1, err
	}
	errorsByJob := make(chan error, len(jobs))
	var group sync.WaitGroup
	for _, item := range jobs {
		jobID := item.ID
		group.Add(1)
		go func() {
			defer group.Done()
			if err := runner.advanceJob(ctx, jobID); err != nil {
				errorsByJob <- err
			}
		}()
	}
	group.Wait()
	close(errorsByJob)
	var combined error
	for err := range errorsByJob {
		combined = errors.Join(combined, err)
	}
	if combined != nil {
		return -1, combined
	}
	var next sql.NullInt64
	if err := runner.app.DB().NewQuery(`SELECT MIN([[not_before]])
		FROM {{leafen_file_manual_cleanup_jobs}} WHERE [[status]]='running'`,
	).WithContext(ctx).Row(&next); err != nil || !next.Valid {
		return -1, err
	}
	wait := time.Duration(next.Int64-runner.clock.now().UTC().UnixMilli()) * time.Millisecond
	if wait < time.Millisecond {
		wait = time.Millisecond
	}
	return wait, nil
}

func (runner *productFileManualCleanupRunner) advanceJob(
	ctx context.Context,
	jobID string,
) error {
	job, err := loadProductFileManualCleanupJob(ctx, runner.app, jobID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != fileManualCleanupStatusRunning) {
		return nil
	}
	if err != nil || !validProductFileManualCleanupJob(job) {
		return errors.Join(errFileManualCleanupUnavailable, err)
	}
	scheduledAt := time.Unix(
		0, job.ScheduledAt*int64(time.Millisecond)+int64(job.Revision+1),
	).UTC()
	var result productFileMaintenanceSliceResult
	if runner.lane != nil {
		result, err = runner.lane(ctx, scheduledAt, job.WorkspaceID, job.ID)
	} else if runner.maintenance != nil {
		result, err = runner.maintenance.runWorkspaceUploadSlice(
			ctx, scheduledAt, job.WorkspaceID, job.ID,
		)
	} else {
		err = errFileManualCleanupUnavailable
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, settleErr := settleProductFileMaintenanceOutcomes(ctx, runner.app); settleErr != nil {
		err = errors.Join(err, settleErr)
	}
	if err != nil {
		return runner.scheduleRetry(ctx, job.ID, err)
	}
	if result.HasMore {
		return nil
	}
	return finalizeProductFileManualCleanup(ctx, runner.app, job.ID, runner.clock.now().UTC().UnixMilli())
}

func (runner *productFileManualCleanupRunner) scheduleRetry(
	ctx context.Context,
	jobID string,
	cause error,
) error {
	job, err := loadProductFileManualCleanupJob(ctx, runner.app, jobID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != fileManualCleanupStatusRunning) {
		return nil
	}
	if err != nil {
		return err
	}
	attempt := job.Attempt + 1
	now := runner.clock.now().UTC().UnixMilli()
	reference := strings.TrimSpace(job.DiagnosticReference)
	if reference == "" {
		reference = newDiagnosticReference()
	}
	status := fileManualCleanupStatusRunning
	completedAt := int64(0)
	notBefore := now + int64(fileMaintenanceRunnerRetrySeconds)*int64(time.Second/time.Millisecond)
	if attempt >= fileManualCleanupMaxAttempts {
		status = fileManualCleanupStatusFailed
		completedAt = now
		notBefore = now
	}
	updated, updateErr := runner.app.DB().NewQuery(`
		UPDATE {{leafen_file_manual_cleanup_jobs}} SET [[status]]={:status},
			[[revision]]=[[revision]]+1,[[attempt]]={:attempt},[[not_before]]={:notBefore},
			[[diagnostic_reference]]={:reference},[[completed_at]]={:completedAt},
			[[updated_at]]={:now}
		WHERE [[id]]={:jobId} AND [[status]]='running' AND [[revision]]={:revision}
	`).WithContext(ctx).Bind(dbx.Params{
		"status": status, "attempt": attempt, "notBefore": notBefore,
		"reference": reference, "completedAt": completedAt, "now": now,
		"jobId": job.ID, "revision": job.Revision,
	}).Execute()
	if updateErr != nil {
		return updateErr
	}
	count, updateErr := updated.RowsAffected()
	if updateErr != nil || count != 1 {
		return errors.Join(errFileManualCleanupUnavailable, updateErr)
	}
	logDurableJobDiagnostic(
		runner.app, errorCodeFileManualCleanupUnavailable, reference, job.ID,
		"file_manual_cleanup.run", "job-turn", cause,
	)
	return nil
}

func notifyProductFileManualCleanupAfterCommit(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(productFileManualCleanupRunnerStoreKey).(*productFileManualCleanupRunner); ok {
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
