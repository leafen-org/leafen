package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

func readBootstrapSnapshotRevision(app core.App) (int, error) {
	var revision int
	err := app.DB().NewQuery(`
		SELECT [[revision]] FROM {{leafen_bootstrap_authority_revision}}
		WHERE [[singleton]] = 'authority'
	`).Row(&revision)
	if err != nil {
		return 0, err
	}
	if revision < 1 {
		return 0, errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return revision, nil
}
