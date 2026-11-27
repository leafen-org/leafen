package app

import "strings"

// Where an item a list's closing tag making no list left open
// (notionMarkdownVersion2ListClosings) closes at the end of a later line
// over a block's tag that line opens, left open to the end — "<ul>⏎<li>a\</li>⏎
// </ul> x⏎<p>y </li>" says "Trailing unclosed …: <p>." first
// (notionMarkdownBlockTagsOpen) — Notion's "v2" reads an item opening a line
// after it, past its marks, in that block's text where the item ends the
// Markdown: on its last line, nothing after it but spaces, its own closing
// tag ending that line, or left open to the end; and so where its closing
// tag has more after it on its line. It says that the <li> cannot stand
// inline, after the items held open outside the one closed over the block,
// not that it is bare (seen 2026-11-15: "…⏎<p>y </li>⏎<li>c</li>" says
// "Trailing …: <p>.", c's inline, a's bare, then the <p> closed as a flow
// tag; so too after lines between, a closed <p>, a blank line, in a quote,
// past an item's mark, indented, a <callout>'s, a heading's, "<p>⏎y </li>"
// and "<p>y⏎</li>", two on the line each, "<li>c" left open "…: <p>, <li>.",
// "<li>⏎c</li>", and "<li>c</li> w" with a line break or lines after it
// too; two items held open say the outer closed as a flow tag and bare
// before it: "<ul>⏎<li>a\</li>⏎<li>b\</li>⏎</ul> x⏎<p>y </li>⏎<li>c</li>"
// says a's so, c's inline, then b's bare). With a line break
// after it, it is bare as before ("…⏎<li>c</li>⏎" says a's bare, the <p>,
// then c's), and so are the items before the last ("…⏎<li>c</li>⏎<li>d</li>"
// says d's inline, c's bare after the <p>), and all where the block's
// closing tag follows ("…⏎z</p>⏎<li>c</li>").

// notionMarkdownVersion2LastLineItems marks the opening tag of each such
// item as one standing in text (notionMarkdownListTagMark); written is the
// Markdown's last line before its lists' closing tags stood on lines of
// their own (notionMarkdownVersion2ListClosings).
func notionMarkdownVersion2LastLineItems(rows []string, written string) []string {
	last := len(rows) - 1
	closed := notionMarkdownVersion2ItemClosedOverOpen(rows)
	if closed < 0 || closed >= last {
		return rows
	}
	// The block tags after it stand in its text (notionMarkdownVersion2CoveredTags).
	rows = notionMarkdownVersion2CoveredTags(rows, closed, written)
	// The Markdown ends on a line that holds more than spaces.
	tail := strings.TrimSpace(rows[last]) != ""
	// An item whose own closing tag the list's closing tag in that text
	// followed on the Markdown's last line has more after it there
	// (notionMarkdownVersion2CoveredTags).
	split := notionMarkdownVersion2ClosingSplit(rows, written)
	for index := closed + 1; index <= last; index++ {
		line := rows[index]
		at := len(line) - len(strings.TrimLeft(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):], " \t"))
		opening := notionMarkdownVersion2ItemPairOpening.FindString(line[at:])
		if opening == "" || notionMarkdownEscapedAt(line, at) {
			continue
		}
		end := notionMarkdownVersion2ItemBroken(rows, index, at+len(opening))
		if end.broken || tail && (index == last || end.line == last || end.line < 0) || split && end.line == last-1 {
			rows[index] = line[:at] + notionMarkdownListTagMark + line[at:]
		}
	}
	return rows
}

// notionMarkdownVersion2ItemClosedOverOpen is the line on which the first
// item a list making no list left open closes over a block's tag its line
// opens that no closing tag of its own closes after, or -1.
func notionMarkdownVersion2ItemClosedOverOpen(rows []string) int {
	fence, texts := "", 0
	for index, line := range rows {
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		trimmed := strings.TrimSpace(line[len(prefix):])
		if fence != "" || notionMarkdownVersion2Fence(trimmed) {
			if fence == "" {
				fence = notionMarkdownFenceOf(trimmed)
			} else if notionMarkdownFenceCloses(trimmed, fence, true) {
				fence = ""
			}
			continue
		}
		texts = notionMarkdownVersion2TextListsAfter(line, texts)
		opening := notionMarkdownVersion2ItemPairOpening.FindString(trimmed)
		if opening == "" || texts == 0 {
			continue
		}
		at := len(line) - len(strings.TrimLeft(line[len(prefix):], " \t")) + len(opening)
		end := notionMarkdownVersion2ItemBroken(rows, index, at)
		if end.line <= index || end.held {
			continue
		}
		holder := notionMarkdownVersion2ItemHolder(rows, index, end)
		if holder.name == "" {
			holder = notionMarkdownVersion2ItemWordedHolder(rows, index, end)
		}
		// One whose closing tag has more after it closes over a block's tag
		// only where words stand before that tag on its line (seen 2026-11-15:
		// "…⏎w <p>y </li> v⏎<li>c</li>" says c's inline, "…⏎<p>y </li> v⏎
		// <li>c</li>" c's bare).
		if end.broken && holder.name != "" {
			continue
		}
		// One standing in text before it on its line ("w <p>y </li>") so too.
		if holder.name == "" {
			holder.name = notionMarkdownVersion2BlockOpenedBefore(rows[end.line][:end.at])
		}
		if holder.name != "" && !notionMarkdownVersion2TagClosesAfter(strings.Join(append([]string{rows[end.line][end.at:]}, rows[end.line+1:]...), "\n"), holder.name) {
			return end.line
		}
	}
	return -1
}

// notionMarkdownVersion2ItemWordedHolder is a block's tag opening a line
// between an item's and its own closing tag's (end), with words after it
// there, that no closing tag of its own closes before that closing tag
// ("<p>y⏎</li>"); name "" for none.
func notionMarkdownVersion2ItemWordedHolder(rows []string, start int, end notionMarkdownVersion2ItemEnd) notionMarkdownVersion2ItemHolding {
	for at := end.line - 1; at > start; at-- {
		text := strings.TrimSpace(rows[at][len(notionMarkdownVersion2MarkerRun.FindString(rows[at])):])
		match := notionMarkdownVersion2ItemHolderTag.FindStringSubmatch(text)
		if match == nil {
			continue
		}
		if !notionMarkdownVersion2TagClosesAfter(strings.Join(append(append([]string{text[len(match[0]):]}, rows[at+1:end.line]...),
			rows[end.line][:end.at]), "\n"), match[1]) {
			return notionMarkdownVersion2ItemHolding{name: match[1], line: at}
		}
		break
	}
	return notionMarkdownVersion2ItemHolding{line: -1}
}

// notionMarkdownVersion2TagClosesAfter tells text after a tag of the name
// opened holding a closing tag of that name past the ones closing tags it
// opens itself ("<p>z</p>" closes none, "z</p>" one).
func notionMarkdownVersion2TagClosesAfter(text, name string) bool {
	depth := 0
	for at := strings.IndexByte(text, '<'); at >= 0; {
		rest := text[at:]
		switch {
		case strings.HasPrefix(rest, "</"+name+">"):
			if depth--; depth < 0 {
				return true
			}
		case strings.HasPrefix(rest, "<"+name) && len(rest) > len(name)+1 && strings.IndexByte(" \t>", rest[len(name)+1]) >= 0:
			depth++
		}
		next := strings.IndexByte(text[at+1:], '<')
		if next < 0 {
			break
		}
		at += next + 1
	}
	return false
}
