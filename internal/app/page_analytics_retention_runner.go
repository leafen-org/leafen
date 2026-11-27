package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const (
	pageAnalyticsRetentionRunnerStoreKey = "leafenPageAnalyticsRetentionRunner"
	pageAnalyticsRetentionTurnDelay      = time.Millisecond
	pageAnalyticsRetentionFailureDelay   = 100 * time.Millisecond
)

type pageAnalyticsRetentionRunner struct {
	app  core.App
	wake chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	runMu  sync.Mutex
}

func newPageAnalyticsRetentionRunner(app core.App) *pageAnalyticsRetentionRunner {
	return &pageAnalyticsRetentionRunner{app: app, wake: make(chan struct{}, 1)}
}

func (runner *pageAnalyticsRetentionRunner) Start() {
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

func (runner *pageAnalyticsRetentionRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%s: %w", errorCodePageAnalyticsFailed, ctx.Err())
	}
}

func (runner *pageAnalyticsRetentionRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *pageAnalyticsRetentionRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := time.Now().UTC()
		turn, err := runner.runTurn(ctx, now)
		wait := time.Duration(-1)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			code := errorCodePageAnalyticsFailed
			causeCode := errorCodePageAnalyticsFailed
			operation := "page_analytics.retention"
			if selection, ok := persistentWriteStorageErrorSelection(
				err, persistentPageAnalyticsRetention,
			); ok {
				code = selection.Code
				causeCode = selection.CauseCode
				operation = selection.Operation
			}
			logDurableJobDiagnosticWithCause(
				runner.app, code, newDiagnosticReference(), "",
				operation, "runner-turn", causeCode, err,
			)
			wait = pageAnalyticsRetentionFailureDelay
		} else if turn.HasMore {
			wait = pageAnalyticsRetentionTurnDelay
		} else if turn.NextDue > 0 {
			wait = time.Duration(turn.NextDue-now.UnixMilli()) * time.Millisecond
			if wait < 0 {
				wait = 0
			}
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

func notifyPageAnalyticsRetentionAfterCommit(app core.App) {
	notify := func() {
		runner, ok := app.Store().Get(pageAnalyticsRetentionRunnerStoreKey).(*pageAnalyticsRetentionRunner)
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
