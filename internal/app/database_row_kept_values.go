package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database page that becomes a page keeps its values, as Notion's does
// (seen 2026-10-31: a row moved into a page came back by "실행 취소" with its
// values). They wait by the page and its property, and when the page goes
// back into that data source it takes again those its properties still hold;
// another database starts it without them, as Notion's.

// keepDatabaseRowPageValues keeps the row's values but its title, in place of
// those it kept before.
func keepDatabaseRowPageValues(app core.App, authority databaseRowPageAuthority) error {
	if err := forgetDatabaseRowPageValues(app, authority.RowID); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_row_kept_values}}
			([[page_id]], [[data_source_id]], [[property_id]], [[type]], [[value]], [[spans]], [[kept]])
		SELECT [[row_id]], {:sourceId}, [[property_id]], [[type]], [[value]], COALESCE([[spans]], ''), {:now}
		FROM {{leafen_database_row_values}}
		WHERE [[row_id]] = {:rowId} AND [[page_id]] = {:pageId} AND [[type]] != 'title'
	`).Bind(dbx.Params{
		"rowId": authority.RowID, "pageId": authority.ContainerPageID, "sourceId": authority.DataSourceID,
		"now": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

// databaseRowPageKeptValues are the page's kept values from this data source
// whose properties it still has, with the same type, but those its database
// works out or numbers itself: the page comes back with a new ID, as Notion's
// (seen 2026-11-06: a row with ID 1 moved out and back came back as 10 with
// its other values).
func databaseRowPageKeptValues(app core.App, pageID, dataSourceID, containerPageID string) (map[string]graphRowValue, error) {
	held := []struct {
		PropertyID string `db:"property_id"`
		Type       string `db:"type"`
		Value      string `db:"value"`
		Spans      string `db:"spans"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[held]].[[property_id]], [[held]].[[type]], [[held]].[[value]], [[held]].[[spans]]
		FROM {{leafen_database_row_kept_values}} AS [[held]]
		JOIN {{leafen_database_properties}} AS [[property]] ON [[property]].[[id]] = [[held]].[[property_id]]
		WHERE [[held]].[[page_id]] = {:pageId} AND [[held]].[[data_source_id]] = {:sourceId}
			AND [[property]].[[data_source_id]] = {:sourceId} AND [[property]].[[page_id]] = {:containerId}
			AND [[property]].[[type]] = [[held]].[[type]] AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
	`).Bind(dbx.Params{"pageId": pageID, "sourceId": dataSourceID, "containerId": containerPageID}).All(&held); err != nil {
		return nil, err
	}
	values := map[string]graphRowValue{}
	for _, cell := range held {
		raw := json.RawMessage(cell.Value)
		if _, uncarried := databaseRowPageMoveUncarriedTypes[cell.Type]; uncarried || !json.Valid(raw) {
			continue
		}
		values[cell.PropertyID] = graphRowValue{Type: cell.Type, Value: raw, Spans: databaseStoredSpans(cell.Spans)}
	}
	return values, nil
}

func forgetDatabaseRowPageValues(app core.App, pageID string) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_row_kept_values}} WHERE [[page_id]] = {:pageId}`).
		Bind(dbx.Params{"pageId": pageID}).Execute()
	return err
}
