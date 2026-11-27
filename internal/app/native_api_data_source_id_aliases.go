package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The native API takes a database's data source by every ID the MCP tools
// give for it, as it takes a page (native_api_id_aliases.go) and a person
// (native_api_person_id_aliases.go): leafen_catalog and notion-fetch give a
// database by its address (/p/<its own ID>) and its data source as
// collection://<the identity made for the API>, while the native routes
// that act on a data source — a database's automations and buttons
// (/pages/{pageId}/databases/{databaseId}/…), its sprints and toolbar
// (/pages/{pageId}/databases/{dataSourceId}/…), /data-sources/{dataSourceId}/…
// — read the data source's own ID (seen 2026-11-26: an automation made on
// the IDs leafen_catalog gave was answered "Automation not found." until the
// agent dug the IDs out of the page's content graph). A data source's place
// — those path wildcards, and a body's or a query's dataSourceId — takes
// what the data source tools take (mcpLocalDataSourceID): the data source's
// own ID or collection://<ID>, its identity made for the API, with or
// without dashes; or a database's own ID, address, identity or full-page
// page, when the database shows one data source. A database of several, or
// an ID naming none, is left as written, for the route to refuse.
//
// Beside them, a page's /pages/{pageId}/databases/… routes take for the
// page a data source's ID, which names the page it is on, and a body's or a
// query's viewId takes view://<ID> as leafen_catalog and notion-fetch write a
// view.

// nativeDataSourceIDWildcards are the route wildcards that name a data
// source: every {databaseId} in a path names one (a database's automations
// and buttons), as every {dataSourceId} does.
var nativeDataSourceIDWildcards = map[string]bool{"databaseId": true, "dataSourceId": true}

// nativeDataSourceID is the data source's own ID for a value naming it in
// any of the forms above; any other value as nativeOwnID leaves it.
func nativeDataSourceID(app core.App, value string) string {
	written := strings.TrimSpace(value)
	if written == "" || nativeDataSourceExists(app, written) {
		return value
	}
	// A UUID in any case, as people's IDs are taken (nativePersonID).
	if bare := strings.TrimPrefix(written, mcpDataSourceURLPrefix); len(bare) <= 36 {
		if _, uuid := normalizeNotionUUIDRequest(strings.ToLower(bare)); uuid {
			written = strings.TrimSuffix(written, bare) + strings.ToLower(bare)
		}
	}
	if id, ok := mcpLocalDataSourceID(app, written); ok && id != written && nativeDataSourceExists(app, id) {
		return id
	}
	if written != strings.TrimSpace(value) && nativeDataSourceExists(app, written) {
		return written
	}
	return nativeOwnID(app, notionObjectIdentityDataSource, nativeIDWithDashes(value))
}

// nativeDataSourceExists says whether a data source has this ID, in the
// trash too: the route says what it makes of one there.
func nativeDataSourceExists(app core.App, id string) bool {
	if id == "" || len(id) > maxGraphIdentifierLength {
		return false
	}
	var found int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": id}).Row(&found)
	return err == nil && found > 0
}

// nativeDataSourcePage is the page a data source a value names is on, for
// a /pages/{pageId}/databases/… route given the data source for its page;
// "" when the value names none.
func nativeDataSourcePage(app core.App, value string) string {
	id := nativeDataSourceID(app, value)
	if !nativeDataSourceExists(app, id) {
		return ""
	}
	pageID := ""
	_ = app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": id}).Row(&pageID)
	return pageID
}

// nativeViewID is a view's ID as view://<ID> writes it, without the prefix.
func nativeViewID(value string) string {
	if id, ok := strings.CutPrefix(strings.TrimSpace(value), mcpViewURLPrefix); ok && id != "" {
		return id
	}
	return value
}
