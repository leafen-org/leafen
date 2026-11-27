package app

import "strings"

// What Notion's "v2" says of a list whose items' text it refuses
// (notionMarkdownVersion2FlowLines), by where the list stands (seen
// 2026-11-14, forty shapes); what it says is in the order the text stands:
//
//   - An <li> outside any list whose text its line doesn't close, before the
//     list with more after the list's closing tag, is read inline: Notion
//     says it is missing its closing tag and inline before the list, and
//     each <li> starting a line after the list inline too — "<li>a\</li>⏎
//     <ul>⏎<li>b\</li>⏎<li>c</li>⏎</ul>⏎z" says the <li> left open, then that,
//     then the list's words; a "</li>" line after the list closes it, saying
//     no tag left open ("<li>a\</li>⏎<ul>⏎<li>b\</li>⏎</ul>⏎</li>" says the
//     <li> inline and the list's item left open). A list ending the
//     Markdown is read so (notion_page_markdown_version2_open_item_lists.go),
//     and so is one another list follows; a list it doesn't refuse leaves
//     the <li> a block left open.
//   - <li> lines closed on their line outside any list before it are bare,
//     said before the list's words ("<li>a</li>⏎<ul>⏎<li>b\</li>⏎</ul>").
//   - A list in an item of another list, the item's <li> on a line of its
//     own, refuses that one too, first: its children, its items before the
//     item bare, then the list's words ("<ul>⏎<li>a</li>⏎<li>⏎<ul>⏎<li>b\</li>⏎
//     <li>c</li>⏎</ul>⏎</li>⏎</ul>").

// notionMarkdownVersion2HeldByItem tells a list whose opening tag at
// lines[list] stands right after an <li> on a line of its own.
func notionMarkdownVersion2HeldByItem(lines []string, list, quotes int) bool {
	for at := list - 1; at >= 0; at-- {
		if text := notionMarkdownVersion2Unquoted(lines[at], quotes); text != "" {
			opening := notionMarkdownVersion2FlowOpening.FindString(text)
			return strings.HasPrefix(opening, "<li") && opening == text
		}
	}
	return false
}

// notionMarkdownVersion2OuterRefused is what the list at lines[outer] says
// first when its item holding the list at lines[list] is refused.
func notionMarkdownVersion2OuterRefused(lines []string, outer, list, quotes int) string {
	said := []string{notionMarkdownHoldsOnly(notionMarkdownVersion2Unquoted(lines[outer], quotes)[1:3], "<li>")}
	for _, line := range lines[outer+1 : list] {
		if notionMarkdownVersion2ClosedItem(notionMarkdownVersion2Unquoted(line, quotes)) {
			said = append(said, notionMarkdownBareItemSaid["li"])
		}
	}
	return strings.Join(said, "\n")
}

// notionMarkdownVersion2ClosedItem tells an <li> line its own closing tag
// ends.
func notionMarkdownVersion2ClosedItem(text string) bool {
	opening := notionMarkdownVersion2FlowOpening.FindString(text)
	if !strings.HasPrefix(opening, "<li") {
		return false
	}
	close := notionMarkdownVersion2FlowClosing(text[len(opening):], "li")
	return close >= 0 && strings.TrimSpace(text[len(opening)+close+len("</li>"):]) == ""
}

// notionMarkdownVersion2InlineItem is what Notion says first of the <li> at
// lines[item] read inline before the refused list at lines[list] ending at
// lines[end], or "" when it is not so read.
func notionMarkdownVersion2InlineItem(lines []string, item, list, end int) string {
	if item < 0 || item > list {
		return ""
	}
	// One whose own closing tag before the list has more after it on its
	// line is read inline so (notionMarkdownVersion2BrokenItems).
	line := lines[item]
	prefix := notionMarkdownVersion2MarkerRun.FindString(line)
	opening := notionMarkdownVersion2ItemPairOpening.FindString(strings.TrimSpace(line[len(prefix):]))
	from := len(line) - len(strings.TrimLeft(line[len(prefix):], " \t")) + len(opening)
	if end := notionMarkdownVersion2ItemBroken(lines, item, from); opening != "" && end.broken && end.line < list {
		return ""
	}
	for _, line := range lines[item+1 : list] {
		if strings.TrimSpace(line) == "</li>" {
			return ""
		}
	}
	left, inline, rest := 1, 0, false
	for _, line := range lines[end+1:] {
		text := strings.TrimSpace(line)
		// Another list next leaves the <li> a block left open, as a list
		// ending the Markdown does.
		if !rest && notionMarkdownVersion2ListLine.MatchString(text) {
			return ""
		}
		rest = rest || text != ""
		switch {
		case text == "</li>":
			left = 0
		case strings.HasPrefix(notionMarkdownVersion2FlowOpening.FindString(text), "<li"):
			inline++
			if notionMarkdownVersion2FlowClosing(text[len(notionMarkdownVersion2FlowOpening.FindString(text)):], "li") < 0 {
				left++
			}
		}
	}
	if !rest {
		return ""
	}
	said := []string{}
	if left > 0 {
		said = append(said, notionMarkdownVersion2TrailingSaid+
			strings.TrimSuffix(strings.Repeat("<li>, ", left), ", ")+".")
	}
	said = append(said, notionMarkdownUnclosedInline("li"),
		notionMarkdownListTagInline("li"))
	for range inline {
		said = append(said, notionMarkdownListTagInline("li"))
	}
	return strings.Join(said, "\n")
}
