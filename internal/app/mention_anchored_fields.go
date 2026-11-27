package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A sub-page made from "@" in a field that is no line's words hangs on that
// field, as Notion's does (seen 2026-11-13 through its API: a table cell's
// page has the table's row as its parent, a caption's the block it is the
// caption of, a page title's that page, a database page's text cell's that
// database page, and none has a block in any body). A table row's cells
// and a block's caption are that block's content, so such a page names the
// block as a line's page does (mention_anchored_pages.go), the block's
// content mentioning it. A page's title and a database page's cell are no
// block: the page names "title", or "cell:" and the cell's property, of the
// page it is under. Each follows its field as a line's page follows its
// line: the field's mention gone, it goes to the trash; back, it comes back.
// A mention an integration takes out of a title or a cell leaves its page
// there unmentioned, as of a line (mention_anchored_unmentioned.go; seen
// 2026-11-18 in two tabs of 한지테스트페이지: the page out of the trash, still
// under that page, out of its sidebar until the title mentions it again, and
// left so when a person then types in the title or the cell).

const (
	pageAnchorTitle      = "title"
	pageAnchorCellPrefix = "cell:"
	// pageAnchorMaxLength is what anchor_block_id holds.
	pageAnchorMaxLength = 64
)

// pageAnchorOnField reports an anchor that is a field of the page above —
// its title or a cell — and no block.
func pageAnchorOnField(anchor string) bool {
	return anchor == pageAnchorTitle || strings.HasPrefix(anchor, pageAnchorCellPrefix)
}

// spansMentionPage reports whether spans mention a page (a mention of one
// of its blocks is not one of the page).
func spansMentionPage(spans []graphTextSpan, pageID string) bool {
	for _, span := range spans {
		if span.Mention == "page" && strings.TrimSpace(span.PageID) == pageID && span.BlockID == "" {
			return true
		}
	}
	return false
}

// blockContentMentionsPage reports whether a block's content mentions a
// page: a table row's cells, or its caption.
func blockContentMentionsPage(content, pageID string) bool {
	if !strings.Contains(content, pageID) {
		return false
	}
	fields := map[string]json.RawMessage{}
	if json.Unmarshal([]byte(content), &fields) != nil {
		return false
	}
	caption := []graphTextSpan{}
	if json.Unmarshal(fields["caption"], &caption) == nil && spansMentionPage(caption, pageID) {
		return true
	}
	cells := [][]graphTextSpan{}
	if json.Unmarshal(fields["cells"], &cells) == nil {
		for _, cell := range cells {
			if spansMentionPage(cell, pageID) {
				return true
			}
		}
	}
	return false
}

// captionMentionsPage reports whether a block's caption mentions a page.
func captionMentionsPage(content, pageID string) bool {
	if !strings.Contains(content, pageID) {
		return false
	}
	held := struct {
		Caption json.RawMessage `json:"caption"`
	}{}
	caption := []graphTextSpan{}
	return json.Unmarshal([]byte(content), &held) == nil && json.Unmarshal(held.Caption, &caption) == nil &&
		spansMentionPage(caption, pageID)
}

// fieldMentionsPage reports whether a field of a page — its title, or a
// cell of it as a database page — mentions another page.
func fieldMentionsPage(app core.App, parentPageID, anchor, pageID string) (bool, error) {
	if anchor == pageAnchorTitle {
		parent, err := app.FindRecordById(pagesCollection, parentPageID)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return spansMentionPage(ordinaryPageTitleSpans(parent), pageID), nil
	}
	spans := ""
	err := app.DB().NewQuery(`
		SELECT [[spans]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:row} AND [[property_id]] = {:property}
	`).Bind(dbx.Params{"row": parentPageID, "property": strings.TrimPrefix(anchor, pageAnchorCellPrefix)}).Row(&spans)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return spansMentionPage(databaseStoredSpans(spans), pageID), nil
}

// bodyBlockContents reads the content of those of the named blocks that
// stand in a page's body.
func bodyBlockContents(app core.App, pageID string, ids []string) (map[string]string, error) {
	contents := map[string]string{}
	if len(ids) == 0 {
		return contents, nil
	}
	found := []struct {
		ID      string `db:"id"`
		Content string `db:"content"`
	}{}
	encoded, _ := json.Marshal(ids)
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[content]] FROM {{leafen_blocks}}
		WHERE [[page_id]] = {:page} AND [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"page": pageID, "ids": string(encoded)}).All(&found); err != nil {
		return nil, err
	}
	for _, block := range found {
		contents[block.ID] = block.Content
	}
	return contents, nil
}

// pageAnchorsHold reads which of a page's anchored sub-pages their anchors
// still mention: a block standing in its body, by its words or its content;
// a field, by its spans. live tells the blocks among the anchors that stand.
func pageAnchorsHold(app core.App, parentPageID string, pages []mentionAnchoredPage) (holds, live map[string]bool, err error) {
	blocks := []string{}
	for _, page := range pages {
		if !pageAnchorOnField(page.Anchor) {
			blocks = append(blocks, page.Anchor)
		}
	}
	contents, err := bodyBlockContents(app, parentPageID, blocks)
	if err != nil {
		return nil, nil, err
	}
	var doc *crdt.Doc
	if len(contents) > 0 {
		loaded, found, err := loadYgoDoc(app, parentPageID)
		if err != nil {
			return nil, nil, err
		}
		if found {
			doc = loaded
		}
	}
	holds, live = map[string]bool{}, map[string]bool{}
	for id := range contents {
		live[id] = true
	}
	for _, page := range pages {
		if pageAnchorOnField(page.Anchor) {
			held, err := fieldMentionsPage(app, parentPageID, page.Anchor, page.ID)
			if err != nil {
				return nil, nil, err
			}
			holds[page.ID] = held
			continue
		}
		content, standing := contents[page.Anchor]
		holds[page.ID] = standing && (doc != nil && textMentionsPage(doc.GetText(ygoBlockTextRootPrefix+page.Anchor), page.ID) ||
			blockContentMentionsPage(content, page.ID))
	}
	return holds, live, nil
}

// pageAnchorAdmitted reports whether a page made under a parent may hang on
// the anchor its maker named: a block of the parent's own that holds words,
// cells or a caption (one the server doesn't have yet comes with the
// parent's next change), the parent's title when it keeps its own, or a
// title or text cell of the parent as a database page.
func pageAnchorAdmitted(app core.App, parentPageID, anchor string) (bool, error) {
	switch {
	case len(anchor) > pageAnchorMaxLength:
		return false, nil
	case anchor == pageAnchorTitle:
		parent, err := app.FindRecordById(pagesCollection, parentPageID)
		if err != nil {
			return false, err
		}
		return titledByOwnSpans(parent), nil
	case strings.HasPrefix(anchor, pageAnchorCellPrefix):
		var cells int
		err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_pages}} [[row]]
			JOIN {{leafen_database_properties}} [[property]] ON [[property]].[[data_source_id]] = [[row]].[[data_source_id]]
			WHERE [[row]].[[id]] = {:row} AND [[row]].[[data_source_id]] != ''
				AND [[property]].[[id]] = {:property} AND [[property]].[[type]] IN ('title', 'rich_text')
		`).Bind(dbx.Params{"row": parentPageID, "property": strings.TrimPrefix(anchor, pageAnchorCellPrefix)}).Row(&cells)
		return cells > 0, err
	}
	var holder, blockType string
	err := app.DB().NewQuery(`SELECT [[page_id]], [[type]] FROM {{leafen_blocks}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": anchor}).Row(&holder, &blockType)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return holder == parentPageID && blockType != graphBlockTypeChildPage, nil
}

// pageFieldAnchor names a field of a page that sub-pages may hang on.
type pageFieldAnchor struct {
	pageID, anchor string
}

// settleFieldAnchoredPages keeps the sub-pages hanging on fields with them
// once the fields were written, as a line's pages follow its words; kept,
// an integration wrote them (anchoredPageStepOf).
func settleFieldAnchoredPages(app core.App, actorID, mutationID string, fields []pageFieldAnchor, kept bool) error {
	if actorID == "" || len(fields) == 0 {
		return nil
	}
	if known, err := pageAnchorsKnown(app); err != nil || !known {
		return err
	}
	byPage := map[string][]string{}
	pages := []string{}
	for _, field := range fields {
		if _, seen := byPage[field.pageID]; !seen {
			pages = append(pages, field.pageID)
		}
		if !slices.Contains(byPage[field.pageID], field.anchor) {
			byPage[field.pageID] = append(byPage[field.pageID], field.anchor)
		}
	}
	// Most fields hold none: one read finds the pages that have any.
	encoded, _ := json.Marshal(pages)
	held := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[parent_id]] FROM {{leafen_pages}}
		WHERE [[anchor_block_id]] != '' AND [[parent_id]] IN (SELECT [[value]] FROM json_each({:pages}))
	`).Bind(dbx.Params{"pages": string(encoded)}).Column(&held); err != nil {
		return err
	}
	for _, pageID := range held {
		if _, err := settleAnchoredPages(app, actorID, pageID, mutationID, anchorChange{blocks: byPage[pageID], kept: kept}); err != nil {
			return err
		}
	}
	return nil
}

// settleTitleAnchoredPages keeps the sub-pages hanging on a page's title
// with it once its title or the title's spans were written.
func settleTitleAnchoredPages(app core.App, actorID string, before map[string]any, page *core.Record) error {
	if actorID == "" || anyString(before["title_spans"]) == page.GetString("title_spans") &&
		anyString(before["title"]) == page.GetString("title") {
		return nil
	}
	return settleFieldAnchoredPages(app, actorID, "title-anchor:"+newDiagnosticReference(),
		[]pageFieldAnchor{{pageID: page.Id, anchor: pageAnchorTitle}}, fieldAnchorsKept(app))
}

// keptFieldWrites holds, by open transaction, field writes that leave a page
// whose mention they take out hanging there unmentioned without being an
// integration's: a migration run again writing a title as Notion has it now
// (renameNotionMigrationPage), where an integration may have taken it out.
var keptFieldWrites sync.Map

// keepingFieldAnchors marks txApp's field writes so until the returned
// release.
func keepingFieldAnchors(txApp core.App) func() {
	if _, held := keptFieldWrites.LoadOrStore(txApp, true); held {
		return func() {}
	}
	return func() { keptFieldWrites.Delete(txApp) }
}

// fieldAnchorsKept reports whether the fields a transaction writes leave the
// pages whose mention they take out hanging unmentioned: an integration
// writes them through the Public API — a Notion-compatible connection or a
// personal access token, as the transaction names it for the page's history
// (page_history_connections.go) — or keepingFieldAnchors marked it. A person
// writes them in the app, and an agent through MCP as its person.
func fieldAnchorsKept(app core.App) bool {
	if _, held := keptFieldWrites.Load(app); held {
		return true
	}
	return pageHistoryConnectionKind(pageHistoryConnectionOf(app).ID) == "integration"
}

func anyString(value any) string {
	text, _ := value.(string)
	return text
}

// rowCellFieldAnchors names the cells a change's rows wrote, which pages may
// hang on.
func rowCellFieldAnchors(operations []graphOperation) []pageFieldAnchor {
	fields := []pageFieldAnchor{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		for propertyID := range operation.Row.Values {
			fields = append(fields, pageFieldAnchor{pageID: operation.Row.ID, anchor: pageAnchorCellPrefix + propertyID})
		}
	}
	return fields
}

// exportLinkedAnchors is, of the anchored pages an export holds, those whose
// mention the export draws as a link to them: one in a line's words or a
// table row's cells. A caption's, a title's or a cell's is not drawn so, and
// its page is listed in its parent's body as any other.
func exportLinkedAnchors(app core.App, ids []string) ([]string, error) {
	encoded, _ := json.Marshal(ids)
	pages := []struct {
		ID      string `db:"id"`
		Type    string `db:"type"`
		Content string `db:"content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page]].[[id]], [[line]].[[type]], [[line]].[[content]] FROM {{leafen_pages}} [[page]]
		JOIN {{leafen_blocks}} [[line]] ON [[line]].[[id]] = [[page]].[[anchor_block_id]]
			AND [[line]].[[page_id]] = [[page]].[[parent_id]]
		WHERE [[page]].[[anchor_block_id]] != '' AND [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&pages); err != nil {
		return nil, err
	}
	linked := []string{}
	for _, page := range pages {
		_, words := graphTextBlockTypes[page.Type]
		if (words || page.Type == graphBlockTypeTableRow) && !captionMentionsPage(page.Content, page.ID) {
			linked = append(linked, page.ID)
		}
	}
	return linked, nil
}

// pageDuplicationAnchor is what a copied sub-page's anchor names: the copy
// of its line, the copy's title, or the copy's cell of the property a copied
// database gave a new ID (its own when the copy is in its database); none
// for a line not copied.
func pageDuplicationAnchor(app core.App, jobID, anchor string) (string, error) {
	kind, id := "block", anchor
	switch {
	case anchor == pageAnchorTitle:
		return anchor, nil
	case strings.HasPrefix(anchor, pageAnchorCellPrefix):
		kind, id = "property", strings.TrimPrefix(anchor, pageAnchorCellPrefix)
	}
	mapped, err := pageDuplicationReferenceMappings(app, jobID, kind, []string{id})
	if err != nil || kind == "block" {
		return mapped[id], err
	}
	if property := mapped[id]; property != "" {
		return pageAnchorCellPrefix + property, nil
	}
	return anchor, nil
}

// workspaceImportArchiveAnchor is what an archived sub-page's anchor names
// once imported: the imported line, the imported parent's title, or its cell
// of the imported property; none for a line or property the archive lacks.
func workspaceImportArchiveAnchor(db workspaceImportArchiveQueryer, anchor string) string {
	kind, id := workspaceExportItemBlock, anchor
	switch {
	case anchor == pageAnchorTitle:
		return anchor
	case strings.HasPrefix(anchor, pageAnchorCellPrefix):
		kind, id = workspaceExportItemProperty, strings.TrimPrefix(anchor, pageAnchorCellPrefix)
	}
	imported, err := workspaceImportArchiveReference(db, id, kind)
	switch {
	case err != nil:
		return ""
	case kind == workspaceExportItemProperty:
		return pageAnchorCellPrefix + imported
	}
	return imported
}

// loadFieldAnchoredPageSet says which of the pages hang on a table row's
// cells or a block's caption, which Notion's sidebar leaves out though it
// lists the pages on a line or a title (seen 2026-11-13); search finds them
// all as their parent's sub-pages. A page hangs on a block's words only
// where its block holds words that take a mention, which a code block's
// don't: on any other block it hangs on its cells or its caption.
func loadFieldAnchoredPageSet(app core.App, pageIDs []string) (map[string]bool, error) {
	result := map[string]bool{}
	if len(pageIDs) == 0 {
		return result, nil
	}
	if known, err := pageAnchorsKnown(app); err != nil || !known {
		return result, err
	}
	encoded, _ := json.Marshal(pageIDs)
	anchored := []struct {
		ID   string `db:"id"`
		Type string `db:"type"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page]].[[id]], [[block]].[[type]] FROM {{leafen_pages}} [[page]]
		JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[page]].[[anchor_block_id]]
			AND [[block]].[[page_id]] = [[page]].[[parent_id]]
		WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[page]].[[anchor_block_id]] != ''
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&anchored); err != nil {
		return nil, err
	}
	for _, page := range anchored {
		if _, words := graphTextBlockTypes[page.Type]; !words || page.Type == graphBlockTypeCode {
			result[page.ID] = true
		}
	}
	return result, nil
}

// isFieldAnchoredPage says whether one page hangs on a table row's cells
// or a block's caption (loadFieldAnchoredPageSet).
func isFieldAnchoredPage(app core.App, record *core.Record) (bool, error) {
	if anchor := record.GetString(pageAnchorBlockField); anchor == "" || pageAnchorOnField(anchor) {
		return false, nil
	}
	set, err := loadFieldAnchoredPageSet(app, []string{record.Id})
	return set[record.Id], err
}
