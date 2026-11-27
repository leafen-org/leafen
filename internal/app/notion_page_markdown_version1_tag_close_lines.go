package app

import (
	"regexp"
	"strings"
)

// Notion's "v1" page Markdown writes, after a text block's first line whose
// text starts with a block's tag — escaped, as its words — that tag's
// closing tag on a line of its own at the line's start (seen 2026-11-14, a
// paragraph, a quote, a list item and a callout's text alike): "<tr>",
// "<callout>a</callout>", "<details>", "<summary>s</summary>", "<columns>",
// "<column>", "<tabs>", "<synced_block>", a <table> with attributes or
// closed in itself ("<table />x"), and after a bare "<table>" the tag
// right after it ("<table><tr><td>a</td></tr></table>" is followed by
// "</tr>"); "<table> x", "<td>a</td>", "<thead>", "<tab>", "<toggle>",
// "<equation>" and "<empty-block/>" have none. Its line breaks after the
// first stand after that line.

var notionMarkdownVersion1TagCloseOpening = regexp.MustCompile(`^<([a-z_-]+)([ \t][^<>]*)?(/?)>`)

// notionMarkdownVersion1TagClosesAfter are the tags whose closing tag
// follows so.
var notionMarkdownVersion1TagClosesAfter = map[string]bool{
	"tr": true, "callout": true, "details": true, "summary": true, "columns": true, "column": true, "tabs": true,
	"synced_block": true,
}

// notionMarkdownVersion1TagCloseLine is the closing tag a text block's
// text, plain, is followed by so, or "".
func notionMarkdownVersion1TagCloseLine(plain string) string {
	match := notionMarkdownVersion1TagCloseOpening.FindStringSubmatch(plain)
	switch {
	case match == nil:
		return ""
	case match[1] == "table" && strings.TrimSpace(match[2]) == "" && match[3] == "":
		if rest := plain[len(match[0]):]; strings.HasPrefix(rest, "<") && !strings.HasPrefix(rest, "<table") {
			return notionMarkdownVersion1TagCloseLine(rest)
		}
		return ""
	case match[1] == "table" || notionMarkdownVersion1TagClosesAfter[match[1]]:
		return "</" + match[1] + ">"
	}
	return ""
}

// notionMarkdownVersion1TagClosed puts that closing tag after the first line
// of a block's written lines.
func notionMarkdownVersion1TagClosed(lines []string, plain string) []string {
	closing := notionMarkdownVersion1TagCloseLine(plain)
	if closing == "" || len(lines) == 0 {
		return lines
	}
	first, rest, broken := strings.Cut(lines[0], notionMarkdownCodeBreak)
	head := []string{first, closing}
	if broken {
		head = append(head, notionMarkdownCodeBreakLead.FindString(lines[0])+rest)
	}
	return append(head, lines[1:]...)
}
