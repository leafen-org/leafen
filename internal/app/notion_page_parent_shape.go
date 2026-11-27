package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The parent the Notion-compatible API gives a thing that sits in a page —
// a block of its body, a sub-page, a database in it — is that page, except
// in a wiki's page, which is the wiki's database (notion_wiki_identity.go):
// Notion gives what sits directly in a wiki {"type":"database_id"} with the
// wiki's ID (seen 2026-11-19, Notion-Version 2025-09-03: a page of the wiki
// read, found, made by parent.database_id or moved to the wiki; blocks added
// to the wiki; a database made in it), and a page under another page of the
// wiki {"type":"page_id"} with that page. A wiki's pages are its data
// source's pages, and each answers so by the page it sits under in the wiki
// (pageWikiParentSQL), not by its data source as another database's page does.

// notionParentInPage is the parent of a thing sitting directly in a page.
func notionParentInPage(app core.App, pageID string) (map[string]any, error) {
	publicID, err := notionPagePublicID(app, pageID)
	if err != nil {
		return nil, err
	}
	wikiDatabase, err := wikiDatabaseOfPage(app, pageID)
	if err != nil {
		return nil, err
	}
	if wikiDatabase != "" {
		return map[string]any{"type": "database_id", "database_id": publicID}, nil
	}
	return map[string]any{"type": "page_id", "page_id": publicID}, nil
}

// notionRowPageParent is a database's page's parent: the page it sits under
// for a wiki's page, else its data source, with that source's database.
func notionRowPageParent(app core.App, rowID, publicSourceID, publicDatabaseID string) (map[string]any, error) {
	holder := ""
	if err := app.DB().NewQuery(`SELECT COALESCE(` + pageWikiParentSQL("[[page]]") + `, '')
		FROM {{leafen_pages}} AS [[page]] WHERE [[page]].[[id]] = {:id}
	`).Bind(dbx.Params{"id": rowID}).Row(&holder); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if holder != "" {
		return notionParentInPage(app, holder)
	}
	return map[string]any{
		"type": "data_source_id", "data_source_id": publicSourceID, "database_id": publicDatabaseID,
	}, nil
}
