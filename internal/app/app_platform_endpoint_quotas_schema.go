package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What an app's endpoints may use and used (document apps decision of
// 2026-10-10, 8): an administrator's limits for the app, the hours' counts,
// and the files its endpoints kept for each person.

const (
	appEndpointDefaultPerMinute   = 600
	appEndpointDefaultConcurrent  = 4
	appEndpointMaxPerMinute       = 60000
	appEndpointMaxConcurrent      = 64
	appEndpointMaxPersonStorageMB = 10 << 20
)

// loadAppEndpointLimits is what an administrator set for the app's
// endpoints, or the defaults: no storage limit of the app's own beyond the
// organization's.
func loadAppEndpointLimits(app core.App, installID string) appEndpointLimitsWire {
	limits := appEndpointLimitsWire{PerMinute: appEndpointDefaultPerMinute, Concurrent: appEndpointDefaultConcurrent}
	_ = app.DB().NewQuery(`SELECT [[per_minute]], [[concurrent]], [[person_storage_mb]] FROM {{leafen_app_endpoint_limits}}
		WHERE [[install_id]] = {:install}`).Bind(dbx.Params{"install": installID}).Row(&limits.PerMinute, &limits.Concurrent, &limits.PersonStorageMB)
	return limits
}

// appEndpointStoredBytes is what an app's endpoints keep now for a person —
// or, with no person, for everyone: the files it kept that are still there.
func appEndpointStoredBytes(app core.App, installID, userID string) (int64, error) {
	var stored int64
	err := app.DB().NewQuery(`SELECT CAST(COALESCE(SUM([[file]].[[size]]), 0) AS INTEGER) FROM {{leafen_app_endpoint_files}} [[kept]]
		JOIN {{leafen_files}} [[file]] ON [[file]].[[id]] = [[kept]].[[file_id]]
		WHERE [[kept]].[[install_id]] = {:install} AND ({:user} = '' OR [[kept]].[[user_id]] = {:user})`).
		Bind(dbx.Params{"install": installID, "user": userID}).Row(&stored)
	return stored, err
}

// appEndpointOpened reports whether an administrator opened a public
// endpoint for the app's permissions as they are now; an update that
// changes them closes it again.
func appEndpointOpened(app core.App, install storedAppInstall, endpoint appManifestEndpoint) bool {
	if !endpoint.Public {
		return false
	}
	hash := ""
	_ = app.DB().NewQuery(`SELECT [[permissions_hash]] FROM {{leafen_app_endpoint_public}} WHERE [[install_id]] = {:install} AND [[endpoint_key]] = {:key}`).
		Bind(dbx.Params{"install": install.ID, "key": endpoint.Key}).Row(&hash)
	return hash != "" && hash == install.PermissionsHash
}
