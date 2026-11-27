package app

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's title is spelled, where its name holds a page or database
// mention, by that name's spans: a database page's title cell's
// (rowTitleSpans), a full-page database's name — the one its block keeps
// for its page (pageTitleSpans, database_page_name_spans.go) or, while it
// holds one source of its own, that source's — or an ordinary page's own
// (title_spans, page_title_spans.go). The title keeps the
// mention's words as written, which whoever may open the target reads.
// Every list, path and title that names the page reads it for its reader as
// their own text reads it (content_graph_reader_mentions.go): a mention
// they can't open in marks, which a plain title shows as "@찾을 수 없음"
// (closedMentions.ts), as Notion's sidebar, path, window title and search
// name such a database (seen 2026-11-10, a guest the mentioned page is
// closed to); its search finds the title without the mention's words
// (workspace_search_page_text.go), as Notion's does.

// mentionSpansSQL says stored spans, an SQL expression, hold a page or
// database mention.
func mentionSpansSQL(spans string) string {
	return `(` + spans + ` LIKE '%"mention":"page"%' OR ` + spans + ` LIKE '%"mention":"database"%')`
}

// anyMentionSpansSQL says stored spans, an SQL expression, hold a mention of
// any kind: a page, database, person or date.
func anyMentionSpansSQL(spans string) string {
	return `(` + spans + ` LIKE '%"mention":%')`
}

// pageNameSpans is the spans a page's title is spelled from, and whether
// they hold a page or database mention.
func pageNameSpans(app core.App, pageID string) ([]graphTextSpan, bool) {
	if spans, mentions := rowTitleSpans(app, pageID); mentions {
		return spans, true
	}
	page := struct {
		Owner  string `db:"owner"`
		Title  string `db:"title"`
		Kind   string `db:"kind"`
		Source string `db:"data_source_id"`
		Spans  string `db:"title_spans"`
	}{}
	if app.DB().NewQuery(`
		SELECT [[owner]], [[title]], [[kind]], [[data_source_id]], [[title_spans]] FROM {{leafen_pages}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": pageID}).One(&page) != nil {
		return nil, false
	}
	var spans []graphTextSpan
	switch {
	case page.Kind == pageKindDatabase:
		spans = databasePageTitleSpans(app, page.Owner, pageID, page.Title)
	case page.Source == "" && page.Spans != "":
		if spans = decodeStoredTextSpans(page.Spans); spelledName(spans) != page.Title {
			spans = nil
		}
	}
	return spans, querySpansHoldMention(spans)
}

// databasePageTitleSpans is the spans a full-page database's page title is
// spelled from: its block's name for its page, or its one source's name,
// while they spell the title; none otherwise.
func databasePageTitleSpans(app core.App, ownerID, pageID, title string) []graphTextSpan {
	block, found, err := loadDatabasePageBlock(app, ownerID, pageID)
	if err != nil || !found {
		return nil
	}
	if spans := databasePageNameSpans([]byte(block.Content), title); spans != nil {
		return spans
	}
	if sourceID := databaseNamingSource([]byte(block.Content)); sourceID != "" {
		return sourceNameSpansForPage(app, sourceID, title)
	}
	return nil
}

// pageTitlesAsRead puts the titles of pages listed for a reader as they read
// them (by page ID), for the few whose name holds a page or database
// mention: one closed to them in marks on a screen, "Untitled" to an app
// that lists them (mcp_reader_mentions.go).
func pageTitlesAsRead(app core.App, readerID string, titles map[string]*string, screen bool) {
	pageTitlesAsReadBy(app, readerCloser(app, readerID), titles, screen)
}

// pageTitlesAsReadBy is pageTitlesAsRead by a reader's closer: a person's,
// a public page's visitor's (publicReaderCloser) or an exporter's.
func pageTitlesAsReadBy(app core.App, closer func() searchMentionCloser, titles map[string]*string, screen bool) {
	ids := make([]string, 0, len(titles))
	for id := range titles {
		ids = append(ids, id)
	}
	named, err := pagesNamedWithMentions(app, ids)
	if err != nil || len(named) == 0 {
		return
	}
	for _, id := range named {
		title := titles[id]
		switch {
		case title == nil:
		case screen:
			*title = pageTitleAsReadBy(app, closer, id, *title)
		default:
			*title = mcpRowTitleAsRead(app, closer(), id, *title)
		}
	}
}

// pagesNamedWithMentions is, of the pages, those whose title is spelled by
// a name holding a page or database mention.
func pagesNamedWithMentions(app core.App, pageIDs []string) ([]string, error) {
	return pagesNamedWith(app, pageIDs, mentionSpansSQL)
}

// pagesNamedWith is, of the pages, those whose title is spelled by a name
// whose spans hold what holds says (an SQL expression of the spans).
func pagesNamedWith(app core.App, pageIDs []string, holds func(spans string) string) ([]string, error) {
	if len(pageIDs) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	named := []string{}
	err = app.DB().NewQuery(`
		SELECT [[value]].[[row_id]] FROM {{leafen_database_row_values}} [[value]]
		JOIN {{leafen_database_properties}} [[property]] ON [[property]].[[id]] = [[value]].[[property_id]]
		WHERE [[value]].[[row_id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[property]].[[type]] = 'title'
			AND ` + holds("[[value]].[[spans]]") + `
		UNION
		SELECT [[page]].[[id]] FROM {{leafen_pages}} [[page]]
		WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[page]].[[kind]] = {:kind} AND (
			EXISTS (SELECT 1 FROM {{leafen_data_sources}} [[source]]
				WHERE [[source]].[[owner]] = [[page]].[[owner]] AND [[source]].[[page_id]] = [[page]].[[id]]
					AND ` + holds("[[source]].[[title_spans]]") + `)
			OR EXISTS (SELECT 1 FROM {{leafen_blocks}} [[block]] INDEXED BY [[idx_leafen_blocks_page_order]]
				WHERE [[block]].[[owner]] = [[page]].[[owner]] AND [[block]].[[page_id]] = [[page]].[[id]]
					AND [[block]].[[parent_id]] = '' AND [[block]].[[type]] = 'inline_database'
					AND [[block]].[[content]] LIKE '%"pageTitleSpans"%' AND ` + holds("[[block]].[[content]]") + `))
		UNION
		SELECT [[page]].[[id]] FROM {{leafen_pages}} [[page]]
		WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[page]].[[kind]] <> {:kind}
			AND [[page]].[[data_source_id]] = '' AND ` + holds("[[page]].[[title_spans]]") + `
	`).Bind(dbx.Params{"ids": string(encoded), "kind": pageKindDatabase}).Column(&named)
	return named, err
}

// pageTitleAsReadBy is a page's title, as written, as a reader's closer
// reads it: its name's mentions closed to them in marks.
func pageTitleAsReadBy(app core.App, closer func() searchMentionCloser, pageID, title string) string {
	spans, mentions := pageNameSpans(app, pageID)
	if !mentions {
		return title
	}
	read, marked := typedSpansAsRead(spans, closer)
	if !marked {
		return title
	}
	return spelledName(read)
}

// databaseNameAsRead is a database's name, as written with its spans, as a
// reader's closer reads it: a mention they can't open in marks, while the
// spans spell the name.
func databaseNameAsRead(closer func() searchMentionCloser, title string, spans []graphTextSpan) string {
	if len(spans) == 0 || spelledName(spans) != title {
		return title
	}
	read, marked := typedSpansAsRead(spans, closer)
	if !marked {
		return title
	}
	return spelledName(read)
}

// databaseNamesAsRead is, of the databases named, each whose first source's
// name holds a mention, that name as its reader reads it, with its spans so
// read: a plain title says its mentions by them (closedMentions.ts).
func databaseNamesAsRead(app core.App, readerID string, containerIDs []string) map[string]databaseNameRead {
	names := map[string]databaseNameRead{}
	encoded, err := json.Marshal(containerIDs)
	if len(containerIDs) == 0 || err != nil {
		return names
	}
	rows := []struct {
		ContainerID string `db:"container_id"`
		Title       string `db:"title"`
		Spans       string `db:"title_spans"`
	}{}
	if app.DB().NewQuery(`
		SELECT [[place]].[[container_id]], [[source]].[[title]], [[source]].[[title_spans]]
		FROM {{leafen_database_container_sources}} [[place]]
		JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[place]].[[data_source_id]]
		WHERE [[place]].[[container_id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND [[place]].[[data_source_id]] = (SELECT [[first]].[[data_source_id]] FROM {{leafen_database_container_sources}} [[first]]
				WHERE [[first]].[[container_id]] = [[place]].[[container_id]]
				ORDER BY [[first]].[[position]], [[first]].[[data_source_id]] LIMIT 1)
			AND `+anyMentionSpansSQL("[[source]].[[title_spans]]")+`
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&rows) != nil {
		return names
	}
	closer := readerCloser(app, readerID)
	for _, row := range rows {
		names[row.ContainerID] = databaseNameSpansAsRead(closer, row.Title, decodeStoredTextSpans(row.Spans))
	}
	return names
}

// databaseNameRead is a database's name as its reader reads it, and its
// spans so read while they spell it; one of the sources naming a database,
// whether it shows that one from another database (Linked), which lists it
// by its linked view's name (database_source_titles.go).
type databaseNameRead struct {
	Title  string
	Spans  []graphTextSpan
	Linked bool
}

// databaseNameSpansAsRead is a name, as written with its spans, as a
// reader's closer reads it, with the spans read so (databaseNameAsRead).
func databaseNameSpansAsRead(closer func() searchMentionCloser, title string, spans []graphTextSpan) databaseNameRead {
	if len(spans) == 0 || spelledName(spans) != title {
		return databaseNameRead{Title: title}
	}
	read, _ := typedSpansAsRead(spans, closer)
	return databaseNameRead{Title: spelledName(read), Spans: read}
}

// sourceNamesAsReadBy puts the names of the data sources listed (by source
// ID) as a reader's closer reads them on a screen: a mention closed to them
// in marks, for the few whose name holds a page or database mention.
func sourceNamesAsReadBy(app core.App, closer func() searchMentionCloser, names map[string]*string) {
	ids := make([]string, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	encoded, err := json.Marshal(ids)
	if len(ids) == 0 || err != nil {
		return
	}
	rows := []struct {
		ID    string `db:"id"`
		Title string `db:"title"`
		Spans string `db:"title_spans"`
	}{}
	if app.DB().NewQuery(`
		SELECT [[id]], [[title]], [[title_spans]] FROM {{leafen_data_sources}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND `+mentionSpansSQL("[[title_spans]]")+`
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&rows) != nil {
		return
	}
	for _, row := range rows {
		if name := names[row.ID]; name != nil && *name == row.Title {
			*name = databaseNameAsRead(closer, row.Title, decodeStoredTextSpans(row.Spans))
		}
	}
}

// mcpSourceNameAsRead is a data source's name as an agent reads it, as all
// it reads (mcp_reader_mentions.go): a page or database mention closed to
// it "Untitled".
func mcpSourceNameAsRead(app core.App, authority mcpAuthority, sourceID, title string) string {
	return sourceNameAsClosed(app, mcpReaderCloses(app, authority), sourceID, title)
}

// sourceNameAsClosed is a data source's name as an API or MCP reader reads
// it: a page or database mention closed to it "Untitled".
func sourceNameAsClosed(app core.App, closed searchMentionCloser, sourceID, title string) string {
	encoded := ""
	if app.DB().NewQuery(`SELECT [[title_spans]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": sourceID}).Row(&encoded) != nil || !strings.Contains(encoded, `"mention"`) {
		return title
	}
	return nameAsClosed(closed, title, decodeStoredTextSpans(encoded))
}

// nameAsClosed is a name, as written with its spans, as an API or MCP
// reader reads it, while the spans spell it.
func nameAsClosed(closed searchMentionCloser, title string, spans []graphTextSpan) string {
	if len(spans) == 0 || spelledName(spans) != title {
		return title
	}
	if words, hid := spansAsRead(closed, spans); hid {
		return words
	}
	return title
}

// mcpDatabaseOwnNameAsRead is a database's own name as an agent reads it:
// a full-page one's, its page's title (pageNameSpans); a linked one's, its
// block's (spans, databaseOwnName).
func mcpDatabaseOwnNameAsRead(app core.App, authority mcpAuthority, pageID, title string, spans []graphTextSpan) string {
	if spans != nil {
		return nameAsClosed(mcpReaderCloses(app, authority), title, spans)
	}
	return mcpRowTitleAsRead(app, mcpReaderCloses(app, authority), pageID, title)
}

// holdsMentionMarks reports words holding the marks a mention closed to
// their reader is read in.
func holdsMentionMarks(words string) bool {
	return strings.ContainsRune(words, closedMentionMark) || strings.ContainsRune(words, missingMentionMark)
}

// withoutMentionMarks is words without such marks.
func withoutMentionMarks(words string) string {
	return strings.Map(func(character rune) rune {
		if character == closedMentionMark || character == missingMentionMark {
			return -1
		}
		return character
	}, words)
}

// pageTitleWrittenAsStored is a page title a writer sends back as they read
// it — with a page's other details, or renamed: their reading of the title
// as it stands is that title; other words keep none of the marks, as a
// renamed name keeps no mention (database_page_name_spans.go), so a mention
// the writer couldn't read is neither stored as marks nor named in words.
func pageTitleWrittenAsStored(app core.App, writerID, pageID, written string) string {
	if !holdsMentionMarks(written) {
		return written
	}
	stored := ""
	_ = app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&stored)
	// Sent as read, the spaces at its ends too, or (from an older app) without them.
	if read := rowTitleAsRead(app, writerID, pageID, stored); stored != "" && (read == written || strings.TrimSpace(read) == written) {
		return stored
	}
	return strings.TrimSpace(withoutMentionMarks(written))
}

// searchPageTitleWords is a full-page database's or an ordinary page's title
// as search finds it: without its name's page or database mentions' words,
// as a database page's title is found (database_row_search.go); others' as
// they stand. An untitled full-page database is found by the name the one
// rule gives it, its sources' names (searchUntitledDatabaseWords).
func searchPageTitleWords(app core.App, page *core.Record) string {
	title := page.GetString("title")
	if isFullPageDatabase(page) {
		if words := searchUntitledDatabaseWords(app, page, title); words != "" {
			return words
		}
		return searchWordsOf(databasePageTitleSpans(app, page.GetString("owner"), page.Id, title), title)
	}
	return searchWordsOf(ordinaryPageTitleSpans(page), title)
}

// searchNameWords is a page's title, as written, as search finds it: without
// the words of the page or database mentions of the name spelling it, nor a
// custom emoji's the workspace never had.
func searchNameWords(app core.App, pageID, title string) string {
	spans, _ := pageNameSpans(app, pageID)
	return searchWordsOf(spans, title)
}

// searchWordsOf is the words spans spell, but a page or database mention's
// and a custom emoji's the workspace never had (customEmojiSpanNeverHad), and
// a date mention's said as search keeps it (searchDateMentionSpanWords),
// while they hold one; the title otherwise.
func searchWordsOf(spans []graphTextSpan, title string) string {
	unsaid := func(span graphTextSpan) bool {
		return searchMentionHasNoWords(span.Mention) || customEmojiSpanNeverHad(span)
	}
	if !slices.ContainsFunc(spans, func(span graphTextSpan) bool { return unsaid(span) || span.Mention == "date" }) {
		return title
	}
	var words strings.Builder
	for _, span := range spans {
		if !unsaid(span) {
			words.WriteString(searchDateMentionSpanWords(span, span.Text))
		}
	}
	return words.String()
}

// searchNameSpansSQL says stored spans, an SQL expression, hold what search
// reads a name otherwise than written (searchWordsOf): a page or database
// mention, a custom emoji mention, of one the workspace never had among them,
// or a date mention.
func searchNameSpansSQL(spans string) string {
	return `(` + mentionSpansSQL(spans) + ` OR ` + spans + ` LIKE '%"mention":"custom_emoji"%' OR ` +
		spans + ` LIKE '%"mention":"date"%')`
}
