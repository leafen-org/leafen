package app

import (
	"encoding/base64"
	"slices"
	"sort"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A line break suggested right after the person's own suggested text goes
// on with that suggestion, as Notion's does (seen 2026-11-15 in Chrome, two
// ways): the break is part of the text suggested in, so no "⏎" marks it and
// the suggestion's summary names no line break for it, and the new line's
// text is suggested under the same thread — one card, "추가: “gh”" then
// "추가: “jk”". A break after text that stands, or on an empty line, is
// marked and named as before.

// suggestionLines are a suggestion of new blocks' text as its summary lists
// it: what it holds at the end of the block it started in (its head, when
// it grew from text suggested there), then each of its blocks' in order.
type suggestionLines struct {
	Head  string
	Lines []string
}

// ownSuggestionBefore is the person's own open suggestion of text that runs
// up to a UTF-16 offset of a block's text, or "" when the text before the
// offset isn't theirs to go on with.
func ownSuggestionBefore(app core.App, target nativeCommentTarget, actorID string, text *crdt.YText, offset int) (string, error) {
	for _, run := range suggestionTextRuns(text) {
		if run.Start >= offset || run.End < offset {
			continue
		}
		kind, id := run.suggestion()
		if kind != textSuggestionInsert {
			return "", nil
		}
		own, err := ownOpenSuggestion(app, target, actorID, id, textSuggestionInsert)
		if err != nil || !own {
			return "", err
		}
		return id, nil
	}
	return "", nil
}

// suggestionRunsHold reports whether a block's text holds a suggestion's.
func suggestionRunsHold(runs []suggestionTextRun, suggestionID string) bool {
	return slices.ContainsFunc(runs, func(run suggestionTextRun) bool {
		_, id := run.suggestion()
		return id == suggestionID
	})
}

// suggestionNewLines is the text a suggestion of new blocks holds, all of
// it, and as its summary lists it: its head in the block it started in
// (anchorID), unless that block is one of its own, then its blocks' lines
// (suggestionBlockLines), as much as a summary holds in all.
func suggestionNewLines(document *crdt.Doc, anchorID string, blocks []string, suggestionID string) (string, suggestionLines) {
	head := ""
	if anchorID != "" && !slices.Contains(blocks, anchorID) {
		head = suggestionTexts(document.GetText(ygoBlockTextRootPrefix + anchorID))[suggestionID]
	}
	shown := suggestionSummary(head)
	text, lines := suggestionBlockLines(document, blocks, suggestionID, suggestionSummaryRunes-len([]rune(shown)))
	return head + text, suggestionLines{Head: shown, Lines: lines}
}

// structuralThreadBlocks are the blocks whose text a suggestion's summary
// lists after its head, from the page's structure: those it suggested in, in
// the page's order, then the block after a break it suggested in a block's
// text (a break that went on with its text there).
func structuralThreadBlocks(doc *crdt.Doc, suggestionID string) ([]string, error) {
	ids, metadata, err := structuralSuggestedBlocks(doc, suggestionID)
	if err != nil {
		return nil, err
	}
	at := map[string]int{}
	for _, id := range ids {
		order, err := structuralOrder(doc, metadata[id].ParentID)
		if err != nil {
			return nil, err
		}
		at[id] = structuralIndex(order, id)
	}
	sort.SliceStable(ids, func(a, b int) bool {
		if first, second := metadata[ids[a]].ParentID, metadata[ids[b]].ParentID; first != second {
			return first < second
		}
		return at[ids[a]] < at[ids[b]]
	})
	for _, entry := range metadata {
		split, _ := entry.Content[graphBlockSplitSuggestionKey].(string)
		after, _ := entry.Content[graphBlockSplitBlockKey].(string)
		if _, there := metadata[after]; split == suggestionID && there {
			ids = append(ids, after)
		}
	}
	return ids, nil
}

// settleBlockSuggestionTexts accepts or rejects a suggestion's text in
// blocks, as a change to the page's collaborative text.
func settleBlockSuggestionTexts(app core.App, pageID, actorID string, blocks []string, suggestionID string, accept bool) (string, error) {
	document, err := loadPageTextDocument(app, pageID)
	if err != nil {
		return "", err
	}
	runs := make(map[string][]suggestionTextRun, len(blocks))
	for _, id := range blocks {
		runs[id] = suggestionTextRuns(document.GetText(ygoBlockTextRootPrefix + id))
	}
	update, changes := transactPageText(document, func(transaction *crdt.Transaction) {
		for _, id := range blocks {
			settleSuggestion(transaction, transaction.GetText(ygoBlockTextRootPrefix+id), runs[id], suggestionID, accept)
		}
	})
	if len(update) == 0 {
		return "", nil
	}
	if err := appendYgoUpdate(app, pageID, update); err != nil {
		return "", err
	}
	publishStoredYgoUpdateAfterCommit(app, pageID, update)
	if err := recordPageHistoryCollaborationUpdate(app, pageID, actorID, update, changes); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(update), nil
}
