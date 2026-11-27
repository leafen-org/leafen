package app

import "strings"

// A list's item tag alone on its line, no list holding it, holding no words
// before its own closing tag, where that tag stands in the text of a block's
// tag its line opens and closes, is its words as Notion's "v2" reads it,
// said nothing of (seen 2026-11-15, create and replace alike): "<li>⏎<p>y
// </li></p>" is the paragraphs "<li>" and "y </li>", and so a callout's, a
// heading's or a quote's, "</li> z" or "</p> w" after it, a blank line
// before it, lines after it, a quote's or an item's line ("> <li>⏎> <p>y
// </li></p>" is the quote "<li>" holding the paragraph) alike; a bare item
// after it is said bare alone ("…⏎<li>c</li>"), a cell's tag said bare
// ("<li>⏎<td>y </li></td>"). In a list it is none of its items, refused
// (notionMarkdownVersion2BrokenItems); one whose closing tag ends its line
// ("<li>⏎<p>y </li>") is a bare item.

// notionMarkdownVersion2LoneTextItems escapes the item tag of each item so
// read as its words.
func notionMarkdownVersion2LoneTextItems(lines []string) []string {
	fence, lists, texts := "", 0, 0
	for index, line := range lines {
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		trimmed := strings.TrimSpace(line[len(prefix):])
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(trimmed, fence, true) {
				fence = ""
			}
			continue
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
			continue
		case strings.HasPrefix(trimmed, "<ul") || strings.HasPrefix(trimmed, "<ol"):
			lists++
			continue
		case (trimmed == "</ul>" || trimmed == "</ol>") && lists > 0:
			lists--
			continue
		}
		if texts = notionMarkdownVersion2TextListsAfter(line, texts); trimmed != "<li>" || lists > 0 || texts > 0 {
			continue
		}
		at := len(line) - len(strings.TrimLeft(line[len(prefix):], " \t")) + len("<li>")
		end := notionMarkdownVersion2ItemBroken(lines, index, at)
		if end.broken && end.held && !notionMarkdownVersion2ItemWorded(lines, index, end.line, "") {
			lines[index] = line[:at-len("<li>")] + `\` + line[at-len("<li>"):]
		}
	}
	return lines
}
