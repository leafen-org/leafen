package app

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// A table row's cells stand in a page's Markdown as blocks of their own
// where a selection is looked for, each by its row's ID and its place in the
// row (cell, from 1): a selection of one cell's words is the row's, quoting
// them in that cell (comment_table_cell_anchors.go), as Notion's MCP takes
// one (seen 2026-11-22: "칸 다" of "칸 다 글" in a pipe table's cell or a
// tagged one's, "<tr>⏎<td>⏎칸 다" too, became a discussion on the row with
// the words marked); one running over two cells or beyond a row's words is
// the block where it starts, as before (blockAtPlace). A cell's text is
// written as notionMarkdownTableRowCells writes it, and one whose Markdown
// this can't build, or a pipe table's cell holding a "|", is left to the
// row's words.

// cellBlocks are a table row's cells as blocks of their text in the page's
// Markdown, found at or after cursor; none for a block that is no row.
func (markdown mcpSelectionMarkdown) cellBlocks(rowID, content string, mentions notionMarkdownMentions, cursor *int) []mcpSelectionMarkdownBlock {
	if markdown.types[rowID] != graphBlockTypeTableRow || content == "" {
		return nil
	}
	row := graphTableRowBlockContent{}
	if json.Unmarshal([]byte(content), &row) != nil {
		return nil
	}
	blocks := []mcpSelectionMarkdownBlock{}
	for index, spans := range row.Cells {
		block, inline := mcpSelectionSpansMarkdown(spans, mentions.at(rowID), false)
		block.id, block.cell, block.at, block.shown = rowID, index+1, -1, true
		if inline != "" && !strings.Contains(inline, "|") {
			if at := mcpSelectionTextAt(markdown.text, *cursor, inline); at >= 0 {
				block.at, *cursor = at, at+len(inline)
			}
		}
		blocks = append(blocks, block)
	}
	return blocks
}

// mcpSelectionSpansMarkdown writes a block's text as the page's Markdown
// writes it — as it stands, suggested words left out, its spans as
// notionMarkdownText writes them, a line break as <br /> — knowing which of
// its words each byte writes; none when what it builds isn't what the
// Markdown writes (notionMarkdownText), which leaves the block to its words.
func mcpSelectionSpansMarkdown(spans []graphTextSpan, mentions notionMarkdownMentions, code bool) (mcpSelectionMarkdownBlock, string) {
	block := &mcpSelectionWrittenBlock{}
	standing, runeAt := []graphTextSpan{}, []int{}
	for _, span := range spans {
		count := utf8.RuneCountInString(span.Text)
		if kept := graphTextSpansAsTheyStand([]graphTextSpan{span}, false); len(kept) == 1 {
			standing = append(standing, kept[0])
			for index := range count {
				runeAt = append(runeAt, len(block.raw)+index)
			}
		}
		block.raw = append(block.raw, []rune(span.Text)...)
	}
	if code {
		// A code block's lines are written as they are.
		for index, character := range []rune(graphTextSpansText(standing)) {
			block.add(string(character), runeAt[index], runeAt[index]+1)
		}
		return block.mcpSelectionMarkdownBlock, block.inline()
	}
	unwritten := mcpSelectionMarkdownBlock{raw: block.raw}
	expected, written := notionMarkdownText(standing, mentions)
	if !written || !mentions.Version2 || mentions.APIForm || expected == "" {
		return unwritten, ""
	}
	expected = strings.ReplaceAll(expected, "\n", "<br />")
	at := 0
	for _, span := range notionMarkdownJoinedSpans(standing) {
		count := utf8.RuneCountInString(span.Text)
		first, last := -1, -1
		if count > 0 {
			first, last = runeAt[at], runeAt[at+count-1]+1
		}
		if !block.addSpan(span, runeAt[at:at+count], first, last, mentions) {
			return unwritten, ""
		}
		at += count
	}
	if block.lineBreaks(); block.inline() != expected {
		return unwritten, ""
	}
	return block.mcpSelectionMarkdownBlock, expected
}
