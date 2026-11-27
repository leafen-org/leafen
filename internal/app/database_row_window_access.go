package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A read that shows some items of its databases — a page graph's window, a
// row's own page — decides the reader's role in those items alone, each
// found by its key, however many of their databases' items are unlinked
// (database_row_unlinked_access.go). Before 2026-11-20 each window of a
// database page read every unlinked page of its workspace and authorized
// every unlinked item of its databases, again where a property of theirs
// has an access rule, to decide the few the window held.

// loadDatabaseWindowRowAccess is loadDatabaseRowAccess for the given items
// of the data sources alone: whether each is unlinked, and the reader's role
// in those that are. Any other item reads as a linked one.
func loadDatabaseWindowRowAccess(app core.App, readerID string, dataSourceIDs, rowIDs []string) (*databaseRowAccess, error) {
	if len(dataSourceIDs) == 0 {
		return nil, nil
	}
	unlinked := []string{}
	if len(rowIDs) > 0 {
		rows, err := json.Marshal(rowIDs)
		if err != nil {
			return nil, err
		}
		sources, err := json.Marshal(dataSourceIDs)
		if err != nil {
			return nil, err
		}
		// From the items asked about to their unlinks, pages and databases,
		// each by key (CROSS JOIN keeps that order).
		if err := app.DB().NewQuery(`
			SELECT DISTINCT [[unlink]].[[page_id]] FROM json_each({:rows}) [[asked]]
			CROSS JOIN {{leafen_page_share_unlinks}} [[unlink]] ON [[unlink]].[[page_id]] = [[asked]].[[value]]
			CROSS JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[unlink]].[[page_id]]
			CROSS JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[page]].[[data_source_id]]
				AND [[source]].[[workspace_id]] = [[unlink]].[[workspace_id]]
			WHERE [[source]].[[id]] IN (SELECT [[value]] FROM json_each({:sources}))
			ORDER BY [[unlink]].[[page_id]]
		`).Bind(dbx.Params{"rows": string(rows), "sources": string(sources)}).Column(&unlinked); err != nil {
			return nil, err
		}
	}
	return newDatabaseRowAccess(app, readerID, unlinked)
}

// newDatabasePropertyAccessReaderOfRows is newDatabasePropertyAccessReader
// for a read of the given items of the data sources alone.
func newDatabasePropertyAccessReaderOfRows(app core.App, readerID string, dataSourceIDs, rowIDs []string) (*databasePropertyAccessReader, error) {
	return newDatabasePropertyAccessReaderRows(app, readerID, dataSourceIDs,
		func(app core.App, readerID string, sources []string) (*databaseRowAccess, error) {
			return loadDatabaseWindowRowAccess(app, readerID, sources, rowIDs)
		})
}
