package app

import (
	"regexp"
	"slices"
	"strings"
)

// An item a list's closing tag makes no list of leaves open — "<ul>⏎<li>a\
// </li>⏎</ul> x" — is a flow tag that tag closes over ("Missing closing
// </li> tag. The flow tag…", then its bare <li>), holding what follows it
// to the end. But where what follows it ends in a bare item — an <li> no
// list holds, closed or left open, the last of the blocks around it, in an
// item or a quote too — Notion's "v2" reads the open item's <li> as a list
// tag standing in text (seen 2026-11-15, once, through its API): its line a
// paragraph's first, which the lines of text after it go on with, so
// "<ul>⏎<li>a\</li>⏎</ul> x⏎<li>c</li>" says the <li> left open and that it
// cannot stand inline, then c's bare <li>; "…⏎</ul> x⏎y <li>c</li>" only
// that it cannot stand inline, its paragraph closing it; "…⏎</ul> x <i>w⏎
// <li>c</li>" nothing of the <i>, which stands in it. Anything after the
// last bare item's closing tag, "<li>c</li> y", "<li>c</li>⏎⏎y", leaves it
// the flow tag it was, and so does an item a list holds.

var notionMarkdownVersion2LiTag = regexp.MustCompile(`<(/?)li(?:[ \t][^<>]*)?(/?)>`)

// notionMarkdownVersion2ItemSteps tells the items a line's text opens, past
// those it closes, as +1 and -1 each in order: escaped tags, ones in code
// and ones closed in themselves are none.
func notionMarkdownVersion2ItemSteps(text string) []int {
	steps := []int{}
	for _, tag := range notionMarkdownVersion2ItemStepsAt(text) {
		steps = append(steps, tag[1])
	}
	return steps
}

// notionMarkdownVersion2ItemStepsAt is each such step with the byte its tag
// starts at: {at, +1} or {at, -1}.
func notionMarkdownVersion2ItemStepsAt(text string) [][2]int {
	tags := [][2]int{}
	for _, found := range notionMarkdownVersion2LiTag.FindAllStringSubmatchIndex(text, -1) {
		if found[5] > found[4] || notionMarkdownEscapedAt(text, found[0]) || notionMarkdownVersion2InCode(text, found[0], false) {
			continue
		}
		if found[3] > found[2] {
			tags = append(tags, [2]int{found[0], -1})
		} else {
			tags = append(tags, [2]int{found[0], 1})
		}
	}
	return tags
}

// notionMarkdownVersion2EndsInBareItem tells, of lines after a list's
// closing tag, quotes deep, how many of the items it leaves open closing tags
// close, items at most, and whether the lines end in a bare item: to the
// Markdown's end, or to the closing tag of a block tag around the list. Each
// closing tag ending a line before any item opens closes one, the innermost
// first (repair.itemOverByTextList), which is a bare item they may end in
// (seen 2026-11-15: "<ul>⏎<li>a\</li>⏎<li>b\</li>⏎</ul> x⏎y </li>" says a's
// <li> left open and that it cannot stand inline, then b's bare <li>, as
// "…⏎</ul> x⏎<li>c</li>" does c's). An item a block tag after the list
// holds is none. A line of code a fence holds closes one so too (seen
// 2026-11-15: "…⏎</ul> x⏎```⏎y </li>⏎```" says only its bare <li>, with a
// line after the code too); fenced is the line in after closing the first
// such code, -1 for none.
func notionMarkdownVersion2EndsInBareItem(after []string, quotes, items int) (closes int, ends bool, fenced int) {
	depth, open, opened, ended, fence, itemAfter, held := 0, 0, false, false, "", false, false
	fenced = -1
	for index, line := range after {
		text := notionMarkdownVersion2Unquoted(line, quotes)
		body := strings.TrimSpace(text[len(notionMarkdownVersion2MarkerRun.FindString(text)):])
		switch {
		case body == "":
			continue
		case fence != "" || notionMarkdownVersion2Fence(body):
			switch {
			case fence == "":
				fence = notionMarkdownFenceOf(body)
			case notionMarkdownFenceCloses(body, fence, true):
				fence = ""
				if held && fenced < 0 {
					fenced = index
				}
			case open == 0 && !itemAfter && closes < items && strings.HasSuffix(body, "</li>"):
				closes, opened, ended, held = closes+1, true, false, true
				continue
			}
			ended = ended || open == 0 && opened
			continue
		}
		if match := notionMarkdownVersion2ContainerLine.FindStringSubmatch(body); match != nil && match[2] != "li" &&
			notionMarkdownVersion2Tags[match[2]] && !notionMarkdownVersion2InlineTags[match[2]] {
			switch {
			case match[1] == "":
				depth++
			case depth > 0:
				depth--
			case match[2] != "ul" && match[2] != "ol":
				// The tag around the list closes: its blocks end here.
				return closes, opened && !ended, fenced
			}
			ended = ended || open == 0 && opened
			continue
		}
		steps := []int{}
		if depth == 0 {
			steps = notionMarkdownVersion2ItemSteps(body)
		}
		// One with more after it on its line, in a block's tag its line opens
		// before it that no closing tag closes after, closes one read inline,
		// which ends in none (seen 2026-11-15: "…⏎</ul> x⏎w <p>y </li> v⏎
		// <li>c</li>" says a's <li> and c's inline, "…⏎<p>y </li> v⏎<li>c</li>"
		// a's left open and inline, then c's bare; notionMarkdownVersion2BrokenItems).
		if open == 0 && !itemAfter && closes < items && len(steps) > 0 && steps[0] < 0 && !strings.HasSuffix(body, "</li>") {
			at := notionMarkdownVersion2ItemStepsAt(body)[0][0]
			if name := notionMarkdownVersion2BlockOpenedBefore(body[:at]); name != "" &&
				!notionMarkdownVersion2TagClosesAfter(body[at:]+"\n"+strings.Join(after[index+1:], "\n"), name) {
				return closes + 1, false, fenced
			}
		}
		// A closing tag ending a line before any item opens closes an item
		// left open (repair.itemOverByTextList).
		if open == 0 && !itemAfter && closes < items && len(steps) > 0 && steps[0] < 0 && strings.HasSuffix(body, "</li>") {
			closes, opened, ended = closes+1, true, false
			// One closing over a block's tag its line opens, left open to the
			// end, with lines after it, ends in none: they stand in that block
			// (seen 2026-11-15: "<ul>⏎<li>a\</li>⏎<li>b\</li>⏎</ul> x⏎<p>y </li>⏎
			// <li>c</li>⏎" says a's <li> closed as a flow tag, as with "z"
			// after; notionMarkdownVersion2LastLineItems).
			rest := strings.Join(after[index+1:], "\n")
			if holder := notionMarkdownVersion2ItemHolderTag.FindStringSubmatch(body); holder != nil && strings.TrimSpace(rest) != "" &&
				!notionMarkdownVersion2TagClosesAfter(body[len(holder[0]):]+"\n"+rest, holder[1]) {
				return closes, false, fenced
			}
			// So too over one opened after words on its line ("w <p>y </li>";
			// seen 2026-11-15: "…⏎</ul> x⏎w <p>y </li>⏎<li>c</li>" says a's <li>
			// closed as a flow tag, c's inline, then b's bare).
			if name := notionMarkdownVersion2BlockOpenedBefore(strings.TrimSuffix(body, "</li>")); name != "" && strings.TrimSpace(rest) != "" &&
				!notionMarkdownVersion2TagClosesAfter(rest, name) {
				return closes, false, fenced
			}
			continue
		}
		// Words after the last item closed, but for another item's.
		if open == 0 && opened && (len(steps) == 0 || steps[0] < 0) {
			ended = true
		}
		for _, step := range steps {
			if step > 0 {
				opened, ended, itemAfter = true, false, true
			}
			open = max(open+step, 0)
		}
		// Nothing but its closing tag may end the line an item closes on.
		if opened && open == 0 && len(steps) > 0 && !strings.HasSuffix(body, "</li>") {
			ended = true
		}
	}
	return closes, opened && !ended, fenced
}

// notionMarkdownVersion2TextItemOpening tells a line whose text is an
// item's opening tag made text (notionMarkdownVersion2EndsInBareItem), or
// words (notionMarkdownVersion2EndsInCode).
func notionMarkdownVersion2TextItemOpening(line string) bool {
	body := strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):])
	rest, ok := strings.CutPrefix(body, notionMarkdownListTagMark)
	rest = strings.TrimPrefix(rest, `\`)
	return ok && strings.HasPrefix(rest, "<li") && notionMarkdownVersion2LiTag.MatchString(rest)
}

// Where the Markdown ends in code — its last line, past the list's closing
// tag, ending in code between single backticks or in <code> — the innermost
// item a list's closing tag making no list leaves open, that no closing tag
// closes, is words, said nothing of, as Notion's "v2" reads it (seen
// 2026-11-15: "<ul>⏎<li>a\</li>⏎</ul> x⏎`y </li>`", "…⏎`y`", "…⏎y `z`" and
// "…⏎</ul> x `y`" are the paragraphs "<ul>" and "<li>a</li>⏎</ul> x⏎…";
// with b's item left open after a's, a's is an item made text, as before a
// bare item; "…⏎`y` " with a space after it, "…⏎``y``" and a fence are not
// so), and so where items closed on their lines, bare, follow that code
// ("…⏎`y`⏎<li>c</li>" says only c's bare <li>). A list item's, a heading's
// or a quote's line ending so reads the items it leaves open inline instead
// ("…⏎- `y`", "…⏎- `y </li>`", "…⏎1. `y`", "…⏎# `y`" and "…⏎> `y </li>`"
// say the <li> left open and that it cannot stand inline).

// notionMarkdownVersion2EndsInCode tells lines, from a list's closing
// tag's, quotes deep, whose last that isn't blank, past items closed on
// their lines, ends in such code: "words" for a paragraph's line, "inline"
// for a list item's, a heading's or a quote's, "" for none; and whether
// such items followed it.
func notionMarkdownVersion2EndsInCode(lines []string, quotes int) (string, bool) {
	end, bare := len(lines)-1, false
	for ; end > 0; end-- {
		text := strings.TrimSpace(notionMarkdownVersion2Unquoted(lines[end], quotes))
		if text != "" && !notionMarkdownVersion2ClosedItem(text) {
			break
		}
		bare = bare || text != ""
	}
	if end < 0 {
		return "", false
	}
	last, raw := notionMarkdownVersion2Unquoted(lines[end], quotes), lines[end]
	if strings.TrimSpace(raw) == "" || strings.TrimRight(raw, " \t") != raw {
		return "", false
	}
	for _, code := range notionMarkdownInlineCodeRanges(last) {
		if code[1] == len(last) && (strings.HasPrefix(last[code[0]:], "<code>") || !strings.HasPrefix(last[code[0]:], "``")) {
			if notionMarkdownVersion2ItemMarker.MatchString(last) || notionMarkdownVersion2Heading.MatchString(last) ||
				strings.HasPrefix(last, ">") {
				return "inline", bare
			}
			return "words", bare
		}
	}
	return "", false
}

// notionMarkdownVersion2WordsListTag marks the list tag a line's text
// starts with as words: escaped, said nothing of, its line a paragraph's first.
func notionMarkdownVersion2WordsListTag(line string) string {
	at := len(notionMarkdownVersion2MarkerRun.FindString(line))
	return line[:at] + notionMarkdownListTagMark + `\` + line[at:]
}

// notionMarkdownVersion2TextListStart tells a line whose text is a list's
// or an item's opening tag its list's closing tag made text, or a block's
// tag its pair made text (notionMarkdownVersion2TextPairOpening), which
// starts a paragraph.
func notionMarkdownVersion2TextListStart(line string) bool {
	return notionMarkdownVersion2TextListOpening(line) || notionMarkdownVersion2TextItemOpening(line) ||
		notionMarkdownVersion2TextPairOpening(line)
}

// An item a list's closing tag closes over where more after it makes no
// list of it ("<ul>⏎<li>a\</li>⏎</ul> x") is closed by the first closing
// tag ending a line after it, as Notion's "v2" reads it (seen 2026-11-15):
// "…⏎</ul> x⏎y </li>" says only its bare <li>, and so does "…⏎</ul> x </li>",
// a closing tag on a line of its own, after a blank line, in a heading, an
// item or a quote, or with lines after it; what follows it is read as
// any text ("…⏎y </li>⏎<li>c</li>" says both bare). One with more after it on
// its line is text ("y </li> w"), and so is an escaped one or "</LI>".

// itemOverByTextList tells an item a list's closing tag standing in text
// closed over, left open since.
func (repair *notionMarkdownRepair) itemOverByTextList() bool {
	type opened struct {
		name       string
		text, over bool
	}
	stack := []opened{}
	for _, tag := range repair.inline {
		switch {
		case tag.closedInSelf:
		case !tag.closing:
			stack = append(stack, opened{name: tag.name, text: tag.text})
		default:
			for at := len(stack) - 1; at >= 0; at-- {
				if stack[at].name != tag.name {
					continue
				}
				for after := at + 1; after < len(stack); after++ {
					stack[after].over = stack[after].over || tag.text && (tag.name == "ul" || tag.name == "ol")
				}
				stack = slices.Delete(stack, at, at+1)
				break
			}
		}
	}
	for at := len(stack) - 1; at >= 0; at-- {
		if stack[at].name == "li" && !stack[at].text {
			return stack[at].over
		}
	}
	return false
}
