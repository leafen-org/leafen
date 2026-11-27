package app

import (
	"net/http"
	"net/url"
	"regexp"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page address the app would move from once it had loaded is redirected,
// before the app loads, to the one it would move to: a browser keeps the
// page's icon for an address redirected to the one the page shows at, and
// not for one the app moved from once it had loaded (2026-11-23, Chrome and
// Edge 158 — a bookmark or history entry of the first showed Leafen's
// mark). The address a page shows at is the one the page link route
// answers (readPageLink), decided here without a reader:
//   - /p/, a title, a dash and the 32 hexadecimal digits of a UUID, as the
//     API's page url and Notion's links name a page (notionPageObjectURL),
//     and the 32 digits alone, name what that UUID names;
//   - a page by any ID it answers to (resolveNotionPageRecordID: its own,
//     its UUID without dashes, its API ID) shows at its own ID, and a
//     full-page database's page at its database's (databaseAddress.link);
//   - a database by its block's, its own or its API ID shows at the one its
//     link names (databaseAddressRecord);
//   - a block's own ID shows its page at the block (resolveBlockPageLink).
// The address stays as it is where the app decides by its reader: People's
// database and its people open as People (peoplePageLink), and a page a
// full-page database left when it turned inline opens the database only for
// one who may read where it is now (loadSignpostDatabaseAddress). The
// redirect tells someone holding a page's ID that it is a full-page
// database and its database's ID, or a block's page; it opens nothing to
// them the page's ID didn't — the server answers both addresses alike, and
// a page or database is reached by who may read it, not by its ID (the
// document request carries no reader to ask).
//
// Some of this server's own IDs end in 32 hexadecimal digits too: a
// database template's database and its rows, "starter-<32 hex>"
// (databaseStarterIdentity), and any ID given through the API. Such an
// address names that ID whole — as the page link route reads it first
// (readPageLink) and the MCP tools do (mcpOwnSlugShapedID) (2026-11-23: the
// app put the dashes in itself, and a template's database went to its
// digits and opened nothing).

var pageAddressTitleForm = regexp.MustCompile(`^([^/]*)-([0-9a-f]{32})$`)

var pageAddressPath = regexp.MustCompile(`^/p/([^/]+)$`)

// pageAddressTitleFormTarget is the address a /p/ path opens at before the
// app loads, with its query, or "" where it opens at the path as it is.
func pageAddressTitleFormTarget(app core.App, request *http.Request) string {
	match := pageAddressPath.FindStringSubmatch(request.URL.Path)
	if match == nil || len(match[1]) > maxGraphIdentifierLength {
		return ""
	}
	piece := match[1]
	id, named := piece, false
	if titled := pageAddressTitleForm.FindStringSubmatch(piece); titled != nil && !pageAddressNamesOwnID(app, piece) {
		id, named = titled[2], true
	}
	at, err := pageAddressDestination(app, id)
	if err != nil {
		return ""
	}
	// A title and a UUID naming nothing here go to the UUID: the app
	// answers it as it answers any address naming nothing they may open.
	if at == "" && named {
		at = "/p/" + id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
	}
	if at == "" || at == "/p/"+url.PathEscape(piece) {
		return ""
	}
	if request.URL.RawQuery != "" {
		next, err := url.Parse(at)
		if err != nil {
			return ""
		}
		next.RawQuery = request.URL.RawQuery
		at = next.String()
	}
	return at
}

// pageAddressDestination is the address an ID opens at, as the page link
// route answers it for one who may read it, or "" where that takes a reader
// or the ID names nothing.
func pageAddressDestination(app core.App, id string) (string, error) {
	if pageID, err := resolveNotionPageRecordID(app, id); err == nil {
		page, err := app.FindRecordById(pagesCollection, pageID)
		if err == nil {
			return pageAddressOf(app, page)
		}
	}
	database, err := databaseAddressRecord(app, id)
	if err != nil {
		return "", err
	}
	if database.ID != "" {
		page, err := app.FindRecordById(pagesCollection, database.PageID)
		if err != nil {
			return "", nil
		}
		if byReader, err := pageOpensByReader(app, page); err != nil || byReader {
			return "", err
		}
		return "/p/" + url.PathEscape(database.ID), nil
	}
	block, found := mcpBlockNamed(app, id)
	if !found {
		return "", nil
	}
	page, err := app.FindRecordById(pagesCollection, block.PageID)
	if err != nil {
		return "", nil
	}
	if byReader, err := pageOpensByReader(app, page); err != nil || byReader {
		return "", err
	}
	return "/p/" + url.PathEscape(page.Id) + mcpDatabaseLinkFragment + url.PathEscape(block.ID), nil
}

// pageAddressOf is the address a page shows at, or "" for one that opens
// by its reader.
func pageAddressOf(app core.App, page *core.Record) (string, error) {
	if byReader, err := pageOpensByReader(app, page); err != nil || byReader {
		return "", err
	}
	block, err := loadFullPageDatabaseBlock(app, page.Id)
	if err != nil {
		return "", err
	}
	if block != "" {
		return "/p/" + url.PathEscape(block), nil
	}
	return "/p/" + url.PathEscape(page.Id), nil
}

// pageOpensByReader tells whether where a page opens depends on its
// reader: People's database and its people's pages (peoplePageLink), and a
// full-page database's signpost (loadSignpostDatabaseAddress).
func pageOpensByReader(app core.App, page *core.Record) (bool, error) {
	if _, _, people, err := peopleDatabasePageOf(app, page); err != nil || people {
		return true, err
	}
	_, signpost, err := loadDatabasePagePlace(app, page.Id)
	return signpost, err
}

// pageAddressNamesOwnID tells whether an ID names a page, a database, a
// row or a block here, whoever may read it: an address naming one opens it
// for those who may, and the page link route answers the rest as for any
// address naming nothing they may open.
func pageAddressNamesOwnID(app core.App, id string) bool {
	found := 0
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM (
			SELECT 1 FROM {{leafen_pages}} WHERE [[id]]={:id}
			UNION ALL
			SELECT 1 FROM {{leafen_database_containers}} WHERE [[id]]={:id}
			UNION ALL
			SELECT 1 FROM {{leafen_database_rows}} WHERE [[id]]={:id}
			UNION ALL
			SELECT 1 FROM {{leafen_blocks}} WHERE [[id]]={:id}
			LIMIT 1
		)
	`).Bind(dbx.Params{"id": id}).Row(&found)
	// Unread, the address is left as it is: the page link route reads it.
	return err != nil || found > 0
}

// redirectPageAddressTitleForm answers a request for the app's document at
// a page address the app would move from with its redirect, or false for
// any other request.
func redirectPageAddressTitleForm(event *core.RequestEvent) (bool, error) {
	target := pageAddressTitleFormTarget(event.App, event.Request)
	if target == "" {
		return false, nil
	}
	event.Response.Header().Set("Cache-Control", "no-cache")
	return true, event.Redirect(http.StatusFound, target)
}
