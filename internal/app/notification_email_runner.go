package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const (
	notificationEmailRunnerStoreKey  = "leafenNotificationEmailRunner"
	notificationEmailRecipientLimit  = 4
	notificationEmailTurnDelay       = time.Millisecond
	notificationEmailFailureDelay    = time.Minute
	notificationEmailMaintenanceWait = time.Hour
)

type notificationEmailTurn struct {
	HasMore bool
	NextDue int64
}

// notificationEmailRunner sends due notification emails, one batch per
// person, page invitations to addresses without an account and managed
// users' notices, and forgets settled ones after the retention period.
type notificationEmailRunner struct {
	app  core.App
	wake chan struct{}

	runMu  sync.Mutex
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newNotificationEmailRunner(app core.App) *notificationEmailRunner {
	return &notificationEmailRunner{app: app, wake: make(chan struct{}, 1)}
}

func (runner *notificationEmailRunner) Start() {
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

func (runner *notificationEmailRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%s: %w", errorCodeEmailDeliveryFailed, ctx.Err())
	}
}

func (runner *notificationEmailRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *notificationEmailRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := time.Now().UTC()
		turn, err := runner.runTurn(ctx, now)
		wait := notificationEmailMaintenanceWait
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// The database busy, the turn left its mail queued and comes back
			// soon (runner_turn_wait.go).
			wait = notificationEmailFailureDelay
			if logRunnerTurnWait(runner.app, "notification.email.send", err) {
				wait = runnerTurnBusyDelay
			} else {
				logNotificationEmailFailure(runner.app, "runner-turn", err)
			}
		} else if turn.HasMore {
			wait = notificationEmailTurnDelay
		} else if turn.NextDue >= 0 {
			wait = min(wait, max(time.Duration(turn.NextDue-now.UnixMilli())*time.Millisecond, notificationEmailTurnDelay))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-runner.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (runner *notificationEmailRunner) runTurn(ctx context.Context, now time.Time) (notificationEmailTurn, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	turn := notificationEmailTurn{NextDue: -1}
	if err := releaseExpiredNotificationEmailLeases(ctx, runner.app, now); err != nil {
		return turn, err
	}
	if err := pruneSettledNotificationEmails(ctx, runner.app, now); err != nil {
		return turn, err
	}
	recipients, err := dueNotificationEmailRecipients(ctx, runner.app, now, notificationEmailRecipientLimit+1)
	if err != nil {
		return turn, err
	}
	turn.HasMore = len(recipients) > notificationEmailRecipientLimit
	if turn.HasMore {
		recipients = recipients[:notificationEmailRecipientLimit]
	}
	turnErrors := []error{}
	for _, userID := range recipients {
		if err := ctx.Err(); err != nil {
			return turn, errors.Join(append(turnErrors, err)...)
		}
		lease, rows, err := claimNotificationEmails(ctx, runner.app, userID, now)
		if err != nil {
			turnErrors = append(turnErrors, err)
			continue
		}
		if err := sendNotificationEmailBatch(ctx, runner.app, lease, rows, now); err != nil {
			turnErrors = append(turnErrors, err)
		}
	}
	if err := sendDuePageInviteEmails(ctx, runner.app, now); err != nil {
		turnErrors = append(turnErrors, err)
	}
	if err := sendDueWorkspaceInvitationEmails(ctx, runner.app, now); err != nil {
		turnErrors = append(turnErrors, err)
	}
	if err := sendDueWorkspaceMemberAddedEmails(ctx, runner.app, now); err != nil {
		turnErrors = append(turnErrors, err)
	}
	if err := sendDueManagedUserNotices(ctx, runner.app, now); err != nil {
		turnErrors = append(turnErrors, err)
	}
	if turn.NextDue, err = nextNotificationEmailDue(ctx, runner.app); err != nil {
		turnErrors = append(turnErrors, err)
	}
	inviteDue, err := nextPageInviteEmailDue(ctx, runner.app)
	if err != nil {
		turnErrors = append(turnErrors, err)
	}
	if inviteDue >= 0 && (turn.NextDue < 0 || inviteDue < turn.NextDue) {
		turn.NextDue = inviteDue
	}
	workspaceInviteDue, err := nextWorkspaceInvitationEmailDue(ctx, runner.app)
	if err != nil {
		turnErrors = append(turnErrors, err)
	}
	if workspaceInviteDue >= 0 && (turn.NextDue < 0 || workspaceInviteDue < turn.NextDue) {
		turn.NextDue = workspaceInviteDue
	}
	addedDue, err := nextWorkspaceMemberAddedEmailDue(ctx, runner.app)
	if err != nil {
		turnErrors = append(turnErrors, err)
	}
	if addedDue >= 0 && (turn.NextDue < 0 || addedDue < turn.NextDue) {
		turn.NextDue = addedDue
	}
	noticeDue, err := nextManagedUserNoticeDue(ctx, runner.app)
	if err != nil {
		turnErrors = append(turnErrors, err)
	}
	if noticeDue >= 0 && (turn.NextDue < 0 || noticeDue < turn.NextDue) {
		turn.NextDue = noticeDue
	}
	return turn, errors.Join(turnErrors...)
}

func notifyNotificationEmailRunnerAfterCommit(app core.App) {
	notify := func() {
		runner, ok := app.Store().Get(notificationEmailRunnerStoreKey).(*notificationEmailRunner)
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
