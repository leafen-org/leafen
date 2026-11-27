package app

import (
	"regexp"
	"slices"
	"strings"
)

// A list's item tag opening a "v2" line whose own closing tag has more on
// its line than another item is refused as Notion's "v2" refuses it (seen
// 2026-11-14): "<ul>⏎<li>a</li> x⏎</ul>", "<li>⏎a</li>x⏎</li>",
// "<ul>⏎<li>⏎``a</li>``⏎</li>⏎</ul>" and "<li>a ``</li>`` b</li>" say what
// <ul> holds (<ol>'s for one) and that <li> is a list tag standing inside
// text (notionMarkdownListTagInline), the second alone for an item no list
// holds; "<li>a</li><li>b</li>" and a closing tag ending its
// line are items. Its own closing tag is the first of its kind that closes
// as many as open after it, past code between single backticks or in
// <code> but not code between more (notionMarkdownVersion2PairMasks), in a
// quote or further in too. One whose closing tag stands in the text of a
// block's tag its line opens and closes — a paragraph's, a heading's, a
// callout's, a toggle's, a quote's or a cell's — closes it in none: its
// <li> left open is said too, before ("<li>a\</li>⏎<p>y </li></p>", in a
// list too, after its children's words; seen 2026-11-15), where the item
// holds words before it (notionMarkdownVersion2ItemWorded); in a list one
// holding none says the list's children alone. So too where the block's tag
// holding it, opening its line or a line of its own before it in the item,
// isn't closed on its line: Notion says that block closed as a flow tag
// first where no closing tag of its own follows anywhere after
// (notionMarkdownVersion2ItemHolder), and no block opened in the item after
// its <li> stays open past it (notionMarkdownVersion2LeftOpen).

var (
	notionMarkdownVersion2ItemPairOpening = regexp.MustCompile(`^<li(?:[ \t][^<>]*)?>`)
	notionMarkdownVersion2ListEndLine     = regexp.MustCompile(`^[ \t]*</(?:ul|ol)>`)
)

// notionMarkdownVersion2BrokenItems is what Notion says of the first item
// whose closing tag so leaves more on its line, and the span its tags open
// in that its closing tag closes over; "" for none.
func notionMarkdownVersion2BrokenItems(lines []string) (string, notionMarkdownVersion2ItemSpan) {
	// listAt are the lines the lists open on, by lists.
	fence, lists, listAt, texts := "", []string{}, []int{}, 0
	for index, line := range lines {
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		trimmed := strings.TrimSpace(line[len(prefix):])
		if fence != "" {
			if notionMarkdownFenceCloses(trimmed, fence, true) {
				fence = ""
			}
			continue
		}
		switch {
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
			continue
		case strings.HasPrefix(trimmed, "<ul") || strings.HasPrefix(trimmed, "<ol"):
			lists, listAt = append(lists, trimmed[1:3]), append(listAt, index)
			continue
		case (trimmed == "</ul>" || trimmed == "</ol>") && len(lists) > 0:
			lists, listAt = lists[:len(lists)-1], listAt[:len(listAt)-1]
			continue
		}
		texts = notionMarkdownVersion2TextListsAfter(line, texts)
		opening := notionMarkdownVersion2ItemPairOpening.FindString(trimmed)
		if opening == "" {
			continue
		}
		at := len(line) - len(strings.TrimLeft(line[len(prefix):], " \t")) + len(opening)
		end := notionMarkdownVersion2ItemBroken(lines, index, at)
		holder, inText := notionMarkdownVersion2ItemHolder(lines, index, end), texts > 0
		if !end.broken && !notionMarkdownVersion2ItemEndsInline(lines, end, holder, inText) {
			continue
		}
		said := notionMarkdownListTagInline("li")
		worded := notionMarkdownVersion2ItemWorded(lines, index, end.line, trimmed[len(opening):])
		held := end.held || holder.name != ""
		// The list it stands in closes where its closing tag stands.
		scope := len(lines)
		if len(listAt) > 0 && end.line >= 0 {
			if closing := notionMarkdownVersion2ListEnd(lines, listAt[len(listAt)-1], 0); closing > end.line && notionMarkdownVersion2ListEndLine.MatchString(lines[closing]) {
				scope = closing
			}
		}
		if held && worded {
			said = notionMarkdownUnclosedInline("li") + "\n" + said
			if !inText && !end.held && !notionMarkdownVersion2HolderClosed(lines, end, holder.name, scope) {
				said = notionMarkdownUnclosedBlock(holder.name) + "\n" + said
			}
		}
		// An item in a list made text closes over nothing: what it holds
		// stood after that list's closing tag (notionMarkdownVersion2ListClosings).
		span := notionMarkdownVersion2ItemSpan{fromLine: index, from: at, toLine: end.line, to: end.at}
		if inText {
			span = notionMarkdownVersion2ItemSpan{fromLine: -1}
		}
		// The items after it no list holds, and what follows it read as
		// Markdown of its own (notionMarkdownVersion2ItemsPast).
		past := notionMarkdownVersion2ItemsPast(lines, index, end, scope, held)
		span.left, span.read, span.lists, span.cut = past.left, past.read, -1, true
		if len(listAt) > 0 {
			span.lists = listAt[len(listAt)-1]
		}
		children := []string{notionMarkdownHoldsOnly(strings.Join(lists[len(lists)-min(len(lists), 1):], ""), "<li>")}
		if past.flow != "" {
			children = append(children, past.flow)
		}
		if len(lists) > 0 && end.held && !worded {
			// One holding no words before it, its <li> alone on its line, is
			// none of the list's items: the list's children alone are said.
			return strings.Join(slices.Concat(past.front, children, past.inline, past.bare), "\n"), span
		}
		if len(lists) > 0 {
			said = strings.Join(slices.Concat(past.front, children, []string{said}, past.inline, past.bare), "\n")
		} else {
			said = notionMarkdownVersion2BareItemsAround(lines, index, said, past.inline, past.bare)
		}
		return said, span
	}
	return "", notionMarkdownVersion2ItemSpan{fromLine: -1}
}

// notionMarkdownVersion2BareItemsAround is what Notion says of an item no
// list holds read inline at lines[broken], inline said once for each item
// its line opens, with the items no list holds before it (seen 2026-11-15):
// each left open first — "Missing closing </li> tag. The flow tag…" in a
// list its closing tag made no list, else among the tags left open — and
// bare, then each closed before it bare, after it ("<li>a\</li>⏎x⏎<li>c</li>
// y" says a's <li> left open and bare, then c's inline; "<li>a</li>⏎x⏎
// <li>c</li> y" c's inline, then a bare) — the items after it read inline
// with it, those after it bare last (notionMarkdownVersion2ItemsPast).
func notionMarkdownVersion2BareItemsAround(lines []string, broken int, inline string, inlinePast, barePast []string) string {
	type item struct{ inText bool }
	open, closed, fence, lists, textLists := []item{}, 0, "", 0, 0
	for _, line := range lines[:broken] {
		body := strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):])
		if fence != "" || notionMarkdownVersion2Fence(body) {
			if fence == "" {
				fence = notionMarkdownFenceOf(body)
			} else if notionMarkdownFenceCloses(body, fence, true) {
				fence = ""
			}
			continue
		}
		text, marked := strings.CutPrefix(body, notionMarkdownListTagMark)
		switch {
		case strings.HasPrefix(text, "<ul") || strings.HasPrefix(text, "<ol"):
			if marked {
				textLists++
			} else {
				lists++
			}
			continue
		case strings.HasPrefix(text, "</ul>") || strings.HasPrefix(text, "</ol>"):
			if marked {
				textLists = max(textLists-1, 0)
			} else {
				lists = max(lists-1, 0)
			}
			continue
		case lists > 0:
			continue
		}
		for _, step := range notionMarkdownVersion2ItemSteps(body) {
			switch {
			case step > 0:
				open = append(open, item{inText: textLists > 0})
			case len(open) > 0:
				open, closed = open[:len(open)-1], closed+1
			}
		}
	}
	said, trailing := []string{}, []string{}
	for _, one := range open {
		if one.inText {
			said = append(said, notionMarkdownUnclosedBlock("li"))
		} else {
			trailing = append(trailing, "<li>")
		}
		said = append(said, notionMarkdownBareItemSaid["li"])
	}
	if len(trailing) > 0 {
		said = append([]string{notionMarkdownVersion2TrailingSaid + strings.Join(trailing, ", ") + "."}, said...)
	}
	openings := 0
	for _, step := range notionMarkdownVersion2ItemSteps(lines[broken]) {
		openings += max(step, 0)
	}
	for range max(openings, 1) {
		said = append(said, inline)
	}
	said = append(said, inlinePast...)
	for range closed {
		said = append(said, notionMarkdownBareItemSaid["li"])
	}
	return strings.Join(append(said, barePast...), "\n")
}

// notionMarkdownVersion2ItemEnd is where an item's own closing tag stands
// (line -1 for none, at its byte there), whether its line has more after it
// than items (broken), and whether it stands in the text of a block's tag
// its line opens and closes (held, notionMarkdownVersion2ItemHeldClosing).
type notionMarkdownVersion2ItemEnd struct {
	line, at     int
	broken, held bool
}

// notionMarkdownVersion2ItemBroken is where the own closing tag of an item
// opening at lines[start] before byte from stands.
func notionMarkdownVersion2ItemBroken(lines []string, start, from int) notionMarkdownVersion2ItemEnd {
	depth := 1
	for index := start; index < len(lines); index++ {
		line, at := lines[index], 0
		if index == start {
			at = from
		}
		masked := notionMarkdownVersion2PairMasks(line)
		for _, found := range notionMarkdownRepairTag.FindAllStringSubmatchIndex(line, -1) {
			if found[0] < at || line[found[4]:found[5]] != "li" || found[9] > found[8] || notionMarkdownEscapedAt(line, found[0]) ||
				slices.ContainsFunc(masked, func(code [2]int) bool { return code[0] < found[0] && found[0] < code[1] }) {
				continue
			}
			if found[3] == found[2] {
				depth++
				continue
			}
			if depth--; depth > 0 {
				continue
			}
			rest := strings.TrimSpace(line[found[1]:])
			return notionMarkdownVersion2ItemEnd{line: index, at: found[0],
				broken: rest != "" && !notionMarkdownVersion2ItemPairOpening.MatchString(rest), held: notionMarkdownVersion2ItemHeldClosing(line, found[0])}
		}
	}
	return notionMarkdownVersion2ItemEnd{line: -1}
}

// notionMarkdownVersion2ItemWorded tells an item opening at lines[start],
// rest after its tag there, whose own closing tag stands in a block's text
// on lines[closed], that holds words before that line: on its tag's line,
// or a line between (seen 2026-11-15: "<ul>⏎<li>a\</li>⏎<p>y </li></p>⏎</ul>"
// and "<ul>⏎<li>⏎x⏎<p>y </li></p>⏎</ul>" say its <li> left open and inline
// after the list's children, "<ul>⏎<li>⏎<p>y </li></p>⏎</ul>" the children
// alone, a callout's, a line of spaces and words after the paragraph alike).
func notionMarkdownVersion2ItemWorded(lines []string, start, closed int, rest string) bool {
	if strings.TrimSpace(rest) != "" {
		return true
	}
	for at := start + 1; at < closed && at < len(lines); at++ {
		if strings.TrimSpace(lines[at][len(notionMarkdownVersion2MarkerRun.FindString(lines[at])):]) != "" {
			return true
		}
	}
	return false
}

// notionMarkdownVersion2ItemHeldClosing tells an item's closing tag at byte
// at of a line standing in the text of a paragraph's, a heading's, a
// callout's, a toggle's, a quote's or a cell's tag opening the line, past its
// marks, and closing after it.
func notionMarkdownVersion2ItemHeldClosing(line string, at int) bool {
	start := len(notionMarkdownVersion2MarkerRun.FindString(line))
	opening := notionMarkdownVersion2ItemHeldTag.FindStringSubmatch(line[start:])
	return opening != nil && start+len(opening[0]) <= at && strings.Contains(line[at:], "</"+opening[1]+">")
}

// notionMarkdownVersion2ItemHeldTag is a block's tag holding text.
var notionMarkdownVersion2ItemHeldTag = regexp.MustCompile(`^<(p|h[1-4]|callout|toggle|quote|td|th)(?:[ \t][^<>]*)?>`)
