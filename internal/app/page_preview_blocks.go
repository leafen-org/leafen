package app

import (
	"encoding/json"
	"math"
	"strings"
	"unicode"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// The levels of nested list items a preview follows, and the rows and
// columns of a table it reads: more than a page's card shows of one.
const (
	pagePreviewNestedDepth  = 3
	pagePreviewTableRows    = 3
	pagePreviewTableColumns = 3
)

type pagePreviewStoredBlock struct {
	ID      string `db:"id"`
	Type    string `db:"type"`
	Content string `db:"content"`
}

// pagePreviewBlocks is a page's first blocks as the search preview draws
// them, as Notion's (seen 2026-10-31): in reading order, list items with the
// items nested in them, a toggle as its line alone, a column list's columns
// and a synced block's blocks one after another; each with its text and what
// its kind draws besides — a to-do's tick, a code block's language, a
// callout's icon, a picture's file, a table's first cells and its columns'
// widths, a sub-page's or
// database's name. A synced copy draws its original's blocks, read from the
// original's page, when the reader may read that page; otherwise nothing.
// What names a page closed to the reader names nothing of it
// (mcp_reader_mentions.go): a sub-page or page link to one isn't drawn, as
// Notion's page leaves such a sub-page out; a mention of one is drawn as no
// access, as in Notion's page (both seen 2026-11-09); a table cell reads it
// "Untitled"; a database living in one has no name. closedLines, when
// known, are the page's sub-pages closed to the reader
// (workspaceSearchClosedLines): its other sub-pages open as it does.
func pagePreviewBlocks(app core.App, pageID, workspaceID, readerID string, closedLines map[string]bool) ([]pagePreviewBlock, error) {
	return readPagePreviewBlocks(app, pageID, workspaceID, readerID, closedLines, false)
}

// pageBlocksWholeLines is pagePreviewBlocks for a reader no one is, each
// block's words whole rather than cut to a card's line: what a public
// page's head describes it with (publicPageDescription).
func pageBlocksWholeLines(app core.App, pageID, workspaceID string) ([]pagePreviewBlock, error) {
	return readPagePreviewBlocks(app, pageID, workspaceID, "", nil, true)
}

func readPagePreviewBlocks(app core.App, pageID, workspaceID, readerID string, closedLines map[string]bool, whole bool) ([]pagePreviewBlock, error) {
	// A wiki whose pages are its database's draws as Notion's wiki: its
	// home's blocks without its database and the links standing for its
	// pages (child_page_block_moves.go) — a home of those alone draws its
	// title alone (seen 2026-11-11: the wiki "W383 Vorquen wikitwo" draws
	// "vorquen home line two" and "w383 home other two", not its pages).
	wikiSources := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_data_sources}} WHERE [[page_id]] = {:pageId} AND [[wiki]] = 1`).
		Bind(dbx.Params{"pageId": pageID}).Column(&wikiSources); err != nil {
		return nil, err
	}
	reader := personReader(readerID)
	walk := &pagePreviewWalk{app: app, readerID: readerID, read: searchTextReader(app, workspaceID),
		reader: reader, closes: reader.closes(app), opens: map[string]bool{}, lines: map[string]map[string]bool{},
		blocks: []pagePreviewBlock{}, originals: map[string]bool{}, placed: map[string]bool{}, whole: whole}
	if len(wikiSources) > 0 {
		walk.wiki = map[string]bool{}
		for _, id := range wikiSources {
			walk.wiki[id] = true
		}
		walk.wikiHome = pageID
	}
	if closedLines != nil {
		walk.lines[pageID] = closedLines
	}
	if err := walk.page(pageID, "", 0); err != nil {
		return nil, err
	}
	return walk.blocks, nil
}

// The synced copies a preview follows to their originals, which keeps a copy
// of a copy from going on and on.
const pagePreviewSyncedCopies = 8

type pagePreviewWalk struct {
	app      core.App
	readerID string
	read     func(*crdt.YText) string
	reader   wordsReader
	closes   searchMentionCloser
	// opens holds whether the reader may open the pages blocks named;
	// lines, by page, the sub-pages known closed to the reader.
	opens     map[string]bool
	lines     map[string]map[string]bool
	blocks    []pagePreviewBlock
	originals map[string]bool
	// wiki holds a wiki home's own database's sources, whose box and page
	// links its preview leaves out of wikiHome.
	wiki     map[string]bool
	wikiHome string
	// placed holds the pages whose blocks' order is their places alone.
	placed map[string]bool
	// whole keeps a block's words whole rather than cut to a card's line.
	whole bool
}

// line is a block's words as the walk keeps them.
func (walk *pagePreviewWalk) line(text string) string {
	if walk.whole {
		return strings.TrimSpace(text)
	}
	return pagePreviewLine(text)
}

// page reads a page's documents and draws the blocks under parentID; one
// page's documents are let go before the next is read.
func (walk *pagePreviewWalk) page(pageID, parentID string, depth int) error {
	doc, found, done, err := borrowYgoStreamDocument(walk.app, ygoCollaborationStream, pageID)
	if err != nil {
		return err
	}
	structural, structuralFound, structuralDone, err := borrowYgoStreamDocument(walk.app, ygoStructuralStream, pageID)
	if err != nil {
		done()
		return err
	}
	if !found && !structuralFound {
		structuralDone()
		done()
		return nil
	}
	// A page of blocks without words, a synced copy alone, has its order but no text.
	if !found {
		doc = crdt.New()
	}
	orderDoc := doc
	if structuralFound {
		orderDoc = structural
	} else if ids, ok := searchStructureChildren(doc, ""); ok && len(ids) == 0 {
		// A database page's body keeps its blocks' order in their places, its
		// text alone in its document (database_row_body_room.go): as the page
		// reads, Notion's preview draws a row's body (seen 2026-11-11).
		walk.placed[pageID] = true
	}
	copies := []pagePreviewCopy{}
	err = walk.visit(pageID, doc, orderDoc, parentID, depth, &copies)
	structuralDone()
	done()
	if err != nil {
		return err
	}
	// A copy's blocks go where the copy stands, once its page is let go.
	for i := len(copies) - 1; i >= 0; i-- {
		if err := walk.original(copies[i]); err != nil {
			return err
		}
	}
	return nil
}

// A synced copy met on a page: where its original's blocks go.
type pagePreviewCopy struct {
	at, depth                int
	originalID, originalPage string
}

func (walk *pagePreviewWalk) visit(pageID string, doc, orderDoc *crdt.Doc, parentID string, depth int, copies *[]pagePreviewCopy) error {
	ids, ok, err := walk.children(pageID, orderDoc, parentID)
	if err != nil || !ok || len(ids) == 0 {
		return err
	}
	stored, err := pagePreviewStoredBlocks(walk.app, pageID, ids)
	if err != nil {
		return err
	}
	names, err := pagePreviewNames(walk.app, walk.readerID, pageID, stored)
	if err != nil {
		return err
	}
	for index, id := range ids {
		if len(walk.blocks) == maxPagePreviewBlocks {
			return nil
		}
		block, ok := stored[id]
		if !ok {
			continue
		}
		if block.Type == "column_list" || block.Type == "column" || syncedBlockIsOriginal(block.Type, block.Content) {
			if err := walk.visit(pageID, doc, orderDoc, id, depth, copies); err != nil {
				return err
			}
			continue
		}
		content := map[string]any{}
		_ = json.Unmarshal([]byte(block.Content), &content)
		if pageID == walk.wikiHome && (content[wikiPageLinkMark] == true ||
			block.Type == graphBlockTypeInlineDatabase && walk.wiki[pagePreviewString(content, "dataSourceId")]) {
			continue
		}
		if block.Type == graphBlockTypeSyncedBlock {
			*copies = append(*copies, pagePreviewCopy{at: len(walk.blocks), depth: depth,
				originalID: pagePreviewString(content, "syncedBlockId"), originalPage: pagePreviewString(content, "syncedPageId")})
			continue
		}
		text := doc.GetText(ygoBlockTextRootPrefix + id)
		preview := pagePreviewBlock{Type: block.Type, Depth: depth}
		if !graphBlockWordless(block.Type) {
			preview.Text = walk.line(walk.read(text))
		}
		if name, named := names[id]; named {
			if lines, known := walk.lines[pageID]; known && block.Type == graphBlockTypeChildPage {
				walk.opens[name.pageID] = !lines[name.pageID]
			}
			if _, known := walk.opens[name.pageID]; !known && name.pageID != "" && name.pageID != pageID {
				if err := walk.judge(pageID, names, ids[index:]); err != nil {
					return err
				}
			}
			if name.page && name.pageID != pageID && !walk.opens[name.pageID] {
				continue
			}
			preview.Text = pagePreviewLine(name.name)
			if name.database {
				preview.Type = pagePreviewDatabasePage
			}
			if !name.page && name.pageID != pageID && !walk.opens[name.pageID] {
				preview.Text = ""
			}
		} else if block.Type != "code" && !graphBlockWordless(block.Type) {
			preview.Spans, preview.BlockID = pagePreviewSpans(graphTextSpansAsTheyStand(graphTextSpansFromYText(text), false), walk.closes), id
		}
		switch block.Type {
		case "to_do":
			preview.Checked = content["checked"] == true
		case "code":
			preview.Language, preview.BlockID = pagePreviewString(content, "language"), id
		case "callout":
			// A custom emoji is named by its ID, as the block's icon reads it.
			preview.Icon, preview.IconType = pagePreviewString(content, "icon"), pagePreviewString(content, "iconType")
			if preview.IconType == "custom_emoji" {
				preview.Icon = pagePreviewString(content, "customEmojiId")
			}
		case "image":
			preview.FileID, preview.URL = pagePreviewString(content, "fileId"), pagePreviewString(content, "url")
		case graphBlockTypeTable:
			rows, _, err := walk.children(pageID, orderDoc, id)
			if err != nil {
				return err
			}
			if preview.Cells, err = pagePreviewTableCells(walk.app, pageID, rows, walk.closes); err != nil {
				return err
			}
			table := graphTableBlockContent{}
			_ = json.Unmarshal([]byte(block.Content), &table)
			for _, width := range table.ColumnWidths[:min(len(table.ColumnWidths), pagePreviewTableColumns)] {
				preview.Widths = append(preview.Widths, int(math.Round(width)))
			}
		}
		walk.blocks = append(walk.blocks, preview)
		if depth < pagePreviewNestedDepth && (block.Type == "bulleted_list_item" || block.Type == "numbered_list_item" || block.Type == "to_do") {
			if err := walk.visit(pageID, doc, orderDoc, id, depth+1, copies); err != nil {
				return err
			}
		}
	}
	return nil
}

// pagePreviewJudged is how many of the blocks coming whose pages are asked
// about at once: a page of thousands of sub-pages draws a card's worth.
const pagePreviewJudged = 64

// judge learns, for the next blocks coming that name a page not known yet,
// at once, whether the reader may open it: a sub-page or linked page, the
// page a database lives in.
func (walk *pagePreviewWalk) judge(pageID string, names map[string]pagePreviewName, coming []string) error {
	asked := []string{}
	for _, id := range coming {
		if len(asked) == pagePreviewJudged {
			break
		}
		name, named := names[id]
		if _, known := walk.opens[name.pageID]; named && !known && name.pageID != "" && name.pageID != pageID {
			walk.opens[name.pageID] = false
			asked = append(asked, name.pageID)
		}
	}
	if len(asked) == 0 {
		return nil
	}
	open, err := walk.reader.opens(walk.app, asked)
	if err != nil {
		return err
	}
	for _, id := range asked {
		walk.opens[id] = open[id]
	}
	return nil
}

// original draws a synced copy's original where the copy stood, from the
// original's page, when the reader may read that page (as the editor shows a
// copy), each original once.
func (walk *pagePreviewWalk) original(copy pagePreviewCopy) error {
	if copy.originalID == "" || walk.originals[copy.originalID] || len(walk.originals) >= pagePreviewSyncedCopies {
		return nil
	}
	walk.originals[copy.originalID] = true
	pageID := copy.originalPage
	if pageID == "" {
		row := struct {
			PageID string `db:"page_id"`
		}{}
		if err := walk.app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_blocks}} WHERE [[id]] = {:id} LIMIT 1`).
			Bind(dbx.Params{"id": copy.originalID}).One(&row); err != nil {
			return nil
		}
		pageID = row.PageID
	}
	readable, err := readablePageIDs(walk.app, walk.readerID, []string{pageID})
	if err != nil {
		return err
	}
	if !readable[pageID] {
		return nil
	}
	after := walk.blocks[copy.at:]
	walk.blocks = append([]pagePreviewBlock{}, walk.blocks[:copy.at]...)
	if err := walk.page(pageID, copy.originalID, copy.depth); err != nil {
		return err
	}
	walk.blocks = append(walk.blocks, after...)
	if len(walk.blocks) > maxPagePreviewBlocks {
		walk.blocks = walk.blocks[:maxPagePreviewBlocks]
	}
	return nil
}

func pagePreviewStoredBlocks(app core.App, pageID string, ids []string) (map[string]pagePreviewStoredBlock, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows := []pagePreviewStoredBlock{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[content]] FROM {{leafen_blocks}}
		WHERE [[page_id]] = {:pageId} AND [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"pageId": pageID, "ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	stored := make(map[string]pagePreviewStoredBlock, len(rows))
	for _, row := range rows {
		stored[row.ID] = row
	}
	return stored, nil
}

// children is a block's children in reading order: as the page's structure
// holds them, or, for a page whose order is its blocks' places, by those.
func (walk *pagePreviewWalk) children(pageID string, orderDoc *crdt.Doc, parentID string) ([]string, bool, error) {
	if !walk.placed[pageID] {
		ids, ok := searchStructureChildren(orderDoc, parentID)
		return ids, ok, nil
	}
	ids := []string{}
	err := walk.app.DB().NewQuery(`
		SELECT [[block]].[[id]] FROM {{leafen_pages}} [[page]]
		CROSS JOIN {{leafen_blocks}} [[block]] INDEXED BY [[idx_leafen_blocks_page_order]]
			ON [[block]].[[owner]] = [[page]].[[owner]] AND [[block]].[[page_id]] = [[page]].[[id]] AND [[block]].[[parent_id]] = {:parentId}
		WHERE [[page]].[[id]] = {:pageId}
		ORDER BY [[block]].[[position]], [[block]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"pageId": pageID, "parentId": parentID, "limit": maxPagePreviewBlocks}).Column(&ids)
	return ids, true, err
}

// pagePreviewTableCells is a table's first rows, each its first cells' text,
// a mention of a page closed to the reader "Untitled".
func pagePreviewTableCells(app core.App, pageID string, ids []string, closes searchMentionCloser) ([][]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ids = ids[:min(len(ids), pagePreviewTableRows)]
	stored, err := pagePreviewStoredBlocks(app, pageID, ids)
	if err != nil {
		return nil, err
	}
	rows := [][]string{}
	for _, id := range ids {
		row := graphTableRowBlockContent{}
		if block, ok := stored[id]; !ok || json.Unmarshal([]byte(block.Content), &row) != nil {
			continue
		}
		cells := make([]string, 0, pagePreviewTableColumns)
		for _, spans := range row.Cells[:min(len(row.Cells), pagePreviewTableColumns)] {
			text, _ := spansAsRead(closes, spans)
			cells = append(cells, pagePreviewLine(text))
		}
		rows = append(rows, cells)
	}
	return rows, nil
}

// pagePreviewName is the name a sub-page, page link or database block shows:
// the page's title and the page, or the database's name and the page it
// lives in. A sub-page that is a full-page database or a wiki whose pages
// are its database's is a database there, as Notion's search preview draws
// it (seen 2026-11-14: a "Database" bar where a sub-page draws nothing).
type pagePreviewName struct {
	name     string
	pageID   string
	page     bool
	database bool
}

// pagePreviewDatabasePage is the kind a sub-page that is a database draws as.
const pagePreviewDatabasePage = "database_page"

// pagePreviewNames is the name each sub-page block and database block among
// these shows, as its reader reads it (page_name_mentions.go).
func pagePreviewNames(app core.App, readerID, pageID string, stored map[string]pagePreviewStoredBlock) (map[string]pagePreviewName, error) {
	pages, databases := []string{}, []string{}
	for id, block := range stored {
		switch block.Type {
		case graphBlockTypeChildPage, "link_to_page":
			pages = append(pages, id)
		case "inline_database", "child_database":
			databases = append(databases, id)
		}
	}
	names := map[string]pagePreviewName{}
	if len(pages) == 0 && len(databases) == 0 {
		return names, nil
	}
	rows := []struct {
		ID     string `db:"id"`
		Name   string `db:"name"`
		PageID string `db:"page_id"`
		Kind   string `db:"kind"`
	}{}
	if len(pages) > 0 {
		encoded, _ := json.Marshal(pages)
		if err := app.DB().NewQuery(`
			SELECT [[block]].[[id]], [[child]].[[title]] AS [[name]], [[child]].[[id]] AS [[page_id]], [[child]].[[kind]]
			FROM {{leafen_blocks}} [[block]]
			JOIN {{leafen_pages}} [[child]] ON [[child]].[[id]] = COALESCE(json_extract([[block]].[[content]], '$.childPageId'), json_extract([[block]].[[content]], '$.pageId'))
			WHERE [[block]].[[page_id]] = {:pageId} AND [[block]].[[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[child]].[[in_trash]] = FALSE
		`).Bind(dbx.Params{"pageId": pageID, "ids": string(encoded)}).All(&rows); err != nil {
			return nil, err
		}
	}
	if len(databases) > 0 {
		encoded, _ := json.Marshal(databases)
		sources := []struct {
			ID          string `db:"id"`
			ContainerID string `db:"container_id"`
			Name        string `db:"name"`
			PageID      string `db:"page_id"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[container]].[[block_id]] AS [[id]], [[container]].[[id]] AS [[container_id]], COALESCE(MIN([[source]].[[title]]), '') AS [[name]],
				COALESCE(MIN([[source]].[[page_id]]), '') AS [[page_id]]
			FROM {{leafen_database_containers}} [[container]]
			JOIN {{leafen_database_container_sources}} [[member]] ON [[member]].[[container_id]] = [[container]].[[id]]
			LEFT JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[member]].[[data_source_id]]
			WHERE [[container]].[[page_id]] = {:pageId} AND [[container]].[[block_id]] IN (SELECT [[value]] FROM json_each({:ids}))
			GROUP BY [[container]].[[block_id]]
		`).Bind(dbx.Params{"pageId": pageID, "ids": string(encoded)}).All(&sources); err != nil {
			return nil, err
		}
		containers := make([]string, 0, len(sources))
		for _, source := range sources {
			containers = append(containers, source.ContainerID)
		}
		read := databaseNamesAsRead(app, readerID, containers)
		// One of several sources by its own name, once it has one (inline_database_own_name.go).
		several, err := inlineDatabaseNamesOfSeveral(app, readerCloser(app, readerID), containers)
		if err != nil {
			return nil, err
		}
		for _, source := range sources {
			if name, named := read[source.ContainerID]; named {
				source.Name = name.Title
			}
			if own := several[source.ContainerID]; own.Title != "" {
				source.Name = own.Title
			}
			names[source.ID] = pagePreviewName{name: source.Name, pageID: source.PageID}
		}
	}
	read := make(map[string]string, len(rows))
	for _, row := range rows {
		read[row.PageID] = row.Name
	}
	titles := make(map[string]*string, len(read))
	for pageID := range read {
		title := read[pageID]
		titles[pageID] = &title
	}
	pageTitlesAsRead(app, readerID, titles, true)
	pagePreviewNamesWithoutNeverHad(app, readerID, titles)
	subPages := make([]string, 0, len(rows))
	for _, row := range rows {
		subPages = append(subPages, row.PageID)
	}
	wikis, err := loadWikiPageMetadataSet(app, subPages)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		wiki := wikis[row.PageID]
		names[row.ID] = pagePreviewName{name: *titles[row.PageID], pageID: row.PageID, page: true,
			database: stored[row.ID].Type == graphBlockTypeChildPage && (row.Kind == pageKindDatabase || wiki.IsWiki && wiki.DataSourceID != "")}
	}
	return names, nil
}

// pagePreviewNamesWithoutNeverHad puts the names, as read, of the pages whose
// names hold a custom emoji the workspace never had without it: a line of
// the preview says nothing for one, as its text's line (searchTextReader).
func pagePreviewNamesWithoutNeverHad(app core.App, readerID string, titles map[string]*string) {
	ids := make([]string, 0, len(titles))
	for id := range titles {
		ids = append(ids, id)
	}
	named, err := pagesNamedWith(app, ids, func(spans string) string { return spans + ` LIKE '%"mention":"custom_emoji"%'` })
	if err != nil || len(named) == 0 {
		return
	}
	closer := readerCloser(app, readerID)
	for _, id := range named {
		spans, _ := pageNameSpans(app, id)
		read, _ := typedSpansAsRead(spans, closer)
		if title := titles[id]; title != nil && len(read) > 0 && spelledName(read) == *title {
			*title = titleWithoutNeverHadCustomEmoji(*title, read)
		}
	}
}

// pagePreviewSpans is a line's words in pieces when any of them draws other
// than its words — a mention of a page, a database, a person or a date, an
// inline equation, a mark, a color or a link — as Notion's preview draws
// them as in the page (seen 2026-11-01; 2026-11-13: marks, colors and links
// in search's preview and a page's card, an inline equation drawn in the
// card and a chip in search's); nil otherwise. The app cuts the line as a
// short line of formatted text (web/src/lib/richTextSummary.ts): what goes
// stops at twice a line's worth, a mention or an equation whole or not at
// all, "…" after when the line went on. A page or database the reader can't
// open is mentioned without its words, as no access, or as not found in
// marks (unnamedMentionWords).
func pagePreviewSpans(spans []graphTextSpan, closes searchMentionCloser) []pagePreviewSpan {
	drawn := false
	for _, span := range spans {
		drawn = drawn || pagePreviewMentions(span.Mention) || graphTextSpanIsEquation(span) || span.Bold || span.Italic ||
			span.Underline || span.Strikethrough || span.Code || span.Color != "" || span.Background != "" || span.Link != ""
	}
	if !drawn {
		return nil
	}
	pieces := make([]pagePreviewSpan, 0, len(spans))
	left := 2 * pagePreviewLineRunes
	for index, span := range spans {
		text := span.Text
		// A custom emoji the workspace never had says nothing, as the line's
		// text (searchTextReader).
		if customEmojiSpanNeverHad(span) {
			text = ""
		}
		reach := searchMentionReach(closes, span.Mention, span.PageID, span.DatabaseID)
		closed := reach != mentionOpens
		if closed {
			// One nothing answers to keeps its not-found marks, for the app
			// to say so.
			text = ""
			if reach == mentionMissing {
				text = unnamedMentionWords(reach, span.Text, true)
			}
		}
		whole := pagePreviewMentions(span.Mention) || graphTextSpanIsEquation(span)
		if index == 0 && !whole {
			text = strings.TrimLeftFunc(text, unicode.IsSpace)
		}
		if index == len(spans)-1 && !whole {
			text = strings.TrimRightFunc(text, unicode.IsSpace)
		}
		runes := []rune(text)
		cut := len(runes) > left
		if cut && whole {
			return append(pieces, pagePreviewSpan{Text: "…"})
		}
		if cut {
			text = strings.TrimRightFunc(string(runes[:left]), unicode.IsSpace)
		}
		piece := pagePreviewSpan{Text: text, Bold: span.Bold, Italic: span.Italic, Underline: span.Underline,
			Strikethrough: span.Strikethrough, Code: span.Code, Color: span.Color, Background: span.Background, Link: span.Link,
			Equation: span.Equation, EmptyPiece: span.EmptyPiece}
		if pagePreviewMentions(span.Mention) {
			piece.Mention, piece.PageID, piece.DatabaseID, piece.UserID = span.Mention, span.PageID, span.DatabaseID, span.UserID
			piece.Date, piece.DateEnd, piece.DateTimeZone = span.Date, span.DateEnd, span.DateTimeZone
			piece.DateFormat, piece.DateTimeFormat = span.DateFormat, span.DateTimeFormat
			piece.Unavailable = closed
		}
		if text != "" || whole {
			pieces = append(pieces, piece)
		}
		if cut {
			return append(pieces, pagePreviewSpan{Text: "…"})
		}
		left -= len(runes)
	}
	return pieces
}

// pagePreviewMentions holds the mentions a preview draws as in the page.
func pagePreviewMentions(mention string) bool {
	switch mention {
	case "page", "database", "person", "date":
		return true
	}
	return false
}

// pagePreviewLine is a block's text cut to a line's worth.
func pagePreviewLine(text string) string {
	text = strings.TrimSpace(text)
	if runes := []rune(text); len(runes) > pagePreviewLineRunes {
		text = strings.TrimSpace(string(runes[:pagePreviewLineRunes])) + "…"
	}
	return text
}

func pagePreviewString(content map[string]any, key string) string {
	value, _ := content[key].(string)
	return strings.TrimSpace(value)
}
