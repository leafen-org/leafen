//go:build !leafen_noapps

package app

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// App budgets (docs/app-platform.md 15, stage supplement ①, T10): every
// call an app makes to Leafen's native API — its screen's token, a worker,
// an agent using its tools — passes the key check, where it draws on a
// budget of the app's and the server's. A call made as a person draws on
// the app's budget for that person, one its own account makes on the app's
// own (2026-10-18: a drive's people shared one budget, so one person's
// folder copy slowed and failed). When one runs short the call waits a
// moment; past that it is refused. An app's writes go one at a time across
// the server, small, so people's saving stays ahead of an app's bulk work.
// An app refused minute after minute is stopped, with the reason kept.

const (
	appBudgetCallsPerMinute  = 600
	appBudgetWritesPerMinute = 120
	appBudgetServerPerMinute = 3000
	appBudgetWait            = 2 * time.Second
	appBudgetWriteWait       = 10 * time.Second
	appBudgetWriteMaxBytes   = 256 << 10
	appBudgetStopMinutes     = 10
	appBudgetStopRefusals    = 100
	appBudgetPausedReason    = "budget"
)

type appBucket struct {
	tokens, capacity, perSecond float64
	refilled                    time.Time
}

func newAppBucket(perMinute float64) appBucket {
	return appBucket{tokens: perMinute, capacity: perMinute, perSecond: perMinute / 60}
}

func (bucket *appBucket) refill(now time.Time) {
	if !bucket.refilled.IsZero() {
		bucket.tokens = min(bucket.capacity, bucket.tokens+now.Sub(bucket.refilled).Seconds()*bucket.perSecond)
	}
	bucket.refilled = now
}

// wait is how long until the bucket has a call for one more, after those
// already promised; none when it has one now.
func (bucket *appBucket) wait() time.Duration {
	if bucket.tokens >= 1 {
		return 0
	}
	return time.Duration((1 - bucket.tokens) / bucket.perSecond * float64(time.Second))
}

type appUsageCount struct {
	reads, writes, delayed, refused int64
}

type appBudgetState struct {
	calls, writes  appBucket
	usage          map[int64]*appUsageCount
	minute         int64
	refusedMinute  int
	overrunMinutes int
}

// appPersonBudget is an app's budget for one person's calls.
type appPersonBudget struct {
	calls, writes appBucket
}

// appBudgetPeopleKept is how many people's budgets are kept before those
// full again are let go: a full budget is the same as a new one.
const appBudgetPeopleKept = 4096

var appBudgets = struct {
	sync.Mutex
	apps      map[string]*appBudgetState
	people    map[string]*appPersonBudget
	server    appBucket
	writeGate chan struct{}
	stopping  func(installID string)
}{apps: map[string]*appBudgetState{}, people: map[string]*appPersonBudget{}, server: newAppBucket(appBudgetServerPerMinute), writeGate: make(chan struct{}, 1)}

func init() {
	appBudgetGate = admitAppCall
}

// appBudgetApplies reports whether a call is one of Leafen's own operations
// rather than an app platform route, which keeps limits of its own.
func appBudgetApplies(path string) bool {
	path = strings.TrimPrefix(path, apiRoot)
	return !strings.HasPrefix(path, "/apps/") && !strings.HasPrefix(path, "/app-guests/") && !strings.HasPrefix(path, "/installed-apps")
}

// appBudgetPerson is whose budget of the app a call draws on: the person
// it is made as, or none for the app's own account.
func appBudgetPerson(authority userKeyAuthority) string {
	if authority.AppAccount {
		return ""
	}
	return authority.OwnerID
}

// admitAppCall lets an app's call through within its budget, waiting a
// moment when the budget runs short; the release ends a write's turn, and
// admitted is false when it answered the refusal or the caller left.
func admitAppCall(event *core.RequestEvent, authority userKeyAuthority) (func(), bool, error) {
	if !appBudgetApplies(event.Request.URL.Path) {
		return func() {}, true, nil
	}
	installID, personID := authority.AppInstallID, appBudgetPerson(authority)
	write := event.Request.Method != http.MethodGet && event.Request.Method != http.MethodHead && !requestOnlyReads(event.Request.URL.Path)
	if write && event.Request.ContentLength > appBudgetWriteMaxBytes {
		countAppCall(installID, appUsageCount{refused: 1}, time.Now())
		return nil, false, apiError(event, http.StatusRequestEntityTooLarge, errorCodeAppWriteTooLarge,
			"An app writes in parts of up to 256 KB.")
	}
	ctx := event.Request.Context()
	// A call short of budget takes its turn and waits for it, so calls go
	// in the order they came (2026-10-18: waiting calls that looked again
	// every 100 ms were overtaken by new ones until they were refused).
	wait, admitted := takeAppBudget(installID, personID, write, time.Now())
	if !admitted {
		countAppCall(installID, appUsageCount{refused: 1}, time.Now())
		event.Response.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil((wait-appBudgetWait).Seconds())))))
		return nil, false, apiError(event, http.StatusTooManyRequests, errorCodeAppBudgetLimited, "The app is over its budget; try again shortly.")
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, false, ctx.Err()
		case <-timer.C:
		}
	}
	count := appUsageCount{reads: 1}
	if write {
		count = appUsageCount{writes: 1}
	}
	if wait > 0 {
		count.delayed = 1
	}
	countAppCall(installID, count, time.Now())
	if !write {
		return func() {}, true, nil
	}
	timer := time.NewTimer(appBudgetWriteWait)
	defer timer.Stop()
	select {
	case appBudgets.writeGate <- struct{}{}:
		return func() { <-appBudgets.writeGate }, true, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case <-timer.C:
		countAppCall(installID, appUsageCount{refused: 1}, time.Now())
		event.Response.Header().Set("Retry-After", "1")
		return nil, false, apiError(event, http.StatusTooManyRequests, errorCodeAppBudgetLimited, "Apps' writes are queued; try again shortly.")
	}
}

func appBudgetFor(installID string) *appBudgetState {
	state := appBudgets.apps[installID]
	if state == nil {
		state = &appBudgetState{calls: newAppBucket(appBudgetCallsPerMinute), writes: newAppBucket(appBudgetWritesPerMinute), usage: map[int64]*appUsageCount{}}
		appBudgets.apps[installID] = state
	}
	return state
}

// takeAppBudget promises a call from the app's budget — the person's, or
// its own account's when personID is empty — and the server's, and a write
// from that budget's writes, when all of them have one within the wait a
// call is given; it says how long the call waits for it.
func takeAppBudget(installID, personID string, write bool, now time.Time) (time.Duration, bool) {
	appBudgets.Lock()
	defer appBudgets.Unlock()
	state := appBudgetFor(installID)
	calls, writes := &state.calls, &state.writes
	if personID != "" {
		person := appPersonBudgetFor(installID, personID, now)
		calls, writes = &person.calls, &person.writes
	}
	calls.refill(now)
	writes.refill(now)
	appBudgets.server.refill(now)
	wait := max(calls.wait(), appBudgets.server.wait())
	if write {
		wait = max(wait, writes.wait())
	}
	if wait > appBudgetWait {
		return wait, false
	}
	calls.tokens--
	appBudgets.server.tokens--
	if write {
		writes.tokens--
	}
	return wait, true
}

func appPersonBudgetFor(installID, personID string, now time.Time) *appPersonBudget {
	key := installID + "\x00" + personID
	person := appBudgets.people[key]
	if person != nil {
		return person
	}
	if len(appBudgets.people) >= appBudgetPeopleKept {
		for name, kept := range appBudgets.people {
			if now.Sub(kept.calls.refilled) >= time.Minute && now.Sub(kept.writes.refilled) >= time.Minute {
				delete(appBudgets.people, name)
			}
		}
	}
	person = &appPersonBudget{calls: newAppBucket(appBudgetCallsPerMinute), writes: newAppBucket(appBudgetWritesPerMinute)}
	appBudgets.people[key] = person
	return person
}

// countAppCall adds a call to the app's hour, and stops an app refused
// often enough for long enough.
func countAppCall(installID string, count appUsageCount, now time.Time) {
	appBudgets.Lock()
	defer appBudgets.Unlock()
	state := appBudgetFor(installID)
	hour := now.Truncate(time.Hour).UnixMilli()
	usage := state.usage[hour]
	if usage == nil {
		usage = &appUsageCount{}
		state.usage[hour] = usage
	}
	usage.reads += count.reads
	usage.writes += count.writes
	usage.delayed += count.delayed
	usage.refused += count.refused
	minute := now.Unix() / 60
	if minute != state.minute {
		if state.refusedMinute >= appBudgetStopRefusals && minute == state.minute+1 {
			state.overrunMinutes++
		} else {
			state.overrunMinutes = 0
		}
		state.minute, state.refusedMinute = minute, 0
	}
	state.refusedMinute += int(count.refused)
	if state.overrunMinutes >= appBudgetStopMinutes && appBudgets.stopping != nil {
		state.overrunMinutes = 0
		// Behind the call, under the app platform's owner (startAppUsageSaving).
		appBudgets.stopping(installID)
	}
}

// stopOverrunningApp stops an app that kept overrunning its budget and says why.
func stopOverrunningApp(app core.App, installID string) {
	// The app stops with its reason and its line in the audit, or not at all.
	install, err := setAppInstallStatusWith(app, installID, "stopped", "", func(tx core.App, install storedAppInstall) error {
		if _, err := tx.DB().Update(appInstallsTable, dbx.Params{"paused_reason": appBudgetPausedReason}, dbx.HashExp{"id": installID}).Execute(); err != nil {
			return err
		}
		return recordAppAudit(tx, install, "budget-stopped", "", map[string]any{"minutes": appBudgetStopMinutes, "refusalsPerMinute": appBudgetStopRefusals})
	})
	if err != nil {
		app.Logger().Warn("leafen app budget stop failed", "install", installID, "error", err)
		return
	}
	notice := "app-stopped:" + installID + ":" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	warnAppNotice(app, install, notice, notifyAppAdministrators(app, install, notice, nil))
}

// saveAppUsage writes the hours' counts kept in memory, keeping the current
// hour's running count there too until the hour is over.
func saveAppUsage(app core.App, now time.Time) error {
	appBudgets.Lock()
	pending := map[string]map[int64]appUsageCount{}
	for installID, state := range appBudgets.apps {
		for hour, usage := range state.usage {
			if pending[installID] == nil {
				pending[installID] = map[int64]appUsageCount{}
			}
			pending[installID][hour] = *usage
			delete(state.usage, hour)
		}
	}
	appBudgets.Unlock()
	for installID, hours := range pending {
		for hour, usage := range hours {
			if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_app_usage}} ([[install_id]], [[hour]], [[reads]], [[writes]], [[delayed]], [[refused]])
				SELECT {:install}, {:hour}, {:reads}, {:writes}, {:delayed}, {:refused}
				WHERE EXISTS (SELECT 1 FROM {{leafen_app_installs}} WHERE [[id]] = {:install})
				ON CONFLICT ([[install_id]], [[hour]]) DO UPDATE SET [[reads]] = [[reads]] + excluded.[[reads]],
					[[writes]] = [[writes]] + excluded.[[writes]], [[delayed]] = [[delayed]] + excluded.[[delayed]],
					[[refused]] = [[refused]] + excluded.[[refused]]`).Bind(dbx.Params{"install": installID, "hour": hour,
				"reads": usage.reads, "writes": usage.writes, "delayed": usage.delayed, "refused": usage.refused}).Execute(); err != nil {
				return err
			}
		}
	}
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_app_usage}} WHERE [[hour]] < {:before}`).
		Bind(dbx.Params{"before": now.Add(-30 * 24 * time.Hour).UnixMilli()}).Execute()
	return err
}

// startAppUsageSaving saves usage every minute and stops apps that kept
// overrunning their budgets.
func startAppUsageSaving(app core.App, background *appPlatformBackground) {
	appBudgets.Lock()
	// Stopped with the server, none once it is stopping (app_platform_background.go).
	appBudgets.stopping = func(installID string) {
		background.run(func(context.Context) { stopOverrunningApp(app, installID) })
	}
	appBudgets.Unlock()
	save := func() {
		if err := saveAppUsage(app, time.Now()); err != nil {
			app.Logger().Warn("leafen app usage save failed", "error", err)
		}
		if err := saveAppEndpointUsage(app, time.Now()); err != nil {
			app.Logger().Warn("leafen app endpoint usage save failed", "error", err)
		}
		if err := saveAppDataReads(app); err != nil {
			app.Logger().Warn("leafen app data reads save failed", "error", err)
		}
	}
	background.run(func(ctx context.Context) {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				// The minute's usage is saved before the server stops.
				save()
				return
			case <-ticker.C:
				save()
			}
		}
	})
}
