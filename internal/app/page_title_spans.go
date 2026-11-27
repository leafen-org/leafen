package app

import (
	"database/sql"
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An ordinary page's title keeps its mentions, links and formatting, as
// Notion's does (seen 2026-11-10: its API takes a title of text, page,
// database, person and date mentions, an inline equation, a link and
// marks, and gives them all back; its editor's "@" opens the body's mention
// menu in the title, and draws mentions, equations and links there, its
// marks in the title's own look): the spans spell the title (title_spans),
// each with its marks, link or mention, as a database page's title cell's
// do (rowTitleSpans) and a full-page database's name's
// (page_name_mentions.go). Every place that names the page reads them for
// its reader by that one rule. A title without any keeps none, its row as
// it stood; a title written again without spans keeps them while its words
// stay, and loses them with other words.

// titledByOwnSpans reports a page whose title keeps its own spans: one
// neither a database's page nor a full-page database.
func titledByOwnSpans(page *core.Record) bool {
	return page.GetString("data_source_id") == "" && page.GetString("kind") != pageKindDatabase
}

// ordinaryPageTitleSpans is an ordinary page's title spans, while they spell
// its title; none otherwise.
func ordinaryPageTitleSpans(page *core.Record) []graphTextSpan {
	if !titledByOwnSpans(page) {
		return nil
	}
	spans := decodeStoredTextSpans(page.GetString("title_spans"))
	if len(spans) == 0 || spelledName(spans) != page.GetString("title") {
		return nil
	}
	return spans
}

// encodedPageTitleSpans is what a title written with spans keeps of them:
// none for spans that only repeat its words.
func encodedPageTitleSpans(title string, spans []graphTextSpan) (string, error) {
	words, _ := json.Marshal(title)
	kept, err := normalizeGraphRowValueSpans(graphValueTypeTitle, words, spans)
	if err != nil || len(kept) == 0 {
		return "", err
	}
	encoded, err := json.Marshal(kept)
	return string(encoded), err
}

// setPageTitle writes a page's title and its spans: those written with it,
// the ones it keeps for the same words written without spans, none for
// other words.
func setPageTitle(page *core.Record, title string, spans *[]graphTextSpan) {
	title, spans = composedTitle(title, spans) // stored composed (composed_text.go)
	previous := page.GetString("title")
	page.Set("title", title)
	switch {
	case !titledByOwnSpans(page):
		if page.GetString("title_spans") != "" {
			page.Set("title_spans", "")
		}
	case spans != nil:
		encoded, err := encodedPageTitleSpans(title, *spans)
		if err != nil {
			encoded = ""
		}
		page.Set("title_spans", encoded)
	case title != previous:
		// Words written over a title without spans — a rename — keep the
		// formatting and mentions of the words they keep (respelledPageTitle).
		encoded, err := encodedPageTitleSpans(title, respelledPageTitle(ordinaryPageTitleSpansOf(page, previous), title))
		if err != nil {
			encoded = ""
		}
		page.Set("title_spans", encoded)
	}
}

// ordinaryPageTitleSpansOf is the spans a page's title kept while it was
// previous; none when they didn't spell it.
func ordinaryPageTitleSpansOf(page *core.Record, previous string) []graphTextSpan {
	spans := decodeStoredTextSpans(page.GetString("title_spans"))
	if len(spans) == 0 || spelledName(spans) != previous {
		return nil
	}
	return spans
}

// respelledPageTitle is a title's spans written again as words: the words
// before and after what changed keep their spans, a mention or equation only
// while all its words stay; the words written are plain, as the title
// field respells a title written to it (pageTitleEngine.ts).
func respelledPageTitle(spans []graphTextSpan, words string) []graphTextSpan {
	if len(spans) == 0 {
		return nil
	}
	before, after := []rune(spelledName(spans)), []rune(words)
	head := 0
	for head < len(before) && head < len(after) && before[head] == after[head] {
		head++
	}
	tail := 0
	for tail < len(before)-head && tail < len(after)-head && before[len(before)-1-tail] == after[len(after)-1-tail] {
		tail++
	}
	kept := []graphTextSpan{}
	keep := func(from, to int) {
		at := 0
		for _, span := range spans {
			runes := []rune(span.Text)
			start, end := max(from, at), min(to, at+len(runes))
			at += len(runes)
			if start >= end {
				continue
			}
			part := span
			part.Text = string(runes[start-(at-len(runes)) : end-(at-len(runes))])
			if (span.Mention != "" || span.Equation != "") && part.Text != span.Text {
				part = graphTextSpan{Text: part.Text}
			}
			kept = append(kept, part)
		}
	}
	keep(0, head)
	// In what changed, a mention or equation whose words are still there, in
	// order, stays; the words around it are plain.
	middle, at := string(after[head:len(after)-tail]), 0
	plain := func(text string) {
		if text != "" {
			kept = append(kept, graphTextSpan{Text: text})
		}
	}
	spanStart := 0
	for _, span := range spans {
		runes := len([]rune(span.Text))
		inside := spanStart >= head && spanStart+runes <= len(before)-tail
		spanStart += runes
		if !inside || span.Mention == "" && span.Equation == "" {
			continue
		}
		if found := strings.Index(middle[at:], span.Text); found >= 0 && span.Text != "" {
			plain(middle[at : at+found])
			kept = append(kept, span)
			at += found + len(span.Text)
		}
	}
	plain(middle[at:])
	keep(len(before)-tail, len(before))
	return kept
}

// setPageTitleAsKept writes a title kept with its spans, as a version or a
// copy of the page keeps them; spans that don't spell it go.
func setPageTitleAsKept(page *core.Record, title, encodedSpans string) {
	spans := decodeStoredTextSpans(encodedSpans)
	if len(spans) == 0 || spelledName(spans) != title {
		spans = []graphTextSpan{}
	}
	setPageTitle(page, title, &spans)
}

// pageTitleInputAsStored is a title an app writer sends as they read it, as
// it is stored (pageTitleWrittenAsStored): with spans, each mention they
// read in marks named again, as a database's name is (restoreNameAsStored).
func pageTitleInputAsStored(app core.App, writerID, pageID string, input pageMetadataInput) pageMetadataInput {
	if input.TitleSpans == nil {
		input.Title = pageTitleWrittenAsStored(app, writerID, pageID, input.Title)
		return input
	}
	// Words and spans written one trimmed and the other not are both
	// trimmed, as they read alike; written alike, both keep their spaces. No
	// spans are plain words, kept as written: one of spaces alone too.
	spans := *input.TitleSpans
	if len(spans) > 0 && spelledName(spans) != input.Title {
		spans = trimGraphTextSpans(spans)
		if trimmed := strings.TrimSpace(input.Title); spelledName(spans) == trimmed {
			input.Title = trimmed
		}
	}
	if spelledName(spans) != input.Title {
		// Spans that don't spell a title its writer read with marks say
		// nothing of its mentions: it is written as words alone, the marks
		// as stored, never in its text.
		if holdsMentionMarks(input.Title) {
			input.Title, input.TitleSpans = pageTitleWrittenAsStored(app, writerID, pageID, input.Title), nil
			return input
		}
		input.TitleSpans = &spans
		return input
	}
	words, restored := restoreNameAsStored(app, map[string]string{}, nil, input.Title, spans)
	input.Title, input.TitleSpans = words, &restored
	return input
}

// validPageTitleSpansInput reports spans written with a title spelling it,
// each a span text may hold.
func validPageTitleSpansInput(input pageMetadataInput) bool {
	if input.TitleSpans == nil {
		return true
	}
	_, err := encodedPageTitleSpans(input.Title, *input.TitleSpans)
	return err == nil
}

// pageTitleSpansChange reports spans written with a page's title that
// change what it keeps.
func pageTitleSpansChange(page *core.Record, input pageMetadataInput) bool {
	if input.TitleSpans == nil || !titledByOwnSpans(page) {
		return false
	}
	encoded, err := encodedPageTitleSpans(input.Title, *input.TitleSpans)
	return err == nil && encoded != page.GetString("title_spans")
}

// pageTitleSpansRequestHashInput tells a change apart by the spans written
// with its title.
func pageTitleSpansRequestHashInput(input pageMetadataInput) string {
	if input.TitleSpans == nil {
		return ""
	}
	encoded, _ := json.Marshal(*input.TitleSpans)
	return "\x00title-spans-v1\x00" + string(encoded)
}

// pageResponseAsRead puts a page's title, and an ordinary page's title
// spans, as its reader reads them (page_name_mentions.go), and the kept
// Notion people who made and last changed it (notion_imported_authors.go).
func pageResponseAsRead(app core.App, readerID string, page *pageResponse) {
	page.Title = rowTitleAsRead(app, readerID, page.ID, page.Title)
	pagesTitleSpansAsRead(app, readerID, []*pageResponse{page})
	page.ImportedCreatedBy, page.ImportedLastEditedBy = notionImportedAuthorsOf(app, page.ID, page.UpdatedAt)
}

// pagesTitleSpansAsRead puts, on the pages listed for a reader whose title
// keeps spans, those spans as they read them: a page or database mention
// they can't open in marks, as the title's words are (pageTitlesAsRead).
// Their screen draws the title from them. An ordinary page keeps its own
// spans; a database's page, a full-page database or a wiki's page is named
// by its name's (namedPagesTitleSpansAsReadBy), whether or not any ordinary
// page listed with it keeps spans (2026-11-11: search results and
// backlinks of database pages alone said a row title's mention without its
// "@").
func pagesTitleSpansAsRead(app core.App, readerID string, pages []*pageResponse) {
	pagesTitleSpansAsReadBy(app, readerCloser(app, readerID), pages)
}

// pagesTitleSpansAsReadBy is pagesTitleSpansAsRead by a reader's closer.
func pagesTitleSpansAsReadBy(app core.App, closer func() searchMentionCloser, pages []*pageResponse) {
	ordinaryPagesTitleSpansAsReadBy(app, closer, pages)
	namedPagesTitleSpansAsReadBy(app, closer, pages)
}

// ordinaryPagesTitleSpansAsReadBy puts, on the ordinary pages listed whose
// title keeps spans, those spans as the reader's closer reads them.
func ordinaryPagesTitleSpansAsReadBy(app core.App, closer func() searchMentionCloser, pages []*pageResponse) {
	ids := make([]string, 0, len(pages))
	for _, page := range pages {
		ids = append(ids, page.ID)
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
		SELECT [[id]], [[title]], [[title_spans]] FROM {{leafen_pages}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[title_spans]] <> ''
			AND [[data_source_id]] = '' AND [[kind]] <> {:kind}
	`).Bind(dbx.Params{"ids": string(encoded), "kind": pageKindDatabase}).All(&rows) != nil || len(rows) == 0 {
		return
	}
	stored := make(map[string][]graphTextSpan, len(rows))
	for _, row := range rows {
		if spans := decodeStoredTextSpans(row.Spans); len(spans) > 0 && spelledName(spans) == row.Title {
			read, _ := typedSpansAsRead(spans, closer)
			stored[row.ID] = read
		}
	}
	for _, page := range pages {
		if spans, kept := stored[page.ID]; kept {
			page.TitleSpans = spans
		}
	}
}

// pageTitleSpansAsReadBy is one page's name's spans, as a reader's closer
// reads them, while its name holds a mention and they spell its title as
// read; none otherwise.
func pageTitleSpansAsReadBy(app core.App, closer func() searchMentionCloser, pageID, title string) []graphTextSpan {
	spans, _ := pageNameSpans(app, pageID)
	held := false
	for _, span := range spans {
		held = held || span.Mention != ""
	}
	if !held {
		return nil
	}
	if read, _ := typedSpansAsRead(spans, closer); spelledName(read) == title {
		return read
	}
	return nil
}

// namedPagesTitleSpansAsReadBy puts, on a full-page database's page or a
// wiki's page listed whose name holds a mention, its name's spans as the
// reader reads them (pageNameSpans), as an ordinary page's: a plain title
// says each mention by them (closedMentions.ts), and a rename box shows and
// keeps them, as Notion's sidebar, path and rename box do (seen 2026-11-10:
// "W202 DB @W202 대상").
func namedPagesTitleSpansAsReadBy(app core.App, closer func() searchMentionCloser, pages []*pageResponse) {
	byID := make(map[string]*pageResponse, len(pages))
	for _, page := range pages {
		if len(page.TitleSpans) == 0 {
			byID[page.ID] = page
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	named, err := pagesNamedWith(app, ids, anyMentionSpansSQL)
	if err != nil {
		return
	}
	for _, id := range named {
		page := byID[id]
		if page == nil {
			continue
		}
		spans, _ := pageNameSpans(app, id)
		if read, _ := typedSpansAsRead(spans, closer); len(read) > 0 && spelledName(read) == page.Title {
			page.TitleSpans = read
		}
	}
}

// titleSpansAsReadFor is, of the pages whose titles a list names by id —
// each title as its reader reads it — the spans of those whose name holds a
// mention, as that reader reads them, while they spell that title: a list
// that names pages without their spans (an inbox's notices, an
// administrator's lists) carries them so, for a plain
// title to say each mention as Notion's does ("@〈name〉", closedMentions.ts).
func titleSpansAsReadFor(app core.App, closer func() searchMentionCloser, titles map[string]string) map[string][]graphTextSpan {
	found := map[string][]graphTextSpan{}
	ids := make([]string, 0, len(titles))
	for id := range titles {
		if id != "" {
			ids = append(ids, id)
		}
	}
	named, err := pagesNamedWith(app, ids, anyMentionSpansSQL)
	if err != nil {
		return found
	}
	for _, id := range named {
		if spans := pageTitleSpansAsReadBy(app, closer, id, titles[id]); len(spans) > 0 {
			found[id] = spans
		}
	}
	return found
}

// pageTitlesSpansAsRead is titleSpansAsReadFor of the titles a list has
// put as its reader reads them (pageTitlesAsRead), by id.
func pageTitlesSpansAsRead(app core.App, readerID string, titles map[string]*string) map[string][]graphTextSpan {
	plain := make(map[string]string, len(titles))
	for id, title := range titles {
		plain[id] = *title
	}
	return titleSpansAsReadFor(app, readerCloser(app, readerID), plain)
}

// keptPageTitleSpans is what a copy of a page titled title keeps of its
// source's title spans: all of them, with the words a copy's title adds
// after its source's (" (1)") plain; none for a title of other words.
func keptPageTitleSpans(source *core.Record, title string) string {
	spans := ordinaryPageTitleSpans(source)
	spelled := spelledName(spans)
	if len(spans) == 0 || !strings.HasPrefix(title, spelled) {
		return ""
	}
	if added := strings.TrimPrefix(title, spelled); added != "" {
		spans = append(slices.Clone(spans), graphTextSpan{Text: added})
	}
	encoded, err := encodedPageTitleSpans(title, spans)
	if err != nil {
		return ""
	}
	return encoded
}

// rowTitleSpansAsPageTitle is what a database page's title cell keeps of its
// spans, as its title keeps them once it is an ordinary page: all of them,
// while they spell its title.
func rowTitleSpansAsPageTitle(app core.App, rowID string) string {
	row := struct {
		Title string `db:"title"`
		Spans string `db:"spans"`
	}{}
	if app.DB().NewQuery(`
		SELECT [[page]].[[title]], [[value]].[[spans]] FROM {{leafen_pages}} [[page]]
		JOIN {{leafen_database_row_values}} [[value]] ON [[value]].[[row_id]] = [[page]].[[id]]
		JOIN {{leafen_database_properties}} [[property]] ON [[property]].[[id]] = [[value]].[[property_id]]
		WHERE [[page]].[[id]] = {:rowId} AND [[property]].[[type]] = 'title' LIMIT 1
	`).Bind(dbx.Params{"rowId": rowID}).One(&row) != nil || row.Spans == "" {
		return ""
	}
	encoded, err := encodedPageTitleSpans(row.Title, databaseStoredSpans(row.Spans))
	if err != nil {
		return ""
	}
	return encoded
}

// workspaceExportTitleSpansPatch is, for an archive's page, its title's spans
// (a JSON patch): none for a title of plain words, so its value stands as it
// did.
func workspaceExportTitleSpansPatch(page string) string {
	return `CASE WHEN ` + page + `.[[title_spans]] <> '' AND json_valid(` + page + `.[[title_spans]])
		THEN json_object('titleSpans', json(` + page + `.[[title_spans]])) ELSE '{}' END`
}

// archivedPageTitleSpans is what an archived page's title keeps of its
// spans, their pages and people those here (rewriteWorkspaceImportArchiveSpans),
// while they spell its title.
func archivedPageTitleSpans(db *sql.DB, title string, spans []graphTextSpan) ([]graphTextSpan, error) {
	if len(spans) == 0 {
		return nil, nil
	}
	kept, err := rewriteWorkspaceImportArchiveSpans(db, spans)
	if err != nil || spelledName(kept) != title {
		return nil, err
	}
	return kept, nil
}
