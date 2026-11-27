package app

import (
	"encoding/csv"
	"html"
	"strconv"
	"strings"
)

// Markdown & CSV pages follow Notion's export: Markdown blocks, callouts as
// HTML asides since Markdown has none, and each database as a CSV file the
// page links to.

func (renderer *workspaceExportRenderer) runsMarkdown(runs []workspaceExportTextRun) string {
	return strings.ReplaceAll(workspaceExportRunsMarkdown(renderer.document.namedRuns(runs, renderer.linkUntitled()), renderer.link, renderer.page), "\n", "  \n")
}

func workspaceExportIndent(text, indent string) string {
	if indent == "" || text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if line != "" {
			lines[index] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

func workspaceExportMarkdownListItem(blockType string) bool {
	return blockType == graphBlockTypeBulletedListItem || blockType == graphBlockTypeNumberedListItem ||
		blockType == graphBlockTypeToDo || blockType == graphBlockTypeToggle
}

// blocksMarkdown separates blocks by a blank line and keeps consecutive list
// items together as a tight list, of one kind or not, as Notion's export
// does (seen 2026-11-07: a to-do, a bullet and a numbered item one per line).
func (renderer *workspaceExportRenderer) blocksMarkdown(blocks []*workspaceExportDocBlock, indent string) string {
	var builder strings.Builder
	number := 0
	previous := ""
	for _, block := range blocks {
		if block.Type == graphBlockTypeNumberedListItem {
			number++
		} else {
			number = 0
		}
		text := renderer.blockMarkdown(block, number)
		if text == "" {
			continue
		}
		if builder.Len() > 0 {
			if workspaceExportMarkdownListItem(previous) && workspaceExportMarkdownListItem(block.Type) {
				builder.WriteString("\n")
			} else {
				builder.WriteString("\n\n")
			}
		}
		builder.WriteString(workspaceExportIndent(text, indent))
		previous = block.Type
	}
	return builder.String()
}

func (renderer *workspaceExportRenderer) blockMarkdown(block *workspaceExportDocBlock, number int) string {
	text := renderer.runsMarkdown(block.Runs)
	nested := func(indent string) string {
		children := renderer.blocksMarkdown(renderer.children[block.ID], indent)
		if children == "" {
			return ""
		}
		if workspaceExportMarkdownListItem(block.Type) {
			return "\n" + children
		}
		return "\n\n" + children
	}
	switch block.Type {
	case graphBlockTypeParagraph:
		return text + nested("    ")
	case graphBlockTypeHeading1, graphBlockTypeToggleHeading1:
		return "# " + text + nested("")
	case graphBlockTypeHeading2, graphBlockTypeToggleHeading2:
		return "## " + text + nested("")
	case graphBlockTypeHeading3, graphBlockTypeToggleHeading3:
		return "### " + text + nested("")
	case graphBlockTypeHeading4, graphBlockTypeToggleHeading4:
		return "#### " + text + nested("")
	case graphBlockTypeBulletedListItem:
		return "- " + text + nested("    ")
	case graphBlockTypeToggle:
		return "- " + text + renderer.toggleChildrenMarkdown(block)
	case graphBlockTypeNumberedListItem:
		return strconv.Itoa(max(number, 1)) + ". " + text + nested("    ")
	case graphBlockTypeToDo:
		mark := " "
		if checked, _ := block.Content["checked"].(bool); checked {
			mark = "x"
		}
		// Notion writes two spaces after the box (seen 2026-11-07).
		return "- [" + mark + "]  " + text + nested("    ")
	case graphBlockTypeQuote:
		// and ends a quote with an empty quoted line.
		return workspaceExportIndent(text+nested(""), "> ") + "\n> "
	case graphBlockTypeCallout:
		icon := mapString(block.Content, "icon")
		if icon == "" || strings.Contains(icon, "/") {
			icon = "💡"
		}
		if mapString(block.Content, "iconType") == calloutIconNone {
			return "<aside>\n" + text + nested("") + "\n\n</aside>"
		}
		return "<aside>\n" + html.EscapeString(icon) + " " + text + nested("") + "\n\n</aside>"
	case graphBlockTypeDivider:
		return "---"
	case graphBlockTypeCode:
		code := workspaceExportPlainText(block.Runs)
		fence := "```"
		for strings.Contains(code, fence) {
			fence += "`"
		}
		return fence + mapString(block.Content, "language") + "\n" + code + "\n" + fence
	case graphBlockTypeEquation:
		return "$$\n" + mapString(block.Content, "expression") + "\n$$"
	case graphBlockTypeTable:
		return renderer.tableMarkdown(block)
	case graphBlockTypeImage, graphBlockTypeVideo, graphBlockTypeAudio, graphBlockTypePDF, graphBlockTypeFile,
		graphBlockTypeEmbed, graphBlockTypeBookmark, graphBlockTypeLinkPreview:
		return renderer.mediaMarkdown(block)
	case graphBlockTypeChildPage, graphBlockTypeLinkToPage:
		target := renderer.pageLinkTarget(block)
		if target == "" && mapString(block.Content, "childPageTitle") == "" ||
			renderer.subPageLeftBehind(block, target) || renderer.linkedBefore(target) {
			return ""
		}
		title := renderer.linkTitle(block, target)
		if href := renderer.pageLinkHref(block, target); href != "" {
			return "[" + title + "](" + href + ")"
		}
		return title
	case graphBlockTypeInlineDatabase, graphBlockTypeChildDatabase:
		container := renderer.document.ByBlock[block.ID]
		if container == nil {
			return ""
		}
		if container.CSVPath != "" {
			return "[" + workspaceExportTitle(renderer.document.linkedViewTitle(container), renderer.untitled) + "](" +
				workspaceExportRelativeHref(renderer.from, container.CSVPath) + ")"
		}
		links := []string{}
		for _, dataSourceID := range container.DataSourceIDs {
			if source := renderer.document.DataSources[dataSourceID]; source != nil {
				title := workspaceExportTitle(source.Title, renderer.untitled)
				links = append(links, "["+title+"]("+workspaceExportRelativeHref(renderer.from, source.CSVPath)+")")
			}
		}
		return strings.Join(links, "\n\n")
	case graphBlockTypeColumnList, graphBlockTypeColumn, graphBlockTypeTab, graphBlockTypeTemplate:
		return strings.TrimPrefix(text+nested(""), "\n\n")
	case graphBlockTypeSyncedBlock:
		source := renderer.document.BlockByID[mapString(block.Content, "syncedBlockId")]
		if source == nil {
			return strings.TrimPrefix(nested(""), "\n\n")
		}
		saved := renderer.children
		renderer.children = map[string][]*workspaceExportDocBlock{}
		for _, candidate := range renderer.document.Blocks[source.PageID] {
			renderer.children[candidate.ParentID] = append(renderer.children[candidate.ParentID], candidate)
		}
		rendered := renderer.blocksMarkdown(renderer.children[source.ID], "")
		renderer.children = saved
		return rendered
	case graphBlockTypeButton, graphBlockTypeTableOfContents, graphBlockTypeBreadcrumb:
		return ""
	default:
		return text + nested("    ")
	}
}

// linkedBefore reports whether the Markdown file being written already has a
// line linking to a page, and notes it otherwise: Notion's Markdown export
// writes one page link line per page, its sub-page's or a page link's
// whichever comes first in the page, inside a toggle too, and leaves out
// those after (seen 2026-11-13: a page link after the sub-page it names, a
// sub-page after a page link to it and a second link to one page outside the
// export all gone). A mention is not a line and does not count. Its HTML
// keeps every one.
func (renderer *workspaceExportRenderer) linkedBefore(target string) bool {
	if target == "" || renderer.linked == nil {
		return false
	}
	if renderer.linked[target] {
		return true
	}
	renderer.linked[target] = true
	return false
}

// toggleChildrenMarkdown is a toggle's children as Notion's export writes
// them (seen 2026-11-11): a blank line after the toggle's own line, the
// children, and a blank line after them, every line of it indented under the
// toggle, blank ones too. A toggle with no children is its line alone.
func (renderer *workspaceExportRenderer) toggleChildrenMarkdown(block *workspaceExportDocBlock) string {
	children := renderer.blocksMarkdown(renderer.children[block.ID], "")
	if children == "" {
		return ""
	}
	lines := strings.Split("\n"+children+"\n", "\n")
	for index, line := range lines {
		lines[index] = "    " + line
	}
	return "\n" + strings.Join(lines, "\n")
}

func (renderer *workspaceExportRenderer) mediaMarkdown(block *workspaceExportDocBlock) string {
	href := workspaceExportSafeLink(mapString(block.Content, "url"))
	if fileID := mapString(block.Content, "fileId"); fileID != "" {
		href = renderer.fileHref(fileID, block.Type)
	}
	name := renderer.mediaName(block)
	caption := workspaceExportPlainText(workspaceExportRunsFromSpans(block.Content["caption"], renderer.document.Language))
	if href == "" {
		return name
	}
	href = workspaceExportMarkdownHref(href)
	if block.Type == graphBlockTypeImage {
		return "![" + workspaceExportTitle(caption, name) + "](" + href + ")"
	}
	if name == "" {
		name = href
	}
	return "[" + name + "](" + href + ")"
}

func (renderer *workspaceExportRenderer) tableMarkdown(block *workspaceExportDocBlock) string {
	lines := []string{}
	for _, row := range renderer.tableRows(block) {
		cells, _ := row.Content["cells"].([]any)
		texts := make([]string, 0, len(cells))
		for _, cell := range cells {
			text := renderer.runsMarkdown(workspaceExportRunsFromSpans(cell, renderer.document.Language))
			texts = append(texts, strings.ReplaceAll(strings.ReplaceAll(text, "|", `\|`), "  \n", "<br>"))
		}
		lines = append(lines, "| "+strings.Join(texts, " | ")+" |")
		if len(lines) == 1 {
			separators := make([]string, len(texts))
			for index := range separators {
				separators[index] = "---"
			}
			lines = append(lines, "| "+strings.Join(separators, " | ")+" |")
		}
	}
	return strings.Join(lines, "\n")
}

// pageMarkdown is a page's file: its title and its body. A database page
// exported as a page, by a page's own Markdown, names its properties under
// its title as its file in its database's folder does (rowMarkdown).
func (renderer *workspaceExportRenderer) pageMarkdown(page *workspaceExportDocPage) string {
	document := renderer.document
	renderer.page = page.ID
	renderer.useBlocks(document.Blocks[page.ID])
	body := []*workspaceExportDocBlock{}
	for _, item := range document.bodyItems(page) {
		if item.Block != nil {
			body = append(body, item.Block)
			continue
		}
		body = append(body, &workspaceExportDocBlock{
			ID: "page-link-" + item.Page.ID, PageID: page.ID, Type: graphBlockTypeLinkToPage,
			Content: map[string]any{"childPageId": item.Page.ID},
		})
	}
	if row := document.RowByID[page.ID]; row != nil {
		return renderer.markdownFile(page.Icon, page.IconType, row.Title, renderer.rowPropertyLines(row), body)
	}
	return renderer.markdownFile(page.Icon, page.IconType, page.Title, nil, body)
}

func (renderer *workspaceExportRenderer) rowMarkdown(row *workspaceExportDocRow) string {
	document := renderer.document
	renderer.page = row.ID
	renderer.useBlocks(document.RowBlocks[row.ID])
	body := append(append([]*workspaceExportDocBlock(nil), renderer.children[""]...), document.rowSubPageLinks(row)...)
	return renderer.markdownFile(row.Icon, row.IconType, row.Title, renderer.rowPropertyLines(row), body)
}

// markdownFile is a page's Markdown file: its icon and title as a heading,
// the lines naming its properties, then its blocks, a blank line apart.
// Notion's files end without a line end (seen 2026-11-07). Its heading is
// its title without the spaces at its ends, the untitled name for none (seen
// 2026-11-13: " 앞뒤 " heads "# 앞뒤", a title of spaces "# ").
func (renderer *workspaceExportRenderer) markdownFile(icon, iconType, title string, properties []string, body []*workspaceExportDocBlock) string {
	renderer.linked = map[string]bool{}
	heading := renderer.untitled
	if title != "" {
		heading = strings.TrimSpace(title)
	}
	parts := []string{"# " + workspaceExportIconText(icon, iconType) + heading}
	if len(properties) > 0 {
		parts = append(parts, strings.Join(properties, "\n"))
	}
	if text := renderer.blocksMarkdown(body, ""); text != "" {
		parts = append(parts, text)
	}
	return strings.TrimRight(strings.Join(parts, "\n\n"), "\n")
}

// rowPropertyLines name a database page's properties and values under its
// title, the title aside, and those with a value only (rowPageListsProperty).
func (renderer *workspaceExportRenderer) rowPropertyLines(row *workspaceExportDocRow) []string {
	lines := []string{}
	for _, property := range renderer.document.orderedProperties(row.DataSourceID) {
		cell := renderer.document.cell(property, row)
		if property.Type != graphValueTypeTitle && renderer.rowPageListsProperty(property, cell) {
			lines = append(lines, property.Name+": "+renderer.cellMarkdown(cell))
		}
	}
	return lines
}

// rowPageListsProperty reports whether a database page's file names a
// property under its title: one with a value, as Notion's export leaves an
// empty one out, never set or cleared alike (seen 2026-11-13: an empty text,
// number, select, multi-select, date, person, file, URL, email, phone,
// relation, rollup of pages and formula of nothing left out; a checkbox's
// and a formula's "No", a count's and a sum's "0" and a status's default
// written). Notion's HTML still lists a multi-select of none, its cell empty
// (seen 2026-11-13), as a printable export does here; its Markdown does not.
func (renderer *workspaceExportRenderer) rowPageListsProperty(property *workspaceExportDocProperty, cell workspaceExportCell) bool {
	if len(cell.Parts) > 0 || cell.Plain != "" {
		return true
	}
	return renderer.mode != workspaceExportModeMarkdown && property.Type == graphValueTypeMultiSelect
}

// databaseCSV writes a database as Notion does: a header of the property
// names its view shows and one line per row the view shows, values as the
// database shows them. The "_all" file beside it (Markdown & CSV only) has
// every property, those the view hides too, and every row (seen 2026-11-07).
// Lines end in CRLF and the last has no line end, as Notion's do. Related
// pages are named from the export's top, wherever the CSV stands, as
// Notion's CSV and "_all" CSV name them (seen 2026-11-13: "빈 행
// (w766%20…/%ED%96%89%ED%91%9C/%EB%B9%88%20%ED%96%89%20〈id〉.md)" in a CSV
// inside "w766 내보내기/", ".html" in an HTML export's).
func (document *workspaceExportDocument) databaseCSV(dataSourceID string, all bool) (string, error) {
	return document.databaseCSVPlacing(dataSourceID, all, nil)
}

// databaseCSVPlacing is databaseCSV whose uploaded files files places, each
// named from the export's top (relatedText); nil names them by name.
func (document *workspaceExportDocument) databaseCSVPlacing(
	dataSourceID string, all bool, files *workspaceExportFilePlacer,
) (string, error) {
	return document.databaseViewCSV(dataSourceID, document.sourceView(dataSourceID), all, files)
}

// databaseViewCSV is a database's CSV as one of its views shows it, or all
// its rows and properties in that view's order.
func (document *workspaceExportDocument) databaseViewCSV(
	dataSourceID string, view *workspaceExportDocView, all bool, files *workspaceExportFilePlacer,
) (string, error) {
	renderer := &workspaceExportRenderer{document: document, mode: workspaceExportModeMarkdown, files: files}
	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	writer.UseCRLF = true
	table := document.viewTable(dataSourceID, view)
	properties, rows := table.Properties, table.Rows
	if all {
		properties = workspaceExportViewOrder(document.Properties[dataSourceID], view)
		rows = []*workspaceExportDocRow{}
		for _, row := range document.Rows[dataSourceID] {
			if !row.Archived {
				rows = append(rows, row)
			}
		}
	}
	header := make([]string, len(properties))
	for index, property := range properties {
		header[index] = property.Name
	}
	if err := writer.Write(header); err != nil {
		return "", err
	}
	for _, row := range rows {
		record := make([]string, len(properties))
		for index, property := range properties {
			record[index] = workspaceExportCSVValue(renderer.cellText(document.cell(property, row)))
		}
		if err := writer.Write(record); err != nil {
			return "", err
		}
	}
	writer.Flush()
	return "\ufeff" + strings.TrimSuffix(builder.String(), "\r\n"), writer.Error()
}

// workspaceExportAllCSVPath is the path of a database's "_all" file.
func workspaceExportAllCSVPath(csvPath string) string {
	return strings.TrimSuffix(csvPath, ".csv") + "_all.csv"
}

// workspaceExportSourceAllCSVPath is where a source's "_all" file stands:
// beside its CSV, or where a linked view's source puts it.
func workspaceExportSourceAllCSVPath(source *workspaceExportDocDataSource) string {
	if source.AllCSVPath != "" {
		return source.AllCSVPath
	}
	return workspaceExportAllCSVPath(source.CSVPath)
}

// workspaceExportDatabaseHTMLPath is the path of a database's table page in
// an HTML export, beside its CSV.
func workspaceExportDatabaseHTMLPath(csvPath string) string {
	return strings.TrimSuffix(csvPath, ".csv") + ".html"
}
