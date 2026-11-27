package app

import (
	"maps"
	"slices"
	"strings"
)

// A block's tag a "v2" line opens pairs with closing tags of its kind as
// Notion's "v2" pairs them, counting the tags of its kind that code between
// two or more backticks holds (seen 2026-11-14): "<callout>⏎``<callout>``⏎
// </callout>⏎</callout>" is a callout holding "``<callout>``⏎</callout>",
// its first closing tag the code's tag's and so text, in a toggle, a quote,
// a <details>, a <p> or a column too. A tag whose closing tags the code's
// tags so take is left open: where nothing but blank lines follows the last
// closing tag of its kind it is text ("<callout>⏎``a<callout>``⏎</callout>" is the paragraph
// of its lines, "<callout>⏎<callout>⏎``<callout>``⏎</callout>⏎</callout>"
// the paragraph "<callout>" and a callout), and otherwise Notion refuses it
// ("<callout>⏎``a<callout>``⏎</callout>⏎z" says the callout is never
// closed, notionMarkdownUnclosedBlock). Code
// between single backticks or in <code> holds no tag so
// (notionMarkdownVersion2PairMasks), and a closing tag a tag in text takes
// is read as text reads it (notionMarkdownRepair.openInText).

var notionMarkdownVersion2CodePairNames = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "quote": true, "toggle": true, "callout": true,
	"details": true, "column": true, "tab": true, "synced-block": true,
}

// A list's tags pair so too, before its items are read (seen 2026-11-14):
// "<ul>⏎<li>⏎``<li>``⏎</li>⏎</li>⏎</ul>" is an item holding the code
// "<li>" and "⏎</li>", "<ul>⏎<li>⏎``a<li>``⏎</li>⏎</ul>" refuses a list
// holding text, its item's tag left open being text as its list's closing
// tag follows, and "<ul>⏎<li>⏎``<ul>``⏎</li>⏎</ul>" a bare item, the
// list's tag text.

var notionMarkdownVersion2ListPairNames = map[string]bool{"ul": true, "ol": true, "li": true}

// notionMarkdownVersion2CodePairs writes as text the closing tags that tags
// in code take and the tags left open so, or what Notion refuses of them.
func notionMarkdownVersion2CodePairs(lines []string) ([]string, string) {
	return notionMarkdownVersion2CodePairsOf(lines, notionMarkdownVersion2CodePairNames)
}

// notionMarkdownVersion2CodePairsOf pairs so the tags of names: what
// follows an item's last closing tag counts to its list's closing tag.
func notionMarkdownVersion2CodePairsOf(lines []string, names map[string]bool) ([]string, string) {
	type opened struct {
		line, at          int
		inCode, lineStart bool
		// taken tells one whose closing tag a tag in code after it took.
		taken bool
	}
	stacks := map[string][]opened{}
	// last is where the last closing tag of each kind ends, its line and byte.
	last := map[string][2]int{}
	escapes := map[int][]int{}
	fence := ""
	for index, line := range lines {
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		trimmed := strings.TrimSpace(line[len(prefix):])
		if fence != "" {
			if notionMarkdownFenceCloses(trimmed, fence, true) {
				fence = ""
			}
			continue
		}
		if notionMarkdownVersion2Fence(trimmed) {
			fence = notionMarkdownFenceOf(trimmed)
			continue
		}
		if !strings.Contains(line, "<") {
			continue
		}
		start := len(line) - len(strings.TrimLeft(line[len(prefix):], " \t"))
		masked, codes := notionMarkdownVersion2PairMasks(line), notionMarkdownInlineCodeRanges(line)
		within := func(ranges [][2]int, at int) bool {
			return slices.ContainsFunc(ranges, func(code [2]int) bool { return code[0] < at && at < code[1] })
		}
		for _, found := range notionMarkdownRepairTag.FindAllStringSubmatchIndex(line, -1) {
			name := line[found[4]:found[5]]
			if !names[name] || found[9] > found[8] || notionMarkdownEscapedAt(line, found[0]) ||
				within(masked, found[0]) {
				continue
			}
			inCode := within(codes, found[0])
			if found[3] == found[2] {
				stacks[name] = append(stacks[name], opened{
					line: index, at: found[0], inCode: inCode, lineStart: found[0] == start && !inCode,
				})
				continue
			}
			last[name] = [2]int{index, found[1]}
			stack := stacks[name]
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if top.inCode && !inCode {
				escapes[index] = append(escapes[index], found[0])
				for at := range stack {
					stack[at].taken = true
				}
			}
			// A list's tag whose closing tag code holds, text after it, is
			// text (seen 2026-11-14: "<ul>⏎<li>⏎``a</ul>``⏎</li>⏎</ul>" is a
			// bare item), as a block's is (notionMarkdownVersion2BrokenPairs);
			// an item's so is a broken item (notionMarkdownVersion2BrokenItems:
			// "<ul>⏎<li>⏎``a</li>``⏎</li>⏎</ul>" says its list's children and
			// its <li> inline).
			if names["ul"] && name != "li" && top.lineStart && inCode && strings.TrimSpace(line[found[1]:]) != "" {
				escapes[top.line] = append(escapes[top.line], top.at)
			}
			stacks[name] = stack
		}
	}
	for _, name := range slices.Sorted(maps.Keys(stacks)) {
		for _, left := range stacks[name] {
			if !left.lineStart || !left.taken {
				continue
			}
			end := last[name]
			rest := strings.TrimSpace(lines[end[0]][end[1]:])
			for _, line := range lines[end[0]+1:] {
				if trimmed := strings.TrimSpace(line); name == "li" && (trimmed == "</ul>" || trimmed == "</ol>") {
					break
				}
				rest += strings.TrimSpace(line)
			}
			if rest != "" {
				return lines, notionMarkdownUnclosedBlock(name)
			}
			escapes[left.line] = append(escapes[left.line], left.at)
		}
	}
	for index, ats := range escapes {
		slices.Sort(ats)
		line := lines[index]
		for at := len(ats) - 1; at >= 0; at-- {
			line = line[:ats[at]] + `\` + line[ats[at]:]
		}
		lines[index] = line
	}
	return lines, ""
}
