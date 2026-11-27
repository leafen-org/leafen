package app

import (
	"strconv"
	"strings"
)

// containerHTML writes a database as a table of the rows its view shows,
// each row linking to its row page when the export includes row pages.
func (renderer *workspaceExportRenderer) containerHTML(container *workspaceExportDocContainer) string {
	var builder strings.Builder
	for _, dataSourceID := range container.DataSourceIDs {
		table := renderer.document.viewTable(dataSourceID, renderer.document.containerView(container, dataSourceID))
		builder.WriteString(renderer.databaseHTML(dataSourceID, `id="`+workspaceExportEscape(container.BlockID)+`" `, table, true))
	}
	return builder.String()
}

// databaseHTML writes a database as Notion's export does (seen 2026-11-07):
// in a page, its name in a small heading over the table, in a box named by
// its block; alone, the table under the page's own title; two line breaks
// after the table (seen 2026-11-14). The columns keep their widths on screen as
// parts of the table's, and each header names its property's kind by icon.
// Each row is named by its ID and each cell by its property's (a title's
// "cell-title"), a row's title linking its page by ID, as Notion's (seen
// 2026-11-14).
func (renderer *workspaceExportRenderer) databaseHTML(dataSourceID, id string, table workspaceExportViewTable, titled bool) string {
	document := renderer.document
	source := document.DataSources[dataSourceID]
	if source == nil {
		return ""
	}
	properties := table.Properties
	var builder strings.Builder
	if titled {
		// An unnamed one's heading is empty, as Notion's (seen 2026-11-13).
		builder.WriteString(`<div ` + id + `class="collection-content" dir="ltr"><h4 class="collection-title">` +
			workspaceExportEscape(workspaceExportIconText(source.Icon, source.IconType)+source.Title) + `</h4>`)
	}
	builder.WriteString(`<div class="collection-content-wrapper"><table class="collection-content">`)
	total := 0.0
	for _, width := range table.Widths {
		total += width
	}
	if total > 0 && len(table.Widths) == len(properties) {
		builder.WriteString("<colgroup>")
		for _, width := range table.Widths {
			builder.WriteString(`<col style="width:` + strconv.FormatFloat(width/total*100, 'f', -1, 64) + `%"/>`)
		}
		builder.WriteString("</colgroup>")
	}
	builder.WriteString("<thead><tr>")
	for _, property := range properties {
		builder.WriteString("<th>" + workspaceExportPropertyIconHTML(property.Type) + workspaceExportEscape(property.Name) + "</th>")
	}
	builder.WriteString("</tr></thead><tbody>")
	for _, row := range table.Rows {
		builder.WriteString(`<tr id="` + workspaceExportEscape(row.ID) + `">`)
		for _, property := range properties {
			if property.Type == graphValueTypeTitle {
				builder.WriteString(`<td class="cell-title">` + renderer.rowTitleHTML(row) + "</td>")
				continue
			}
			builder.WriteString(`<td class="cell-` + workspaceExportEscape(property.ID) + `">` + renderer.cellHTML(document.cell(property, row)) + "</td>")
		}
		builder.WriteString("</tr>")
	}
	builder.WriteString("</tbody></table></div><br/><br/>")
	if titled {
		builder.WriteString("</div>")
	}
	return builder.String()
}

// workspaceExportPropertyIconHTML is a property kind's icon before its name.
func workspaceExportPropertyIconHTML(kind string) string {
	icon := workspaceExportPropertyIcons[kind]
	if icon == "" {
		return ""
	}
	return `<span class="icon property-icon">` + icon + "</span>"
}

// databaseArticleOpen opens the article of a database's own page, as
// Notion's export names a full-page database's (seen 2026-11-14: its ID, its
// icon).
func (renderer *workspaceExportRenderer) databaseArticleOpen(source *workspaceExportDocDataSource, icon workspaceExportIcon) string {
	id := source.PageID
	if id == "" {
		id = source.ID
	}
	return `<article id="` + workspaceExportEscape(renderer.articleID("page", id)) + `" class="page sans"` + icon.nameAttr() + `>`
}

// rowTitleHTML is a row's title in its database's table: its icon and
// title, linking its row page when the export holds one, as Notion's export
// writes it (seen 2026-11-14: <a …><img class="icon notion-static-icon" …/>
// 내장 행</a>, an emoji's <span class="icon" data-emoji="🍊"></span>).
func (renderer *workspaceExportRenderer) rowTitleHTML(row *workspaceExportDocRow) string {
	words := renderer.pageIcon(row.Icon, row.IconType).HTML() + workspaceExportEscape(workspaceExportTitle(row.Title, renderer.untitled))
	if href := renderer.link(row.ID); href != "" {
		return `<a href="` + workspaceExportEscape(href) + `"` + workspaceExportPageIDAttr(row.ID) + `>` + words + "</a>"
	}
	return words
}

func workspaceExportIconText(icon, iconType string) string {
	if iconType == "emoji" && strings.TrimSpace(icon) != "" {
		return icon + " "
	}
	return ""
}

// headerHTML opens a page header with its cover, icon and title, as
// Notion's export does (seen 2026-11-14): the cover (coverHTML), then the
// icon in its box (workspaceExportIcon), marked as under a cover when there
// is one; the caller closes it after any row properties.
func (renderer *workspaceExportRenderer) headerHTML(title string, icon workspaceExportIcon, cover workspaceExportCover) string {
	var builder strings.Builder
	builder.WriteString(`<header>`)
	drawn := renderer.coverHTML(cover)
	covered := drawn != ""
	builder.WriteString(drawn)
	if drawn := icon.HTML(); drawn != "" {
		box := "page-header-icon undefined"
		if covered {
			box = "page-header-icon page-header-icon-with-cover"
		}
		builder.WriteString(`<div class="` + box + `">` + drawn + `</div>`)
	}
	// Notion's export keeps an empty description line under the title (seen
	// 2026-11-07), which gives the title its room. The title is as written,
	// empty for none (seen 2026-11-13: "   " and "").
	builder.WriteString(`<h1 class="page-title" dir="auto">` + workspaceExportEscape(title) + "</h1>" +
		`<p class="page-description" dir="auto"></p>`)
	return builder.String()
}

// pageArticleHTML writes one page: its header, body in page order with
// subpages as links, and its discussions when comments are included.
func (renderer *workspaceExportRenderer) pageArticleHTML(page *workspaceExportDocPage) string {
	document := renderer.document
	renderer.useBlocks(document.Blocks[page.ID])
	body := []*workspaceExportDocBlock{}
	for _, item := range document.bodyItems(page) {
		if item.Block != nil {
			body = append(body, item.Block)
		} else {
			body = append(body, &workspaceExportDocBlock{
				ID: "page-link-" + item.Page.ID, PageID: page.ID, Type: graphBlockTypeLinkToPage,
				Content: map[string]any{"childPageId": item.Page.ID},
			})
		}
	}
	icon := renderer.pageIcon(page.Icon, page.IconType)
	return `<article id="` + workspaceExportEscape(renderer.articleID("page", page.ID)) + `" class="page sans"` + icon.nameAttr() + `>` +
		renderer.headerHTML(page.Title, icon, page.Cover) + "</header>" +
		`<div class="page-body">` + renderer.blocksHTML(body) + "</div>" +
		renderer.commentsHTML(page.ID) + "</article>"
}

// rowArticleHTML writes a database row as a page: its properties with a
// value (rowPageListsProperty), then its body blocks.
func (renderer *workspaceExportRenderer) rowArticleHTML(row *workspaceExportDocRow) string {
	document := renderer.document
	renderer.useBlocks(document.RowBlocks[row.ID])
	var properties strings.Builder
	properties.WriteString(`<table class="properties"><tbody>`)
	for _, property := range document.orderedProperties(row.DataSourceID) {
		cell := document.cell(property, row)
		if property.Type == graphValueTypeTitle || !renderer.rowPageListsProperty(property, cell) {
			continue
		}
		// Each row names its property's kind by Notion's own name for it, as
		// its MCP does (seen 2026-11-14: property-row-text, -file,
		// -phone_number, -auto_increment_id, -person, -place).
		properties.WriteString(`<tr class="property-row property-row-` + workspaceExportEscape(mcpNotionPropertyType(property.Type)) + `"><th>` +
			workspaceExportPropertyIconHTML(property.Type) + workspaceExportEscape(property.Name) + "</th><td>" + renderer.cellHTML(cell) + "</td></tr>")
	}
	properties.WriteString("</tbody></table>")
	icon := renderer.pageIcon(row.Icon, row.IconType)
	return `<article id="` + workspaceExportEscape(renderer.articleID("row", row.ID)) + `" class="page sans"` + icon.nameAttr() + `>` +
		renderer.headerHTML(row.Title, icon, row.Cover) + properties.String() + "</header>" +
		`<div class="page-body">` + renderer.blocksHTML(append(append([]*workspaceExportDocBlock(nil), renderer.children[""]...),
		document.rowSubPageLinks(row)...)) +
		"</div></article>"
}

// commentsHTML lists a page's discussions after its body: page comments
// first, then each block discussion linked to its block, replies in order.
func (renderer *workspaceExportRenderer) commentsHTML(pageID string) string {
	document := renderer.document
	if !document.Payload.Options.IncludeComments {
		return ""
	}
	comments := document.Comments[pageID]
	if len(comments) == 0 {
		return ""
	}
	replies := map[string][]*workspaceExportDocComment{}
	threads := []*workspaceExportDocComment{}
	for _, comment := range comments {
		if comment.ParentID != "" {
			replies[comment.ParentID] = append(replies[comment.ParentID], comment)
		} else {
			threads = append(threads, comment)
		}
	}
	var builder strings.Builder
	builder.WriteString(`<section class="comments"><h2>Comments</h2>`)
	for _, thread := range threads {
		class := "discussion"
		if thread.Resolved {
			class += " resolved"
		}
		builder.WriteString(`<div class="` + class + `">`)
		if thread.BlockID != "" {
			excerpt := ""
			if block := document.BlockByID[thread.BlockID]; block != nil {
				excerpt = workspaceExportPlainText(block.Runs)
			}
			builder.WriteString(`<p class="comment-target"><a href="#` + workspaceExportEscape(thread.BlockID) + `">` +
				workspaceExportEscape(workspaceExportTitle(excerpt, "↗")) + "</a></p>")
		}
		for _, comment := range append([]*workspaceExportDocComment{thread}, replies[thread.ID]...) {
			builder.WriteString(renderer.commentHTML(comment))
		}
		builder.WriteString("</div>")
	}
	builder.WriteString("</section>")
	return builder.String()
}

func (renderer *workspaceExportRenderer) commentHTML(comment *workspaceExportDocComment) string {
	author := renderer.document.personName(comment.AuthorID)
	body, _ := comment.Body.(map[string]any)
	status := ""
	if comment.Resolved {
		status = ` <span class="comment-status">Resolved</span>`
	}
	return `<div class="comment"><p class="comment-meta"><strong>` + workspaceExportEscape(author) + `</strong> <time>` +
		workspaceExportEscape(comment.CreatedAt) + "</time>" + status + `</p><div class="comment-body">` +
		renderer.runsHTML(workspaceExportRunsFromSpans(body["rich"], renderer.document.Language)) + "</div></div>"
}

// workspaceExportHTMLDocument is an exported file as Notion's export writes
// one (seen 2026-11-14): its character set as an http-equiv, its title and
// what else its head holds (its page's icon, its equations' style), its
// style, then its body and an empty line of the page's font after it.
// Unlike Notion's it is a standards document, which KaTeX needs to draw its
// equations (it refuses a quirks mode one), and fits a phone's width.
func workspaceExportHTMLDocument(title, head, body string) string {
	return "<!DOCTYPE html>\n<html><head><meta http-equiv=\"Content-Type\" content=\"text/html; charset=utf-8\"/>" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"/><title>" + workspaceExportEscape(title) + "</title>" +
		head + "<style>" + workspaceExportDocumentCSS + "</style></head><body>" + body +
		`<span class="sans" style="font-size:14px;padding-top:2em"></span></body></html>` + "\n"
}
