package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A copy's own edit of a page it made — "〈x〉 (1) 페이지를 편집했습니다" in
// Notion's workspace updates — lists what the copy holds, each as made: "Aa
// 제목 〈x (1)〉" first, then its lines and sub-pages (workspaceActivityCopyOrder),
// a line inside another under it (seen 2026-11-17 by worker w1613 08:2x and
// by w1625 09:1x in another tab, first account: pages at the top with text
// alone and with sub-pages and a toggle duplicated from '⋯', and a sub-page
// duplicated from its block's handle, its own line its name and its body). A
// copy writes its blocks with no versions of the page's history, so they are
// read as the copy left them: the page's first kept state where its history
// began after the copy, else the page as it is. A sub-page's copy is named by
// its making (workspaceActivityMadeTitle); a copy at the top by its edit's
// name. A copy holding a database shows no line at all
// (workspace_activity_copy.go).

// workspaceActivityCopyKind is the kind of copy whose own edit of a page a
// group is, if it is one: the page's edit by a shown copy operation — a
// page's copy, whose only page edits are of the pages it made
// (pageDuplicationJobKind), or blocks copied into a page (blockMoveJobKind:
// a button's blocks, a template's body, a paste that copies) — else "".
func workspaceActivityCopyKind(row storedWorkspaceActivity, metadata workspaceActivityMetadata, copyJobs map[string]string) string {
	if row.EntityType != "page" || row.Action != "edit" || row.EntityID != metadata.PageID {
		return ""
	}
	return copyJobs[metadata.OperationJobID]
}

// workspaceActivityCopyChanges lists a copy's own edit of a page as
// workspaceActivityChangesOf lists an edit's: each block as a block the edit
// made (workspaceActivityMadeLine), those past workspaceActivityChangeItems
// counted.
func workspaceActivityCopyChanges(app core.App, actor string, scope workspaceAuthorization, pageID string, row storedWorkspaceActivity,
	cache map[string]*pageAuthorization) ([]workspaceActivityChange, int, error) {
	changes := []workspaceActivityChange{}
	var metadata workspaceActivityMetadata
	if json.Unmarshal([]byte(row.Metadata), &metadata) == nil && !metadata.Made && strings.TrimSpace(metadata.Name) != "" {
		changes = append(changes, workspaceActivityChange{Kind: workspaceActivityChangeProperty, BlockID: graphValueTypeTitle,
			PropertyType: graphValueTypeTitle, After: workspaceActivityClip(metadata.Name)})
	}
	blocks, document, err := workspaceActivityCopyState(app, pageID)
	if err != nil {
		return nil, 0, err
	}
	return workspaceActivityMadeChanges(app, actor, scope, blocks, document, changes, cache)
}

// workspaceActivityMadeChanges adds the lines of blocks a copy made, in its
// order (workspaceActivityCopyOrder), to changes, each as a block an edit
// made (workspaceActivityMadeLine); those past workspaceActivityChangeItems
// are counted.
func workspaceActivityMadeChanges(app core.App, actor string, scope workspaceAuthorization, blocks []pageHistoryBlockState, document *crdt.Doc,
	changes []workspaceActivityChange, cache map[string]*pageAuthorization) ([]workspaceActivityChange, int, error) {
	more := 0
	for _, block := range workspaceActivityCopyOrder(blocks) {
		span := pageHistoryChangeSpan{ParentID: block.ParentID, Type: block.Type, Content: string(block.Content)}
		text := func() (workspaceActivityText, error) {
			runs := pageHistoryBlockTextRuns(document, block.ID)
			rich, err := encodePageHistoryTextRuns(runs)
			return workspaceActivityTextOf(pageHistoryTextRunsText(runs), rich), err
		}
		// Those past the list are counted by whether they would be a line.
		if len(changes) == workspaceActivityChangeItems {
			if workspaceActivityWouldBeLine(block, document) {
				more++
			}
			continue
		}
		change, made, err := workspaceActivityMadeLine(app, actor, scope, block.ID, span, text, cache)
		if err != nil {
			return nil, 0, err
		}
		if made {
			changes = append(changes, workspaceActivityClipped(change))
		}
	}
	return changes, more, nil
}

// workspaceActivityWouldBeLine is whether a block a copy made would be a
// line (workspaceActivityMadeLine), told without reading its line: a
// sub-page, an inline database, a block shown bare, or one with words.
func workspaceActivityWouldBeLine(block pageHistoryBlockState, document *crdt.Doc) bool {
	span := pageHistoryChangeSpan{ParentID: block.ParentID, Type: block.Type, Content: string(block.Content)}
	return block.Type == graphBlockTypeChildPage || block.Type == graphBlockTypeInlineDatabase || workspaceActivityBareKinds[block.Type] ||
		strings.TrimSpace(workspaceActivityExpression(span)) != "" || strings.TrimSpace(pageHistoryTextRunsText(pageHistoryBlockTextRuns(document, block.ID))) != ""
}

// workspaceActivityCopyState is a copied page's blocks and their text as the
// copy left them: its history's first kept state, which the page's first
// change after the copy keeps before it, else the page now.
func workspaceActivityCopyState(app core.App, pageID string) ([]pageHistoryBlockState, *crdt.Doc, error) {
	blocks := []pageHistoryBlockState{}
	checkpoint, err := loadPageHistoryCheckpoint(app, pageID, 0)
	if err == nil {
		if err := json.Unmarshal([]byte(checkpoint.BlocksJSON), &blocks); err != nil {
			return nil, nil, err
		}
		document, err := pageHistoryDocument(checkpoint.CollaborationState)
		return blocks, document, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	stored := []storedPageHistoryBlockState{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[parent_id]],[[position]],[[type]],[[content]],[[created]],[[updated]]
		FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).All(&stored); err != nil {
		return nil, nil, err
	}
	for _, block := range stored {
		blocks = append(blocks, pageHistoryBlockState{ID: block.ID, ParentID: block.ParentID, Position: block.Position,
			Type: block.Type, Content: json.RawMessage(block.Content)})
	}
	document, _, err := loadYgoDoc(app, pageID)
	return blocks, document, err
}

// workspaceActivityCopyOrder is a copy's blocks as Notion's updates list
// them: the page's order — each block's children right under it, siblings
// by their places — but a sub-page before the sibling right above it, that
// sibling's children with it, where that sibling is no sub-page. Notion's
// "w1625 d (1)" listed d sub A, d one, d sub B, d two, d three for a page of
// d one, d sub A, d two, d sub B, d three; "w1625 e (1)" E1, E2, e one, E3,
// e two, E4, e toggle, e inner for E1, e one, E2, E3, e two, e toggle (e
// inner in it), E4 (seen 2026-11-17 by worker w1625 09:1x and 09:2x in
// another tab, and so "t4 (1)" by w1613 08:2x). Its own edits list a page's
// blocks in its order. A block whose parent isn't among them stands with
// those at the top.
func workspaceActivityCopyOrder(blocks []pageHistoryBlockState) []pageHistoryBlockState {
	known := make(map[string]bool, len(blocks))
	for _, block := range blocks {
		known[block.ID] = true
	}
	children := map[string][]pageHistoryBlockState{}
	for _, block := range blocks {
		parent := block.ParentID
		if !known[parent] {
			parent = ""
		}
		children[parent] = append(children[parent], block)
	}
	for _, siblings := range children {
		sort.SliceStable(siblings, func(left, right int) bool {
			if siblings[left].Position != siblings[right].Position {
				return siblings[left].Position < siblings[right].Position
			}
			return siblings[left].ID < siblings[right].ID
		})
	}
	var level func(parent string) []pageHistoryBlockState
	level = func(parent string) []pageHistoryBlockState {
		ordered, above := []pageHistoryBlockState{}, []pageHistoryBlockState(nil)
		for _, block := range children[parent] {
			unit := append([]pageHistoryBlockState{block}, level(block.ID)...)
			if block.Type == graphBlockTypeChildPage {
				ordered, above = append(append(ordered, unit...), above...), nil
				continue
			}
			ordered, above = append(ordered, above...), unit
		}
		return append(ordered, above...)
	}
	return level("")
}

// workspaceActivityMadeLine is the line of a block an edit made — each of a
// copy's — as Notion's updates list one: an equation's expression or a
// line's words added, with its kind (workspaceActivityMarked); a sub-page by
// its name, only to who can see it; an inline database by its name
// (workspaceActivityDatabaseLine). A block with no words is no line, but one
// shown bare (workspaceActivityBareKinds). text reads the block's words when
// needed.
func workspaceActivityMadeLine(app core.App, actor string, scope workspaceAuthorization, blockID string, block pageHistoryChangeSpan,
	text func() (workspaceActivityText, error), cache map[string]*pageAuthorization) (workspaceActivityChange, bool, error) {
	change := workspaceActivityChange{Kind: workspaceActivityChangeAdded, BlockID: blockID}
	switch block.Type {
	case graphBlockTypeChildPage:
		child, err := workspaceActivityPage(app, actor, scope, blockID, cache)
		if err != nil || child == nil {
			return change, false, err
		}
		change.Kind, change.Text = workspaceActivityChangePage, strings.TrimSpace(child.Page.GetString("title"))
		return change, true, nil
	case graphBlockTypeInlineDatabase:
		change, err := workspaceActivityDatabaseLine(app, blockID, block)
		return change, err == nil, err
	case graphBlockTypeEquation:
		change.Added = strings.TrimSpace(workspaceActivityExpression(block))
	default:
		words, err := text()
		if err != nil {
			return change, false, err
		}
		change.Added, change.Spans = strings.TrimSpace(words.plain), words.spans
	}
	change = workspaceActivityMarked(change, block)
	return change, change.Added != "" || workspaceActivityBareKinds[block.Type], nil
}

// workspaceActivityClipped is a change with its words cut to the list's
// length; a line cut short shows as plain text, as its spans spell it whole.
func workspaceActivityClipped(change workspaceActivityChange) workspaceActivityChange {
	for _, field := range []*string{&change.Before, &change.Added, &change.After, &change.Text, &change.Was} {
		if clipped := workspaceActivityClip(*field); clipped != *field {
			*field, change.Spans = clipped, nil
		}
	}
	return change
}
