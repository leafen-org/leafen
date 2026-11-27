package app

import (
	"slices"
	"strings"
)

// table writes a plain table — a header row and nothing more — as a pipe
// table and any other as <table> with each cell's text on its own line, its
// columns' colors in <colgroup> and its rows' and cells' on <tr> and <td>.
func (w *notionMarkdownWriter) table(block workspaceExportMarkdownBlock, prefix string) ([]string, bool) {
	rows := [][]string{}
	rowColors, cellColors := []string{}, [][]string{}
	columnTags := notionMarkdownColumnTags(block.Content, prefix, false)
	fitted, _ := block.Content["fitPageWidth"].(bool)
	colored := len(columnTags) > 0 || fitted
	for _, row := range w.children[block.ID] {
		cells, written := notionMarkdownTableRowCells(row, w.mentions, "<br />")
		if !written {
			return nil, false
		}
		rows = append(rows, cells)
		color, _ := row.Content["color"].(string)
		cellColor := notionMarkdownStringList(row.Content["cellColors"])
		rowColors, cellColors = append(rowColors, color), append(cellColors, cellColor)
		colored = colored || color != "" || strings.Join(cellColor, "") != ""
	}
	columnHeader, _ := block.Content["hasColumnHeader"].(bool)
	rowHeader, _ := block.Content["hasRowHeader"].(bool)
	w.rendered = append(w.rendered, block.ID)
	for _, row := range w.children[block.ID] {
		w.visited[row.ID] = true
		w.rendered = append(w.rendered, row.ID)
	}
	if columnHeader && !rowHeader && !colored {
		// One without rows is an empty header cell, a row without cells none,
		// as Notion writes them (seen 2026-11-14: "|  |⏎| --- |", "|  |⏎|  |").
		if len(rows) == 0 {
			rows = [][]string{{""}}
		}
		lines := []string{}
		for index, cells := range rows {
			escaped := make([]string, len(cells))
			for column, cell := range cells {
				escaped[column] = notionMarkdownCellPipes(cell)
			}
			lines = append(lines, prefix+"| "+strings.Join(escaped, " | ")+" |")
			if index == 0 {
				lines = append(lines, prefix+"| "+strings.Join(slices.Repeat([]string{"---"}, len(cells)), " | ")+" |")
			}
		}
		return lines, true
	}
	opening := "<table"
	if fitted {
		opening += ` fit-page-width="true"`
	}
	if columnHeader {
		opening += ` header-row="true"`
	}
	if rowHeader {
		opening += ` header-column="true"`
	}
	lines := append([]string{prefix + opening + ">"}, columnTags...)
	for at, cells := range rows {
		row := []string{prefix + "<tr" + notionMarkdownColorAttribute(rowColors[at]) + ">"}
		for column, cell := range cells {
			color := ""
			if column < len(cellColors[at]) {
				color = cellColors[at][column]
			}
			// An empty cell closes at once, as Notion writes it (seen 2026-11-14).
			if opening := prefix + "<td" + notionMarkdownColorAttribute(color) + ">"; cell == "" {
				row = append(row, opening+"</td>")
			} else {
				row = append(row, opening, prefix+cell, prefix+"</td>")
			}
		}
		// Each row is a block of its own where it stands in a page's Markdown
		// (notion_page_markdown_block_marks.go).
		row = append(row, prefix+"</tr>")
		lines = append(lines, w.marked(w.children[block.ID][at].ID, graphBlockTypeTableRow, prefix, row)...)
	}
	return append(lines, prefix+"</table>"), true
}
