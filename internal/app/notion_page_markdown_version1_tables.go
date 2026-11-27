package app

import (
	"regexp"
	"strings"
)

// A public API write's "v1" Markdown reads a line that starts with "|" as a
// table's row, as Notion's does (seen 2026-11-14, each written and read back
// as blocks), with no delimiter row needed: "|b|", "|b", "|b|x", "| b | c"
// and "|" are tables of one row, in a list item or a to-do too ("- |b|"
// holds one). Its rows are the lines starting with "|" that follow it,
// however far in and with blank lines between ("|a|⏎⏎⏎\t|b|" is one table
// of two rows); any other line ends it, and lines further in after it stand
// beside it, as a table holds no blocks. Its second row, when each of its
// cells is dashes with a colon at either end or none ("|-|", "| :-: |"), is
// no row but its column header. A row's cells are what its "|" split it
// into, the first piece and an empty last one left out, each trimmed: a
// backslash or backticks keep no "|" in a cell ("|a\|b|" is "a\" and "b"),
// and a tag in one is its text ("|b|</code>" is "b" and "</code>"). It is as
// wide as its last row, the others cut to it or filled with empty cells
// ("|a|b|⏎|c|" is "a" and "c"). Leafen writes it as its tags
// (notionMarkdownWriter.apiTable), as its reader takes them.

var notionMarkdownVersion1PipeDelimiter = regexp.MustCompile(`^:?-+:?$`)

var notionMarkdownVersion1CellTag = strings.NewReplacer(
	"<table", `\<table`, "</table", `\</table`, "<tr", `\<tr`, "</tr", `\</tr`,
	"<td", `\<td`, "</td", `\</td`, "<th", `\<th`, "</th", `\</th`,
)

// notionMarkdownVersion1PipeTable is the table whose first row is a line's
// rest at lines[index], standing as far in as written, as Leafen's form
// writes one; the lines its other rows take, and the blank ones between
// them, read as none.
func notionMarkdownVersion1PipeTable(lines []string, index int, rest, written string) string {
	rows := [][]string{notionMarkdownVersion1PipeCells(strings.TrimSpace(rest))}
	for next := index + 1; next < len(lines); next++ {
		trimmed := strings.TrimSpace(lines[next])
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			break
		}
		for at := index + 1; at <= next; at++ {
			lines[at] = notionMarkdownVersion1Dropped
		}
		index = next
		rows = append(rows, notionMarkdownVersion1PipeCells(trimmed))
	}
	opening := "<table>"
	if len(rows) > 1 && notionMarkdownVersion1PipeHeader(rows[1]) {
		opening = `<table header-row="true">`
		rows = append(rows[:1], rows[2:]...)
	}
	width := len(rows[len(rows)-1])
	table := []string{written + opening}
	for _, cells := range rows {
		table = append(table, "<tr>")
		for column := range width {
			cell := ""
			if column < len(cells) {
				cell = cells[column]
			}
			table = append(table, "<td>"+cell+"</td>")
		}
		table = append(table, "</tr>")
	}
	return strings.Join(append(table, written+"</table>"), "\n")
}

// notionMarkdownVersion1PipeCells are a row's cells, written as a cell's
// text in Leafen's form: the "v2" tags and the table's own escaped, and a
// "\" ending one as written.
func notionMarkdownVersion1PipeCells(trimmed string) []string {
	pieces := strings.Split(trimmed, "|")[1:]
	if len(pieces) > 1 && strings.TrimSpace(pieces[len(pieces)-1]) == "" {
		pieces = pieces[:len(pieces)-1]
	}
	cells := make([]string, len(pieces))
	for at, piece := range pieces {
		cell := strings.TrimSpace(piece)
		if (len(cell)-len(strings.TrimRight(cell, `\`)))%2 == 1 {
			cell += `\`
		}
		cells[at] = notionMarkdownVersion1Text(notionMarkdownVersion1CellTag.Replace(cell))
	}
	return cells
}

// notionMarkdownVersion1PipeHeader tells a row that marks the one before it
// its table's column header.
func notionMarkdownVersion1PipeHeader(cells []string) bool {
	for _, cell := range cells {
		if !notionMarkdownVersion1PipeDelimiter.MatchString(cell) {
			return false
		}
	}
	return len(cells) > 0
}
