package app

import "golang.org/x/text/unicode/norm"

// A SQL query compares text composed (NFC), the snapshot's values, the
// query's strings and its parameters alike, so text kept decomposed (NFD,
// as a file name a Mac wrote) matches as it reads: "광동이샹" kept as its
// letters' parts is found by =, IN and LIKE as typed (2026-11-16: an import
// left such titles, which = and IN missed and only part of a LIKE found).
// Results give the text composed.
func databaseSQLComposed(value any) any {
	switch typed := value.(type) {
	case string:
		return norm.NFC.String(typed)
	case []any:
		composed := make([]any, len(typed))
		for index, item := range typed {
			composed[index] = databaseSQLComposed(item)
		}
		return composed
	case []string:
		composed := make([]string, len(typed))
		for index, item := range typed {
			composed[index] = norm.NFC.String(item)
		}
		return composed
	case map[string]any:
		composed := make(map[string]any, len(typed))
		for key, item := range typed {
			composed[key] = databaseSQLComposed(item)
		}
		return composed
	}
	return value
}

// A title's or text's value in SQL is its words, as Notion's (seen
// 2026-11-11 and 2026-11-22: "a <mention>page</mention> 끝" reads "a  끝", a
// link its words alone, formatting left out), while words are all it holds
// that an agent needs; one holding a mention, a link or an equation reads
// as the Markdown fetch and rows mode give it (mcpTextCellMarkdown) — its
// words, and each mention, link, equation and mark as fetch writes them —
// so an agent sees what it names and, writing it back as it read it, keeps
// it (user 2026-11-26 "추천대로": an agent tidying some 200 rows read the
// mentions in its memos as blanks, and lost the links of the memos it wrote
// back from SQL). A query compares the value as it gives it.

// databaseSQLTextValue is a title's or text's value as SQL reads it: words,
// its words (mcpSQLTextWords), or its Markdown.
func databaseSQLTextValue(property storedNotionDatabaseRowPageProperty, words any, mentions notionMarkdownMentions) any {
	if !databaseSQLTextHoldsMore(databaseStoredSpans(property.Spans.String)) {
		return words
	}
	if markdown, written := mcpTextCellMarkdown(property, mentions); written {
		return markdown
	}
	return words
}

// databaseSQLTextHoldsMore says spans hold what words alone lose: a
// mention, a link or an equation.
func databaseSQLTextHoldsMore(spans []graphTextSpan) bool {
	for _, span := range spans {
		if span.Mention != "" || span.Link != "" || span.Equation != "" {
			return true
		}
	}
	return false
}
