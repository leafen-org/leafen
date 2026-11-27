package app

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A page's Markdown may be written with each block's place marked, to find
// where a block's lines start and where they end with the blocks it holds —
// what Notion's MCP anchors place content by (notion_page_markdown_anchor.go).
// A writer with marks puts an unprintable mark of the block's number after
// the indentation of its first line, where its leading Markdown (`- `, `#`,
// an opening tag) starts, and another at the end of its last line; the
// marks are taken out again, and the Markdown read without them is the one
// any reader of the page reads.

// notionMarkdownBlockMarks are the blocks a writer has marked, by number,
// with their kinds (a block's type; notionMarkdownTabSection for a tab of a
// tabs block).
type notionMarkdownBlockMarks struct {
	ids, kinds []string
}

const (
	notionMarkdownMarkStart, notionMarkdownMarkStartEnd = "\U000FFFF0", "\U000FFFF1"
	notionMarkdownMarkEnd, notionMarkdownMarkEndEnd     = "\U000FFFF2", "\U000FFFF3"
)

var notionMarkdownMarkPattern = regexp.MustCompile("\U000FFFF0([0-9]+)\U000FFFF1|\U000FFFF2([0-9]+)\U000FFFF3")

// markedBlock writes a block as block does, marked when the writer marks.
func (w *notionMarkdownWriter) markedBlock(block workspaceExportMarkdownBlock, prefix string, number int) ([]string, notionMarkdownListFamily) {
	lines, family := w.block(block, prefix, number)
	return w.marked(block.ID, block.Type, prefix, lines), family
}

// marked marks a block's lines, prefix their indentation, when the writer
// marks.
func (w *notionMarkdownWriter) marked(blockID, kind, prefix string, lines []string) []string {
	if w.marks == nil || len(lines) == 0 {
		return lines
	}
	lines = slices.Clone(lines)
	number := strconv.Itoa(len(w.marks.ids))
	w.marks.ids, w.marks.kinds = append(w.marks.ids, blockID), append(w.marks.kinds, kind)
	first := strings.TrimPrefix(lines[0], prefix)
	lines[0] = lines[0][:len(lines[0])-len(first)] + notionMarkdownMarkStart + number + notionMarkdownMarkStartEnd + first
	lines[len(lines)-1] += notionMarkdownMarkEnd + number + notionMarkdownMarkEndEnd
	return lines
}

// notionMarkdownBlockSpan is where a block stands in a page's Markdown: the
// start of its line, where its leading Markdown starts after the line's
// indentation, and the end of its last line, the blocks it holds included.
type notionMarkdownBlockSpan struct {
	ID, Kind         string
	line, start, end int
}

// indentation is what stands before the block's leading Markdown on its
// line.
func (span notionMarkdownBlockSpan) indentation(markdown string) string {
	return markdown[span.line:span.start]
}

// notionMarkdownBlockSpansOf takes the marks out of marked Markdown: the
// Markdown without them, and where each marked block stands in it, in the
// order the blocks start.
func notionMarkdownBlockSpansOf(marked string, marks *notionMarkdownBlockMarks) (string, []notionMarkdownBlockSpan, bool) {
	var clean strings.Builder
	spans := make([]notionMarkdownBlockSpan, len(marks.ids))
	seen := make([]int, len(marks.ids))
	at, lineStart := 0, 0
	write := func(text string) {
		if newline := strings.LastIndexByte(text, '\n'); newline >= 0 {
			lineStart = clean.Len() + newline + 1
		}
		clean.WriteString(text)
	}
	for _, match := range notionMarkdownMarkPattern.FindAllStringSubmatchIndex(marked, -1) {
		write(marked[at:match[0]])
		at = match[1]
		ending, digits := match[2] < 0, ""
		if ending {
			digits = marked[match[4]:match[5]]
		} else {
			digits = marked[match[2]:match[3]]
		}
		number, err := strconv.Atoi(digits)
		if err != nil || number >= len(spans) {
			return "", nil, false
		}
		if ending {
			spans[number].end = clean.Len()
			seen[number] |= 2
			continue
		}
		spans[number] = notionMarkdownBlockSpan{ID: marks.ids[number], Kind: marks.kinds[number], line: lineStart, start: clean.Len()}
		seen[number] |= 1
	}
	write(marked[at:])
	for _, both := range seen {
		if both != 3 {
			return "", nil, false
		}
	}
	slices.SortStableFunc(spans, func(left, right notionMarkdownBlockSpan) int { return left.start - right.start })
	return clean.String(), spans, true
}
