package app

import (
	"regexp"
	"strings"
)

// A list that ends the Markdown inside an <li> standing outside any list
// and left open to the end is read as Notion's "v2" reads it (seen
// 2026-11-14, thirty shapes): its items whose closing tag no backslash
// leaves on their line are said as <li> tags left open inline — that their
// closing tag is missing, then that an <li> cannot stand inline — before
// all else but the tags left open, and the items closed on their line as
// bare, after the items said bare; the list's own children are not said.
// "<li>a\</li>⏎<ul>⏎<li>b\</li>⏎<li>c</li>⏎</ul>" says <li> left open, the
// <li> b inline, then two bare (a's and c's); "<ul>⏎<li>a\</li>⏎<li>b</li>⏎
// </ul>⏎<li>c\</li>⏎<ul>⏎<li>d\</li>⏎</ul>" the <li> left open, d inline,
// then the first list as any, then two bare. A list that is all closed
// items is a list; other lists in the item read as anywhere.

// notionMarkdownVersion2ListLine is a list's opening tag alone on its line,
// one whose opening is refused aside, which is text
// (notionMarkdownVersion2RefusedOpening).
var notionMarkdownVersion2ListLine = notionMarkdownVersion2ListOpenings{regexp.MustCompile(`^<(ul|ol)(?:[ \t][^<>]*)?>$`)}

// notionMarkdownVersion2OpenItemSaid is what such a list says: first, and
// its bare items last.
type notionMarkdownVersion2OpenItemSaid struct{ first, bare []string }

// notionMarkdownVersion2OpenItemList reads the list whose opening tag is
// lines[list] when it so ends the Markdown in an item left open: what it
// says first, the bare items it says last, and the line of its closing tag.
func notionMarkdownVersion2OpenItemList(lines []string, list int) (first, bare []string, end int, ok bool) {
	if !notionMarkdownVersion2ListLine.MatchString(lines[list]) || !notionMarkdownVersion2ItemLeftOpen(lines[:list]) {
		return nil, nil, 0, false
	}
	open, closed, end := 0, 0, -1
	for at := list + 1; at < len(lines) && end < 0; at++ {
		trimmed := strings.TrimSpace(lines[at])
		opening := notionMarkdownVersion2FlowOpening.FindString(trimmed)
		switch {
		case trimmed == "":
		case trimmed == "</ul>" || trimmed == "</ol>":
			end = at
		case opening == "" || !strings.HasPrefix(opening, "<li"):
			return nil, nil, 0, false
		case notionMarkdownVersion2FlowClosing(trimmed, "li") < 0:
			open++
		case strings.HasSuffix(trimmed, "</li>") && notionMarkdownVersion2FlowClosing(trimmed, "li") == len(trimmed)-len("</li>"):
			closed++
		default:
			return nil, nil, 0, false
		}
	}
	if end < 0 || open == 0 || strings.TrimSpace(strings.Join(lines[end+1:], "")) != "" {
		return nil, nil, 0, false
	}
	for range open {
		first = append(first, notionMarkdownUnclosedInline("li"),
			notionMarkdownListTagInline("li"))
	}
	for range closed {
		bare = append(bare, notionMarkdownBareItemSaid["li"])
	}
	return first, bare, end, true
}

// notionMarkdownVersion2ItemLeftOpen tells lines that end inside an <li>
// standing outside any list, its text not closed on its line nor after it.
func notionMarkdownVersion2ItemLeftOpen(lines []string) bool {
	depth, open := 0, false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, ">"):
			return false
		case notionMarkdownVersion2ListLine.MatchString(trimmed):
			depth++
		case trimmed == "</ul>" || trimmed == "</ol>":
			depth = max(depth-1, 0)
		case depth > 0:
		case strings.HasPrefix(notionMarkdownVersion2FlowOpening.FindString(trimmed), "<li"):
			open = open || notionMarkdownVersion2FlowClosing(trimmed, "li") < 0
		case notionMarkdownVersion2FlowClosing(trimmed, "li") >= 0:
			return false
		}
	}
	return open && depth == 0
}
