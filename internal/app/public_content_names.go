package app

import (
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A published page names a page or a database as its visitor reads it, as
// its text names them (public_content_mentions.go): a page or database
// mention in a title — the page's own, its tree's and its search results',
// its site's, a page link's or a mention's — or in a database's name or
// description, a linked one's own name too, that the visitor can't open is
// unnamed (page_name_mentions.go), in one mark, which keeps no title's
// length. The page's title says it "@찾을 수 없음" (closedMentions.ts) and a
// database's name "사용 권한 없음" by the crossed-out eye, as to a member it
// is closed to; the page's head, for a link's preview, "@찾을 수 없음" in
// the request's language. A site's search finds a title without such a
// mention's words, as the app's search does.

// publicReaderCloser is a public page's visitor's closer: a page opens to
// them as a page link to it does, through the same reach
// (public_content_page_links.go).
func publicReaderCloser(app core.App, reach *publicLinkedReach) func() searchMentionCloser {
	var closes searchMentionCloser
	return func() searchMentionCloser {
		if closes == nil {
			closes = memoMentionCloser(func(pageID string) bool {
				open, err := reach.reachable(pageID)
				return err != nil || !open
			}, app)
		}
		return closes
	}
}

// publicVisitorCloser is publicReaderCloser through a reach of its own;
// without an authority read, every page is closed.
func publicVisitorCloser(app core.App, authorization publicVisibilityAuthorization, now time.Time, shownPageIDs ...string) func() searchMentionCloser {
	if authorization.Workspace == nil {
		closes := memoMentionCloser(func(string) bool { return true }, app)
		return func() searchMentionCloser { return closes }
	}
	return publicReaderCloser(app, newPublicLinkedReach(app, authorization, now, shownPageIDs...))
}

// oneMarkPerMention is words with each run of a closed mention's marks one
// mark.
func oneMarkPerMention(words string) string {
	if !holdsMentionMarks(words) {
		return words
	}
	var out strings.Builder
	var run rune
	for _, character := range words {
		if character == closedMentionMark || character == missingMentionMark {
			if character == run {
				continue
			}
			run = character
		} else {
			run = 0
		}
		out.WriteRune(character)
	}
	return out.String()
}

// publicTitleAsRead is a page's title, as written, as a visitor reads it.
func publicTitleAsRead(app core.App, closer func() searchMentionCloser, pageID, title string) string {
	spans, mentions := pageNameSpans(app, pageID)
	if !mentions {
		return title
	}
	read, marked := publicSpansAsRead(spans, closer)
	if !marked {
		return title
	}
	return spelledName(read)
}

// publicSpansAsRead is spans as a visitor reads them: each page or database
// mention they can't open in one mark, and whether any was.
func publicSpansAsRead(spans []graphTextSpan, closer func() searchMentionCloser) ([]graphTextSpan, bool) {
	read, marked := typedSpansAsRead(spans, closer)
	if !marked {
		return spans, false
	}
	for index := range read {
		if read[index].Text != spans[index].Text {
			read[index].Text = oneMarkPerMention(read[index].Text)
		}
	}
	return read, true
}

// publicPageAsRead puts a public page's own title, and an ordinary page's
// title spans, as its visitor reads them, so its header draws a mention
// closed to them as the app does (pagesTitleSpansAsRead).
func publicPageAsRead(app core.App, closer func() searchMentionCloser, page *pageResponse) {
	if readPageKind(page.Kind) == pageKindDatabase && page.Title == "" && !page.ViewBlock {
		page.SourceTitles = publicSourceTitlesAsRead(app, closer, []string{page.ID})[page.ID]
	}
	page.Title = publicTitleAsRead(app, closer, page.ID, page.Title)
	pagesTitleSpansAsReadBy(app, closer, []*pageResponse{page})
	if len(page.TitleSpans) == 0 {
		return
	}
	for index := range page.TitleSpans {
		page.TitleSpans[index].Text = oneMarkPerMention(page.TitleSpans[index].Text)
	}
	page.Title = spelledName(page.TitleSpans)
}

// publicTitlesSpansAsRead is, of the pages, each whose title is spelled by
// a name holding a mention of any kind — a custom emoji's too — that name's
// spans as their visitor reads them (publicSpansAsRead): their screen draws
// its emoji and says its mentions by them, as a member's does
// (TitleWithCustomEmoji.tsx).
func publicTitlesSpansAsRead(app core.App, closer func() searchMentionCloser, pageIDs []string) map[string][]graphTextSpan {
	named, err := pagesNamedWith(app, pageIDs, anyMentionSpansSQL)
	if err != nil || len(named) == 0 {
		return nil
	}
	titles := make(map[string][]graphTextSpan, len(named))
	for _, id := range named {
		spans, _ := pageNameSpans(app, id)
		if len(spans) > 0 {
			titles[id], _ = publicSpansAsRead(spans, closer)
		}
	}
	return titles
}

// publicNameSpansAsRead is a database's name's spans, as written with it,
// as a visitor reads them, while they spell it and hold a mention; none
// otherwise.
func publicNameSpansAsRead(closer func() searchMentionCloser, title string, spans []graphTextSpan) []graphTextSpan {
	held := false
	for _, span := range spans {
		held = held || span.Mention != ""
	}
	if !held || spelledName(spans) != title {
		return nil
	}
	read, _ := publicSpansAsRead(spans, closer)
	return read
}

// publicSpanValues writes spans in the form a public page's graph holds them.
func publicSpanValues(spans []graphTextSpan) []any {
	values := make([]any, len(spans))
	for index, span := range spans {
		values[index] = publicTextSpanValue(span)
	}
	return values
}

// publicNameAsRead is a database's name, as written with its spans, as a
// visitor reads it.
func publicNameAsRead(closer func() searchMentionCloser, title string, spans []graphTextSpan) string {
	return oneMarkPerMention(databaseNameAsRead(closer, title, spans))
}

// publicSummariesAsRead puts the titles of the pages a public page lists —
// its tree, its search results — as its visitor reads them, with the spans
// of each spelled by a name holding a mention (publicTitlesSpansAsRead), as
// the page's own title has them (publicPageAsRead); an untitled full-page
// database of several sources with its sources' titles.
func publicSummariesAsRead(app core.App, closer func() searchMentionCloser, summaries []publicContentPageSummary) {
	publicSummariesSourceTitles(app, closer, summaries)
	titles := make(map[string]*string, len(summaries))
	ids := make([]string, 0, len(summaries))
	for index := range summaries {
		titles[summaries[index].ID] = &summaries[index].Title
		ids = append(ids, summaries[index].ID)
	}
	pageTitlesAsReadBy(app, closer, titles, true)
	spans := publicTitlesSpansAsRead(app, closer, ids)
	for index := range summaries {
		summaries[index].Title = oneMarkPerMention(summaries[index].Title)
		if read := spans[summaries[index].ID]; len(read) > 0 {
			summaries[index].TitleSpans = read
			summaries[index].Title = spelledName(read)
		}
	}
}

// publicReadTitleCustomEmoji is the tokens of the workspace emoji the titles
// of a public read hold — its page's and the pages it lists
// (publicTitleCustomEmoji).
func publicReadTitleCustomEmoji(
	app core.App, source string, authorization publicVisibilityAuthorization, now time.Time,
	page *pageResponse, summaries []publicContentPageSummary,
) (map[string]string, error) {
	titles := make([][]graphTextSpan, 0, len(summaries)+1)
	if page != nil {
		titles = append(titles, page.TitleSpans)
	}
	for _, summary := range summaries {
		titles = append(titles, summary.TitleSpans)
	}
	return publicTitleCustomEmoji(app, source, authorization, now, titles...)
}

// publicSearchFound is, of the pages a site's search found by their titles
// as written, those whose titles as search finds them — without a page or
// database mention's words (searchNameWords) — hold the query.
func publicSearchFound(app core.App, summaries []publicContentPageSummary, rows []storedPublicContentPage, query string) []publicContentPageSummary {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	named, err := pagesNamedWithMentions(app, ids)
	if err != nil || len(named) == 0 {
		return summaries
	}
	unfound := map[string]bool{}
	for _, row := range rows {
		for _, id := range named {
			if id == row.ID && !strings.Contains(strings.ToLower(searchNameWords(app, row.ID, row.Title)), strings.ToLower(query)) {
				unfound[id] = true
			}
		}
	}
	found := summaries[:0]
	for _, summary := range summaries {
		if !unfound[summary.ID] {
			found = append(found, summary)
		}
	}
	return found
}

// publicGraphNamesAsRead puts the names a public page's graph holds as its
// visitor reads them: a data source's name and description, a database
// block's linked name and the name its page's title spells.
func publicGraphNamesAsRead(items []graphReadItem, closer func() searchMentionCloser) {
	for _, item := range items {
		switch item.Kind {
		case graphReadKindDataSource:
			publicGraphNameAsRead(item.Value, "title", "titleSpans", closer)
			publicGraphNameAsRead(item.Value, "description", "descriptionSpans", closer)
		case graphReadKindBlock:
			if content, ok := item.Value["content"].(map[string]any); ok {
				publicGraphNameAsRead(content, "linkedTitle", "linkedTitleSpans", closer)
				publicGraphNameAsRead(content, "", "pageTitleSpans", closer)
			}
		}
	}
}

// publicGraphNameAsRead is graphNameAsRead, each closed mention in one mark.
func publicGraphNameAsRead(value map[string]any, wordsKey, spansKey string, closer func() searchMentionCloser) {
	spans, ok := value[spansKey].([]any)
	if !ok {
		return
	}
	spelled := graphSpansWords(spans)
	if !graphSpansAsRead(spans, closer) {
		return
	}
	for _, raw := range spans {
		if span, ok := raw.(map[string]any); ok && span[publicMentionUnavailableKey] == true {
			span["text"] = oneMarkPerMention(stringField(span, "text"))
		}
	}
	if words, plain := value[wordsKey].(string); plain && words == spelled {
		value[wordsKey] = graphSpansWords(spans)
	}
}

// publicHeadTitle is a public page's title for its head, as a visitor
// reads it, in words: a mention closed to them "@찾을 수 없음" in language.
func publicHeadTitle(app core.App, authorization publicVisibilityAuthorization, now time.Time, title, language string) string {
	read := pageTitleAsReadBy(app, publicVisitorCloser(app, authorization, now), authorization.RootPageID, title)
	return closedMentionsInTitle(read, language)
}

// publicLinkedPagesAsRead puts the titles of pages a public page links or
// mentions as its visitor reads them, an untitled full-page database of
// several sources with its sources' titles, which name it
// (publicSourceTitlesAsRead).
func publicLinkedPagesAsRead(app core.App, closer func() searchMentionCloser, pages map[string]storedPublicLinkedPage) {
	titles := make(map[string]*string, len(pages))
	databases := []string{}
	for id, page := range pages {
		title := page.Title
		titles[id] = &title
		if readPageKind(page.Kind) == pageKindDatabase && page.Title == "" && !page.ViewBlock {
			databases = append(databases, id)
		}
	}
	pageTitlesAsReadBy(app, closer, titles, true)
	for id, title := range titles {
		if page := pages[id]; *title != page.Title {
			page.Title = oneMarkPerMention(*title)
			pages[id] = page
		}
	}
	for id, sources := range publicSourceTitlesAsRead(app, closer, databases) {
		page := pages[id]
		page.SourceTitles = sources
		pages[id] = page
	}
}

// publicSourceTitlesAsRead is, of the pages listed, each untitled full-page
// database's sources' titles while it holds several, as the visitor reads
// them: they name it "A 및 B" on a public page — its own title, the site's
// pages and search, a link or a mention of it — as they name it to a member
// (full_page_database_sources_name.go). One the read fails for is named by
// its kind, as a title the read fails for is left as written.
func publicSourceTitlesAsRead(app core.App, closer func() searchMentionCloser, ids []string) map[string][]databaseSourceTitle {
	titles, err := untitledFullPageDatabaseSourceTitlesAsRead(app, closer, ids)
	if err != nil {
		return nil
	}
	return titles
}

// publicSummariesSourceTitles gives the untitled full-page databases of
// several sources among the pages a public read lists their sources' titles.
func publicSummariesSourceTitles(app core.App, closer func() searchMentionCloser, summaries []publicContentPageSummary) {
	ids := []string{}
	for _, summary := range summaries {
		if summary.Kind == pageKindDatabase && summary.Title == "" {
			ids = append(ids, summary.ID)
		}
	}
	titles := publicSourceTitlesAsRead(app, closer, ids)
	for index := range summaries {
		summaries[index].SourceTitles = titles[summaries[index].ID]
	}
}
