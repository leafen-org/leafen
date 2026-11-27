package app

import (
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// The blocks that hold others or span lines read in the forms Notion's page
// Markdown used until 2026-10-17 and Leafen wrote until 2026-10-29, which
// Notion's current tags are first put in (notion_page_markdown_containers.go):
// a toggle as <details> with its title in <summary> and its blocks one level
// in, a toggle heading as its heading with {toggle="true"}, an equation
// between $$ lines, a table as <table> with a <tr> line for each row and a
// <td> line for each cell, columns as <columns> with a <column> one level in
// for each, and tabs as <tabs> with each tab's name one level in. Each block
// reads back as one item, so a page's Markdown written back keeps them.

var (
	notionMarkdownToggleHeadingPattern = regexp.MustCompile(`^(#{1,6})(?:[ \t]+(.+?))?[ \t]+\{toggle="true"\}$`)
	notionMarkdownSummaryPattern       = regexp.MustCompile(`^<summary>(.*)</summary>$`)
	notionMarkdownDetailsPattern       = regexp.MustCompile(`^<details(?:[ \t]+color="([a-z_]*)")?[ \t]*>$`)
	notionMarkdownTableOpenPattern     = regexp.MustCompile(`^<table((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>$`)
	notionMarkdownTableCellPattern     = regexp.MustCompile(`^<td(?: color="([a-z_]+)")?>(.*)</td>$`)
	notionMarkdownTableRowPattern      = regexp.MustCompile(`^<tr(?: color="([a-z_]+)")?>$`)
	notionMarkdownColumnPattern        = regexp.MustCompile(`^<column(?:[ \t]+ratio="([0-9.]+)")?[ \t]*>$`)
	notionMarkdownTabNamePattern       = regexp.MustCompile(`^<tab-name icon="([^"]*)">(.*)$`)
)

// notionMarkdownToggleHeadings are the toggle headings, by their Markdown
// marks.
var notionMarkdownToggleHeadings = map[string]string{
	graphBlockTypeToggleHeading1: "#", graphBlockTypeToggleHeading2: "##", graphBlockTypeToggleHeading3: "###",
	graphBlockTypeToggleHeading4: "####",
}

// notionMarkdownTableRowCells writes a table row's cells, each line break in
// them as lineBreak — "v1"'s <br>, "v2"'s <br /> — or reports false for a
// cell Markdown can't write.
func notionMarkdownTableRowCells(row workspaceExportMarkdownBlock, mentions notionMarkdownMentions, lineBreak string) ([]string, bool) {
	if row.Type != graphBlockTypeTableRow {
		return nil, false
	}
	encoded, err := json.Marshal(row.Content)
	content := graphTableRowBlockContent{}
	if err != nil || json.Unmarshal(encoded, &content) != nil {
		return nil, false
	}
	cells := make([]string, 0, len(content.Cells))
	for _, spans := range content.Cells {
		text, written := notionMarkdownText(spans, mentions.at(row.ID))
		if !written {
			return nil, false
		}
		// Its code's line break too is the break (seen 2026-11-14), and a
		// "<br />" its text holds stays as written.
		cells = append(cells, strings.ReplaceAll(strings.ReplaceAll(text, notionMarkdownCodeBreak, "\n"), "\n", lineBreak))
	}
	return cells, true
}

// notionMarkdownStructure reads the block a line of Notion's enhanced
// Markdown starts with a tag of its own: a toggle with its title line, an
// equation to its closing $$, a whole table, a column list or a column. It
// returns the blocks, the lines used (none when the line starts no such
// block; a closing tag is used with no block) and false for a table wider
// than a row holds. Their text reads as its form's, "v1"'s when version1 is
// set (seen 2026-11-10: a table cell's emphasis and equations too), the
// addresses of sitePages' pages standing bare in it as their mentions.
func notionMarkdownStructure(
	lines []string, start int, indentOf func(string) int, version1 bool, sitePages *notionMarkdownSitePages,
) ([]workspaceImportParsedBlock, int, bool) {
	trimmed, indent := strings.TrimSpace(lines[start]), indentOf(lines[start])
	inline := func(text string) (string, []graphTextSpan) {
		return notionMarkdownSiteInlineSpans(notionMarkdownLineBreaks(text, version1), version1, sitePages)
	}
	switch {
	case trimmed == "</details>", trimmed == "</column>", trimmed == "</columns>", trimmed == "</tabs>":
		return nil, 1, true
	case trimmed == "<tabs>":
		// Its tabs are the paragraphs one level in, each tab's blocks under its own.
		return []workspaceImportParsedBlock{{Type: graphBlockTypeTab, Content: json.RawMessage(`{}`), Indent: indent}}, 1, true
	case notionMarkdownTabNamePattern.MatchString(trimmed):
		// A tab with its icon, put so by the tabs' reading.
		match := notionMarkdownTabNamePattern.FindStringSubmatch(trimmed)
		name := match[2]
		if strings.EqualFold(name, "<empty-block/>") {
			name = ""
		}
		text, spans := inline(name)
		content := json.RawMessage(`{}`)
		if icon := notionMarkdownCalloutIconContent(html.UnescapeString(match[1])); icon != nil {
			content, _ = json.Marshal(icon)
		}
		return []workspaceImportParsedBlock{{Type: graphBlockTypeParagraph, Text: text, Spans: spans, Content: content, Indent: indent}}, 1, true
	case trimmed == "<columns>":
		return []workspaceImportParsedBlock{{Type: graphBlockTypeColumnList, Content: json.RawMessage(`{}`), Indent: indent}}, 1, true
	case notionMarkdownColumnPattern.MatchString(trimmed):
		// Its share of the row, a percentage, as Notion's ratio.
		content := json.RawMessage(`{}`)
		if ratio, err := strconv.ParseFloat(notionMarkdownColumnPattern.FindStringSubmatch(trimmed)[1], 64); err == nil && ratio > 0 && ratio < 100 {
			content, _ = json.Marshal(map[string]float64{"width": ratio})
		}
		return []workspaceImportParsedBlock{{Type: graphBlockTypeColumn, Content: content, Indent: indent}}, 1, true
	case notionMarkdownDetailsPattern.MatchString(trimmed):
		title, used := "", 1
		if start+1 < len(lines) {
			if match := notionMarkdownSummaryPattern.FindStringSubmatch(strings.TrimSpace(lines[start+1])); match != nil {
				// Its edge spaces go, as Notion's (seen 2026-11-10).
				title, used = strings.TrimSpace(match[1]), 2
			}
		}
		text, spans := inline(title)
		_, color := takeNotionMarkdownBlockColor(`x {color="` + notionMarkdownDetailsPattern.FindStringSubmatch(trimmed)[1] + `"}`)
		return []workspaceImportParsedBlock{{Type: graphBlockTypeToggle, Text: text, Spans: spans,
			Content: withNotionMarkdownBlockColor(json.RawMessage(`{}`), color), Indent: indent}}, used, true
	case trimmed == "$$" || strings.HasPrefix(trimmed, "$$ {color="):
		// A colored one carries its color on its opening line.
		_, color := takeNotionMarkdownBlockColor(trimmed)
		for end := start + 1; end < len(lines); end++ {
			if strings.TrimSpace(lines[end]) != "$$" {
				continue
			}
			expression := make([]string, 0, end-start-1)
			for _, line := range lines[start+1 : end] {
				expression = append(expression, notionMarkdownDedent(line, indent))
			}
			content, _ := json.Marshal(map[string]string{"expression": strings.Join(expression, "\n")})
			return []workspaceImportParsedBlock{{Type: graphBlockTypeEquation, Content: withNotionMarkdownBlockColor(content, color),
				Indent: indent}}, end - start + 1, true
		}
	case notionMarkdownTableOpenPattern.MatchString(trimmed):
		return notionMarkdownTable(lines, start, indent, version1, func(text string) (string, []graphTextSpan) {
			return notionMarkdownCellBullets(notionMarkdownCellInlineSpans(notionMarkdownLineBreaks(text, version1), version1, sitePages))
		})
	}
	// A toggle heading's color ends its line after {toggle="true"}.
	uncolored, color := takeNotionMarkdownBlockColor(trimmed)
	if match := notionMarkdownToggleHeadingPattern.FindStringSubmatch(uncolored); match != nil {
		kind := notionMarkdownToggleHeadingKind(len(match[1]))
		text, spans := inline(match[2])
		return []workspaceImportParsedBlock{{Type: kind, Text: text, Spans: spans,
			Content: withNotionMarkdownBlockColor(json.RawMessage(`{}`), color), Indent: indent}}, 1, true
	}
	return nil, 0, true
}

func notionMarkdownTable(
	lines []string, start, indent int, version1 bool, inline func(string) (string, []graphTextSpan),
) ([]workspaceImportParsedBlock, int, bool) {
	attributes := notionMarkdownTableOpenPattern.FindStringSubmatch(strings.TrimSpace(lines[start]))[1]
	rows := [][]string{}
	var row []string
	rowColor, rowColors, cellColors, cellColor := "", []string{}, [][]string{}, []string{}
	for end := start + 1; end < len(lines); end++ {
		line := strings.TrimSpace(lines[end])
		switch match := notionMarkdownTableCellPattern.FindStringSubmatch(line); {
		case notionMarkdownTableRowPattern.MatchString(line):
			row, rowColor, cellColor = []string{}, notionMarkdownColorValue(notionMarkdownTableRowPattern.FindStringSubmatch(line)[1]), []string{}
		case line == "</tr>" && row != nil:
			rows, row = append(rows, row), nil
			rowColors, cellColors = append(rowColors, rowColor), append(cellColors, cellColor)
		case match != nil && row != nil:
			row = append(row, match[2])
			cellColor = append(cellColor, notionMarkdownColorValue(match[1]))
		case line == "</table>":
			// It is as wide as its widest row, and in "v2" as its <colgroup>
			// too; one without rows has no width, nor its rows without cells
			// (content_graph_table.go), as Notion's reads them (seen
			// 2026-11-14: "v2" <col /><col /> over a row of one cell is two
			// wide, over none none).
			width := 0
			for _, cells := range rows {
				width = max(width, len(cells))
			}
			if columns, _ := strconv.Atoi(notionMarkdownAttributes(attributes)["columns"]); !version1 && len(rows) > 0 {
				width = max(width, columns)
			}
			if width > maxGraphTableWidth {
				return nil, end - start + 1, false
			}
			columnColors := []string{}
			if listed := notionMarkdownAttributes(attributes)["column-colors"]; listed != "" {
				for _, name := range strings.Split(listed, ",") {
					columnColors = append(columnColors, notionMarkdownColorValue(name))
				}
			}
			tableContent, _ := json.Marshal(graphTableBlockContent{
				TableWidth:      width,
				HasColumnHeader: strings.Contains(attributes, `header-row="true"`),
				HasRowHeader:    strings.Contains(attributes, `header-column="true"`),
				FitPageWidth:    strings.Contains(attributes, `fit-page-width="true"`),
				ColumnWidths:    notionMarkdownColumnWidths(notionMarkdownAttributes(attributes)["column-widths"], width),
				ColumnColors:    graphTableColorsKept(columnColors, width),
			})
			blocks := []workspaceImportParsedBlock{{Type: graphBlockTypeTable, Content: tableContent, Indent: indent}}
			for at, cells := range rows {
				spans := make([][]graphTextSpan, width)
				for column := range spans {
					spans[column] = []graphTextSpan{}
					if column < len(cells) && cells[column] == notionMarkdownEmptyCellText {
						spans[column] = []graphTextSpan{{}}
					} else if column < len(cells) && cells[column] != "" {
						text, formatted := inline(cells[column])
						if formatted == nil {
							formatted = []graphTextSpan{{Text: text}}
						}
						spans[column] = formatted
					}
				}
				rowContent, _ := json.Marshal(graphTableRowBlockContent{
					Cells: spans, Color: rowColors[at], CellColors: graphTableColorsKept(cellColors[at], width),
				})
				blocks = append(blocks, workspaceImportParsedBlock{Type: graphBlockTypeTableRow, Content: rowContent, Indent: indent + 1})
			}
			return blocks, end - start + 1, true
		case line == "" || notionMarkdownTableGroup.MatchString(line) || strings.HasPrefix(line, "<col"):
			// Its <thead>, <tbody>, <colgroup> and blank lines read as a
			// table's at the top do, in a block that holds it too (seen
			// 2026-11-14: a "v1" callout's "<table>⏎<thead>⏎<tr>…" is the table).
			if canonical, used := notionMarkdownTableRows(lines, start, 0); used > 0 {
				blocks, _, fits := notionMarkdownTable(canonical, 0, indent, version1, inline)
				return blocks, used, fits
			}
			return nil, 0, true
		default:
			// Not a table Notion writes: its lines read as they are.
			return nil, 0, true
		}
	}
	return nil, 0, true
}

// notionMarkdownColumnWidths reads a table's column widths as content keeps
// them: within what a column may be, none past the last, as many as the table
// is wide.
func notionMarkdownColumnWidths(listed string, width int) []float64 {
	widths := []float64{}
	for _, written := range strings.Split(listed, ",") {
		value, err := strconv.ParseFloat(strings.TrimSpace(written), 64)
		if err != nil || value <= 0 {
			value = 0
		} else {
			value = min(max(value, minGraphTableColumnWidth), maxGraphTableColumnWidth)
		}
		widths = append(widths, value)
	}
	end := min(len(widths), width)
	for end > 0 && widths[end-1] == 0 {
		end--
	}
	if end == 0 {
		return nil
	}
	return widths[:end]
}

// notionMarkdownDedent takes up to depth levels of indentation off a line.
func notionMarkdownDedent(line string, depth int) string {
	for range depth {
		switch {
		case strings.HasPrefix(line, "\t"):
			line = line[1:]
		case strings.HasPrefix(line, "  "):
			line = line[2:]
		}
	}
	return line
}
