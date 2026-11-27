package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Blocks are suggested away whole, as Notion's suggesting mode does from
// "/" "삭제", a block's "⋮⋮" "삭제" and Backspace over chosen blocks (seen
// 2026-11-18 in Chrome, a page and a database row's side peek): each
// block's own text that stands is suggested away, struck through, under one
// suggestion; its card lists each block's words ("삭제: “four line”", then
// "삭제: “tog line”"), none for an empty block, its words those the block
// shows, text suggested into it included ("삭제: “one line ”"). Accepting
// takes the blocks away with all inside them; rejecting leaves them and
// their text as they were. A block suggested in is not suggested away (its
// own suggestion is rejected instead), nor is a block that holds no text,
// as Notion's "삭제" on a divider while suggesting does nothing (seen once,
// 2026-11-18); an empty text line is, its card listing no words.

// textSuggestionRemove is a request to suggest blocks away.
const textSuggestionRemove = "remove"

// suggestionRemovalBlocks is the most blocks one removal suggests away.
const suggestionRemovalBlocks = 1000

// suggestionRemovalProblem is what a removal request is refused for, "" for none.
func suggestionRemovalProblem(input suggestionCreateInput) string {
	switch {
	case input.Offset != 0 || input.Length != 0 || len(input.Marks) != 0 || input.Text != "" ||
		input.NewBlockID != "" || input.BlockType != "" || input.FirstChild:
		return "kind \"remove\" sends blockId and blockIds alone: no offset, length, marks, text, newBlockId, blockType or firstChild"
	case len(input.BlockIDs) >= suggestionRemovalBlocks:
		return fmt.Sprintf("a removal takes at most %d blocks", suggestionRemovalBlocks)
	}
	for _, id := range input.BlockIDs {
		if problem := commentIdentifierProblem("blockIds", id); problem != "" {
			return problem + ": the blocks after blockId the removal takes"
		}
	}
	return ""
}

// suggestionRemovalBlockIDs are the blocks a removal request names, blockId
// first, each once.
func suggestionRemovalBlockIDs(input suggestionCreateInput) []string {
	ids, seen := []string{input.BlockID}, map[string]struct{}{input.BlockID: {}}
	for _, id := range input.BlockIDs {
		if _, again := seen[id]; !again {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

// suggestionRemovalLines are each block's words as a removal's card lists
// them, as much as a summary holds in all.
func suggestionRemovalLines(texts []string) (string, []string) {
	left, lines := suggestionSummaryRunes, make([]string, 0, len(texts))
	for _, text := range texts {
		line := []rune(text)
		if len(line) > left {
			line = line[:left]
		}
		left -= len(line)
		lines = append(lines, string(line))
	}
	return suggestionSummary(strings.Join(lines, "")), lines
}

// createRemovalSuggestion suggests the named blocks away, one suggestion
// for them all, and answers with its thread.
func (p *pocketBaseYgoPersistence) createRemovalSuggestion(
	actorID string, surface suggestionSurface, input suggestionCreateInput, hash string,
) (commentMutationResponse, error) {
	response := commentMutationResponse{}
	ids := suggestionRemovalBlockIDs(input)
	err := p.runSuggestionWrite(surface, func(ctx context.Context, txApp core.App) error {
		authorization, err := authorizeSuggestionBody(txApp, actorID, surface)
		if err != nil {
			return err
		}
		if replay, err := readCommentReceipt(txApp, actorID, input.RequestID, hash, &response); err != nil || replay {
			response.Duplicate = replay
			return err
		}
		if err := refuseSuggestionBody(txApp, authorization, actorID); err != nil {
			return err
		}
		for _, id := range ids {
			suggested, err := checkSuggestionBlock(txApp, authorization, id)
			if err != nil {
				return err
			}
			if suggested != "" {
				return commentInvalid("block %q is suggested in: reject its suggestion rather than suggest it away", id)
			}
		}
		document, err := loadPageTextDocument(txApp, surface.Room)
		if err != nil {
			return err
		}
		texts, runs := make([]string, len(ids)), make([][]suggestionTextRun, len(ids))
		for index, id := range ids {
			text := document.GetText(ygoBlockTextRootPrefix + id)
			texts[index], runs[index] = text.ToString(), suggestionTextRuns(text)
		}
		update, changes := transactPageText(document, func(transaction *crdt.Transaction) {
			for index, id := range ids {
				markSuggestionRemoval(transaction.GetText(ygoBlockTextRootPrefix+id), transaction, runs[index], input.ID)
			}
		})
		if err := storePageSuggestionText(ctx, txApp, authorization, actorID, input.RequestID, update, changes); err != nil {
			return err
		}
		text, lines := suggestionRemovalLines(texts)
		body, err := json.Marshal(suggestionThreadBody{Rich: []any{}, Suggestion: suggestionThreadFacts{
			Kind: textSuggestionDelete, Text: text, Lines: lines, Removed: ids,
		}})
		if err != nil {
			return err
		}
		thread, err := insertCommentRow(txApp, authorization, authorization.Target, actorID, commentCreateInput{
			ID: input.ID, RequestID: input.RequestID, BlockID: input.BlockID, Body: body,
		}, time.Now().UTC())
		if err != nil {
			return err
		}
		record, err := renderComment(thread)
		if err != nil {
			return err
		}
		if err := attachCommentDetail(txApp, actorID, &record); err != nil {
			return err
		}
		response.Comment = record
		return insertCommentReceipt(
			txApp, actorID, thread.WorkspaceID, authorization.Target.StoragePageID, input.RequestID, hash, "create", response,
		)
	})
	return response, err
}

// markSuggestionRemoval suggests away the text of a block that stands, as a
// delete suggestion of the removal's; text already suggested stays as it is.
func markSuggestionRemoval(text *crdt.YText, transaction *crdt.Transaction, runs []suggestionTextRun, suggestionID string) {
	for index := len(runs) - 1; index >= 0; index-- {
		run := runs[index]
		if kind, id := run.suggestion(); kind == "" && id == "" {
			text.Format(transaction, run.Start, run.End-run.Start, crdt.Attributes{
				"suggestion": textSuggestionDelete, "suggestionId": suggestionID,
			})
		}
	}
}

// suggestionRemovedBlocks are the blocks a suggestion suggests away, none
// for a suggestion of another kind.
func (p *pocketBaseYgoPersistence) suggestionRemovedBlocks(actorID string, surface suggestionSurface, commentID string) ([]string, error) {
	removed := []string{}
	err := runReadSnapshot(context.Background(), p.app, func(txApp core.App) error {
		authorization, err := authorizeSuggestionBody(txApp, actorID, surface)
		if err != nil {
			return err
		}
		row, err := loadCommentForTarget(txApp, authorization.Target, commentID)
		if errors.Is(err, errCommentNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if body, isSuggestion := pageSuggestionThread(row); isSuggestion {
			removed = body.Suggestion.Removed
		}
		return nil
	})
	return removed, err
}

// resolveRemovalSuggestion accepts a removal, taking its blocks away with
// all inside them, or rejects it, leaving the blocks and their text as they
// were. One whose blocks are all gone settles its text alone.
func (p *pocketBaseYgoPersistence) resolveRemovalSuggestion(
	actorID string, surface suggestionSurface, commentID string, removed []string, input suggestionResolveInput, hash string, accept bool,
) (commentMutationResponse, error) {
	if accept {
		response, err := p.acceptRemovalSuggestion(actorID, surface, commentID, removed, input, hash)
		if !errors.Is(err, errNoSuggestedBlocks) {
			return response, err
		}
	}
	response := commentMutationResponse{}
	err := p.runSuggestionWrite(surface, func(ctx context.Context, txApp core.App) error {
		authorization, err := authorizeSuggestionBody(txApp, actorID, surface)
		if err != nil {
			return err
		}
		if replay, err := readCommentReceipt(txApp, actorID, input.RequestID, hash, &response); err != nil || replay {
			response.Duplicate = replay
			return err
		}
		if err := refuseSuggestionBody(txApp, authorization, actorID); err != nil {
			return err
		}
		row, body, err := checkSuggestionSettle(txApp, authorization, actorID, commentID, input.ExpectedRevision, accept)
		if err != nil {
			return err
		}
		document, err := loadPageTextDocument(txApp, surface.Room)
		if err != nil {
			return err
		}
		runs := make([][]suggestionTextRun, len(removed))
		for index, id := range removed {
			runs[index] = suggestionTextRuns(document.GetText(ygoBlockTextRootPrefix + id))
		}
		update, changes := transactPageText(document, func(transaction *crdt.Transaction) {
			for index, id := range removed {
				settleSuggestion(transaction, transaction.GetText(ygoBlockTextRootPrefix+id), runs[index], commentID, accept)
			}
		})
		if err := storePageSuggestionText(ctx, txApp, authorization, actorID, input.RequestID, update, changes); err != nil {
			return err
		}
		response, err = finishSuggestionSettle(txApp, authorization, actorID, row, body, input.RequestID, hash, accept)
		return err
	})
	return response, err
}

// acceptRemovalSuggestion takes a removal's blocks that are still there
// away, with all inside them, as one change to the page's content.
func (p *pocketBaseYgoPersistence) acceptRemovalSuggestion(
	actorID string, surface suggestionSurface, commentID string, removed []string, input suggestionResolveInput, hash string,
) (commentMutationResponse, error) {
	pageID := surface.Room
	response := commentMutationResponse{}
	if replayed, err := p.replaySuggestionRequest(actorID, surface, input.RequestID, hash, &response); err != nil || replayed {
		return response, err
	}
	authorization := commentAuthorization{}
	row, body := storedComment{}, suggestionThreadBody{}
	hooks := contentGraphMutationHooks{
		Authorize: func(txApp core.App) (pageAuthorization, error) {
			var err error
			if authorization, err = authorizeSuggestionBody(txApp, actorID, surface); err != nil {
				return pageAuthorization{}, err
			}
			if err := refuseSuggestionBody(txApp, authorization, actorID); err != nil {
				return pageAuthorization{}, err
			}
			return authorization.Page, nil
		},
		PrepareInput: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc, next graphMutationInput) (graphMutationInput, error) {
			var err error
			if row, body, err = checkSuggestionSettle(txApp, authorization, actorID, commentID, input.ExpectedRevision, true); err != nil {
				return next, err
			}
			metadata, err := structuralMetadata(doc)
			if err != nil {
				return next, err
			}
			for _, id := range removed {
				if _, there := metadata[id]; there {
					next.Operations = append(next.Operations, graphOperation{
						Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: id},
					})
				}
			}
			if len(next.Operations) == 0 {
				return next, errNoSuggestedBlocks
			}
			return next, nil
		},
		AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
			var err error
			response, err = finishSuggestionSettle(txApp, authorization, actorID, row, body, input.RequestID, hash, true)
			return err
		},
	}
	if _, err := p.applyContentGraphMutationContext(surface.requestContext(), actorID, pageID, graphMutationInput{MutationID: suggestionMutationID(hash)}, hooks); err != nil || response.Comment.ID != "" {
		return response, err
	}
	_, err := p.replaySuggestionRequest(actorID, surface, input.RequestID, hash, &response)
	return response, err
}
