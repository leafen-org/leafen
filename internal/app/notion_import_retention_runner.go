package app

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const (
	notionImportRetentionRunnerStoreKey = "leafenNotionImportRetentionRunner"
	notionImportRetentionFailureDelay   = 100 * time.Millisecond
)

type notionImportRetentionSliceResult struct {
	Status      string
	DeletedRows int
	DeletedJobs int
	HasMore     bool
	NextDue     int64
}

type notionImportRetentionRunner struct {
	app       core.App
	clock     durableJobClock
	retention time.Duration
	wake      chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	runMu  sync.Mutex
}

func newNotionImportRetentionRunner(app core.App) *notionImportRetentionRunner {
	return newNotionImportRetentionRunnerWithClock(
		app,
		realDurableJobClock{},
		notionImportRetentionDuration(os.Getenv(notionImportRetentionEnvironment)),
	)
}

func newNotionImportRetentionRunnerWithClock(
	app core.App,
	clock durableJobClock,
	retention time.Duration,
) *notionImportRetentionRunner {
	return &notionImportRetentionRunner{
		app: app, clock: clock, retention: retention, wake: make(chan struct{}, 1),
	}
}

func (runner *notionImportRetentionRunner) Start() {
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

func (runner *notionImportRetentionRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%s: %w", errorCodeNotionImportRetentionFailed, ctx.Err())
	}
}

func (runner *notionImportRetentionRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *notionImportRetentionRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait, err := runner.runTurn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeNotionImportRetentionFailed, "notion-import.retention", err)
			wait = notionImportRetentionFailureDelay
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

func (runner *notionImportRetentionRunner) runTurn(ctx context.Context) (time.Duration, error) {
	result, err := runner.runSlice(ctx, runner.clock.now().UTC())
	if err != nil {
		return -1, err
	}
	if result.HasMore {
		return 0, nil
	}
	if result.NextDue == 0 {
		return -1, nil
	}
	wait := time.Duration(result.NextDue-runner.clock.now().UTC().UnixMilli()) * time.Millisecond
	if wait <= 0 {
		return 0, nil
	}
	return wait, nil
}

func notifyNotionImportRetentionAfterCommit(app core.App) {
	notify := func() {
		runner, ok := app.Store().Get(notionImportRetentionRunnerStoreKey).(*notionImportRetentionRunner)
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
