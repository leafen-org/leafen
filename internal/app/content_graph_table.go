package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A simple table is as wide as Notion's (2,000 columns kept, seen
// 2026-10-13): its bound, maxGraphTableWidth, is the most cells a row's
// content can hold within a change's bytes ("[]," each), and a row's text is
// bounded by those bytes, not by a block's text counts. A table may have no
// rows, and then no width, its rows (a "v2" <tr></tr>'s) no cells, as
// Notion's Markdown makes one (its API's table_width null, seen 2026-11-14);
// one its rows were all deleted from keeps its width. The public API's
// blocks and a Markdown or HTML file's import make none narrower than
// minGraphTableWidth.
const (
	minGraphTableWidth = 1
	// A dragged or fitted column is 35px to 4000px, as Notion's narrowest
	// (seen 2026-10-11); 0 leaves a column to its text.
	minGraphTableColumnWidth = 35
	maxGraphTableColumnWidth = 4000
	// A merged cell reaches at most this many rows down.
	maxGraphTableCellSpanRows = 1 << 20
)

// A table's columns, its rows and their cells may each have a color, a text
// color or a background as a block's (Notion's "색", whose page Markdown
// writes <col color>, <tr color> and <td color>); a cell's comes before its
// row's, and a row's before its column's.
type graphTableBlockContent struct {
	TableWidth      int  `json:"tableWidth"`
	HasColumnHeader bool `json:"hasColumnHeader"`
	HasRowHeader    bool `json:"hasRowHeader"`
	// Notion's page Markdown keeps a table's fit-page-width, which nothing
	// draws (seen 2026-11-05).
	FitPageWidth bool      `json:"fitPageWidth,omitzero"`
	ColumnWidths []float64 `json:"columnWidths,omitempty"`
	ColumnColors []string  `json:"columnColors,omitempty"`
}

type graphTableRowBlockContent struct {
	Cells      [][]graphTextSpan    `json:"cells"`
	Color      string               `json:"color,omitempty"`
	CellColors []string             `json:"cellColors,omitempty"`
	CellSpans  []graphTableCellSpan `json:"cellSpans,omitempty"`
}

// A merged cell, Notion's "셀 병합" (seen 2026-11-10): kept on the row of
// its first cell, it stands over that many rows from there and that many
// columns from its own, its first cell's content shown over the others,
// which the merge emptied. The public API and page Markdown give a merged
// table's cells one by one, as Notion's do.
type graphTableCellSpan struct {
	Column  int `json:"column"`
	Rows    int `json:"rows"`
	Columns int `json:"columns"`
}

// A row's merged cells are within its cells, in their order, each two cells
// or more and apart from one another; how far down one reaches is the
// table's to clip.
func validGraphTableCellSpans(spans []graphTableCellSpan, width int) bool {
	if len(spans) > width {
		return false
	}
	end := 0
	for _, span := range spans {
		if span.Column < end || span.Column >= width || span.Columns < 1 ||
			span.Columns > width-span.Column || span.Rows < 1 || span.Rows > maxGraphTableCellSpanRows ||
			span.Rows < 2 && span.Columns < 2 {
			return false
		}
		end = span.Column + span.Columns
	}
	return true
}

// validGraphTableColor is a column's, a row's or a cell's color: none, or
// a block color other than the default.
func validGraphTableColor(color string) bool {
	return color == "" || color != "default" && color != "default_background" && validNotionBlockColor(color)
}

// graphTableColorsKept is a list of colors as content keeps it: none past
// the last color, as many as the table is wide, no list without one.
func graphTableColorsKept(colors []string, width int) []string {
	end := min(len(colors), width)
	for end > 0 && colors[end-1] == "" {
		end--
	}
	if end == 0 {
		return nil
	}
	return colors[:end]
}

func validGraphTableColors(colors []string, most int) bool {
	if len(colors) > most {
		return false
	}
	for _, color := range colors {
		if !validGraphTableColor(color) {
			return false
		}
	}
	return true
}

func parseGraphTableBlockContent(raw json.RawMessage) (graphTableBlockContent, error) {
	content := graphTableBlockContent{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&content) != nil || content.TableWidth < 0 || content.TableWidth > maxGraphTableWidth {
		return graphTableBlockContent{}, graphInputError(errorCodeGraphInvalid, "Check the table width.")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return graphTableBlockContent{}, graphInputError(errorCodeGraphInvalid, "Check the table content.")
	}
	if len(content.ColumnWidths) > content.TableWidth {
		return graphTableBlockContent{}, graphInputError(errorCodeGraphInvalid, "Check the table column widths.")
	}
	for _, width := range content.ColumnWidths {
		if width != 0 && (width < minGraphTableColumnWidth || width > maxGraphTableColumnWidth) {
			return graphTableBlockContent{}, graphInputError(errorCodeGraphInvalid, "Check the table column widths.")
		}
	}
	if !validGraphTableColors(content.ColumnColors, content.TableWidth) {
		return graphTableBlockContent{}, graphInputError(errorCodeGraphInvalid, "Check the table column colors.")
	}
	return content, nil
}

func parseGraphTableRowBlockContent(raw json.RawMessage) (graphTableRowBlockContent, error) {
	content := graphTableRowBlockContent{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&content) != nil || len(content.Cells) > maxGraphTableWidth {
		return graphTableRowBlockContent{}, graphInputError(errorCodeGraphInvalid, "Check the table row cells.")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return graphTableRowBlockContent{}, graphInputError(errorCodeGraphInvalid, "Check the table row content.")
	}
	if content.Cells == nil {
		content.Cells = [][]graphTextSpan{}
	}
	for _, cell := range content.Cells {
		for _, span := range cell {
			if !validGraphTableCellSpan(span) {
				return graphTableRowBlockContent{}, graphInputError(errorCodeGraphInvalid, "Check the table cell text.")
			}
		}
	}
	if !validGraphTableColor(content.Color) || !validGraphTableColors(content.CellColors, len(content.Cells)) {
		return graphTableRowBlockContent{}, graphInputError(errorCodeGraphInvalid, "Check the table row colors.")
	}
	if !validGraphTableCellSpans(content.CellSpans, len(content.Cells)) {
		return graphTableRowBlockContent{}, graphInputError(errorCodeGraphInvalid, "Check the table row's merged cells.")
	}
	if len(raw) > maxGraphChangeBytes {
		return graphTableRowBlockContent{}, graphInputError(errorCodeGraphInvalid, "Reduce the table row's text size.")
	}
	return content, nil
}

// graphContentRichTextField names the block content fields that hold rich
// text spans: a caption, a table row's cells, a database page body block's
// text as the API and exports write it, and the text a block keeps in its
// content until its page's collaborative document takes it.
func graphContentRichTextField(field string) bool {
	switch field {
	case "caption", "cells", "notionRichTextSpans", "rich":
		return true
	}
	return false
}

// validGraphContentTextSpan checks one span of rich text kept in block content:
// a table cell or a caption.
func validGraphContentTextSpan(span graphTextSpan) bool {
	// Suggested edits live on block text only; captions and cells carry none.
	if span.Color != "" && !validNotionBlockColor(span.Color) ||
		span.Background != "" && !validNotionSpanBackground(span.Background) ||
		span.Link != "" && !validGraphHeldTextLink(span.Link) || span.CommentID != "" ||
		span.DateReminder != "" && (span.Mention != "date" || !validDateMentionReminder(span.Date, span.DateReminder)) ||
		span.Suggestion != "" || span.SuggestionID != "" || span.SuggestionBefore != "" ||
		span.DateFormat != "" && (span.Mention != "date" || !validDateMentionFormat(span.DateFormat)) ||
		span.DateTimeFormat != "" && (span.Mention != "date" || !validDateMentionTimeFormat(span.DateTimeFormat)) ||
		span.BlockID != "" && span.Mention != "page" ||
		!validGraphLinkMentionPreview(span) || !validGraphDateStartUnset(span) {
		return false
	}
	if span.Equation != "" && (span.Text != span.Equation ||
		len([]rune(span.Equation)) > maxNotionInlineEquationRunes || span.Link != "" ||
		span.Mention != "" || span.PageID != "" || span.DatabaseID != "" ||
		span.CustomEmojiID != "" || span.Date != "" || span.DateEnd != "" ||
		span.DateTimeZone != "" || span.UserID != "" || span.IconURL != "") {
		return false
	}
	switch span.Mention {
	case "":
		return span.PageID == "" && span.DatabaseID == "" && span.CustomEmojiID == "" &&
			span.Date == "" && span.DateEnd == "" && span.DateTimeZone == "" && span.UserID == "" && span.IconURL == ""
	case "page":
		return span.PageID != "" && span.DatabaseID == "" && span.CustomEmojiID == "" && span.UserID == "" &&
			span.Date == "" && span.DateEnd == "" && span.DateTimeZone == "" && span.IconURL == ""
	case "database":
		return span.DatabaseID != "" && span.PageID == "" && span.CustomEmojiID == "" && span.UserID == "" &&
			span.Date == "" && span.DateEnd == "" && span.DateTimeZone == "" && span.IconURL == ""
	case "custom_emoji":
		return span.CustomEmojiID != "" && span.PageID == "" && span.DatabaseID == "" && span.UserID == "" &&
			span.Date == "" && span.DateEnd == "" && span.DateTimeZone == "" && span.IconURL == ""
	case "person":
		return span.UserID != "" && span.PageID == "" && span.DatabaseID == "" && span.CustomEmojiID == "" &&
			span.Date == "" && span.DateEnd == "" && span.DateTimeZone == "" && span.IconURL == ""
	case "date":
		// Its date may be one Markdown kept as written (notionMarkdownDateSpan),
		// as a block's text's may.
		return span.PageID == "" && span.DatabaseID == "" && span.CustomEmojiID == "" &&
			span.UserID == "" && span.IconURL == ""
	case "external":
		return span.PageID == "" && span.DatabaseID == "" && span.CustomEmojiID == "" && span.UserID == "" &&
			span.Date == "" && span.DateEnd == "" && span.DateTimeZone == "" && span.Link != "" &&
			(span.IconURL == "" || validGraphExternalFileURL(span.IconURL))
	default:
		return false
	}
}

// validGraphLinkMentionPreview checks what a mention of an outside page
// keeps of that page's preview, as Notion's link mention does: the site's
// name drawn before its title, its description and its picture for the
// mention's card. Only such a mention keeps them.
func validGraphLinkMentionPreview(span graphTextSpan) bool {
	if span.Provider == "" && span.Description == "" && span.ImageURL == "" {
		return true
	}
	return span.Mention == "external" && (span.ImageURL == "" || validGraphExternalFileURL(span.ImageURL))
}

type graphTableValidationBlock struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
	Type     string `db:"type"`
	Content  string `db:"content"`
}

func validateNativeTableBlockOperations(
	app core.App,
	owner, pageID string,
	operations []graphOperation,
) error {
	lookupIDs := map[string]struct{}{}
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			lookupIDs[operation.Block.ID] = struct{}{}
			if operation.Block.ParentID != nil {
				lookupIDs[*operation.Block.ParentID] = struct{}{}
			}
		case graphOperationKindMoveBlock:
			lookupIDs[operation.Move.BlockID] = struct{}{}
			if operation.Move.ParentID != nil {
				lookupIDs[*operation.Move.ParentID] = struct{}{}
			}
		case graphOperationKindDeleteBlock:
			lookupIDs[operation.Deletion.BlockID] = struct{}{}
		}
	}
	known, err := loadGraphTableValidationBlocksByID(app, owner, pageID, lookupIDs)
	if err != nil {
		return err
	}
	virtual := make(map[string]graphTableValidationBlock, len(known)+len(operations))
	for id, block := range known {
		virtual[id] = block
	}
	affectedTableIDs := map[string]struct{}{}
	markRelatedTable := func(block graphTableValidationBlock) {
		switch block.Type {
		case graphBlockTypeTable:
			affectedTableIDs[block.ID] = struct{}{}
		case graphBlockTypeTableRow:
			if block.ParentID != "" {
				affectedTableIDs[block.ParentID] = struct{}{}
			}
		}
		if parent, exists := virtual[block.ParentID]; exists {
			switch parent.Type {
			case graphBlockTypeTable:
				affectedTableIDs[parent.ID] = struct{}{}
			case graphBlockTypeTableRow:
				affectedTableIDs[parent.ParentID] = struct{}{}
			}
		}
	}
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			if existing, exists := virtual[operation.Block.ID]; exists {
				markRelatedTable(existing)
			}
			parentID := ""
			if operation.Block.ParentID != nil {
				parentID = *operation.Block.ParentID
			}
			block := graphTableValidationBlock{
				ID: operation.Block.ID, ParentID: parentID, Type: operation.Block.Type,
				Content: string(operation.Block.Content),
			}
			virtual[block.ID] = block
			markRelatedTable(block)
		case graphOperationKindMoveBlock:
			block := virtual[operation.Move.BlockID]
			markRelatedTable(block)
			block.ParentID = ""
			if operation.Move.ParentID != nil {
				block.ParentID = *operation.Move.ParentID
			}
			virtual[block.ID] = block
			markRelatedTable(block)
		case graphOperationKindDeleteBlock:
			markRelatedTable(virtual[operation.Deletion.BlockID])
		}
	}
	delete(affectedTableIDs, "")
	if len(affectedTableIDs) == 0 {
		return nil
	}
	blocks, err := loadGraphTableValidationScope(
		app, owner, pageID, affectedTableIDs, lookupIDs,
	)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			parentID := ""
			if operation.Block.ParentID != nil {
				parentID = *operation.Block.ParentID
			}
			blocks[operation.Block.ID] = graphTableValidationBlock{
				ID: operation.Block.ID, ParentID: parentID, Type: operation.Block.Type,
				Content: string(operation.Block.Content),
			}
		case graphOperationKindMoveBlock:
			block := blocks[operation.Move.BlockID]
			block.ParentID = ""
			if operation.Move.ParentID != nil {
				block.ParentID = *operation.Move.ParentID
			}
			blocks[block.ID] = block
		case graphOperationKindDeleteBlock:
			deleteGraphTableValidationSubtree(blocks, operation.Deletion.BlockID)
		}
	}
	children := map[string][]graphTableValidationBlock{}
	for _, block := range blocks {
		children[block.ParentID] = append(children[block.ParentID], block)
	}
	for _, block := range blocks {
		switch block.Type {
		case graphBlockTypeTable:
			content, err := parseGraphTableBlockContent(json.RawMessage(block.Content))
			if err != nil {
				return err
			}
			for _, row := range children[block.ID] {
				if row.Type != graphBlockTypeTableRow {
					return graphInputError(errorCodeGraphInvalid, "A table's direct children must be table rows.")
				}
				rowContent, err := parseGraphTableRowBlockContent(json.RawMessage(row.Content))
				if err != nil {
					return err
				}
				if len(rowContent.Cells) != content.TableWidth {
					return graphInputError(errorCodeGraphInvalid, "A table row's cell count must match the table width.")
				}
			}
		case graphBlockTypeTableRow:
			parent, exists := blocks[block.ParentID]
			if !exists || parent.Type != graphBlockTypeTable {
				return graphInputError(errorCodeGraphInvalid, "A table row must be a direct child of a table.")
			}
			if len(children[block.ID]) != 0 {
				return graphInputError(errorCodeGraphInvalid, "A table row can't have child blocks.")
			}
		default:
			if parent, exists := blocks[block.ParentID]; exists && parent.Type == graphBlockTypeTable {
				return graphInputError(errorCodeGraphInvalid, "A table's direct children must be table rows.")
			}
		}
	}
	return nil
}

func loadGraphTableValidationBlocksByID(
	app core.App,
	owner, pageID string,
	ids map[string]struct{},
) (map[string]graphTableValidationBlock, error) {
	delete(ids, "")
	if len(ids) == 0 {
		return map[string]graphTableValidationBlock{}, nil
	}
	params := dbx.Params{"owner": owner, "pageId": pageID}
	placeholders := graphTableValidationPlaceholders(ids, "id", params)
	rows := []graphTableValidationBlock{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]],[[type]],[[content]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
			AND [[id]] IN (` + strings.Join(placeholders, ",") + `)
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	result := make(map[string]graphTableValidationBlock, len(rows))
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, nil
}

func loadGraphTableValidationScope(
	app core.App,
	owner, pageID string,
	tableIDs, touchedIDs map[string]struct{},
) (map[string]graphTableValidationBlock, error) {
	params := dbx.Params{"owner": owner, "pageId": pageID}
	tables := graphTableValidationPlaceholders(tableIDs, "table", params)
	touched := graphTableValidationPlaceholders(touchedIDs, "touched", params)
	rows := []graphTableValidationBlock{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]],[[type]],[[content]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND (
			[[id]] IN (` + strings.Join(tables, ",") + `)
			OR [[parent_id]] IN (` + strings.Join(tables, ",") + `)
			OR [[id]] IN (` + strings.Join(touched, ",") + `)
		)
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	result := make(map[string]graphTableValidationBlock, len(rows)+len(touchedIDs))
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, nil
}

func graphTableValidationPlaceholders(
	ids map[string]struct{},
	prefix string,
	params dbx.Params,
) []string {
	placeholders := make([]string, 0, len(ids))
	index := 0
	for id := range ids {
		if id == "" {
			continue
		}
		key := prefix + strconv.Itoa(index)
		params[key] = id
		placeholders = append(placeholders, "{:"+key+"}")
		index++
	}
	return placeholders
}

func deleteGraphTableValidationSubtree(blocks map[string]graphTableValidationBlock, rootID string) {
	queue := []string{rootID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, block := range blocks {
			if block.ParentID == id {
				queue = append(queue, block.ID)
			}
		}
		delete(blocks, id)
	}
}
