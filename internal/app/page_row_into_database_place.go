package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// pageIntoDatabasePosition is where a page moved into a database stands among
// its rows: after the source's last row, as in Notion (seen 2026-11-10);
// before its first (request.First), as a block moved into the database is
// put; or at the place it names (request.Position), the place a row cut and
// pasted on a page left, which the paste's ⌘Z gives back, as Notion's ⌘Z
// brings the item back where it stood (seen 2026-11-27 in Chrome, two tabs at
// two times, a table's and a board's).
func pageIntoDatabasePosition(app core.App, request pageToDatabaseRowRequest, ownerID string) (string, error) {
	if request.Position != "" {
		return request.Position, nil
	}
	if request.First {
		return databaseRowPositionAtStart(app, ownerID, request.ContainerPageID, request.DataSourceID)
	}
	var tail string
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[row]].[[position]]), '') FROM {{leafen_database_rows}} AS [[row]] WHERE ` + databaseSourceRowsSQL("[[row]]", "{:sourceId}") + `
	`).Bind(dbx.Params{"sourceId": request.DataSourceID}).Row(&tail); err != nil {
		return "", err
	}
	return databasePositionAfter(tail)
}

// keepRowCreatedWhereItStood gives a page going back to the place it left
// (request.Position) its row's creation time back: the page's own, which its
// row had, so a view that shows its rows newest first shows it where it stood
// rather than first (2026-11-27: a cut row's paste taken back by ⌘Z came back
// at the top of such a view).
func keepRowCreatedWhereItStood(app core.App, request pageToDatabaseRowRequest, page *core.Record) error {
	created := page.GetDateTime("created").Time()
	if request.Position == "" || created.IsZero() {
		return nil
	}
	for table, column := range map[string]string{"leafen_database_rows": "id", "leafen_database_row_pages": "row_id"} {
		if _, err := app.DB().Update(table, dbx.Params{"created": created.UTC().UnixMilli()}, dbx.HashExp{column: request.PageID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
