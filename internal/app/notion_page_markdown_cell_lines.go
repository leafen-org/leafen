package app

import (
	"regexp"
	"strings"
)

// notionMarkdownCellOpening is a line a table cell's tag opens, and what
// follows the tag on it.
var notionMarkdownCellOpening = regexp.MustCompile(`^([ \t]*<(t[dh])(?:[ \t]+[^<>]*)?>)(.*)$`)

// notionMarkdownFoldedCells puts a "v2" table cell written over several
// lines on one line, its lines joined by <br>, as the public API's Markdown
// reads it (seen 2026-11-11, 2026-11-14; "v1"'s cells,
// notionMarkdownVersion1TagTables): the line breaks right after its opening
// tag and right before its closing one are the tags' own, its other lines
// standing, blank ones too ("<td>⏎⏎a⏎⏎</td>" is "⏎a⏎", "<td>a⏎  </td>"
// "a⏎"), the indent of the lines the tag stands in going ("  <td>⏎  a⏎
// </td>" is "a"); then a text of one line keeps its edges ("<td>⏎  a  ⏎</td>"
// is "  a  "), and one of more goes without the spaces each line starts with
// and, but for its last, without those it ends with ("\n  f  \n  g  \n" is
// "f\ng  "). A cell standing on one line keeps its edges as written, so the
// reader takes the folded line as it is. Code, and a cell another tag
// follows on its closing line, are as written; a <code> tag over its lines
// holds their line breaks (notionMarkdownCodeBreak, seen 2026-11-14). Code a
// fence closes in the cell is its text, the fences' backticks a code span
// whose line breaks are spaces (seen 2026-11-14: "<td>⏎```⏎b⏎c⏎```⏎</td>" is
// the code "b c", "```js⏎b⏎```" "js b ", "a⏎```⏎b⏎```⏎c" "a⏎", the code "b"
// and "⏎c"); one no fence closes leaves the cell as written.
func notionMarkdownFoldedCells(lines []string) []string {
	out := make([]string, 0, len(lines))
	fence := ""
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if notionMarkdownFenceCloses(trimmed, fence, true) {
				fence = ""
			}
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			fence = notionMarkdownFenceOf(trimmed)
			out = append(out, line)
			continue
		}
		match := notionMarkdownCellOpening.FindStringSubmatch(line)
		if match == nil || strings.Contains(match[3], "</"+match[2]+">") {
			out = append(out, line)
			continue
		}
		closing := "</" + match[2] + ">"
		end := index + 1
		for end < len(lines) && !strings.Contains(lines[end], closing) && !notionMarkdownCellOpening.MatchString(lines[end]) {
			// Code a fence closes on a line of its own is the cell's text
			// (notionMarkdownCellLinesJoined); code none closes isn't.
			if strings.HasPrefix(strings.TrimSpace(lines[end]), "```") {
				closed := notionMarkdownCellFenceEnd(lines, end)
				if closed < 0 {
					break
				}
				end = closed
			}
			end++
		}
		if end >= len(lines) || !strings.Contains(lines[end], closing) {
			out = append(out, line)
			continue
		}
		at := strings.Index(lines[end], closing)
		if strings.TrimSpace(lines[end][at+len(closing):]) != "" {
			out = append(out, line)
			continue
		}
		texts := append(append([]string{match[3]}, lines[index+1:end]...), lines[end][:at])
		texts = notionMarkdownVersion2CellTexts(texts, len(match[1])-len(strings.TrimLeft(match[1], " \t")))
		out = append(out, match[1]+notionMarkdownCellLinesJoined(texts, true)+closing)
		index = end
	}
	return out
}

// notionMarkdownCellFenceEnd is the line of the fence closing the code a
// "v2" fence of backticks or tildes opens at lines[open] — a cell's text
// past it — or -1.
func notionMarkdownCellFenceEnd(lines []string, open int) int {
	trimmed := strings.TrimLeft(lines[open], " \t>")
	tildes := len(trimmed) - len(strings.TrimLeft(trimmed, "~"))
	if !notionMarkdownVersion2Fence(trimmed) && tildes < 3 {
		return -1
	}
	fence := notionMarkdownFenceOf(trimmed)
	for at := open + 1; at < len(lines); at++ {
		line := strings.TrimSpace(strings.TrimLeft(lines[at], " \t>"))
		if tildes >= 3 && len(line) >= tildes && strings.Trim(line, "~") == "" ||
			tildes < 3 && notionMarkdownFenceCloses(line, fence, true) {
			return at
		}
	}
	return -1
}

// notionMarkdownCellFence tells a cell's line a fence of backticks or
// tildes opens.
func notionMarkdownCellFence(line string) bool {
	trimmed := strings.TrimLeft(line, " \t>")
	return notionMarkdownVersion2Fence(trimmed) || strings.HasPrefix(trimmed, "~~~")
}

// notionMarkdownVersion2CellTexts are a "v2" cell's lines as its text holds
// them (notionMarkdownFoldedCells), the lines after its first in as far as
// indent.
func notionMarkdownVersion2CellTexts(texts []string, indent int) []string {
	for at := 1; at < len(texts); at++ {
		texts[at] = notionMarkdownDedentWhitespace(texts[at], indent)
	}
	if len(texts) > 1 && texts[0] == "" {
		texts = texts[1:]
	}
	if len(texts) > 1 && texts[len(texts)-1] == "" {
		texts = texts[:len(texts)-1]
	}
	if len(texts) > 1 {
		for at := range texts {
			texts[at] = strings.TrimLeft(texts[at], " \t")
			if at < len(texts)-1 {
				texts[at] = strings.TrimRight(texts[at], " \t")
			}
		}
	}
	return texts
}

// notionMarkdownDedentWhitespace takes up to width spaces and tabs off a
// line's start.
func notionMarkdownDedentWhitespace(line string, width int) string {
	taken := 0
	for taken < width && taken < len(line) && (line[taken] == ' ' || line[taken] == '\t') {
		taken++
	}
	return line[taken:]
}
