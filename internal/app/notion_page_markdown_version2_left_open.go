package app

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
)

// Where a "v2" write is refused for a list's item read inline
// (notionMarkdownVersion2BrokenItems), Notion's "v2" still reads it to its
// end and says, of the tags it leaves open, the block tags first, with the
// trailing ones its words name, and the inline tags last, each in the order
// written (seen 2026-11-15: "<li>c</li> <i>y" says the <li> cannot stand
// inline, then that <i> is never closed (notionMarkdownUnclosedInline),
// "<li>c</li> <i>y⏎<b>z" and "<li>c</li> <i>y <b>z" both, "<li>c</li>
// <i>y⏎⏎<p>z" the <p> left open (notionMarkdownVersion2TrailingSaid) before
// all; "<li>c</li> <i>y</i>" nothing more), an item
// after it left open among the block tags (seen 2026-11-15: "<ul>⏎<li>a\</li>⏎
// </ul> x⏎<p>y </li></li>⏎<li>c\</li>" says "…: <p>, <li>."). Tags
// in code, escaped or closed in themselves are none, and so are a list's,
// which the refusal says. Then each table's row, cell or header cell no
// <table> holds, starting its line, is said bare, in order (seen
// 2026-11-15: "…</ul> x⏎<td>y </li></td>" says its <td> bare, "…⏎<td>y
// </li></td>⏎<i>z" after the <i> left open, "…⏎<p>y </li></p>⏎<td>z</td>",
// "…⏎<th>y </li></th>" and "…⏎<tr>⏎<td>z</td>⏎</tr>" alike).

// notionMarkdownVersion2LeftOpenTag is an inline tag or a block tag a text
// may leave open.
var notionMarkdownVersion2LeftOpenTag = regexp.MustCompile(`<(/?)(b|i|s|u|em|strong|code|span|a|p|h[1-4]|quote|toggle|callout|details|todo)(?:[ \t][^<>]*)?(/?)>`)

// notionMarkdownVersion2LeftOpen is said, a refusal's words, with what
// Notion says of the tags lines leave open; the block tags opened in the
// span of the item read inline that are open at its closing tag it closes
// over (notionMarkdownVersion2ItemSpan).
func notionMarkdownVersion2LeftOpen(lines []string, said string, span notionMarkdownVersion2ItemSpan) string {
	type tag struct {
		name        string
		block, over bool
		line, at    int
	}
	open, fence, bare, tables := []tag{}, "", []string{}, 0
	for index, line := range lines {
		// Lines read as Markdown of their own say their own (notionMarkdownVersion2ItemsPast).
		if span.reads(index) {
			continue
		}
		rest := line[len(notionMarkdownVersion2MarkerRun.FindString(line)):]
		base := len(line) - len(strings.TrimLeft(rest, " \t"))
		text := strings.TrimSpace(rest)
		if fence != "" || notionMarkdownVersion2Fence(text) {
			if fence == "" {
				fence = notionMarkdownFenceOf(text)
			} else if notionMarkdownFenceCloses(text, fence, true) {
				fence = ""
			}
			continue
		}
		codes := notionMarkdownInlineCodeRanges(text)
		if part := notionMarkdownVersion2TablePartOpening.FindStringSubmatch(text); part != nil {
			switch {
			case part[2] == "table" && part[1] == "/":
				tables = max(tables-1, 0)
			case part[2] == "table":
				tables++
			case part[1] == "" && tables == 0:
				bare = append(bare, notionMarkdownTablePartAlone(part[2]))
			}
		}
		for _, found := range notionMarkdownVersion2LeftOpenTag.FindAllStringSubmatchIndex(text, -1) {
			if found[7] > found[6] || notionMarkdownEscapedAt(text, found[0]) ||
				slices.ContainsFunc(codes, func(code [2]int) bool { return code[0] <= found[0] && found[0] < code[1] }) {
				continue
			}
			if index > span.toLine && span.fromLine >= 0 || index == span.toLine && base+found[0] >= span.to {
				open = slices.DeleteFunc(open, func(one tag) bool { return one.over })
			}
			name := text[found[4]:found[5]]
			if found[3] == found[2] {
				block := !notionMarkdownVersion2InlineTags[name]
				open = append(open, tag{name: name, block: block, over: block && span.holds(index, base+found[0]), line: index, at: base + found[0]})
				continue
			}
			for at := len(open) - 1; at >= 0; at-- {
				if open[at].name == name {
					open = slices.Delete(open, at, at+1)
					break
				}
			}
		}
	}
	trailing, inline := []string{}, []string{said}
	if span.fromLine >= 0 {
		open = slices.DeleteFunc(open, func(one tag) bool { return one.over })
	}
	// The items after it left open are among them where they stand.
	for _, item := range span.left {
		open = append(open, tag{name: "li", block: true, line: item[0], at: item[1]})
	}
	slices.SortStableFunc(open, func(one, other tag) int { return cmp.Or(one.line-other.line, one.at-other.at) })
	for _, one := range open {
		if one.block {
			trailing = append(trailing, "<"+one.name+">")
		} else {
			inline = append(inline, notionMarkdownUnclosedInline(one.name))
		}
	}
	if len(trailing) > 0 {
		if first, rest, _ := strings.Cut(said, "\n"); strings.HasPrefix(first, notionMarkdownVersion2TrailingSaid) {
			inline[0] = strings.TrimSuffix(first, ".") + ", " + strings.Join(trailing, ", ") + "." + strings.TrimSuffix("\n"+rest, "\n")
		} else {
			inline[0] = notionMarkdownVersion2TrailingSaid + strings.Join(trailing, ", ") + ".\n" + said
		}
	}
	return strings.Join(slices.Concat(inline, bare), "\n")
}

// notionMarkdownVersion2TablePartOpening is a table's tag, or its row's or
// cell's, starting a line's text, not closed in itself.
var notionMarkdownVersion2TablePartOpening = regexp.MustCompile(`^<(/?)(table|tr|td|th)(?:[ \t][^<>]*[^/<>])?>`)
