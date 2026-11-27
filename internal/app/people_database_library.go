package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// libraryPeopleDatabaseOf is what the library and the sidebar's favorites
// show of People's own database page, which is in no one's page list
// (people_database.go), nil for any other page: Notion's library lists a
// starred People as "People" by "Notion App" — Leafen's own author here —
// with when it last changed, and opens People from it (seen 2026-11-16 in
// Chrome at 1440, a free workspace's owner).
func libraryPeopleDatabaseOf(app core.App, pageID string) (*libraryRowPageWire, error) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && isDatabaseRowPageRecord(page) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	database, _, people, err := peopleDatabasePageOf(app, page)
	if err != nil || !people {
		return nil, err
	}
	return &libraryRowPageWire{
		Title: peopleDatabaseTitle, DataSourceID: database.DataSourceID,
		CreatedAt: page.GetString("created"), UpdatedAt: page.GetString("updated"),
		CreatedBy: page.GetString("created_by"), LastEditedBy: page.GetString("last_edited_by"),
	}, nil
}
