package app

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const (
	recoverableBlockTrashRetentionRunnerStoreKey = "leafenRecoverableBlockTrashRetentionRunner"
	recoverableBlockTrashRetentionFailureDelay   = 100 * time.Millisecond
)

type recoverableBlockTrashRetentionRunner struct {
	app   core.App
	clock durableJobClock
	wake  chan struct{}
	// rooms sends collaborators of a workspace away before a page there is
	// deleted for good, as a person's permanent deletion does.
	rooms workspaceRoomInvalidator

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	runMu  sync.Mutex
}

func newRecoverableBlockTrashRetentionRunner(app core.App) *recoverableBlockTrashRetentionRunner {
	return &recoverableBlockTrashRetentionRunner{
		app: app, clock: realDurableJobClock{}, wake: make(chan struct{}, 1),
	}
}

func (runner *recoverableBlockTrashRetentionRunner) bindRoomInvalidator(rooms workspaceRoomInvalidator) {
	runner.rooms = rooms
}

func (runner *recoverableBlockTrashRetentionRunner) Start() {
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

func (runner *recoverableBlockTrashRetentionRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("recoverable block trash retention shutdown: %w", ctx.Err())
	}
}

func (runner *recoverableBlockTrashRetentionRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *recoverableBlockTrashRetentionRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait, err := runner.runTurn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeJobRunnerFailed, "block-trash.retention", err)
			wait = recoverableBlockTrashRetentionFailureDelay
		}
		if wait == 0 {
			continue
		}
		var timerOwner durableJobTimer
		var timer <-chan time.Time
		if wait > 0 {
			timerOwner = runner.clock.newTimer(wait)
			timer = timerOwner.channelValue()
		}
		select {
		case <-ctx.Done():
			if timerOwner != nil {
				timerOwner.stop()
			}
			return
		case <-runner.wake:
		case <-timer:
		}
		if timerOwner != nil {
			timerOwner.stop()
		}
	}
}

func (runner *recoverableBlockTrashRetentionRunner) runTurn(ctx context.Context) (time.Duration, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	// Sources kept in the trash where they stand whose time is over go
	// first, into the trash's forms this turn then ends
	// (database_source_trash_in_place_retention.go).
	inPlaceLeft, inPlaceDue, err := retireDatabaseSourcesTrashedInPlace(ctx, runner.app, runner.rooms, runner.clock.now())
	if err != nil {
		return -1, err
	}
	now := runner.clock.now().UTC().UnixMilli()
	deleted := int64(0)
	rowTrashDeleted, templateTrashDeleted := int64(0), int64(0)
	rowPageContentLeft := false
	nextDue := sql.NullInt64{}
	err = runner.app.RunInTransaction(func(txApp core.App) error {
		var err error
		deleted, err = purgeExpiredRecoverableBlockTrashSlice(txApp, now)
		if err != nil {
			return err
		}
		rowTrashDeleted, err = deleteExpiredDatabaseRowTrashSlice(txApp, now)
		if err != nil {
			return err
		}
		templateTrashDeleted, err = deleteExpiredDatabaseTemplateTrashSlice(txApp, now)
		if err != nil {
			return err
		}
		rowPageContentLeft, err = cleanRowPageContentSlice(txApp)
		if err != nil {
			return err
		}
		// What a legal hold keeps is looked at again within the longest wait.
		// Each trash's next end is its first entry in expiry order that may
		// go, read along its expiry index and stopped there, a hold judged
		// for the entries up to it only, not every entry of the trash after
		// each deletion (2026-11-18: the block trash; then the rows' and the
		// templates').
		return txApp.DB().NewQuery(`
			SELECT MIN([[expires_at]]) FROM (
				SELECT * FROM (
					SELECT [[tombstone]].[[expires_at]]
					FROM {{leafen_block_trash_tombstones}} AS [[tombstone]]
					WHERE NOT EXISTS (
						SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
						WHERE [[trash]].[[root_block_id]] = [[tombstone]].[[root_block_id]]
							AND [[trash]].[[kind]] = 'source'
					) AND NOT ` + legalHoldRetainsPageSQL("[[tombstone]].[[page_id]]", "[[tombstone]].[[workspace_id]]") + `
					ORDER BY [[tombstone]].[[expires_at]] LIMIT 1
				)
				UNION ALL
				SELECT * FROM (
					SELECT [[row_trash]].[[expires_at]] FROM {{leafen_database_row_trash}} AS [[row_trash]]
					INDEXED BY [[idx_leafen_database_row_trash_expiry]]
					WHERE [[row_trash]].[[deleting]] = 0
						AND NOT ` + databaseRowTrashHeldSQL("[[row_trash]].[[root_row_id]]") + `
					ORDER BY [[row_trash]].[[expires_at]] LIMIT 1
				)
				UNION ALL
				` + databaseTemplateTrashDueSQL + `
			)
		`).WithContext(ctx).Row(&nextDue)
	})
	if err != nil {
		return -1, err
	}
	// Pages 30 days in the trash go, one deletion at a time.
	pagesLeft, pageDue, err := retirePageTrash(runner.app, runner.rooms, runner.clock.now())
	if err != nil {
		return -1, err
	}
	for _, due := range []sql.NullInt64{pageDue, inPlaceDue} {
		if due.Valid && (!nextDue.Valid || due.Int64 < nextDue.Int64) {
			nextDue = due
		}
	}
	if deleted == recoverableBlockTrashPurgeSize || rowTrashDeleted == databaseRowTrashPurgeSize ||
		templateTrashDeleted == databaseRowTrashPurgeSize || rowPageContentLeft || pagesLeft || inPlaceLeft {
		return 0, nil
	}
	// A database page queued for cleanup by a row deletion that did not wake
	// the runner is cleared within this wait.
	if !nextDue.Valid {
		return rowPageContentCleanupWait, nil
	}
	wait := time.Duration(nextDue.Int64-now) * time.Millisecond
	if wait <= 0 {
		return 0, nil
	}
	return min(wait, rowPageContentCleanupWait), nil
}

const rowPageContentCleanupWait = 15 * time.Minute

func notifyRecoverableBlockTrashRetentionAfterCommit(app core.App) {
	notify := func() {
		runner, ok := app.Store().Get(recoverableBlockTrashRetentionRunnerStoreKey).(*recoverableBlockTrashRetentionRunner)
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
