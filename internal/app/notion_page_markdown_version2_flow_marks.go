package app

import (
	"regexp"
	"strings"
)

// A "v2" flow tag reads its text on to its closing tag past the marks of the
// blocks it stands in, as Notion's "v2" reads it (seen 2026-11-14): in a
// quote, its lines' > aside ("> <details>⏎> <summary>a⏎> b</summary>⏎>
// </details>" is a quote holding the toggle "a⏎b", "> <ul>⏎> <li>a⏎> b</li>"
// the item "a⏎b", "> <td>a⏎> b</td>" the cell "a⏎b", a line without its >
// going on too), and in a list item, a quote in it or an item in a quote,
// the <details> after the item's mark titled as any ("- <details>⏎
// <summary>a⏎  b</summary>⏎  </details>" is an empty item holding the toggle
// "a⏎b"); a <summary> on its <details>'s line is its title too, after an
// item's mark or not ("<details><summary>a⏎b</summary>", "1. <details>
// <summary>a⏎   b</summary>"). A quote's item's text and closing tag on
// lines of their own stand past its > (notionMarkdownVersion2ItemLines).

// notionMarkdownVersion2FlowListMark is a list item's mark in a line's marks.
var notionMarkdownVersion2FlowListMark = regexp.MustCompile(`[-*+]|\d{1,9}[.)]`)

// notionMarkdownVersion2FlowStart reads a line a flow tag may start: its
// marks (the quotes' > and the items' marks it starts with), how many > of
// quotes they hold, where in the line the flow tag stands, the tag as
// notionMarkdownVersion2FlowOpening finds it there (none for no flow tag),
// and whether it is a toggle's title by its <details> on the line.
func notionMarkdownVersion2FlowStart(line string) (prefix string, quotes, at int, match []string, title bool) {
	prefix = notionMarkdownVersion2MarkerRun.FindString(line)
	quotes = strings.Count(prefix, ">")
	at = len(line) - len(strings.TrimLeft(line[len(prefix):], " \t"))
	body := strings.TrimRight(line[at:], " \t")
	// A title its <details>'s line closes reads as that line does
	// ("<details><summary>s</summary>a</details>", notionMarkdownToggleBlocks).
	if details := notionMarkdownVersion2DetailsSummary.FindString(body); details != "" {
		start := at + strings.LastIndex(details, "<summary")
		if notionMarkdownVersion2FlowClosing(line[start:], "summary") >= 0 {
			return prefix, quotes, at, nil, false
		}
		return prefix, quotes, start, notionMarkdownVersion2FlowOpening.FindStringSubmatch(line[start:]), true
	}
	if strings.Trim(prefix, " \t>") != "" {
		return prefix, quotes, at, nil, false
	}
	return prefix, quotes, at, notionMarkdownVersion2FlowOpening.FindStringSubmatch(body), false
}

// notionMarkdownVersion2FlowAfter is what a line of the flow's after its
// closing tag starts with: its marks, an item's mark as wide in spaces.
func notionMarkdownVersion2FlowAfter(prefix string) string {
	return notionMarkdownVersion2FlowListMark.ReplaceAllStringFunc(prefix, func(mark string) string {
		return strings.Repeat(" ", len(mark))
	})
}

// notionMarkdownVersion2FlowDetails tells a line that opens a <details>, past
// its marks.
func notionMarkdownVersion2FlowDetails(line string) bool {
	return notionMarkdownDetailsPattern.MatchString(strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):]))
}

// notionMarkdownVersion2QuoteMarks is the > of the quotes a line starts
// with and the whitespace around them, or "" for none.
func notionMarkdownVersion2QuoteMarks(line string) string {
	marks := line[:len(line)-len(strings.TrimLeft(line, " \t>"))]
	if !strings.Contains(marks, ">") {
		return ""
	}
	return marks
}
