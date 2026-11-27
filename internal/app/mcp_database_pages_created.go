package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The database pages one notion-create-pages call makes share one created
// time, the first's, as Notion's do (seen 2026-11-17 in two calls, of three
// and four pages: one createdTime for all, and read back newest first with
// those of the call in the order of their IDs, in rows and view mode alike —
// databaseNaturalOrder). Each is made apart here (createMCPDatabasePages),
// so each took a time of its own (databaseRowCreatedAt), which read them back
// last first.

// shareMCPCreatedRowsTime gives the database pages a call made the first's
// created time, on their rows and their pages alike.
func shareMCPCreatedRowsTime(app core.App, pages []notionMCPCreatedPage) error {
	if len(pages) < 2 {
		return nil
	}
	ids := make([]any, 0, len(pages)-1)
	for _, page := range pages[1:] {
		ids = append(ids, page.ID)
	}
	return app.RunInTransaction(func(txApp core.App) error {
		var created int64
		if err := txApp.DB().NewQuery(`SELECT [[created]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": pages[0].ID}).Row(&created); err != nil {
			return err
		}
		for _, table := range []string{"leafen_database_rows", "leafen_database_row_pages"} {
			column := "id"
			if table == "leafen_database_row_pages" {
				column = "row_id"
			}
			if _, err := txApp.DB().Update(table, dbx.Params{"created": created}, dbx.In(column, ids...)).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
}
