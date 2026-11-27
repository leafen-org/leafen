package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A row put in the trash, brought back or deleted for good changes what a
// read gives of it and of the rows whose relations name it, which reads
// leave out while it is in the trash or gone (hideTrashedRelationTargets).
// Its change names them, so a page behind it, and each page showing its
// rows, reads those again, not their whole graph (content_graph_changed_rows.go):
// before, one row put in the trash in a database of 5,000 had its page and
// each page linking it read 51 windows again, some 7 seconds on a NAS-class
// server (2026-11-25).

// databaseRowsAsRead is the rows given and the rows whose relations name one
// of them, sorted; nil when they are more than a page reads as its changes
// (maxGraphChangedRows), and the page is read whole.
func databaseRowsAsRead(app core.App, rowIDs []string) ([]string, error) {
	if len(rowIDs) == 0 || len(rowIDs) > maxGraphChangedRows {
		return nil, nil
	}
	encoded, err := json.Marshal(rowIDs)
	if err != nil {
		return nil, err
	}
	// The relations naming them are those pointing at their databases.
	naming := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[item]].[[row_id]]
		FROM {{leafen_database_relation_property_targets}} AS [[target]]
		JOIN {{leafen_database_relation_items}} AS [[item]] INDEXED BY [[idx_leafen_database_relation_items_target]]
			ON [[item]].[[owner]] = [[target]].[[owner]] AND [[item]].[[page_id]] = [[target]].[[page_id]]
				AND [[item]].[[target_row_id]] IN (SELECT [[value]] FROM json_each({:ids}))
				AND [[item]].[[property_id]] = [[target]].[[property_id]]
		WHERE ([[target]].[[owner]], [[target]].[[target_source_id]]) IN (
			SELECT [[owner]], [[data_source_id]] FROM {{leafen_database_rows}}
			WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
		)
		LIMIT {:limit}
	`).Bind(dbx.Params{"ids": string(encoded), "limit": maxGraphChangedRows + 1}).Column(&naming); err != nil {
		return nil, err
	}
	rows := map[string]struct{}{}
	for _, id := range append(append([]string{}, rowIDs...), naming...) {
		rows[id] = struct{}{}
	}
	if len(rows) > maxGraphChangedRows {
		return nil, nil
	}
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// databaseRowTrashEventAsRead is the rows of a trash event and the rows
// whose relations name one of them (databaseRowsAsRead).
func databaseRowTrashEventAsRead(app core.App, rootRowID string) ([]string, error) {
	rowIDs := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[row_id]] FROM {{leafen_database_row_trash}}
		INDEXED BY [[idx_leafen_database_row_trash_root]]
		WHERE [[root_row_id]] = {:rootId}
		LIMIT {:limit}
	`).Bind(dbx.Params{"rootId": rootRowID, "limit": maxGraphChangedRows + 1}).Column(&rowIDs); err != nil {
		return nil, err
	}
	return databaseRowsAsRead(app, rowIDs)
}
