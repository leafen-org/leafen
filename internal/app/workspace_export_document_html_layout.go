package app

import (
	"crypto/sha256"
	"math"
	"strconv"
	"strings"
)

// The blocks an HTML export lays other blocks out in — a callout, columns, a
// simple table, a table of contents, a synced block, a breadcrumb — as
// Notion's HTML export writes them.

// calloutHTML writes a callout as Notion's HTML export does (seen
// 2026-11-14): an aside marked a callout, in its color's class and named by
// data-notion-callout-background, its icon's box, then its words and blocks
// in a box of its width. Its icon is named in data-notion-callout-icon — an
// emoji as it is, drawn from the attribute, a built-in icon by its picture's
// path in Notion's icon set, drawn inside the file — and a picture, linked or
// uploaded, is its image alone. A callout with no icon has no icon box. Its words are its first
// line's, a paragraph in Leafen, as Notion's callout holds its own.
func (renderer *workspaceExportRenderer) calloutHTML(block *workspaceExportDocBlock) string {
	color := workspaceExportBlockColor(block)
	class, background := "callout", ""
	if color != "" {
		class, background = "block-color-"+color+" callout", ` data-notion-callout-background="`+color+`"`
	}
	named, iconHTML := renderer.calloutIcon(block)
	if named != "" {
		named = ` data-notion-callout-icon="` + workspaceExportEscape(named) + `"`
	}
	if iconHTML != "" {
		iconHTML = `<div style="font-size:1.5em">` + iconHTML + "</div>"
	}
	text, children := renderer.runsHTML(block.Runs), renderer.children[block.ID]
	if len(block.Runs) == 0 && len(children) > 0 && children[0].Type == graphBlockTypeParagraph &&
		workspaceExportBlockColor(children[0]) == "" && len(renderer.children[children[0].ID]) == 0 {
		text, children = renderer.runsHTML(children[0].Runs), children[1:]
	}
	return `<aside class="` + class + `" data-notion-callout=""` + named + background +
		` style="white-space:pre-wrap;display:flex" id="` + workspaceExportEscape(block.ID) + `" dir="ltr">` + iconHTML +
		`<div style="width:100%">` + text + renderer.blocksHTML(children) + "</div></aside>"
}

// calloutIcon is the name a callout's icon is given in its attribute and
// what its box draws (workspaceExportIcon): an emoji (the bulb for none
// set) and a built-in icon named, a linked picture or an uploaded one placed
// with the page's files not.
func (renderer *workspaceExportRenderer) calloutIcon(block *workspaceExportDocBlock) (string, string) {
	icon := strings.TrimSpace(mapString(block.Content, "icon"))
	switch mapString(block.Content, "iconType") {
	case calloutIconNone:
		return "", ""
	case nativeIconType:
		drawn := workspaceExportNativeIcon(icon)
		return drawn.Name, drawn.HTML()
	case notionIconTypeExternal:
		if link := workspaceExportSafeLink(icon); strings.HasPrefix(link, "http") {
			return "", workspaceExportPictureIcon(link).HTML()
		}
		return "", ""
	case graphCalloutIconTypeImage:
		if fileID := graphCalloutIconFileIDOf(block.Type, block.Content); fileID != "" {
			return "", workspaceExportPictureIcon(renderer.fileHref(fileID, block.Type)).HTML()
		}
		return "", ""
	}
	if icon == "" || strings.Contains(icon, "/") {
		icon = "💡"
	}
	drawn := workspaceExportEmojiIcon(icon)
	return drawn.Name, drawn.HTML()
}

// workspaceExportToggleHeadingSizes are a toggle heading's sizes, as Notion's
// export draws its heading in the toggle's summary (seen 2026-11-14: 1.875em,
// 1.5em and 1.25em; a fourth level the text's).
var workspaceExportToggleHeadingSizes = map[string]string{"1": "1.875em", "2": "1.5em", "3": "1.25em", "4": "1em"}

// columnListHTML writes columns side by side, as Notion's export does (seen
// 2026-11-14, 2026-11-15): each column named by its block and its share of
// the row, the part of the row its width keeps in percent (the editor's,
// columnLayoutModel.ts) — "0.3" and "width:30%" for 30 — or 1 and
// "width:100%" each for columns sharing it alike, which Notion's draws in
// the row's flex box.
func (renderer *workspaceExportRenderer) columnListHTML(block *workspaceExportDocBlock) string {
	columns := renderer.children[block.ID]
	// Each column's weight is its width ÷ 100, or 1 without one, and its
	// share of the row its part of their sum, as the editor lays them out.
	widths := make([]float64, len(columns))
	total := 0.0
	for index, column := range columns {
		widths[index] = 1
		if width, ok := column.Content["width"].(float64); ok && width > 0 && width <= 100 {
			widths[index] = width / 100
		}
		total += widths[index]
	}
	even := true
	for index := range widths {
		widths[index] /= total
		even = even && math.Abs(widths[index]*float64(len(columns))-1) < 0.0005
	}
	var builder strings.Builder
	builder.WriteString(`<div ` + strings.Replace(workspaceExportBlockAttrs(block.ID, "column-list", ""), ` dir=`, ` data-notion-column-list="" dir=`, 1) + `>`)
	for index, column := range columns {
		ratio, style := "1", "width:100%"
		if !even {
			ratio = strconv.FormatFloat(math.Round(widths[index]*10000)/10000, 'f', -1, 64)
			style = "width:" + strconv.FormatFloat(math.Round(widths[index]*10000)/100, 'f', -1, 64) + "%"
		}
		builder.WriteString(`<div id="` + workspaceExportEscape(column.ID) + `" style="` + style + `" class="column" data-notion-column-ratio="` +
			ratio + `" dir="auto">` + renderer.blocksHTML(renderer.children[column.ID]) + "</div>")
	}
	builder.WriteString("</div>")
	return builder.String()
}

func (renderer *workspaceExportRenderer) syncedHTML(block *workspaceExportDocBlock, children string) string {
	sourceID := mapString(block.Content, "syncedBlockId")
	source := renderer.document.BlockByID[sourceID]
	if sourceID == "" || source == nil {
		return children
	}
	sourceChildren := []*workspaceExportDocBlock{}
	for _, candidate := range renderer.document.Blocks[source.PageID] {
		if candidate.ParentID == source.ID {
			sourceChildren = append(sourceChildren, candidate)
		}
	}
	saved, savedBlocks := renderer.children, renderer.blocks
	renderer.children, renderer.blocks = map[string][]*workspaceExportDocBlock{}, map[string]*workspaceExportDocBlock{}
	for _, candidate := range renderer.document.Blocks[source.PageID] {
		renderer.children[candidate.ParentID] = append(renderer.children[candidate.ParentID], candidate)
		renderer.blocks[candidate.ID] = candidate
	}
	rendered := renderer.blocksHTML(sourceChildren)
	renderer.children, renderer.blocks = saved, savedBlocks
	return rendered
}

// tableHTML writes a simple table as Notion's export does (seen 2026-11-14):
// a header row in the table's head, each row named by its block, each cell
// by its column (workspaceExportTableColumnID), a header cell in the
// header's colors and any other cell of no class.
func (renderer *workspaceExportRenderer) tableHTML(block *workspaceExportDocBlock) string {
	columnHeader, _ := block.Content["hasColumnHeader"].(bool)
	rowHeader, _ := block.Content["hasRowHeader"].(bool)
	rows := renderer.tableRows(block)
	if len(rows) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(`<table id="` + workspaceExportEscape(block.ID) + `" class="simple-table" dir="ltr">`)
	for rowIndex, row := range rows {
		switch {
		case rowIndex == 0 && columnHeader:
			builder.WriteString(`<thead class="simple-table-header">`)
		case rowIndex == 0 || rowIndex == 1 && columnHeader:
			builder.WriteString("<tbody>")
		}
		cells, _ := row.Content["cells"].([]any)
		builder.WriteString(`<tr id="` + workspaceExportEscape(row.ID) + `" dir="ltr">`)
		for cellIndex, cell := range cells {
			column := workspaceExportTableColumnID(block.ID, cellIndex)
			open, closing := `<td id="`+column+`" class="">`, "</td>"
			if columnHeader && rowIndex == 0 || rowHeader && cellIndex == 0 {
				open, closing = `<th id="`+column+`" class="simple-table-header-color simple-table-header">`, "</th>"
			}
			builder.WriteString(open + renderer.runsHTML(workspaceExportRunsFromSpans(cell, renderer.document.Language)) + closing)
		}
		builder.WriteString("</tr>")
		if rowIndex == 0 && columnHeader {
			builder.WriteString("</thead>")
		}
	}
	if len(rows) > 1 || len(rows) == 1 && !columnHeader {
		builder.WriteString("</tbody>")
	}
	builder.WriteString("</table>")
	return builder.String()
}

// tableRows are a simple table's rows as an export writes them: none for one
// without rows or without width, which Notion's exports leave out, Markdown
// and HTML (seen 2026-11-14).
func (renderer *workspaceExportRenderer) tableRows(block *workspaceExportDocBlock) []*workspaceExportDocBlock {
	if width, _ := notionIntegerContentValue(block.Content["tableWidth"]); width < minGraphTableWidth {
		return nil
	}
	rows := []*workspaceExportDocBlock{}
	for _, row := range renderer.children[block.ID] {
		if row.Type == graphBlockTypeTableRow {
			rows = append(rows, row)
		}
	}
	return rows
}

// workspaceExportTableColumnID names a simple table's column, which Notion's
// export names each of its cells by (seen 2026-11-14: <td id="fFzg" …>, the
// same down a column). Leafen's table keeps no column IDs, so an export makes
// one from the table's block and the column's place, the same each time.
func workspaceExportTableColumnID(tableID string, column int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	sum := sha256.Sum256([]byte(tableID + "/" + strconv.Itoa(column)))
	id := make([]byte, 4)
	for index := range id {
		id[index] = letters[int(sum[index])%len(letters)]
	}
	return string(id)
}

// tableOfContentsHTML writes a table of contents as Notion's export does
// (seen 2026-11-14): a nav in its color, gray when it has none, as Notion
// draws one, its headings' links under it, each indented by how many
// headings above it are of a higher level (a third level under a first is
// one in).
func (renderer *workspaceExportRenderer) tableOfContentsHTML(id, color string) string {
	var builder strings.Builder
	shade := strings.TrimPrefix(workspaceExportColorClass(color), "highlight-")
	if shade == "" {
		shade = "gray"
	}
	builder.WriteString(`<nav id="` + workspaceExportEscape(id) + `" class="block-color-` + shade + ` table_of_contents" data-notion-highlight="` +
		shade + `" data-notion-toc="" dir="ltr">`)
	open := []int{}
	for _, heading := range renderer.headings {
		level, _ := strconv.Atoi(heading.Type[len(heading.Type)-1:])
		for len(open) > 0 && open[len(open)-1] >= level {
			open = open[:len(open)-1]
		}
		indent := len(open)
		open = append(open, level)
		builder.WriteString(`<div class="table_of_contents-item table_of_contents-indent-` + strconv.Itoa(indent) +
			`"><a class="table_of_contents-link" href="#` + workspaceExportEscape(heading.ID) + `">` +
			workspaceExportEscape(workspaceExportPlainText(heading.Runs)) + "</a></div>")
	}
	builder.WriteString("</nav>")
	return builder.String()
}

func (renderer *workspaceExportRenderer) breadcrumbHTML(pageID string) string {
	trail := []string{}
	seen := map[string]bool{}
	for page := renderer.document.Pages[pageID]; page != nil && !seen[page.ID]; page = renderer.document.Pages[page.ParentID] {
		seen[page.ID] = true
		title := workspaceExportEscape(workspaceExportTitle(page.Title, renderer.untitled))
		if href := renderer.link(page.ID); href != "" && page.ID != pageID {
			title = `<a href="` + workspaceExportEscape(href) + `">` + title + "</a>"
		}
		trail = append([]string{title}, trail...)
	}
	return strings.Join(trail, " / ")
}
