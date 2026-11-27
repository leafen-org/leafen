package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A synced block reference naming a database is refused, on a new page's
// first body too, where Notion's create takes one (seen 2026-11-27 twice,
// "v1" and "v2", page and data source parents, inline and full-page): its
// copy there shows nothing of the database — an empty line on screen, an
// empty tag read back — while lines written in it are kept under the
// database, where the database itself never shows them; unsyncing it does
// nothing, and an export writes a link to the database instead. Leafen says
// at once what such a reference is and how to show the database instead.
// Insert and replace refuse it as Notion's do, naming the database's block
// by Notion's name for its kind: "collection_view" inline, and
// "collection_view_page" for a full-page database. A data source's address
// names no block or page here and makes a copy syncing from none, as
// Notion's does (synced_block_sourceless.go). A full-page database's own
// page, by its address in the app, is the database too, never a page whose
// body a copy shows (synced_block_page_original.go).

// notionMarkdownSyncedDatabase is the refusal of a reference whose address
// names a database: its block, or a full-page database's own page, which
// Notion's addresses name as the database itself.
func notionMarkdownSyncedDatabase(address string, fullPage bool) error {
	kind := "collection_view"
	if fullPage {
		kind = "collection_view_page"
	}
	return graphInputError(errorCodeGraphReferenceInvalid, "A synced block reference points to an original synced block, and "+
		address+" is a "+kind+" block, a database, which a synced block doesn't show: mention it with a <mention-database> tag "+
		"instead, or use the url a <synced_block> tag gives.")
}

// isDatabasePage reports whether a page is a full-page database's own page.
func isDatabasePage(app core.App, pageID string) bool {
	var pages int
	_ = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]] = {:id} AND [[kind]] = {:kind}`).
		Bind(dbx.Params{"id": pageID, "kind": pageKindDatabase}).Row(&pages)
	return pages > 0
}
