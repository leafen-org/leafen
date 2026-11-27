package app

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"sort"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A suggested edit is made as suggesting mode makes one, step by step
// through the page's suggestion route (page_suggestions.go), each on its
// block's text as it is then: a replacement is one suggestion, as typing
// over a selection is — old_text's words struck and new_text's first line
// in before them, a piece for each formatting (page_suggestion_text.go). A line break
// splits the block where words go on after it, as Enter there does
// (page_suggestion_splits.go); the new lines are blocks suggested one after
// another (page_suggestion_blocks.go), and the last line's words go in at
// the start of the block the words after the break stand in. At the
// block's start nothing splits: the new lines are blocks after the block
// before it, the first a block of its own as a menu picks one
// (page_suggestion_picked.go), and the block's words stay — or, with no
// text block before it, the block breaks at its start. The edit names
// each thread its steps made or grew; when a step fails, those made before
// it are taken back.

// mcpSuggester makes a suggested edit's steps on a page.
type mcpSuggester struct {
	ctx         context.Context
	app         core.App
	pageID      string
	threads     []string
	workspaceID string
}

// text is a block's text as it is now and its length in UTF-16 units.
func (s *mcpSuggester) text(blockID string) (string, int, error) {
	document, err := loadPageTextDocument(s.app, s.pageID)
	if err != nil {
		return "", 0, failedBecause(errMCPToolFailed, err)
	}
	text := document.GetText(ygoBlockTextRootPrefix + blockID).ToString()
	return text, len(utf16.Encode([]rune(text))), nil
}

func (s *mcpSuggester) step(input suggestionCreateInput) error {
	text, units, err := s.text(input.BlockID)
	if err != nil {
		return err
	}
	input.ID, input.RequestID, input.ExpectedText = uuid.NewString(), "mcp-suggest:"+newDiagnosticReference(), text
	if input.Kind == textSuggestionBlock {
		input.Offset = units
	}
	result, err := mcpNativeCall(s.ctx, "POST", "/pages/"+s.pageID+"/suggestions", "", input)
	if err != nil {
		return err
	}
	if err := mcpNativeRefusal(result); err != nil {
		return err
	}
	made := commentMutationResponse{}
	if encoded, err := json.Marshal(result.Body); err != nil || json.Unmarshal(encoded, &made) != nil {
		return failedBecause(errMCPToolFailed, err)
	}
	if !slices.Contains(s.threads, made.Comment.ID) {
		s.threads = append(s.threads, made.Comment.ID)
	}
	s.workspaceID = made.Comment.WorkspaceID
	return nil
}

// insert suggests words in at a block's offset, a piece after another.
func (s *mcpSuggester) insert(blockID string, offset int, pieces []mcpSuggestedPiece) error {
	for _, piece := range pieces {
		if err := s.step(suggestionCreateInput{Kind: textSuggestionInsert, BlockID: blockID, Offset: offset, Text: piece.text, Marks: piece.marks}); err != nil {
			return err
		}
		offset += len(utf16.Encode([]rune(piece.text)))
	}
	return nil
}

// suggest makes the edit new_text (text) suggests at old_text's place.
func (s *mcpSuggester) suggest(place mcpSuggestionPlace, mode string, text mcpSuggestedText) error {
	at := place.Offset
	if mode != "insert_before" {
		at += place.Length
	}
	replaced := mode != "insert_before" && mode != "insert_after" && len(text.first) > 0 && !text.before
	if mode != "insert_before" && mode != "insert_after" {
		_, before, err := s.text(place.BlockID)
		if err != nil {
			return err
		}
		step := suggestionCreateInput{Kind: textSuggestionDelete, BlockID: place.BlockID, Offset: place.Offset, Length: place.Length}
		if replaced {
			step.Kind, step.Text, step.Marks = textSuggestionReplace, text.first[0].text, text.first[0].marks
		}
		if err := s.step(step); err != nil {
			return err
		}
		// Taking words away that one suggested in oneself takes them out, as
		// on the screen — old_text found in fetch's Markdown can hold them
		// unseen — so what goes after them goes that much nearer.
		_, after, err := s.text(place.BlockID)
		if err != nil {
			return err
		}
		at -= before - after
		if replaced {
			// The new words' other pieces go on after the first, before the
			// struck words; the edit's line breaks go after those.
			words := len(utf16.Encode([]rune(text.first[0].text)))
			if err := s.insert(place.BlockID, place.Offset+words, text.first[1:]); err != nil {
				return err
			}
			for _, piece := range text.first[1:] {
				at += len(utf16.Encode([]rune(piece.text)))
			}
		}
	}
	head, tail, blocks := place.BlockID, "", text.blocks
	switch {
	case text.before && len(blocks) > 0:
		// Blocks before a block go after the text block before it, a block of
		// their own (picked: no break suggested there), or first in a toggle.
		// With none (the first in the page, a column or a callout) the block
		// breaks at its start, as Enter there does: the new block before it
		// ends in the suggested break, and rejecting it keeps the block.
		anchor, first, found, err := mcpSuggestionAnchorBefore(s.app, s.pageID, place.BlockID)
		if err != nil {
			return err
		}
		made := uuid.NewString()
		step := suggestionCreateInput{Kind: textSuggestionBlock, BlockID: anchor, NewBlockID: made, BlockType: blocks[0].kind, Picked: !first, FirstChild: first}
		if !found {
			step = suggestionCreateInput{Kind: textSuggestionSplit, BlockID: place.BlockID, NewBlockID: made, BlockType: blocks[0].kind}
		}
		if err := s.step(step); err != nil {
			return err
		}
		if err := s.insert(made, 0, blocks[0].pieces); err != nil {
			return err
		}
		head, tail, blocks = made, place.BlockID, blocks[1:]
	case text.before:
		tail = place.BlockID
	case text.last != nil:
		made := uuid.NewString()
		if err := s.step(suggestionCreateInput{Kind: textSuggestionSplit, BlockID: place.BlockID, Offset: at, NewBlockID: made, BlockType: text.last.kind}); err != nil {
			return err
		}
		tail = made
	}
	if !replaced {
		if err := s.insert(place.BlockID, at, text.first); err != nil {
			return err
		}
	}
	anchor := head
	for index, line := range blocks {
		made := uuid.NewString()
		// An open toggle's Markdown holds the lines after its own as its blocks.
		first := index == 0 && tail == "" && place.Type == graphBlockTypeToggle
		if err := s.step(suggestionCreateInput{Kind: textSuggestionBlock, BlockID: anchor, NewBlockID: made, BlockType: line.kind, FirstChild: first}); err != nil {
			return err
		}
		if err := s.insert(made, 0, line.pieces); err != nil {
			return err
		}
		anchor = made
	}
	if text.last != nil {
		if err := s.insert(tail, 0, text.last.pieces); err != nil {
			return err
		}
	}
	if len(s.threads) == 0 {
		return mcpInvalid("new_text suggests nothing there: give the words to put in, or <empty-block/> for an empty line")
	}
	return nil
}

// mcpSuggestionAnchorBefore is the block new blocks suggested before a
// block go after: the block before it among its siblings, when its text
// takes suggestions — else, for a toggle's first block, the toggle, which
// takes them as its first (first); none otherwise.
func mcpSuggestionAnchorBefore(app core.App, pageID, blockID string) (string, bool, bool, error) {
	document, found, done, err := borrowYgoStreamDocument(app, ygoStructuralStream, pageID)
	if err != nil {
		return "", false, false, failedBecause(errMCPToolFailed, err)
	}
	defer done()
	if !found {
		return "", false, false, nil
	}
	metadata, err := structuralMetadata(document)
	if err != nil {
		return "", false, false, failedBecause(errMCPToolFailed, err)
	}
	parentID := metadata[blockID].ParentID
	order, err := structuralOrder(document, parentID)
	if err != nil {
		return "", false, false, failedBecause(errMCPToolFailed, err)
	}
	if at := structuralIndex(order, blockID); at > 0 {
		_, takes := suggestionBlockTypes[metadata[order[at-1]].Type]
		return order[at-1], false, takes && metadata[order[at-1]].Type != graphBlockTypeCode, nil
	}
	_, toggle := suggestionToggleTypes[metadata[parentID].Type]
	return parentID, true, parentID != "" && toggle, nil
}

// mcpSuggestedEditBlocks are the blocks a suggested edit's threads stand
// in, in reading order: its own block, the blocks whose text holds their
// words, the blocks they suggested in (suggested), and the block each break
// they suggested made — not a block new blocks were only suggested after —
// and which of them are code blocks (code).
func mcpSuggestedEditBlocks(app core.App, document *crdt.Doc, edit mcpSuggestedEdit) ([]string, map[string]bool, map[string]bool, error) {
	rows := []struct {
		ID      string `db:"id"`
		Type    string `db:"type"`
		Content string `db:"content"`
	}{}
	err := app.DB().NewQuery(`SELECT [[id]],[[type]],[[content]] FROM {{leafen_blocks}} WHERE [[page_id]]={:pageId}`).
		Bind(dbx.Params{"pageId": edit.pageID}).All(&rows)
	if err != nil {
		return nil, nil, nil, failedBecause(errMCPToolFailed, err)
	}
	threads := map[string]bool{}
	named := map[string]bool{edit.blockID: true}
	for _, thread := range edit.threads {
		threads[thread.ID] = true
	}
	for _, thread := range edit.threads {
		for _, run := range suggestionTextRuns(document.GetText(ygoBlockTextRootPrefix + thread.BlockID)) {
			if _, id := run.suggestion(); threads[id] {
				named[thread.BlockID] = true
			}
		}
	}
	there, suggested, code := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		there[row.ID], code[row.ID] = true, row.Type == graphBlockTypeCode
		content := map[string]any{}
		if json.Unmarshal([]byte(row.Content), &content) != nil {
			continue
		}
		if id, _ := content[graphBlockSuggestionKey].(string); threads[id] {
			named[row.ID], suggested[row.ID] = true, true
		}
		if id, _ := content[graphBlockSplitSuggestionKey].(string); threads[id] {
			after, _ := content[graphBlockSplitBlockKey].(string)
			named[row.ID], named[after] = true, true
		}
	}
	ids := []string{}
	for id := range named {
		if there[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return mcpBlocksInReadingOrder(app, edit.pageID, ids), suggested, code, nil
}

// mcpSuggestedEditURL names a suggestion's threads: its first one's
// discussion address, the others' after "?with=".
func mcpSuggestedEditURL(app core.App, workspaceID, pageID, blockID string, threads []string) string {
	address := mcpDiscussionID(app, workspaceID, pageID, blockID, threads[0])
	others := url.Values{}
	for _, thread := range threads[1:] {
		if thread != threads[0] {
			others.Add(notionMCPSuggestedPair, thread)
		}
	}
	if len(others) > 0 {
		address += "?" + others.Encode()
	}
	return address
}
