package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// The server's background work that is no runner of its own — the loops it
// starts as it begins to serve, the waits they and its requests start (a
// moved workspace's follow-up, a move waiting for the new server, the
// restore code's watch), the short one-time writes an event starts (a
// notice that the disk is full or the address changed, the last use of an
// agent's key, a session's or an earlier address's), the catch-ups it does
// as it starts, every job of its clock, PocketBase's own among them
// (ownServerClock), and the tunnels' door (server_tunnel_door.go) — runs
// under one owner for each server, serverBackgroundOf. The owner's stop is
// one of the server's parts (app.go leafenProductShutdown): it cancels the
// work and waits for it, so none reaches the database after the server
// closes it, and starts nothing once the server is stopping — a write an
// event would start then is let go, as the server would have lost it a
// moment later. Work the server stopped goes on where it left off when a
// server starts on the data again (each says how).
//
// The desktop and phone apps start their server again in the same process
// ("지금 다시 시작" after a restore): the first server's data closes and a new
// server opens on the same folder, so work the first one left running would
// read its closed database on its next turn and end the whole app
// (2026-11-21: the moved workspaces' follow-up, a minute after the restart),
// and a door the first one left open would keep its port from the new one.
// A runner with a Start and a Shutdown of its own (app.go), the app
// platform's work (app_platform_background.go) and the organization
// module's runners (organization_module.go) stop with the server the same
// way.
//
// Left as they are, each for its reason: a person's long one-time run — a
// server move's sending (server_move_out.go) and a setup restore's staging
// (setup_restore_agent.go) — holds the backup mark, so no restore, and so
// no restart in the same process, begins while it runs, and a stop then is
// the app ending; the screens' own reads behind a change
// (ygo_room_graph_readers.go) end on a defect quietly in their own guard;
// and the address check a moment after it applies holds nothing of the data.

const serverBackgroundStoreKey = "leafen.serverBackground"

type serverBackground struct {
	app     core.App
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	closed  bool
	running map[string]int
	wait    sync.WaitGroup
}

// serverBackgroundOf is the server's owner of its background work.
func serverBackgroundOf(app core.App) *serverBackground {
	return app.Store().GetOrSet(serverBackgroundStoreKey, func() any {
		ctx, cancel := context.WithCancel(context.Background())
		return &serverBackground{app: app, ctx: ctx, cancel: cancel, running: map[string]int{}}
	}).(*serverBackground)
}

// run starts work behind the caller, named what for the logs; its ctx ends
// as the server begins to stop. A defect in it is logged rather than ending
// the server. Once the server is stopping it starts nothing and says false.
func (background *serverBackground) run(what string, work func(ctx context.Context)) bool {
	background.mu.Lock()
	defer background.mu.Unlock()
	if background.closed {
		return false
	}
	background.running[what]++
	background.wait.Add(1)
	go func() {
		defer func() {
			background.mu.Lock()
			if background.running[what]--; background.running[what] == 0 {
				delete(background.running, what)
			}
			background.mu.Unlock()
			background.wait.Done()
		}()
		runGuarded(background.app, what, func() { work(background.ctx) })
	}()
	return true
}

// behind runs a short one-time write an event starts behind it, as run does.
func (background *serverBackground) behind(what string, work func()) bool {
	return background.run(what, func(context.Context) { work() })
}

// Shutdown cancels the work and waits for it until ctx ends, naming what is
// still running then.
func (background *serverBackground) Shutdown(ctx context.Context) error {
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
		return nil
	case <-ctx.Done():
	}
	background.mu.Lock()
	names := make([]string, 0, len(background.running))
	for what := range background.running {
		names = append(names, what)
	}
	background.mu.Unlock()
	sort.Strings(names)
	return fmt.Errorf("leafen background work didn't stop with the server (%s): %w", strings.Join(names, ", "), ctx.Err())
}

// waitUnlessStopped waits for d and says true, or false at once when the
// server begins to stop.
func waitUnlessStopped(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// serverClockOwnedPriority binds ownServerClock ahead of every other serve
// hook, so that it puts the jobs under the owner once they have all run.
const serverClockOwnedPriority = -1 << 30

// ownServerClock puts every job on the server's clock under its owner once
// the server serves: PocketBase's own (its logs', one-time codes' and rate
// limits' cleanups) as well as Leafen's. A job running as the server stops
// is then waited for like the rest, and none begins once the server is
// stopping (the clock itself is stopped first, app.go); PocketBase's ran
// on, reaching the closed database after the stop. A job put on the clock
// later is one of Leafen's, which runs under the owner in its own body
// (PocketBase's settings that would add one stay closed, product_boundary.go).
func ownServerClock(pb core.App) {
	pb.OnServe().Bind(&hook.Handler[*core.ServeEvent]{
		Id:       "leafenServerClockOwned",
		Priority: serverClockOwnedPriority,
		Func: func(event *core.ServeEvent) error {
			if err := event.Next(); err != nil {
				return err
			}
			background := serverBackgroundOf(event.App)
			clock := event.App.Cron()
			for _, job := range clock.Jobs() {
				what := "clock job " + job.Id()
				_ = clock.Add(job.Id(), job.Expression(), func() {
					background.run(what, func(context.Context) { job.Run() })
				})
			}
			return nil
		},
	})
}
