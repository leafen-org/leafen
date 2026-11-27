package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A comment's notifications are written a slice of its recipients at a
// commit. Opening and closing a commit cost more than the slice's work (a
// comment to 300 followers, 20 a commit: 6,000 notifications took 12.6 s,
// 2026-10-15), so a slice is hundreds; delivery, which judges each
// recipient's access, also stops when it has held the write connection
// commentNotificationDeliveryHold, so people's edits wait little behind it
// (write_lane.go).
const (
	commentNotificationMaterializationEventLimit     = 4
	commentNotificationMaterializationRecipientLimit = 200
	commentNotificationDeliveryRecipientLimit        = 200
	commentNotificationDeliveryHold                  = 25 * time.Millisecond
	commentNotificationMaterializationTurnDelay      = time.Millisecond
	commentNotificationMaterializationFailureDelay   = time.Second
	commentNotificationMaterializationMaxRetryDelay  = 15 * time.Minute
	commentNotificationMaterializationRunnerStoreKey = "leafenCommentNotificationMaterializationRunner"
)

type commentNotificationMaterializationReadyEvent struct {
	ID        string `db:"id"`
	Status    string `db:"status"`
	Attempt   int    `db:"materialization_attempt"`
	ReadyAt   int64  `db:"ready_at"`
	CreatedAt string `db:"created_at"`
}

type commentNotificationMaterializationTurn struct {
	Events          int
	Candidates      int
	Deliveries      int
	RetentionPruned int
	RetentionFailed bool
	HasMore         bool
	NextDue         int64
}

type commentNotificationMaterializationRunner struct {
	app  core.App
	wake chan struct{}

	runMu  sync.Mutex
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newCommentNotificationMaterializationRunner(app core.App) *commentNotificationMaterializationRunner {
	return &commentNotificationMaterializationRunner{app: app, wake: make(chan struct{}, 1)}
}

func (runner *commentNotificationMaterializationRunner) Start() {
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

func (runner *commentNotificationMaterializationRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%s: %w", errorCodeCommentNotificationMaterializationFailed, ctx.Err())
	}
}

func (runner *commentNotificationMaterializationRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *commentNotificationMaterializationRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := time.Now().UTC()
		turn, err := runner.runTurn(ctx, now)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeCommentNotificationMaterializationFailed, "notification.materialization.continue", err)
		}
		wait := commentNotificationMaterializationTurnWait(turn, now)
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

func commentNotificationMaterializationTurnWait(
	turn commentNotificationMaterializationTurn,
	now time.Time,
) time.Duration {
	if turn.RetentionFailed {
		return notificationFanoutRetentionFailureDelay
	}
	if turn.HasMore || (turn.NextDue >= 0 && turn.NextDue <= now.UnixMilli()) {
		return commentNotificationMaterializationTurnDelay
	}
	if turn.NextDue > now.UnixMilli() {
		return time.Duration(turn.NextDue-now.UnixMilli()) * time.Millisecond
	}
	return -1
}

func (runner *commentNotificationMaterializationRunner) runTurn(
	ctx context.Context,
	now time.Time,
) (commentNotificationMaterializationTurn, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	turn := commentNotificationMaterializationTurn{NextDue: -1}
	if err := ctx.Err(); err != nil {
		return turn, err
	}
	ready, err := runner.readyEvents(ctx, now)
	if err != nil {
		return turn, err
	}
	turn.HasMore = len(ready) > commentNotificationMaterializationEventLimit
	if turn.HasMore {
		ready = ready[:commentNotificationMaterializationEventLimit]
	}
	turn.Events = len(ready)
	turnErrors := []error{}
	for _, event := range ready {
		if err := ctx.Err(); err != nil {
			return turn, errors.Join(append(turnErrors, err)...)
		}
		if event.Status != "pending" {
			deliveries, deliveryErr := deliverCommentNotificationEventSlice(
				ctx, runner.app, event.ID, now,
			)
			turn.Deliveries += deliveries
			if deliveryErr != nil {
				turnErrors = append(turnErrors, deliveryErr)
				logCommentNotificationDeliveryFailure(runner.app, event.ID, deliveryErr)
			}
			continue
		}
		candidates := 0
		// People's edits first (write_lane.go).
		yieldToPeople(ctx)
		err := runner.app.RunInTransaction(func(txApp core.App) error {
			var materializeErr error
			candidates, materializeErr = materializeCommentNotificationEventSlice(
				ctx, txApp, event.ID, now,
			)
			return materializeErr
		})
		if err == nil {
			turn.Candidates += candidates
			continue
		}
		if ctx.Err() != nil {
			return turn, errors.Join(append(turnErrors, ctx.Err())...)
		}
		turnErrors = append(turnErrors, err)
		if scheduleErr := runner.scheduleRetry(ctx, event, now); scheduleErr != nil {
			turnErrors = append(turnErrors, scheduleErr)
		}
	}
	if err := ctx.Err(); err != nil {
		return turn, errors.Join(append(turnErrors, err)...)
	}
	// Database row followers hear of row changes in the same turns.
	rowsMore, rowsDue, rowsErr := runDatabaseRowFollowerFanouts(ctx, runner.app, now)
	turn.HasMore = turn.HasMore || rowsMore
	if rowsErr != nil {
		turnErrors = append(turnErrors, rowsErr)
	}
	pruned, retentionErr := pruneCompletedNotificationFanoutEvents(ctx, runner.app, now)
	if retentionErr != nil {
		turn.RetentionFailed = true
		turnErrors = append(turnErrors, retentionErr)
		logNotificationFanoutRetentionFailure(runner.app, retentionErr)
	} else {
		turn.RetentionPruned = int(pruned)
		if pruned == notificationFanoutRetentionPruneBatch {
			turn.HasMore = true
		}
	}
	nextDue, err := runner.nextDue(ctx)
	if err != nil {
		turnErrors = append(turnErrors, err)
	} else {
		turn.NextDue = nextDue
	}
	if rowsDue >= 0 && (turn.NextDue < 0 || rowsDue < turn.NextDue) {
		turn.NextDue = rowsDue
	}
	return turn, errors.Join(turnErrors...)
}

func (runner *commentNotificationMaterializationRunner) readyEvents(
	ctx context.Context,
	now time.Time,
) ([]commentNotificationMaterializationReadyEvent, error) {
	ready := []commentNotificationMaterializationReadyEvent{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[id]], [[status]], [[materialization_attempt]],
			[[materialization_next_attempt_at]] AS [[ready_at]], [[created_at]]
		FROM {{leafen_notification_events}} INDEXED BY [[idx_leafen_notification_events_materialization]]
		WHERE [[status]] = 'pending' AND [[materialization_next_attempt_at]] <= {:now}
		ORDER BY [[materialization_next_attempt_at]], [[created_at]], [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"now": now.UnixMilli(), "limit": commentNotificationMaterializationEventLimit + 1,
	}).All(&ready); err != nil {
		return nil, err
	}
	delivery := []commentNotificationMaterializationReadyEvent{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[id]], [[status]], 0 AS [[materialization_attempt]],
			[[delivery_next_attempt_at]] AS [[ready_at]], [[created_at]]
		FROM {{leafen_notification_events}} INDEXED BY [[idx_leafen_notification_events_delivery]]
		WHERE [[status]] IN ('delivering','retrying')
			AND [[delivery_next_attempt_at]] <= {:now}
		ORDER BY [[delivery_next_attempt_at]], [[created_at]], [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"now": now.UnixMilli(), "limit": commentNotificationMaterializationEventLimit + 1,
	}).All(&delivery); err != nil {
		return nil, err
	}
	ready = append(ready, delivery...)
	sort.Slice(ready, func(left, right int) bool {
		if ready[left].ReadyAt != ready[right].ReadyAt {
			return ready[left].ReadyAt < ready[right].ReadyAt
		}
		if ready[left].CreatedAt != ready[right].CreatedAt {
			return ready[left].CreatedAt < ready[right].CreatedAt
		}
		return ready[left].ID < ready[right].ID
	})
	return ready, nil
}

func (runner *commentNotificationMaterializationRunner) scheduleRetry(
	ctx context.Context,
	event commentNotificationMaterializationReadyEvent,
	now time.Time,
) error {
	attempt := event.Attempt + 1
	delay := commentNotificationMaterializationFailureDelay
	for step := 1; step < attempt && delay < commentNotificationMaterializationMaxRetryDelay; step++ {
		delay *= 2
		if delay > commentNotificationMaterializationMaxRetryDelay {
			delay = commentNotificationMaterializationMaxRetryDelay
		}
	}
	result, err := runner.app.DB().NewQuery(`
		UPDATE {{leafen_notification_events}}
		SET [[materialization_attempt]] = {:nextAttempt},
			[[materialization_next_attempt_at]] = {:nextAttemptAt},
			[[materialization_error_code]] = {:errorCode}, [[updated_at]] = {:updatedAt}
		WHERE [[id]] = {:id} AND [[status]] = 'pending'
			AND [[materialization_attempt]] = {:currentAttempt}
	`).WithContext(ctx).Bind(dbx.Params{
		"id": event.ID, "currentAttempt": event.Attempt, "nextAttempt": attempt,
		"nextAttemptAt": now.Add(delay).UnixMilli(),
		"errorCode":     errorCodeCommentNotificationMaterializationFailed,
		"updatedAt":     now.UTC().Format(time.RFC3339Nano),
	}).Execute()
	if err != nil {
		return err
	}
	_, err = result.RowsAffected()
	return err
}

func (runner *commentNotificationMaterializationRunner) nextDue(ctx context.Context) (int64, error) {
	var next sql.NullInt64
	err := runner.app.DB().NewQuery(`
		SELECT MIN([[next_due]]) FROM (
			SELECT MIN([[materialization_next_attempt_at]]) AS [[next_due]]
			FROM {{leafen_notification_events}} WHERE [[status]] = 'pending'
			UNION ALL
			SELECT MIN([[delivery_next_attempt_at]]) AS [[next_due]]
			FROM {{leafen_notification_events}}
			INDEXED BY [[idx_leafen_notification_events_delivery]]
			WHERE [[status]] IN ('delivering','retrying')
			UNION ALL
			SELECT MIN([[completed_at]]) + {:retentionMillis} AS [[next_due]]
			FROM {{leafen_notification_events}}
			INDEXED BY [[idx_leafen_notification_events_completed_retention]]
			WHERE [[status]]='complete'
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"retentionMillis": int64(notificationFanoutRetention / time.Millisecond),
	}).Row(&next)
	if err != nil {
		return -1, err
	}
	if !next.Valid {
		return -1, nil
	}
	return next.Int64, nil
}

func notifyCommentNotificationMaterializationAfterCommit(app core.App) {
	notify := func() {
		runner, ok := app.Store().Get(commentNotificationMaterializationRunnerStoreKey).(*commentNotificationMaterializationRunner)
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
