package app

import (
	"net/url"
	"strings"
)

// notionViewListNeedsID is what a view list naming neither a database nor a
// data source is told.
const notionViewListNeedsID = "Give database_id, data_source_id, or both."

// notionViewListScope is what a view list reads the views of: a data source
// named beside a database decides, the database not looked at, as Notion's
// list does (seen 2026-11-21: beside a data_source_id, the database_id of its
// own database, another's, a linked database's or one that doesn't exist lists
// that data source's views across its databases; a data source that doesn't
// exist is not found). given is false when the query names neither.
func notionViewListScope(query url.Values) (databaseID, dataSourceID string, given bool) {
	databaseID, dataSourceID = strings.TrimSpace(query.Get("database_id")), strings.TrimSpace(query.Get("data_source_id"))
	if dataSourceID != "" {
		databaseID = ""
	}
	return databaseID, dataSourceID, databaseID != "" || dataSourceID != ""
}
