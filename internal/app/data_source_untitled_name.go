package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database keeps what it calls its pages without a title, as Notion's keeps
// its item name: a database made from Notion's task tracker calls them "신규
// 작업", its projects "신규 프로젝트", its docs hub "신규 문서" (collection
// item_name_config, seen 2026-11-11), whichever template is its default. It
// is written once, in the language the database was made in, as the
// template's database is made; a database made otherwise has none and names
// them by its default template (databaseUntitledRowName).

func recordDataSourceUntitledName(app core.App, dataSourceID, name string) error {
	_, err := app.DB().NewQuery(`UPDATE {{leafen_data_sources}} SET [[untitled_name]]={:name} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"name": name, "id": dataSourceID}).Execute()
	return err
}

// dataSourceUntitledName is the name a database keeps for its pages without
// a title, or "".
func dataSourceUntitledName(app core.App, dataSourceID string) (string, error) {
	name := ""
	err := app.DB().NewQuery(`SELECT [[untitled_name]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": dataSourceID}).Row(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}
