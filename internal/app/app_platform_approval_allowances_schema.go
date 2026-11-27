package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The approvals' readable fields and the hour a person may allow an action
// for (app_platform_approval_allowances.go).

// appOverseen says the administrator oversees the organization's apps:
// every action asks, and nothing is allowed for an hour.
func appOverseen(app core.App, tenancy string) bool {
	var count int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_oversight}} WHERE [[tenancy_key]] = {:tenancy}`).
		Bind(dbx.Params{"tenancy": tenancy}).Row(&count)
	return err == nil && count > 0
}
