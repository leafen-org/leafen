package app

import (
	"net/http"
	"strings"
)

// Notion's content capabilities (seen 2026-10-30 on an internal connection's
// "기능"): "콘텐츠 업데이트" edits existing pages and database items, "콘텐츠
// 삽입" makes new ones. An internal connection's grant names them as
// content:update and content:insert, and its writes are kept to them for
// every route, where Notion refuses what a token may not do
// (refuseNotionRequest); a public connection is kept to its write scopes.

const (
	internalConnectionClientPrefix = "internal:"
	notionContentUpdateScope       = "content:update"
	notionContentInsertScope       = "content:insert"
)

// notionContentWriteScope is the content capability a request needs: insert
// for what makes a page, database, data source, block children or file
// upload; update for what changes, moves or deletes them, or a view.
func notionContentWriteScope(method, path string) string {
	rest, found := strings.CutPrefix(path, notionAPIRoot+"/")
	if !found {
		return ""
	}
	parts := strings.Split(rest, "/")
	changes := method == http.MethodPatch || method == http.MethodDelete
	switch parts[0] {
	case "pages", "databases", "data_sources":
		switch {
		case len(parts) == 1 && method == http.MethodPost:
			return notionContentInsertScope
		case len(parts) == 2 && parts[1] != "" && changes,
			len(parts) == 3 && parts[0] == "pages" && (parts[2] == "markdown" && method == http.MethodPatch || parts[2] == "move" && method == http.MethodPost):
			return notionContentUpdateScope
		}
	case "views":
		if len(parts) == 1 && method == http.MethodPost || len(parts) == 2 && parts[1] != "" && changes {
			return notionContentUpdateScope
		}
	case "blocks":
		switch {
		case len(parts) == 2 && parts[1] != "" && changes:
			return notionContentUpdateScope
		case len(parts) == 3 && parts[2] == "children" && method == http.MethodPatch:
			return notionContentInsertScope
		}
	case "file_uploads":
		if method == http.MethodPost && (len(parts) == 1 || len(parts) == 3 && (parts[2] == "send" || parts[2] == "complete")) {
			return notionContentInsertScope
		}
	}
	return ""
}

// notionContentWriteAllowed is whether a token may make the content write a
// request is (required, from notionContentWriteScope): an internal
// connection only as its content capabilities allow; anything else as its
// scopes do. refuseNotionRequest answers what isn't allowed.
func notionContentWriteAllowed(identity notionOAuthBearerIdentity, required string) bool {
	return required == "" || !notionInternalConnection(identity) || notionOAuthHasScope(identity, required)
}

// notionInternalConnection is whether a token is an internal connection's,
// owned by its workspace rather than by one person.
func notionInternalConnection(identity notionOAuthBearerIdentity) bool {
	return strings.HasPrefix(identity.ClientID, internalConnectionClientPrefix)
}

// An internal connection belongs to no one person, so it makes no page or
// database at the workspace's top level: Notion's API says so when a page
// or a database is asked for there or a page without a parent (seen
// 2026-11-11). A database without a parent is refused by its shape.
const (
	notionInternalPageParentMessage = "A new page needs a `parent.page_id` or `parent.database_id`, " +
		"unless a public integration with the `insert_content` capability makes it. An internal integration belongs to no one person, " +
		"so it makes no private page at the workspace's top level."
	notionInternalDatabaseParentMessage = "A new database needs a `parent.page_id`, " +
		"unless a public integration with the `insert_content` capability makes it. An internal integration belongs to no one person, " +
		"so it makes no private database at the workspace's top level."
)
