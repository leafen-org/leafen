package app

import (
	"strings"
	"unicode"

	"github.com/pocketbase/pocketbase/core"
)

// A database's name keeps its formatting, as Notion's does (seen
// 2026-11-07: ⌘B in an inline database's name bolds the words chosen and ⌘K
// opens the link box under it): the spans spell the name, each with its
// marks and link, as a text value's do. A name without formatting keeps
// none. Lists, the sidebar, links and search show the name's text.

// trimGraphTextSpans takes the spaces around a name off its spans, as its
// text is trimmed; spans left empty go.
func trimGraphTextSpans(spans []graphTextSpan) []graphTextSpan {
	trimmed := append([]graphTextSpan(nil), spans...)
	for len(trimmed) > 0 {
		trimmed[0].Text = strings.TrimLeftFunc(trimmed[0].Text, unicode.IsSpace)
		if trimmed[0].Text != "" {
			break
		}
		trimmed = trimmed[1:]
	}
	for len(trimmed) > 0 {
		last := len(trimmed) - 1
		trimmed[last].Text = strings.TrimRightFunc(trimmed[last].Text, unicode.IsSpace)
		if trimmed[last].Text != "" {
			break
		}
		trimmed = trimmed[:last]
	}
	return trimmed
}

// renamedDataSource is a source given another name by the server, as a
// full-page database's page title names it: the words it keeps keep their
// formatting and mentions, as a page's title does renamed
// (respelledPageTitle) and Notion's database renamed from its rename box
// (seen 2026-11-10); the words written are plain.
func renamedDataSource(source graphDataSource, title string) *graphDataSource {
	source.Title, source.TitleSpans = title, respelledPageTitle(source.TitleSpans, title)
	source.TitleOmitted = false
	return &source
}

// notionFormattedTitle is a database's name or description as Notion's API
// gives it to its reader: rich text keeping the formatting stored for it,
// when that spells the text shown, its mentions read as a cell's are
// (notionSpansRichText) — by their target's name now, one the reader can't
// open an "Untitled" page mention — or the text as one plain run.
func notionFormattedTitle(
	app core.App, reader notionBlockAuthorization, files *notionBlockFileCapabilityIssuer,
	title string, spans []graphTextSpan,
) ([]map[string]any, error) {
	var spelled strings.Builder
	for _, span := range spans {
		spelled.WriteString(span.Text)
	}
	if len(spans) == 0 || spelled.String() != title {
		return notionPageTitleRichText(title), nil
	}
	formatted, ok, err := notionSpansRichText(app, reader, spans, files)
	if err != nil || ok {
		return formatted, err
	}
	// Text the official form can't hold reads as its words, a mention the
	// reader can't open "Untitled" in them.
	words, _ := spansAsRead(notionIdentityCloses(app, reader.Identity), spans)
	return notionPageTitleRichText(words), nil
}

// notionNameAsRead is a data source's name as plain words to its reader, as
// Notion's API lists a database's sources: a page or database mention by its
// target's name now, one the reader can't open "Untitled".
func notionNameAsRead(app core.App, identity notionOAuthBearerIdentity, title string, spans []graphTextSpan) string {
	var spelled strings.Builder
	for _, span := range spans {
		spelled.WriteString(span.Text)
	}
	if len(spans) == 0 || spelled.String() != title {
		return title
	}
	closed := notionIdentityCloses(app, identity)
	var words strings.Builder
	for _, span := range spans {
		read := searchMentionWords(closed, span.Mention, span.PageID, span.DatabaseID, span.Text)
		if searchMentionReach(closed, span.Mention, span.PageID, span.DatabaseID) == mentionOpens {
			name, there := "", false
			switch span.Mention {
			case "page":
				name, _, there = pageMentionTargetNow(app, span.PageID)
			case "database":
				name, there, _ = databaseContainerTitle(app, span.DatabaseID)
			}
			if there && name != "" {
				read = name
			}
		}
		words.WriteString(read)
	}
	return words.String()
}
