//go:build !leafen_noapps

package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The app's own data (docs/app-platform.md 6): its screen, with the
// person's app token, and its workers, as the app's account, read where
// each setup step's data went in the organization they act in — a page it
// made; a database's block, its data source, its first view and the page it
// is on, for the page's own routes, and its columns when the call may read
// that page, with the names the app wrote; a view — and whether people edit
// it, only view it or never see it, instead of searching the workspace for it.

func registerAppDataMapRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPlatformDataMapPath, readAppDataMapRoute)
	event.Router.GET(apiRoot+appPlatformPeoplePath, readAppPeopleRoute)
	event.Router.GET(apiRoot+appPlatformGroupsPath, readAppGroupsRoute)
	event.Router.GET(apiRoot+appPlatformWorkspacesPath, readAppWorkspacesRoute)
}

func readAppDataMapRoute(event *core.RequestEvent) error {
	authority, ok := event.Get(userKeyAuthorityKey).(userKeyAuthority)
	if !ok || authority.AppInstallID == "" || authority.TenancyKey == "" {
		return apiError(event, http.StatusForbidden, errorCodeAppRelayDenied, "Only the app itself reads its data's places.")
	}
	install, err := loadAppInstall(event.App, authority.AppInstallID)
	if err != nil || install.Status != "active" || install.RemovedAt != 0 {
		return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "App not found.")
	}
	_, manifest, err := loadAppPackage(event.App, install.PackageHash)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	answer := appDataMapWire{Items: []appDataEntryWire{}}
	err = event.App.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_app_workspaces}}
		WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy}`).
		Bind(dbx.Params{"install": install.ID, "tenancy": authority.TenancyKey}).Row(&answer.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return event.JSON(http.StatusOK, answer)
	}
	if err != nil {
		return appPlatformFailure(event, err)
	}
	rows := []struct {
		Key    string `db:"data_key"`
		Page   string `db:"page_id"`
		Source string `db:"data_source_id"`
		View   string `db:"view_id"`
		Spec   string `db:"spec"`
	}{}
	principal := ""
	if user, ok := authenticatedUser(event); ok {
		principal = user.Id
	}
	// The organization's data, then the calling person's own.
	personID := authority.OwnerID
	if authority.AppAccount {
		personID = ""
	}
	if err := event.App.DB().NewQuery(`SELECT [[data_key]], [[page_id]], [[data_source_id]], [[view_id]], [[spec]] FROM (
		SELECT [[data_key]], [[page_id]], [[data_source_id]], [[view_id]], [[spec]], [[created]], 0 AS [[own]] FROM {{leafen_app_data}}
			WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy}
		UNION ALL SELECT [[data_key]], [[page_id]], [[data_source_id]], [[view_id]], [[spec]], [[created]], 1 AS [[own]] FROM {{leafen_app_person_data}}
			WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy} AND [[user_id]] = {:person} AND {:person} != ''
	) ORDER BY [[own]], [[created]], [[data_key]]`).
		Bind(dbx.Params{"install": install.ID, "tenancy": authority.TenancyKey, "person": personID}).All(&rows); err != nil {
		return appPlatformFailure(event, err)
	}
	for _, row := range rows {
		// The teamspaces and root pages the data went into are Leafen's own arrangement.
		if strings.HasPrefix(row.Key, "teamspace-") || strings.HasPrefix(row.Key, "root-") {
			continue
		}
		entry := appDataEntryWire{Key: row.Key, DataSourceID: row.Source, ViewID: row.View, Place: appDataPlace(manifest, row.Key)}
		// A step recorded what it made: a database's block, a page, or a view.
		var page string
		lookup := event.App.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_blocks}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": row.Page})
		switch {
		case row.Source != "":
			entry.BlockID = row.Page
			if err := lookup.Row(&page); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return appPlatformFailure(event, err)
			}
			entry.PageID = page
			if entry.Properties, err = appDataProperties(event.App, authority, principal, page, row.Source); err != nil {
				return appPlatformFailure(event, err)
			}
			// The app reads its columns and options by the names it wrote.
			appDataWrittenProperties(entry.Properties, appDataWritten(row.Spec))
		case appDataPageExists(event.App, row.Page):
			entry.PageID = row.Page
		default:
			entry.ViewID = row.Page
		}
		answer.Items = append(answer.Items, entry)
	}
	return event.JSON(http.StatusOK, answer)
}

func appDataPageExists(app core.App, id string) bool {
	var count int
	return app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": id}).Row(&count) == nil && count > 0
}

// appDataProperties lists a database's columns in order, as its page reads
// them, when the call could read that page itself: in one of its key's
// workspaces and allowed to the person or account calling. A person's screen
// learns nothing of data kept from them.
func appDataProperties(app core.App, authority userKeyAuthority, principal, pageID, sourceID string) ([]appDataPropertyWire, error) {
	var workspaceID string
	err := app.DB().NewQuery(`SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !slices.Contains(authority.WorkspaceIDs, workspaceID)) || principal == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := authorizePage(app, principal, pageID, permissionWorkspaceRead); isPermissionDenied(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	rows := []struct {
		ID       string `db:"id"`
		Name     string `db:"name"`
		Type     string `db:"type"`
		Position string `db:"position"`
		Config   string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[name]], [[type]], [[position]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} AND [[page_id]] = {:page} AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]]`).
		Bind(dbx.Params{"source": sourceID, "page": pageID}).All(&rows); err != nil {
		return nil, err
	}
	properties := make([]appDataPropertyWire, 0, len(rows))
	for _, row := range rows {
		config := map[string]any{}
		if row.Config != "" {
			if err := json.Unmarshal([]byte(row.Config), &config); err != nil {
				return nil, err
			}
		}
		properties = append(properties, appDataPropertyWire{ID: row.ID, Name: row.Name, Type: row.Type, Position: row.Position, Config: config})
	}
	return properties, nil
}

// appReviewerLanguage is the language Leafen writes in for the person a
// package review is for, which names its data changes.
func appReviewerLanguage(event *core.RequestEvent) string {
	user, ok := authenticatedUser(event)
	if !ok {
		return ""
	}
	language, err := serverTextLanguage(event.App, user.Id, event.Request)
	if err != nil {
		return ""
	}
	return language
}
