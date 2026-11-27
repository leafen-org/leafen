package app

import "github.com/pocketbase/pocketbase/core"

// What a title's mention of what is not there shows as Notion's fetch names
// the title at its top (seen 2026-11-22, a reader of Korean: "T18  end" for
// a mention of a data source nothing has, and "T1  end" for one of a source
// there, "Q2 @찾을 수 없음 end" for a page's or a database's no page has,
// "T10 @익명 end" for a person no one is) (mcpSpansAsShown). Its search keeps
// each as any mention, "‣" (mcpTitleAsIndexed).

// mcpTitleMentionShowsNothing is whether a title's mention shows nothing at
// all: a data source's, kept by its address, a source answering to it or
// not (notionKeptDataSourceID, data_source_mentions.go).
func mcpTitleMentionShowsNothing(span graphTextSpan) bool {
	_, source := notionMarkdownDataSource(span)
	return source
}

// mcpTitleMentionMissing is whether a title's page or database mention
// names nothing here: a page of an ID no page has
// (notionUnresolvedPageMention), or a database none is.
func mcpTitleMentionMissing(app core.App, span graphTextSpan) bool {
	if span.Mention == "page" {
		return span.BlockID == "" && notionUnresolvedPageMention(app, span.PageID)
	}
	_, found, err := loadNotionMentionedDatabase(app, span.DatabaseID)
	return err == nil && !found
}

// anonymousMentionLabel is what a person mention of no one shows in a
// language, as Notion's fetch names it in a title.
func anonymousMentionLabel(language string) string {
	if language == accountLanguageKo {
		return "익명"
	}
	return "Anonymous"
}
