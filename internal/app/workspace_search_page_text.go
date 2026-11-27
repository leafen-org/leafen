package app

import (
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page's search text: its blocks in line order, its text document and
// its files' text, built for the index (workspace_search_projection.go).

func buildSearchPageProjection(app core.App, dirty storedSearchDirty) (searchPageProjection, error) {
	return buildSearchPage(app, dirty, false, nil)
}

// buildSearchPageAs is a page's search text as a reader some pages are
// closed to reads it (searchMentionCloser): without the line of a sub-page
// closed to it, which it doesn't see on the page, as Notion's page and
// search leave such a sub-page out (seen 2026-11-09); a database page's title
// shown with a mention closed to it "Untitled".
func buildSearchPageAs(app core.App, dirty storedSearchDirty, closed searchMentionCloser) (searchPageProjection, error) {
	return buildSearchPage(app, dirty, false, closed)
}

// buildSearchPageForIndex is a page's entry for the index: a long page from
// what was kept of it when only some blocks' text changed, and kept when made
// whole (workspace_search_memo.go).
func buildSearchPageForIndex(app core.App, dirty storedSearchDirty) (searchPageProjection, error) {
	return buildSearchPage(app, dirty, true, nil)
}

func buildSearchPage(app core.App, dirty storedSearchDirty, index bool, closed searchMentionCloser) (searchPageProjection, error) {
	page, err := app.FindRecordById(pagesCollection, dirty.PageID)
	if errors.Is(err, sql.ErrNoRows) {
		// A database item is indexed beside pages under its row's ID.
		row, found, rowErr := buildSearchRowProjection(app, dirty, closed)
		if rowErr != nil || found {
			return row, rowErr
		}
		// So is a database held in a page's body, under its block's ID
		// (workspace_search_databases.go).
		database, found, databaseErr := buildSearchInlineDatabaseProjection(app, dirty)
		if databaseErr != nil || found {
			return database, databaseErr
		}
		return searchPageProjection{PageID: dirty.PageID, SourceVersion: dirty.Version, Deleted: true}, nil
	}
	if err != nil {
		return searchPageProjection{}, err
	}
	if page.GetString("workspace") != dirty.WorkspaceID {
		return searchPageProjection{}, errors.New(errorCodeWorkspaceSearchProjectionFailed)
	}
	// A database template is indexed by its name and body.
	if isDatabaseTemplatePageRecord(page) {
		return buildSearchTemplateProjection(app, dirty, page)
	}
	// A database page is indexed as its database's item.
	if isDatabaseRowPageRecord(page) {
		row, found, rowErr := buildSearchRowProjection(app, dirty, closed)
		if rowErr != nil || found {
			return row, rowErr
		}
		return searchPageProjection{PageID: dirty.PageID, SourceVersion: dirty.Version, Deleted: true}, nil
	}

	var memo *searchPageMemo
	if index {
		memo = searchPageMemos.take(page.Id)
	}
	if blocks, reordered, changed := dirty.changedBlocks(); changed && memo != nil {
		checking := memo.steps+1 >= searchMemoCheckEvery
		markBefore, _, _ := loadYgoStreamMark(app, ygoCollaborationStream, page.Id)
		projection, err := buildSearchPageIncrement(app, page, dirty, memo, blocks, reordered)
		if !errors.Is(err, errSearchMemoMiss) {
			if err != nil || !checking {
				return projection, err
			}
			// Now and then made whole too, and compared, unless the page
			// changed meanwhile.
			whole, wholeErr := buildSearchPage(app, dirty, true, nil)
			markAfter, _, _ := loadYgoStreamMark(app, ygoCollaborationStream, page.Id)
			if wholeErr == nil && whole.memo != nil && markAfter == markBefore && !whole.memo.sameAs(memo) {
				app.Logger().Warn("search index: a long page made from its changed blocks differed from the page made whole", "pageId", page.Id)
			}
			return whole, wholeErr
		}
	}

	bodyParts := []string{}
	head := page.GetString("content")
	if head != "" {
		bodyParts = append(bodyParts, head)
	}
	// The text blocks' and sub-pages' texts in reading order, for a long page
	// kept; a page with files' words isn't.
	order := []searchBodyPart{}
	files := false
	doc, found, done, err := borrowYgoStreamDocument(app, ygoCollaborationStream, page.Id)
	if err != nil {
		return searchPageProjection{}, err
	}
	defer done()
	// A file block's name and its document's words are read where it stands.
	fileTexts, err := searchFileTexts(app, page.Id)
	if err != nil {
		return searchPageProjection{}, err
	}
	partial := false
	// The page's structural document holds its blocks: its sub-pages and
	// databases, whose names are lines of it, even before it has any text —
	// a page an API, an agent or an import gave sub-pages alone has no live
	// document (2026-11-11: such a parent wasn't found by its new sub-page's
	// title, as Notion's search finds it).
	structural, structuralFound, structuralDone, err := borrowYgoStreamDocument(app, ygoStructuralStream, page.Id)
	if err != nil {
		return searchPageProjection{}, err
	}
	defer structuralDone()
	if !found && !structuralFound {
		// A page with no document yet, such as one an API filled with files
		// alone, still has its documents' words, in its blocks' order.
		for _, id := range searchFileBlockOrder(app, page.Id, fileTexts) {
			bodyParts = appendSearchFileText(bodyParts, fileTexts[id], &partial)
		}
	}
	if found || structuralFound {
		indexed, children, err := searchPageIndexed(app, page.Id)
		if err != nil {
			return searchPageProjection{}, err
		}
		for id := range fileTexts {
			indexed = append(indexed, id)
		}
		// The body's lines and highlights come in the page's line order
		// (searchBlocksInLineOrder), which the page's structural document holds.
		orderDoc := doc
		if structuralFound {
			orderDoc = structural
		}
		// A person mention is kept with the person's name now, a page or database
		// mention without words, a date in English (workspace_search_date_words.go).
		read := searchIndexTextReader(app, dirty.WorkspaceID)
		for _, id := range searchBlocksInLineOrder(orderDoc, indexed) {
			if child, isChild := children[id]; isChild {
				if searchMentionClosed(closed, "page", child.PageID, "") {
					continue
				}
				bodyParts = append(bodyParts, child.Title)
				order = append(order, searchBodyPart{id: id, text: child.Title})
			} else if text, file := fileTexts[id]; file {
				bodyParts = appendSearchFileText(bodyParts, text, &partial)
				files = true
			} else if found {
				// A block's text is in the live document alone.
				text := searchBlockText(read(doc.GetText(ygoBlockTextRootPrefix + id)))
				if text != "" {
					bodyParts = append(bodyParts, text)
				}
				order = append(order, searchBodyPart{id: id, text: text})
			}
		}
	}

	if index && found && !files && !partial {
		size := len(bodyParts)
		for _, part := range bodyParts {
			size += len(part)
		}
		if size > searchMemoMinBytes {
			memo := newSearchPageMemo(head, order)
			if memo.bodyRunes() > maxWorkspaceSearchBodyRunes {
				return searchPageProjection{}, errors.New(errorCodeWorkspaceSearchDocumentTooLarge)
			}
			projection := searchPageProjection{
				PageID: page.Id, WorkspaceID: dirty.WorkspaceID, OwnerID: page.GetString("owner"),
				Title: page.GetString("title"), TitleFolded: foldSearchText(searchPageTitleWords(app, page)), SourceVersion: dirty.Version,
			}
			memo.project(&projection)
			return projection, nil
		}
	}
	body := strings.Join(bodyParts, "\n")
	if utf8.RuneCountInString(body) > maxWorkspaceSearchBodyRunes {
		return searchPageProjection{}, errors.New(errorCodeWorkspaceSearchDocumentTooLarge)
	}
	return searchPageProjection{
		PageID: page.Id, WorkspaceID: dirty.WorkspaceID, OwnerID: page.GetString("owner"),
		Title: page.GetString("title"), TitleFolded: foldSearchText(searchPageTitleWords(app, page)),
		Body: body, BodyFolded: foldSearchText(body), SourceVersion: dirty.Version, Partial: partial,
	}, nil
}

// searchPageIndexed is a page's text blocks and the sub-page and database
// blocks whose names are lines of it, with those lines, read without their
// text.
func searchPageIndexed(app core.App, pageID string) ([]string, map[string]searchChildLine, error) {
	cursor := ""
	indexed := []string{}
	for {
		blocks := []storedSearchBlock{}
		err := app.DB().NewQuery(`
			SELECT [[id]]
			FROM {{leafen_blocks}} INDEXED BY [[idx_leafen_blocks_search_projection]]
			WHERE [[page_id]] = {:pageId} AND [[type]] IN (SELECT [[value]] FROM json_each({:types}))
				AND [[id]] > {:cursor}
			ORDER BY [[id]]
			LIMIT {:limit}
		`).Bind(dbx.Params{
			"pageId": pageID, "types": searchTextBlockTypesJSON, "cursor": cursor, "limit": ygoLegacySeedWindow,
		}).All(&blocks)
		if err != nil {
			return nil, nil, err
		}
		for _, block := range blocks {
			indexed = append(indexed, block.ID)
		}
		if len(blocks) < ygoLegacySeedWindow {
			break
		}
		cursor = blocks[len(blocks)-1].ID
	}
	named, lines, err := searchPageChildLines(app, pageID)
	if err != nil {
		return nil, nil, err
	}
	return append(indexed, named...), lines, nil
}

// searchPassageWithoutSubPages is a found page's passage without its
// sub-pages' titles (searchPageChildLines) when its own title holds every
// keyword: Notion's row for such a page shows no sub-page as its line, only
// its text's (seen 2026-11-14: "w925qx" finds "w925qx 부모 가" with no line,
// though its sub-page "w925qx 자식 가" is one of its lines, and "w925qx 부모
// 다" with its paragraph's; "w925qx 자식" shows the sub-page's line under
// "w925qx 부모 가", which holds only some of the keywords). A page found by
// a sub-page's title alone still is, with that line ("zorvak").
func searchPassageWithoutSubPages(app core.App, pageID, title string, keywords workspaceSearchKeywords, passage workspaceSearchPassage) (workspaceSearchPassage, error) {
	if len(passage) == 0 {
		return passage, nil
	}
	if all, _ := keywords.match(title); !all {
		return passage, nil
	}
	sub, err := searchSubPageTitles(app, pageID)
	if err != nil || len(sub) == 0 {
		return passage, err
	}
	kept := make(workspaceSearchPassage, 0, len(passage))
	for _, block := range passage {
		if !sub[strings.TrimSpace(block.text)] {
			kept = append(kept, block)
		}
	}
	return kept, nil
}

// searchSubPageTitles is the titles of a page's sub-pages, and of a wiki's
// pages its home links, as its search text's lines write them
// (searchPageChildLines).
func searchSubPageTitles(app core.App, pageID string) (map[string]bool, error) {
	titles := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[child]].[[title]]
		FROM {{leafen_blocks}} [[block]] INDEXED BY [[idx_leafen_blocks_search_projection]]
		JOIN {{leafen_pages}} [[child]] ON [[child]].[[id]] = json_extract([[block]].[[content]], '$.childPageId')
		WHERE [[block]].[[page_id]] = {:pageId} AND [[block]].[[type]] IN ({:type}, {:link})
			AND ([[block]].[[type]] = {:type} OR ` + wikiPageLinkSQL("[[block]]") + `)
	`).Bind(dbx.Params{"pageId": pageID, "type": graphBlockTypeChildPage, "link": graphBlockTypeLinkToPage}).Column(&titles); err != nil {
		return nil, err
	}
	sub := make(map[string]bool, len(titles))
	for _, child := range titles {
		sub[strings.TrimSpace(searchLineShown(searchBlockText(child)))] = true
	}
	return sub, nil
}

// searchFileBlockOrder lists a page's file blocks with a name or words in
// line order, by the blocks' places.
func searchFileBlockOrder(app core.App, pageID string, texts map[string]searchFileText) []string {
	if len(texts) == 0 {
		return nil
	}
	ordered, _, err := searchPlacedBlocksInLineOrder(app, pageID)
	if err != nil {
		return nil
	}
	ids := []string{}
	for _, id := range ordered {
		if _, ok := texts[id]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// appendSearchFileText adds a file block's name as its line of the body,
// then its document's words while the body stays within half of what an
// entry may hold — the page's own words come first — and the beginning of
// one that doesn't fit; partial notes a document whose words are not all in.
func appendSearchFileText(parts []string, file searchFileText, partial *bool) []string {
	if file.Name != "" {
		parts = append(parts, file.Name)
	}
	if file.Partial {
		*partial = true
	}
	if file.Text == "" {
		return parts
	}
	total := 0
	for _, part := range parts {
		total += utf8.RuneCountInString(part) + 1
	}
	room := maxWorkspaceSearchBodyRunes/2 - total
	if length := utf8.RuneCountInString(file.Text); length > room {
		*partial = true
		if room <= 0 {
			return parts
		}
		return append(parts, string([]rune(file.Text)[:room]))
	}
	return append(parts, file.Text)
}

// searchBlocksInReadingOrder orders a page's text blocks as the page shows
// them, each before the blocks inside it; any its structure doesn't reach
// follow in their own order.
func searchBlocksInReadingOrder(doc *crdt.Doc, ids []string) []string {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	ordered := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	var visit func(parentID string) bool
	visit = func(parentID string) bool {
		children, ok := searchStructureChildren(doc, parentID)
		if !ok {
			return false
		}
		for _, id := range children {
			if _, repeated := seen[id]; repeated {
				continue
			}
			seen[id] = struct{}{}
			if _, isText := wanted[id]; isText {
				ordered = append(ordered, id)
			}
			if !visit(id) {
				return false
			}
		}
		return true
	}
	if !visit("") {
		return ids
	}
	for _, id := range ids {
		if _, reached := seen[id]; !reached {
			ordered = append(ordered, id)
		}
	}
	return ordered
}

// searchBlocksInLineOrder orders a page's text blocks as its search lines
// stand, as Notion's search lists a page's lines and highlights (seen
// 2026-11-11 on a page and on database items, made at once and a block at a
// time: "first, alpha, beta, after, alpha one, alpha two, beta one, alpha one
// deep"): its top-level blocks, then the blocks inside them, a level at a
// time, each level in the page's order; any its structure doesn't reach
// follow in their own order.
func searchBlocksInLineOrder(doc *crdt.Doc, ids []string) []string {
	return searchLevelOrder(ids, func(parentID string) ([]string, bool) { return searchStructureChildren(doc, parentID) })
}

// searchLevelOrder orders blocks a level at a time from the top, by the
// blocks each one holds; false from children, the blocks are kept as they
// came.
func searchLevelOrder(ids []string, children func(parentID string) ([]string, bool)) []string {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	ordered := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for level := []string{""}; len(level) > 0; {
		next := []string{}
		for _, parentID := range level {
			held, ok := children(parentID)
			if !ok {
				return ids
			}
			for _, id := range held {
				if _, repeated := seen[id]; repeated {
					continue
				}
				seen[id] = struct{}{}
				if _, isText := wanted[id]; isText {
					ordered = append(ordered, id)
				}
				next = append(next, id)
			}
		}
		level = next
	}
	for _, id := range ids {
		if _, reached := seen[id]; !reached {
			ordered = append(ordered, id)
		}
	}
	return ordered
}

// searchPlacedBlocksInLineOrder is a page's blocks in line order
// (searchBlocksInLineOrder) by their places, for a body without a structural
// document: a database page's, or a page's an API gave files alone; and
// those of them that hold no words (graphBlockWordless).
func searchPlacedBlocksInLineOrder(app core.App, pageID string) ([]string, map[string]bool, error) {
	placed := []struct {
		ID       string `db:"id"`
		ParentID string `db:"parent_id"`
		Type     string `db:"type"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]], [[type]] FROM {{leafen_blocks}}
		WHERE [[page_id]] = {:pageId} ORDER BY [[parent_id]], [[position]], [[id]]
	`).Bind(dbx.Params{"pageId": pageID}).All(&placed); err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(placed))
	held := map[string][]string{}
	wordless := map[string]bool{}
	for _, block := range placed {
		ids = append(ids, block.ID)
		held[block.ParentID] = append(held[block.ParentID], block.ID)
		if graphBlockWordless(block.Type) {
			wordless[block.ID] = true
		}
	}
	return searchLevelOrder(ids, func(parentID string) ([]string, bool) { return held[parentID], true }), wordless, nil
}

// searchStructureChildren is a block's children in the page's structure, read
// as structuralOrder reads them but not checked for repeats, which the walks
// of search pass over: a map for each block was most of what a long page's
// order cost to read.
func searchStructureChildren(doc *crdt.Doc, parentID string) ([]string, bool) {
	values := doc.GetArray(blockOrderRoot(parentID)).ToSlice()
	if len(values) == 0 {
		return nil, true
	}
	children := make([]string, len(values))
	for index, value := range values {
		id, ok := value.(string)
		if !ok || id == "" {
			return nil, false
		}
		children[index] = id
	}
	return children, true
}
