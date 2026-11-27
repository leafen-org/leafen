package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Strict mode for one organization (docs/app-platform.md 7): beside the
// server's switch, an administrator may put just some organizations' apps
// under it — their frames reach outside only through the relay, which
// records what it sends.

// appTenancyStrict says the organization alone is under strict mode.
func appTenancyStrict(app core.App, tenancy string) bool {
	var count int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_strict_tenancies}} WHERE [[tenancy_key]] = {:tenancy}`).
		Bind(dbx.Params{"tenancy": tenancy}).Row(&count)
	return err == nil && count > 0
}

// appStrictFor says an organization's apps are under strict mode: the
// server's or its own.
func appStrictFor(app core.App, tenancy string) bool {
	if settings, err := loadAppPlatformSettings(app); err != nil || settings.StrictNetwork {
		return true
	}
	return tenancy != "" && appTenancyStrict(app, tenancy)
}
