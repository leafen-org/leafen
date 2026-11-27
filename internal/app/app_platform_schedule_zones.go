//go:build !leafen_noapps

package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Where and for whom a schedule runs (docs/app-platform.md 11): its time
// zone, and the people who turned a per-person schedule off for themselves.

const appScheduleServerZone = "server"

type appScheduleState struct {
	enabled    bool
	zone       string
	lastQueued int64
}

func loadAppScheduleState(app core.App, installID, key string) appScheduleState {
	state, enabled := appScheduleState{enabled: true}, 1
	err := app.DB().NewQuery(`SELECT [[enabled]], [[time_zone]], [[last_queued_at]] FROM {{leafen_app_schedule_states}}
		WHERE [[install_id]] = {:install} AND [[schedule]] = {:key}`).
		Bind(dbx.Params{"install": installID, "key": key}).Row(&enabled, &state.zone, &state.lastQueued)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		// A schedule whose state can't be read waits rather than runs.
		return appScheduleState{}
	}
	state.enabled = enabled == 1
	return state
}

// appScheduleLocation is the zone a schedule runs in, or none (eachPerson)
// when each person's own zone decides.
func appScheduleLocation(schedule appManifestSchedule, chosen string) (location *time.Location, eachPerson bool) {
	for _, zone := range []string{chosen, schedule.TimeZone} {
		if zone == appScheduleServerZone {
			return time.Local, false
		}
		if zone != "" {
			if location, err := time.LoadLocation(zone); err == nil {
				return location, false
			}
		}
	}
	if schedule.PerPerson {
		return nil, true
	}
	return time.Local, false
}

// appPersonLocation is the person's own time zone, or the server's (person_clock.go).
func appPersonLocation(app core.App, userID string) *time.Location {
	return personLocation(app, userID)
}

func appScheduleOptOuts(app core.App, installID, key string) (map[string]bool, error) {
	users := []string{}
	if err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_app_schedule_optouts}} WHERE [[install_id]] = {:install} AND [[schedule]] = {:key}`).
		Bind(dbx.Params{"install": installID, "key": key}).Column(&users); err != nil {
		return nil, err
	}
	optedOut := map[string]bool{}
	for _, user := range users {
		optedOut[user] = true
	}
	return optedOut, nil
}

func registerAppPersonScheduleRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPlatformPersonSchedulesPath, listPersonAppSchedulesRoute)
	event.Router.PUT(apiRoot+appPlatformPersonScheduleSetPath, setPersonAppScheduleRoute).Bind(apis.BodyLimit(appPlatformInputMaxBytes))
}

// appSchedulePerson is the person deciding about schedules run for them —
// signed in or through their key, never an app's token; ok false when it
// answered the refusal instead.
func appSchedulePerson(event *core.RequestEvent) (string, bool, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return "", false, apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	if appTokenCall(event) {
		return "", false, apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "An app can't decide what runs.")
	}
	return user.Id, true, nil
}

// personAppSchedules are the per-person schedules of the apps the person
// uses, with whether each runs for them: the administrator's turned-off
// ones aren't listed.
func personAppSchedules(app core.App, userID string) ([]appPersonScheduleWire, error) {
	installIDs := []string{}
	if err := app.DB().NewQuery(`SELECT DISTINCT [[connection]].[[install_id]] FROM {{leafen_app_connections}} [[connection]]
		JOIN {{leafen_app_installs}} [[install]] ON [[install]].[[id]] = [[connection]].[[install_id]]
		WHERE [[connection]].[[user_id]] = {:user} AND [[install]].[[removed_at]] = 0 ORDER BY [[connection]].[[install_id]]`).
		Bind(dbx.Params{"user": userID}).Column(&installIDs); err != nil {
		return nil, err
	}
	schedules := []appPersonScheduleWire{}
	for _, installID := range installIDs {
		install, err := loadAppInstall(app, installID)
		if err != nil {
			continue
		}
		_, manifest, err := loadAppPackage(app, install.PackageHash)
		if err != nil {
			continue
		}
		for _, schedule := range manifest.Schedules {
			state := loadAppScheduleState(app, install.ID, schedule.Key)
			if !schedule.PerPerson || !state.enabled {
				continue
			}
			optedOut, err := appScheduleOptOuts(app, install.ID, schedule.Key)
			if err != nil {
				return nil, err
			}
			zone := state.zone
			if zone == "" {
				zone = schedule.TimeZone
			}
			schedules = append(schedules, appPersonScheduleWire{InstallID: install.ID, AppName: appTextOrEmpty(manifest.Name), Key: schedule.Key,
				Name: appTextOrEmpty(schedule.Name), Cron: schedule.Cron, TimeZone: zone, On: !optedOut[userID]})
		}
	}
	return schedules, nil
}

func listPersonAppSchedulesRoute(event *core.RequestEvent) error {
	userID, ok, err := appSchedulePerson(event)
	if !ok {
		return err
	}
	schedules, err := personAppSchedules(event.App, userID)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, appPersonScheduleList{Schedules: schedules})
}

// setPersonAppScheduleRoute turns a per-person schedule of an app the
// person uses off or back on, for them alone.
func setPersonAppScheduleRoute(event *core.RequestEvent) error {
	userID, ok, err := appSchedulePerson(event)
	if !ok {
		return err
	}
	input := appPersonScheduleInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return appPlatformFailure(event, bodyRefusal(errAppInputInvalid, "schedule choice", err))
	}
	installID, key := event.Request.PathValue("installId"), event.Request.PathValue("schedule")
	schedules, err := personAppSchedules(event.App, userID)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	listed := false
	for _, schedule := range schedules {
		listed = listed || schedule.InstallID == installID && schedule.Key == key
	}
	if !listed {
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "No such schedule runs for you.")
	}
	if input.On {
		_, err = event.App.DB().NewQuery(`DELETE FROM {{leafen_app_schedule_optouts}} WHERE [[install_id]] = {:install} AND [[schedule]] = {:key} AND [[user_id]] = {:user}`).
			Bind(dbx.Params{"install": installID, "key": key, "user": userID}).Execute()
	} else {
		_, err = event.App.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_app_schedule_optouts}} ([[install_id]], [[schedule]], [[user_id]], [[created]])
			VALUES ({:install}, {:key}, {:user}, {:now})`).Bind(dbx.Params{"install": installID, "key": key, "user": userID, "now": time.Now().UnixMilli()}).Execute()
	}
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if schedules, err = personAppSchedules(event.App, userID); err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, appPersonScheduleList{Schedules: schedules})
}
