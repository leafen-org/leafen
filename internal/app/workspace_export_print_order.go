package app

// workspaceExportPrintItem is one thing that prints after a page's own
// article: a sub-page, or a database whose rows print.
type workspaceExportPrintItem struct {
	page      *workspaceExportDocPage
	container *workspaceExportDocContainer
}

// printItems lists what prints after a page (or a database page) in the
// order its body shows it: a page block's sub-page, a sub-page hanging on a
// line, and a database block each where it stands, inside other blocks too.
// Sub-pages and databases its body doesn't show come last, as the page's
// links to them do.
func (writer *workspaceExportDocumentWriter) printItems(pageID string, top []workspaceExportBodyItem,
	blocks []*workspaceExportDocBlock, children []*workspaceExportDocPage) []workspaceExportPrintItem {
	document := writer.document
	nested := map[string][]*workspaceExportDocBlock{}
	for _, block := range blocks {
		if block.ParentID != "" {
			nested[block.ParentID] = append(nested[block.ParentID], block)
		}
	}
	byID := map[string]*workspaceExportDocPage{}
	for _, child := range children {
		byID[child.ID] = child
	}
	items := []workspaceExportPrintItem{}
	placed := map[string]bool{}
	addPage := func(id string) {
		if child := byID[id]; child != nil && !placed[id] {
			placed[id] = true
			items = append(items, workspaceExportPrintItem{page: child})
		}
	}
	addContainer := func(container *workspaceExportDocContainer) {
		if container != nil && !placed["container:"+container.ID] {
			placed["container:"+container.ID] = true
			items = append(items, workspaceExportPrintItem{container: container})
		}
	}
	var walk func(block *workspaceExportDocBlock)
	walk = func(block *workspaceExportDocBlock) {
		if block.Type == graphBlockTypeChildPage {
			addPage(mapString(block.Content, "childPageId"))
		}
		for _, id := range writer.anchors[block.ID] {
			addPage(id)
		}
		addContainer(document.ByBlock[block.ID])
		for _, child := range nested[block.ID] {
			walk(child)
		}
	}
	for _, id := range writer.anchors[workspaceExportTitleAnchor+pageID] {
		addPage(id)
	}
	for _, item := range top {
		if item.Page != nil {
			addPage(item.Page.ID)
		} else {
			walk(item.Block)
		}
	}
	for _, child := range children {
		addPage(child.ID)
	}
	for _, container := range document.pageContainers(pageID) {
		addContainer(container)
	}
	return items
}

// rowPrintItems lists what prints after a database page: its body's
// top blocks in order, then its sub-pages its body doesn't show.
func (writer *workspaceExportDocumentWriter) rowPrintItems(row *workspaceExportDocRow) []workspaceExportPrintItem {
	blocks := writer.document.RowBlocks[row.ID]
	top := []workspaceExportBodyItem{}
	for _, block := range blocks {
		if block.ParentID == "" {
			top = append(top, workspaceExportBodyItem{Block: block})
		}
	}
	return writer.printItems(row.ID, top, blocks, row.Children)
}

// noteAnchors keeps which line each of a page's sub-pages hanging on a line
// hangs on, read while the page's text is decoded, so the sub-page prints
// where its line stands. One no line of its body names hangs on its title
// and prints first, as the title stands above the body (childPageBodyOrder).
func (writer *workspaceExportDocumentWriter) noteAnchors(page *workspaceExportDocPage) {
	anchored := map[string]bool{}
	for _, child := range page.Children {
		if child.Anchored {
			anchored[child.ID] = true
		}
	}
	if len(anchored) == 0 {
		return
	}
	for _, block := range writer.document.Blocks[page.ID] {
		for _, run := range block.Runs {
			if anchored[run.PageID] {
				delete(anchored, run.PageID)
				writer.anchors[block.ID] = append(writer.anchors[block.ID], run.PageID)
			}
		}
	}
	for _, child := range page.Children {
		if anchored[child.ID] {
			writer.anchors[workspaceExportTitleAnchor+page.ID] = append(writer.anchors[workspaceExportTitleAnchor+page.ID], child.ID)
		}
	}
}

// workspaceExportTitleAnchor keys, in noteAnchors, the sub-pages hanging on
// a page's title.
const workspaceExportTitleAnchor = "title:"
