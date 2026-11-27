package app

import (
	"slices"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The readable export formats (Markdown & CSV, HTML and printable HTML) turn
// the captured inventory into one document model: pages with their block
// trees, databases with their rows and row pages, comments and files. Block
// text stays in each page's Ygo document and is decoded one page at a time
// while the artifact is written.

type workspaceExportDocPage struct {
	ID           string
	ParentID     string
	Title        string
	Icon         string
	IconType     string
	Cover        workspaceExportCover
	Position     string
	BodyPosition string
	Trashed      bool
	// Listed marks a subpage the export names without exporting it.
	Listed bool
	// Anchored marks a sub-page hanging on a line, which its line's
	// mention shows (mention_anchored_pages.go).
	Anchored bool
	Children []*workspaceExportDocPage
	Path     string
	Folder   string
	// Kind is the page's kind: a full-page database's ("database") is its
	// database, which Database names once its files are placed
	// (workspaceExportDocument.fullPageSource).
	Kind     string
	Database *workspaceExportDocDataSource
}

type workspaceExportDocBlock struct {
	ID           string
	PageID       string
	ParentID     string
	Position     string
	BodyPosition string
	Type         string
	Content      map[string]any
	Runs         []workspaceExportTextRun
}

type workspaceExportDocContainer struct {
	ID            string
	PageID        string
	BlockID       string
	Kind          string
	Position      string
	DataSourceIDs []string
	ViewIDs       []string
	// CSVPath is a linked database's own CSV in a Markdown & CSV export
	// (workspace_export_document_linked.go).
	CSVPath string
}

type workspaceExportDocDataSource struct {
	ID       string
	PageID   string
	Title    string
	Icon     string
	IconType string
	// Cover is the database's own, which shows over it opened alone; a
	// full-page database's is its page's (data_source_cover.go).
	Cover   workspaceExportCover
	Folder  string
	CSVPath string
	// External marks a source outside the export that a linked database on
	// an exported page shows; Container is that database.
	External  bool
	Container *workspaceExportDocContainer
	// FullPage marks a full-page database's own, written in its page's
	// place (fullPageSource).
	FullPage bool
	// AllCSVPath is where its "_all" file stands when not beside its CSV
	// (workspace_export_linked_view.go).
	AllCSVPath string
}

type workspaceExportDocProperty struct {
	ID           string
	DataSourceID string
	Type         string
	Name         string
	Position     string
	Config       map[string]any
}

type workspaceExportDocView struct {
	ID           string
	DataSourceID string
	ContainerID  string
	Type         string
	Name         string
	Position     string
	Config       map[string]any
}

type workspaceExportDocRow struct {
	ID           string
	DataSourceID string
	Position     string
	Icon         string
	IconType     string
	// Cover is the row page's cover (workspace_export_document_html_covers.go).
	Cover  workspaceExportCover
	Values map[string]any
	Title  string
	Path   string
	// Folder holds the row page's own files, with folders (assignPaths).
	Folder   string
	Archived bool
	// Children are the row page's sub-pages.
	Children []*workspaceExportDocPage
	// Created and Updated are the row page's times in milliseconds.
	Created      int64
	Updated      int64
	CreatedBy    string
	LastEditedBy string
	// Hidden names the values the principal may not see
	// (workspace_export_property_access.go).
	Hidden map[string]bool
}

type workspaceExportDocComment struct {
	ID        string
	PageID    string
	BlockID   string
	ParentID  string
	AuthorID  string
	Body      any
	Resolved  bool
	CreatedAt string
}

type workspaceExportDocument struct {
	Payload     workspaceExportJobPayload
	Pages       map[string]*workspaceExportDocPage
	Roots       []*workspaceExportDocPage
	Blocks      map[string][]*workspaceExportDocBlock
	BlockByID   map[string]*workspaceExportDocBlock
	Containers  map[string]*workspaceExportDocContainer
	ByBlock     map[string]*workspaceExportDocContainer
	DataSources map[string]*workspaceExportDocDataSource
	Properties  map[string][]*workspaceExportDocProperty
	Views       map[string]*workspaceExportDocView
	Rows        map[string][]*workspaceExportDocRow
	RowByID     map[string]*workspaceExportDocRow
	RowBlocks   map[string][]*workspaceExportDocBlock
	Comments    map[string][]*workspaceExportDocComment
	Files       map[string]workspaceExportFileSnapshot
	People      map[string]string
	// WorkspaceID is the workspace exported, whose connections' bots its
	// person values may name.
	WorkspaceID string
	// SyncedSources are pages holding an original synced block that another
	// page copies; their text is decoded before any page is rendered.
	SyncedSources map[string]bool
	// Language is the one its dates are written in (workspaceExportLanguage).
	Language string
	// TimeZone and Location are the exporter's time zone, whose clock its
	// dates are written on (workspace_export_document_dates.go).
	TimeZone string
	Location *time.Location
	// Origin is this server's address, which a page the export leaves out
	// is linked at (workspaceExportServerOrigin); "" when it knows none.
	Origin string
	// folders are the folders assignPaths gave out.
	folders workspaceExportFolderPlacer
	// SourceFolder is the folder of the pages holding a linked view's source
	// (workspaceExportSourceFolder).
	SourceFolder string
	// Principal is the person exporting, whom "me" filters of views mean.
	Principal  string
	ViewTables map[string]workspaceExportViewTable
	QueryRows  map[string]storedDatabaseQueryRow
	// Computed holds each row's formula and rollup values by property, for
	// the databases in ComputedSources.
	Computed        map[string]map[string]databaseComputedValue
	ComputedSources map[string]bool
	// Access applies the databases' property access rules for Principal.
	Access *databasePropertyAccessReader
	// closes is Principal's closer: a page or database mention closed to
	// them goes in no-access marks, said "사용 권한 없음" in the text and
	// "@찾을 수 없음" in a database page's title, a page link to one
	// "사용 권한 없음", and a sub-page closed to them not at all, as the
	// app reads them (content_graph_reader_mentions.go).
	closes func() searchMentionCloser
	// names names the pages and people the text mentions now
	// (workspace_export_document_mention_names.go).
	names *workspaceExportMentionNames
}

func mapString(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return text
}

func mapStrings(value map[string]any, key string) []string {
	items, _ := value[key].([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && text != "" {
			result = append(result, text)
		}
	}
	return result
}

func collectWorkspaceExportDocument(
	app core.App,
	job storedJob,
	payload workspaceExportJobPayload,
) (*workspaceExportDocument, error) {
	language := workspaceExportLanguage(app, job, payload)
	zone, location := workspaceExportTimeZone(loadAccountTimeZone(app, job.Owner))
	document := &workspaceExportDocument{
		Payload: payload, Pages: map[string]*workspaceExportDocPage{},
		Blocks: map[string][]*workspaceExportDocBlock{}, BlockByID: map[string]*workspaceExportDocBlock{},
		Containers: map[string]*workspaceExportDocContainer{}, ByBlock: map[string]*workspaceExportDocContainer{},
		DataSources: map[string]*workspaceExportDocDataSource{}, Properties: map[string][]*workspaceExportDocProperty{},
		Views: map[string]*workspaceExportDocView{}, Rows: map[string][]*workspaceExportDocRow{},
		RowByID: map[string]*workspaceExportDocRow{}, RowBlocks: map[string][]*workspaceExportDocBlock{},
		Comments: map[string][]*workspaceExportDocComment{}, Files: map[string]workspaceExportFileSnapshot{},
		People: map[string]string{}, SyncedSources: map[string]bool{},
		Principal: job.Owner, ViewTables: map[string]workspaceExportViewTable{}, WorkspaceID: job.WorkspaceID,
		Language: language, TimeZone: zone, Location: location,
		Origin: workspaceExportServerOrigin(app),
		closes: readerCloser(app, job.Owner),
		names:  newWorkspaceExportMentionNames(app, job, language),
	}
	err := walkWorkspaceExportChunks(app, job, func(chunk workspaceExportInventoryChunk) error {
		for _, file := range chunk.Files {
			document.Files[file.RecordID] = file
		}
		for _, item := range chunk.Items {
			document.add(item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// A copy an API or import wrote before 2026-10-31 names only its
	// original; the original's own block tells its page.
	for pageID, blocks := range document.Blocks {
		for _, block := range blocks {
			if mapString(block.Content, "syncedPageId") != "" {
				continue
			}
			if original := document.BlockByID[mapString(block.Content, "syncedBlockId")]; original != nil && original.PageID != pageID {
				document.SyncedSources[original.PageID] = true
			}
		}
	}
	if err := document.withoutHiddenRows(app); err != nil {
		return nil, err
	}
	document.link()
	if err := document.loadListedSubpages(app, job); err != nil {
		return nil, err
	}
	if err := document.markAnchoredPages(app); err != nil {
		return nil, err
	}
	document.namesAsRead(app)
	if err := document.loadRowPages(app); err != nil {
		return nil, err
	}
	if err := document.loadPeople(app); err != nil {
		return nil, err
	}
	if err := document.applyPropertyAccess(app); err != nil {
		return nil, err
	}
	return document, nil
}

func (document *workspaceExportDocument) add(item workspaceExportInventoryItem) {
	value := item.Value
	switch item.Kind {
	case workspaceExportItemPage:
		trashed, _ := value["inTrash"].(bool)
		// An ordinary page's title as its exporter reads it, from its spans
		// (namesAsRead says it in words).
		graphNameAsRead(value, "title", "titleSpans", document.closes)
		page := &workspaceExportDocPage{
			ID: mapString(value, "id"), ParentID: mapString(value, "parentId"), Title: mapString(value, "title"),
			Icon: mapString(value, "icon"), IconType: mapString(value, "iconType"), Cover: workspaceExportCoverOf(value),
			Position: mapString(value, "position"), BodyPosition: mapString(value, "bodyPosition"), Trashed: trashed,
			Kind: mapString(value, "kind"),
		}
		document.Pages[page.ID] = page
	case workspaceExportItemBlock, workspaceExportItemRowPageBlock:
		block := &workspaceExportDocBlock{
			ID: mapString(value, "id"), PageID: mapString(value, "pageId"), ParentID: mapString(value, "parentId"),
			Position: mapString(value, "position"), BodyPosition: mapString(value, "bodyPosition"),
			Type: mapString(value, "type"), Content: map[string]any{},
		}
		if content, ok := value["content"].(map[string]any); ok {
			block.Content = content
		}
		// A document shows the page as it stands, without suggested blocks.
		if mapString(block.Content, graphBlockSuggestionKey) != "" {
			return
		}
		graphBlockAsRead(map[string]any{"type": block.Type, "pageId": block.PageID, "content": block.Content}, document.closes)
		// Imported and row page blocks carry their text in the block content;
		// a page's Ygo document replaces it when the text is decoded.
		block.Runs = workspaceExportRowBlockRuns(block.Content, document.Language)
		if item.Kind == workspaceExportItemRowPageBlock {
			rowID := mapString(value, "rowId")
			document.RowBlocks[rowID] = append(document.RowBlocks[rowID], block)
			return
		}
		if syncedPage := mapString(block.Content, "syncedPageId"); syncedPage != "" && syncedPage != block.PageID {
			document.SyncedSources[syncedPage] = true
		}
		document.Blocks[block.PageID] = append(document.Blocks[block.PageID], block)
		document.BlockByID[block.ID] = block
	case workspaceExportItemDatabaseContainer:
		container := &workspaceExportDocContainer{
			ID: mapString(value, "id"), PageID: mapString(value, "pageId"), BlockID: mapString(value, "blockId"),
			Kind: mapString(value, "kind"), Position: mapString(value, "position"),
			DataSourceIDs: mapStrings(value, "dataSourceIds"), ViewIDs: mapStrings(value, "viewIds"),
		}
		document.Containers[container.ID] = container
		if container.BlockID != "" {
			document.ByBlock[container.BlockID] = container
		}
	case workspaceExportItemDataSource:
		external, _ := value["external"].(bool)
		graphNameAsRead(value, "title", "titleSpans", document.closes)
		source := &workspaceExportDocDataSource{
			ID: mapString(value, "id"), PageID: mapString(value, "pageId"), Title: mapString(value, "title"),
			Icon: mapString(value, "icon"), IconType: mapString(value, "iconType"), Cover: workspaceExportCoverOf(value),
			External: external,
		}
		document.DataSources[source.ID] = source
	case workspaceExportItemProperty:
		// A document export leaves out Deleted properties.
		if mapString(value, "deletedAt") != "" {
			break
		}
		property := &workspaceExportDocProperty{
			ID: mapString(value, "id"), DataSourceID: mapString(value, "dataSourceId"), Type: mapString(value, "type"),
			Name: mapString(value, "name"), Position: mapString(value, "position"), Config: map[string]any{},
		}
		if config, ok := value["config"].(map[string]any); ok {
			property.Config = config
		}
		document.Properties[property.DataSourceID] = append(document.Properties[property.DataSourceID], property)
	case workspaceExportItemView:
		view := &workspaceExportDocView{
			ID: mapString(value, "id"), DataSourceID: mapString(value, "dataSourceId"),
			ContainerID: mapString(value, "containerId"), Type: mapString(value, "type"),
			Name: mapString(value, "name"), Position: mapString(value, "position"), Config: map[string]any{},
		}
		if config, ok := value["config"].(map[string]any); ok {
			view.Config = config
		}
		document.Views[view.ID] = view
	case workspaceExportItemRow:
		// A document export leaves out rows in trash, as Notion's does.
		if mapString(value, "trashedAt") != "" {
			break
		}
		row := &workspaceExportDocRow{
			ID: mapString(value, "id"), DataSourceID: mapString(value, "dataSourceId"),
			Position: mapString(value, "position"), Icon: mapString(value, "icon"), IconType: mapString(value, "iconType"),
			Cover: workspaceExportCoverOf(value), Values: map[string]any{}, Archived: mapString(value, "archivedAt") != "",
		}
		if values, ok := value["values"].(map[string]any); ok {
			row.Values = values
		}
		for _, raw := range row.Values {
			if wrapper, ok := raw.(map[string]any); ok {
				graphCellAsRead(wrapper, document.closes)
			}
		}
		document.Rows[row.DataSourceID] = append(document.Rows[row.DataSourceID], row)
		document.RowByID[row.ID] = row
	case workspaceExportItemComment:
		resolved, _ := value["resolved"].(bool)
		comment := &workspaceExportDocComment{
			ID: mapString(value, "id"), PageID: mapString(value, "pageId"), BlockID: mapString(value, "blockId"),
			ParentID: mapString(value, "parentId"), AuthorID: mapString(value, "authorId"), Body: value["body"],
			Resolved: resolved, CreatedAt: mapString(value, "createdAt"),
		}
		if body, ok := comment.Body.(map[string]any); ok {
			if spans, ok := body["rich"].([]any); ok {
				graphSpansAsRead(spans, document.closes)
			}
		}
		document.Comments[comment.PageID] = append(document.Comments[comment.PageID], comment)
	}
}

func workspaceExportPositionLess(leftPosition, leftID, rightPosition, rightID string) bool {
	if leftPosition != rightPosition {
		return leftPosition < rightPosition
	}
	return leftID < rightID
}

func (document *workspaceExportDocument) link() {
	for _, blocks := range document.Blocks {
		sortWorkspaceExportBlocks(blocks)
		// A source its database keeps in the trash where it stands is left
		// out with its rows, as a source in the trash and rows in the trash
		// are (database_source_trash_in_place.go).
		for _, block := range blocks {
			container := document.ByBlock[block.ID]
			if block.Type != graphBlockTypeInlineDatabase || container == nil {
				continue
			}
			for _, trashed := range storedDatabaseTrashedSources(block.Content) {
				if slices.Contains(container.DataSourceIDs, trashed) {
					container.DataSourceIDs = slices.DeleteFunc(container.DataSourceIDs, func(id string) bool { return id == trashed })
					delete(document.Rows, trashed)
				}
			}
		}
	}
	for _, blocks := range document.RowBlocks {
		sortWorkspaceExportBlocks(blocks)
	}
	for _, properties := range document.Properties {
		sort.Slice(properties, func(left, right int) bool {
			return workspaceExportPositionLess(properties[left].Position, properties[left].ID,
				properties[right].Position, properties[right].ID)
		})
	}
	for dataSourceID, rows := range document.Rows {
		sort.Slice(rows, func(left, right int) bool {
			return workspaceExportPositionLess(rows[left].Position, rows[left].ID, rows[right].Position, rows[right].ID)
		})
		titleID := ""
		for _, property := range document.Properties[dataSourceID] {
			if property.Type == graphValueTypeTitle {
				titleID = property.ID
				break
			}
		}
		for _, row := range rows {
			if wrapper, ok := row.Values[titleID].(map[string]any); ok {
				row.Title, _ = wrapper["value"].(string)
				row.Title = closedMentionsInTitle(row.Title, document.Language)
			}
		}
	}
	for _, comments := range document.Comments {
		sort.Slice(comments, func(left, right int) bool {
			return workspaceExportPositionLess(comments[left].CreatedAt, comments[left].ID,
				comments[right].CreatedAt, comments[right].ID)
		})
	}
	// A page under one in the trash is in the trash with it, as the trash
	// keeps a page with its sub-pages: a document leaves both out, rather
	// than showing the sub-pages as top pages of their own.
	underTrash := []*workspaceExportDocPage{}
	for _, page := range document.Pages {
		parent := document.Pages[page.ParentID]
		for steps := 0; parent != nil && parent != page && steps < len(document.Pages); steps++ {
			if parent.Trashed {
				underTrash = append(underTrash, page)
				break
			}
			parent = document.Pages[parent.ParentID]
		}
	}
	for _, page := range underTrash {
		page.Trashed = true
	}
	ids := make([]string, 0, len(document.Pages))
	for id, page := range document.Pages {
		if !page.Trashed {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		page := document.Pages[id]
		parent, nested := document.Pages[page.ParentID]
		if nested && !parent.Trashed && page.ParentID != page.ID {
			parent.Children = append(parent.Children, page)
			continue
		}
		// A database page's sub-page goes with the database page.
		if row := document.RowByID[page.ParentID]; row != nil {
			row.Children = append(row.Children, page)
			continue
		}
		if document.Payload.Scope.Kind == workspaceExportScopeWorkspace || page.ID == document.Payload.Scope.TargetID {
			document.Roots = append(document.Roots, page)
		}
	}
	sortChildren := func(children []*workspaceExportDocPage) {
		sort.Slice(children, func(left, right int) bool {
			if children[left].BodyPosition != children[right].BodyPosition {
				return children[left].BodyPosition < children[right].BodyPosition
			}
			return workspaceExportPositionLess(children[left].Position, children[left].ID,
				children[right].Position, children[right].ID)
		})
	}
	for _, page := range document.Pages {
		sortChildren(page.Children)
	}
	for _, row := range document.RowByID {
		sortChildren(row.Children)
	}
}

// rowSubPageLinks are links to a database page's sub-pages that its body's
// page blocks don't show, as a page's body lists its sub-pages.
func (document *workspaceExportDocument) rowSubPageLinks(row *workspaceExportDocRow) []*workspaceExportDocBlock {
	shown := map[string]bool{}
	for _, block := range document.RowBlocks[row.ID] {
		if block.Type == graphBlockTypeChildPage {
			shown[mapString(block.Content, "childPageId")] = true
		}
	}
	links := []*workspaceExportDocBlock{}
	for _, child := range row.Children {
		if !shown[child.ID] && !child.Anchored {
			links = append(links, &workspaceExportDocBlock{
				ID: "page-link-" + child.ID, PageID: row.ID, Type: graphBlockTypeLinkToPage,
				Content: map[string]any{"childPageId": child.ID},
			})
		}
	}
	return links
}

func sortWorkspaceExportBlocks(blocks []*workspaceExportDocBlock) {
	sort.Slice(blocks, func(left, right int) bool {
		return workspaceExportPositionLess(blocks[left].Position, blocks[left].ID, blocks[right].Position, blocks[right].ID)
	})
}

// bodyItems returns a page body's direct items in reading order: top-level
// blocks and subpages share the page body order.
func (document *workspaceExportDocument) bodyItems(page *workspaceExportDocPage) []workspaceExportBodyItem {
	items := []workspaceExportBodyItem{}
	// A sub-page shown by a page block, inside another block too, is not
	// listed again.
	shown := map[string]bool{}
	for _, block := range document.Blocks[page.ID] {
		if block.Type == graphBlockTypeChildPage {
			shown[mapString(block.Content, "childPageId")] = true
		}
		if block.ParentID == "" {
			items = append(items, workspaceExportBodyItem{Order: block.BodyPosition, Block: block})
		}
	}
	for _, child := range page.Children {
		if !shown[child.ID] && !child.Anchored {
			items = append(items, workspaceExportBodyItem{Order: child.BodyPosition, Page: child})
		}
	}
	sort.SliceStable(items, func(left, right int) bool {
		leftOrder, rightOrder := items[left].Order, items[right].Order
		if leftOrder == "" || rightOrder == "" {
			return leftOrder != "" && rightOrder == ""
		}
		return leftOrder < rightOrder
	})
	return items
}

type workspaceExportBodyItem struct {
	Order string
	Block *workspaceExportDocBlock
	Page  *workspaceExportDocPage
}

func (document *workspaceExportDocument) childBlocks(pageID, parentID string) []*workspaceExportDocBlock {
	children := []*workspaceExportDocBlock{}
	for _, block := range document.Blocks[pageID] {
		if block.ParentID == parentID {
			children = append(children, block)
		}
	}
	return children
}

// loadListedSubpages names the direct subpages of a page, or a database page,
// exported without its subpages, so its body still shows where they sit.
func (document *workspaceExportDocument) loadListedSubpages(app core.App, job storedJob) error {
	payload := document.Payload
	if payload.Scope.Kind != workspaceExportScopePage || payload.Options.IncludeSubpages {
		return nil
	}
	rootID := payload.Scope.TargetID
	root, row := document.Pages[rootID], document.RowByID[rootID]
	if root == nil && row == nil {
		return nil
	}
	type listedSubpage struct {
		ID       string `db:"id"`
		Title    string `db:"title"`
		Icon     string `db:"icon"`
		IconType string `db:"icon_type"`
		Position string `db:"position"`
		Created  string `db:"created"`
	}
	// Every direct subpage, a thousand at a time in the order they were made.
	rows := []listedSubpage{}
	for afterCreated, afterID := "", ""; ; {
		page := []listedSubpage{}
		err := app.DB().NewQuery(`
			SELECT [[pages]].[[id]], [[pages]].[[title]], [[pages]].[[icon]], [[pages]].[[icon_type]], [[pages]].[[position]], [[pages]].[[created]]
			FROM {{leafen_pages}} AS [[pages]]
			WHERE [[pages]].[[workspace]] = {:workspaceId} AND [[pages]].[[owner]] = {:ownerId}
				AND [[pages]].[[parent_id]] = {:parentId} AND [[pages]].[[notion_import_staging]] = FALSE
				AND [[pages]].[[in_trash]] = FALSE
				AND ({:afterId} = '' OR [[pages]].[[created]] > {:afterCreated}
					OR ([[pages]].[[created]] = {:afterCreated} AND [[pages]].[[id]] > {:afterId}))
			ORDER BY [[pages]].[[created]], [[pages]].[[id]] LIMIT 1000
		`).Bind(dbx.Params{
			"workspaceId": job.WorkspaceID, "ownerId": payload.AuthorityOwnerID, "parentId": rootID,
			"afterCreated": afterCreated, "afterId": afterID,
		}).All(&page)
		if err != nil {
			return err
		}
		rows = append(rows, page...)
		if len(page) < 1000 {
			break
		}
		afterCreated, afterID = page[len(page)-1].Created, page[len(page)-1].ID
	}
	var children []*workspaceExportDocPage
	if root != nil {
		children = root.Children
	} else {
		children = row.Children
	}
	for _, listed := range rows {
		if document.Pages[listed.ID] != nil {
			continue
		}
		child := &workspaceExportDocPage{
			ID: listed.ID, ParentID: rootID, Title: listed.Title, Icon: listed.Icon, IconType: listed.IconType,
			Position: listed.Position, Listed: true,
		}
		document.Pages[child.ID] = child
		children = append(children, child)
	}
	if root != nil {
		root.Children = children
	} else {
		row.Children = children
	}
	sort.SliceStable(children, func(left, right int) bool {
		if children[left].BodyPosition != children[right].BodyPosition {
			return children[left].BodyPosition < children[right].BodyPosition
		}
		return workspaceExportPositionLess(children[left].Position, children[left].ID,
			children[right].Position, children[right].ID)
	})
	return nil
}
