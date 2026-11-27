package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A Notion wiki's page is a page block in the body of the page it sits under
// in the wiki — the wiki's own page for one at its top — and the API lists it
// there once, as a child_page where its block stands (seen 2026-11-19,
// Notion-Version 2025-09-03: GET /v1/blocks/<wiki>/children of a wiki whose
// first page was its sub-page, then a page made by parent.database_id, a page
// moved in, a database made in it and a paragraph appended, lists the three
// pages as child_page, the database as child_database and the paragraph, in
// that order, each with parent {"type":"database_id"} and the wiki's ID, and
// no link_to_page; a page of the wiki under another one is a child_page of
// that page's children, and that page's Markdown writes it as
// <page url="…">title</page>). Here a wiki's page is its database's page, and
// its page block is a link marked as the wiki's (wiki_page_links.go), so the
// API reads that link as the page it stands for, and lists a page of the
// wiki with no such link in the body after the body's blocks, as a sub-page
// with no block yet is (notionPageBodyCTE).

// notionPageBodyWikiPagesCTE names the wiki's live pages that sit directly
// under a page — a page whose row is in the trash isn't (database_row_trash.go)
// — read from the wikis (CROSS JOIN: SQLite's own order read the owner's
// every row).
func notionPageBodyWikiPagesCTE() string {
	return `[[wiki_pages]] AS (
			SELECT [[wiki_page]].[[id]], [[wiki_page]].[[position]]
			FROM {{leafen_data_sources}} [[wiki_source]]
			CROSS JOIN {{leafen_database_rows}} [[wiki_row]] ON [[wiki_row]].[[owner]]=[[wiki_source]].[[owner]]
				AND [[wiki_row]].[[page_id]]=[[wiki_source]].[[page_id]]
				AND [[wiki_row]].[[data_source_id]]=[[wiki_source]].[[id]]
			JOIN {{leafen_pages}} [[wiki_page]] ON [[wiki_page]].[[id]]=[[wiki_row]].[[id]]
			WHERE [[wiki_source]].[[owner]]={:owner} AND [[wiki_source]].[[workspace_id]]={:workspaceId}
				AND [[wiki_source]].[[wiki]]=1 AND [[wiki_page]].[[in_trash]]=FALSE
				AND [[wiki_page]].[[notion_import_staging]]=FALSE AND ` + databaseRowNotTrashedSQL("[[wiki_row]].[[id]]") + `
				AND (([[wiki_source]].[[page_id]]={:pageId} AND [[wiki_row]].[[subitem_parent_id]]='')
					OR [[wiki_row]].[[subitem_parent_id]]={:pageId})
		)`
}

// notionPageBodyWikiLinkSQL holds a block of the body that is a link standing
// for one of the wiki's pages under the page.
func notionPageBodyWikiLinkSQL(block string) string {
	return `(` + wikiPageLinkSQL(block) + `
		AND json_extract(` + block + `.[[content]], '$.childPageId') IN (SELECT [[id]] FROM [[wiki_pages]]))`
}

// notionPageBodyWikiItemsSQL lists the wiki's pages under a page as its body
// items: each at its link's place in the body's top level, the first link
// when a change left two, and after the body's blocks, in sidebar order, one
// whose link isn't in the body; one whose link sits deeper in the body is
// listed there (notionPageBlockStandIn). It leaves out the wiki's own
// database block on the wiki's page: the wiki is that database.
func notionPageBodyWikiItemsSQL() string {
	return `
			UNION ALL
			SELECT 0, MIN([[link]].[[position]]), 0.0, 'page', json_extract([[link]].[[content]], '$.childPageId')
			FROM {{leafen_blocks}} [[link]]
			WHERE [[link]].[[owner]]={:owner} AND [[link]].[[page_id]]={:pageId} AND [[link]].[[parent_id]]=''
				AND ` + blockStandsSQL("link") + ` AND ` + notionPageBodyWikiLinkSQL("[[link]]") + `
			GROUP BY json_extract([[link]].[[content]], '$.childPageId')
			UNION ALL
			SELECT 1, '', CAST([[wiki_pages]].[[position]] AS REAL), 'page', [[wiki_pages]].[[id]]
			FROM [[wiki_pages]]
			WHERE NOT EXISTS (
				SELECT 1 FROM {{leafen_blocks}} [[shown]]
				WHERE [[shown]].[[owner]]={:owner} AND [[shown]].[[page_id]]={:pageId}
					AND ` + blockStandsSQL("shown") + ` AND ` + wikiPageLinkSQL("[[shown]]") + `
					AND json_extract([[shown]].[[content]], '$.childPageId')=[[wiki_pages]].[[id]]
			)`
}

// notionPageBodyWikiBlockLeftOutSQL holds a top-level block the body lists
// otherwise: a wiki page's link, listed as its page, or the wiki's database
// block on its own page.
func notionPageBodyWikiBlockLeftOutSQL(block string) string {
	return `(` + notionPageBodyWikiLinkSQL(block) + ` OR EXISTS (
					SELECT 1 FROM {{leafen_database_containers}} [[wiki_container]]
					JOIN {{leafen_database_container_sources}} [[wiki_member]]
						ON [[wiki_member]].[[container_id]]=[[wiki_container]].[[id]]
					JOIN {{leafen_data_sources}} [[wiki_database]] ON [[wiki_database]].[[id]]=[[wiki_member]].[[data_source_id]]
					WHERE [[wiki_container]].[[block_id]]=` + block + `.[[id]] AND [[wiki_container]].[[page_id]]={:pageId}
						AND [[wiki_database]].[[wiki]]=1
				))`
}

// notionPageBlockStandIn reads a block that stands for a page as that page's
// child_page block: a sub-page's own block, with the sub-page's parent, or a
// link standing for a wiki's page, with the parent its place in the body
// gives (notionParentInPage at the body's top). isPage is false for any other
// block, and for a link whose page has left the wiki or is in the trash,
// which reads as the link it is.
func notionPageBlockStandIn(app core.App, block storedNotionBlock) (map[string]any, bool, error) {
	var page *core.Record
	var parent map[string]any
	var err error
	if isStoredChildPageBlock(block.ID, block.Type, block.Content) {
		if page, err = app.FindRecordById(pagesCollection, block.ID); err != nil {
			return nil, true, err
		}
		if parent, err = notionChildPageParent(app, page); err != nil {
			return nil, true, err
		}
	} else {
		pageID, err := wikiPageOfLinkBlock(app, block)
		if err != nil || pageID == "" {
			return nil, false, err
		}
		if page, err = app.FindRecordById(pagesCollection, pageID); err != nil {
			return nil, true, err
		}
		if block.ParentID == "" {
			parent, err = notionParentInPage(app, block.PageID)
		} else {
			parent = map[string]any{"type": "block_id"}
			parent["block_id"], err = notionBlockPublicID(app, block.WorkspaceID, block.ParentID)
		}
		if err != nil {
			return nil, true, err
		}
	}
	hasChildren, err := loadNotionPageHasChildren(app, []string{page.Id})
	if err != nil {
		return nil, true, err
	}
	value, err := notionChildPageBlockObject(app, page, parent, hasChildren[page.Id])
	return value, true, err
}

// wikiPageOfLinkBlock is the live page of a wiki a link marked as the wiki's
// stands for — not in the trash itself or as its row (database_row_trash.go)
// — or "" for any other block.
func wikiPageOfLinkBlock(app core.App, block storedNotionBlock) (string, error) {
	if block.Type != graphBlockTypeLinkToPage {
		return "", nil
	}
	pageID := ""
	err := app.DB().NewQuery(`
		SELECT [[page]].[[id]] FROM {{leafen_blocks}} AS [[link]]
		JOIN {{leafen_database_rows}} AS [[row]] ON [[row]].[[id]] = json_extract([[link]].[[content]], '$.childPageId')
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[row]].[[data_source_id]] AND [[source]].[[wiki]] = 1
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[row]].[[id]] AND [[page]].[[in_trash]] = FALSE
		WHERE [[link]].[[id]] = {:id} AND [[link]].[[owner]] = {:owner} AND ` + wikiPageLinkSQL("[[link]]") + `
			AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]") + `
		LIMIT 1
	`).Bind(dbx.Params{"id": block.ID, "owner": block.Owner}).Row(&pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return pageID, err
}
