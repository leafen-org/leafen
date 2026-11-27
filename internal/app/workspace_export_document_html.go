package app

import (
	"net/url"
	"strconv"
	"strings"
)

const (
	workspaceExportModeHTML     = "html"
	workspaceExportModeMarkdown = "markdown"
	workspaceExportModePrint    = "print"
)

// workspaceExportRenderer writes one page (or row page) of a readable export.
type workspaceExportRenderer struct {
	document *workspaceExportDocument
	mode     string
	from     string
	folder   string
	// page is the page a Markdown file is being written for.
	page string
	// linked are the pages a Markdown file has a page link line for so far
	// (blockMarkdown).
	linked   map[string]bool
	children map[string][]*workspaceExportDocBlock
	// blocks are the blocks being written by ID, to find a block's parents.
	blocks   map[string]*workspaceExportDocBlock
	headings []*workspaceExportDocBlock
	files    *workspaceExportFilePlacer
	embed    func(workspaceExportFileSnapshot) string
	// embedBytes takes bytes of a printable export's embedding budget, false
	// when they don't fit (embedDrawn).
	embedBytes func(int64) bool
	untitled   string
}

// link is where a page or a row page is in the export, or, for one the
// export leaves out, its page on this server, as Notion's export links one at
// its own app (seen 2026-11-07: a subpage left out is
// "https://app.notion.com/p/<id>", its ID without dashes as its file names
// write it). A trashed page stays unlinked.
func (renderer *workspaceExportRenderer) link(targetID string) string {
	if href := renderer.exportedLink(targetID); href != "" || renderer.mode == workspaceExportModePrint {
		return href
	}
	return renderer.document.pageAddress(targetID)
}

// pageAddress is a page's or a row page's address on this server, "" for a
// trashed page or without the server's address.
func (document *workspaceExportDocument) pageAddress(targetID string) string {
	if page := document.Pages[targetID]; targetID == "" || document.Origin == "" || page != nil && page.Trashed {
		return ""
	}
	return document.Origin + "/p/" + url.PathEscape(workspaceExportNameID(targetID))
}

// exportedLink is where a page or a row page is in the export, "" for one it
// leaves out.
func (renderer *workspaceExportRenderer) exportedLink(targetID string) string {
	document := renderer.document
	path, anchor := "", ""
	if page := document.Pages[targetID]; page != nil && page.Database != nil && !page.Trashed {
		// A full-page database is its database's file.
		path = page.Database.CSVPath
		if renderer.mode == workspaceExportModeHTML {
			path = workspaceExportDatabaseHTMLPath(path)
		}
	} else if page != nil && page.Path != "" && !page.Trashed {
		path, anchor = page.Path, "#page-"+targetID
	} else if row := document.RowByID[targetID]; row != nil && row.Path != "" {
		path, anchor = row.Path, "#row-"+targetID
	}
	if path == "" {
		return ""
	}
	if renderer.mode == workspaceExportModePrint {
		return anchor
	}
	return workspaceExportRelativeHref(renderer.from, path)
}

// fileHref places an included file and links to it. A printable export
// embeds images instead, since it is a single document.
func (renderer *workspaceExportRenderer) fileHref(fileID, _ string) string {
	return renderer.fileHrefIn(fileID, renderer.folder)
}

// fileHrefIn is fileHref for a file placed in folder, the folder of the page
// whose file it is wherever it is written (workspaceExportCellPart.FileFolder).
func (renderer *workspaceExportRenderer) fileHrefIn(fileID, folder string) string {
	file, exists := renderer.document.Files[fileID]
	if !exists || !renderer.document.Payload.Options.IncludeFiles || renderer.files == nil {
		return ""
	}
	if renderer.mode == workspaceExportModePrint {
		if renderer.embed == nil {
			return ""
		}
		return renderer.embed(file)
	}
	return workspaceExportRelativeHref(renderer.from, renderer.files.place(file, folder))
}

func (renderer *workspaceExportRenderer) runsHTML(runs []workspaceExportTextRun) string {
	return workspaceExportRunsHTML(renderer.document.namedRuns(runs, renderer.linkUntitled()), renderer.link,
		func(pageID string) string { return renderer.pageIconOf(pageID).HTML() }, renderer.document.People)
}

func (renderer *workspaceExportRenderer) useBlocks(blocks []*workspaceExportDocBlock) {
	renderer.children = map[string][]*workspaceExportDocBlock{}
	renderer.blocks = map[string]*workspaceExportDocBlock{}
	renderer.headings = nil
	for _, block := range blocks {
		renderer.children[block.ParentID] = append(renderer.children[block.ParentID], block)
		renderer.blocks[block.ID] = block
		if strings.Contains(block.Type, "heading_") {
			renderer.headings = append(renderer.headings, block)
		}
	}
}

// workspaceExportBlockColor is a block's color as Notion's export names it in
// a block-color-<color> class and a data-notion-highlight, "" for none.
func workspaceExportBlockColor(block *workspaceExportDocBlock) string {
	return strings.TrimPrefix(workspaceExportColorClass(mapString(block.Content, "color")), "highlight-")
}

// workspaceExportBlockAttrs are a block's id, class and direction as Notion's
// HTML export writes them on a block's element (seen 2026-11-14): its ID as
// it is, its class with its color's first (class="" for none), its color's
// data-notion-highlight, and dir="auto".
func workspaceExportBlockAttrs(id, class, color string) string {
	attrs := `id="` + workspaceExportEscape(id) + `" class="`
	if color != "" {
		return attrs + strings.TrimSpace("block-color-"+color+" "+class) + `" data-notion-highlight="` + color + `" dir="auto"`
	}
	return attrs + class + `" dir="auto"`
}

// articleID is the id a page's or a row page's article has: its ID, as
// Notion's HTML export names a page's (seen 2026-11-14), or in a printable
// export, which holds every page and links each by it, its kind before it
// (exportedLink).
func (renderer *workspaceExportRenderer) articleID(kind, id string) string {
	if renderer.mode == workspaceExportModePrint {
		return kind + "-" + id
	}
	return id
}

// listDepth is how many lists of the block's kind hold it right above it,
// which Notion's export marks a bulleted item's disc, circle and square and a
// numbered list's 1, a and i by (seen 2026-11-14), however deep
// (parent_chain.go).
func (renderer *workspaceExportRenderer) listDepth(block *workspaceExportDocBlock) int {
	depth := 0
	chain := parentChain[string]{block.ID: {}}
	for parent := renderer.blocks[block.ParentID]; parent != nil && parent.Type == block.Type && chain.climb(parent.ID); parent = renderer.blocks[parent.ParentID] {
		depth++
	}
	return depth
}

// blocksHTML writes blocks in order. Each list item is a list of its own, as
// Notion's export writes them (seen 2026-11-07), numbered items counting on
// from the one before; a to-do's text is marked done or not. A list item in
// a color is its list in a box of that color (seen 2026-11-14), and an
// item's list in an item of its kind is marked by its depth (listDepth).
func (renderer *workspaceExportRenderer) blocksHTML(blocks []*workspaceExportDocBlock) string {
	var builder strings.Builder
	number := 0
	for _, block := range blocks {
		if block.Type == graphBlockTypeNumberedListItem {
			number++
		} else {
			number = 0
		}
		color := workspaceExportBlockColor(block)
		children := renderer.blocksHTML(renderer.children[block.ID])
		list := ""
		switch block.Type {
		case graphBlockTypeBulletedListItem:
			marks := []string{"disc", "circle", "square"}
			list = `<ul ` + workspaceExportListAttrs(block.ID, "bulleted-list", color, "") + `><li style="list-style-type:` +
				marks[renderer.listDepth(block)%len(marks)] + `">` + renderer.runsHTML(block.Runs) + children + "</li></ul>"
		case graphBlockTypeNumberedListItem:
			marks := []string{"1", "a", "i"}
			list = `<ol type="` + marks[renderer.listDepth(block)%len(marks)] + `" ` +
				workspaceExportListAttrs(block.ID, "numbered-list", color, ` start="`+strconv.Itoa(number)+`"`) + "><li>" +
				renderer.runsHTML(block.Runs) + children + "</li></ol>"
		case graphBlockTypeToDo:
			box, checked, state := "off", "", "unchecked"
			if value, _ := block.Content["checked"].(bool); value {
				box, checked, state = "on", ` checked=""`, "checked"
			}
			list = `<ul ` + workspaceExportListAttrs(block.ID, "to-do-list", color, "") + `><li><input type="checkbox" class="checkbox checkbox-` +
				box + `" disabled=""` + checked + `/> <span class="to-do-children-` + state + `">` + renderer.runsHTML(block.Runs) +
				`</span><div class="indented">` + children + "</div></li></ul>"
		default:
			builder.WriteString(renderer.blockHTML(block))
			continue
		}
		if color != "" {
			list = `<div data-notion-highlight="` + color + `" dir="auto">` + list + "</div>"
		}
		builder.WriteString(list)
	}
	return builder.String()
}

// workspaceExportListAttrs are a list's attributes: its color's in its class
// with its data-notion-highlight and then any of its own (the box around it
// has the direction), else dir="auto" after its own.
func workspaceExportListAttrs(id, class, color, own string) string {
	if color == "" {
		return `id="` + workspaceExportEscape(id) + `" class="` + class + `"` + own + ` dir="auto"`
	}
	return `id="` + workspaceExportEscape(id) + `" class="block-color-` + color + " " + class + `" data-notion-highlight="` + color + `"` + own
}

// blockHTML writes a block that is no list item as Notion's HTML export
// writes it (seen 2026-11-14): its attributes (workspaceExportBlockAttrs);
// a toggle heading a toggle holding its heading, which has the block's ID;
// a toggle's and a toggle heading's blocks indented under it.
func (renderer *workspaceExportRenderer) blockHTML(block *workspaceExportDocBlock) string {
	id := `id="` + workspaceExportEscape(block.ID) + `"`
	color := workspaceExportBlockColor(block)
	attrs := workspaceExportBlockAttrs(block.ID, "", color)
	text := renderer.runsHTML(block.Runs)
	children := renderer.blocksHTML(renderer.children[block.ID])
	switch block.Type {
	case graphBlockTypeParagraph:
		// An empty one holds a line break, which keeps its line in the
		// page's pre-wrap text, and its blocks are indented inside it, as
		// Notion's (seen 2026-11-14: <p …>글<div class="indented">…</div></p>,
		// which a reader closes before the box).
		if text == "" {
			text = "\n"
		}
		return "<p " + attrs + ">" + text + workspaceExportIndented(children) + "</p>"
	case graphBlockTypeHeading1, graphBlockTypeHeading2, graphBlockTypeHeading3, graphBlockTypeHeading4:
		level := block.Type[len(block.Type)-1:]
		return "<h" + level + " " + attrs + ">" + text + "</h" + level + ">"
	case graphBlockTypeToggleHeading1, graphBlockTypeToggleHeading2, graphBlockTypeToggleHeading3, graphBlockTypeToggleHeading4:
		level := block.Type[len(block.Type)-1:]
		box := `<details open="" class=""` + ` dir="auto">`
		if color != "" {
			box = `<details open="" class="block-color-` + color + `" data-notion-highlight="` + color + `" dir="auto">`
		}
		return box + `<summary style="font-weight:600;font-size:` + workspaceExportToggleHeadingSizes[level] +
			`;line-height:1.3;margin:0"><h` + level + " " + id + ` style="display:inline-block">` + text + "</h" + level +
			`></summary><div class="indented">` + children + "</div></details>"
	case graphBlockTypeToggle:
		toggle := strings.Replace(workspaceExportBlockAttrs(block.ID, "toggle", color), ` class=`, ` open="" class=`, 1)
		return "<details " + toggle + "><summary>" + text + `</summary><div class="indented">` + children + "</div></details>"
	case graphBlockTypeQuote:
		return "<blockquote " + attrs + ">" + text + children + "</blockquote>"
	case graphBlockTypeCallout:
		return renderer.calloutHTML(block)
	case graphBlockTypeDivider:
		return "<hr " + id + ` dir="auto"/>`
	case graphBlockTypeCode:
		return renderer.codeHTML(block, id)
	case graphBlockTypeEquation:
		expression := workspaceExportEscape(mapString(block.Content, "expression"))
		return `<figure ` + id + ` class="equation" data-notion-equation="` + expression + `" dir="auto"><div class="equation-container"><code>` +
			expression + "</code></div></figure>"
	case graphBlockTypeTable:
		return renderer.tableHTML(block)
	case graphBlockTypeImage, graphBlockTypeVideo, graphBlockTypeAudio, graphBlockTypePDF, graphBlockTypeFile,
		graphBlockTypeEmbed, graphBlockTypeBookmark, graphBlockTypeLinkPreview:
		return renderer.mediaHTML(block, id)
	case graphBlockTypeChildPage, graphBlockTypeLinkToPage:
		return renderer.pageLinkHTML(block)
	case graphBlockTypeInlineDatabase, graphBlockTypeChildDatabase:
		if container := renderer.document.ByBlock[block.ID]; container != nil {
			return renderer.containerHTML(container)
		}
		return renderer.pageLinkHTML(block)
	case graphBlockTypeColumnList:
		return renderer.columnListHTML(block)
	case graphBlockTypeSyncedBlock:
		// A copy of a page's body is its page's link alone (workspace_export_synced_page.go).
		if page := syncedCopyPage(block.Content); page != "" {
			return renderer.pageLinkHTML(syncedPageLinkBlock(block, page))
		}
		return `<div ` + id + ` class="synced-block">` + renderer.syncedHTML(block, children) + "</div>"
	case graphBlockTypeTab:
		var tabs strings.Builder
		for _, label := range renderer.children[block.ID] {
			tabs.WriteString(`<section class="tab"><h4 class="tab-label">` +
				workspaceExportEscape(strings.TrimSpace(mapString(label.Content, "icon")+" ")) + renderer.runsHTML(label.Runs) +
				"</h4>" + renderer.blocksHTML(renderer.children[label.ID]) + "</section>")
		}
		return `<div ` + id + ` class="tabs">` + tabs.String() + "</div>"
	case graphBlockTypeButton:
		// The name the button shows, as the editor stores it.
		label := mapString(block.Content, "label")
		if label == "" {
			label = mapString(block.Content, "buttonLabel")
		}
		return `<p ` + id + ` class="button"><span>` + workspaceExportEscape(label) + "</span></p>"
	case graphBlockTypeTemplate:
		return `<div ` + id + ` class="template"><p>` + text + "</p>" + children + "</div>"
	case graphBlockTypeTableOfContents:
		return renderer.tableOfContentsHTML(block.ID, mapString(block.Content, "color"))
	case graphBlockTypeBreadcrumb:
		return `<p ` + id + ` class="breadcrumb">` + renderer.breadcrumbHTML(block.PageID) + "</p>"
	case graphBlockTypeColumn:
		return children
	default:
		return "<p " + attrs + ">" + text + "</p>" + children
	}
}

// workspaceExportPageIDAttr names the page a link leads to, as Notion's
// export marks a link to a page (seen 2026-11-14). Notion names its
// workspace there too (data-notion-space-id), which Leafen leaves out.
func workspaceExportPageIDAttr(pageID string) string {
	if pageID == "" {
		return ""
	}
	return ` data-notion-page-id="` + workspaceExportEscape(pageID) + `"`
}

func workspaceExportIndented(children string) string {
	if children == "" {
		return ""
	}
	return `<div class="indented">` + children + "</div>"
}

// pageTitle is the title a page link names its page by: the export's page or
// database page, or one it leaves out as its exporter reads it now, as a
// mention of it is named (seen 2026-11-13: a link to a page outside the
// export "[w766 밖](https://app.notion.com/p/…)"), else the title the link
// was given.
func (renderer *workspaceExportRenderer) pageTitle(pageID string, fallback string) string {
	if page := renderer.document.Pages[pageID]; page != nil {
		// A full-page database is linked as its database is, an unnamed one
		// by the untitled name in Markdown too (seen 2026-11-13: "[제목
		// 없음](…제목%20없음%20〈id〉.csv)").
		if page.Database != nil {
			return workspaceExportTitle(page.Title, renderer.untitled)
		}
		return workspaceExportTitle(page.Title, renderer.linkUntitled())
	}
	if row := renderer.document.RowByID[pageID]; row != nil {
		return workspaceExportTitle(row.Title, renderer.linkUntitled())
	}
	if pageID != "" && renderer.document.names != nil {
		if title, named := renderer.document.names.page(pageID); named {
			return workspaceExportTitle(title, renderer.linkUntitled())
		}
	}
	return workspaceExportTitle(fallback, renderer.linkUntitled())
}

// linkUntitled is the words a link to a page without a title shows: none in
// Markdown, the untitled name in HTML, as Notion's export writes them (seen
// 2026-11-13: "[](…)" and "<a …>제목 없음</a>").
func (renderer *workspaceExportRenderer) linkUntitled() string {
	if renderer.mode == workspaceExportModeMarkdown {
		return ""
	}
	return renderer.untitled
}

// linkTitle is a page link's or a sub-page's words: its page's title, or
// for one closed to the exporter "사용 권한 없음" (graphBlockAsRead).
func (renderer *workspaceExportRenderer) linkTitle(block *workspaceExportDocBlock, target string) string {
	if unavailable, _ := block.Content[publicPageLinkUnavailableKey].(bool); unavailable {
		return closedMentionLabel(renderer.document.Language)
	}
	return renderer.pageTitle(target, mapString(block.Content, "childPageTitle"))
}

// pageLinkHref is where a page link or sub-page leads, "" for one naming a
// page closed to the exporter, which says so without its page's address,
// as a mention of it does (workspace_export_document_mention_names.go).
func (renderer *workspaceExportRenderer) pageLinkHref(block *workspaceExportDocBlock, target string) string {
	if unavailable, _ := block.Content[publicPageLinkUnavailableKey].(bool); unavailable {
		return ""
	}
	return renderer.link(target)
}

// workspaceExportTitle is a title as a link, a mention, a table's title
// cell or a file's <title> names its page or database: as written, its
// spaces too, and untitled for none, as Notion's export names them (seen
// 2026-11-13: a title of spaces "[   ]" and "<a …>   </a>", " 앞뒤 " as it
// is). A page's own heading is written apart (markdownFile, headerHTML).
func workspaceExportTitle(title, untitled string) string {
	if title == "" {
		return untitled
	}
	return title
}

// pageLinkTarget is the page a sub-page's line or a page link names: its
// page, or for a link to a database on a page of its own, that page, which
// Notion's export links as it does the database's sub-page line (seen
// 2026-11-13).
func (renderer *workspaceExportRenderer) pageLinkTarget(block *workspaceExportDocBlock) string {
	if target := mapString(block.Content, "childPageId"); target != "" {
		return target
	}
	if container := renderer.document.Containers[mapString(block.Content, "databaseContainerId")]; container != nil {
		if page := renderer.document.Pages[container.PageID]; page != nil && page.Kind == pageKindDatabase {
			return page.ID
		}
	}
	return ""
}

// pageLinkHTML writes a sub-page's line or a page link as a figure named by
// the page it leads to, as Notion's HTML export names each — a sub-page, a
// link to a page, to a database page and to a database alike (seen
// 2026-11-13: id="〈the page's ID〉", the same twice for a page linked twice);
// one leading nowhere it may name is named by its own block. Its link names
// the page in data-notion-page-id (workspaceExportPageIDAttr) and shows its
// icon before its title (workspaceExportIcon).
func (renderer *workspaceExportRenderer) pageLinkHTML(block *workspaceExportDocBlock) string {
	target := renderer.pageLinkTarget(block)
	if (target == "" && mapString(block.Content, "childPageTitle") == "") || renderer.subPageLeftBehind(block, target) {
		return ""
	}
	figure := target
	if unavailable, _ := block.Content[publicPageLinkUnavailableKey].(bool); unavailable || figure == "" {
		figure = block.ID
	}
	title := workspaceExportEscape(renderer.linkTitle(block, target))
	icon := renderer.pageIconOf(target).HTML()
	if href := renderer.pageLinkHref(block, target); href != "" {
		title = `<a href="` + workspaceExportEscape(href) + `"` + workspaceExportPageIDAttr(target) + `>` + icon + title + "</a>"
	} else {
		title = icon + title
	}
	return `<figure id="` + workspaceExportEscape(figure) + `" class="link-to-page">` + title + "</figure>"
}

// subPageLeftBehind is a sub-page's line whose page went while the export ran:
// the export took the line, then found no page, so nothing of it is left to say.
func (renderer *workspaceExportRenderer) subPageLeftBehind(block *workspaceExportDocBlock, target string) bool {
	return block.Type == graphBlockTypeChildPage && target != "" && mapString(block.Content, "childPageTitle") == "" &&
		renderer.exportedLink(target) == "" && renderer.document.Pages[target] == nil
}
