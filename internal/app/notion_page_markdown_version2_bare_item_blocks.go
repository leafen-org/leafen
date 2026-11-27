package app

import (
	"regexp"
	"slices"
	"strings"
)

// An item no list holds whose lines hold an item's opening Notion's "v2"
// refuses, first on its line, is read as a block of its own name is
// (notionMarkdownVersion2SameNameBlock): Notion first closes at the end of
// the Markdown's last line every item opening left open, the refused one
// counted, then the refused opening counts for none and the item ends at
// the first closing tag after it (seen 2026-11-16, written twice each). It
// says the refusal first, then the item bare and what its lines say read on
// their own, through that closing tag ("<li>a⏎<li x:y=\"1\">b</li>⏎</li>"
// says the malformed attribute, the bare <li>, then the attribute twice, as
// "a⏎<li x:y=\"1\">b</li>" does; the closing tag after it closes nothing).
// Where more stands after that closing tag on its line, the closing tags put
// at the end among it, the item is read inline, said so after what its lines
// say in place of bare ("<li>a⏎<li x:y=\"1\">b</li></li>" says the malformed
// attribute three times, then that the <li> cannot stand inline; so do
// "<li>a⏎<li x:y=\"1\">b</li> x" and "<li>a⏎<li x:y=\"1\">b</li>", "Trailing
// unclosed …: <li>." first, as an item left open says it; "…⏎c" after it
// leaves the item bare, a closing tag on its own line too).

// notionMarkdownVersion2BareItemTag is an item's opening or closing tag.
var notionMarkdownVersion2BareItemTag = regexp.MustCompile(`<(/?)li(?:[ \t][^<>]*)?(/?)>`)

// notionMarkdownVersion2BareItemBlockAt is such an item opening lines[open],
// head what stands after its opening tag there; ok false where its lines
// hold no refused item opening, or item tags Leafen can't be sure of.
func notionMarkdownVersion2BareItemBlockAt(lines []string, open int, opening, head string) (notionMarkdownVersion2SameNameBlock, bool) {
	if notionMarkdownVersion2BareItemTag.MatchString(head) {
		return notionMarkdownVersion2SameNameBlock{}, false
	}
	block := notionMarkdownVersion2SameNameBlock{opening: opening, name: "li", close: -1}
	// The item openings left open, the item's own counted, close at the end of the last line.
	left, fence := 1, ""
	read := slices.Clone(lines)
	for at := open + 1; at < len(lines); at++ {
		line := strings.TrimSpace(lines[at])
		if notionMarkdownVersion2GroupSkipped(&fence, line) {
			continue
		}
		for _, found := range notionMarkdownVersion2BareItemTag.FindAllStringSubmatchIndex(line, -1) {
			switch {
			case notionMarkdownEscapedAt(line, found[0]) || found[5] > found[4]:
			case found[3] > found[2]:
				left--
			case found[0] != 0:
				// An item's opening standing in text is left to the reading as it is.
				return notionMarkdownVersion2SameNameBlock{}, false
			default:
				left++
			}
		}
	}
	if block.autoClosed = left > 0; block.autoClosed {
		read[len(read)-1] += strings.Repeat("</li>", left)
	}
	depth, fence, after := 0, "", ""
	for at := open + 1; at < len(read) && block.close < 0; at++ {
		line := strings.TrimSpace(read[at])
		if notionMarkdownVersion2GroupSkipped(&fence, line) {
			continue
		}
		for _, found := range notionMarkdownVersion2BareItemTag.FindAllStringSubmatchIndex(line, -1) {
			switch {
			case notionMarkdownEscapedAt(line, found[0]) || found[5] > found[4]:
			case found[3] > found[2] && depth == 0:
				block.close, block.closedAfter = at, strings.TrimSpace(line[:found[0]]) != ""
				after = strings.TrimSpace(line[found[1]:])
			case found[3] > found[2]:
				depth--
			case notionMarkdownVersion2RefusedOpening(line):
				// A refused opening counts for none.
				if depth == 0 {
					block.first = append(block.first, notionMarkdownVersion2OpeningRefusal(line)...)
				}
			default:
				depth++
			}
			if block.close >= 0 {
				// Its lines through its closing tag are what it holds.
				read[at] = strings.TrimRight(read[at], " \t")
				read[at] = read[at][:len(read[at])-len(strings.TrimLeft(read[at], " \t"))+found[1]]
				break
			}
		}
	}
	if len(block.first) == 0 || block.close < 0 {
		return notionMarkdownVersion2SameNameBlock{}, false
	}
	// The closing tags put at the end that the lines after it leave closing
	// nothing land on the last line (notionMarkdownVersion2Landings).
	if block.autoClosed && block.close < len(read)-1 && notionMarkdownVersion2ItemsCloseNothing(read[block.close+1:]) {
		block.land(lines, len(lines)-1)
	}
	held := []string{}
	if head != "" {
		held = append(held, head)
	}
	held = append(held, read[open+1:block.close+1]...)
	block.inline = after != ""
	block.held = notionMarkdownVersion2Dedented(held)
	return block, true
}

// bareItemHeld says the bare item a block's lines stand in as such a block
// says it: inline in place of the bare item said at its opening, after what
// its lines say.
func (repair *notionMarkdownRepair) bareItemHeld(read notionMarkdownVersion2HeldRead) bool {
	if !read.inline {
		return false
	}
	if last := len(repair.missing) - 1; last >= 0 && repair.missing[last] == notionMarkdownBareItemSaid["li"] {
		repair.missing = slices.Delete(repair.missing, last, last+1)
	}
	repair.openings = append(repair.openings, read.first...)
	repair.missing = append(append(repair.missing, read.document...), notionMarkdownListTagInline("li"))
	return true
}
