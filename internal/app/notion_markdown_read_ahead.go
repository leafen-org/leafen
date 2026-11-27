package app

import (
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's Markdown read what it names a page at a time: a page naming its
// 2,100 sub-pages, in sub-page blocks and in its text, read for each one its
// record twice — for its <page> tag and its reader's access — whether it is
// a wiki and its public ID, and for each mention of it whether a page has
// its ID, its title and its trash, which "v2" writes a mention of a page in
// by its address alone (2026-11-25, NAS condition: an agent's fetch of it
// took some 5 s, nearly all of it that). The reads take the pages of its
// sub-page blocks together first (notionMarkdownSubPagesAhead), the writer
// tells its mentions' targets ahead the pages its blocks and titles mention
// (notion_markdown_mention_targets.go), and a block's public ID is read for
// a file's line only (fileBlockID); each then reads as it did.

// notionMarkdownSubPagesAhead reads the pages of a page's sub-page blocks
// together into the read scope's memo — their records
// (primeRecordsRemembered), where its references and its reader's access
// then find them, and which are wikis (notionMarkdownWikiOfPage). Outside a
// read scope it reads nothing; one it couldn't read is read alone, as before.
func notionMarkdownSubPagesAhead(app core.App, blocks []workspaceExportMarkdownBlock) {
	pageIDs := []string{}
	for _, block := range blocks {
		if block.Type == graphBlockTypeChildPage {
			pageIDs = append(pageIDs, block.ID)
		}
	}
	_ = primeRecordsRemembered(app, pagesCollection, pageIDs)
	_ = primeWikiDatabasesOfPages(app, pageIDs)
}

type wikiDatabaseOfPageMemoKey struct{ pageID string }

// notionMarkdownWikiOfPage is wikiDatabaseOfPage, once a read scope, where
// many may be read ahead together (primeWikiDatabasesOfPages).
func notionMarkdownWikiOfPage(app core.App, pageID string) (string, error) {
	return remembered(app, wikiDatabaseOfPageMemoKey{pageID}, func() (string, error) {
		return wikiDatabaseOfPage(app, pageID)
	})
}

// primeWikiDatabasesOfPages reads which pages are wikis, and of which
// database, together into a read scope's memo, as wikiDatabaseOfPage reads
// each: its database's container of the lowest ID, "" for a page that is no
// wiki.
func primeWikiDatabasesOfPages(app core.App, pageIDs []string) error {
	holder, ok := app.(accessMemoHolder)
	if !ok {
		return nil
	}
	memo := holder.accessMemo()
	wanted, listed := make([]string, 0, len(pageIDs)), map[string]bool{}
	for _, pageID := range pageIDs {
		if _, known := memo[wikiDatabaseOfPageMemoKey{pageID}]; !known && pageID != "" && !listed[pageID] {
			listed[pageID] = true
			wanted = append(wanted, pageID)
		}
	}
	for start := 0; start < len(wanted); start += 500 {
		part := wanted[start:min(start+500, len(wanted))]
		params, placeholders := dbx.Params{}, make([]string, 0, len(part))
		for index, pageID := range part {
			name := "page" + strconv.Itoa(index)
			params[name] = pageID
			placeholders = append(placeholders, "{:"+name+"}")
		}
		rows := []struct {
			PageID      string `db:"page_id"`
			ContainerID string `db:"container_id"`
		}{}
		if err := app.DB().NewQuery(`SELECT [[container]].[[page_id]] AS [[page_id]], MIN([[container]].[[id]]) AS [[container_id]] ` +
			wikiDatabaseSQL + ` WHERE [[container]].[[page_id]] IN (` + strings.Join(placeholders, ",") + `)
			GROUP BY [[container]].[[page_id]]`).Bind(params).All(&rows); err != nil {
			return err
		}
		for _, pageID := range part {
			memo[wikiDatabaseOfPageMemoKey{pageID}] = accessMemoEntry{value: ""}
		}
		for _, row := range rows {
			memo[wikiDatabaseOfPageMemoKey{row.PageID}] = accessMemoEntry{value: row.ContainerID}
		}
	}
	return nil
}

// fileBlockID is the ID a block's line names it by when it is a file's
// (notionMarkdownFileBlockLine), none for any other block, which isn't
// written so: each sub-page block read its page's public ID for it.
func (w *notionMarkdownWriter) fileBlockID(block workspaceExportMarkdownBlock) string {
	if _, tagged := notionMarkdownFileTagNames[block.Type]; !tagged && block.Type != graphBlockTypeImage {
		return ""
	}
	return w.publicBlockID(block.ID)
}
