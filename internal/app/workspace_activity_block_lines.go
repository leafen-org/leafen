package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How an edit's changed line names a block's kind, as Notion's workspace
// updates and a page's updates tab draw it alike (seen 2026-11-17 by workers
// w1645 and w1658, two tabs each, first account, test workspace): a toggle's
// caret, a bulleted item's bullet, a numbered one's "1.", a to-do's box,
// ticked when done, a heading's and a toggle heading's weight, a code block,
// an equation, a quote and a callout boxed under their kind — each drawn by
// the list from the block's type (web updates/UpdateLine.tsx), a deleted one
// struck with its mark — and an inline database made in the edit by its own
// name (workspaceActivityChangesOf).

// workspaceActivityChangeDatabase is an inline database made in the edit:
// Text its name, empty for one with none.
const workspaceActivityChangeDatabase = "database"

// workspaceActivityBareKinds are the blocks whose made line shows with no
// words, as Notion's (seen 2026-11-17, two tabs each): a button's, a
// breadcrumb's, an image's and a synced block's an empty band — a button's
// name not shown even where it has one — a bookmark's an empty slot and a
// callout's its box. A divider, a table of contents and a line with no words
// show none. The page's updates tab keeps the same kinds (web
// lib/updateLineKinds.ts).
var workspaceActivityBareKinds = map[string]bool{graphBlockTypeButton: true, graphBlockTypeBreadcrumb: true, graphBlockTypeImage: true,
	graphBlockTypeSyncedBlock: true, graphBlockTypeBookmark: true, graphBlockTypeCallout: true}

// workspaceActivityMarked is a change with its block's kind: its type — none
// for a line of text — a code block's language and whether a to-do is done.
// A made bare block's line keeps no words, but a callout's; a deleted
// button's is its name with its workflow's steps, boxed and struck as
// Notion's (seen 2026-11-17, two pages).
func workspaceActivityMarked(change workspaceActivityChange, block pageHistoryChangeSpan) workspaceActivityChange {
	change.BlockType, change.Language, change.Checked, change.Steps = "", "", false, 0
	if block.Type == graphBlockTypeParagraph {
		return change
	}
	change.BlockType = block.Type
	content := struct {
		Language string `json:"language"`
		Checked  bool   `json:"checked"`
		Label    string `json:"label"`
		Document struct {
			Actions []json.RawMessage `json:"actions"`
		} `json:"buttonActionDocument"`
	}{}
	_ = json.Unmarshal([]byte(block.Content), &content)
	switch block.Type {
	case graphBlockTypeCode:
		change.Language = content.Language
	case graphBlockTypeToDo:
		change.Checked = content.Checked
	case graphBlockTypeButton:
		if change.Kind == workspaceActivityChangeDeleted {
			change.Text, change.Spans, change.Steps = strings.TrimSpace(content.Label), nil, len(content.Document.Actions)
		}
	}
	if change.Kind == workspaceActivityChangeAdded && block.Type != graphBlockTypeCallout && workspaceActivityBareKinds[block.Type] {
		change.Before, change.Added, change.After, change.Text, change.Spans = "", "", "", "", nil
	}
	return change
}

// workspaceActivityExpression is an equation block's expression.
func workspaceActivityExpression(block pageHistoryChangeSpan) string {
	content := map[string]any{}
	_ = json.Unmarshal([]byte(block.Content), &content)
	expression, _ := content["expression"].(string)
	return expression
}

// workspaceActivityDatabaseLine is an inline database's line when the edit
// made it, as Notion's workspace updates list one (seen 2026-11-16 at 1440
// dark, three edits): the page glyph and its name on the added line's band,
// opening the database alone by its block. Its name is a linked one's own,
// else its source's as it is now. One there before the edit is no line — its
// renaming, views and rows are activities of their own — and one taken off
// the page shows nothing yet (Notion's not seen).
func workspaceActivityDatabaseLine(app core.App, blockID string, after pageHistoryChangeSpan) (workspaceActivityChange, error) {
	change := workspaceActivityChange{Kind: workspaceActivityChangeDatabase, BlockID: blockID}
	content, err := parseDatabaseContainerBlockContent(json.RawMessage(after.Content))
	if err != nil {
		return change, nil
	}
	linked := struct {
		Title string `json:"linkedTitle"`
	}{}
	_ = json.Unmarshal([]byte(after.Content), &linked)
	if change.Text = strings.TrimSpace(linked.Title); change.Text != "" || content.DataSourceID == "" {
		return change, nil
	}
	err = app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": content.DataSourceID}).Row(&change.Text)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	change.Text = strings.TrimSpace(change.Text)
	return change, err
}
