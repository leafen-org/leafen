package app

import (
	"slices"
	"strings"
)

// What Notion's "v2" says of a list whose items' text runs on over the
// lines after them (seen 2026-11-14), each item a line of its list starting
// with <li> that no item before it holds in its text: an item no closing tag
// of its own closes on its line holds the lines after it to the first
// </li> no backslash escapes, or to its list's closing tag, where it is left
// open.
//
//   - An item left open says its flow tag closed at the end, then the items
//     no closing tag closed on their line but that one as tags left open,
//     an <li> standing in its text after words as inline, and each item
//     after the first so left as bare: "<ul>⏎<li>a\</li>⏎<li>b</li>⏎<li>c\
//     </li>⏎</ul>" says "Missing closing </li> tag. The flow tag …", the
//     <li> left open and two bare <li>; "<li>a\</li>⏎<li>b\</li>⏎<li>c\</li>"
//     two left open.
//   - Else an item holding another says the list's children, then for each
//     <li> its items' text holds that it is missing its closing tag and
//     inline, and each of the list's items but those as bare: "<li>a\</li>⏎
//     <li>b</li>⏎<li>c\</li>⏎<li>d</li>" says it twice and two bare, "<li>a\
//     </li>⏎<li>b\</li>⏎<li>c</li>" twice and one bare; an <li> after words
//     ("x <li>b</li>") says only the list's children and inline.

// notionMarkdownVersion2ListFlow is what the items of a list hold.
type notionMarkdownVersion2ListFlow struct {
	// items are its items; unclosed those no closing tag closes on their
	// line, the first of them at first (-1 for none).
	items, unclosed, first int
	// open tells an item left open; held counts the items its items' text
	// holds, and inText those standing after words there.
	open         bool
	held, inText int
}

// notionMarkdownVersion2ListFlows reads the items of the list whose opening
// tag is lines[list], quotes deep in quotes.
func notionMarkdownVersion2ListFlows(lines []string, list, quotes int) notionMarkdownVersion2ListFlow {
	flow, depth, covered := notionMarkdownVersion2ListFlow{first: -1}, 0, list
	for at := list + 1; at < len(lines) && depth >= 0; at++ {
		trimmed := notionMarkdownVersion2Unquoted(lines[at], quotes)
		switch {
		case strings.HasPrefix(trimmed, "<ul") || strings.HasPrefix(trimmed, "<ol"):
			depth++
			continue
		case strings.HasPrefix(trimmed, "</ul>") || strings.HasPrefix(trimmed, "</ol>"):
			depth--
			continue
		case depth > 0 || !notionMarkdownVersion2FlowOpening.MatchString(trimmed) || !strings.HasPrefix(trimmed, "<li"):
			continue
		}
		flow.items++
		opening := notionMarkdownVersion2FlowOpening.FindString(trimmed)
		if notionMarkdownVersion2FlowClosing(trimmed[len(opening):], "li") >= 0 {
			continue
		}
		if flow.unclosed++; flow.first < 0 {
			flow.first = flow.items - 1
		}
		if at <= covered {
			continue
		}
		end := at + 1
		for ; end < len(lines); end++ {
			text := notionMarkdownVersion2Unquoted(lines[end], quotes)
			if notionMarkdownVersion2FlowEnds(text, "li") {
				flow.open, end = true, len(lines)
				break
			}
			if close := notionMarkdownVersion2FlowClosing(text, "li"); close >= 0 {
				text = text[:close]
			}
			if opened := strings.Index(text, "<li"); opened >= 0 && !notionMarkdownEscapedAt(text, opened) &&
				notionMarkdownVersion2FlowOpening.MatchString(text[opened:]) &&
				!slices.ContainsFunc(notionMarkdownInlineCodeRanges(text), func(code [2]int) bool { return code[0] <= opened && opened < code[1] }) {
				flow.held++
				if opened > 0 {
					flow.inText++
				}
			}
			if notionMarkdownVersion2FlowClosing(notionMarkdownVersion2Unquoted(lines[end], quotes), "li") >= 0 {
				break
			}
		}
		if end >= len(lines) {
			flow.open = true
			continue
		}
		covered = end
	}
	return flow
}

// leftOpen is what Notion says of a list an item of which is left open, or
// "" for none; plain tells it says no more than that its flow tag closed at
// the end, as an item's left open does anywhere (notionMarkdownBlockTagsLeft).
func (flow notionMarkdownVersion2ListFlow) leftOpen() (said string, plain bool) {
	if !flow.open {
		return "", false
	}
	lines := []string{notionMarkdownUnclosedBlock("li")}
	if flow.unclosed > 1 {
		lines = append(lines, notionMarkdownVersion2TrailingSaid+
			strings.TrimSuffix(strings.Repeat("<li>, ", flow.unclosed-1), ", ")+".")
	}
	for range flow.inText {
		lines = append(lines, notionMarkdownListTagInline("li"))
	}
	bare := 0
	if flow.first >= 0 {
		bare = flow.items - flow.first - 1
	}
	for range bare {
		lines = append(lines, notionMarkdownBareItemSaid["li"])
	}
	return strings.Join(lines, "\n"), len(lines) == 1
}
