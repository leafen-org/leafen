package app

// A readable export with folders names each page's, database's and database
// page's folder in the folder holding it, a name taken there already getting
// its ID's first and last four characters (workspaceExportFolderPlacer). Which
// one comes first is the order below.

// claimsInBodyOrder reports whether a page's databases and sub-pages take
// their folders' names in its body's order, as Notion's Markdown & CSV export
// names them: an untitled page before an unnamed database takes "제목 없음/"
// and the database's rows "제목 없음 <ID 앞4-뒤4>/", a page before a database
// of its name likewise (seen 2026-11-13). Its HTML export names its
// databases' folders first and then its sub-pages' (seen 2026-11-13: the
// database "제목 없음/", the page's sub-page "같은 4161-397c/"), as the
// other exports here do.
func (document *workspaceExportDocument) claimsInBodyOrder(extension string) bool {
	return document.Payload.Options.CreateFolders && extension == ".md"
}

// workspaceExportFolderClaim is a database block or a sub-page taking its
// folder's name in the folder of the page holding it.
type workspaceExportFolderClaim struct {
	Container *workspaceExportDocContainer
	Page      *workspaceExportDocPage
}

// bodyFolderClaims are a page's or a database page's database blocks and
// sub-pages in its body's order — its top items in order (top), each
// block's nested blocks in place after it — then any its body does not show:
// its other database blocks, then its other sub-pages, a sub-page hanging on
// a line among them.
func (document *workspaceExportDocument) bodyFolderClaims(
	ownerID string, blocks []*workspaceExportDocBlock, top []workspaceExportBodyItem, children []*workspaceExportDocPage,
) []workspaceExportFolderClaim {
	claims := []workspaceExportFolderClaim{}
	containers, pages := map[*workspaceExportDocContainer]bool{}, map[string]bool{}
	sub := map[string]*workspaceExportDocPage{}
	for _, child := range children {
		sub[child.ID] = child
	}
	claimPage := func(page *workspaceExportDocPage) {
		if page != nil && sub[page.ID] == page && !pages[page.ID] {
			pages[page.ID] = true
			claims = append(claims, workspaceExportFolderClaim{Page: page})
		}
	}
	claimContainer := func(container *workspaceExportDocContainer) {
		if container != nil && !containers[container] {
			containers[container] = true
			claims = append(claims, workspaceExportFolderClaim{Container: container})
		}
	}
	nested := map[string][]*workspaceExportDocBlock{}
	for _, block := range blocks {
		if block.ParentID != "" {
			nested[block.ParentID] = append(nested[block.ParentID], block)
		}
	}
	var walk func(block *workspaceExportDocBlock)
	walk = func(block *workspaceExportDocBlock) {
		claimContainer(document.ByBlock[block.ID])
		if block.Type == graphBlockTypeChildPage {
			claimPage(sub[mapString(block.Content, "childPageId")])
		}
		for _, child := range nested[block.ID] {
			walk(child)
		}
	}
	for _, item := range top {
		if item.Block != nil {
			walk(item.Block)
		} else {
			claimPage(item.Page)
		}
	}
	for _, container := range document.pageContainers(ownerID) {
		claimContainer(container)
	}
	for _, child := range children {
		claimPage(child)
	}
	return claims
}
