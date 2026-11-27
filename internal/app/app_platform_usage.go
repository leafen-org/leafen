//go:build !leafen_noapps

package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What an app used (docs/app-platform.md 15): the last day's calls to
// Leafen by hour, worker runs, relayed traffic, the temporary store and
// who is in its rooms now, beside its limits; and its log for export with
// nothing of anyone's data — no run logs, inputs, outputs or error texts,
// only what ran, when, how long and how it ended.

const appLogExportDays = 30

func registerAppUsageRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPlatformUsagePath, appUsageRoute)
	event.Router.GET(apiRoot+appPlatformLogExportPath, appLogExportRoute)
}

func appUsageRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	usage, err := loadAppUsage(event.App, install, manifest, time.Now())
	if err != nil {
		return appPlatformFailure(event, err)
	}
	appEndpointUsageAddresses(event, usage.Endpoints)
	return event.JSON(http.StatusOK, usage)
}

func loadAppUsage(app core.App, install storedAppInstall, manifest appManifest, now time.Time) (appUsageWire, error) {
	first := now.Truncate(time.Hour).Add(-23 * time.Hour)
	usage := appUsageWire{Since: first.UnixMilli(), Hours: []appUsageHourWire{}, Stores: []appUsageStoreWire{},
		CallsPerMinute: appBudgetCallsPerMinute, WritesPerMinute: appBudgetWritesPerMinute,
		WorkerMemoryMB: int(appWorkerLimits.memoryPages / 16), WorkerSeconds: int(appRunTimeout / time.Second)}
	hours := map[int64]*appUsageHourWire{}
	for hour := first; !hour.After(now); hour = hour.Add(time.Hour) {
		usage.Hours = append(usage.Hours, appUsageHourWire{Hour: hour.UnixMilli()})
	}
	for index := range usage.Hours {
		hours[usage.Hours[index].Hour] = &usage.Hours[index]
	}
	stored := []struct {
		Hour    int64 `db:"hour"`
		Reads   int64 `db:"reads"`
		Writes  int64 `db:"writes"`
		Delayed int64 `db:"delayed"`
		Refused int64 `db:"refused"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[hour]], [[reads]], [[writes]], [[delayed]], [[refused]] FROM {{leafen_app_usage}}
		WHERE [[install_id]] = {:install} AND [[hour]] >= {:first}`).Bind(dbx.Params{"install": install.ID, "first": first.UnixMilli()}).All(&stored); err != nil {
		return usage, err
	}
	add := func(hour, reads, writes, delayed, refused int64) {
		if target := hours[hour]; target != nil {
			target.Reads, target.Writes, target.Delayed, target.Refused = target.Reads+reads, target.Writes+writes, target.Delayed+delayed, target.Refused+refused
		}
	}
	for _, row := range stored {
		add(row.Hour, row.Reads, row.Writes, row.Delayed, row.Refused)
	}
	// The running hour not saved yet.
	appBudgets.Lock()
	if state := appBudgets.apps[install.ID]; state != nil {
		for hour, count := range state.usage {
			add(hour, count.reads, count.writes, count.delayed, count.refused)
		}
	}
	appBudgets.Unlock()

	since := dbx.Params{"install": install.ID, "first": first.UnixMilli()}
	if err := app.DB().NewQuery(`SELECT COUNT(*), COALESCE(SUM([[status]] NOT IN ('ok', 'running')), 0), COALESCE(SUM([[duration_ms]]), 0), COALESCE(SUM([[host_calls]]), 0)
		FROM {{leafen_app_runs}} WHERE [[install_id]] = {:install} AND [[created]] >= {:first}`).Bind(since).
		Row(&usage.Runs, &usage.RunFailures, &usage.RunMs, &usage.HostCalls); err != nil {
		return usage, err
	}
	if err := app.DB().NewQuery(`SELECT COUNT(*), COALESCE(SUM([[outcome]] != 'ok'), 0), COALESCE(SUM([[request_bytes]]), 0), COALESCE(SUM([[response_bytes]]), 0)
		FROM {{leafen_app_relay_log}} WHERE [[install_id]] = {:install} AND [[created]] >= {:first}`).Bind(since).
		Row(&usage.RelayCalls, &usage.RelayFailures, &usage.RelaySentBytes, &usage.RelayReceivedBytes); err != nil {
		return usage, err
	}
	if err := app.DB().NewQuery(`SELECT [[paused_reason]] FROM {{leafen_app_installs}} WHERE [[id]] = {:install}`).Bind(since).Row(&usage.PausedReason); err != nil {
		return usage, err
	}

	if manifest.KV != nil {
		names := map[string]string{}
		if tenancies, err := appTenancies(app); err == nil {
			for _, tenancy := range tenancies {
				names[tenancy.Key] = tenancy.Name
			}
		}
		workspaces, err := loadAppWorkspaces(app, install.ID)
		if err != nil {
			return usage, err
		}
		for _, workspace := range workspaces {
			space := appKV.space(install.ID, workspace.TenancyKey)
			space.mu.Lock()
			size := space.size
			space.mu.Unlock()
			usage.Stores = append(usage.Stores, appUsageStoreWire{TenancyKey: workspace.TenancyKey, Name: names[workspace.TenancyKey],
				Bytes: size, MaxBytes: manifest.KV.MaxBytes})
		}
	}
	if rooms := appRoomRegistry; rooms != nil {
		rooms.mu.Lock()
		prefix := "app." + install.ID + "."
		for name, count := range rooms.peers {
			if strings.HasPrefix(name, prefix) {
				usage.RoomPeople += count
			}
		}
		for name, count := range rooms.guests {
			if strings.HasPrefix(name, prefix) {
				usage.RoomGuests += count
			}
		}
		rooms.mu.Unlock()
		usage.RoomPeople -= usage.RoomGuests
	}
	if len(manifest.Endpoints) > 0 {
		endpoints, err := loadAppEndpointUsage(app, install, manifest, first)
		if err != nil {
			return usage, err
		}
		usage.Endpoints = endpoints
	}
	return usage, nil
}

// appLogExportRoute downloads the app's last 30 days of what ran, what it
// reached and what changed about it, without anyone's data.
func appLogExportRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	install, manifest, err := adminAppInstall(event)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	now := time.Now()
	since := dbx.Params{"install": install.ID, "first": now.Add(-appLogExportDays * 24 * time.Hour).UnixMilli()}
	runs := []struct {
		Created    int64  `db:"created" json:"created"`
		Trigger    string `db:"trigger" json:"trigger"`
		Worker     string `db:"worker" json:"worker"`
		Function   string `db:"function" json:"function"`
		Status     string `db:"status" json:"status"`
		DurationMs int64  `db:"duration_ms" json:"durationMs"`
		HostCalls  int64  `db:"host_calls" json:"hostCalls"`
	}{}
	relay := []struct {
		Created       int64  `db:"created" json:"created"`
		Method        string `db:"method" json:"method"`
		Host          string `db:"host" json:"host"`
		Status        int    `db:"status" json:"status"`
		Outcome       string `db:"outcome" json:"outcome"`
		RequestBytes  int64  `db:"request_bytes" json:"requestBytes"`
		ResponseBytes int64  `db:"response_bytes" json:"responseBytes"`
		DurationMs    int64  `db:"duration_ms" json:"durationMs"`
	}{}
	usage := []struct {
		Hour    int64 `db:"hour" json:"hour"`
		Reads   int64 `db:"reads" json:"reads"`
		Writes  int64 `db:"writes" json:"writes"`
		Delayed int64 `db:"delayed" json:"delayed"`
		Refused int64 `db:"refused" json:"refused"`
	}{}
	audit := []struct {
		Created int64  `db:"created" json:"created"`
		Action  string `db:"action" json:"action"`
		Actor   string `db:"actor_id" json:"actor"`
	}{}
	for _, read := range []struct {
		query  string
		target any
	}{
		{`SELECT [[created]], [[trigger]], [[worker]], [[function]], [[status]], [[duration_ms]], [[host_calls]] FROM {{leafen_app_runs}}
			WHERE [[install_id]] = {:install} AND [[created]] >= {:first} ORDER BY [[created]]`, &runs},
		{`SELECT [[created]], [[method]], [[host]], [[status]], [[outcome]], [[request_bytes]], [[response_bytes]], [[duration_ms]]
			FROM {{leafen_app_relay_log}} WHERE [[install_id]] = {:install} AND [[created]] >= {:first} ORDER BY [[created]]`, &relay},
		{`SELECT [[hour]], [[reads]], [[writes]], [[delayed]], [[refused]] FROM {{leafen_app_usage}}
			WHERE [[install_id]] = {:install} AND [[hour]] >= {:first} ORDER BY [[hour]]`, &usage},
		{`SELECT [[created]], [[action]], [[actor_id]] FROM {{leafen_app_audit_events}}
			WHERE [[install_id]] = {:install} AND [[created]] >= {:first} ORDER BY [[id]]`, &audit},
	} {
		if err := event.App.DB().NewQuery(read.query).Bind(since).All(read.target); err != nil {
			return appPlatformFailure(event, err)
		}
	}
	encoded, err := json.MarshalIndent(map[string]any{
		"app":        map[string]string{"id": manifest.ID, "slug": manifest.Slug, "version": manifest.Version, "installId": install.ID},
		"exportedAt": now.UnixMilli(), "days": appLogExportDays, "runs": runs, "relay": relay, "apiCallsByHour": usage, "audit": audit,
	}, "", "  ")
	if err != nil {
		return appPlatformFailure(event, err)
	}
	header := event.Response.Header()
	header.Set("Content-Type", "application/json")
	// The file's day is the person's (person_clock.go).
	day := now.In(personLocation(event.App, event.Auth.Id)).Format("20060102")
	header.Set("Content-Disposition", `attachment; filename="`+manifest.Slug+`-log-`+day+`.json"`)
	header.Set("Cache-Control", "no-store")
	return event.Blob(http.StatusOK, "application/json", encoded)
}
