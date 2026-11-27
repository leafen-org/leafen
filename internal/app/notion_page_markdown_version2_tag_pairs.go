package app

import (
	"regexp"
	"slices"
	"strings"
)

// A block's tag opening a "v2" line pairs with the closing tag Notion's "v2"
// reads as its own, and where text follows that one on its line the opening
// tag is text and what it would hold stands as it is (seen 2026-11-14):
// "<callout>⏎a</callout>b⏎</callout>", "<callout>a</callout>b", "<p>a</p>
// b", "<callout>⏎a⏎</callout>." and "<callout>⏎a⏎</callout></callout>" are
// text as written, "- <callout>⏎  ``a</callout>``⏎  </callout>" an item's;
// "<callout>⏎<callout>⏎a⏎</callout>⏎</callout> x" is the paragraph
// "<callout>", the callout "a" and the paragraph "</callout> x";
// "<columns>⏎<column>⏎``a</column>``⏎</column>⏎</columns>" is refused, its
// columns holding text. Its own is the first closing tag of its kind after
// it that closes as many as open after it, past code between single
// backticks and <code> tags, but not past code between more backticks or
// fenced code: "<callout>⏎``a</callout>``⏎</callout>" is the paragraph
// "<callout>⏎", the code "a</callout>" and "⏎</callout>",
// "<callout>⏎`a``b</callout>`⏎</callout>" a callout holding the code
// "a``b</callout>"; "<empty-block>⏎</empty-block> y" is the paragraph of
// its words, and so are a contents' and an <unknown>'s (their pairs
// refused otherwise, notionMarkdownRepair.selfClosing). v1's names are not
// read so ("<synced_block>⏎
// ``a</synced_block>``⏎</synced_block>" is a synced block), nor are lists'
// tags.

var notionMarkdownVersion2PairOpening = regexp.MustCompile(`^<(p|h[1-4]|quote|toggle|callout|details|column|tab|synced-block|empty-block|table-of-contents|unknown)(?:[ \t]+[^<>]*)?>`)

// notionMarkdownVersion2BrokenPairs writes a block's tag opening a line as
// text where the closing tag Notion pairs with it has text after it on its
// line, outside code.
func notionMarkdownVersion2BrokenPairs(lines []string) []string {
	fence, items := "", 0
	for index, line := range lines {
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		trimmed := strings.TrimSpace(line[len(prefix):])
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		}
		if notionMarkdownVersion2Fence(trimmed) {
			fence = notionMarkdownFenceOf(trimmed)
			continue
		}
		held := items
		for _, step := range notionMarkdownVersion2ItemSteps(trimmed) {
			items = max(items+step, 0)
		}
		lead := len(line) - len(strings.TrimLeft(line[len(prefix):], " \t"))
		match := notionMarkdownVersion2PairOpening.FindStringSubmatch(line[lead:])
		if match == nil || strings.HasSuffix(match[0], "/>") {
			continue
		}
		closer, at := notionMarkdownVersion2PairBroken(lines, index, lead+len(match[0]), match[1])
		if closer < 0 || held > 0 && notionMarkdownVersion2PairItemClosed(lines[index:closer]) {
			continue
		}
		// Its closing tag is text too, which closes no other, outside the
		// code it may stand in ("<callout>⏎<callout>⏎a⏎</callout>b⏎</callout>"
		// is a callout holding "<callout>⏎a⏎</callout>b"), and so is the
		// <summary> a <details> starts with.
		if !slices.ContainsFunc(notionMarkdownInlineCodeRanges(lines[closer]), func(code [2]int) bool { return code[0] <= at && at < code[1] }) {
			lines[closer] = lines[closer][:at] + `\` + lines[closer][at:]
		}
		// Its line starts a paragraph all the same (notionMarkdownVersion2TextPairOpening).
		lines[index] = lines[index][:lead] + notionMarkdownListTagMark + `\` + lines[index][lead:]
		if next := index + 1; match[1] == "details" && next < len(lines) {
			summary := len(lines[next]) - len(strings.TrimLeft(lines[next], " \t>"))
			if strings.HasPrefix(lines[next][summary:], "<summary") {
				lines[next] = lines[next][:summary] + `\` + lines[next][summary:]
			}
		}
	}
	return lines
}

// notionMarkdownVersion2TextPairOpening tells a line whose text is a
// block's tag made text so (notionMarkdownVersion2BrokenPairs): its line
// starts a paragraph, as Notion's "v2" reads it, which the lines of text
// after it go on with (seen 2026-11-15: "x⏎<p>a</p> b⏎c" is the paragraphs
// "x" and "<p>a</p> b⏎c" — a callout's, a heading's, a toggle's, a quote's,
// a <details>, a column's, a tab's, a synced block's, an empty block's, a
// contents' and an <unknown> alike, one over lines, "x⏎<callout>⏎a⏎
// </callout> b" too; after an item or a quote it stands after them, in a
// quote or further in under an item held by them: "> x⏎> <p>a</p> b" is the
// quote "x" holding the paragraph). One escaped as written is words that go
// on with the text before them ("x⏎\<p>a</p> b" is one paragraph).
func notionMarkdownVersion2TextPairOpening(line string) bool {
	body := strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):])
	return strings.HasPrefix(body, notionMarkdownListTagMark+`\<`)
}

// notionMarkdownVersion2PairItemClosed tells lines, from a block's tag's
// opening in an item left open to the line of its own closing tag, of which
// one ends in an item's closing tag: that closes the item over it, the tag
// then closed as a flow tag, as Notion's "v2" reads it (seen 2026-11-15:
// "<ul>⏎<li>a\</li>⏎</ul> x⏎<p>y </li>⏎z</p> w" says a's bare <li>, then the
// <p> closed so; notionMarkdownBlockTagsOpen), its own closing tag after
// closing nothing.
func notionMarkdownVersion2PairItemClosed(lines []string) bool {
	return slices.ContainsFunc(lines, func(line string) bool {
		text := strings.TrimRight(line, " \t")
		return strings.HasSuffix(text, "</li>") && !notionMarkdownEscapedAt(text, len(text)-len("</li>")) &&
			!notionMarkdownVersion2InCode(text, len(text)-len("</li>"), false)
	})
}

// notionMarkdownVersion2PairBroken is the closing tag of the tag of the
// name opening at lines[start] before column from, its line and where it
// stands there, when text follows it on its line; -1 for none.
func notionMarkdownVersion2PairBroken(lines []string, start, from int, name string) (int, int) {
	index, at, end := notionMarkdownVersion2PairOf(lines, start, from, name)
	if index < 0 || strings.TrimSpace(lines[index][end:]) == "" {
		return -1, 0
	}
	return index, at
}

// notionMarkdownVersion2PairOf is the closing tag Notion pairs with the tag
// of the name opening at lines[start] before column from: its line, and
// where it starts and ends there; -1 for none.
func notionMarkdownVersion2PairOf(lines []string, start, from int, name string) (int, int, int) {
	depth := 1
	for index := start; index < len(lines); index++ {
		line, at := lines[index], 0
		if index == start {
			at = from
		}
		masked, codes := notionMarkdownVersion2PairMasks(line), notionMarkdownInlineCodeRanges(line)
		for _, found := range notionMarkdownRepairTag.FindAllStringSubmatchIndex(line, -1) {
			if found[0] < at || line[found[4]:found[5]] != name || found[9] > found[8] || notionMarkdownEscapedAt(line, found[0]) ||
				slices.ContainsFunc(masked, func(code [2]int) bool { return code[0] < found[0] && found[0] < code[1] }) {
				continue
			}
			// On the tag's own line its text runs past code between any
			// backticks to a closing tag after it outside code, or else the
			// first closing tag closes it (seen 2026-11-14: "<p>``a</p>``"
			// leaves <p> open, "<callout>a ``b</callout>`` c</callout>" is
			// the paragraph of its words, "<callout>a `b</callout>` c</callout>"
			// a callout).
			if index == start && found[3] != found[2] &&
				slices.ContainsFunc(codes, func(code [2]int) bool { return code[0] <= found[0] && found[0] < code[1] }) &&
				!notionMarkdownVersion2ClosingOutsideCode(line[found[1]:], "</"+name+">") {
				continue
			}
			if found[3] == found[2] {
				// One in code took its closing tag already (notionMarkdownVersion2CodePairs).
				if !slices.ContainsFunc(codes, func(code [2]int) bool { return code[0] < found[0] && found[0] < code[1] }) {
					depth++
				}
				continue
			}
			if depth--; depth == 0 {
				return index, found[0], found[1]
			}
		}
	}
	return -1, 0, 0
}

// notionMarkdownVersion2PairMasks are where a line's code stands as Notion
// reads it pairing tags: between two backticks standing alone, no backtick
// beside either, and in a <code> tag.
func notionMarkdownVersion2PairMasks(line string) [][2]int {
	masked, opened := [][2]int{}, -1
	for at := 0; at < len(line); at++ {
		switch {
		case line[at] == '`':
			if at > 0 && line[at-1] == '`' || at+1 < len(line) && line[at+1] == '`' || notionMarkdownEscapedAt(line, at) {
				continue
			}
			if opened < 0 {
				opened = at
				continue
			}
			masked, opened = append(masked, [2]int{opened, at + 1}), -1
		case opened < 0 && strings.HasPrefix(line[at:], "<code>"):
			if end := strings.Index(line[at:], "</code>"); end >= 0 {
				masked = append(masked, [2]int{at, at + end + len("</code>")})
				at += end + len("</code>") - 1
			}
		}
	}
	return masked
}
