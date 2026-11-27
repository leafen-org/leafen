package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A link to a data source's or a view's address written by the ID Leafen
// gives it — the address the MCP tools' fetch names it by
// (collection://ID, view://ID) — links what a link to its public API ID
// does: the app's address of that ID, /p/<32 hexadecimal digits>, as Notion's
// write links collection://ID and view://ID at its app's address of the ID
// (notionMarkdownAppAddress). Before 2026-11-21 one by an ID of the app's own
// form, which is no UUID, was words — "\[a\](collection://ID)" in the public
// API's "v2" and the MCP's Markdown, the words alone in "v1" — and one by a
// UUID linked /p/<that ID>, an address that names nothing a link by the
// public API's ID names.
//
// An address naming by an ID that is no UUID is kept as written until the
// write knows its workspace (notionMarkdownSitePages.sourceLinkTarget); the
// write's boundary points it here (notionOwnSourceLinkHere), or where
// Notion's write links one of no ID of its own when nothing here has it
// (seen 2026-11-21, "v1" and "v2": [a](collection://x) links its app's
// address, [v](view://x) its app's address of /p/x, the ID's dashes left
// out, seen 2026-11-22). A reader with no site
// to write to links that at once. The MCP's writer, which makes a link to a
// data source its mention, makes one by such an ID its mention too, as one
// by a UUID (mcpLinkAsWritten).

// notionMarkdownOwnSourceAddress is the address and the ID of a data
// source's or a view's address naming it by an ID of the app's own form,
// which is no UUID; false for any other.
func notionMarkdownOwnSourceAddress(raw string) (string, bool) {
	address := strings.TrimSpace(raw)
	id, collection := strings.CutPrefix(address, notionMarkdownDataSourceAddress)
	if !collection {
		var viewed bool
		if id, viewed = strings.CutPrefix(address, notionMarkdownViewAddress); !viewed {
			return "", false
		}
	}
	if _, isUUID := normalizeNotionUUIDRequest(id); isUUID || !workspaceImportReferenceIDPattern.MatchString(id) {
		return "", false
	}
	return address, true
}

// sourceLinkTarget is the link a [text](address) link to a data source's or a
// view's address keeps as it is read: for a writer that makes links mentions
// (LinksMentioned), a data source's as written, the source it mentions; one
// by an ID of the app's own form as written, for the write to point here
// (notionOwnSourceLinkHere), or with no site to write to, where Notion's
// links it. false for any other address.
func (site *notionMarkdownSitePages) sourceLinkTarget(raw string) (string, bool) {
	address := strings.TrimSpace(raw)
	if site.linksMentioned() && strings.HasPrefix(address, notionMarkdownDataSourceAddress) {
		if _, named := notionMarkdownAppAddress(address); named {
			return address, true
		}
	}
	own, named := notionMarkdownOwnSourceAddress(address)
	if !named || site != nil {
		return own, named
	}
	return notionOwnSourceLinkElsewhere(own), true
}

// notionOwnSourceLinkElsewhere is where Notion's write links a data source's
// or a view's address of an ID no data source or view has: its app's
// address, and of /p/ and the ID for a view's.
func notionOwnSourceLinkElsewhere(address string) string {
	if id, viewed := strings.CutPrefix(address, notionMarkdownViewAddress); viewed {
		return "/p/" + strings.ReplaceAll(id, "-", "")
	}
	return "/"
}

// notionMarkdownViewID is whether an ID after view:// names a view as
// Notion's Markdown reads one (seen 2026-11-22): letters, digits, "-" and
// "_", one letter or digit at least ("view://" alone and "view://가나" are
// words).
func notionMarkdownViewID(id string) bool {
	return workspaceImportReferenceIDPattern.MatchString(id) && strings.Trim(id, "-_") != ""
}

// notionMarkdownViewLink is the app's address a view's address shows and the
// link it makes, false for any other address: a UUID's /p/<hex>
// (notionMarkdownAppAddress); any other ID's /p/ and the ID, its dashes left
// out, as Notion's links one (seen 2026-11-22, "v1", "v2" and the MCP: bare
// "view://Ab-c_d", "[a](view://Ab-c_d)" and "v1"'s <mention-page|database>
// of one link /p/Abc_d), kept as written where there is a site to write to,
// for the write to point at the workspace's view of that ID
// (notionOwnSourceLinkHere).
func notionMarkdownViewLink(address string, site bool) (string, string, bool) {
	id, viewed := strings.CutPrefix(address, notionMarkdownViewAddress)
	if !viewed {
		return "", "", false
	}
	if link, ok := notionMarkdownAppAddress(address); ok {
		return link, link, true
	}
	if !notionMarkdownViewID(id) {
		return "", "", false
	}
	shown := notionOwnSourceLinkElsewhere(address)
	if site {
		return shown, address, true
	}
	return shown, shown, true
}

// notionOwnSourceLinkHere is the link a write keeps for a data source's or a
// view's address by an ID of the app's own form (notionMarkdownOwnSourceAddress):
// the address of the workspace's data source or view of that ID, else where
// Notion's links it; false for any other link.
func notionOwnSourceLinkHere(app core.App, workspaceID, link string) (string, bool, error) {
	own, named := notionMarkdownOwnSourceAddress(link)
	if !named {
		return "", false, nil
	}
	id, collection := strings.CutPrefix(own, notionMarkdownDataSourceAddress)
	if !collection {
		id = strings.TrimPrefix(own, notionMarkdownViewAddress)
	}
	pointed, err := notionOwnSourcePath(app, workspaceID, id, collection, !collection)
	if err != nil || pointed != "" {
		return pointed, true, err
	}
	return notionOwnSourceLinkElsewhere(own), true, nil
}

// notionOwnSourcePath is the address a link to the workspace's data source
// (sources) or view (views) named by its own ID reads at, its public API ID's
// (/p/<hex>), or "" for none.
func notionOwnSourcePath(app core.App, workspaceID, id string, sources, views bool) (string, error) {
	kind := ""
	err := app.DB().NewQuery(`
		SELECT {:source} AS [[kind]] FROM {{leafen_data_sources}}
		WHERE {:sources} AND [[id]] = {:id} AND [[workspace_id]] = {:workspaceId}
		UNION ALL
		SELECT {:view} FROM {{leafen_database_views}}
		WHERE {:views} AND [[id]] = {:id} AND [[workspace_id]] = {:workspaceId}
		LIMIT 1
	`).Bind(dbx.Params{
		"source": notionObjectIdentityDataSource, "view": notionObjectIdentityView, "sources": sources, "views": views,
		"id": id, "workspaceId": workspaceID,
	}).Row(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	public, err := ensureNotionObjectIdentity(app, kind, workspaceID, id)
	if err != nil {
		return "", err
	}
	return "/p/" + strings.ReplaceAll(public, "-", ""), nil
}
