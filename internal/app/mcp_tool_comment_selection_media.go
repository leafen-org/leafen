package app

// A selection on a block whose words the page's Markdown doesn't hold — a
// file's, a picture's or a video's line, a divider — is that block's, as
// Notion's MCP puts a comment on the media block whose line it names (seen
// 2026-11-22: a file, a picture and a video line each took a comment on its
// own block). Its place is read from where each block stands in the
// Markdown (notion_page_markdown_block_marks.go): the innermost block whose
// lines hold where the selection starts. Before, it went to the first such
// block after the last block whose words ended before it — the file above a
// video took the video's comment.

// mcpSelectionMarkdownSpans takes the marks out of a page's Markdown read
// with them: the Markdown, and where its blocks stand, none when the marks
// don't read back.
func mcpSelectionMarkdownSpans(marked string, marks *notionMarkdownBlockMarks) (string, []notionMarkdownBlockSpan) {
	markdown, spans, read := notionMarkdownBlockSpansOf(marked, marks)
	if !read {
		return notionMarkdownMarkPattern.ReplaceAllString(marked, ""), nil
	}
	return markdown, spans
}

// blockAtPlace is the block a selection from start to end in the Markdown
// starts in: within the innermost block whose lines hold its start, when
// that block's words aren't placed in the Markdown (a medium, a column, a
// table's row), the block whose words the selection goes on into and ends
// in — as Notion highlights a column's paragraph a selection from the
// column's <column> takes in (seen 2026-11-22) — or else that block; any
// other start as blockAt finds it.
func (markdown mcpSelectionMarkdown) blockAtPlace(start, end int) mcpSelectionMarkdownBlock {
	inner := -1
	for index, span := range markdown.spans {
		if span.line > start {
			break
		}
		if start < span.end {
			inner = index
		}
	}
	if inner >= 0 {
		holder := markdown.spans[inner]
		for _, block := range markdown.blocks {
			if block.id != holder.ID || !block.shown || block.at >= 0 {
				continue
			}
			for _, words := range markdown.blocks {
				if words.shown && words.at >= start && words.at < end && words.at < holder.end {
					if end <= words.end() {
						return words
					}
					break
				}
			}
			return block
		}
	}
	return markdown.blockAt(start)
}
