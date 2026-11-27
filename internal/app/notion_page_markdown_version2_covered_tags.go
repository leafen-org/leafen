package app

import "strings"

// After the line on which an item a list making no list left open closes
// over a block's tag left open to the end (notionMarkdownVersion2LastLineItems),
// Notion's "v2" reads a block's tag opening a later line, past its marks,
// that no closing tag of its own closes after it — a <p>'s, a <callout>'s, a
// heading's, a <toggle>'s or a <quote>'s — and a <ul> or <ol> alone on its
// line that none closes, as tags standing in that block's text (seen
// 2026-11-15): each among the tags left open, holding no block, so that an
// item ending the Markdown there is said inline before the items closed over
// ("…⏎<p>y </li>⏎<p>z </li>⏎<li>c</li>" says "Trailing …: <p>, <p>.", c's
// inline, a's bare, then the first <p> closed as a flow tag; "…⏎<p>z",
// "…⏎<callout>z", "…⏎<h2>z", "…⏎<ul>" and "…⏎<ol>" before "<li>c</li>"
// alike, "…⏎<ol>⏎<li>c" "…: <p>, <ol>, <li>."), and an item closed before
// it bare after them ("…⏎<ol>⏎<li>c</li>⏎<li>d</li>" says c's bare last,
// "…⏎<ol>⏎<li>c</li>⏎" too) — no list holding them. A block's closing tag
// after it ("…⏎<p>z</p>⏎<li>c</li>") leaves it the block it is, and so does
// a list's closing tag a line break follows; one ending the Markdown's last
// line stands in that text too, the list's items bare after the items
// closed over ("…⏎<ul>⏎<li>c</li>⏎</ul>" says c's bare after the <p> closed
// as a flow tag, "</ul> " and an <ol>'s alike, "…⏎</ul>⏎" nothing of c).

// notionMarkdownVersion2CoveredTags marks the tags so read on lines after
// rows[closed] as tags standing in text (notionMarkdownListTagMark).
func notionMarkdownVersion2CoveredTags(rows []string, closed int, written string) []string {
	fence := ""
	for index := closed + 1; index < len(rows); index++ {
		line := rows[index]
		text := strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):])
		if fence != "" || notionMarkdownVersion2Fence(text) {
			if fence == "" {
				fence = notionMarkdownFenceOf(text)
			} else if notionMarkdownFenceCloses(text, fence, true) {
				fence = ""
			}
			continue
		}
		after := strings.Join(rows[index+1:], "\n")
		if match := notionMarkdownVersion2ItemHolderTag.FindStringSubmatch(text); match != nil {
			if !notionMarkdownVersion2TagClosesAfter(text[len(match[0]):]+"\n"+after, match[1]) {
				rows[index] = notionMarkdownVersion2MarkListTag(line)
			}
			continue
		}
		// One holding more on its line stands in that text too
		// (notionMarkdownVersion2CoveredListLine).
		if notionMarkdownVersion2CoveredListLine(rows, index, written) {
			continue
		}
		if match := notionMarkdownVersion2ListLine.FindStringSubmatch(text); match != nil {
			// Its closing tag on the Markdown's last line stands in that text too.
			switch closing := notionMarkdownVersion2ListClosingLine(rows, index, match[1]); {
			case closing == len(rows)-1:
				rows[closing] = notionMarkdownVersion2MarkListTag(rows[closing])
				fallthrough
			case closing < 0:
				rows[index] = notionMarkdownVersion2MarkListTag(line)
			}
		}
	}
	return rows
}

// notionMarkdownVersion2ListClosingLine is the line of the closing tag
// starting a line's text, past its marks, that closes a list of the kind
// opening rows[opened], past the lists of its kind opened after it; -1 for
// none.
func notionMarkdownVersion2ListClosingLine(rows []string, opened int, kind string) int {
	depth := 0
	for index := opened + 1; index < len(rows); index++ {
		text := strings.TrimSpace(rows[index][len(notionMarkdownVersion2MarkerRun.FindString(rows[index])):])
		switch {
		case strings.HasPrefix(text, "<"+kind) && notionMarkdownVersion2ListLine.MatchString(text):
			depth++
		case strings.HasPrefix(text, "</"+kind+">"):
			if depth--; depth < 0 {
				return index
			}
		}
	}
	return -1
}

// notionMarkdownVersion2TextGroupList are the list tags that are said among
// the inline tags of the text they stand in (notionMarkdownListTagMark).
var notionMarkdownVersion2TextGroupList = map[string]bool{"ul": true, "ol": true, "li": true, "todo": true}
