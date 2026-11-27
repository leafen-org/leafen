package app

import "strings"

// After the first item read inline (notionMarkdownVersion2BrokenItems),
// Notion's "v2" reads on to the end of the Markdown and says each item
// opening after its own closing tag that no list opened after it holds —
// the list it stands in holds none (seen 2026-11-15, its API's create and
// replace alike): bare where it alone opens its line, past its marks, and its
// own closing tag, if on that line, ends it — "<li>a\</li>⏎<p>y </li> w⏎
// <li>c</li>", after lines of words, a blank line or a "<p>c</p>", under "- "
// or "> ", indented, in a <callout>, "<li>⏎c</li>", in a list its closing
// tag made no list, after "<li>a</li> w", before or after the closing tag of
// the list around it ("<ul>⏎<li>a\</li>⏎<p>y </li> w⏎<li>c</li>⏎</ul>",
// "<ul>⏎<li>⏎<p>y </li></p>⏎</ul>⏎<li>c</li>"); inline where words stand
// before it on its line, its own item's closing tag among them, more after
// its own closing tag, or another item on its line ("…⏎z <li>c</li>",
// "…</li> w <li>c</li>", "…⏎<li>c</li> w", "…⏎<li>c</li><li>d</li>" says both).
// The inline ones are said with the item read inline, the bare ones after
// the items closed before it, each in order ("…⏎<li>c</li>⏎z <li>d</li>" says
// d's inline before c's bare). One in code says nothing; one in a list or a
// callout after it, and one in the rest of the list around it, is said as
// that Markdown read alone says it (notionMarkdownVersion2RestSaid); one left
// open to the end is said among the tags left open too ("…⏎<li>c" says
// "Trailing …: <li>." first, notionMarkdownVersion2LeftOpen).

// notionMarkdownVersion2PastItems is what Notion says of such items after an
// item read inline: the inline ones, the bare ones with the words of lines
// read as Markdown of their own among them (read), those inline after the
// list around it (front), and, of the items its list leaves open where its
// closing tag holds more words after it, the first's flow tag (flow).
type notionMarkdownVersion2PastItems struct {
	inline, bare, front []string
	flow                string
	left, read          [][2]int
}

// notionMarkdownVersion2ItemsPast is what Notion says of such items after an
// item opening at lines[start] whose own closing tag stands at end, and
// where those left open stand, by line and byte; the lines from its
// closing tag's line to scope stand in the list around it, which held reads
// as Markdown of its own (notionMarkdownVersion2RestSaid).
func notionMarkdownVersion2ItemsPast(lines []string, start int, end notionMarkdownVersion2ItemEnd, scope int, held bool) notionMarkdownVersion2PastItems {
	past := notionMarkdownVersion2PastItems{}
	if end.line < 0 {
		return past
	}
	// items are those met, in order, and open the indexes of those open.
	type met struct {
		at                [2]int
		bare, quiet, flow bool
		words             string
	}
	items, open := []met{}, []int{}
	step := func(line, at, step int, alone bool) {
		switch {
		case step > 0:
			items, open = append(items, met{at: [2]int{line, at}, bare: alone}), append(open, len(items))
		case len(open) > 0:
			open = open[:len(open)-1]
		}
	}
	// Those on its own closing tag's line after it, a line after its own.
	if end.line > start {
		for _, tag := range notionMarkdownVersion2ItemStepsAt(lines[end.line][end.at:]) {
			step(end.line, end.at+tag[0], tag[1], false)
		}
	}
	first := end.line + 1
	if held && scope < len(lines) {
		// The rest of the list around it reads as Markdown of its own.
		if said := notionMarkdownVersion2RestSaid(lines[first:scope]); said != "" {
			items = append(items, met{at: [2]int{first, 0}, bare: true, words: said})
		}
		past.read, first = append(past.read, [2]int{first, scope - 1}), scope
	}
	fence, lists := "", 0
	for index := first; index < len(lines); index++ {
		line := lines[index]
		rest := line[len(notionMarkdownVersion2MarkerRun.FindString(line)):]
		body := strings.TrimSpace(rest)
		if fence != "" || notionMarkdownVersion2Fence(body) {
			if fence == "" {
				fence = notionMarkdownFenceOf(body)
			} else if notionMarkdownFenceCloses(body, fence, true) {
				fence = ""
			}
			continue
		}
		// A list or a callout after it on a line of its own, outside the
		// list around it, to its closing tag, reads as Markdown of its own.
		if closing := notionMarkdownVersion2RestContainerEnd(lines, index); lists == 0 && index >= min(scope, end.line+1) && closing > index && line == body {
			if said := notionMarkdownVersion2RestSaid(lines[index : closing+1]); said != "" {
				items = append(items, met{at: [2]int{index, 0}, bare: true, words: said})
			}
			past.read, index = append(past.read, [2]int{index, closing}), closing
			continue
		}
		switch {
		case strings.HasPrefix(body, "<ul") || strings.HasPrefix(body, "<ol"):
			lists++
			continue
		case (body == "</ul>" || body == "</ol>") && lists > 0:
			lists--
			continue
		case body == "</ul>" || body == "</ol>":
			// The list around it closing closes the items left open as the
			// reading of its list says them, but where its closing tag holds
			// more words after it, as flow tags.
			for _, at := range open {
				items[at].quiet, items[at].flow = held, !held
			}
			open = open[:0]
			continue
		case lists > 0:
			continue
		}
		text := strings.TrimPrefix(body, notionMarkdownListTagMark)
		// Where text starts in the line, past its marks, spaces and a list tag's mark.
		lead := len(line) - len(strings.TrimLeft(rest, " \t")) + len(body) - len(text)
		tags, openings := notionMarkdownVersion2ItemStepsAt(text), 0
		for _, tag := range tags {
			openings += max(tag[1], 0)
		}
		// One standing in a block's text is inline (notionMarkdownVersion2LastLineItems).
		alone := false
		if opening := notionMarkdownVersion2ItemPairOpening.FindString(text); openings == 1 && opening != "" && text == body {
			alone = !notionMarkdownVersion2ItemBroken(lines, index, lead+len(opening)).broken
		}
		for _, tag := range tags {
			step(index, lead+tag[0], tag[1], alone)
		}
	}
	flow := notionMarkdownUnclosedBlock("li")
	for _, one := range items {
		switch {
		case one.quiet:
		case one.words != "":
			past.bare = append(past.bare, one.words)
		case one.flow && past.flow == "":
			past.flow = flow
			past.bare = append(past.bare, notionMarkdownBareItemSaid["li"])
		case one.flow:
			past.bare = append(past.bare, flow, notionMarkdownBareItemSaid["li"])
		case one.bare:
			past.bare = append(past.bare, notionMarkdownBareItemSaid["li"])
		case scope < len(lines) && one.at[0] >= scope:
			past.front = append(past.front, notionMarkdownListTagInline("li"))
		default:
			past.inline = append(past.inline, notionMarkdownListTagInline("li"))
		}
	}
	for _, at := range open {
		past.left = append(past.left, items[at].at)
	}
	return past
}
