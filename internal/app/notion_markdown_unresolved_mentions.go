package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's API and Markdown keep a mention of a page or database that is not
// there, or that the writer can't read, and read it back to the writer as a
// mention of an "Untitled" page by its ID; a data source mention of that kind
// keeps no text. Leafen links a mention of a page or database the writer
// can't read to it all the same (notionMentionPreparation.pageMention), and
// keeps one of nothing here by the ID written in Notion's UUID form,
// "Untitled": it reaches no page and reads as missing to everyone — "찾을 수
// 없음" on a screen, "Untitled" through an API (mcp_reader_mentions.go). A
// page made in the editor has an ID of that form too (2026-11-09: a sub-page
// made with "/페이지"), so a mention is one of these only while no page has
// its ID. A mention kept so before 2026-11-09 for a page that was there but
// closed to its writer stays one of nothing: it names no page by Leafen's
// IDs, and no reader is told what it was. The MCP tools, which name pages by
// Leafen's IDs, keep a mention of nothing as the word "Untitled".

const notionUnresolvedMentionTitle = "Untitled"

// notionUnresolvedPageMention reports whether a page mention names a
// Notion ID written that no page answered to.
func notionUnresolvedPageMention(app core.App, pageID string) bool {
	normalized, ok := normalizeNotionUUIDRequest(pageID)
	if !ok || !strings.EqualFold(normalized, pageID) {
		return false
	}
	found := 0
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&found)
	return err == nil && found == 0
}

// keepUnresolvedPageMention makes a mention an "Untitled" mention of the
// page ID written, keeping its formatting; it reports whether the ID was one.
func keepUnresolvedPageMention(span *graphTextSpan, written string) bool {
	kept, ok := notionKeptUUID(strings.TrimPrefix(written, notionPageMentionRowPrefix))
	if !ok {
		return false
	}
	span.Mention, span.PageID, span.DatabaseID, span.Text = "page", kept, "", notionUnresolvedMentionTitle
	return true
}

// dropUnresolvedDataSourceMention leaves no text where a data source mention
// of nothing the writer may read was, as Notion's pages show it.
func dropUnresolvedDataSourceMention(span *graphTextSpan) {
	*span = graphTextSpan{}
}

// titled is whether a page mention reads by its page's title now, as a pair
// of tags with the title between, none for a page without one, as Notion's
// Markdown writes it in both forms (seen 2026-11-14). One of an ID no page
// answers to reads by its address alone, as Notion's does (seen 2026-11-16:
// '<mention-page url="…"/>', and '<mention url="…" />' in "v2"), as does one
// "v2" names in the trash itself.
func (mentions notionMarkdownMentions) titled(span graphTextSpan) bool {
	return mentions.PageTitle != nil && !mentions.inTrash(span) &&
		(mentions.PageMissing == nil || !mentions.PageMissing(span.PageID))
}

// inTrash is whether "v2" writes a page's or a database's mention by its
// address alone, as one of a target in the trash itself.
func (mentions notionMarkdownMentions) inTrash(span graphTextSpan) bool {
	if !mentions.Version2 || mentions.Targets.in == nil {
		return false
	}
	return mentions.Targets.in(span.Mention, span.PageID+span.DatabaseID)
}
