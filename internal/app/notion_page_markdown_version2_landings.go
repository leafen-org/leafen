package app

import (
	"slices"
	"strings"
)

// Notion's "v2" reads a write's block tags twice (seen 2026-11-16, written
// twice each). First it counts every block tag's opening — one it refuses
// or one standing in text counted as any — and closes those left open: their
// closing tags go at the end of the Markdown's last line, or, for one a
// closing tag of a block around it closes over, at the end of the line before
// that closing tag, and it says them ("Trailing unclosed …"). Then it reads
// the Markdown without the openings it refuses, a block's closing tag closing
// only alone on its line. A closing tag so put that closes nothing in that
// reading — its opening refused or standing in text, or a block of its name
// that reading closed before it — is text after what that line holds (where
// it lands): an item closed there, or left open and closed there by its own
// put closing tag, is a list tag that cannot stand inline ("<toggle
// x:y=\"1\">⏎<li>a" says "…: <toggle>, <li>.", the malformed attribute and
// the <li> inline; "<toggle>⏎<callout x:y=\"1\">⏎<li>a</li>⏎</toggle>" the
// <callout> left open, its attribute and the <li> inline; "<toggle>⏎<toggle
// x:y=\"1\">⏎<li>a</li>⏎</toggle>⏎<li>b</li>" b's <li> inline, said after the
// refusal said first, then the block's lines read on their own;
// "<li>a⏎<li x:y=\"1\">b</li>⏎<li>c</li>" c's inline so, before a's bare),
// and a list's closing tag alone there has more after it, which makes no
// list of it (notionMarkdownVersion2ListClosings: "<toggle x:y=\"1\">⏎<ul>⏎
// <li>a</li>⏎</ul>" says the <toggle> left open, its attribute and a bare
// <li>). Where the closing tag of a block of its own name alone on its line
// is where it lands, nothing closes that block
// (notionMarkdownVersion2SameNameBlock).

// notionMarkdownVersion2LandingTags are the block tags whose openings the
// first reading counts.
var notionMarkdownVersion2LandingTags = []string{"p", "h1", "h2", "h3", "h4", "quote", "toggle", "callout", "ul", "ol", "li",
	"columns", "column", "tabs", "tab", "table", "thead", "tbody", "tr", "td", "th", "details", "summary"}

// notionMarkdownVersion2Landing is a block tag's opening the first reading
// leaves open: its line, the line its closing tag lands on, and whether that
// closing tag closes nothing in the second reading for its opening alone
// (refused or standing in text).
type notionMarkdownVersion2Landing struct {
	open, line int
	nothing    bool
}

// notionMarkdownVersion2Landings reads lines' block tags as Notion's first
// reading does, outside code, and gives where the closing tags of those it
// leaves open land, in the order they opened.
func notionMarkdownVersion2Landings(lines []string) []notionMarkdownVersion2Landing {
	type opened struct {
		name    string
		landing notionMarkdownVersion2Landing
	}
	stack, landed, fence := []opened{}, []notionMarkdownVersion2Landing{}, ""
	for index, line := range lines {
		if notionMarkdownVersion2GroupSkipped(&fence, strings.TrimSpace(line)) {
			continue
		}
		codes, marks := notionMarkdownInlineCodeRanges(line), len(notionMarkdownVersion2MarkerRun.FindString(line))
		for _, found := range notionMarkdownRepairTag.FindAllStringSubmatchIndex(line, -1) {
			name := line[found[4]:found[5]]
			if notionMarkdownEscapedAt(line, found[0]) || found[9] > found[8] || !slices.Contains(notionMarkdownVersion2LandingTags, name) ||
				slices.ContainsFunc(codes, func(code [2]int) bool { return code[0] <= found[0] && found[0] < code[1] }) {
				continue
			}
			first := found[0] <= marks || strings.TrimSpace(line[marks:found[0]]) == ""
			if found[3] == found[2] {
				refused := first && notionMarkdownVersion2RefusedOpening(strings.TrimSpace(line[found[0]:]))
				stack = append(stack, opened{name, notionMarkdownVersion2Landing{open: index, line: -1, nothing: refused || !first}})
				continue
			}
			at := len(stack) - 1
			for at >= 0 && stack[at].name != name {
				at--
			}
			if at < 0 {
				continue
			}
			// The ones it closes over land on the line before it, or on its line after what stands before it.
			for _, over := range stack[at+1:] {
				over.landing.line = index
				if first {
					over.landing.line = index - 1
				}
				landed = append(landed, over.landing)
			}
			stack = stack[:at]
		}
	}
	for _, left := range stack {
		left.landing.line = len(lines) - 1
		landed = append(landed, left.landing)
	}
	slices.SortStableFunc(landed, func(a, b notionMarkdownVersion2Landing) int { return a.open - b.open })
	return landed
}

// notionMarkdownVersion2LandingOf is where the closing tag of the block
// opening lines[open] lands, or -1 where the first reading closes it.
func notionMarkdownVersion2LandingOf(lines []string, open int) int {
	for _, one := range notionMarkdownVersion2Landings(lines) {
		if one.open == open {
			return one.line
		}
	}
	return -1
}

// notionMarkdownVersion2LandedItems is how many items a line a closing tag
// that closes nothing lands on holds, read inline: the item openings on it
// where it starts with one.
func notionMarkdownVersion2LandedItems(line string) int {
	body := strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):])
	if !strings.HasPrefix(body, "<li") || !notionMarkdownVersion2Opened(body, "li") || notionMarkdownVersion2RefusedOpening(body) {
		return 0
	}
	return notionMarkdownVersion2Openings(body, "li")
}

// notionMarkdownVersion2LandedListClosings marks a list's closing tag alone
// on a line where a closing tag that closes nothing lands as one with more
// after it (notionMarkdownVersion2ListClosings).
func notionMarkdownVersion2LandedListClosings(rows []string) []string {
	for _, one := range notionMarkdownVersion2Landings(rows) {
		if one.nothing && one.line >= 0 {
			if body := strings.TrimSpace(rows[one.line]); body == "</ul>" || body == "</ol>" {
				rows[one.line] += notionMarkdownVersion2LandedMark
			}
		}
	}
	return rows
}

// notionMarkdownVersion2LandedMark stands for the closing tags that land
// on a line and close nothing; no text a write sends holds the byte.
const notionMarkdownVersion2LandedMark = "\xfd"

// notionMarkdownVersion2ItemsCloseNothing tells lines whose item closing
// tags, read without the openings Notion refuses, close more than they open.
func notionMarkdownVersion2ItemsCloseNothing(lines []string) bool {
	depth, fence := 0, ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if notionMarkdownVersion2GroupSkipped(&fence, line) {
			continue
		}
		for _, found := range notionMarkdownVersion2BareItemTag.FindAllStringSubmatchIndex(line, -1) {
			switch {
			case notionMarkdownEscapedAt(line, found[0]) || found[5] > found[4]:
			case found[3] > found[2]:
				if depth--; depth < 0 {
					return true
				}
			case found[0] != 0 || !notionMarkdownVersion2RefusedOpening(line):
				depth++
			}
		}
	}
	return false
}

// land records that a closing tag Notion puts for the block lands closing
// nothing on lines[landing]: the items there read inline, said after the
// refusals said first, and the block's opening is one Notion closes at the
// end, or before a closing tag around it.
func (block *notionMarkdownVersion2SameNameBlock) land(lines []string, landing int) {
	block.autoClosed, block.landing, block.landed = landing == len(lines)-1, landing, notionMarkdownVersion2LandedItems(lines[landing])
	for range block.landed {
		block.first = append(block.first, notionMarkdownListTagInline("li"))
	}
}

// notionMarkdownVersion2Unlisted is a line with its item openings escaped,
// text the reading says nothing of.
func notionMarkdownVersion2Unlisted(line string) string {
	out := ""
	for {
		at := strings.Index(line, "<li")
		if at < 0 {
			return out + line
		}
		if end := at + 3; !notionMarkdownEscapedAt(line, at) && (end == len(line) || strings.ContainsRune(" \t/>", rune(line[end]))) {
			out += line[:at] + `\`
		} else {
			out += line[:at]
		}
		out, line = out+"<li", line[at+3:]
	}
}
