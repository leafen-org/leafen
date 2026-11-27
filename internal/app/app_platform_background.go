//go:build !leafen_noapps

package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const appPlatformBackgroundStoreKey = "leafen.appPlatformBackground"

// appPlatformBackground owns the app platform's loops, the hook calls they
// make and its long runs. Shutting it down cancels them and waits for them,
// so none reaches the database after the server closes it, then ends the
// idle workers.
type appPlatformBackground struct {
	app    core.App
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	wait   sync.WaitGroup
}

func newAppPlatformBackground(app core.App) *appPlatformBackground {
	ctx, cancel := context.WithCancel(context.Background())
	background := &appPlatformBackground{app: app, ctx: ctx, cancel: cancel}
	app.Store().Set(appPlatformBackgroundStoreKey, background)
	return background
}

// appPlatformBackgroundOf is the server's owner; without one, as when a
// step is driven on its own, the work runs untracked.
func appPlatformBackgroundOf(app core.App) *appPlatformBackground {
	if background, ok := app.Store().Get(appPlatformBackgroundStoreKey).(*appPlatformBackground); ok {
		return background
	}
	return &appPlatformBackground{app: app, ctx: context.Background(), cancel: func() {}}
}

// run starts work under the owner, unless it is shutting down.
func (background *appPlatformBackground) run(work func(ctx context.Context)) bool {
	background.mu.Lock()
	defer background.mu.Unlock()
	if background.closed {
		return false
	}
	background.wait.Add(1)
	go func() {
		defer background.wait.Done()
		runGuarded(background.app, "app platform background work", func() { work(background.ctx) })
	}()
	return true
}

func (background *appPlatformBackground) Shutdown(ctx context.Context) error {
	background.mu.Lock()
	background.closed = true
	background.mu.Unlock()
	background.cancel()
	done := make(chan struct{})
	go func() {
		background.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("leafen app platform shutdown: %w", ctx.Err())
	}
	appWorkers.endIdle(time.Now(), func(string) bool { return true })
	return nil
}
