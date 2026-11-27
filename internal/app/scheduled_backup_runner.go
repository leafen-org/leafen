package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	scheduledBackupRunnerStoreKey = "leafenScheduledBackupRunner"
	// How long the runner sleeps with nothing due, and how soon a run that
	// failed, or found another backup or restore going, tries again.
	scheduledBackupIdleDelay  = time.Hour
	scheduledBackupRetryDelay = time.Hour
	scheduledBackupBusyDelay  = time.Minute
)

type scheduledBackupRunner struct {
	app    core.App
	wake   chan struct{}
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newScheduledBackupRunner(app core.App) *scheduledBackupRunner {
	return &scheduledBackupRunner{app: app, wake: make(chan struct{}, 1)}
}

func (runner *scheduledBackupRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel, runner.done = cancel, make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *scheduledBackupRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("scheduled backup shutdown: %w", ctx.Err())
	}
}

func (runner *scheduledBackupRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func notifyScheduledBackupRunner(app core.App) {
	if runner, ok := app.Store().Get(scheduledBackupRunnerStoreKey).(*scheduledBackupRunner); ok {
		runner.notify()
	}
}

func (runner *scheduledBackupRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait, err := runner.runTurn(ctx, time.Now())
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeJobRunnerFailed, "scheduled-backup", err)
			wait = scheduledBackupRetryDelay
		}
		select {
		case <-ctx.Done():
			return
		case <-runner.wake:
		case <-time.After(wait):
		}
	}
}

// runTurn backs up when backups are on and one is due or asked for, and
// answers how long to wait before looking again. Only this runner marks a
// run going, one turn at a time, so a mark it finds is a run cut off with
// the server, or one a restored snapshot was taken during.
func (runner *scheduledBackupRunner) runTurn(ctx context.Context, now time.Time) (time.Duration, error) {
	schedule, err := loadScheduledBackup(runner.app)
	if err != nil {
		return 0, err
	}
	if !schedule.Enabled || !schedule.targetSet() {
		return scheduledBackupIdleDelay, nil
	}
	if due := time.UnixMilli(schedule.NextRunAt); !schedule.RunRequested && due.After(now) {
		return min(due.Sub(now), scheduledBackupIdleDelay), nil
	}
	secret, err := sealingBackupSecretOf(runner.app, schedule)
	if errors.Is(err, errBackupProtectionRequired) {
		// Shown on the screen as the last run's failure until one exists.
		return scheduledBackupIdleDelay, runner.refuseUnsealed(now)
	}
	if err != nil {
		return 0, err
	}
	// One backup or restore at a time, with PocketBase's own as well.
	if runner.app.Store().Has(core.StoreKeyActiveBackup) {
		return scheduledBackupBusyDelay, nil
	}
	runner.app.Store().Set(core.StoreKeyActiveBackup, "leafen-scheduled-backup")
	defer runner.app.Store().Remove(core.StoreKeyActiveBackup)
	// Its progress shows from here until it ends (backup_progress.go),
	// from before the request for it is cleared: a backup asked for while
	// it runs finds it and starts nothing more.
	progress := startBackupProgress(runner.app, backupProgressStoreKey, backupProgressKindBackup, "")
	ended := false
	finish := func() administratorBackupProgress {
		ended = true
		return endBackupProgress(runner.app, backupProgressStoreKey, progress)
	}
	defer func() {
		if !ended {
			finish()
		}
	}()
	if _, err := runner.app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[running_since]] = {:now},
		[[run_requested]] = 0 WHERE [[id]] = 'default'`).Bind(dbx.Params{"now": now.UnixMilli()}).Execute(); err != nil {
		return 0, err
	}
	outcome := scheduledBackupOutcome{}
	store, runErr := openScheduledBackupStore(ctx, runner.app, schedule)
	if runErr == nil {
		outcome, runErr = writeScheduledBackupSnapshotWith(withBackupProgress(ctx, progress), runner.app, store, schedule.Keep, secret, now)
		closeScheduledBackupStore(store)
	}
	if ctx.Err() != nil {
		// Stopped with the server: the next start tries again.
		_, _ = runner.app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[running_since]] = 0,
			[[run_requested]] = 1 WHERE [[id]] = 'default'`).Execute()
		return 0, ctx.Err()
	}
	finished := time.Now()
	// The run as it ended stays: where a failed one stopped, or all a
	// finished one went through and sent.
	run := finish()
	if runErr != nil {
		// Why, in the place's own words, beside its code (backup_failure_reason.go).
		code := scheduledBackupErrorCode(runErr)
		run.ErrorCode, run.ErrorReason = code, backupFailureReason(runErr, scheduledBackupSecrets(runner.app, schedule)...)
		run.ErrorCause = backupFailureCause(runErr)
		logBackupFailure(runner.app, code, "scheduled-backup", "snapshot", runErr, run.ErrorReason)
		next := finished.Add(min(scheduledBackupRetryDelay, time.Duration(schedule.IntervalHours)*time.Hour))
		_, err := runner.app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[running_since]] = 0,
			[[last_finished_at]] = {:finished}, [[last_error_code]] = {:code}, [[next_run_at]] = {:next},
			[[last_run_json]] = {:run}
			WHERE [[id]] = 'default'`).Bind(dbx.Params{
			"finished": finished.UnixMilli(), "code": code, "next": next.UnixMilli(), "run": scheduledBackupLastRunJSON(run),
		}).Execute()
		if err == nil {
			// Failing again and again, it tells the administrators.
			if alertErr := recordScheduledBackupRunFailure(runner.app, run, finished); alertErr != nil {
				logDurableJobDiagnostic(runner.app, code, newDiagnosticReference(), "", "scheduled-backup", "alert", alertErr)
			}
		}
		return min(next.Sub(finished), scheduledBackupIdleDelay), err
	}
	next := now.Add(time.Duration(schedule.IntervalHours) * time.Hour)
	// What it sent counts the packed database too.
	run.Step, run.BytesSent = backupProgressStepDone, outcome.AddedBytes
	_, err = runner.app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[running_since]] = 0,
		[[last_finished_at]] = {:finished}, [[last_success_at]] = {:finished}, [[last_error_code]] = '',
		[[last_snapshot]] = {:snapshot}, [[last_snapshot_sha256]] = {:sha256}, [[last_added_bytes]] = {:added},
		[[snapshot_count]] = {:count}, [[next_run_at]] = {:next}, [[last_run_json]] = {:run}
		WHERE [[id]] = 'default'`).Bind(dbx.Params{
		"finished": finished.UnixMilli(), "snapshot": outcome.Snapshot, "sha256": outcome.ManifestSHA256,
		"added": outcome.AddedBytes, "count": outcome.Snapshots, "next": next.UnixMilli(), "run": scheduledBackupLastRunJSON(run),
	}).Execute()
	if err == nil {
		err = clearScheduledBackupFailures(runner.app)
	}
	if err == nil {
		// Beside the data, for the startup recovery screen while the
		// database won't open.
		if noteErr := writeLastBackupNote(runner.app.DataDir(), schedule, outcome, finished); noteErr != nil {
			logDurableJobDiagnostic(runner.app, errorCodeScheduledBackupRestoreFailed, newDiagnosticReference(), "",
				"scheduled-backup", "last-backup-note", noteErr)
		}
	}
	if err == nil {
		// A backup after a restore holds what the restore brought back, so
		// the data from before it is no longer kept.
		if releaseErr := releaseRestoreKeepAfterBackup(runner.app); releaseErr != nil {
			logDurableJobDiagnostic(runner.app, errorCodeScheduledBackupRestoreFailed, newDiagnosticReference(), "",
				"scheduled-backup", "restore-release", releaseErr)
		}
	}
	return min(next.Sub(finished), scheduledBackupIdleDelay), err
}

// refuseUnsealed keeps a backup due for a place off this machine that has
// nothing to seal it, showing why; it goes once a recovery code or a
// password exists (the protection routes wake the runner).
func (runner *scheduledBackupRunner) refuseUnsealed(now time.Time) error {
	_, err := runner.app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[last_finished_at]] = {:now},
		[[last_error_code]] = {:code} WHERE [[id]] = 'default' AND [[last_error_code]] != {:code}`).Bind(dbx.Params{
		"now": now.UnixMilli(), "code": errorCodeBackupProtectionRequired,
	}).Execute()
	return err
}
