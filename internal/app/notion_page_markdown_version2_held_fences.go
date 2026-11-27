package app

import (
	"regexp"
	"strings"
)

// Code fenced in a list item or a quote ends where that block ends, as
// Notion's "v2" reads Markdown's blocks (seen 2026-11-14): "- ```⏎  a⏎⏎b"
// is an item holding the code "a⏎" and then the paragraph "b", "- ```⏎  a⏎-
// b" two items, "1. ```⏎  a⏎b" an item holding empty code and the paragraph
// "a⏎b" (its blocks stand three columns in), "- ```⏎  a⏎```" the code "a"
// and empty code after the item, "- x⏎  - ```⏎    a⏎  b" the code "a" in
// the inner item and "b" in the outer, "> ```⏎> a⏎b" a quote holding "a" and
// the paragraph "b" after it, and a block's closing tag a line that ends
// the item ("<callout>⏎- ```⏎  a⏎</callout>"). The lines before the one
// that ends it are its code, blank ones and ones of whitespace alone too
// ("- ```⏎  a⏎   ⏎b" is "a⏎ "), and of a tab an item's columns take part of
// the spaces left over are ("- ```⏎\ta" is "  a", "1. ```⏎\ta" " a", "-
// ```⏎\t\ta" "  \ta"). A line of text goes on a paragraph's text in an item
// or a quote even where it stands outside them, so code after it stands in
// them still.

// notionMarkdownVersion2Held is a block a "v2" line stands in: a quote, or a
// list item whose blocks stand column columns past where its marker's line
// does.
type notionMarkdownVersion2Held struct {
	quote  bool
	column int
}

var notionMarkdownVersion2HeldMarker = regexp.MustCompile(`^(?:[-*+]|\d{1,9}[.)])(?:[ \t]|$)`)

// notionMarkdownVersion2HeldFences closes code fenced in a list item or a
// quote with a fence of its own at the line that ends them, and writes its
// lines' tabs that the item's columns take part of as the spaces left over.
func notionMarkdownVersion2HeldFences(lines []string) []string {
	out, _ := notionMarkdownVersion2HeldFencesPut(lines)
	return out
}

// notionMarkdownVersion2HeldFencesPut is notionMarkdownVersion2HeldFences
// with the lines it puts in marked, by line.
func notionMarkdownVersion2HeldFencesPut(lines []string) ([]string, []bool) {
	out, putAt := make([]string, 0, len(lines)), []int{}
	held, text := []notionMarkdownVersion2Held(nil), false
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		rest, column, count := notionMarkdownVersion2Within(line, held)
		if strings.TrimSpace(rest) == "" {
			held, text = held[:count], false
			out = append(out, line)
			continue
		}
		if count < len(held) {
			if text && !notionMarkdownVersion2FlowStarts(strings.TrimSpace(rest), "", false) {
				out = append(out, line)
				continue
			}
			held = held[:count]
		}
		opened, body := notionMarkdownVersion2Opens(rest, column)
		held = append(held, opened...)
		out = append(out, line)
		trimmed := strings.TrimSpace(body)
		fence := ""
		if notionMarkdownVersion2Fence(trimmed) || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:len(trimmed)-len(strings.TrimLeft(trimmed, trimmed[:1]))]
		}
		if fence == "" {
			text = trimmed != "" && !notionMarkdownVersion2FlowStarts(trimmed, "", true)
			continue
		}
		text = false
		items := false
		for _, block := range held {
			items = items || !block.quote
		}
		for index+1 < len(lines) {
			next := lines[index+1]
			inside, _, count := notionMarkdownVersion2Within(next, held)
			if count < len(held) {
				putAt = append(putAt, len(out))
				out = append(out, notionMarkdownVersion2HeldPrefix(held)+fence)
				break
			}
			index++
			if lead := next[:len(next)-len(strings.TrimLeft(next, " \t>"))]; items &&
				strings.Contains(lead, "\t") {
				next = notionMarkdownVersion2HeldPrefix(held) + inside
			}
			out = append(out, next)
			if closing := strings.TrimSpace(inside); strings.HasPrefix(closing, fence) && strings.Trim(closing, fence[:1]) == "" {
				break
			}
		}
	}
	put := make([]bool, len(out))
	for _, at := range putAt {
		put[at] = true
	}
	return out, put
}

// notionMarkdownVersion2Within is what a line holds inside the blocks held
// it may stand in, the column it starts at, and how many of them the line
// goes on: a quote's line starts with its >, three spaces before it at
// most, an item's line stands its columns in or is blank, without what it
// has of them.
func notionMarkdownVersion2Within(line string, held []notionMarkdownVersion2Held) (string, int, int) {
	rest, column := line, 0
	for count, block := range held {
		indent := notionMarkdownVersion2Indent(rest, column)
		if block.quote {
			trimmed := strings.TrimLeft(rest, " \t")
			if indent > 3 || !strings.HasPrefix(trimmed, ">") {
				return rest, column, count
			}
			rest, column = notionMarkdownVersion2Strip(trimmed[1:], column+indent+1, 1)
			continue
		}
		if indent < block.column && strings.TrimSpace(rest) != "" {
			return rest, column, count
		}
		rest, column = notionMarkdownVersion2Strip(rest, column, block.column)
	}
	return rest, column, len(held)
}

// notionMarkdownVersion2Opens is the blocks a line's text standing at
// column opens — its quotes' > and its list items' markers — and what it
// holds past them. An item's blocks stand past its marker and the spaces
// after it, one when it holds nothing on its line or they are five or more.
func notionMarkdownVersion2Opens(rest string, column int) ([]notionMarkdownVersion2Held, string) {
	opened := []notionMarkdownVersion2Held{}
	for {
		indent := notionMarkdownVersion2Indent(rest, column)
		trimmed := strings.TrimLeft(rest, " \t")
		if indent > 3 || trimmed == "" {
			return opened, rest
		}
		if strings.HasPrefix(trimmed, ">") {
			opened = append(opened, notionMarkdownVersion2Held{quote: true})
			rest, column = notionMarkdownVersion2Strip(trimmed[1:], column+indent+1, 1)
			continue
		}
		marker := notionMarkdownVersion2HeldMarker.FindString(trimmed)
		if marker == "" || notionMarkdownFlowThematic.MatchString(trimmed) {
			return opened, rest
		}
		mark := strings.TrimRight(marker, " \t")
		after, at := trimmed[len(mark):], column+indent+len(mark)
		spaces := notionMarkdownVersion2Indent(after, at)
		if strings.TrimSpace(after) == "" || spaces > 4 {
			spaces = 1
		}
		opened = append(opened, notionMarkdownVersion2Held{column: indent + len(mark) + spaces})
		rest, column = notionMarkdownVersion2Strip(after, at, spaces)
	}
}

// notionMarkdownVersion2Indent is how many columns the whitespace text
// starts with takes, standing at column, a tab to the next of every four.
func notionMarkdownVersion2Indent(text string, column int) int {
	start := column
	for at := 0; at < len(text); at++ {
		switch text[at] {
		case ' ':
			column++
		case '\t':
			column += 4 - column%4
		default:
			return column - start
		}
	}
	return column - start
}

// notionMarkdownVersion2Strip is text standing at column without width
// columns of the whitespace it starts with, or what it has of them, and the
// column it then stands at: of a tab they take part of the spaces left over
// stay.
func notionMarkdownVersion2Strip(text string, column, width int) (string, int) {
	target, at := column+width, 0
	for ; at < len(text) && column < target; at++ {
		if text[at] == ' ' {
			column++
			continue
		}
		if text[at] != '\t' {
			break
		}
		next := column + 4 - column%4
		if next > target {
			return strings.Repeat(" ", next-target) + text[at+1:], target
		}
		column = next
	}
	return text[at:], column
}

// notionMarkdownVersion2HeldPrefix is what a line standing in the blocks
// held starts with: a > for a quote, an item's columns of spaces.
func notionMarkdownVersion2HeldPrefix(held []notionMarkdownVersion2Held) string {
	var prefix strings.Builder
	for _, block := range held {
		if block.quote {
			prefix.WriteString("> ")
		} else {
			prefix.WriteString(strings.Repeat(" ", block.column))
		}
	}
	return prefix.String()
}
