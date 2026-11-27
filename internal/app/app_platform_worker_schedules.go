//go:build !leafen_noapps

package app

import (
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/cron"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Schedules (docs/app-platform.md 11): each minute, a schedule whose turn it
// is in its time zone goes into the queue once for that minute — for each
// organization the app has a workspace in, or, for a per-person schedule,
// for each person still granted the app who has used it there and hasn't
// turned it off. A schedule runs in the zone its administrator chose, else
// the one the app declares, else each person's own for a per-person one,
// else the server's. A stopped app or a schedule its administrator turned
// off is left out, and minutes the server was down aren't made up.

// queueDueAppSchedules adds the schedules due at the given minute.
func queueDueAppSchedules(app core.App, now time.Time) error {
	minute := now.Truncate(time.Minute)
	installs, err := loadAppInstalls(app, "[[status]] = 'active' AND [[removed_at]] = 0", dbx.Params{})
	if err != nil {
		return err
	}
	for _, install := range installs {
		_, manifest, err := loadAppPackage(app, install.PackageHash)
		if err != nil || len(manifest.Schedules) == 0 {
			continue
		}
		workspaces, err := loadAppWorkspaces(app, install.ID)
		if err != nil {
			return err
		}
		for _, schedule := range manifest.Schedules {
			parsed, err := cron.NewSchedule(schedule.Cron)
			if err != nil {
				continue
			}
			state := loadAppScheduleState(app, install.ID, schedule.Key)
			if !state.enabled {
				continue
			}
			due := func(location *time.Location) bool { return parsed.IsDue(cron.NewMoment(minute.In(location))) }
			location, eachPerson := appScheduleLocation(schedule, state.zone)
			if !eachPerson && !due(location) {
				continue
			}
			optedOut, err := appScheduleOptOuts(app, install.ID, schedule.Key)
			if err != nil {
				return err
			}
			queued := false
			for _, workspace := range workspaces {
				people := []string{""}
				if schedule.PerPerson {
					if people, err = appScheduledPeople(app, install.ID, workspace.TenancyKey); err != nil {
						return err
					}
				}
				for _, personID := range people {
					if optedOut[personID] || eachPerson && !due(appPersonLocation(app, personID)) {
						continue
					}
					delivery := "schedule:" + schedule.Key + ":" + workspace.TenancyKey + ":" + personID + ":" + minute.Format(time.RFC3339)
					if _, err := app.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_app_hook_events}}
						([[id]], [[install_id]], [[hook]], [[delivery]], [[content_type]], [[body]], [[status]], [[received_at]], [[kind]], [[topic]], [[person_id]])
						VALUES ({:id}, {:install}, {:key}, {:delivery}, '', x'', 'waiting', {:at}, 'schedule', {:tenancy}, {:person})`).Bind(dbx.Params{
						"id": security.RandomString(20), "install": install.ID, "key": schedule.Key, "delivery": delivery,
						"at": minute.UnixMilli(), "tenancy": workspace.TenancyKey, "person": personID,
					}).Execute(); err != nil {
						return err
					}
					queued = true
				}
			}
			if eachPerson && !queued {
				continue
			}
			if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_app_schedule_states}} ([[install_id]], [[schedule]], [[last_queued_at]])
				VALUES ({:install}, {:key}, {:at}) ON CONFLICT ([[install_id]], [[schedule]]) DO UPDATE SET [[last_queued_at]] = excluded.[[last_queued_at]]`).
				Bind(dbx.Params{"install": install.ID, "key": schedule.Key, "at": minute.UnixMilli()}).Execute(); err != nil {
				return err
			}
		}
	}
	return nil
}

// appScheduledPeople are those who used the app in the organization and are
// still granted it.
func appScheduledPeople(app core.App, installID, tenancyKey string) ([]string, error) {
	users := []string{}
	if err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_app_connections}} WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy}
		ORDER BY [[user_id]]`).Bind(dbx.Params{"install": installID, "tenancy": tenancyKey}).Column(&users); err != nil {
		return nil, err
	}
	people := []string{}
	for _, userID := range users {
		granted, err := appGranted(app, installID, tenancyKey, userID)
		if err != nil {
			return nil, err
		}
		if active, err := accountMayAct(app, userID); err == nil && active && granted {
			people = append(people, userID)
		}
	}
	return people, nil
}

// appPersonAuthority is what the person's own app connection reaches in the
// organization — the same as their app token, without one. An opened-file
// app's reaches nothing without a file the person opened with it.
func appPersonAuthority(app core.App, install storedAppInstall, manifest appManifest, tenancyKey, userID string) (*userKeyAuthority, error) {
	if manifest.PersonAccess == appOpenedFileAccess {
		return nil, errors.New("the app reaches a person's data only on a file they open with it")
	}
	stored := struct {
		ConnectionID string `db:"connection_id"`
		Name         string `db:"client_name"`
		Scope        string `db:"access_scope"`
		Created      int64  `db:"created"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[link]].[[connection_id]], [[connection]].[[client_name]], [[connection]].[[access_scope]], [[connection]].[[created]]
		FROM {{leafen_app_connections}} [[link]]
		JOIN {{leafen_mcp_connections}} [[connection]] ON [[connection]].[[id]] = [[link]].[[connection_id]] AND [[connection]].[[revoked_at]] = 0
		WHERE [[link]].[[install_id]] = {:install} AND [[link]].[[tenancy_key]] = {:tenancy} AND [[link]].[[user_id]] = {:user}`).
		Bind(dbx.Params{"install": install.ID, "tenancy": tenancyKey, "user": userID}).One(&stored); err != nil {
		return nil, errors.New("the person has no connection to the app here")
	}
	scope, known := parseUserKeyScope(stored.Scope)
	granted, err := appGranted(app, install.ID, tenancyKey, userID)
	if err != nil || !known || !granted {
		return nil, errors.New("the person is no longer granted the app")
	}
	workspaces, err := appPersonWorkspaces(app, userID, tenancyKey)
	if err != nil || len(workspaces) == 0 {
		return nil, errors.New("the person has no workspace in the organization")
	}
	return &userKeyAuthority{ConnectionID: stored.ConnectionID, Name: stored.Name, OwnerID: userID, Scope: scope, WorkspaceIDs: workspaces,
		Created: stored.Created, AppInstallID: install.ID, TenancyKey: tenancyKey}, nil
}

// loadAppJobs is what the administrator sees of an app's work: its
// schedules, its data events, what waits or failed, and its recent runs.
func loadAppJobs(app core.App, install storedAppInstall, manifest appManifest) (appJobsWire, error) {
	jobs := appJobsWire{Schedules: []appScheduleWire{}, Events: []appEventWire{}, Runs: []appRunWire{}, LongRuns: appLongRunStates(install.ID)}
	jobs.ServerTimeZone = time.Now().Format("UTC-07:00")
	for _, schedule := range manifest.Schedules {
		state := loadAppScheduleState(app, install.ID, schedule.Key)
		wire := appScheduleWire{Key: schedule.Key, Cron: schedule.Cron, PerPerson: schedule.PerPerson, Enabled: state.enabled,
			LastQueuedAt: state.lastQueued, Name: appTextOrEmpty(schedule.Name), TimeZone: state.zone, DeclaredTimeZone: schedule.TimeZone}
		if schedule.PerPerson {
			if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_schedule_optouts}} WHERE [[install_id]] = {:install} AND [[schedule]] = {:key}`).
				Bind(dbx.Params{"install": install.ID, "key": schedule.Key}).Row(&wire.OptedOut); err != nil {
				return jobs, err
			}
		}
		jobs.Schedules = append(jobs.Schedules, wire)
	}
	for _, event := range manifest.Events {
		// What it watches, as the administrator reads it, not only its key
		// (2026-11-06: the jobs window showed "done" under the trigger).
		wire := appEventWire{Key: event.Key, Types: event.Types, Properties: event.Properties}
		for _, condition := range event.Where {
			wire.Where = append(wire.Where, appEventConditionWire{Property: condition.Property, Equals: condition.Equals, NotEquals: condition.NotEquals, Empty: condition.Empty})
		}
		jobs.Events = append(jobs.Events, wire)
	}
	if err := app.DB().NewQuery(`SELECT COUNT(*) FILTER (WHERE [[status]] IN ('waiting', 'running')), COUNT(*) FILTER (WHERE [[status]] = 'failed')
		FROM {{leafen_app_hook_events}} WHERE [[install_id]] = {:install}`).Bind(dbx.Params{"install": install.ID}).Row(&jobs.Waiting, &jobs.Failed); err != nil {
		return jobs, err
	}
	var err error
	jobs.Runs, jobs.RunsNextCursor, err = loadAppRuns(app, install, manifest, timeIDCursor{})
	return jobs, err
}

// loadAppRuns is a window of the app's runs, newest first, from after on. A
// run is named by the action or schedule it ran, not its function; a key the
// app doesn't declare (a run recorded before runs kept their key) is left out.
func loadAppRuns(app core.App, install storedAppInstall, manifest appManifest, after timeIDCursor) ([]appRunWire, string, error) {
	runs := []appRunWire{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[trigger]], [[subject]], [[function]], [[trigger_ref]] AS [[ref]], [[status]], [[error]], [[duration_ms]], [[created]]
		FROM {{leafen_app_runs}} WHERE [[install_id]] = {:install}
			AND ({:afterId} = '' OR [[created]] < {:afterAt} OR ([[created]] = {:afterAt} AND [[id]] > {:afterId}))
		ORDER BY [[created]] DESC, [[id]] LIMIT {:limit}`).
		Bind(dbx.Params{"install": install.ID, "afterAt": after.At, "afterId": after.ID, "limit": appRunsPerWindow + 1}).All(&runs); err != nil {
		return nil, "", err
	}
	runs, next := appWindowOf(runs, appRunsPerWindow, func(run appRunWire) (int64, string) { return run.Created, run.ID })
	for index, run := range runs {
		known := true
		switch run.Trigger {
		case "action", "tool", appQueueAutomation:
			action, found := appManifestActionByKey(manifest, run.Ref)
			known, runs[index].Name = found, appTextOrEmpty(action.Name)
		case appQueueSchedule:
			schedule, found := appManifestScheduleByKey(manifest, run.Ref)
			known, runs[index].Name = found, appTextOrEmpty(schedule.Name)
		case appQueueEvent:
			_, known = appManifestEventByKey(manifest, run.Ref)
		case appQueueHook:
			_, known = appManifestHook(manifest, run.Ref)
		}
		if !known {
			runs[index].Ref, runs[index].Name = "", nil
		}
	}
	return runs, next, nil
}

// setAppSchedule turns a schedule off or on and sets the time zone it runs
// in: "" for the app's own choice, "server", or an IANA zone.
func setAppSchedule(app core.App, install storedAppInstall, manifest appManifest, key string, input appScheduleInput, actorID string) error {
	if _, found := appManifestScheduleByKey(manifest, key); !found {
		return refuse(errAppInputInvalid, "the app declares no schedule named %q", key)
	}
	state := loadAppScheduleState(app, install.ID, key)
	if input.Enabled != nil {
		state.enabled = *input.Enabled
	}
	if input.TimeZone != nil {
		if zone := *input.TimeZone; zone != "" && zone != appScheduleServerZone && !appTimeZoneValid(zone) {
			return refuse(errAppInputInvalid, "timeZone is empty for the app's own choice, %s, or a time zone such as Asia/Seoul, not %q", appScheduleServerZone, zone)
		}
		state.zone = *input.TimeZone
	}
	value := 0
	if state.enabled {
		value = 1
	}
	return app.RunInTransaction(func(tx core.App) error {
		if _, err := tx.DB().NewQuery(`INSERT INTO {{leafen_app_schedule_states}} ([[install_id]], [[schedule]], [[enabled]], [[time_zone]], [[updated_by]], [[updated_at]])
			VALUES ({:install}, {:key}, {:enabled}, {:zone}, {:actor}, {:now}) ON CONFLICT ([[install_id]], [[schedule]]) DO UPDATE SET
			[[enabled]] = excluded.[[enabled]], [[time_zone]] = excluded.[[time_zone]], [[updated_by]] = excluded.[[updated_by]], [[updated_at]] = excluded.[[updated_at]]`).Bind(dbx.Params{
			"install": install.ID, "key": key, "enabled": value, "zone": state.zone, "actor": actorID, "now": time.Now().UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		return recordAppAudit(tx, install, "schedule", actorID, map[string]any{"schedule": key, "enabled": state.enabled, "timeZone": state.zone})
	})
}
