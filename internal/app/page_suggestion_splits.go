package app

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"time"
	"unicode/utf16"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A line break suggested in a text block splits the block at once, as
// Notion's does, and only the break is suggested. In the middle of the text,
// the text after the break moves to a new block right after it; at the start
// of the text, a new empty block goes right before it. The block before the
// break holds the suggestion's thread (splitSuggestionId) and the block after
// the break (splitBlockId), and shows the break at its end; readers see both
// blocks. Accepting keeps both blocks and drops the mark; rejecting joins
// them back into the block that was there before
// (page_suggestion_split_join.go).

const (
	graphBlockSplitSuggestionKey = "splitSuggestionId"
	graphBlockSplitBlockKey      = "splitBlockId"
)

// withoutGraphBlockSplit is block content without a suggested break's mark:
// only the suggestion routes set one.
func withoutGraphBlockSplit(content json.RawMessage) (json.RawMessage, error) {
	object := map[string]any{}
	if err := json.Unmarshal(content, &object); err != nil || object == nil {
		return nil, graphInputError(errorCodeGraphInvalid, "Check the content structure.")
	}
	_, suggested := object[graphBlockSplitSuggestionKey]
	_, made := object[graphBlockSplitBlockKey]
	if !suggested && !made {
		return content, nil
	}
	delete(object, graphBlockSplitSuggestionKey)
	delete(object, graphBlockSplitBlockKey)
	return json.Marshal(object)
}

// setStructuralBlockSplit marks a suggested break at a block's end, with the
// block after the break, or clears it for "", in the page's structure and
// block row.
func setStructuralBlockSplit(app core.App, doc *crdt.Doc, owner, pageID, blockID, suggestionID, splitBlockID string) error {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return err
	}
	entry, ok := metadata[blockID]
	if !ok {
		return graphNotFoundError()
	}
	content := make(map[string]any, len(entry.Content)+2)
	for key, value := range entry.Content {
		if key != graphBlockSplitSuggestionKey && key != graphBlockSplitBlockKey {
			content[key] = value
		}
	}
	if suggestionID != "" {
		content[graphBlockSplitSuggestionKey] = suggestionID
		content[graphBlockSplitBlockKey] = splitBlockID
	}
	doc.Transact(func(transaction *crdt.Transaction) {
		transaction.GetMap(ygoBlockMetadataRoot).Set(transaction, blockID, map[string]any{
			"parentId": entry.ParentID, "type": entry.Type, "content": content,
		})
	})
	encoded, err := json.Marshal(content)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_blocks}} SET [[content]]={:content}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:id}
	`).Bind(dbx.Params{"content": string(encoded), "owner": owner, "pageId": pageID, "id": blockID}).Execute()
	return err
}

// pageHasSplitSuggestion reports whether a suggestion is a break suggested
// in a page's block, from the page's block rows.
func pageHasSplitSuggestion(app core.App, pageID, suggestionID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_blocks}}
		WHERE [[page_id]]={:pageId} AND json_valid([[content]])
			AND json_extract([[content]], '$.splitSuggestionId')={:suggestionId}
	`).Bind(dbx.Params{"pageId": pageID, "suggestionId": suggestionID}).Row(&count)
	return count > 0, err
}

// createSplitSuggestion suggests a line break in a text block: the block
// splits at once and the break is the suggestion.
func (p *pocketBaseYgoPersistence) createSplitSuggestion(
	actorID string, surface suggestionSurface, input suggestionCreateInput, hash string,
) (commentMutationResponse, error) {
	pageID := surface.Room
	response := commentMutationResponse{}
	if replayed, err := p.replaySuggestionRequest(actorID, surface, input.RequestID, hash, &response); err != nil || replayed {
		return response, err
	}
	authorization := commentAuthorization{}
	// A break at the start of the text makes the block before it; the break
	// then ends the new block, and the block keeps its text.
	atStart := input.Offset == 0
	markedID, afterBreakID := input.BlockID, input.NewBlockID
	if atStart {
		markedID, afterBreakID = input.NewBlockID, input.BlockID
	}
	// A break suggested earlier at the block's end now ends the new block.
	carried, carriedBlock := "", ""
	// The person's own suggestion of text running up to the break, which the
	// break goes on with (page_suggestion_lines.go), and whether the text
	// after the break is all of that suggestion: the new block is then one of
	// its blocks rather than a break in the block's text.
	grown, whole := "", false
	_, err := p.applyContentGraphMutationContext(surface.requestContext(), actorID, pageID, graphMutationInput{MutationID: suggestionMutationID(hash)}, contentGraphMutationHooks{
		ExplicitStartBlockIDs: map[string]struct{}{input.NewBlockID: {}},
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
			anchor, found := metadata[input.BlockID]
			if !found {
				return next, errCommentBlockScope
			}
			if _, suggestable := suggestionBlockTypes[anchor.Type]; !suggestable || anchor.Type == graphBlockTypeCode {
				return next, errCommentInvalid
			}
			if _, taken := metadata[input.NewBlockID]; taken {
				return next, errCommentIDConflict
			}
			document, err := loadPageTextDocument(txApp, pageID)
			if err != nil {
				return next, err
			}
			text := document.GetText(ygoBlockTextRootPrefix + input.BlockID)
			if text.ToString() != input.ExpectedText {
				return next, errSuggestionTextChanged
			}
			var parentID *string
			if anchor.ParentID != "" {
				parentID = &anchor.ParentID
			}
			block := &graphBlock{
				ID: input.NewBlockID, ParentID: parentID, Position: "p",
				Type: input.BlockType, Content: json.RawMessage(`{}`),
			}
			next.Operations = []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: block}}
			if atStart {
				// The new block goes right before the block: after the sibling
				// before it, or first.
				order, err := structuralOrder(doc, anchor.ParentID)
				if err != nil {
					return next, err
				}
				if at := structuralIndex(order, input.BlockID); at > 0 {
					block.AfterID = &order[at-1]
				}
				block.Content, err = startSplitBlockContent(anchor, input.BlockType)
				return next, err
			}
			// Suggested text after the break stays in the block its thread
			// names: such a break isn't taken, unless it is the text of the
			// suggestion the break goes on with.
			if grown, err = ownSuggestionBefore(txApp, authorization.Target, actorID, text, input.Offset); err != nil {
				return next, err
			}
			whole = grown != ""
			for _, run := range suggestionTextRuns(text) {
				if kind, id := run.suggestion(); run.End <= input.Offset {
					continue
				} else if kind == "" {
					whole = false
				} else if kind != textSuggestionInsert || id != grown {
					return next, errCommentInvalid
				}
			}
			carried, _ = anchor.Content[graphBlockSplitSuggestionKey].(string)
			carriedBlock, _ = anchor.Content[graphBlockSplitBlockKey].(string)
			anchorID := input.BlockID
			block.AfterID = &anchorID
			return next, nil
		},
		ApplyAdditionalText: func(txApp core.App, _ pageAuthorization) (string, error) {
			if atStart {
				return "", nil
			}
			return moveSuggestionSplitText(txApp, pageID, actorID, input.BlockID, input.NewBlockID, input.Offset)
		},
		AfterApply: func(txApp core.App, page pageAuthorization, doc *crdt.Doc) error {
			owner := page.Workspace.OwnerID
			if whole {
				if err := setStructuralBlockSuggestion(txApp, doc, owner, pageID, input.NewBlockID, grown); err != nil {
					return err
				}
			} else if err := setStructuralBlockSplit(txApp, doc, owner, pageID, markedID, cmp.Or(grown, input.ID), afterBreakID); err != nil {
				return err
			}
			if carried != "" {
				if err := setStructuralBlockSplit(txApp, doc, owner, pageID, input.NewBlockID, carried, carriedBlock); err != nil {
					return err
				}
			}
			thread, err := storedComment{}, error(nil)
			if grown != "" {
				thread, err = saveBlockSuggestionThread(txApp, authorization, actorID, input, grown, true, doc)
			} else {
				thread, err = insertSplitSuggestionThread(txApp, authorization, actorID, input, markedID, atStart)
			}
			if err != nil {
				return err
			}
			record, err := renderComment(thread)
			if err != nil {
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

// insertSplitSuggestionThread starts the thread of a suggested break, on the
// block before it, noting a break at the start of the block's text.
func insertSplitSuggestionThread(
	app core.App, authorization commentAuthorization, actorID string, input suggestionCreateInput, markedID string, atStart bool,
) (storedComment, error) {
	body, err := json.Marshal(suggestionThreadBody{Rich: []any{}, Suggestion: suggestionThreadFacts{
		Kind: textSuggestionInsert, Blocks: 1, AtStart: atStart,
	}})
	if err != nil {
		return storedComment{}, err
	}
	return insertCommentRow(app, authorization, authorization.Target, actorID, commentCreateInput{
		ID: input.ID, RequestID: input.RequestID, BlockID: markedID, Body: body,
	}, time.Now().UTC())
}

// resolveSplitSuggestion accepts a suggested break, keeping both blocks, or
// rejects it, joining them back into the block that was there before
// (planSuggestionSplitJoin); the suggestion's text on either side of the
// break settles with it.
func (p *pocketBaseYgoPersistence) resolveSplitSuggestion(
	actorID string, surface suggestionSurface, commentID string, input suggestionResolveInput, hash string, accept bool,
) (commentMutationResponse, error) {
	pageID := surface.Room
	response := commentMutationResponse{}
	if replayed, err := p.replaySuggestionRequest(actorID, surface, input.RequestID, hash, &response); err != nil || replayed {
		return response, err
	}
	authorization := commentAuthorization{}
	row, body := storedComment{}, suggestionThreadBody{}
	anchorID, splitID, joining, splitThere := "", "", false, false
	join := suggestionSplitJoin{}
	hooks := contentGraphMutationHooks{
		AllowBlockUpdateBeforeDelete: true,
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
			if row, body, err = checkSuggestionSettle(txApp, authorization, actorID, commentID, input.ExpectedRevision, accept); err != nil {
				return next, err
			}
			if !accept && body.Suggestion.Turned {
				return next, commentInvalid("the break before a block picked from \"/\" is accepted, not rejected: reject the block's own suggestion")
			}
			metadata, err := structuralMetadata(doc)
			if err != nil {
				return next, err
			}
			for id, entry := range metadata {
				if suggestionID, _ := entry.Content[graphBlockSplitSuggestionKey].(string); suggestionID == commentID {
					anchorID = id
					splitID, _ = entry.Content[graphBlockSplitBlockKey].(string)
				}
			}
			if anchorID == "" {
				return next, errNoSuggestedBlocks
			}
			// Accepted, or with the block after the break gone, the block
			// before it stays where it is, as it is; rejected, the two join.
			if _, splitThere = metadata[splitID]; !accept && splitThere {
				joining = true
				var operations []graphOperation
				if join, operations, err = planSuggestionSplitJoin(doc, metadata, anchorID, splitID, commentID, body.Suggestion.AtStart); err != nil {
					return next, err
				}
				next.Operations = append(next.Operations, operations...)
				return next, nil
			}
			anchor, err := suggestionSplitJoinBlock(anchorID, metadata[anchorID].ParentID, nil, metadata[anchorID])
			if err != nil {
				return next, err
			}
			next.Operations = append(next.Operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: anchor})
			return next, nil
		},
		// The block that goes gives its text back before it goes, so its
		// removal has no text left to clear; the suggestion's text goes with
		// the break. Accepted, its text on either side stays.
		BeforeApplyOperations: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc, _ []graphOperation) error {
			settled := []string{anchorID}
			if joining {
				if err := join.text(txApp, pageID, actorID); err != nil {
					return err
				}
				settled = []string{join.kept}
			} else if splitThere {
				settled = append(settled, splitID)
			}
			_, err := settleBlockSuggestionTexts(txApp, pageID, actorID, settled, commentID, accept)
			return err
		},
		AfterApply: func(txApp core.App, page pageAuthorization, doc *crdt.Doc) error {
			marked, mark, markBlock := anchorID, "", ""
			if joining {
				marked, mark, markBlock = join.kept, join.mark, join.markBlock
			}
			if err := setStructuralBlockSplit(txApp, doc, page.Workspace.OwnerID, pageID, marked, mark, markBlock); err != nil {
				return err
			}
			var err error
			response, err = finishSuggestionSettle(txApp, authorization, actorID, row, body, input.RequestID, hash, accept)
			return err
		},
	}
	if _, err := p.applyContentGraphMutationContext(surface.requestContext(), actorID, pageID, graphMutationInput{MutationID: suggestionMutationID(hash)}, hooks); err != nil || response.Comment.ID != "" {
		return response, err
	}
	_, err := p.replaySuggestionRequest(actorID, surface, input.RequestID, hash, &response)
	return response, err
}

// moveSuggestionSplitText moves a block's text after a break to the start of
// the block the break made, formatting and all, as a change to the page's
// collaborative text.
func moveSuggestionSplitText(app core.App, pageID, actorID, blockID, newBlockID string, offset int) (string, error) {
	return changeSuggestionSplitText(app, pageID, actorID, func(document *crdt.Doc) func(*crdt.Transaction) {
		tail := suggestionTextDeltaFrom(document.GetText(ygoBlockTextRootPrefix+blockID).ToDelta(), offset)
		return func(transaction *crdt.Transaction) {
			if len(tail) > 0 {
				transaction.GetText(ygoBlockTextRootPrefix+newBlockID).ApplyDelta(transaction, tail)
			}
			from := transaction.GetText(ygoBlockTextRootPrefix + blockID)
			if length := from.Len(); length > offset {
				from.Delete(transaction, offset, length-offset)
			}
		}
	})
}

// changeSuggestionSplitText makes a change to a page's collaborative text
// and stores it as the page's other text changes are stored. The change is
// planned from the document before its transaction, where a text's delta
// can't be read.
func changeSuggestionSplitText(
	app core.App, pageID, actorID string, plan func(*crdt.Doc) func(*crdt.Transaction),
) (string, error) {
	document, err := loadPageTextDocument(app, pageID)
	if err != nil {
		return "", err
	}
	update, changes := transactPageText(document, plan(document))
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

// suggestionTextDeltaFrom is a text's delta from a UTF-16 offset on, as
// inserts that keep their formatting.
func suggestionTextDeltaFrom(delta []crdt.Delta, offset int) []crdt.Delta {
	tail := []crdt.Delta{}
	position := 0
	for _, item := range delta {
		if item.Op != crdt.DeltaOpInsert {
			continue
		}
		content, isText := item.Insert.(string)
		if !isText {
			if position >= offset {
				tail = append(tail, crdt.Delta{Op: crdt.DeltaOpInsert, Insert: item.Insert, Attributes: item.Attributes})
			}
			position++
			continue
		}
		units := utf16.Encode([]rune(content))
		end := position + len(units)
		if end > offset {
			start := 0
			if offset > position {
				start = offset - position
			}
			tail = append(tail, crdt.Delta{
				Op: crdt.DeltaOpInsert, Insert: string(utf16.Decode(units[start:])), Attributes: item.Attributes,
			})
		}
		position = end
	}
	return tail
}
