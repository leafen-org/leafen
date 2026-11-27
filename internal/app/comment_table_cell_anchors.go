package app

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A thread on words in a simple table's cell is its row's, as Notion's is
// (seen 2026-11-22 through its MCP: a selection of one cell's words, a pipe
// table's or a tagged one's, became a discussion on the row, context block,
// its text-context the words, and the words took its mark in the cell —
// <span discussion-urls> around them in the row's Markdown). Its first
// comment names the cell in its quote's field, "cell:<n>" counting from 0,
// with the words' offsets in the cell's text; the cell's runs, which the
// row's content holds rather than the page's collaborative text, take the
// thread's id in their commentId as a caption's do (comment_caption_anchors.go),
// and keep it when the row is written again.

// commentQuoteCellPrefix starts a quote's field naming a table row's cell.
const commentQuoteCellPrefix = "cell:"

// commentQuoteFieldPlace reads a quote's field: a caption, a table row's
// cell (from 1), or the block's own text for none; false for another.
func commentQuoteFieldPlace(field string) (caption bool, cell int, named bool) {
	switch {
	case field == "":
		return false, 0, true
	case field == commentQuoteCaption:
		return true, 0, true
	}
	digits, isCell := strings.CutPrefix(field, commentQuoteCellPrefix)
	index, err := strconv.Atoi(digits)
	if !isCell || err != nil || index < 0 || index >= maxGraphTableWidth || strconv.Itoa(index) != digits {
		return false, 0, false
	}
	return false, index + 1, true
}

// commentQuoteCellField is the field naming a table row's cell (from 1).
func commentQuoteCellField(cell int) string {
	return commentQuoteCellPrefix + strconv.Itoa(cell-1)
}

// commentTableCellRuns are a table row's runs in one cell (from 1), as its
// content holds them; none for a cell the row doesn't have.
func commentTableCellRuns(content map[string]any, cell int) []any {
	cells, _ := content["cells"].([]any)
	if cell < 1 || cell > len(cells) {
		return nil
	}
	runs, _ := cells[cell-1].([]any)
	return runs
}

// commentTableCellRunsSet puts a cell's runs in its row's content.
func commentTableCellRunsSet(content map[string]any, cell int, runs []any) {
	cells, _ := content["cells"].([]any)
	if cell < 1 || cell > len(cells) {
		return
	}
	cells[cell-1] = runs
	content["cells"] = cells
}

// validGraphTableCellSpan checks a table cell's span as any block content's,
// save the threads its words were left on, which a cell keeps.
func validGraphTableCellSpan(span graphTextSpan) bool {
	threads := span.CommentID
	span.CommentID = ""
	if !validGraphContentTextSpan(span) {
		return false
	}
	for _, thread := range strings.Fields(threads) {
		if !validCommentIdentifier(thread) {
			return false
		}
	}
	return threads == "" || len(strings.Fields(threads)) > 0
}

// commentTableCellThreads are the threads whose marks a page's table rows
// hold in their cells, by row, among the blocks named.
func commentTableCellThreads(app core.App, pageID string, blockIDs []string) map[string][]string {
	rows := []struct {
		ID      string `db:"id"`
		Content string `db:"content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[content]] FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId} AND [[type]]={:type}
	`).Bind(dbx.Params{"pageId": pageID, "type": graphBlockTypeTableRow}).All(&rows); err != nil {
		return nil
	}
	threads := map[string][]string{}
	for _, row := range rows {
		content := graphTableRowBlockContent{}
		if !slices.Contains(blockIDs, row.ID) || json.Unmarshal([]byte(row.Content), &content) != nil {
			continue
		}
		for _, cell := range content.Cells {
			for _, span := range cell {
				for _, thread := range strings.Fields(span.CommentID) {
					if !slices.Contains(threads[row.ID], thread) {
						threads[row.ID] = append(threads[row.ID], thread)
					}
				}
			}
		}
	}
	return threads
}
