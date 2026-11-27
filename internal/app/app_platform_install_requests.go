//go:build !leafen_noapps

package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// People's requests for apps under the "request" install policy
// (app_platform_install_policy.go): a person asks once while it waits, the
// administrators hear of it and see it in Settings → Apps, and approving an
// installed app lets that person use it in their organization; either way
// the person hears.

func appRequestCreateRoute(event *core.RequestEvent) error {
	user, ok := appPerson(event)
	if !ok {
		return nil
	}
	input := appRequestInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
			refusalMessage("Name the app and your organization.", bodyRefusalReason("app request", err)))
	}
	request, err := createAppRequest(event.App, user.Id, input, time.Now())
	if err != nil {
		return appPolicyFailure(event, err)
	}
	return event.JSON(http.StatusOK, request)
}

// createAppRequest asks the administrators for an app, once while it waits.
func createAppRequest(app core.App, personID string, input appRequestInput, now time.Time) (appRequestWire, error) {
	settings, err := loadAppPlatformSettings(app)
	if err != nil {
		return appRequestWire{}, err
	}
	if settings.InstallPolicy != "request" {
		return appRequestWire{}, appPolicyRefused("This server doesn't take requests for apps.")
	}
	if err := appPersonTenancy(app, personID, input.TenancyKey); err != nil {
		return appRequestWire{}, err
	}
	entry, err := appCatalogEntryFor(app, input.CatalogID, input.AppID)
	if err != nil {
		return appRequestWire{}, err
	}
	var waiting string
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_app_requests}} WHERE [[person_id]] = {:person} AND [[app_id]] = {:app}
		AND [[status]] = 'pending'`).Bind(dbx.Params{"person": personID, "app": input.AppID}).Row(&waiting); err == nil {
		return loadAppRequest(app, waiting)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return appRequestWire{}, err
	}
	id := security.RandomString(15)
	if _, err := app.DB().Insert(appRequestsTable, dbx.Params{"id": id, "catalog_id": input.CatalogID, "app_id": input.AppID, "person_id": personID,
		"tenancy_key": input.TenancyKey, "reason": appOneLine(input.Reason, appRequestReasonRunes), "status": "pending", "created": now.UnixMilli(),
	}).Execute(); err != nil {
		return appRequestWire{}, err
	}
	administrators, err := serverAdministrators(app)
	if err != nil {
		return appRequestWire{}, err
	}
	request, err := loadAppRequest(app, id)
	if err != nil {
		return appRequestWire{}, err
	}
	// The notice says who asked, and why when they said (2026-10-16: it said
	// only the reason).
	preview := request.PersonName
	if reason := strings.TrimSpace(request.Reason); reason != "" {
		preview += ": " + reason
	}
	notice := appRequestNoticeKind + id
	if err := notifyAppPeopleFrom(app, personID, appNoticeRequestPrefix+id, entry.Name, administrators, "", notice, map[string]string{"en": preview},
		"/admin/settings?section=apps"); err != nil {
		app.Logger().Warn("leafen app notice failed", "request", id, "error", err)
	}
	return request, nil
}

func loadAppRequest(app core.App, id string) (appRequestWire, error) {
	requests, err := listAppRequests(app, "[[request]].[[id]] = {:id}", dbx.Params{"id": id})
	if err != nil {
		return appRequestWire{}, err
	}
	if len(requests) != 1 {
		return appRequestWire{}, errAppRequestMissing
	}
	return requests[0], nil
}

// appRequestsWindow is how many waiting requests a list gives at a time.
const appRequestsWindow = 200

// listPendingAppRequests is a window of the waiting requests, newest first,
// from the cursor on; next goes on from there, "" at the end.
func listPendingAppRequests(app core.App, cursor string) ([]appRequestWire, string, error) {
	after, err := decodeTimeIDCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	requests, err := listAppRequests(app, `[[request]].[[status]] = 'pending'
		AND ({:afterId} = '' OR [[request]].[[created]] < {:afterAt}
			OR ([[request]].[[created]] = {:afterAt} AND [[request]].[[id]] < {:afterId}))`,
		dbx.Params{"afterAt": after.At, "afterId": after.ID, "limit": appRequestsWindow + 1})
	if err != nil || len(requests) <= appRequestsWindow {
		return requests, "", err
	}
	requests = requests[:appRequestsWindow]
	last := requests[len(requests)-1]
	return requests, encodeTimeIDCursor(last.Created, last.ID), nil
}

// listAppRequests reads the requests a condition names, newest first, as
// many as params' limit (one when it gives none).
func listAppRequests(app core.App, where string, params dbx.Params) ([]appRequestWire, error) {
	if _, given := params["limit"]; !given {
		params["limit"] = 1
	}
	rows := []struct {
		ID         string `db:"id"`
		CatalogID  string `db:"catalog_id"`
		AppID      string `db:"app_id"`
		PersonID   string `db:"person_id"`
		PersonName string `db:"person_name"`
		TenancyKey string `db:"tenancy_key"`
		Reason     string `db:"reason"`
		Status     string `db:"status"`
		Created    int64  `db:"created"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[request]].[[id]], [[request]].[[catalog_id]], [[request]].[[app_id]], [[request]].[[person_id]],
			` + accountNameSQL("person") + ` AS [[person_name]], [[request]].[[tenancy_key]], [[request]].[[reason]],
			[[request]].[[status]], [[request]].[[created]]
		FROM {{leafen_app_requests}} [[request]] JOIN {{users}} [[person]] ON [[person]].[[id]] = [[request]].[[person_id]]
		WHERE ` + where + ` ORDER BY [[request]].[[created]] DESC, [[request]].[[id]] DESC LIMIT {:limit}`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	requests := []appRequestWire{}
	// No request, nothing to name: the catalogs aren't read, the built-in
	// apps among them packed on their first reading (2026-11-16: Server ›
	// Apps' first list after a start waited seconds for them, its rows blank).
	if len(rows) == 0 {
		return requests, nil
	}
	tenancies, err := appTenancies(app)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, tenancy := range tenancies {
		names[tenancy.Key] = tenancy.Name
	}
	browse, err := browseAppCatalogs(app)
	if err != nil {
		return nil, err
	}
	installed := map[string]string{}
	installs, err := loadAppInstalls(app, "[[removed_at]] = 0", dbx.Params{})
	if err != nil {
		return nil, err
	}
	for _, install := range installs {
		installed[install.AppID] = install.ID
	}
	for _, row := range rows {
		request := appRequestWire{ID: row.ID, CatalogID: row.CatalogID, AppID: row.AppID, AppName: map[string]string{"en": row.AppID},
			PersonID: row.PersonID, PersonName: row.PersonName, TenancyKey: row.TenancyKey, TenancyName: names[row.TenancyKey],
			Reason: row.Reason, Status: row.Status, Created: row.Created, InstalledID: installed[row.AppID]}
		for _, entry := range browse.Apps {
			if entry.ID == row.AppID {
				request.AppName = entry.Name
			}
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func appRequestsListRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	requests, next, err := listPendingAppRequests(event.App, event.Request.URL.Query().Get("cursor"))
	if errors.Is(err, errListWindowCursor) {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, "Give the nextCursor of the last list, or none to start.")
	}
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, appRequestListWire{Requests: requests, NextCursor: next})
}

func appRequestDecideRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	input := appRequestDecision{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
			refusalMessage("Approve or decline.", bodyRefusalReason("decision", err)))
	}
	request, err := decideAppRequest(event.App, event.Request.PathValue("requestId"), input.Approve, user.Id, time.Now())
	if err != nil {
		return appPolicyFailure(event, err)
	}
	return event.JSON(http.StatusOK, request)
}

// decideAppRequest lets the person use the app once administrators
// installed it, or declines; the person hears either way.
func decideAppRequest(app core.App, id string, approve bool, actorID string, now time.Time) (appRequestWire, error) {
	request, err := loadAppRequest(app, id)
	if err != nil {
		return appRequestWire{}, err
	}
	if request.Status != "pending" {
		return appRequestWire{}, errAppRequestMissing
	}
	status := "declined"
	if approve {
		if request.InstalledID == "" {
			return appRequestWire{}, appPolicyRefused("Install the app first, then approve the request.")
		}
		if err := grantAppPerson(app, request.InstalledID, request.TenancyKey, request.PersonID, actorID); err != nil {
			return appRequestWire{}, err
		}
		status = "approved"
	}
	if _, err := app.DB().Update(appRequestsTable, dbx.Params{"status": status, "decided_by": actorID, "decided_at": now.UnixMilli()},
		dbx.HashExp{"id": id}).Execute(); err != nil {
		return appRequestWire{}, err
	}
	organization := strings.TrimPrefix(request.TenancyKey, "org:")
	if approve {
		install, err := loadAppInstall(app, request.InstalledID)
		if err == nil {
			err = notifyAppPeople(app, install, []string{request.PersonID}, organization, "app-request-approved:"+id, nil, "/apps/"+install.Slug)
		}
		warnAppNotice(app, install, "app-request-approved:"+id, err)
	} else if err := notifyAppPeopleAbout(app, appNoticeRequestPrefix+id, request.AppName, []string{request.PersonID}, organization,
		"app-request-declined:"+id, nil, "/"); err != nil {
		app.Logger().Warn("leafen app notice failed", "request", id, "error", err)
	}
	return loadAppRequest(app, id)
}

// grantAppPerson adds a person to who may use an app in an organization.
func grantAppPerson(app core.App, installID, tenancy, personID, actorID string) error {
	grants, err := loadAppGrants(app, installID)
	if err != nil {
		return err
	}
	grant := appGrantWire{TenancyKey: tenancy, UserIDs: []string{}}
	for _, existing := range grants {
		if existing.TenancyKey == tenancy {
			grant = existing
		}
	}
	if grant.Everyone {
		return nil
	}
	for _, userID := range grant.UserIDs {
		if userID == personID {
			return nil
		}
	}
	grant.UserIDs = append(grant.UserIDs, personID)
	return setAppGrants(app, installID, []appGrantWire{grant}, actorID)
}
