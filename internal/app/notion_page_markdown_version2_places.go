package app

import (
	"regexp"
	"strings"
)

// notionMarkdownVersion2ItemMarker is a list item's marker a line starts with.
var notionMarkdownVersion2ItemMarker = regexp.MustCompile(`^(?:[-*+]|\d{1,9}[.)])(?:[ \t]|$)`)

// Where a "v2" write's lines stand as Notion's "v2" reads them (seen
// 2026-11-14, as CommonMark reads them), followed line by line as they are
// put (notionMarkdownVersion2Input): a line four columns or more past where
// blocks start — the left, or the text of the list item it stands in — is
// text as written, no item ("\t1. a⏎  2. b" is the paragraph "1. a⏎2. b",
// "\t1) a" "1) a"), and a numbered item from a number other than 1 starts no
// block after text standing where it stands, but does after text it stands
// out of: past a list item's text ("- x⏎2. b" is two items, "- x⏎  2. b" and
// "- x⏎    2. b" the item "x⏎2. b") or in a quote it has no > of ("> q⏎2. b"
// is a quote and an item), and under a list item starting with a tab, a
// level in ("- x⏎\t2. b" is "x" holding an item). A number of zeros before 1
// is 1 ("x⏎01. b" is "x" and an item).

// notionMarkdownVersion2Place is where the last line that started a block
// stood: the column its list item's text starts at, or -1, and whether it
// stood in a quote.
type notionMarkdownVersion2Place struct {
	item   int
	quoted bool
}

// written tells a line that is text as written.
func (place *notionMarkdownVersion2Place) written(line string) bool {
	return !strings.HasPrefix(strings.TrimSpace(line), ">") && notionMarkdownVersion2Indent(line, 0) >= max(place.item, 0)+4
}

// elsewhere tells a line that stands out of the block the line before it
// stands in, or in a block of its own in a list item, as a line written
// starts with a tab: raw is the line as written.
func (place *notionMarkdownVersion2Place) elsewhere(line, raw string) bool {
	return place.item >= 0 && (notionMarkdownVersion2Indent(line, 0) < place.item || strings.HasPrefix(raw, "\t")) ||
		place.quoted && !strings.HasPrefix(strings.TrimSpace(line), ">")
}

// read takes a line that starts a block, written telling it text as
// written, and tells whether it is a list item.
func (place *notionMarkdownVersion2Place) read(line string, written bool) bool {
	trimmed := strings.TrimSpace(line)
	column := notionMarkdownVersion2Indent(line, 0)
	place.quoted = strings.HasPrefix(trimmed, ">")
	if !written && (notionMarkdownVersion2Bullet.MatchString(trimmed) || notionMarkdownVersion2Number.MatchString(trimmed)) {
		// The text of the last item the line starts ("- \t1. a" holds "a" at 7).
		for text := trimmed; ; {
			marker := notionMarkdownVersion2ItemMarker.FindString(text)
			if marker == "" || notionMarkdownFlowThematic.MatchString(text) {
				place.item = column
				return true
			}
			column += len(strings.TrimRight(marker, " \t"))
			rest := text[len(strings.TrimRight(marker, " \t")):]
			after, spaces := strings.TrimLeft(rest, " \t"), notionMarkdownVersion2Indent(rest, column)
			if notionMarkdownVersion2ItemMarker.FindString(after) == "" {
				place.item = column + max(spaces, 1)
				return true
			}
			column, text = column+spaces, after
		}
	}
	if column < place.item {
		place.item = -1
	}
	return false
}

// notionMarkdownVersion2FromOne tells an item's number that is 1.
func notionMarkdownVersion2FromOne(number string) bool {
	return strings.TrimLeft(number, "0") == "1"
}

// notionMarkdownVersion2TopWritten follows a "v2" write's lines one by one
// and tells those four columns or more in at the top — out of block tags,
// no list's item or quote before them — which Notion's "v2" reads as text
// as written, an item's marks or an <unknown/> too (seen 2026-11-15: "    -
// <unknown url=\"…\"/>" is the paragraph of those words), as it reads a
// heading so (notionMarkdownVersion2Input).
type notionMarkdownVersion2TopWritten struct {
	depth int
	held  bool
}

// written takes the next line and tells whether it is text as written.
func (top *notionMarkdownVersion2TopWritten) written(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if top.depth == 0 && !top.held && notionMarkdownVersion2Indent(line, 0) >= 4 {
		return true
	}
	if strings.HasPrefix(trimmed, ">") || notionMarkdownVersion2Bullet.MatchString(trimmed) || notionMarkdownVersion2Number.MatchString(trimmed) {
		top.held = true
	}
	if notionMarkdownVersion2BlockTag.MatchString(trimmed) && !strings.Contains(trimmed[1:], "<") {
		if strings.HasPrefix(trimmed, "</") {
			top.depth = max(top.depth-1, 0)
		} else if !strings.HasSuffix(trimmed, "/>") {
			top.depth++
		}
	}
	return false
}
