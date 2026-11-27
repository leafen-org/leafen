package app

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Work beside the requests that reads or writes the data on a clock of its
// own wherever the data is open, a command's as well as a server's — the
// log's checkpoints (wal_checkpoint.go) and the page lists' look for changes
// (workspace_page_list_wait.go) — runs under dataWorkOf (2026-11-26).
// PocketBase closes the databases in ClearBootstrap: at the end of every
// stop's OnTerminate chain (app.go), as a restore puts other data in their
// place (backup_restore.go) and before a bootstrap opens them again. Its
// OnBootstrapClear stops that work and waits for it first, so none reads a
// database while it closes; it starts again once the data opens again.
// Whether the data is open is read from here as well, never from
// app.IsBootstrapped(), which reads the fields ClearBootstrap writes without
// a lock (2026-11-26: the race detector said so at every stop, the log's
// checkpoint asking as the databases closed).
//
// Work that stops earlier in the OnTerminate chain, before the replica's
// last sending — runners with a Start and a Shutdown, the server's
// background work (server_background.go) — stays there.

const dataWorkStoreKey = "leafen.dataWork"

// dataWorkStopWait is how long a close waits for the work; past it, the
// databases close under what is left and the log names it.
const dataWorkStopWait = 2 * time.Second

type dataWork struct {
	mu      sync.Mutex
	open    bool
	ctx     context.Context
	cancel  context.CancelFunc
	wait    *sync.WaitGroup
	running map[string]int
	// each is the work that runs whenever the data is open.
	each []dataWorkEach
}

type dataWorkEach struct {
	what string
	work func(context.Context, core.App)
}

// dataWorkOf is the owner of the app's work on its data.
func dataWorkOf(app core.App) *dataWork {
	return app.Store().GetOrSet(dataWorkStoreKey, func() any {
		return &dataWork{running: map[string]int{}}
	}).(*dataWork)
}

// registerDataWork starts the work as the data opens and stops it, waited
// for, before the data closes.
func registerDataWork(pb core.App) {
	pb.OnBootstrap().BindFunc(func(event *core.BootstrapEvent) error {
		if err := event.Next(); err != nil {
			return err
		}
		dataWorkOf(event.App).opened(event.App)
		return nil
	})
	pb.OnBootstrapClear().BindFunc(func(event *core.BootstrapEvent) error {
		dataWorkOf(event.App).closing(event.App)
		return event.Next()
	})
}

// whileOpen has work run each time the data opens, until it closes.
func (owner *dataWork) whileOpen(what string, work func(context.Context, core.App)) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.each = append(owner.each, dataWorkEach{what: what, work: work})
}

// isOpen is whether the app's data is open.
func (owner *dataWork) isOpen() bool {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return owner.open
}

func (owner *dataWork) opened(app core.App) {
	owner.mu.Lock()
	owner.ctx, owner.cancel = context.WithCancel(context.Background())
	owner.wait, owner.open = &sync.WaitGroup{}, true
	each := slices.Clone(owner.each)
	owner.mu.Unlock()
	for _, one := range each {
		owner.run(app, one.what, func(ctx context.Context) { one.work(ctx, app) })
	}
}

// run starts work on the data now, named what for the log; its ctx ends as
// the data begins to close. While the data isn't open it starts nothing and
// says false. A defect in it is logged rather than ending the process.
func (owner *dataWork) run(app core.App, what string, work func(ctx context.Context)) bool {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if !owner.open {
		return false
	}
	ctx, wait := owner.ctx, owner.wait
	owner.running[what]++
	wait.Add(1)
	go func() {
		defer func() {
			owner.mu.Lock()
			if owner.running[what]--; owner.running[what] <= 0 {
				delete(owner.running, what)
			}
			owner.mu.Unlock()
			wait.Done()
		}()
		runGuarded(app, what, func() { work(ctx) })
	}()
	return true
}

// closing stops the work and waits for it, up to dataWorkStopWait.
func (owner *dataWork) closing(app core.App) {
	owner.mu.Lock()
	if !owner.open {
		owner.mu.Unlock()
		return
	}
	owner.open = false
	owner.cancel()
	wait := owner.wait
	owner.mu.Unlock()
	done := make(chan struct{})
	go func() {
		wait.Wait()
		close(done)
	}()
	select {
	case <-done:
		return
	case <-time.After(dataWorkStopWait):
	}
	owner.mu.Lock()
	names := make([]string, 0, len(owner.running))
	for what := range owner.running {
		names = append(names, what)
	}
	owner.mu.Unlock()
	sort.Strings(names)
	app.Logger().Warn("leafen work on the data didn't stop before the data closed", "running", names,
		"waited", dataWorkStopWait.String())
}
