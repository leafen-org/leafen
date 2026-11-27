//go:build !leafen_noapps

package app

import (
	"net/http"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// An app's endpoints within limits (document apps decision of 2026-10-10,
// 8): requests a minute for each person and at once for the app, and how
// much its endpoints may keep for each person — none of the app's own by
// default, the organization's storage limit still applying — each of which
// an administrator raises or lowers for the app. What they did is counted by
// the hour beside the app's other usage.

const appEndpointMiB = 1 << 20

type appEndpointGateState struct {
	mu      sync.Mutex
	windows map[string]*appRelayWindow
	running map[string]int
	// waiting are the requests of each app waiting for their turn, in the
	// order they came.
	waiting map[string][]appEndpointWaiter
	tickets uint64
}

type appEndpointWaiter struct {
	ticket uint64
	person string
}

var appEndpointGates = &appEndpointGateState{windows: map[string]*appRelayWindow{}, running: map[string]int{},
	waiting: map[string][]appEndpointWaiter{}}

// admit counts a request against the person's minute and the app's
// requests under way — of which one person holds at most half, so slow
// requests of one can't shut the others out: 0 when it may go on, with
// release to call after, or the status refusing it. A request turned away
// for now holds a ticket, and waiting requests go in the order they came:
// the first that may go goes (2026-10-18: requests that looked again every
// 50 ms were overtaken by new ones until their wait ran out).
func (gate *appEndpointGateState) admit(installID, personID string, limits appEndpointLimitsWire, now time.Time, ticket *uint64) (func(), int) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	key := installID + "\x00" + personID
	window := gate.windows[key]
	if window == nil || now.Sub(window.started) >= time.Minute {
		window = &appRelayWindow{started: now}
		gate.windows[key] = window
	}
	if window.count >= limits.PerMinute {
		gate.leaveLocked(installID, ticket)
		return nil, http.StatusTooManyRequests
	}
	free := func(person string) bool {
		return gate.running[installID] < limits.Concurrent && gate.running[installID+"\x00"+person] < max(1, limits.Concurrent/2)
	}
	ahead := false
	for _, waiter := range gate.waiting[installID] {
		if waiter.ticket == *ticket {
			break
		}
		if free(waiter.person) {
			ahead = true
			break
		}
	}
	if ahead || !free(personID) {
		if *ticket == 0 {
			gate.tickets++
			*ticket = gate.tickets
			gate.waiting[installID] = append(gate.waiting[installID], appEndpointWaiter{ticket: *ticket, person: personID})
		}
		return nil, http.StatusServiceUnavailable
	}
	gate.leaveLocked(installID, ticket)
	window.count++
	gate.running[installID]++
	gate.running[key]++
	if len(gate.windows) > 10000 {
		for name, stale := range gate.windows {
			if now.Sub(stale.started) >= time.Minute {
				delete(gate.windows, name)
			}
		}
	}
	return func() {
		gate.mu.Lock()
		for _, held := range []string{installID, key} {
			gate.running[held]--
			if gate.running[held] <= 0 {
				delete(gate.running, held)
			}
		}
		gate.mu.Unlock()
	}, 0
}

// leave gives up a request's place among those waiting, if it has one.
func (gate *appEndpointGateState) leave(installID string, ticket *uint64) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.leaveLocked(installID, ticket)
}

func (gate *appEndpointGateState) leaveLocked(installID string, ticket *uint64) {
	if *ticket == 0 {
		return
	}
	waiting := gate.waiting[installID]
	for index, waiter := range waiting {
		if waiter.ticket == *ticket {
			waiting = append(waiting[:index], waiting[index+1:]...)
			break
		}
	}
	if len(waiting) == 0 {
		delete(gate.waiting, installID)
	} else {
		gate.waiting[installID] = waiting
	}
	*ticket = 0
}

type appEndpointCount struct {
	requests, refused, received, sent int64
}

var appEndpointCounts = struct {
	sync.Mutex
	hours map[string]map[int64]*appEndpointCount
}{hours: map[string]map[int64]*appEndpointCount{}}

// countAppEndpoint adds to the app's count for the hour.
func countAppEndpoint(installID string, now time.Time, change appEndpointCount) {
	hour := now.Truncate(time.Hour).UnixMilli()
	appEndpointCounts.Lock()
	defer appEndpointCounts.Unlock()
	if appEndpointCounts.hours[installID] == nil {
		appEndpointCounts.hours[installID] = map[int64]*appEndpointCount{}
	}
	count := appEndpointCounts.hours[installID][hour]
	if count == nil {
		count = &appEndpointCount{}
		appEndpointCounts.hours[installID][hour] = count
	}
	count.requests += change.requests
	count.refused += change.refused
	count.received += change.received
	count.sent += change.sent
}

// saveAppEndpointUsage writes the counts kept in memory, with the app's
// other usage each minute.
func saveAppEndpointUsage(app core.App, now time.Time) error {
	appEndpointCounts.Lock()
	pending := appEndpointCounts.hours
	appEndpointCounts.hours = map[string]map[int64]*appEndpointCount{}
	appEndpointCounts.Unlock()
	for installID, hours := range pending {
		for hour, count := range hours {
			if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_app_endpoint_usage}}
					([[install_id]], [[hour]], [[requests]], [[refused]], [[received_bytes]], [[sent_bytes]])
				SELECT {:install}, {:hour}, {:requests}, {:refused}, {:received}, {:sent}
				WHERE EXISTS (SELECT 1 FROM {{leafen_app_installs}} WHERE [[id]] = {:install})
				ON CONFLICT ([[install_id]], [[hour]]) DO UPDATE SET [[requests]] = [[requests]] + excluded.[[requests]],
					[[refused]] = [[refused]] + excluded.[[refused]], [[received_bytes]] = [[received_bytes]] + excluded.[[received_bytes]],
					[[sent_bytes]] = [[sent_bytes]] + excluded.[[sent_bytes]]`).Bind(dbx.Params{"install": installID, "hour": hour,
				"requests": count.requests, "refused": count.refused, "received": count.received, "sent": count.sent}).Execute(); err != nil {
				return err
			}
		}
	}
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_app_endpoint_usage}} WHERE [[hour]] < {:before}`).
		Bind(dbx.Params{"before": now.Add(-30 * 24 * time.Hour).UnixMilli()}).Execute()
	return err
}

// recordAppEndpointFile keeps that the app's endpoint kept a file for a
// person, counted against what it may keep for them, for as long as the
// file is there: its row goes with the file (migration 781's trigger).
func recordAppEndpointFile(app core.App, installID, userID, fileID string) {
	if userID == "" {
		return
	}
	if _, err := app.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_app_endpoint_files}} ([[file_id]], [[install_id]], [[user_id]], [[created_at]])
		SELECT {:file}, {:install}, {:user}, {:now} WHERE EXISTS (SELECT 1 FROM {{leafen_files}} WHERE [[id]] = {:file})`).Bind(dbx.Params{"file": fileID, "install": installID, "user": userID,
		"now": time.Now().UnixMilli()}).Execute(); err != nil {
		app.Logger().Warn("leafen app endpoint file record failed", "install", installID, "error", err)
	}
}

// appEndpointStorageLeft reports whether a person's files through the app
// may grow by adding bytes.
func appEndpointStorageLeft(app core.App, installID, userID string, limits appEndpointLimitsWire, adding int64) (bool, error) {
	if limits.PersonStorageMB == 0 || userID == "" {
		return true, nil
	}
	stored, err := appEndpointStoredBytes(app, installID, userID)
	return stored+adding <= int64(limits.PersonStorageMB)*appEndpointMiB, err
}

// loadAppEndpointUsage is the last day's endpoint counts, what they keep,
// and the limits, for the administrator's usage window.
func loadAppEndpointUsage(app core.App, install storedAppInstall, manifest appManifest, since time.Time) (*appEndpointUsageWire, error) {
	installID := install.ID
	usage := &appEndpointUsageWire{Limits: loadAppEndpointLimits(app, installID),
		Defaults:  appEndpointLimitsWire{PerMinute: appEndpointDefaultPerMinute, Concurrent: appEndpointDefaultConcurrent},
		Endpoints: []appEndpointInfoWire{}}
	// Addresses are paths here; the route puts the server's address before them.
	for _, endpoint := range manifest.Endpoints {
		usage.Endpoints = append(usage.Endpoints, appEndpointInfoWire{Key: endpoint.Key, Name: endpoint.Name,
			Address: appEndpointAddress("", install.Slug, endpoint.Key), Public: endpoint.Public, Open: appEndpointOpened(app, install, endpoint)})
	}
	if err := app.DB().NewQuery(`SELECT COALESCE(SUM([[requests]]), 0), COALESCE(SUM([[refused]]), 0), COALESCE(SUM([[received_bytes]]), 0),
			COALESCE(SUM([[sent_bytes]]), 0) FROM {{leafen_app_endpoint_usage}} WHERE [[install_id]] = {:install} AND [[hour]] >= {:since}`).
		Bind(dbx.Params{"install": installID, "since": since.UnixMilli()}).
		Row(&usage.Requests, &usage.Refused, &usage.ReceivedBytes, &usage.SentBytes); err != nil {
		return nil, err
	}
	// The minute not yet saved counts too.
	appEndpointCounts.Lock()
	for hour, count := range appEndpointCounts.hours[installID] {
		if hour >= since.UnixMilli() {
			usage.Requests += count.requests
			usage.Refused += count.refused
			usage.ReceivedBytes += count.received
			usage.SentBytes += count.sent
		}
	}
	appEndpointCounts.Unlock()
	stored, err := appEndpointStoredBytes(app, installID, "")
	usage.StoredBytes = stored
	return usage, err
}

func registerAppEndpointLimitRoutes(event *core.ServeEvent) {
	event.Router.PUT(apiRoot+appPlatformEndpointLimitsSetPath, setAppEndpointLimitsRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
	event.Router.PUT(apiRoot+appPlatformEndpointPublicSetPath, setAppEndpointPublicRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
}

// setAppEndpointLimitsRoute is an administrator setting the app's endpoint
// limits; the defaults' values clear them.
func setAppEndpointLimitsRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	input := appEndpointLimitsWire{}
	if event.BindBody(&input) != nil || input.PerMinute < 1 || input.PerMinute > appEndpointMaxPerMinute ||
		input.Concurrent < 1 || input.Concurrent > appEndpointMaxConcurrent ||
		input.PersonStorageMB < 0 || input.PersonStorageMB > appEndpointMaxPersonStorageMB || len(manifest.Endpoints) == 0 {
		return apiError(event, http.StatusBadRequest, errorCodeAppEndpointLimitsInvalid,
			"Requests a minute 1–60000, at once 1–64, and storage for each person in MB (0 for none of the app's own).")
	}
	actorID := ""
	if actor, ok := authenticatedUser(event); ok {
		actorID = actor.Id
	}
	err = event.App.RunInTransaction(func(tx core.App) error {
		var err error
		if input.PerMinute == appEndpointDefaultPerMinute && input.Concurrent == appEndpointDefaultConcurrent && input.PersonStorageMB == 0 {
			_, err = tx.DB().Delete("leafen_app_endpoint_limits", dbx.HashExp{"install_id": install.ID}).Execute()
		} else {
			_, err = tx.DB().NewQuery(`INSERT INTO {{leafen_app_endpoint_limits}}
				([[install_id]], [[per_minute]], [[concurrent]], [[person_storage_mb]], [[updated_by]], [[updated_at]])
			VALUES ({:install}, {:perMinute}, {:concurrent}, {:storage}, {:by}, {:now})
			ON CONFLICT ([[install_id]]) DO UPDATE SET [[per_minute]] = excluded.[[per_minute]], [[concurrent]] = excluded.[[concurrent]],
				[[person_storage_mb]] = excluded.[[person_storage_mb]], [[updated_by]] = excluded.[[updated_by]],
				[[updated_at]] = excluded.[[updated_at]]`).Bind(dbx.Params{"install": install.ID, "perMinute": input.PerMinute,
				"concurrent": input.Concurrent, "storage": input.PersonStorageMB, "by": actorID, "now": time.Now().UnixMilli()}).Execute()
		}
		if err != nil {
			return err
		}
		return recordAppAudit(tx, install, "endpoint-limits", actorID, map[string]any{
			"perMinute": input.PerMinute, "concurrent": input.Concurrent, "personStorageMb": input.PersonStorageMB,
		})
	})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return appEndpointUsageAnswer(event, install, manifest)
}

// appEndpointUsageAnswer answers the endpoints' usage after a change, with
// the server's address before their paths.
func appEndpointUsageAnswer(event *core.RequestEvent, install storedAppInstall, manifest appManifest) error {
	usage, err := loadAppEndpointUsage(event.App, install, manifest, time.Now().Truncate(time.Hour).Add(-23*time.Hour))
	if err != nil {
		return appPlatformFailure(event, err)
	}
	appEndpointUsageAddresses(event, usage)
	return event.JSON(http.StatusOK, usage)
}

// appEndpointUsageAddresses puts the server's public address before the
// endpoints' paths.
func appEndpointUsageAddresses(event *core.RequestEvent, usage *appEndpointUsageWire) {
	origin, err := outwardServerOrigin(event.Request)
	if err != nil || usage == nil {
		return
	}
	for index := range usage.Endpoints {
		usage.Endpoints[index].Address = origin + usage.Endpoints[index].Address
	}
}

// setAppEndpointPublicRoute is an administrator opening a public endpoint
// to requests without signing in, or closing it; an opening holds for the
// app's permissions as they are now.
func setAppEndpointPublicRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	input := appEndpointPublicInput{}
	declared := false
	if event.BindBody(&input) == nil {
		for _, endpoint := range manifest.Endpoints {
			declared = declared || endpoint.Key == input.Key && endpoint.Public
		}
	}
	if !declared {
		return apiError(event, http.StatusBadRequest, errorCodeAppEndpointLimitsInvalid, "The app declares no public endpoint by that key.")
	}
	actorID := ""
	if actor, ok := authenticatedUser(event); ok {
		actorID = actor.Id
	}
	// A key's call asks its person once the app declares the endpoint public
	// (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	err = event.App.RunInTransaction(func(tx core.App) error {
		var err error
		if input.Open {
			_, err = tx.DB().NewQuery(`INSERT INTO {{leafen_app_endpoint_public}}
				([[install_id]], [[endpoint_key]], [[permissions_hash]], [[allowed_by]], [[allowed_at]])
			VALUES ({:install}, {:key}, {:hash}, {:by}, {:now})
			ON CONFLICT ([[install_id]], [[endpoint_key]]) DO UPDATE SET [[permissions_hash]] = excluded.[[permissions_hash]],
				[[allowed_by]] = excluded.[[allowed_by]], [[allowed_at]] = excluded.[[allowed_at]]`).Bind(dbx.Params{
				"install": install.ID, "key": input.Key, "hash": install.PermissionsHash, "by": actorID, "now": time.Now().UnixMilli(),
			}).Execute()
		} else {
			_, err = tx.DB().Delete("leafen_app_endpoint_public", dbx.HashExp{"install_id": install.ID, "endpoint_key": input.Key}).Execute()
		}
		if err != nil {
			return err
		}
		return recordAppAudit(tx, install, "endpoint-public", actorID, map[string]any{"endpoint": input.Key, "open": input.Open})
	})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return appEndpointUsageAnswer(event, install, manifest)
}
