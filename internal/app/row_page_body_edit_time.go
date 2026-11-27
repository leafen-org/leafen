package app

import (
	"context"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database page's last edited time is its row page's (leafen_database_row_pages):
// its page's '마지막 편집', its database's last edited time property, the
// Public API's last_edited_time, backlinks and MCP all read that one value.
// A value written to its row moves it (the row's own time, copied by its
// trigger); so does its body changed, as Notion's does (seen 2026-11-22 on
// a synthetic row: a paragraph added to its body minutes after it was made
// moved its API last_edited_time and its backlinks' and fetch's time to the
// edit) — by its socket's text (noteDatabaseRowBodyChangeAt) or by its
// content graph, a block made, changed or taken out through the Public API,
// MCP or the editor (notePageContentEdited).

// noteRowPageBodyEdited moves a database page's last edited time on to now
// after its body changed, with its body revision, as its socket's text does;
// a page that is no database page's, or one an import is still bringing in
// (which keeps the time it came with), stays as it is.
func noteRowPageBodyEdited(ctx context.Context, app core.App, pageID string) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_row_pages}}
		SET [[body_snapshot_revision]]=[[body_snapshot_revision]]+1, [[updated]]=MAX([[updated]], {:edited})
		WHERE [[row_id]]={:rowId} AND NOT EXISTS (
			SELECT 1 FROM {{leafen_pages}} WHERE [[id]]={:rowId} AND [[notion_import_staging]]
		)
	`).WithContext(ctx).Bind(dbx.Params{"rowId": pageID, "edited": time.Now().UTC().UnixMilli()}).Execute()
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed == 0 {
		return err
	}
	return markDatabaseRowSearchDirty(app, pageID)
}

// databaseRowPageEditedAt is a database page's last edited time, as its row
// page keeps it; "" for a page that is none.
func databaseRowPageEditedAt(app core.App, rowID string) string {
	var edited int64
	if app.DB().NewQuery(`SELECT [[updated]] FROM {{leafen_database_row_pages}} WHERE [[row_id]]={:rowId}`).
		Bind(dbx.Params{"rowId": rowID}).Row(&edited) != nil || edited <= 0 {
		return ""
	}
	return time.UnixMilli(edited).UTC().Format(time.RFC3339Nano)
}
