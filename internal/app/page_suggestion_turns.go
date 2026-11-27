package app

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A block the person suggested in, still open, turns into another kind of
// text block where it stands, as Notion's "/" does on a suggested line (seen
// 2026-11-18 in Chrome, a page and a database page: "/" on the empty line
// Enter suggested, "제목1" or "번호 매기기 목록" picked — the line is that
// block, suggested). A block that stands, or someone else's suggestion, is
// not turned.
//
// The picked block is a suggestion of its own, apart from the break that
// made its line, as Notion's cards are (seen 2026-11-18, a page and a
// database page's side peek): "추가: 줄바꿈" stays on the line before, with
// its "⏎", and the block's card reads "추가: 블록", then "추가: “hd”" once
// words go in. Each settles alone: accepting the block keeps it and the
// break's card stays; rejecting it takes the block away and the break's card
// stays with its "⏎"; accepting the break then drops the mark. Notion
// doesn't take the break's rejection (pressed twice, once with the block
// accepted and once with it rejected: nothing changed), so the break isn't
// rejected apart from its block (resolveSplitSuggestion). The break is the
// line's suggested break (page_suggestion_splits.go) when the line was its
// suggestion's only block; one of several stays with the others.

// turnBlockSuggestion turns the person's own open suggested block into
// input.BlockType, a suggestion of its own (input.ID), and answers with that
// suggestion's thread. With stay it turns within the suggestion it is in,
// which answers instead: as Notion's Enter on an empty suggested list item
// turns it to text, and a "/" row picked on a block a menu picked turns it,
// each card staying as it was (measured 2026-11-18 in Chrome at 1440: "추가:
// 줄바꿈" then "추가: “q”" in one card; "추가: 블록" then "추가: “k”").
func (p *pocketBaseYgoPersistence) turnBlockSuggestion(
	actorID string, surface suggestionSurface, input suggestionCreateInput, hash string,
) (commentMutationResponse, error) {
	pageID := surface.Room
	response := commentMutationResponse{}
	if replayed, err := p.replaySuggestionRequest(actorID, surface, input.RequestID, hash, &response); err != nil || replayed {
		return response, err
	}
	threadID, anchorID, others := "", "", []string{}
	authorization := commentAuthorization{}
	_, err := p.applyContentGraphMutationContext(surface.requestContext(), actorID, pageID, graphMutationInput{MutationID: suggestionMutationID(hash)}, contentGraphMutationHooks{
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
			metadata, err := structuralMetadata(doc)
			if err != nil {
				return next, err
			}
			entry, found := metadata[input.BlockID]
			if !found {
				return next, errCommentBlockScope
			}
			if _, suggestable := suggestionBlockTypes[entry.Type]; !suggestable || entry.Type == graphBlockTypeCode {
				return next, errCommentInvalid
			}
			threadID, _ = entry.Content[graphBlockSuggestionKey].(string)
			own, err := ownOpenSuggestion(txApp, authorization.Target, actorID, threadID, textSuggestionInsert)
			if err != nil {
				return next, err
			}
			if !own {
				return next, commentInvalid("kind \"turn\" changes a block you suggested in, while its suggestion is open: block %q isn't one", input.BlockID)
			}
			// The break before the line is the one into it from the block
			// its suggestion started in, when that is its only block.
			blocks, _, err := structuralSuggestedBlocks(doc, threadID)
			if err != nil {
				return next, err
			}
			others = slices.DeleteFunc(blocks, func(id string) bool { return id == input.BlockID })
			if thread, err := loadCommentForTarget(txApp, authorization.Target, threadID); err != nil {
				return next, err
			} else if anchor, there := metadata[thread.BlockID]; there && len(others) == 0 && thread.BlockID != input.BlockID {
				// A block holds one suggested break at its end.
				if split, _ := anchor.Content[graphBlockSplitSuggestionKey].(string); split == "" {
					anchorID = thread.BlockID
				}
			}
			document, err := loadPageTextDocument(txApp, pageID)
			if err != nil {
				return next, err
			}
			if document.GetText(ygoBlockTextRootPrefix+input.BlockID).ToString() != input.ExpectedText {
				return next, errSuggestionTextChanged
			}
			// The block stays where it is, with what it holds.
			content, err := json.Marshal(entry.Content)
			if err != nil {
				return next, err
			}
			var parentID *string
			if entry.ParentID != "" {
				parentID = &entry.ParentID
			}
			next.Operations = []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
				ID: input.BlockID, ParentID: parentID, Position: "p", Type: input.BlockType, Content: content,
			}}}
			return next, nil
		},
		AfterApply: func(txApp core.App, page pageAuthorization, doc *crdt.Doc) error {
			owner := page.Workspace.OwnerID
			if input.Stay {
				return answerSuggestionThread(txApp, authorization, actorID, threadID, input.RequestID, hash, &response)
			}
			if err := setStructuralBlockSuggestion(txApp, doc, owner, pageID, input.BlockID, input.ID); err != nil {
				return err
			}
			if err := keepTurnedBreak(txApp, authorization, doc, owner, pageID, threadID, anchorID, input.BlockID, others); err != nil {
				return err
			}
			body, err := json.Marshal(suggestionThreadBody{Rich: []any{}, Suggestion: suggestionThreadFacts{
				Kind: textSuggestionInsert, Blocks: 1, Lines: []string{""}, Picked: true,
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
		},
	})
	if err != nil || response.Comment.ID != "" {
		return response, err
	}
	// The same request made the change at the same time.
	_, err = p.replaySuggestionRequest(actorID, surface, input.RequestID, hash, &response)
	return response, err
}

// keepTurnedBreak leaves the suggestion that made a turned line with the
// break into it: the break marked on the block it started in, or, when the
// suggestion has other blocks, its facts as they now are.
func keepTurnedBreak(
	app core.App, authorization commentAuthorization, doc *crdt.Doc, owner, pageID, threadID, anchorID, turnedID string, others []string,
) error {
	row, err := loadCommentForTarget(app, authorization.Target, threadID)
	if err != nil {
		return err
	}
	body, _ := pageSuggestionThread(row)
	if anchorID != "" {
		if err := setStructuralBlockSplit(app, doc, owner, pageID, anchorID, threadID, turnedID); err != nil {
			return err
		}
		body.Suggestion.Turned = true
	} else {
		document, err := loadPageTextDocument(app, pageID)
		if err != nil {
			return err
		}
		text, lines := suggestionNewLines(document, row.BlockID, others, threadID)
		body.Suggestion.Text, body.Suggestion.Blocks = suggestionSummary(text), len(others)
		body.Suggestion.Head, body.Suggestion.Lines = lines.Head, lines.Lines
	}
	if _, err := rewriteSuggestionThread(app, row, body, time.Now().UTC()); err != nil {
		return err
	}
	return advanceCommentPageRevision(app, authorization.Target.StoragePageID)
}

// answerSuggestionThread answers a suggestion request with a thread as it
// stands, kept for the request's replay.
func answerSuggestionThread(
	app core.App, authorization commentAuthorization, actorID, threadID, requestID, hash string, response *commentMutationResponse,
) error {
	thread, err := loadCommentForTarget(app, authorization.Target, threadID)
	if err != nil {
		return err
	}
	record, err := renderComment(thread)
	if err != nil {
		return err
	}
	if err := attachCommentDetail(app, actorID, &record); err != nil {
		return err
	}
	response.Comment = record
	return insertCommentReceipt(app, actorID, thread.WorkspaceID, authorization.Target.StoragePageID, requestID, hash, "create", *response)
}
