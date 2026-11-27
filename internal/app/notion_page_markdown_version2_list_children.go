package app

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

// What a "v2" <ul> or <ol> on lines of its own holds, as Notion's "v2" reads
// it (seen 2026-11-14, seventy shapes): its first child, past blank lines,
// tells how. An <li>, another block's tag or a list starts HTML, every child
// of which has to be an <li> — when one is not, it says "<ul> children must
// be <li>." before all the list holds says, and its items are bare
// ("<ul>⏎<li>a</li>⏎x⏎<li>b</li>⏎</ul>" says it and two bare, "<ul>⏎<p>x</p>⏎
// </ul>" and "<ul>⏎<callout>⏎x⏎</callout>⏎</ul>" it alone, a list in it
// holding its own items). Markdown starts text, whose list items are the
// list's ("<ul>⏎- a⏎</ul>" is the item "a"): each other block of it says
// that <ul> held something that isn't a list item (notionMarkdownNotListContent)
// once its items have said they are bare ("<ul>⏎x⏎<li>a</li>⏎
// y⏎</ul>" says one bare, then it twice; "<ul>⏎# h⏎x⏎</ul>" twice). An item
// whose closing tag stands in code between single backticks, or which holds
// an <li> in code between more, is no item and says nothing of its own
// ("<ul>⏎<li>`a</li>`⏎</ul>" and "<ul>⏎<li>⏎``a<li>``⏎</li>⏎</ul>" say only
// the list's children). Text's list holding an item Notion reads otherwise —
// one left open, or closed with more on its line — reads what it holds as
// Markdown of its own, whose <li>s are bare (seen 2026-11-14): it says what
// that Markdown says, its tags left open first, then that it held something
// that isn't a list item for each other block before the item, where it stands
// ("<ul>⏎x⏎<li>b\</li>⏎</ul>" says <li> left open, one bare and it once,
// "<li>a</li>⏎<ul>⏎x⏎<li>b\</li>⏎</ul>" a's bare first); one that tags start
// is read as before (notion_page_markdown_version2_flow_lines.go). A list
// whose closing tag has more after it on its line is no list, its tags text
// (notionMarkdownVersion2ListClosings): in a list, its opening tag's line
// starts a paragraph, the list's first child a tag's. Another list's
// closing tag is a child of it, text in text; a list nothing closes holds
// the lines to the end (seen 2026-11-15: "<ul>⏎<li>a</li>⏎</ol>" says it left
// open, its children, a's bare; "<ul>⏎x" it left open, its non-list content).

var notionMarkdownVersion2OneLineList = regexp.MustCompile(`^<(ul|ol)(?:[ \t][^<>]*)?>(.*\S.*)</(ul|ol)>$`)

// notionMarkdownVersion2ListSaid is what the lists of a "v2" write's lines
// say, by line: at a list's opening tag, at its closing tag, the lists that
// hold no items, and the lines of items read as words; own are the lists
// whose lines read as Markdown of their own, by their closing tag's line,
// and others how many blocks besides items each holds.
type notionMarkdownVersion2ListSaid struct {
	opening map[int]string
	closing map[int][]string
	refused map[int]bool
	words   map[int]bool
	own     map[int]int
	others  map[int]int
}

// notionMarkdownVersion2ListChildren reads what each list of lines holds.
func notionMarkdownVersion2ListChildren(lines []string) notionMarkdownVersion2ListSaid {
	said := notionMarkdownVersion2ListSaid{opening: map[int]string{}, closing: map[int][]string{}, refused: map[int]bool{},
		words: map[int]bool{}, own: map[int]int{}, others: map[int]int{}}
	// around are the kinds of the lists open, which their closing tags
	// close over the lists in them.
	fence, around := "", []string{}
	for index, line := range lines {
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		body := strings.TrimSpace(line[len(prefix):])
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(strings.TrimLeft(strings.TrimSpace(line), " \t>"), fence, true) {
				fence = ""
			}
		case notionMarkdownVersion2Fence(body):
			fence = notionMarkdownFenceOf(body)
		case notionMarkdownVersion2ListLine.MatchString(body):
			said.read(lines, index, strings.Count(prefix, ">"), body[1:3], around, strings.Trim(prefix, " \t") == "")
			around = append(around, body[1:3])
		case (body == "</ul>" || body == "</ol>") && slices.Contains(around, body[2:4]):
			around = around[:slices.Index(around, body[2:4])]
		case notionMarkdownVersion2OneLineList.MatchString(body):
			// One on a line holding text alone holds no items (seen
			// 2026-11-14: "<ul>x</ul>"; "<ul><li>a</li></ul>" is a list).
			if match := notionMarkdownVersion2OneLineList.FindStringSubmatch(body); match[1] == match[3] && !strings.Contains(match[2], "<") {
				said.refused[index] = true
				said.closing[index] = []string{notionMarkdownNotListContent(match[1])}
			}
		}
	}
	return said
}

// read reads the list whose opening tag of kind stands at lines[list], in
// the lists of the kinds around; one standing on the left with nothing
// around it but block tags (top) holds the lines to the end when nothing
// closes it.
func (said notionMarkdownVersion2ListSaid) read(lines []string, list, quotes int, kind string, around []string, top bool) {
	// kinds are its children's: an item (l), an item read as words (w),
	// another tag's block (b), Markdown's list item (m) or another block (t).
	kinds, words, end, paragraph := "", []int{}, -1, false
	for at := list + 1; at < len(lines) && end < 0; at++ {
		text := notionMarkdownVersion2Unquoted(lines[at], quotes)
		switch {
		case text == "</"+kind+">":
			end = at
		case strings.HasPrefix(text, "</"+kind+">"):
			said.refused[list] = true
			return
		case text == "</ul>" || text == "</ol>":
			// Another list's closing tag closes this one over where one of
			// its kind holds it, and is a child of it else (seen 2026-11-15:
			// "<ul>⏎<li>a</li>⏎</ol>⏎</ul>" says its children, "<ul>⏎x⏎</ol>⏎
			// </ul>" holds the paragraph "x⏎</ol>").
			switch {
			case slices.Contains(around, text[2:4]):
				end = at
			case kinds == "" || strings.ContainsRune("lbw", rune(kinds[0])):
				kinds, paragraph = kinds+"b", false
			case !paragraph:
				kinds, paragraph = kinds+"t", true
			}
		case text == "":
			paragraph = false
		case notionMarkdownVersion2TextListOpening(text):
			// A list its closing tag made text starts a paragraph
			// (notionMarkdownVersion2ListClosings), its tag first a tag's
			// (seen 2026-11-15: "<ul>⏎<ul>⏎</ul> y⏎</ul>" says its children).
			if kinds == "" {
				kinds, paragraph = "b", true
			} else {
				kinds, paragraph = kinds+"t", true
			}
		case strings.HasSuffix(kinds, "m") && strings.HasPrefix(lines[at][len(notionMarkdownVersion2QuoteMarks(lines[at])):], "  "):
			// A line further in goes on with Markdown's item.
		case notionMarkdownVersion2Fence(text):
			if at = notionMarkdownVersion2ListChildEnd(lines, at, quotes, func(line string) bool {
				return notionMarkdownFenceCloses(line, notionMarkdownFenceOf(text), true)
			}); at < 0 {
				return
			}
			kinds, paragraph = kinds+"t", false
		case notionMarkdownVersion2ListLine.MatchString(text):
			depth := 0
			if at = notionMarkdownVersion2ListChildEnd(lines, at, quotes, func(line string) bool {
				if notionMarkdownVersion2ListLine.MatchString(line) {
					depth++
				} else if strings.HasPrefix(line, "</ul>") || strings.HasPrefix(line, "</ol>") {
					depth--
				}
				return depth < 0
			}); at < 0 {
				return
			}
			kinds, paragraph = kinds+"b", false
		case strings.HasPrefix(text, `\<li`) && notionMarkdownVersion2FlowOpening.MatchString(text[1:]) &&
			strings.TrimSpace(text[len(notionMarkdownVersion2FlowOpening.FindString(text[1:]))+1:]) == "":
			// An item the pairing of tags in code left as words
			// (notionMarkdownVersion2CodePairsOf), its text holding an <li> in
			// code between more than single backticks.
			last := notionMarkdownVersion2ListChildEnd(lines, at, quotes, func(line string) bool { return line == `\</li>` || line == "</li>" })
			if last < 0 || !slices.ContainsFunc(lines[at+1:last], func(line string) bool {
				held := notionMarkdownVersion2HeldItemTag(line)
				return held >= 0 && notionMarkdownVersion2InCode(line, held, false) && !notionMarkdownVersion2InCode(line, held, true)
			}) {
				return
			}
			kinds, at, paragraph = kinds+"w", last, false
		case strings.HasPrefix(notionMarkdownVersion2FlowOpening.FindString(text), "<li"):
			last, item := notionMarkdownVersion2ListItem(lines, at, quotes)
			switch {
			case item == 0 && kinds != "" && strings.ContainsRune("mt", rune(kinds[0])) && !strings.ContainsAny(kinds, "bw"):
				// Text's list reads its lines as Markdown of their own.
				if end = notionMarkdownVersion2ListClose(lines, at, quotes); end >= 0 {
					said.own[list], said.others[list] = end, strings.Count(kinds, "t")
				}
				return
			case item == 0:
				return
			case item == 'w':
				words = append(words, at)
			}
			kinds, at, paragraph = kinds+string(item), last, false
		case strings.HasPrefix(text, "<!--") || notionMarkdownVersion2BlockTag.MatchString(text):
			kinds, paragraph = kinds+"b", false
		case notionMarkdownVersion2Bullet.MatchString(text) || notionMarkdownVersion2Number.MatchString(text):
			kinds, paragraph = kinds+"m", true
		case notionMarkdownVersion2Heading.MatchString(text) || notionMarkdownFlowThematic.MatchString(text):
			kinds, paragraph = kinds+"t", false
		case !paragraph:
			kinds, paragraph = kinds+"t", true
		}
	}
	// One nothing closes holds the lines to the end, as one closed there
	// (seen 2026-11-15: "<ul>⏎x" says it left open, then its non-list
	// content; "<ul>⏎<li>a</li>⏎x" its children).
	if end < 0 && top {
		end = len(lines)
	}
	// All items, or all Markdown's, are a list.
	if end < 0 || strings.Trim(kinds, "l") == "" || strings.Trim(kinds, "m") == "" {
		return
	}
	switch {
	case kinds[0] == 'm' || kinds[0] == 't':
		// Another tag's block in text is read as before.
		if strings.ContainsAny(kinds, "bw") {
			return
		}
		said.refused[list] = true
		for range strings.Count(kinds, "t") {
			said.closing[end] = append(said.closing[end], notionMarkdownNotListContent(kind))
		}
	case strings.Trim(kinds, "l") != "":
		said.refused[list] = true
		said.opening[list] = notionMarkdownHoldsOnly(kind, "<li>")
		for _, at := range words {
			said.words[at] = true
		}
	}
}

// notionMarkdownVersion2ListClose is the line of the closing tag of the list
// lines[start] stands in, quotes deep, the lists after it closing first, or
// -1.
func notionMarkdownVersion2ListClose(lines []string, start, quotes int) int {
	depth := 0
	return notionMarkdownVersion2ListChildEnd(lines, start, quotes, func(line string) bool {
		if notionMarkdownVersion2ListLine.MatchString(line) {
			depth++
		} else if strings.HasPrefix(line, "</ul>") || strings.HasPrefix(line, "</ol>") {
			depth--
		}
		return depth < 0
	})
}

// notionMarkdownVersion2OwnLists reads the lists whose lines read as Markdown
// of their own (notionMarkdownVersion2ListChildren) as Notion's "v2" does:
// what that Markdown says, then that it held something that isn't a list
// item for each other block, said at the list's closing tag, its lines left blank but for the
// quotes they stand in; a list in one of them is its Markdown's.
func notionMarkdownVersion2OwnLists(lines []string, said notionMarkdownVersion2ListSaid) {
	last := -1
	for _, list := range slices.Sorted(maps.Keys(said.own)) {
		end := said.own[list]
		if list < last {
			continue
		}
		last = end
		lead := lines[end][:len(lines[end])-len(strings.TrimLeft(lines[end], " \t>"))]
		inner := make([]string, 0, end-list-1)
		for _, line := range lines[list+1 : end] {
			if rest, ok := strings.CutPrefix(line, lead); ok {
				inner = append(inner, rest)
			} else {
				inner = append(inner, strings.TrimLeft(line, " \t>"))
			}
		}
		_, _, err := notionMarkdownVersion2Input(strings.Join(inner, "\n"))
		words, ok := "", false
		if err != nil {
			words, ok = strings.CutPrefix(err.Error(), notionMarkdownRefusedHead)
		}
		if !ok {
			continue
		}
		kind := strings.TrimSpace(lines[list][len(notionMarkdownVersion2MarkerRun.FindString(lines[list])):])[1:3]
		said.refused[list] = true
		said.closing[end] = strings.Split(words, "\n")
		for range said.others[list] {
			said.closing[end] = append(said.closing[end], notionMarkdownNotListContent(kind))
		}
		for at := list + 1; at < end; at++ {
			lines[at] = strings.TrimRight(notionMarkdownVersion2QuoteMarks(lines[at]), " ")
		}
	}
}

// notionMarkdownVersion2ListChildEnd is the line after lines[start] that
// ends a child of a list, quotes deep, or -1.
func notionMarkdownVersion2ListChildEnd(lines []string, start, quotes int, ends func(string) bool) int {
	for at := start + 1; at < len(lines); at++ {
		if ends(notionMarkdownVersion2Unquoted(lines[at], quotes)) {
			return at
		}
	}
	return -1
}

// notionMarkdownVersion2ListItem reads the item whose <li> opens
// lines[start]: its last line and whether it is an item (l), one read as
// words (w), or one Notion reads otherwise (0).
func notionMarkdownVersion2ListItem(lines []string, start, quotes int) (int, byte) {
	text := notionMarkdownVersion2Unquoted(lines[start], quotes)
	rest := text[len(notionMarkdownVersion2FlowOpening.FindString(text)):]
	for at := start; at < len(lines); at++ {
		if at > start {
			rest = notionMarkdownVersion2Unquoted(lines[at], quotes)
			if rest == "</ul>" || rest == "</ol>" {
				return 0, 0
			}
		}
		close := notionMarkdownVersion2FlowClosing(rest, "li")
		held := notionMarkdownVersion2HeldItemTag(rest)
		if held >= 0 && (close < 0 || held < close) {
			// An <li> it holds in code between more than single backticks
			// takes the first </li> line after it, its own the next (seen
			// 2026-11-14: "<ul>⏎<li>⏎``<li>``⏎</li>⏎</li>⏎</ul>" is the item
			// "<li>" in code and "⏎</li>").
			if !notionMarkdownVersion2InCode(rest, held, false) || notionMarkdownVersion2InCode(rest, held, true) {
				return 0, 0
			}
			// Its pair in code may leave the first escaped
			// (notionMarkdownVersion2CodePairsOf).
			closings := 0
			last := notionMarkdownVersion2ListChildEnd(lines, at, quotes, func(line string) bool {
				if line == "</li>" || line == `\</li>` {
					closings++
				}
				return closings == 2 || line == "</ul>" || line == "</ol>"
			})
			switch {
			case last >= 0 && closings == 2:
				return last, 'l'
			case closings == 1:
				return notionMarkdownVersion2ListChildEnd(lines, at, quotes, func(line string) bool { return line == "</li>" || line == `\</li>` }), 'w'
			}
			return 0, 0
		}
		// A closing tag in code between single backticks is the code's
		// where a </li> line after it closes the item (seen 2026-11-14:
		// "<ul>⏎<li>⏎`a</li>`⏎</li>⏎</ul>" is an item); else the item is no
		// item's ("<ul>⏎<li>`a</li>`⏎</ul>", its text put on a line of its own,
		// notionMarkdownVersion2ItemLines).
		if close >= 0 && notionMarkdownVersion2InCode(rest, close, true) && notionMarkdownVersion2ItemClosedAfter(lines, at, quotes) {
			continue
		}
		if close < 0 {
			continue
		}
		if strings.TrimSpace(rest[close+len("</li>"):]) == "" {
			return at, 'l'
		}
		if notionMarkdownVersion2InCode(rest, close, true) {
			return at, 'w'
		}
		return 0, 0
	}
	return 0, 0
}

// notionMarkdownVersion2ItemClosedAfter tells a "</li>" line after
// lines[at] before its list's closing tag.
func notionMarkdownVersion2ItemClosedAfter(lines []string, at, quotes int) bool {
	end := notionMarkdownVersion2ListChildEnd(lines, at, quotes, func(line string) bool {
		return line == "</li>" || line == "</ul>" || line == "</ol>"
	})
	return end >= 0 && notionMarkdownVersion2Unquoted(lines[end], quotes) == "</li>"
}

// notionMarkdownVersion2HeldItemTag is where the first <li> that no
// backslash escapes stands in text, or -1.
func notionMarkdownVersion2HeldItemTag(text string) int {
	for at := 0; at < len(text); {
		found := strings.Index(text[at:], "<li")
		if found < 0 {
			return -1
		}
		if at += found; !notionMarkdownEscapedAt(text, at) && notionMarkdownVersion2FlowOpening.MatchString(text[at:]) {
			return at
		}
		at += len("<li")
	}
	return -1
}

// notionMarkdownVersion2InCode tells whether text[at] stands in inline code,
// between single backticks only when single.
func notionMarkdownVersion2InCode(text string, at int, single bool) bool {
	for _, code := range notionMarkdownInlineCodeRanges(text) {
		if code[0] <= at && at < code[1] && (!single || text[code[0]] == '`' && (code[0]+1 >= len(text) || text[code[0]+1] != '`')) {
			return true
		}
	}
	return false
}
