package app

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

const (
	bootstrapVisibilityProjectionRunnerStoreKey = "leafenBootstrapVisibilityProjectionRunner"
	bootstrapVisibilityProjectionWakeHandlerID  = "leafenBootstrapVisibilityProjectionWake"
	bootstrapVisibilityProjectionRunnerBurst    = bootstrapVisibilityProjectionRequestTurnLimit
	bootstrapVisibilityProjectionTurnDelay      = time.Millisecond
	bootstrapVisibilityProjectionFailureDelay   = time.Second
	bootstrapVisibilityProjectionIdlePoll       = 30 * time.Second
	errorCodeBootstrapVisibilityProjection      = "LEAFEN_BOOTSTRAP_VISIBILITY_PROJECTION_FAILED"
)

type bootstrapVisibilityProjectionRunner struct {
	app  core.App
	wake chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newBootstrapVisibilityProjectionRunner(app core.App) *bootstrapVisibilityProjectionRunner {
	return &bootstrapVisibilityProjectionRunner{app: app, wake: make(chan struct{}, 1)}
}

func (runner *bootstrapVisibilityProjectionRunner) Start() {
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

func (runner *bootstrapVisibilityProjectionRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%s: %w", errorCodeBootstrapVisibilityProjection, ctx.Err())
	}
}

func (runner *bootstrapVisibilityProjectionRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

// notifyBootstrapVisibilityProjectionAfterCommit wakes the runner for the
// jobs a background change queued — a workspace deleted, a deleted account's
// memberships — as a request's change wakes it (registerBootstrapVisibilityProjectionWake):
// otherwise their rows wait for its idle look, up to half a minute (2026-11-17:
// a self-deletion's workspace row was still listed when the deletion ended).
func notifyBootstrapVisibilityProjectionAfterCommit(app core.App) {
	notify := func() {
		if runner, ok := app.Store().Get(bootstrapVisibilityProjectionRunnerStoreKey).(*bootstrapVisibilityProjectionRunner); ok {
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

func (runner *bootstrapVisibilityProjectionRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		hasMore, err := runner.advanceBurst(ctx)
		wait := bootstrapVisibilityProjectionIdlePoll
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeBootstrapVisibilityProjection, "bootstrap.visibility.project", err)
			wait = bootstrapVisibilityProjectionFailureDelay
		} else if hasMore {
			wait = bootstrapVisibilityProjectionTurnDelay
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-runner.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (runner *bootstrapVisibilityProjectionRunner) advanceBurst(ctx context.Context) (bool, error) {
	for turnIndex := 0; turnIndex < bootstrapVisibilityProjectionRunnerBurst; turnIndex++ {
		// People's edits first (write_lane.go).
		yieldToPeople(ctx)
		turn, err := advanceBootstrapVisibilityProjection(ctx, runner.app)
		if err != nil {
			return false, err
		}
		if !turn.HasMore {
			return false, nil
		}
	}
	return true, nil
}

func registerBootstrapVisibilityProjectionWake(
	event *core.ServeEvent,
	runner *bootstrapVisibilityProjectionRunner,
) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: bootstrapVisibilityProjectionWakeHandlerID,
		Func: func(request *core.RequestEvent) error {
			err := request.Next()
			if request.Request.Method != http.MethodGet && request.Request.Method != http.MethodHead &&
				request.Request.Method != http.MethodOptions {
				runner.notify()
			}
			return err
		},
	})
}
