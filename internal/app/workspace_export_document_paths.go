package app

import (
	"sort"
	"strings"
	"unicode"
)

// Readable exports name each page "<title> <id>" like Notion so titles may
// repeat. With folders, a page's subpages, databases and files sit in a
// folder named by the page's title alone, as Notion's export names it (seen
// 2026-11-07: "출시 계획 <id>.md" beside "출시 계획/"); pages of one title in
// one folder share it, their files told apart by their ids. Without folders,
// every file sits at the archive root.

// workspaceExportNameLength is how many characters of a title Notion's export
// keeps in a name: 50, an emoji one (seen 2026-11-13).
const workspaceExportNameLength = 50

// workspaceExportFileStem names a page's, a database's or a database page's
// file and folder as Notion's export does (seen 2026-11-13 in a Markdown &
// CSV export, pages, an inline database, its CSVs, folder and database
// pages alike): each of / \ : * ? " < > | and . a space, as is each space,
// tab or line break, and any other control character gone; runs of spaces
// one, the edges trimmed, then its first 50 characters, a space cut there
// kept ("a…a bcd" is 49 a's and a space). # % [ ] { } ^ ~ ' ! & $ @ + = ; ,
// stay. No title takes the untitled name; a title of those characters,
// dots or spaces alone leaves a name of nothing before the ID, as Notion's
// export writes it (seen 2026-11-13: "***", "...", "   " and "<>|" named
// " <id>.md" and " <id>.csv", where no title is "제목 없음 <id>.md"); such a
// name stands apart (workspaceExportFileFolder, blankFolder).
func workspaceExportFileStem(title, id, untitled string) string {
	stem := workspaceExportNameText(title)
	if runes := []rune(stem); len(runes) > workspaceExportNameLength {
		stem = string(runes[:workspaceExportNameLength])
	}
	if title == "" {
		stem = untitled
	}
	if id == "" {
		return stem
	}
	return stem + " " + workspaceExportNameID(id)
}

// workspaceExportNameID is an ID as an export's names end in it: a UUID as
// Notion's export and its addresses write one, its 32 characters without
// dashes (seen 2026-11-13: "작업 목록 4161fd679738817e8289cc9a487b08d2.csv",
// which /p/4161fd679738817e8289cc9a487b08d2 opens), as this server's /p/
// opens it too; a page's own 15 characters, its address, as they are.
func workspaceExportNameID(id string) string {
	if len(id) == 36 && strings.Count(id, "-") == 4 {
		return strings.ToLower(strings.ReplaceAll(id, "-", ""))
	}
	return id
}

// databaseNameID is the ID a database's files are named by: its address,
// the block its database is, which opens it as Notion's does (a full-page
// one's page too, inlineDatabaseAddress.ts), not its data source's; a source
// one database shows among others, or none, keeps its own.
func (document *workspaceExportDocument) databaseNameID(source *workspaceExportDocDataSource) string {
	for _, container := range document.Containers {
		if container.Kind != databaseContainerKindSource || len(container.DataSourceIDs) != 1 ||
			container.DataSourceIDs[0] != source.ID {
			continue
		}
		if container.BlockID != "" {
			return container.BlockID
		}
		return container.ID
	}
	return source.ID
}

// workspaceExportBlankName reports a title that names nothing in an export:
// a title there is, made only of characters a name leaves out.
func workspaceExportBlankName(title string) bool {
	return title != "" && workspaceExportNameText(title) == ""
}

// workspaceExportFileFolder is the folder a page's, a database's or a row's
// file stands in, given the folder it would stand in. Notion's export joins
// a name of nothing to that folder as its own folder, which is that folder
// itself, so its file stands one folder up (seen 2026-11-13: a page "***"
// under "w748 빈 이름 내보내기", a database "<>|" and its row "***" beside
// that page's own file, not in its folder).
func workspaceExportFileFolder(folder, title string) string {
	if !workspaceExportBlankName(title) {
		return folder
	}
	return workspaceExportParentFolder(folder)
}

// workspaceExportParentFolder is the folder holding a folder, "" at the top.
func workspaceExportParentFolder(folder string) string {
	trimmed := strings.TrimSuffix(folder, "/")
	if at := strings.LastIndex(trimmed, "/"); at >= 0 {
		return trimmed[:at+1]
	}
	return ""
}

// workspaceExportNameText is a title's words as Notion's export names a file
// by them (workspaceExportFileStem), before they are cut to length.
func workspaceExportNameText(title string) string {
	var builder strings.Builder
	for _, character := range title {
		switch {
		case unicode.IsSpace(character) || strings.ContainsRune(`/\:*?"<>|.`, character):
			builder.WriteRune(' ')
		case unicode.IsControl(character):
		default:
			builder.WriteRune(character)
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

// workspaceExportFolderName is the folder holding a page's, a database's or
// a row's own files: its title alone, none for a name of nothing (its files
// stand in the folder holding it, workspaceExportFileFolder).
func workspaceExportFolderName(title, untitled string) string {
	if workspaceExportBlankName(title) {
		return ""
	}
	return workspaceExportFileStem(title, "", untitled) + "/"
}

// workspaceExportFolderPlacer gives each page, database and row a folder of
// its own: its title, or where that folder is taken in the same place, its
// title and its ID's first and last four characters, as Notion's export
// names the second of two pages of one title (seen 2026-11-07: "같은 이름/"
// and "같은 이름 4111-8d5d/").
type workspaceExportFolderPlacer map[string]bool

func (placer workspaceExportFolderPlacer) folder(parent, title, id, untitled string) string {
	if workspaceExportBlankName(title) {
		return placer.blankFolder(parent, id)
	}
	name := parent + workspaceExportFolderName(title, untitled)
	if placer[name] && len(id) > 8 {
		name = parent + workspaceExportFileStem(title, workspaceExportShortID(id), untitled) + "/"
	}
	placer[name] = true
	return name
}

// blankFolder is the folder of a page, a database or a row whose name is of
// nothing: the folder it stands in, as Notion's export joins the name to it,
// taken there by the page that folder is, so named again with the ID's
// first and last four characters beside it (seen 2026-11-13: "???"'s
// sub-page and "<>|"'s rows in "w748 빈 이름 내보내기 4161-ac03/" and
// "w748 빈 이름 내보내기 24f0-dfdd/" beside "w748 빈 이름 내보내기/").
func (placer workspaceExportFolderPlacer) blankFolder(parent, id string) string {
	if parent == "" {
		return ""
	}
	name := parent
	if placer[name] && len(id) > 8 {
		name = strings.TrimSuffix(parent, "/") + " " + workspaceExportShortID(id) + "/"
	}
	placer[name] = true
	return name
}

// workspaceExportShortID is an ID's first and last four characters, as
// Notion's export tells apart two folders of one name.
func workspaceExportShortID(id string) string {
	compact := strings.ReplaceAll(id, "-", "")
	return compact[:4] + "-" + compact[len(compact)-4:]
}

// workspaceExportFileName names an attached file in the archive by its own
// name, as Notion's export does, whose upload already kept only the
// characters it names files with (notionFileUploadName; seen 2026-11-13:
// "..점_파일..txt" as it was kept, its dots too). A name given here otherwise
// loses / \ : * ? " < > | # % and control characters; one of dots alone,
// or of nothing, is "file"; a long one keeps its last 100 characters, its
// extension among them.
func workspaceExportFileName(name string) string {
	var builder strings.Builder
	for _, character := range strings.TrimSpace(name) {
		if unicode.IsControl(character) || strings.ContainsRune(`/\:*?"<>|#%`, character) {
			continue
		}
		builder.WriteRune(character)
	}
	cleaned := strings.TrimSpace(builder.String())
	if strings.Trim(cleaned, ".") == "" {
		return "file"
	}
	if runes := []rune(cleaned); len(runes) > 100 {
		cleaned = string(runes[len(runes)-100:])
	}
	return cleaned
}

// workspaceExportRelativeHref links from one archive file to another,
// escaping each path segment for use in an href as Notion's export does.
func workspaceExportRelativeHref(from, to string) string {
	fromParts := strings.Split(from, "/")
	fromParts = fromParts[:len(fromParts)-1]
	toParts := strings.Split(to, "/")
	common := 0
	for common < len(fromParts) && common < len(toParts)-1 && fromParts[common] == toParts[common] {
		common++
	}
	segments := []string{}
	for range fromParts[common:] {
		segments = append(segments, "..")
	}
	for _, part := range toParts[common:] {
		segments = append(segments, workspaceExportHrefSegment(part))
	}
	return strings.Join(segments, "/")
}

// workspaceExportHrefSegment escapes one file or folder name for a link the
// way Notion's export writes them, as JavaScript's encodeURI does (seen
// 2026-11-12 in a Markdown export's links: ",;@&=+$!'()~" stay, a space,
// "[]{}^`" and every non-ASCII byte become %XX). "/", "?" and "#", which
// would end the segment, are escaped too: a name may hold "#"
// (workspaceExportFileStem), which Notion's link writes bare and so opens
// nothing (seen 2026-11-13).
func workspaceExportHrefSegment(part string) string {
	const hex = "0123456789ABCDEF"
	var builder strings.Builder
	for index := 0; index < len(part); index++ {
		character := part[index]
		if 'a' <= character && character <= 'z' || 'A' <= character && character <= 'Z' || '0' <= character && character <= '9' ||
			strings.IndexByte("-_.!~*'();,:@&=+$", character) >= 0 {
			builder.WriteByte(character)
			continue
		}
		builder.WriteByte('%')
		builder.WriteByte(hex[character>>4])
		builder.WriteByte(hex[character&15])
	}
	return builder.String()
}

// assignPaths places every exported page, database and row page in the
// export's top folder, section (workspaceExportSectionFolder), which a name
// of nothing there stands above as it stands above any page's folder
// (workspaceExportFileFolder; seen 2026-11-16 twice: "<>|" and its sub-page
// "???" at the archive's root, their other sub-pages and databases in
// "개인 페이지 & 공유된 페이지/"). Printable exports keep one document and
// link by anchor instead.
func (document *workspaceExportDocument) assignPaths(extension, untitled, section string) {
	createFolders := document.Payload.Options.CreateFolders
	folders := workspaceExportFolderPlacer{}
	document.folders = folders
	var place func(page *workspaceExportDocPage, folder string)
	var placeRowChildren func(dataSourceID string)
	// A full-page database's database is placed with its page, in the
	// page's place, as Notion's export writes it, though a linked view of
	// it comes first (seen 2026-11-13: a view before the page in its
	// parent's body, and Notion's Markdown & CSV export still links the page
	// to the database's CSV, with no file of the page's own); the view
	// links to it there. One whose page the export never reaches stands
	// where its first view is (waiting).
	ownPages := document.fullPageContainers()
	type waitingPlace struct {
		container *workspaceExportDocContainer
		folder    string
	}
	waiting := map[string]waitingPlace{}
	// placeContainer places a database block's databases, each once, and
	// their rows.
	placeContainer := func(container *workspaceExportDocContainer, folder string) {
		if document.placeLinkedView(container, folder, extension, untitled) {
			return
		}
		for _, dataSourceID := range container.DataSourceIDs {
			if own := ownPages[dataSourceID]; own != nil && own != container {
				if _, kept := waiting[dataSourceID]; !kept {
					waiting[dataSourceID] = waitingPlace{container: container, folder: folder}
				}
				continue
			}
			if source := document.DataSources[dataSourceID]; source != nil && source.CSVPath == "" {
				document.placeDatabase(dataSourceID, container, folder, extension, untitled)
				placeRowChildren(dataSourceID)
			}
		}
	}
	// placeClaims places a page's or a database page's databases and
	// sub-pages in the folder they share, in the order they take their
	// folders' names there (bodyFolderClaims).
	placeClaims := func(claims []workspaceExportFolderClaim, folder string) {
		for _, claim := range claims {
			if claim.Container != nil {
				placeContainer(claim.Container, folder)
			} else if claim.Page.Path == "" {
				place(claim.Page, folder)
			}
		}
	}
	// A database page's sub-pages and databases go with the database page's
	// file.
	placeRow := func(row *workspaceExportDocRow, folder string) {
		if createFolders {
			folder = folders.folder(folder, row.Title, row.ID, untitled)
			row.Folder = folder
		}
		if document.claimsInBodyOrder(extension) {
			top := []workspaceExportBodyItem{}
			for _, block := range document.RowBlocks[row.ID] {
				if block.ParentID == "" {
					top = append(top, workspaceExportBodyItem{Block: block})
				}
			}
			for _, link := range document.rowSubPageLinks(row) {
				top = append(top, workspaceExportBodyItem{Page: document.Pages[mapString(link.Content, "childPageId")]})
			}
			placeClaims(document.bodyFolderClaims(row.ID, document.RowBlocks[row.ID], top, row.Children), folder)
			return
		}
		for _, child := range row.Children {
			if child.Path == "" {
				place(child, folder)
			}
		}
		for _, container := range document.pageContainers(row.ID) {
			placeContainer(container, folder)
		}
	}
	placeRowChildren = func(dataSourceID string) {
		source := document.DataSources[dataSourceID]
		if source == nil {
			return
		}
		for _, row := range document.Rows[dataSourceID] {
			if row.Path != "" {
				placeRow(row, source.Folder)
			}
		}
	}
	place = func(page *workspaceExportDocPage, folder string) {
		if page.Listed {
			return
		}
		// A full-page database is its database: its files stand where the
		// page's would, and the page has none of its own.
		if source, container := document.fullPageSource(page); source != nil {
			page.Database, source.FullPage = source, true
			document.placeDatabase(source.ID, container, folder, extension, untitled)
			placeRowChildren(source.ID)
			return
		}
		stem := workspaceExportFileStem(page.Title, page.ID, untitled)
		page.Path = workspaceExportFileFolder(folder, page.Title) + stem + extension
		page.Folder = folder
		if createFolders {
			page.Folder = folders.folder(folder, page.Title, page.ID, untitled)
		}
		if document.claimsInBodyOrder(extension) {
			placeClaims(document.bodyFolderClaims(page.ID, document.Blocks[page.ID], document.bodyItems(page), page.Children), page.Folder)
			return
		}
		for _, container := range document.pageContainers(page.ID) {
			placeContainer(container, page.Folder)
		}
		for _, child := range page.Children {
			place(child, page.Folder)
		}
	}
	for _, root := range document.Roots {
		place(root, section)
	}
	// A database page shared on its own stands at the top as a page.
	for _, row := range document.sharedRows() {
		row.Path = workspaceExportFileFolder(section, row.Title) + workspaceExportFileStem(row.Title, row.ID, untitled) + extension
		placeRow(row, section)
	}
	if document.Payload.Scope.Kind == workspaceExportScopeDatabase {
		document.placeDatabase(document.Payload.Scope.TargetID, nil, section, extension, untitled)
		// A linked view's CSV is named after the view; its source's "_all"
		// stands where the source does (workspace_export_linked_view.go).
		if view, source := document.Payload.LinkedView, document.DataSources[document.Payload.Scope.TargetID]; view != nil && source != nil {
			source.AllCSVPath = workspaceExportFileFolder(section+document.SourceFolder, source.Title) +
				workspaceExportFileStem(source.Title, document.databaseNameID(source), untitled) + "_all.csv"
			source.CSVPath = workspaceExportFileFolder(section, view.Title) + workspaceExportFileStem(view.Title, view.ID, untitled) + ".csv"
		}
		placeRowChildren(document.Payload.Scope.TargetID)
	}
	for _, dataSourceID := range workspaceExportSortedKeys(waiting) {
		if source := document.DataSources[dataSourceID]; source != nil && source.CSVPath == "" {
			document.placeDatabase(dataSourceID, waiting[dataSourceID].container, waiting[dataSourceID].folder, extension, untitled)
			placeRowChildren(dataSourceID)
		}
	}
}

// fullPageSource is the database a full-page database's page is, as Notion's
// export writes one (seen 2026-10-09, a task tracker: "작업 트래커 <id>.csv"
// and its "_all" file beside "작업 트래커/" of its database pages, and no
// file of the page's own; its HTML export the database's table in the
// page's place): the page's own one database with its container, or nil for
// any other page and in a printable export, which keeps the page.
func (document *workspaceExportDocument) fullPageSource(page *workspaceExportDocPage) (*workspaceExportDocDataSource, *workspaceExportDocContainer) {
	source, container := document.fullPageDatabase(page)
	if source == nil || source.CSVPath != "" {
		return nil, nil
	}
	return source, container
}

// fullPageDatabase is a full-page database's page's own one database and its
// container, placed or not (fullPageSource).
func (document *workspaceExportDocument) fullPageDatabase(page *workspaceExportDocPage) (*workspaceExportDocDataSource, *workspaceExportDocContainer) {
	if page.Kind != pageKindDatabase || len(page.Children) > 0 || workspaceExportDocumentMode(document.Payload) == workspaceExportModePrint {
		return nil, nil
	}
	containers := document.pageContainers(page.ID)
	if len(containers) != 1 || containers[0].Kind != databaseContainerKindSource || len(containers[0].DataSourceIDs) != 1 {
		return nil, nil
	}
	source := document.DataSources[containers[0].DataSourceIDs[0]]
	if source == nil || source.External || source.PageID != page.ID {
		return nil, nil
	}
	return source, containers[0]
}

// fullPageContainers are the exported full-page databases' own containers
// by their databases (fullPageDatabase).
func (document *workspaceExportDocument) fullPageContainers() map[string]*workspaceExportDocContainer {
	containers := map[string]*workspaceExportDocContainer{}
	for _, page := range document.Pages {
		if page.Listed || page.Trashed {
			continue
		}
		if source, container := document.fullPageDatabase(page); source != nil {
			containers[source.ID] = container
		}
	}
	return containers
}

// placeDatabase gives a database its CSV next to the first page that shows
// it. A source outside the export has no row pages of its own.
func (document *workspaceExportDocument) placeDatabase(
	dataSourceID string, container *workspaceExportDocContainer, folder, extension, untitled string,
) {
	source := document.DataSources[dataSourceID]
	if source == nil || source.CSVPath != "" {
		return
	}
	nameID := document.databaseNameID(source)
	stem := workspaceExportFileStem(source.Title, nameID, untitled)
	source.CSVPath = workspaceExportFileFolder(folder, source.Title) + stem + ".csv"
	source.Folder = folder
	source.Container = container
	if document.Payload.Options.CreateFolders {
		source.Folder = document.folders.folder(folder, source.Title, nameID, untitled)
	}
	if !document.exportsRowPages() || source.External {
		return
	}
	for _, row := range document.Rows[dataSourceID] {
		row.Path = workspaceExportFileFolder(source.Folder, row.Title) + workspaceExportFileStem(row.Title, row.ID, untitled) + extension
	}
}

// sharedRows are the database pages an export takes without their databases,
// in title order: those shared with a workspace export's exporter on their
// own, and a page export's root database page (workspaceExportBesideRootSQL).
func (document *workspaceExportDocument) sharedRows() []*workspaceExportDocRow {
	rows, kind := []*workspaceExportDocRow{}, document.Payload.Scope.Kind
	if kind != workspaceExportScopeWorkspace && kind != workspaceExportScopePage {
		return rows
	}
	for _, id := range workspaceExportSortedKeys(document.RowByID) {
		if row := document.RowByID[id]; document.DataSources[row.DataSourceID] == nil && (kind != workspaceExportScopePage || id == document.Payload.Scope.TargetID) {
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(left, right int) bool { return rows[left].Title < rows[right].Title })
	return rows
}

// exportsRowPages reports whether database rows get their own page files:
// a workspace export always does, a page or database export with subpages,
// as Notion's (seen 2026-11-07: a database alone is its CSV, or its HTML and
// CSV).
func (document *workspaceExportDocument) exportsRowPages() bool {
	return document.Payload.Scope.Kind == workspaceExportScopeWorkspace || document.Payload.Options.IncludeSubpages
}

func (document *workspaceExportDocument) pageContainers(pageID string) []*workspaceExportDocContainer {
	containers := []*workspaceExportDocContainer{}
	// A database page's body shows databases as a page's does.
	for _, blocks := range [][]*workspaceExportDocBlock{document.Blocks[pageID], document.RowBlocks[pageID]} {
		for _, block := range blocks {
			if container := document.ByBlock[block.ID]; container != nil {
				containers = append(containers, container)
			}
		}
	}
	return containers
}
