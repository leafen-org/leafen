package app

import (
	"regexp"
	"strings"
)

// Lines written a few columns in nest as Notion's "v2" and its MCP's
// Markdown read them (seen 2026-11-10, as CommonMark reads them): a list
// item's blocks stand at least as far in as its text starts — two columns
// for "- " and "- [ ] ", three for "1. ", four for "10. " — and a line less
// far in is no longer the item's; a line four columns further in than
// where its blocks start goes on with the text before it, or is text of its
// own, read as written. What > lines hold is blocks: the first, when text,
// is the quote's text and the others its blocks, and lines of text one
// after another, a line further in or one without its >, go on with the
// text the quote ends in. A list item's text that starts another block holds
// that block, its own text empty ("- - b", "1. > q"), save a to-do's; $$
// that no later $$ closes is an item's or a quote's text ("- $$"). An
// item or a quote without text is an empty one. Lines Leafen writes a tab a
// level read at that level as they are (notionMarkdownFlow). A "v2" item
// numbered with ")" reads as one with "." (notionMarkdownVersion2Input).

var (
	notionMarkdownFlowListMarker = regexp.MustCompile(`^(?:[-*+]|[0-9]{1,9}\.)(?:[ \t]+|$)`)
	notionMarkdownFlowTodo       = regexp.MustCompile(`^\[[ xX]\](?:[ \t]|$)`)
	notionMarkdownFlowThematic   = regexp.MustCompile(`^(?:(?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$`)
)

// notionMarkdownFlowItem is a list item a line starts: its marker, as the
// reader takes it, and how far its text stands in from the marker.
func notionMarkdownFlowItem(trimmed string) (marker string, width int, ok bool) {
	found := notionMarkdownFlowListMarker.FindString(trimmed)
	if found == "" || notionMarkdownFlowThematic.MatchString(trimmed) {
		return "", 0, false
	}
	mark := strings.TrimRight(found, " \t")
	width = len(found)
	if width == len(mark) {
		// An empty item's blocks stand a column past its marker.
		width++
	}
	return mark, width, true
}

// notionMarkdownFlowStarts tells text that starts a block of its own rather
// than going on with text before it; afterItem lets a numbered item start
// from any number, as one after a list item or as an item's first block.
func notionMarkdownFlowStarts(trimmed, next string, afterItem bool) bool {
	return notionMarkdownVersion2Starts(trimmed, next, afterItem) || strings.HasPrefix(trimmed, "```") ||
		strings.HasPrefix(trimmed, "~~~") || trimmed == "$$" || notionMarkdownFlowThematic.MatchString(trimmed)
}

// notionMarkdownFlowItemHolds tells an item's text that is another block,
// which the item holds: a list item, a quote, a heading, a divider, code or
// an equation.
func notionMarkdownFlowItemHolds(text string) bool {
	if _, _, item := notionMarkdownFlowItem(text); item {
		return true
	}
	return strings.HasPrefix(text, ">") || notionMarkdownVersion2Heading.MatchString(text) ||
		notionMarkdownFlowThematic.MatchString(text) || strings.HasPrefix(text, "```") ||
		strings.HasPrefix(text, "~~~") || text == "$$"
}

// notionMarkdownFlowTakesText tells a line whose text a next line of text
// goes on with: a paragraph's, a list item's, a to-do's or a quote's, the
// innermost one a line of > and items holds.
func notionMarkdownFlowTakesText(trimmed string) bool {
	for {
		switch {
		case strings.HasPrefix(trimmed, ">"):
			trimmed = strings.TrimSpace(trimmed[1:])
			continue
		}
		if _, width, item := notionMarkdownFlowItem(trimmed); item {
			if width > len(trimmed) {
				return false
			}
			trimmed = strings.TrimSpace(trimmed[width:])
			if notionMarkdownFlowTodo.MatchString(trimmed) {
				return true
			}
			continue
		}
		break
	}
	return trimmed != "" && !notionMarkdownFlowStarts(trimmed, "", false) && !notionMarkdownEmptyParagraph.MatchString(trimmed)
}

// notionMarkdownFlowLiteral is a line read as written, as its text: what
// would start a block escaped.
func notionMarkdownFlowLiteral(trimmed string) string {
	if escaped := notionMarkdownVersion2Paragraph(trimmed); escaped != trimmed {
		return escaped
	}
	if notionMarkdownFlowStarts(trimmed, "", true) || strings.HasPrefix(trimmed, "|") {
		return `\` + trimmed
	}
	return trimmed
}

// notionMarkdownFlowJoined puts lines of text one after another into one,
// joined by line breaks: a line goes on with the one before it when that
// takes text and it starts no block. Code and equations are as written; $$
// that no later $$ closes starts text.
func notionMarkdownFlowJoined(lines []string) []string {
	out := make([]string, 0, len(lines))
	fence, equation, takes, item, table := "", false, false, false, false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			out = append(out, line)
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		}
		if equation || trimmed == "$$" && notionMarkdownEquationCloses(lines[index+1:]) {
			out, equation, takes = append(out, line), equation != (trimmed == "$$"), false
			continue
		}
		next := ""
		if index+1 < len(lines) {
			next = strings.TrimSpace(lines[index+1])
		}
		// A line starting with a pipe is text, but for a table's row (seen
		// 2026-11-14: "> |a|⏎> |b|" is the quote "|a|⏎|b|", "> |a|⏎> |---|"
		// a table), which takes no text.
		row := strings.HasPrefix(trimmed, "|") && (table || workspaceImportTableSeparatorPattern.MatchString(next))
		table = row
		if takes && trimmed != "" && !row && !notionMarkdownFlowStarts(trimmed, next, item) {
			out[len(out)-1] = notionMarkdownVersion2Break(out[len(out)-1]) + "<br>" + trimmed
			continue
		}
		out = append(out, line)
		if strings.HasPrefix(trimmed, "```") {
			fence, takes = notionMarkdownFenceOf(trimmed), false
			continue
		}
		takes = !row && (notionMarkdownFlowTakesText(trimmed) || trimmed == "$$")
		var width int
		_, width, item = notionMarkdownFlowItem(trimmed)
		// An item whose text is $$ that no later $$ closes takes text too, as
		// $$ alone does (seen 2026-11-14: "> - $$⏎>   b" is the item "$$⏎b").
		takes = takes || item && width <= len(trimmed) && strings.TrimSpace(trimmed[width:]) == "$$" &&
			!notionMarkdownEquationCloses(lines[index+1:])
	}
	return out
}

// notionMarkdownFlowQuote reads a quote whose > stands at lines[start]: its
// lines as Leafen reads them at level, and the lines it took. base is the
// column its blocks start at, the item's text it stands in or the left.
func notionMarkdownFlowQuote(lines []string, start, base, level int) ([]string, int) {
	inner, fence, equation := []string{}, "", false
	end := start
	for ; end < len(lines); end++ {
		line := lines[end]
		trimmed := strings.TrimSpace(line)
		column := len(line) - len(strings.TrimLeft(line, " "))
		if end > start {
			if trimmed == "" || strings.HasPrefix(line, "\t") || column < base {
				break
			}
			// A line ending in its color, as Leafen wrote quotes before, is one;
			// an equation's lines are its words as written (seen 2026-11-15: "> $$⏎
			// > x {color=\"red\"}⏎> $$" and "> $$⏎> x⏎> $$ {color=\"red\"}⏎> $$" are a
			// quote holding the equation, in an item or a quote too).
			if !equation && (notionMarkdownBlockColorPattern.MatchString(strings.TrimSpace(lines[end-1])) ||
				notionMarkdownBlockColorPattern.MatchString(trimmed)) {
				break
			}
			if !strings.HasPrefix(trimmed, ">") || column > base+3 {
				// Text without its > goes on with the text the quote ends in,
				// as does a line four columns in, which starts nothing; a
				// numbered item standing out of the quote starts from any
				// number (notionMarkdownVersion2Place).
				last := strings.TrimSpace(inner[len(inner)-1])
				if fence != "" || !notionMarkdownFlowTakesText(last) || column < base+4 &&
					(notionMarkdownFlowStarts(trimmed, "", false) || notionMarkdownVersion2Number.MatchString(trimmed)) {
					break
				}
				inner[len(inner)-1] = notionMarkdownVersion2Break(inner[len(inner)-1]) + "<br>" + trimmed
				continue
			}
		}
		// What ends it stays for a line break after it to read
		// (notionMarkdownVersion2Break): its tabs, or a backslash before spaces.
		held := strings.TrimPrefix(strings.TrimLeft(line, " \t")[1:], " ")
		if inside := strings.TrimSpace(held); fence != "" {
			if strings.HasPrefix(inside, fence) && strings.Trim(inside, "`") == "" {
				fence = ""
			}
		} else if strings.HasPrefix(inside, "```") {
			fence = notionMarkdownFenceOf(inside)
		} else {
			equation = notionMarkdownFlowQuoteEquation(equation, inside, lines[end+1:])
		}
		inner = append(inner, held)
	}
	inner = notionMarkdownFlowJoined(inner)
	first := 0
	for first < len(inner) && strings.TrimSpace(inner[first]) == "" {
		first++
	}
	text, blocks := "", inner
	if first < len(inner) {
		lead := len(inner[first]) - len(strings.TrimLeft(inner[first], " "))
		trimmed, next := strings.TrimSpace(inner[first]), ""
		if first+1 < len(inner) {
			next = strings.TrimSpace(inner[first+1])
		}
		quoted, paragraph := notionMarkdownFlowQuoteParagraph(trimmed)
		switch {
		// A paragraph a tag makes first in it is its text (notionMarkdownFlowQuoteParagraph).
		case lead < 4 && paragraph:
			text, blocks = quoted, inner[first+1:]
		// Four columns in, or $$ that no later $$ closes, it is text as written.
		case lead >= 4 || trimmed == "$$" && !notionMarkdownEquationCloses(inner[first+1:]):
			text, blocks = notionMarkdownFlowLiteral(trimmed), inner[first+1:]
		case !notionMarkdownFlowStarts(trimmed, next, false) && !strings.HasPrefix(inner[first], "\t"):
			text, blocks = trimmed, inner[first+1:]
		}
	}
	quote := ">"
	if text != "" {
		quote = "> " + text
	}
	return append([]string{strings.Repeat("\t", level) + quote}, notionMarkdownFlow(blocks, level+1)...), end - start
}

// notionMarkdownEquationLines are an equation's lines between its $$ as
// Notion's "v2" and its MCP's Markdown read them (seen 2026-11-13): without
// what the blocks it stands in start with (written), and, when more than one
// line stands between the empty ones at its ends, without the whitespace
// they all start with, a tab and a space alike one ("$$⏎  a⏎    b⏎$$" is
// "a⏎  b", "\ta⏎  b" is "a⏎ b"); one line keeps it ("$$⏎   a⏎$$", "  $$⏎
// x⏎  $$" stay so). An empty line counts for none, and so does one of
// whitespace alone, which goes without as much as it holds ("  a⏎   " is
// "a⏎ ", seen 2026-11-14: "$$⏎ ⏎ ⏎$$" is " ⏎ ").
func notionMarkdownEquationLines(lines []string, written string) []string {
	held := make([]string, len(lines))
	for index, line := range lines {
		held[index] = strings.TrimPrefix(line, written)
	}
	first, last := 0, len(held)
	for first < last && held[first] == "" {
		first++
	}
	for last > first && held[last-1] == "" {
		last--
	}
	if last-first < 2 {
		return held
	}
	shared := -1
	for _, line := range held[first:last] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if lead := len(line) - len(strings.TrimLeft(line, " \t")); shared < 0 || lead < shared {
			shared = lead
		}
	}
	for index := first; index < last && shared > 0; index++ {
		held[index] = held[index][min(shared, len(held[index])):]
	}
	return held
}

// notionMarkdownFlowQuoteParagraphTag is a <p> on one line around its text.
var notionMarkdownFlowQuoteParagraphTag = regexp.MustCompile(`^<p>(.*)</p>$`)

// notionMarkdownFlowQuoteParagraph is the text of a quote whose first line
// is a tag making a paragraph, which Notion's "v2" reads as the quote's own
// text (seen 2026-11-14): "> <p>a</p>" is the quote "a", "> <p></p>" and
// "> <empty-block/>" a quote of no text ("> <empty-block/>⏎> a" holds "a"),
// "> <unknown url=…>" and "> <unknown>" the quote of the tag's words; a
// heading's, a quote's or a toggle's tag stays a block in it.
func notionMarkdownFlowQuoteParagraph(trimmed string) (string, bool) {
	if match := notionMarkdownFlowQuoteParagraphTag.FindStringSubmatch(trimmed); match != nil &&
		!strings.Contains(match[1], "</p>") && !notionMarkdownVersion2Opened(match[1], "p") {
		return match[1], true
	}
	switch {
	case trimmed == "<empty-block/>" || trimmed == "<empty-block />":
		return "", true
	case notionMarkdownFlowQuoteUnknown.MatchString(trimmed) && !strings.HasSuffix(trimmed, "/>"):
		return `\` + trimmed, true
	}
	return "", false
}

var notionMarkdownFlowQuoteUnknown = regexp.MustCompile(`^<unknown(?:[ \t][^<>]*)?>$`)

// notionMarkdownFlowQuoteEquation tells whether an equation is open in a
// quote after a line of it, inside, past its quotes' >, open before it: $$
// alone opens one a later line's $$ closes, and closes one (a colored one
// as Leafen writes it opens on "$$ {color=…}"); its lines' list marks and
// quotes stand before it.
func notionMarkdownFlowQuoteEquation(open bool, inside string, after []string) bool {
	body := strings.TrimSpace(inside[len(notionMarkdownVersion2MarkerRun.FindString(inside)):])
	if open {
		return body != "$$"
	}
	if body != "$$" && !strings.HasPrefix(body, "$$ {color=") {
		return false
	}
	for _, line := range after {
		if strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):]) == "$$" {
			return true
		}
	}
	return false
}
