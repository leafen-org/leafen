package app

import (
	"regexp"
	"slices"
	"strings"
)

// A "v2" flow tag starting a line that no closing tag of its own closes
// there reads its text on to its closing tag, as Notion's "v2" reads one
// (seen 2026-11-14): a <summary>'s, a <td>'s or a <th>'s lines after it,
// whatever they hold, are its text, each a line of it — "<summary>a\</summary>⏎
// b</summary>" is the title "a</summary>⏎b", "<td>a\</td>⏎</td>" the cell
// "a</td>", "<td>a\</td>⏎⏎</td>" "a</td>⏎", "<summary>a\</summary>⏎- b⏎
// </summary>" "a</summary>⏎- b" — what follows the closing tag on its line a
// line of its own after it ("…⏎b</summary> x⏎c" holds "x⏎c" after the
// toggle's title). A closing tag of the block around it first ends nothing
// (</details>, </tr>, </table>): the tag is left open as written
// (notionMarkdownBlockTagsLeft). A <td> or a <th> whose text so holds another
// of its kind is refused, and so is a list's item whose text holds another
// <li>, as Notion's reading refuses them, in Leafen's words
// (notionMarkdownVersion2FlowRefused): a list so
// refused is said and read no further, the reading going on past it to say
// what else it refuses ("<ul>⏎<li>a\</li>⏎<li>b</li>⏎</ul>⏎<ul>⏎<li>c\</li>⏎
// </ul>" says the first list's words, then the second's <li> left open). A
// list's item's lines read as its blocks (notionMarkdownFlow).

var notionMarkdownVersion2FlowOpening = regexp.MustCompile(`^<(summary|td|th|li)(?:[ \t][^<>]*)?>`)

// notionMarkdownVersion2FlowAround are the closing tags of the blocks a flow
// tag stands in.
var notionMarkdownVersion2FlowAround = map[string][]string{
	"summary": {"</details>"}, "td": {"</tr>", "</table>", "</thead>", "</tbody>"},
	"th": {"</tr>", "</table>", "</thead>", "</tbody>"}, "li": {"</ul>", "</ol>"},
}

// notionMarkdownVersion2FlowLines puts each flow tag's lines that read as
// its text on its line, joined by line breaks, and what Notion refuses of
// them: of a cell, where it stops (refused), of lists, each (said), with
// the line each list opens on (saidAt).
func notionMarkdownVersion2FlowLines(lines []string) (out []string, refused string, said []string, saidAt []int, ending notionMarkdownVersion2OpenItemSaid) {
	out = make([]string, 0, len(lines))
	fence := ""
	// lists are the lines of the <ul> and <ol> tags open and how many lines
	// stood in out before each, and titled tells a line a toggle's title may
	// stand on, after its <details> (repair.summary).
	lists, before, titled := []int{}, []int{}, false
	// openItem is the last <li> outside any list its line leaves open, and
	// bareAt the lines in out of those closed there; itemRefused is all
	// Notion says when the first is read inline
	// (notion_page_markdown_version2_refused_lists.go).
	openItem, bareAt, itemRefused := -1, []int{}, ""
	// Text's lists holding an item read otherwise read as Markdown of their
	// own (notionMarkdownVersion2OwnLists).
	own := notionMarkdownVersion2ListChildren(lines).own
	// refuse says a list refused, its lines left blank to its closing tag.
	refuse := func(list, quotes int, words string) int {
		at := slices.Index(lists, list)
		if at > 0 && notionMarkdownVersion2HeldByItem(lines, list, quotes) {
			words = notionMarkdownVersion2OuterRefused(lines, lists[at-1], list, quotes) + "\n" + words
			at--
			list = lists[at]
		}
		end := notionMarkdownVersion2ListEnd(lines, list, quotes)
		if at == 0 && quotes == 0 {
			if first := notionMarkdownVersion2InlineItem(lines, openItem, list, end); first != "" {
				itemRefused = strings.Join(slices.Concat([]string{first}, said, []string{words}), "\n")
			}
			for _, put := range slices.Backward(bareAt) {
				words = notionMarkdownBareItemSaid["li"] + "\n" + words
				out[put] = strings.Replace(out[put], "<li", `\<li`, 1)
			}
			bareAt = nil
		}
		said, saidAt = append(said, words), append(saidAt, list)
		out = out[:before[at]]
		lists, before = lists[:at], before[:at]
		for _, line := range lines[list : end+1] {
			out = append(out, strings.TrimRight(notionMarkdownVersion2QuoteMarks(line), " "))
		}
		return end
	}
	for index := 0; index < len(lines); index++ {
		if itemRefused != "" {
			return lines, itemRefused, nil, nil, ending
		}
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		// Past the marks of the blocks it stands in (notion_page_markdown_version2_flow_marks.go).
		prefix, quotes, start, match, ownTitle := notionMarkdownVersion2FlowStart(line)
		body := strings.TrimSpace(line[start:])
		title := titled || ownTitle
		if trimmed != "" {
			titled = fence == "" && notionMarkdownVersion2FlowDetails(line)
		}
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			out = append(out, line)
			continue
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
			out = append(out, line)
			continue
		case own[index] > index:
			out = append(out, lines[index:own[index]+1]...)
			index = own[index]
			continue
		case strings.HasPrefix(body, "<ul") || strings.HasPrefix(body, "<ol"):
			// A list ending the Markdown in an item left open
			// (notion_page_markdown_version2_open_item_lists.go).
			if first, bare, end, ok := notionMarkdownVersion2OpenItemList(lines, index); ok && quotes == 0 && len(lists) == 0 {
				ending.first, ending.bare = append(ending.first, first...), append(ending.bare, bare...)
				out = append(out, make([]string, end+1-index)...)
				index = end
				continue
			}
			lists, before = append(lists, index), append(before, len(out))
		case (body == "</ul>" || body == "</ol>") && len(lists) > 0:
			lists, before = lists[:len(lists)-1], before[:len(before)-1]
		}
		// A <summary> other than a toggle's title is its words (repair.summary).
		if match == nil || match[1] == "summary" && !title {
			out = append(out, line)
			continue
		}
		if match[1] == "li" && quotes == 0 && len(lists) == 0 {
			if notionMarkdownVersion2ClosedItem(body) {
				bareAt = append(bareAt, len(out))
			} else if notionMarkdownVersion2FlowClosing(body[len(match[0]):], "li") < 0 {
				openItem = index
			}
		}
		if close := notionMarkdownVersion2FlowClosing(body, match[1]); close >= 0 {
			// What follows a toggle's title on its line goes on with the lines
			// after it (seen 2026-11-14: "<details>⏎<summary>a</summary>b⏎c" holds
			// the paragraph "b⏎c").
			after := strings.TrimSpace(body[close+len("</"+match[1]+">"):])
			if match[1] != "summary" || after == "" {
				out = append(out, line)
				continue
			}
			out = append(out, line[:start+close+len("</summary>")], notionMarkdownVersion2FlowAfter(prefix)+after)
			continue
		}
		name, closing := match[1], "</"+match[1]+">"
		// A cell on lines of its own is read as it stands (seen 2026-11-14:
		// "<td>⏎<code>a⏎b</code>⏎</td>" is the code "a⏎b").
		if (name == "td" || name == "th") && strings.TrimSpace(body[len(match[0]):]) == "" {
			out = append(out, line)
			continue
		}
		pieces, end, after := []string{strings.TrimRight(line, " \t")}, -1, ""
		// raws are the pieces as written, for the code spans they run over
		// (notionMarkdownVersion2FlowCode).
		raws, unquoted := []string{line}, line[len(notionMarkdownVersion2QuoteMarks(line)):]
		lead := unquoted[:len(unquoted)-len(strings.TrimLeft(unquoted, " \t"))]
		for at := index + 1; at < len(lines) && end < 0; at++ {
			text, raw := notionMarkdownVersion2Unquoted(lines[at], quotes), notionMarkdownVersion2FlowRaw(lines[at], quotes, lead)
			if notionMarkdownVersion2FlowEnds(text, name) {
				break
			}
			if close := notionMarkdownVersion2FlowClosing(text, name); close >= 0 {
				// A cell's closing tag further in than the cell's lines stand
				// ends a line of its text (seen 2026-11-14: "<td>a⏎  </td>" is "a⏎").
				if strings.TrimSpace(text[:close]) != "" ||
					(name == "td" || name == "th") && notionMarkdownVersion2FlowClosing(raw, name) > 0 {
					pieces = append(pieces, text[:close])
					raws = append(raws, raw[:max(notionMarkdownVersion2FlowClosing(raw, name), 0)])
				}
				end, after = at, strings.TrimSpace(text[close+len(closing):])
				continue
			}
			pieces, raws = append(pieces, text), append(raws, raw)
		}
		// A cell's code no line of its own closes takes the rest of the
		// Markdown (notionMarkdownVersion2SwallowedTable).
		if (name == "td" || name == "th") && end >= 0 && !notionMarkdownTableCellCloses(notionMarkdownVersion2UnquotedLines(lines[index+1:end+1], quotes), name) {
			out = append(out, line)
			continue
		}
		list := -1
		if len(lists) > 0 {
			list = lists[len(lists)-1]
		}
		if end < 0 {
			// An item left open says more than that when others are left
			// too, or follow it (notionMarkdownVersion2ListFlow.leftOpen).
			if name == "li" && list >= 0 {
				// An item left open says that alone (repair.frames) but where an
				// item of a list around the list holds it, or <li> lines before
				// the list are read inline (notion_page_markdown_version2_refused_lists.go).
				if left, plain := notionMarkdownVersion2ListFlows(lines, list, quotes).leftOpen(); left != "" && (!plain ||
					len(lists) > 1 && notionMarkdownVersion2HeldByItem(lines, list, quotes) || len(lists) == 1 && quotes == 0 &&
					notionMarkdownVersion2InlineItem(lines, openItem, list, notionMarkdownVersion2ListEnd(lines, list, quotes)) != "") {
					index = refuse(list, quotes, left)
					continue
				}
			}
			out = append(out, line)
			continue
		}
		if words := notionMarkdownVersion2FlowRefused(name, pieces[1:], lines, list, quotes); words != "" && name == "li" {
			index = refuse(list, quotes, words)
			continue
		} else if words != "" {
			return lines, words, said, saidAt, ending
		}
		if name == "li" {
			// Its lines read as its blocks, as they stand.
			out = append(out, line)
			continue
		}
		pieces = notionMarkdownVersion2FlowCode(pieces, raws)
		// A cell's text of lines goes without the spaces it starts with
		// (seen 2026-11-14: "<td>  a  ⏎  b  </td>" is "a⏎b  ").
		if (name == "td" || name == "th") && len(pieces) > 1 {
			head := strings.Index(pieces[0], match[0]) + len(match[0])
			pieces[0] = pieces[0][:head] + strings.TrimLeft(pieces[0][head:], " \t")
		}
		// A toggle's title starts at its first line of text and ends at its
		// last (seen 2026-11-14: "<summary>⏎a</summary>" is "a").
		for name == "summary" && len(pieces) > 1 && strings.TrimSpace(pieces[len(pieces)-1]) == "" {
			pieces = pieces[:len(pieces)-1]
		}
		for head := body; name == "summary" && len(pieces) > 1 && strings.TrimSpace(strings.TrimPrefix(head, match[0])) == ""; {
			head += pieces[1]
			pieces = append([]string{strings.TrimRight(pieces[0], " \t") + pieces[1]}, pieces[2:]...)
		}
		out = append(out, notionMarkdownVersion2TagText(pieces)+closing)
		if after != "" {
			out = append(out, notionMarkdownVersion2FlowAfter(prefix)+after)
		}
		index = end
	}
	return out, "", said, saidAt, ending
}

// notionMarkdownVersion2ListEnd is the line of the closing tag of the list
// whose opening tag is lines[list], quotes deep, or the last line.
func notionMarkdownVersion2ListEnd(lines []string, list, quotes int) int {
	depth := 0
	for at := list + 1; at < len(lines); at++ {
		switch trimmed := notionMarkdownVersion2Unquoted(lines[at], quotes); {
		case strings.HasPrefix(trimmed, "<ul") || strings.HasPrefix(trimmed, "<ol"):
			depth++
		case strings.HasPrefix(trimmed, "</ul>") || strings.HasPrefix(trimmed, "</ol>"):
			if depth == 0 {
				return at
			}
			depth--
		}
	}
	return len(lines) - 1
}

// notionMarkdownVersion2FlowClosing is where the first closing tag of that
// name in text stands that no backslash escapes, or -1.
func notionMarkdownVersion2FlowClosing(text, name string) int {
	closing := "</" + name + ">"
	for at := 0; at < len(text); {
		found := strings.Index(text[at:], closing)
		if found < 0 {
			return -1
		}
		if !notionMarkdownEscapedAt(text, at+found) {
			return at + found
		}
		at += found + len(closing)
	}
	return -1
}

// notionMarkdownVersion2FlowEnds tells a line that a closing tag of the
// block the flow tag stands in starts.
func notionMarkdownVersion2FlowEnds(trimmed, name string) bool {
	for _, around := range notionMarkdownVersion2FlowAround[name] {
		if strings.HasPrefix(trimmed, around) {
			return true
		}
	}
	return false
}

// notionMarkdownVersion2FlowRefused is what Notion refuses of a flow tag
// whose text, the lines after its first, holds another of its kind (seen
// 2026-11-14): a cell's "<td>a\</td>⏎<td>b</td>" in its row, at a line's
// start or not, its row's children; an item's "<li>a\</li>⏎<li>b</li>", as
// its list's items read together say (notionMarkdownVersion2ListFlow): its
// list's children, each <li> the items hold left open and inline, and each
// item of the list but those bare ("<ul>⏎<li>x</li>⏎<li>a\</li>⏎<li>b</li>⏎
// </ul>" two), unless an item is left open; "<li>a\</li>⏎x <li>b</li>" its
// list's children and the <li> inline; an item holding a list before it,
// none. list is the line of the list the item stands in, or -1.
func notionMarkdownVersion2FlowRefused(name string, text []string, lines []string, list, quotes int) string {
	held, inText := false, false
	for _, line := range text {
		// An item's list in it holds its own items.
		if name == "li" && (strings.HasPrefix(line, "<ul") || strings.HasPrefix(line, "<ol")) {
			return ""
		}
		opening := "<" + name
		// An item's <li> in code is its code's (notionMarkdownVersion2CodePairsOf).
		codes := [][2]int{}
		if name == "li" {
			codes = notionMarkdownInlineCodeRanges(line)
		}
		for at := strings.Index(line, opening); at >= 0; {
			if end := at + len(opening); !notionMarkdownEscapedAt(line, at) && (end == len(line) || strings.ContainsRune(" \t/>", rune(line[end]))) &&
				!slices.ContainsFunc(codes, func(code [2]int) bool { return code[0] <= at && at < code[1] }) {
				held, inText = true, inText || at > 0
				break
			}
			next := strings.Index(line[at+1:], opening)
			if next < 0 {
				break
			}
			at += 1 + next
		}
	}
	switch {
	case !held:
		return ""
	case name == "td" || name == "th":
		return notionMarkdownHoldsOnly("tr", "<td>, <th>") + notionMarkdownPartialChildSaid
	case name != "li" || list < 0:
		return ""
	}
	// The list's items read together (notion_page_markdown_version2_list_flows.go).
	flow := notionMarkdownVersion2ListFlows(lines, list, quotes)
	if left, _ := flow.leftOpen(); left != "" {
		return left
	}
	kind := notionMarkdownVersion2Unquoted(lines[list], quotes)[1:3]
	said := []string{notionMarkdownHoldsOnly(kind, "<li>")}
	if inText {
		return strings.Join(append(said, notionMarkdownListTagInline("li")), "\n")
	}
	for range max(flow.held, 1) {
		said = append(said, notionMarkdownUnclosedInline("li"),
			notionMarkdownListTagInline("li"))
	}
	for range max(flow.items-flow.held, 1) {
		said = append(said, notionMarkdownBareItemSaid["li"])
	}
	return strings.Join(said, "\n")
}

// notionMarkdownVersion2UnquotedLines are lines without the > of the quotes
// they stand in, trimmed (notionMarkdownVersion2Unquoted).
func notionMarkdownVersion2UnquotedLines(lines []string, quotes int) []string {
	out := make([]string, len(lines))
	for at, line := range lines {
		out[at] = notionMarkdownVersion2Unquoted(line, quotes)
	}
	return out
}
