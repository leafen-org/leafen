package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const accessRevisionTable = "leafen_access_revision"

// currentAccessRevision is the access revision now.
func currentAccessRevision(app core.App) (int64, error) {
	var revision int64
	err := app.DB().NewQuery(`SELECT [[revision]] FROM {{leafen_access_revision}} WHERE [[singleton]] = {:singleton}`).
		Bind(dbx.Params{"singleton": "access"}).Row(&revision)
	return revision, err
}
