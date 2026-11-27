package app

import (
	"regexp"
	"strconv"
	"strings"
)

// Content placed by an anchor goes where Notion's MCP puts it (seen
// 2026-11-22, notion-update-page insert_content and notion-create-pages
// position on a page of its own): after_content after the block where the
// anchor ends, at that block's level — so an anchor on a toggle's opening
// tag puts it after the toggle and all it holds, and one on a block inside
// it after that block, inside — and before_content before the block where
// the anchor starts, at its level. The anchor is text copied from the page
// as it reads: it is looked for as written, and it must be found once
// (counting every place it stands, inside blocks too) and start where a
// block starts, at its leading Markdown (`- `, `#`, an opening tag) or at
// its line's start before only the line's indentation; it needn't be a
// whole line. Each refusal is a validation error with its template, as
// Notion's (notion_page_markdown_matches.go), in Leafen's words.

// notionMarkdownAnchorFetchAgain closes an anchor's refusal.
const notionMarkdownAnchorFetchAgain = "\n\nRead the page again and take the anchor from its text as it is now."

// notionMarkdownAnchored tells a position that places content by an anchor.
func notionMarkdownAnchored(positionType string) bool {
	return positionType == "after_content" || positionType == "before_content"
}

// notionMarkdownAnchorTarget is the block content placed by an anchor goes
// beside in a page's Markdown, whose blocks stand at spans.
func notionMarkdownAnchorTarget(markdown string, spans []notionMarkdownBlockSpan, before bool, anchor string) (notionMarkdownBlockSpan, error) {
	// A stored file's address is the page's own (notion_page_markdown_file_matches.go).
	anchor = notionMarkdownFileAddressesRebased(markdown, anchor)
	found := 0
	if anchor != "" {
		found = strings.Count(markdown, anchor)
	}
	switch {
	case found == 0:
		return notionMarkdownBlockSpan{}, pageMarkdownMatchError{
			message: "Nothing in the page matches the anchor " + anchor + ". Copy it from the page as it was read, " +
				"its formatting and escapes too." + notionMarkdownAnchorFetchAgain,
			template: "Nothing in the page matches the anchor {anchor}. Copy it from the page as it was read, " +
				"its formatting and escapes too." + notionMarkdownAnchorFetchAgain,
		}
	case found > 1:
		return notionMarkdownBlockSpan{}, pageMarkdownMatchError{
			message: "The anchor " + anchor + " matches " + strconv.Itoa(found) + " places. Give more of the text around it " +
				"so that only one place matches, or use a `start` or `end` position." + notionMarkdownAnchorFetchAgain,
			template: "The anchor {anchor} matches {matchCount} places. Give more of the text around it " +
				"so that only one place matches, or use a `start` or `end` position." + notionMarkdownAnchorFetchAgain,
		}
	}
	at := strings.Index(markdown, anchor)
	// One inside a table, its rows' cells too, is refused before all else
	// (notion_page_markdown_anchor_sections.go).
	for _, span := range spans {
		if span.Kind == graphBlockTypeTable && span.start < at && at < span.end {
			return notionMarkdownBlockSpan{}, notionMarkdownAnchorInTable(anchor)
		}
	}
	started := -1
	for index, span := range spans {
		if span.start == at || span.line == at && strings.TrimSpace(markdown[span.line:span.start]) == "" {
			started = index
			break
		}
	}
	if started < 0 {
		return notionMarkdownBlockSpan{}, pageMarkdownMatchError{
			message: "The anchor " + anchor + " starts inside a block. Start it where a block starts, with its leading " +
				"Markdown such as `- `, `- [ ] `, `#`, `>` or an opening tag: copy a whole line from the page.",
			template: "The anchor {anchor} starts inside a block. Start it where a block starts, with its leading " +
				"Markdown such as `- `, `- [ ] `, `#`, `>` or an opening tag: copy a whole line from the page.",
		}
	}
	if before {
		return spans[started], notionMarkdownAnchorBeside(spans[started], anchor)
	}
	// After: the innermost block whose lines hold the anchor's last
	// character, the blank lines it ends with left out.
	last := at + len(strings.TrimRight(anchor, " \t\n")) - 1
	target := spans[started]
	for _, span := range spans[started:] {
		if span.start > last {
			break
		}
		if span.start <= last && last < span.end {
			target = span
		}
	}
	return target, notionMarkdownAnchorBeside(target, anchor)
}

// notionMarkdownPlacedBeside is a page's Markdown with content's blocks placed
// before or after a block, at its level: each of their lines indented as its
// line is, a blank line apart from the blocks around them, and none before a
// closing tag after them, as the page's Markdown is written. A numbered list
// they split starts again at 1 after them, as Notion's does (seen
// 2026-11-22: a paragraph placed after "1. a" of "1. a 2. b 3. c" left
// "1. b 2. c"): of a run only its first item's number is read.
func notionMarkdownPlacedBeside(markdown string, span notionMarkdownBlockSpan, before bool, content string) string {
	indentation := span.indentation(markdown)
	lines := strings.Split(strings.Trim(content, "\n"), "\n")
	last := ""
	for index, line := range lines {
		if line != "" {
			if strings.TrimLeft(line, " \t") == line {
				last = line
			}
			lines[index] = indentation + line
		}
	}
	placed := strings.Join(lines, "\n")
	// An item of a list written as its tags splits it (notion_page_markdown_anchor_sections.go).
	if list := notionMarkdownTaggedListOf(markdown, span); list.opening != "" {
		return list.placed(markdown, span, before, placed, last)
	}
	if before {
		head, tail := markdown[:span.line], markdown[span.line:]
		if head != "" && !strings.HasSuffix(head, "\n\n") {
			head += strings.Repeat("\n", 2-min(2, len(head)-len(strings.TrimRight(head, "\n"))))
		}
		return head + placed + "\n\n" + notionMarkdownRunRestarted(tail, indentation, last)
	}
	head, tail := markdown[:span.end], markdown[span.end:]
	if next := strings.TrimLeft(strings.TrimLeft(tail, "\n"), " \t"); tail != "" && !strings.HasPrefix(tail, "\n\n") &&
		!strings.HasPrefix(next, "</") {
		placed += "\n"
	}
	return head + "\n\n" + placed + notionMarkdownRunRestarted(tail, indentation, last)
}

// notionMarkdownNumberedLine is a numbered list item's line: its number and
// what follows it.
var notionMarkdownNumberedLine = regexp.MustCompile(`^([0-9]+)(\. .*)$`)

// notionMarkdownRunRestarted is the Markdown after placed blocks whose last
// top line is last, the numbered item standing next at their level numbered
// 1 when the placed blocks end in no numbered item.
func notionMarkdownRunRestarted(tail, indentation, last string) string {
	if notionMarkdownNumberedLine.MatchString(last) {
		return tail
	}
	at := len(tail) - len(strings.TrimLeft(tail, "\n"))
	end := strings.IndexByte(tail[at:], '\n')
	if end < 0 {
		end = len(tail) - at
	}
	line := tail[at : at+end]
	rest, inside := strings.CutPrefix(line, indentation)
	match := notionMarkdownNumberedLine.FindStringSubmatch(rest)
	if !inside || match == nil || match[1] == "1" {
		return tail
	}
	return tail[:at] + indentation + "1" + match[2] + tail[at+end:]
}
