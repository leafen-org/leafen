package app

import (
	"regexp"
	"strings"
)

// What Notion's "v2" says, after the tags left open, of a table that code
// or an equation nothing closes swallowed from a cell (seen 2026-11-14,
// twenty shapes): with the table's closing tag on the Markdown's last line,
// each row whose <tr> starts its line and whose </tr> ends one is bare, and
// its one cell too when it holds one — "<table>⏎<tr><td>a⏎```⏎b⏎```</td></tr>⏎
// </table>" says a bare <tr> and <td>, "…```</td><td>c</td></tr>⏎</table>" a
// bare <tr> alone, a row starting after another's closing tag on its line
// nothing — a <thead> or <tbody> on a line of its own first; with the line
// closing the code last, its row's or table's children, as the closing tag
// ending it leaves them ("…```</td>" says the row's, "…```</td></tr>" the
// table's); with text after the table, nothing more.

var (
	notionMarkdownVersion2RowLine     = regexp.MustCompile(`^<tr(?:[ \t][^<>]*)?>`)
	notionMarkdownVersion2CellTag     = regexp.MustCompile(`<(td|th)(?:[ \t][^<>]*)?>`)
	notionMarkdownVersion2SectionLine = regexp.MustCompile(`^<(thead|tbody)(?:[ \t][^<>]*)?>$`)
)

// notionMarkdownVersion2SwallowedTable is what Notion says of the table so
// swallowed in lines, as written, the tags left open said as trailing.
func notionMarkdownVersion2SwallowedTable(lines []string, trailing string) []string {
	if !strings.Contains(trailing, "<table>") {
		return nil
	}
	last := len(lines) - 1
	for last >= 0 && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	table := -1
	for at := last; at >= 0 && table < 0; at-- {
		if strings.TrimSpace(lines[at]) == "<table>" || strings.HasPrefix(strings.TrimSpace(lines[at]), "<table ") {
			table = at
		}
	}
	if table < 0 {
		return nil
	}
	end := strings.TrimSpace(lines[last])
	if end != "</table>" {
		// The line closing the code last.
		if !strings.HasPrefix(end, "```") && !strings.HasPrefix(end, "~~~") && !strings.HasPrefix(end, "$$") {
			return nil
		}
		switch {
		case strings.HasSuffix(end, "</tr>"):
			return []string{notionMarkdownHoldsOnly("table", "<colgroup>, <thead>, <tbody>, <tr>") + notionMarkdownPartialChildSaid}
		case strings.HasSuffix(end, "</td>") || strings.HasSuffix(end, "</th>"):
			return []string{notionMarkdownHoldsOnly("tr", "<td>, <th>") + notionMarkdownPartialChildSaid}
		}
		return nil
	}
	said, row, cells, kind := []string{}, false, 0, ""
	bare := func(name string) string {
		return notionMarkdownTablePartAlone(name)
	}
	for _, line := range lines[table+1 : last] {
		text := strings.TrimSpace(line)
		if match := notionMarkdownVersion2SectionLine.FindStringSubmatch(text); match != nil {
			said = append(said, bare(match[1]))
			continue
		}
		if notionMarkdownVersion2RowLine.MatchString(text) {
			row, cells = true, 0
		}
		for _, cell := range notionMarkdownVersion2CellTag.FindAllStringSubmatch(text, -1) {
			cells, kind = cells+1, cell[1]
		}
		if strings.HasSuffix(text, "</tr>") {
			if row && strings.Count(text, "<tr") <= 1 && (strings.Index(text, "<tr") < 0 || notionMarkdownVersion2RowLine.MatchString(text)) {
				said = append(said, bare("tr"))
				if cells == 1 {
					said = append(said, bare(kind))
				}
			}
			row = false
		} else if strings.Contains(text, "</tr>") {
			row = false
		}
	}
	return said
}
