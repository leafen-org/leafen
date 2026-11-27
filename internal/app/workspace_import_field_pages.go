package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page made from "@" in a line, a table cell, a caption, a page's title or
// a database page's cell has no block in its parent's body, in Notion and
// here (mention_anchored_pages.go, mention_anchored_fields.go). An export
// brought back keeps such a page in its parent's folder and names it only
// by that field's mention (seen 2026-11-13: Notion's HTML export links it
// from the title, the line, the table cell and the caption; its Markdown
// export from the line and the cell, the title naming it "@" and its name),
// where a sub-page with a block of its own stands alone on a line. Brought
// back, it hangs on that field as it did, with no block: the document's
// text names it inline as its own sub-page (leafen-subpage) and nowhere
// alone, and the field it is named in is its anchor; a title heading so
// naming its sub-pages is the page's title. The page made before its
// parent's text comes in loses the block its making gave it
// (hangWorkspaceImportSubPages); one made after hangs on the field it finds
// (workspaceImportBatchFieldAnchor).

// workspaceImportStagedTitle is an imported page's title as its document's
// first heading gives it, with its sub-pages' mentions.
type workspaceImportStagedTitle struct {
	Text  string          `json:"text"`
	Spans []graphTextSpan `json:"spans"`
}

// workspaceImportPlacedSubPagePattern is a line holding only a link to the
// page's own sub-page, its place (workspaceImportMarkdownReader.subPage).
var workspaceImportPlacedSubPagePattern = regexp.MustCompile(
	`(?m)^[ \t]*\[(?:\\.|[^\]\\\n])*\]\(\s*` + workspaceImportSubPageReference + `([A-Za-z0-9_-]{1,64})\s*\)[ \t]*$`)

// workspaceImportFieldPages finds, as a document's blocks are read, the
// fields its own sub-pages hang on.
type workspaceImportFieldPages struct {
	// title is the page's own, as its file names it; "" for none.
	title string
	// placed are the sub-pages a line of the document places; inline are
	// those its words name inline, and hung those given a field.
	placed, inline, hung map[string]bool
}

func newWorkspaceImportFieldPages(markdown, title string) *workspaceImportFieldPages {
	fields := &workspaceImportFieldPages{
		title: strings.TrimSpace(title), placed: map[string]bool{}, inline: map[string]bool{}, hung: map[string]bool{},
	}
	for _, match := range workspaceImportPlacedSubPagePattern.FindAllStringSubmatch(markdown, -1) {
		fields.placed[match[1]] = true
	}
	return fields
}

// anchors are the sub-pages a block's words, a table row's cells or a
// block's caption name as its field's: those the document names inline and
// nowhere alone, each on the first field naming it.
func (fields *workspaceImportFieldPages) anchors(block workspaceImportParsedBlock) []string {
	if len(fields.inline) == 0 {
		return nil
	}
	spans := slices.Clone(block.Spans)
	if block.Type == graphBlockTypeTableRow {
		row := graphTableRowBlockContent{}
		if json.Unmarshal(block.Content, &row) == nil {
			for _, cell := range row.Cells {
				spans = append(spans, cell...)
			}
		}
	} else if workspaceImportCaptionedKinds[block.Type] {
		captioned := struct {
			Caption []graphTextSpan `json:"caption"`
		}{}
		if json.Unmarshal(block.Content, &captioned) == nil {
			spans = append(spans, captioned.Caption...)
		}
	}
	found := []string(nil)
	for _, span := range spans {
		pageID := strings.TrimSpace(span.PageID)
		if span.Mention != "page" || span.BlockID != "" || !fields.inline[pageID] || fields.placed[pageID] || fields.hung[pageID] {
			continue
		}
		fields.hung[pageID] = true
		found = append(found, pageID)
	}
	return found
}

// titleHeading reads the document's first block as the page's title: a
// first-level heading with page or data source mentions that spells its
// title as an export names its file, each mention "@" and the name
// (Notion's), the page's own sub-pages it names hanging on the title.
func (fields *workspaceImportFieldPages) titleHeading(block workspaceImportParsedBlock) (workspaceImportStagedItem, bool) {
	if fields.title == "" || block.Type != graphBlockTypeHeading1 || !slices.ContainsFunc(block.Spans, workspaceImportNamedMention) ||
		strings.TrimSpace(workspaceImportExportedName(block.Spans)) != fields.title {
		return workspaceImportStagedItem{}, false
	}
	return workspaceImportStagedItem{
		Title: &workspaceImportStagedTitle{Text: block.Text, Spans: block.Spans}, Anchors: fields.anchors(block),
	}, true
}

// workspaceImportExportedName is a title as an export names its file, each
// page or data source mention "@" and the name.
func workspaceImportExportedName(spans []graphTextSpan) string {
	named := strings.Builder{}
	for _, span := range spans {
		if workspaceImportNamedMention(span) {
			named.WriteString("@")
		}
		named.WriteString(span.Text)
	}
	return named.String()
}

// workspaceImportNamedMention is a mention an export names "@" and the name:
// a page's, or a data source's (workspace_import_data_source_references.go).
func workspaceImportNamedMention(span graphTextSpan) bool {
	_, source := notionMarkdownDataSource(span)
	return span.Mention == "page" || source
}

// workspaceImportItemsHangPages reports whether a turn's items give their
// page its title, icon or cover or hang its sub-pages on fields.
func workspaceImportItemsHangPages(items []workspaceImportStagedItem) bool {
	for _, item := range items {
		if item.Title != nil || item.Head != nil || len(item.Anchors) > 0 {
			return true
		}
	}
	return false
}

// hangWorkspaceImportSubPages gives a page the title its document's heading
// gave it and the icon and cover its first line named
// (workspace_import_page_head.go), and hangs the sub-pages its document's
// fields name there, once the text is in: a sub-page already made gives up
// its block for the field.
func hangWorkspaceImportSubPages(app core.App, actorID, pageID string, items []workspaceImportStagedItem) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Head != nil {
			if page, err = headWorkspaceImportPage(app, actorID, page, *item.Head); err != nil {
				return err
			}
			continue
		}
		anchor := item.ID
		if item.Title != nil {
			anchor = pageAnchorTitle
			titled, err := titleWorkspaceImportPage(app, actorID, page, *item.Title)
			if err != nil {
				return err
			}
			if !titled {
				continue
			}
		}
		for _, subPageID := range item.Anchors {
			if err := hangWorkspaceImportSubPage(app, actorID, page, subPageID, anchor); err != nil {
				return err
			}
		}
	}
	return nil
}

// titleWorkspaceImportPage writes the title heading's words and mentions as
// the page's title, while it still has the title its file gave it.
func titleWorkspaceImportPage(app core.App, actorID string, page *core.Record, title workspaceImportStagedTitle) (bool, error) {
	if isDatabaseRowPageRecord(page) || page.GetBool("in_trash") {
		return false, nil
	}
	if strings.TrimSpace(workspaceImportExportedName(title.Spans)) != strings.TrimSpace(page.GetString("title")) {
		return false, nil
	}
	spans := title.Spans
	input := normalizePageMetadataInput(pageMetadataInput{
		Title: title.Text, TitleSpans: &spans, ParentID: page.GetString("parent_id"), InTrash: false,
		MutationID: fmt.Sprintf("workspace-import-title-%s", page.Id), BaseCursor: page.GetInt("cursor"),
	})
	if !validPageMetadataMutationInput(page.Id, input) {
		return false, nil
	}
	if _, _, err := applyPageMetadataTransaction(app, actorID, page.Id, input, pageMetadataRequestHash(page.Id, input)); err != nil {
		return false, err
	}
	return true, nil
}

// hangWorkspaceImportSubPage hangs a sub-page made already on a field of
// its parent: its block leaves the body. One not made yet finds the field
// as it is made; one hanging already, or gone, stays as it is.
func hangWorkspaceImportSubPage(app core.App, actorID string, parent *core.Record, subPageID, anchor string) error {
	page, err := app.FindRecordById(pagesCollection, subPageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if page.GetString("parent_id") != parent.Id || page.GetBool("in_trash") || page.GetString(pageAnchorBlockField) != "" {
		return nil
	}
	ownerID, workspaceID := parent.GetString("owner"), parent.GetString("workspace")
	if err := removeChildPageBlock(app, ownerID, workspaceID, subPageID, actorID, false); err != nil {
		return err
	}
	if err := setPageAnchor(app, subPageID, anchor, false); err != nil {
		return err
	}
	return alignMentionAnchoredPages(app, parent.Id, []string{subPageID})
}

// workspaceImportExportNamePattern is a file named as an export names a
// page: its title and its ID (Notion's 32 characters or UUID, Leafen's 15).
var workspaceImportExportNamePattern = regexp.MustCompile(
	`(?i)\s(?:[0-9a-f]{32}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[a-z0-9]{15})\.[a-z0-9]+$`)

// workspaceImportBodyMentions are the pages a parent's body names: the
// first block naming each, and the sub-pages with blocks there.
type workspaceImportBodyMentions struct {
	named  map[string]string
	blocks map[string]bool
}

// placeWorkspaceImportBatchPage places a batch's page in its parent: on the
// field of the parent's that names it, for an exported page its parent's
// text names in a field only, or else in the parent's body
// (workspaceImportBatchPlacement).
func placeWorkspaceImportBatchPage(
	app core.App, jobID string, row workspaceImportBatchPageRow, input *pageCreationAdmissionInput,
	bodies map[string]*workspaceImportBodyMentions,
) error {
	anchor, err := workspaceImportBatchFieldAnchor(app, input.ParentID, row.PageID, row.Name, bodies)
	if err != nil || anchor != "" {
		input.AnchorBlockID = anchor
		return err
	}
	input.BodyPlacement, err = workspaceImportBatchPlacement(app, jobID, row)
	return err
}

// workspaceImportBatchFieldAnchor is the field of its parent an exported
// sub-page made after its parent's text hangs on: the parent's title, a
// database page's title or text cell, or a block of its body that names it
// while the body has no block of it; "" for none.
func workspaceImportBatchFieldAnchor(
	app core.App, parentID, pageID, name string, bodies map[string]*workspaceImportBodyMentions,
) (string, error) {
	if parentID == "" || !workspaceImportExportNamePattern.MatchString(name) {
		return "", nil
	}
	parent, err := app.FindRecordById(pagesCollection, parentID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if spansMentionPage(ordinaryPageTitleSpans(parent), pageID) {
		return pageAnchorTitle, nil
	}
	if isDatabaseRowPageRecord(parent) {
		cells := []struct {
			Property string `db:"property_id"`
			Spans    string `db:"spans"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[property_id]], [[spans]] FROM {{leafen_database_row_values}}
			WHERE [[row_id]] = {:row} AND [[type]] IN ('title', 'rich_text') AND instr([[spans]], {:page}) > 0
			ORDER BY [[property_id]]
		`).Bind(dbx.Params{"row": parentID, "page": pageID}).All(&cells); err != nil {
			return "", err
		}
		for _, cell := range cells {
			if spansMentionPage(databaseStoredSpans(cell.Spans), pageID) {
				return pageAnchorCellPrefix + cell.Property, nil
			}
		}
	}
	body, found := bodies[parentID]
	if !found {
		if body, err = loadWorkspaceImportBodyMentions(app, parentID); err != nil {
			return "", err
		}
		bodies[parentID] = body
	}
	if body.blocks[pageID] {
		return "", nil
	}
	return body.named[pageID], nil
}

// loadWorkspaceImportBodyMentions reads which pages a page's body names, by
// its blocks' words and by its table rows' cells and its captions.
func loadWorkspaceImportBodyMentions(app core.App, pageID string) (*workspaceImportBodyMentions, error) {
	body := &workspaceImportBodyMentions{named: map[string]string{}, blocks: map[string]bool{}}
	blocks := []struct {
		ID      string `db:"id"`
		Type    string `db:"type"`
		Content string `db:"content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[content]] FROM {{leafen_blocks}} WHERE [[page_id]] = {:page} ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"page": pageID}).All(&blocks); err != nil || len(blocks) == 0 {
		return body, err
	}
	doc, found, err := loadYgoDoc(app, pageID)
	if err != nil {
		return nil, err
	}
	name := func(blockID string, spans []graphTextSpan) {
		for _, span := range spans {
			if target := strings.TrimSpace(span.PageID); span.Mention == "page" && span.BlockID == "" && body.named[target] == "" {
				body.named[target] = blockID
			}
		}
	}
	for _, block := range blocks {
		if isStoredChildPageBlock(block.ID, block.Type, block.Content) {
			body.blocks[block.ID] = true
			continue
		}
		content := struct {
			Caption []graphTextSpan   `json:"caption"`
			Cells   [][]graphTextSpan `json:"cells"`
		}{}
		if json.Unmarshal([]byte(block.Content), &content) == nil {
			name(block.ID, content.Caption)
			for _, cell := range content.Cells {
				name(block.ID, cell)
			}
		}
		text := (*crdt.YText)(nil)
		if found {
			text = doc.GetText(ygoBlockTextRootPrefix + block.ID)
		}
		if text == nil {
			continue
		}
		for _, delta := range textDeltaAsItStands(text.ToDelta(), false) {
			mention, _ := delta.Attributes["mention"].(string)
			target, _ := delta.Attributes["pageId"].(string)
			named, _ := delta.Attributes["blockId"].(string)
			if mention == "page" && named == "" {
				name(block.ID, []graphTextSpan{{Mention: "page", PageID: target}})
			}
		}
	}
	return body, nil
}
