//go:build !leafen_noapps

package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Long-running workers (docs/app-platform.md 12): a worker the manifest
// marks longRunning — approved with the app — keeps its run function going
// in each organization the app has a workspace in: a mail sync holding an
// IMAP connection, a stream without webhooks. Each run is a worker process
// of its own for up to an hour, with its host calls counted by the minute;
// leafen_wait lets it rest and tells it when to stop (the app stopped, the
// hour is nearly up). A run that ends is started again — a minute after it
// returns, at once after its hour, after a growing pause when it fails —
// and nothing runs while the app is stopped.

const (
	appLongRunLifetime       = time.Hour
	appLongRunCallsPerMinute = 600
	appLongRunWaitMax        = 60 * time.Second
	appLongRunCheck          = 30 * time.Second
	appLongRunRest           = time.Minute
	appLongRunGrace          = 10 * time.Second
	appLongRunBackoffMax     = 10 * time.Minute
)

type appLongRun struct {
	installID, tenancy, worker, function string

	stopping   atomic.Bool
	stopSignal chan struct{}
	cancel     context.CancelFunc
	started    time.Time
	running    bool
	restarts   int
	nextStart  time.Time
	lastError  string
	lastRunID  string
}

var appLongRuns = struct {
	sync.Mutex
	runs map[string]*appLongRun
}{runs: map[string]*appLongRun{}}

func init() {
	appStopCleanups = append(appStopCleanups, stopAppLongRuns)
}

// startAppLongRunner keeps the long-running workers going until the server stops.
func startAppLongRunner(app core.App, background *appPlatformBackground) {
	if appWorkerLimits.longRuns == 0 {
		return
	}
	background.run(func(ctx context.Context) {
		ticker := time.NewTicker(appLongRunCheck)
		defer ticker.Stop()
		for {
			if err := reconcileAppLongRuns(app, time.Now()); err != nil {
				app.Logger().Warn("leafen long-running app workers failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
}

func appLongRunKey(installID, worker, tenancy string) string {
	return installID + "\x00" + worker + "\x00" + tenancy
}

// reconcileAppLongRuns starts the runs that are due and stops those whose
// app or workspace is gone.
func reconcileAppLongRuns(app core.App, now time.Time) error {
	installs, err := loadAppInstalls(app, "[[status]] = 'active' AND [[removed_at]] = 0", dbx.Params{})
	if err != nil {
		return err
	}
	type due struct {
		install  storedAppInstall
		manifest appManifest
		run      *appLongRun
		ctx      context.Context
	}
	background := appPlatformBackgroundOf(app)
	starting := []due{}
	wanted := map[string]bool{}
	appLongRuns.Lock()
	running := 0
	for _, run := range appLongRuns.runs {
		if run.running {
			running++
		}
	}
	for _, install := range installs {
		_, manifest, err := loadAppPackage(app, install.PackageHash)
		if err != nil {
			continue
		}
		workspaces, err := loadAppWorkspaces(app, install.ID)
		if err != nil {
			appLongRuns.Unlock()
			return err
		}
		for _, worker := range manifest.Workers {
			if !worker.LongRunning || worker.Run == "" {
				continue
			}
			for _, workspace := range workspaces {
				key := appLongRunKey(install.ID, worker.Name, workspace.TenancyKey)
				wanted[key] = true
				run := appLongRuns.runs[key]
				if run == nil {
					run = &appLongRun{installID: install.ID, tenancy: workspace.TenancyKey, worker: worker.Name, function: worker.Run, nextStart: now}
					appLongRuns.runs[key] = run
				}
				if !run.running && !now.Before(run.nextStart) && running < appWorkerLimits.longRuns {
					// Ready to be stopped before it begins.
					// A run ends with the server too.
					ctx, cancel := context.WithTimeout(background.ctx, appLongRunLifetime)
					run.running, run.started, run.cancel, run.stopSignal = true, now, cancel, make(chan struct{})
					run.stopping.Store(false)
					running++
					starting = append(starting, due{install: install, manifest: manifest, run: run, ctx: ctx})
				}
			}
		}
	}
	for key, run := range appLongRuns.runs {
		if wanted[key] {
			continue
		}
		run.stop()
		if !run.running {
			delete(appLongRuns.runs, key)
		}
	}
	appLongRuns.Unlock()
	for _, next := range starting {
		next.run.begin(next.ctx, app, next.install, next.manifest, background)
	}
	return nil
}

// begin runs the function once on a worker of its own, then sets when it
// starts again.
func (run *appLongRun) begin(ctx context.Context, app core.App, install storedAppInstall, manifest appManifest,
	background *appPlatformBackground) {
	appLongRuns.Lock()
	started, restarts, cancel := run.started, run.restarts, run.cancel
	appLongRuns.Unlock()
	input, _ := json.Marshal(map[string]any{"run": run.function, "tenancy": run.tenancy, "startedAt": started.UnixMilli(), "restarts": restarts})
	launched := background.run(func(context.Context) {
		defer cancel()
		_, runID, err := runAppWorker(ctx, app, install, manifest, appRun{Trigger: "long", Ref: run.worker, Worker: run.worker,
			Function: run.function, Input: input, Tenancy: run.tenancy}, run)
		appLongRuns.Lock()
		defer appLongRuns.Unlock()
		now := time.Now()
		run.running, run.lastRunID = false, runID
		switch {
		case run.stopping.Load():
			run.nextStart = now
		case err != nil && now.Sub(started) < appLongRunLifetime-appLongRunWaitMax:
			run.restarts++
			run.lastError = appOneLine(err.Error(), 300)
			pause := 30 * time.Second << min(run.restarts-1, 5)
			run.nextStart = now.Add(min(pause, appLongRunBackoffMax))
		case now.Sub(started) >= appLongRunLifetime-appLongRunWaitMax:
			run.restarts, run.nextStart = 0, now
		default:
			run.restarts, run.nextStart = 0, now.Add(appLongRunRest)
		}
	})
	if !launched {
		// The server is stopping: the run doesn't start.
		cancel()
		appLongRuns.Lock()
		run.running = false
		appLongRuns.Unlock()
	}
}

// stop tells the run to end, and ends it if it doesn't soon.
func (run *appLongRun) stop() {
	if !run.running || run.stopping.Load() {
		return
	}
	run.stopping.Store(true)
	close(run.stopSignal)
	cancel := run.cancel
	time.AfterFunc(appLongRunGrace, cancel)
}

// stopAppLongRuns stops an app's runs when it stops or goes.
func stopAppLongRuns(installID string) {
	appLongRuns.Lock()
	defer appLongRuns.Unlock()
	for key, run := range appLongRuns.runs {
		if run.installID != installID {
			continue
		}
		run.stop()
		if !run.running {
			delete(appLongRuns.runs, key)
		}
	}
}

// runAppLongWorker runs one long call on a worker process of its own,
// ended with the run's context.
func runAppLongWorker(ctx context.Context, call appWorkerCall, host func(string, []byte) ([]byte, error)) ([]byte, error) {
	conn, err := appWorkerStart()
	if err != nil {
		return nil, err
	}
	defer conn.stop()
	stopWatch := context.AfterFunc(ctx, conn.stop)
	defer stopWatch()
	return appWorkers.exchange(conn, appLongRunCallIDs.Add(1), call, host)
}

var appLongRunCallIDs atomic.Int64

// wait answers leafen_wait {ms}: a long-running worker rests up to a minute
// and hears whether to stop.
func (host *appWorkerHost) wait(payload []byte) ([]byte, error) {
	if host.long == nil {
		return nil, errors.New("leafen_wait is for long-running workers")
	}
	request := struct {
		Ms int64 `json:"ms"`
	}{}
	_ = json.Unmarshal(payload, &request)
	pause := min(max(time.Duration(request.Ms)*time.Millisecond, 0), appLongRunWaitMax)
	timer := time.NewTimer(pause)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-host.long.stopSignal:
	case <-host.ctx.Done():
	}
	stop := host.long.stopping.Load() || host.ctx.Err() != nil
	if deadline, ok := host.ctx.Deadline(); ok && time.Until(deadline) < appLongRunWaitMax {
		stop = true
	}
	return json.Marshal(map[string]bool{"stop": stop})
}

// appLongRunStates is each long-running worker's state, for the
// administrators' jobs window.
func appLongRunStates(installID string) []appLongRunWire {
	appLongRuns.Lock()
	defer appLongRuns.Unlock()
	states := []appLongRunWire{}
	for _, run := range appLongRuns.runs {
		if run.installID != installID {
			continue
		}
		status := "waiting"
		if run.running {
			status = "running"
		} else if run.restarts > 0 {
			status = "failing"
		}
		state := appLongRunWire{Worker: run.worker, TenancyKey: run.tenancy, Status: status, Restarts: run.restarts, LastError: run.lastError}
		if run.running {
			state.StartedAt = run.started.UnixMilli()
		} else {
			state.NextStart = run.nextStart.UnixMilli()
		}
		states = append(states, state)
	}
	return states
}
