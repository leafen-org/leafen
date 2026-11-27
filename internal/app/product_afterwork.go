package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/pocketbase/pocketbase/core"
)

const productAfterworkStoreKey = "leafen.productAfterwork"

// productAfterwork owns the work a request leaves to do after it answers —
// a renamed person's mentions, a security notice's mail. The server's shutdown stops new work, cancels
// what runs and waits for it, so none reaches the database after the
// server closes it (2026-11-10: a join run after a test app closed read a
// nil database and took the whole process down).
//
// What the stop cancelled or refused goes on when the server starts again
// (product_afterwork_kept.go): each work is a kind, by which it is made
// again from what it was given. Cancelled, a renamed person's search kept
// the old name after the restart, with nothing to read their mentions again
// (2026-11-18).
type productAfterwork struct {
	app    core.App
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	wait   sync.WaitGroup
	// root is the server's own app, which keeps what is left to do
	// (resumeProductAfterwork); running and left are the work under way and
	// the work the stop cancelled or refused, until kept says they are written.
	root    core.App
	running map[*keptAfterwork]struct{}
	left    []keptAfterwork
	kept    bool
}

// productAfterworkOf is the app's one owner, made when first asked.
func productAfterworkOf(app core.App) *productAfterwork {
	owner, _ := app.Store().GetOrSet(productAfterworkStoreKey, func() any {
		ctx, cancel := context.WithCancel(context.Background())
		return &productAfterwork{app: app, ctx: ctx, cancel: cancel, running: map[*keptAfterwork]struct{}{}}
	}).(*productAfterwork)
	return owner
}

// run starts work of a kind (product_afterwork_kept.go) made from input after
// the request, unless the server is shutting down; a panic in it is logged,
// not the process's end. Work the stop refuses, or cancels before it ends —
// it returns an error once its ctx is done — is done again when the server
// starts again.
func (afterwork *productAfterwork) run(kind, input string, work func(ctx context.Context) error) bool {
	entry := &keptAfterwork{Kind: kind, Input: input}
	afterwork.mu.Lock()
	defer afterwork.mu.Unlock()
	if afterwork.closed {
		afterwork.leave(entry)
		return false
	}
	afterwork.running[entry] = struct{}{}
	afterwork.wait.Add(1)
	go func() {
		defer afterwork.wait.Done()
		var err error
		runGuarded(afterwork.app, kind, func() { err = work(afterwork.ctx) })
		afterwork.mu.Lock()
		defer afterwork.mu.Unlock()
		delete(afterwork.running, entry)
		// One begun while its request's writes were the request's is stopped
		// with them (request_write_stop.go) before the stop cancels it.
		if err != nil && (afterwork.ctx.Err() != nil || requestWriteStoppedBy(err)) {
			afterwork.leave(entry)
		}
	}()
	return true
}

// begin notes what an operation left to write as under way
// (ygo_persistence_follow_ups.go): a stop that comes while it runs writes
// it for the next start, as it does work still running, and the end it
// gives leaves it for the next start when the stop cut it, or else drops it,
// and says whether it is left. Left only when it ended, one the stop cut was
// often written after the stop had written what it leaves, and lost; one
// begun after that is lost when the stop cuts it.
func (afterwork *productAfterwork) begin(kept keptAfterwork) func(cut bool) bool {
	entry := &kept
	afterwork.mu.Lock()
	late := afterwork.kept
	afterwork.running[entry] = struct{}{}
	afterwork.mu.Unlock()
	return func(cut bool) bool {
		afterwork.mu.Lock()
		defer afterwork.mu.Unlock()
		delete(afterwork.running, entry)
		switch {
		case !cut:
			return false
		case !afterwork.kept:
			afterwork.left = append(afterwork.left, *entry)
			return true
		case late:
			afterwork.app.Logger().Warn("leafen request afterwork lost as the server stopped", "kind", entry.Kind)
			return false
		}
		// Once the stop wrote what it leaves, it was written as running.
		return true
	}
}

// leave notes work the stop cancelled or refused, under mu.
func (afterwork *productAfterwork) leave(entry *keptAfterwork) {
	if afterwork.kept {
		afterwork.app.Logger().Warn("leafen request afterwork lost as the server stopped", "kind", entry.Kind)
		return
	}
	afterwork.left = append(afterwork.left, *entry)
}

func (afterwork *productAfterwork) Shutdown(ctx context.Context) error {
	afterwork.mu.Lock()
	afterwork.closed = true
	afterwork.mu.Unlock()
	afterwork.cancel()
	done := make(chan struct{})
	go func() {
		afterwork.wait.Wait()
		close(done)
	}()
	var waitErr error
	select {
	case <-done:
	case <-ctx.Done():
		waitErr = fmt.Errorf("leafen request afterwork shutdown: %w", ctx.Err())
	}
	// What the stop cancelled or refused, and what still runs past its time,
	// is written for the next start.
	afterwork.mu.Lock()
	left := append([]keptAfterwork(nil), afterwork.left...)
	for entry := range afterwork.running {
		left = append(left, *entry)
	}
	afterwork.left, afterwork.kept = nil, true
	root := afterwork.root
	afterwork.mu.Unlock()
	if err := keepAfterworkLeft(root, left); err != nil {
		afterwork.app.Logger().Warn("leafen request afterwork lost as the server stopped", "count", len(left), "error", err)
	}
	return waitErr
}

// errAfterworkUnkept is work left with no server app to keep it.
var errAfterworkUnkept = errors.New("no server app keeps the request afterwork")
