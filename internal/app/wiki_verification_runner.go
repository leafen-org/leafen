package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const (
	wikiVerificationRunnerStoreKey = "leafenWikiVerificationRunner"
	wikiVerificationTurnDelay      = time.Millisecond
	wikiVerificationFailureDelay   = 100 * time.Millisecond
	// A verification a wiki page's value schedules is found within the idle
	// poll, whichever write set the value.
	wikiVerificationIdlePoll     = time.Minute
	errorCodeWikiVerificationRun = "LEAFEN_WIKI_VERIFICATION_RUN_FAILED"
)

type wikiVerificationTurn struct {
	Expired       int
	Notifications int
	Emails        int
	Retried       int
	Failed        int
	Cancelled     int
	HasMore       bool
	NextDue       int64
}

type wikiVerificationRunner struct {
	app      core.App
	provider wikiVerificationEmailProvider
	wake     chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	runMu  sync.Mutex
}

func newWikiVerificationRunner(
	app core.App,
	provider wikiVerificationEmailProvider,
) *wikiVerificationRunner {
	return &wikiVerificationRunner{
		app: app, provider: provider, wake: make(chan struct{}, 1),
	}
}

func (runner *wikiVerificationRunner) Start() {
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

func (runner *wikiVerificationRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%s: %w", errorCodeWikiVerificationRun, ctx.Err())
	}
}

func (runner *wikiVerificationRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *wikiVerificationRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := time.Now().UTC()
		turn, err := runner.runTurn(ctx, now)
		wait := time.Duration(-1)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeWikiVerificationRun, "wiki_verification.run", err)
			wait = wikiVerificationFailureDelay
		} else if turn.HasMore {
			wait = wikiVerificationTurnDelay
		} else if turn.NextDue > 0 {
			wait = time.Duration(turn.NextDue-now.UnixMilli()) * time.Millisecond
			if wait < 0 {
				wait = 0
			}
		}
		if wait < 0 || wait > wikiVerificationIdlePoll {
			wait = wikiVerificationIdlePoll
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

func (runner *wikiVerificationRunner) runTurn(
	ctx context.Context,
	now time.Time,
) (wikiVerificationTurn, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return wikiVerificationTurn{}, err
	}
	claims, queueMore, claimErr := claimDueWikiVerifications(ctx, runner.app, now)
	expiry, expiryErr := expireWikiVerificationClaims(ctx, runner.app, claims, now)
	deliveries, deliveryMore, deliveryClaimErr := claimWikiVerificationEmailDeliveries(ctx, runner.app, now)
	sending, results, holdErr := holdWikiVerificationEmailsToInactive(runner.app, deliveries)
	for id, result := range deliverWikiVerificationEmails(ctx, sending, runner.provider) {
		results[id] = result
	}
	delivery, deliveryErr := settleWikiVerificationEmailDeliveries(
		ctx, runner.app, deliveries, results, now,
	)
	expiry.Emails += delivery.Emails
	expiry.Retried += delivery.Retried
	expiry.Failed += delivery.Failed
	expiry.HasMore = queueMore || deliveryMore
	nextDue, nextDueErr := nextWikiVerificationDue(ctx, runner.app)
	expiry.NextDue = nextDue
	return expiry, errors.Join(claimErr, expiryErr, deliveryClaimErr, holdErr, deliveryErr, nextDueErr)
}

func nextWikiVerificationDue(ctx context.Context, app core.App) (int64, error) {
	var next sql.NullInt64
	err := app.DB().NewQuery(`
		SELECT MIN([[next_due]]) FROM (
			SELECT MIN([[next_attempt_at]]) AS [[next_due]]
			FROM {{leafen_wiki_verification_queue}}
			INDEXED BY [[idx_leafen_wiki_verification_ready]]
			WHERE [[status]]='ready'
			UNION ALL
			SELECT MIN([[lease_until]]) AS [[next_due]]
			FROM {{leafen_wiki_verification_queue}}
			INDEXED BY [[idx_leafen_wiki_verification_claimed]]
			WHERE [[status]]='claimed'
			UNION ALL
			SELECT MIN([[next_attempt_at]]) AS [[next_due]]
			FROM {{leafen_wiki_verification_email_deliveries}}
			INDEXED BY [[idx_leafen_wiki_verification_email_ready]]
			WHERE [[status]]='ready'
			UNION ALL
			SELECT MIN([[next_attempt_at]]) AS [[next_due]]
			FROM {{leafen_wiki_verification_email_deliveries}}
			INDEXED BY [[idx_leafen_wiki_verification_email_ready]]
			WHERE [[status]]='retryable'
			UNION ALL
			SELECT MIN([[lease_until]]) AS [[next_due]]
			FROM {{leafen_wiki_verification_email_deliveries}}
			INDEXED BY [[idx_leafen_wiki_verification_email_claimed]]
			WHERE [[status]]='claimed'
		)
	`).WithContext(ctx).Row(&next)
	if err != nil || !next.Valid {
		return 0, err
	}
	return next.Int64, nil
}

func notifyWikiVerificationAfterCommit(app core.App) {
	notify := func() {
		runner, ok := app.Store().Get(wikiVerificationRunnerStoreKey).(*wikiVerificationRunner)
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
