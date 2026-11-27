//go:build !leafen_noapps

package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The person's store for an app's screen (schema_baseline.json):
// get, set, remove and keys through the app's token, in place of the
// browser storage a frame without an origin can't have — up to 200 keys of
// 100 characters, each value JSON of up to 16 KB, 256 KB in all, for each
// app, organization and person.

var (
	errAppStorageRefused = errors.New(errorCodeAppStorageRefused)
	errAppStorageFull    = errors.New(errorCodeAppStorageFull)
)

func registerAppStorageRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+appPlatformStoragePath, appStorageRoute)
	// The same store from a worker running for the person (an action or a
	// per-person schedule), which doesn't know its install's id.
	event.Router.POST(apiRoot+appPlatformOwnStoragePath, appStorageRoute)
}

func appStorageRoute(event *core.RequestEvent) error {
	authority, keyed := userKeyOf(event)
	var installID, personID, tenancy string
	if keyed && authority.AppInstallID == "" && event.Request.PathValue("installId") != "" {
		// The person's own key — their agent — reaches their store for an
		// app they may use, as its screen does (agent full-permission
		// decision of 2026-10-16, 6). A person in several organizations
		// names one with ?tenancy=.
		usable, ok, failure := appStorageUsableForPerson(event, authority)
		if !ok {
			return failure
		}
		installID, personID, tenancy = usable.Install.ID, authority.OwnerID, usable.TenancyKey
	} else {
		install, _, owner, ok, failure := appTokenInstall(event)
		if !ok {
			return failure
		}
		if authority.AppAccount {
			return apiError(event, http.StatusForbidden, errorCodeAppStorageRefused, "A store belongs to a person; the app's own account has none.")
		}
		installID, personID, tenancy = install.ID, owner, authority.TenancyKey
	}
	request := appStorageRequest{}
	raw, err := io.ReadAll(io.LimitReader(event.Request.Body, appPlatformInputMaxBytes+1))
	reason := readBodyRefusalReason(raw, err, appPlatformInputMaxBytes)
	if reason == "" {
		if err := json.Unmarshal(raw, &request); err != nil {
			reason = bodyRefusalReason("storage command", err)
		}
	}
	if reason != "" {
		return apiError(event, http.StatusBadRequest, errorCodeAppStorageRefused, refusalMessage("Send a storage command up to 64 KB.", reason))
	}
	// A key that only reads doesn't change the store.
	if keyed && authority.AppInstallID == "" && (request.Command == "set" || request.Command == "remove") {
		if scope, err := userKeyEffectiveScope(event.App, authority, nil); err != nil {
			return appPlatformFailure(event, err)
		} else if scope < userKeyScopeContent {
			return apiError(event, http.StatusForbidden, errorCodeUserKeyScope, "This key's scope is "+userKeyScopeNames[scope]+"; changing the store needs content.")
		}
	}
	result, err := runAppStorage(event.App, installID, tenancy, personID, request, time.Now())
	switch {
	case err == nil:
		return event.JSON(http.StatusOK, result)
	case errors.Is(err, errAppStorageFull):
		return apiError(event, http.StatusInsufficientStorage, errorCodeAppStorageFull, "The person's storage for this app is full.")
	case errors.Is(err, errAppStorageRefused):
		return apiError(event, http.StatusBadRequest, errorCodeAppStorageRefused, strings.TrimPrefix(err.Error(), errorCodeAppStorageRefused+": "))
	}
	return appPlatformFailure(event, err)
}

// appStorageUsableForPerson is the app of the path as the key's person may
// use it, in the organization ?tenancy= names or the only one they may use
// it in.
func appStorageUsableForPerson(event *core.RequestEvent, authority userKeyAuthority) (usableApp, bool, error) {
	apps, err := appAgentReachableApps(event.App, authority.OwnerID, authority.WorkspaceIDs)
	if err != nil {
		return usableApp{}, false, appPlatformFailure(event, err)
	}
	installID, tenancy := event.Request.PathValue("installId"), strings.TrimSpace(event.Request.URL.Query().Get("tenancy"))
	found := []usableApp{}
	for _, usable := range apps {
		if usable.Install.ID == installID && (tenancy == "" || usable.TenancyKey == tenancy) {
			found = append(found, usable)
		}
	}
	switch {
	case len(found) == 0:
		return usableApp{}, false, apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "App not found.")
	case len(found) > 1:
		return usableApp{}, false, apiError(event, http.StatusBadRequest, errorCodeAppStorageRefused, "You use this app in more than one organization; name one with ?tenancy=.")
	}
	return found[0], true, nil
}

func appStorageRefused(message string) error {
	return fmt.Errorf("%w: %s", errAppStorageRefused, message)
}

// appStorageKeyValid takes a key of printable characters up to the limit.
func appStorageKeyValid(key string) bool {
	if key == "" || utf8.RuneCountInString(key) > appStorageKeyMaxRunes || !utf8.ValidString(key) {
		return false
	}
	for _, r := range key {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func runAppStorage(app core.App, installID, tenancy, personID string, request appStorageRequest, now time.Time) (appStorageResult, error) {
	scope := dbx.Params{"install": installID, "tenancy": tenancy, "person": personID}
	used := func() (int, error) {
		var total int
		err := app.DB().NewQuery(`SELECT COALESCE(SUM([[bytes]]), 0) FROM {{leafen_app_person_storage}}
			WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy} AND [[person_id]] = {:person}`).Bind(scope).Row(&total)
		return total, err
	}
	if request.Command != "keys" && !appStorageKeyValid(request.Key) {
		return appStorageResult{}, appStorageRefused("A key is 1 to 100 printable characters.")
	}
	with := func(extra dbx.Params) dbx.Params {
		params := dbx.Params{"key": request.Key}
		for name, value := range scope {
			params[name] = value
		}
		for name, value := range extra {
			params[name] = value
		}
		return params
	}
	result := appStorageResult{}
	switch request.Command {
	case "get":
		var stored string
		err := app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_app_person_storage}} WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy}
			AND [[person_id]] = {:person} AND [[key]] = {:key}`).Bind(with(nil)).Row(&stored)
		if err == nil {
			result.Value = json.RawMessage(stored)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return appStorageResult{}, err
		}
	case "set":
		encoded, err := json.Marshal(request.Value)
		if err != nil || request.Value == nil {
			return appStorageResult{}, appStorageRefused("A value is any JSON but null; remove a key instead.")
		}
		if len(encoded) > appStorageValueMaxBytes {
			return appStorageResult{}, appStorageRefused("A value is up to 16 KB of JSON.")
		}
		bytes := len(request.Key) + len(encoded)
		err = app.RunInTransaction(func(tx core.App) error {
			var keys, total, previous int
			if err := tx.DB().NewQuery(`SELECT COUNT(*), COALESCE(SUM([[bytes]]), 0),
				COALESCE(SUM(CASE WHEN [[key]] = {:key} THEN [[bytes]] ELSE 0 END), 0) FROM {{leafen_app_person_storage}}
				WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy} AND [[person_id]] = {:person}`).Bind(with(nil)).Row(&keys, &total, &previous); err != nil {
				return err
			}
			if previous == 0 && keys >= appStorageMaxKeys || total-previous+bytes > appStorageMaxBytes {
				return errAppStorageFull
			}
			_, err := tx.DB().NewQuery(`INSERT INTO {{leafen_app_person_storage}} ([[install_id]], [[tenancy_key]], [[person_id]], [[key]], [[value]], [[bytes]], [[updated_at]])
				VALUES ({:install}, {:tenancy}, {:person}, {:key}, {:value}, {:bytes}, {:now})
				ON CONFLICT ([[install_id]], [[tenancy_key]], [[person_id]], [[key]]) DO UPDATE SET [[value]] = excluded.[[value]],
					[[bytes]] = excluded.[[bytes]], [[updated_at]] = excluded.[[updated_at]]`).
				Bind(with(dbx.Params{"value": string(encoded), "bytes": bytes, "now": now.UnixMilli()})).Execute()
			return err
		})
		if err != nil {
			return appStorageResult{}, err
		}
	case "remove":
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_app_person_storage}} WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy}
			AND [[person_id]] = {:person} AND [[key]] = {:key}`).Bind(with(nil)).Execute(); err != nil {
			return appStorageResult{}, err
		}
	case "keys":
		result.Keys = []string{}
		if err := app.DB().NewQuery(`SELECT [[key]] FROM {{leafen_app_person_storage}} WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy}
			AND [[person_id]] = {:person} ORDER BY [[key]]`).Bind(scope).Column(&result.Keys); err != nil {
			return appStorageResult{}, err
		}
	default:
		return appStorageResult{}, appStorageRefused("The command is get, set, remove or keys.")
	}
	total, err := used()
	result.Used = total
	return result, err
}
