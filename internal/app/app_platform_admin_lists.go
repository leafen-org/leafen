//go:build !leafen_noapps

package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// An app's lists for the server's administrators — its open guest links, the
// reports about its guests, its runs and its approvals — come a window at a
// time, newest first. The guests and jobs answers carry each list's first
// window and its next cursor; the list's own route reads on from a cursor,
// and gives "" once the last is read.

const (
	appGuestLinksPerWindow   = 50
	appGuestReportsPerWindow = 30
	appRunsPerWindow         = 20
	appApprovalsPerWindow    = 30
)

// appWindowOf is an app list's window (timeIDWindowOf).
func appWindowOf[T any](items []T, size int, cursor func(T) (int64, string)) ([]T, string) {
	return timeIDWindowOf(items, size, cursor)
}

// appAdminWindowRoute answers an administrator's read of one of an app's
// lists from the request's cursor on.
func appAdminWindowRoute[T any](read func(app core.App, install storedAppInstall, manifest appManifest, after timeIDCursor) (T, error)) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		if !authorizeAdministratorRequest(event) {
			return nil
		}
		install, manifest, err := adminAppInstall(event)
		if err != nil {
			return appPlatformFailure(event, err)
		}
		after, err := decodeTimeIDCursor(event.Request.URL.Query().Get("cursor"))
		if err != nil {
			return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, "Give the nextCursor of the last list, or none to start.")
		}
		window, err := read(event.App, install, manifest, after)
		if err != nil {
			return appPlatformFailure(event, err)
		}
		return event.JSON(http.StatusOK, window)
	}
}

func registerAppAdminWindowRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPlatformGuestLinksListPath, appAdminWindowRoute(
		func(app core.App, install storedAppInstall, _ appManifest, after timeIDCursor) (appGuestLinksWindow, error) {
			links, next, err := loadAppGuestLinks(app, install.ID, after)
			return appGuestLinksWindow{Links: links, NextCursor: next}, err
		}))
	event.Router.GET(apiRoot+appPlatformGuestReportsListPath, appAdminWindowRoute(
		func(app core.App, install storedAppInstall, _ appManifest, after timeIDCursor) (appGuestReportsWindow, error) {
			reports, next, err := loadAppGuestReports(app, install.ID, after)
			return appGuestReportsWindow{Reports: reports, NextCursor: next}, err
		}))
	event.Router.GET(apiRoot+appPlatformJobRunsListPath, appAdminWindowRoute(
		func(app core.App, install storedAppInstall, manifest appManifest, after timeIDCursor) (appRunsWindow, error) {
			runs, next, err := loadAppRuns(app, install, manifest, after)
			return appRunsWindow{Runs: runs, NextCursor: next}, err
		}))
	event.Router.GET(apiRoot+appPlatformJobApprovalsListPath, appAdminWindowRoute(
		func(app core.App, install storedAppInstall, manifest appManifest, after timeIDCursor) (appApprovalHistoryWindow, error) {
			approvals, next, err := loadAppApprovals(app, install, manifest, after)
			return appApprovalHistoryWindow{Approvals: approvals, NextCursor: next}, err
		}))
}
