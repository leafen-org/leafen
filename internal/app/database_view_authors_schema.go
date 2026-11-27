package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database's view keeps who made it and who last changed it, as Notion's
// view object names them (seen 2026-11-11): the person whose change wrote
// it, a connection's bot for one a connection made or changed
// (connection_authorship.go), and no one for the first view of a database
// the API made. A view from before reads as made and last changed by the
// person who made its page.

// clearDatabaseViewAuthors leaves a view made by no one, as the first view
// of a database Notion's API makes is.
func clearDatabaseViewAuthors(app core.App, viewID string) error {
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_views}} SET [[created_by]] = '', [[last_edited_by]] = '' WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": viewID}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_connection_authorship}} WHERE [[kind]] = {:kind} AND [[object_id]] = {:id}
	`).Bind(dbx.Params{"kind": connectionAuthoredView, "id": viewID}).Execute()
	return err
}
