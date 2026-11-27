package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A block suggested into a page is a real block whose content holds its
// suggestion's thread (suggestionId), with its text suggested in under the
// same thread: pressing Enter at the end of a text block in suggesting mode
// suggests the next block. Readers that show a page as it stands leave it
// out, as Notion's Public API does, while Notion's page Markdown and the
// editor keep it. Only the suggestion routes suggest a block or settle one:
// accepting keeps the block as a plain block, and rejecting removes it.

const graphBlockSuggestionKey = "suggestionId"

// withGraphBlockSuggestion is block content with its suggestion set, or
// without one for "".
func withGraphBlockSuggestion(content json.RawMessage, suggestionID string) (json.RawMessage, error) {
	object := map[string]any{}
	if err := json.Unmarshal(content, &object); err != nil || object == nil {
		return nil, graphInputError(errorCodeGraphInvalid, "Check the content structure.")
	}
	_, alone := object[graphBlockSuggestionAloneKey]
	if _, present := object[graphBlockSuggestionKey]; !present && !alone && suggestionID == "" {
		return content, nil
	}
	delete(object, graphBlockSuggestionKey)
	delete(object, graphBlockSuggestionAloneKey)
	if suggestionID != "" {
		object[graphBlockSuggestionKey] = suggestionID
	}
	return json.Marshal(object)
}

// graphBlockSuggestion is the suggestion a block's stored content holds.
func graphBlockSuggestion(content string) string {
	object := map[string]any{}
	if json.Unmarshal([]byte(content), &object) != nil {
		return ""
	}
	suggestionID, _ := object[graphBlockSuggestionKey].(string)
	return suggestionID
}

// blockStandsSQL is the condition that a block row, by its table alias or
// "" for none, is not a suggested block: readers that show a page as it
// stands leave suggested blocks out.
func blockStandsSQL(alias string) string {
	column := "[[content]]"
	if alias != "" {
		column = "[[" + alias + "]].[[content]]"
	}
	return "(NOT json_valid(" + column + ") OR COALESCE(json_extract(" + column + ", '$.suggestionId'), '') = '')"
}

// graphItemsAsTheyStand are a page's content graph items without its
// suggested blocks, each block after the one it now follows.
func graphItemsAsTheyStand(items []graphReadItem) []graphReadItem {
	removed := map[string]any{}
	for _, item := range items {
		content, _ := item.Value["content"].(map[string]any)
		if suggestionID, _ := content[graphBlockSuggestionKey].(string); item.Kind == graphReadKindBlock && suggestionID != "" {
			id, _ := item.Value["id"].(string)
			removed[id] = item.Value["afterId"]
		}
	}
	if len(removed) == 0 {
		return items
	}
	result := make([]graphReadItem, 0, len(items)-len(removed))
	for _, item := range items {
		id, _ := item.Value["id"].(string)
		if _, gone := removed[id]; item.Kind == graphReadKindBlock && gone {
			continue
		}
		if item.Kind == graphReadKindBlock {
			for after, _ := item.Value["afterId"].(string); after != ""; after, _ = item.Value["afterId"].(string) {
				previous, gone := removed[after]
				if !gone {
					break
				}
				value := make(map[string]any, len(item.Value))
				for key, field := range item.Value {
					value[key] = field
				}
				value["afterId"] = previous
				item.Value = value
			}
		}
		result = append(result, item)
	}
	return result
}

// setStructuralBlockSuggestion marks a block suggested, or settles it for
// "", in the page's structure and its block row.
func setStructuralBlockSuggestion(app core.App, doc *crdt.Doc, owner, pageID, blockID, suggestionID string) error {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return err
	}
	entry, ok := metadata[blockID]
	if !ok {
		return graphNotFoundError()
	}
	content := make(map[string]any, len(entry.Content)+1)
	for key, value := range entry.Content {
		if key != graphBlockSuggestionKey && key != graphBlockSuggestionAloneKey {
			content[key] = value
		}
	}
	if suggestionID != "" {
		content[graphBlockSuggestionKey] = suggestionID
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

// structuralSuggestedBlocks are the blocks a suggestion suggested into a
// page, from the page's structure.
func structuralSuggestedBlocks(doc *crdt.Doc, suggestionID string) ([]string, map[string]structuralBlockMetadata, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return nil, nil, err
	}
	ids := []string{}
	for id, entry := range metadata {
		if value, _ := entry.Content[graphBlockSuggestionKey].(string); suggestionID != "" && value == suggestionID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, metadata, nil
}

// pageHasSuggestedBlocks reports whether a suggestion suggested blocks into
// a page, from the page's block rows.
func pageHasSuggestedBlocks(app core.App, pageID, suggestionID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_blocks}}
		WHERE [[page_id]]={:pageId} AND json_valid([[content]])
			AND json_extract([[content]], '$.suggestionId')={:suggestionId}
	`).Bind(dbx.Params{"pageId": pageID, "suggestionId": suggestionID}).Row(&count)
	return count > 0, err
}

// suggestionMutationID names the content change a suggestion request makes.
func suggestionMutationID(requestHash string) string {
	digest := sha256.Sum256([]byte("leafen:page-suggestion:v1\x00" + requestHash))
	return "suggestion-" + hex.EncodeToString(digest[:16])
}

// ownOpenSuggestion reports whether a thread is the person's own open
// suggestion of a kind.
func ownOpenSuggestion(app core.App, target nativeCommentTarget, actorID, suggestionID, kind string) (bool, error) {
	if suggestionID == "" {
		return false, nil
	}
	row, err := loadCommentForTarget(app, target, suggestionID)
	if errors.Is(err, errCommentNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	body, isSuggestion := pageSuggestionThread(row)
	return isSuggestion && body.Suggestion.Kind == kind && body.Suggestion.State == "" &&
		!row.Resolved && row.AuthorID == actorID, nil
}

// replaySuggestionRequest answers a suggestion request already made, as it
// was answered.
func (p *pocketBaseYgoPersistence) replaySuggestionRequest(
	actorID string, surface suggestionSurface, requestID, hash string, response *commentMutationResponse,
) (bool, error) {
	replayed := false
	err := runReadSnapshot(context.Background(), p.app, func(txApp core.App) error {
		if _, err := authorizeSuggestionBody(txApp, actorID, surface); err != nil {
			return err
		}
		var err error
		replayed, err = readCommentReceipt(txApp, actorID, requestID, hash, response)
		return err
	})
	response.Duplicate = replayed
	return replayed, err
}

// suggestionToggleTypes are the toggles whose Enter, open, suggests their
// first child.
var suggestionToggleTypes = map[string]struct{}{
	graphBlockTypeToggle: {}, graphBlockTypeToggleHeading1: {}, graphBlockTypeToggleHeading2: {},
	graphBlockTypeToggleHeading3: {}, graphBlockTypeToggleHeading4: {},
}

// createBlockSuggestion suggests a new block after a text block's end, or
// as an open toggle's first child, with its first text: the next block after
// the person's own open suggested block, or after their own open suggested
// text at the block's end, grows that suggestion (page_suggestion_lines.go),
// unless a menu picked it (page_suggestion_picked.go).
func (p *pocketBaseYgoPersistence) createBlockSuggestion(
	actorID string, surface suggestionSurface, input suggestionCreateInput, hash string,
) (commentMutationResponse, error) {
	pageID := surface.Room
	response := commentMutationResponse{}
	if replayed, err := p.replaySuggestionRequest(actorID, surface, input.RequestID, hash, &response); err != nil || replayed {
		return response, err
	}
	threadID, grew := input.ID, false
	authorization := commentAuthorization{}
	// A toggle's first child is placed first by name: a position alone puts
	// a new block after the blocks already there (content_graph_position_place.go).
	var starts map[string]struct{}
	if input.FirstChild {
		starts = map[string]struct{}{input.NewBlockID: {}}
	}
	_, err := p.applyContentGraphMutationContext(surface.requestContext(), actorID, pageID, graphMutationInput{MutationID: suggestionMutationID(hash)}, contentGraphMutationHooks{
		ExplicitStartBlockIDs: starts,
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
			anchorSuggestion, _ := anchor.Content[graphBlockSuggestionKey].(string)
			if input.Picked {
				// A block a menu picked grows nothing (page_suggestion_picked.go).
			} else if grew, err = ownOpenSuggestion(txApp, authorization.Target, actorID, anchorSuggestion, textSuggestionInsert); err != nil {
				return next, err
			} else if grew {
				threadID = anchorSuggestion
			} else if ending, err := ownSuggestionBefore(txApp, authorization.Target, actorID, text, text.Len()); err != nil {
				return next, err
			} else if ending != "" {
				threadID, grew = ending, true
			}
			var parentID, afterID *string
			anchorID := input.BlockID
			if _, toggle := suggestionToggleTypes[anchor.Type]; input.FirstChild && !toggle {
				return next, errCommentInvalid
			} else if input.FirstChild {
				// The toggle's first child, named a start above.
				parentID = &anchorID
			} else {
				afterID = &anchorID
				if anchor.ParentID != "" {
					parentID = &anchor.ParentID
				}
			}
			next.Operations = []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
				ID: input.NewBlockID, ParentID: parentID, AfterID: afterID, Position: "p",
				Type: input.BlockType, Content: json.RawMessage(`{}`),
			}}}
			if input.Text != "" {
				next.TextUpdates = []graphTextUpdate{{BlockID: input.NewBlockID, Spans: []graphTextSpan{{
					Text: input.Text, Suggestion: textSuggestionInsert, SuggestionID: threadID,
				}}}}
			}
			return next, nil
		},
		AfterApply: func(txApp core.App, page pageAuthorization, doc *crdt.Doc) error {
			if err := setStructuralBlockSuggestion(txApp, doc, page.Workspace.OwnerID, pageID, input.NewBlockID, threadID); err != nil {
				return err
			}
			if input.Picked {
				if err := markStructuralBlockAlone(txApp, doc, page.Workspace.OwnerID, pageID, input.NewBlockID); err != nil {
					return err
				}
			}
			thread, err := saveBlockSuggestionThread(txApp, authorization, actorID, input, threadID, grew, doc)
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

// saveBlockSuggestionThread starts or grows the thread of a suggestion of
// new blocks, its text the text of all of them (structuralThreadBlocks) after
// what it holds in the block it started in.
func saveBlockSuggestionThread(
	app core.App, authorization commentAuthorization, actorID string, input suggestionCreateInput,
	threadID string, grew bool, doc *crdt.Doc,
) (storedComment, error) {
	ids, err := structuralThreadBlocks(doc, threadID)
	if err != nil {
		return storedComment{}, err
	}
	document, err := loadPageTextDocument(app, authorization.Target.ID)
	if err != nil {
		return storedComment{}, err
	}
	now := time.Now().UTC()
	if grew {
		row, err := loadCommentForTarget(app, authorization.Target, threadID)
		if err != nil {
			return storedComment{}, err
		}
		text, lines := suggestionNewLines(document, row.BlockID, ids, threadID)
		body, _ := pageSuggestionThread(row)
		body.Suggestion.Text, body.Suggestion.Blocks = suggestionSummary(text), len(ids)
		body.Suggestion.Head, body.Suggestion.Lines = lines.Head, lines.Lines
		if row, err = rewriteSuggestionThread(app, row, body, now); err != nil {
			return storedComment{}, err
		}
		return row, advanceCommentPageRevision(app, authorization.Target.StoragePageID)
	}
	text, lines := suggestionNewLines(document, input.BlockID, ids, threadID)
	body, err := json.Marshal(suggestionThreadBody{Rich: []any{}, Suggestion: suggestionThreadFacts{
		Kind: textSuggestionInsert, Text: suggestionSummary(text), Blocks: len(ids), Head: lines.Head, Lines: lines.Lines, Picked: input.Picked,
	}})
	if err != nil {
		return storedComment{}, err
	}
	return insertCommentRow(app, authorization, authorization.Target, actorID, commentCreateInput{
		ID: threadID, RequestID: input.RequestID, BlockID: input.BlockID, Body: body,
	}, now)
}

// errNoSuggestedBlocks: the suggestion's blocks are gone, so it settles its
// text alone.
var errNoSuggestedBlocks = errors.New("page suggestion has no blocks")

// resolveBlockSuggestion accepts a suggestion of new blocks, keeping them
// as plain blocks with plain text, or rejects it, removing them; its text in
// the block it started in settles with them.
func (p *pocketBaseYgoPersistence) resolveBlockSuggestion(
	actorID string, surface suggestionSurface, commentID string, input suggestionResolveInput, hash string, accept bool,
) (commentMutationResponse, error) {
	pageID := surface.Room
	response := commentMutationResponse{}
	if replayed, err := p.replaySuggestionRequest(actorID, surface, input.RequestID, hash, &response); err != nil || replayed {
		return response, err
	}
	authorization := commentAuthorization{}
	row, body := storedComment{}, suggestionThreadBody{}
	blocks, started := []string{}, ""
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
			if row, body, err = checkSuggestionSettle(txApp, authorization, actorID, commentID, input.ExpectedRevision, accept); err != nil {
				return next, err
			}
			ids, metadata, err := structuralSuggestedBlocks(doc, commentID)
			if err != nil {
				return next, err
			}
			if len(ids) == 0 {
				return next, errNoSuggestedBlocks
			}
			blocks = ids
			if _, there := metadata[row.BlockID]; there && !slices.Contains(ids, row.BlockID) {
				started = row.BlockID
			}
			for _, id := range ids {
				if !accept {
					next.Operations = append(next.Operations, graphOperation{
						Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: id},
					})
					continue
				}
				// The block stays where it is, as it is.
				entry := metadata[id]
				content, err := json.Marshal(entry.Content)
				if err != nil {
					return next, err
				}
				var parentID *string
				if entry.ParentID != "" {
					parentID = &entry.ParentID
				}
				next.Operations = append(next.Operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
					ID: id, ParentID: parentID, Position: "p", Type: entry.Type, Content: content,
				}})
			}
			return next, nil
		},
		AfterApply: func(txApp core.App, page pageAuthorization, doc *crdt.Doc) error {
			if accept {
				for _, id := range blocks {
					if err := setStructuralBlockSuggestion(txApp, doc, page.Workspace.OwnerID, pageID, id, ""); err != nil {
						return err
					}
				}
			}
			var err error
			response, err = finishSuggestionSettle(txApp, authorization, actorID, row, body, input.RequestID, hash, accept)
			return err
		},
	}
	if accept {
		hooks.ApplyAdditionalText = func(txApp core.App, _ pageAuthorization) (string, error) {
			settled := blocks
			if started != "" {
				settled = append(slices.Clone(blocks), started)
			}
			return settleBlockSuggestionTexts(txApp, pageID, actorID, settled, commentID, true)
		}
	} else {
		// The text before the break goes as the blocks do, before they go:
		// their removal then clears their text in the same change.
		hooks.BeforeApplyOperations = func(txApp core.App, _ pageAuthorization, _ *crdt.Doc, _ []graphOperation) error {
			if started == "" {
				return nil
			}
			_, err := settleBlockSuggestionTexts(txApp, pageID, actorID, []string{started}, commentID, false)
			return err
		}
	}
	if _, err := p.applyContentGraphMutationContext(surface.requestContext(), actorID, pageID, graphMutationInput{MutationID: suggestionMutationID(hash)}, hooks); err != nil || response.Comment.ID != "" {
		return response, err
	}
	_, err := p.replaySuggestionRequest(actorID, surface, input.RequestID, hash, &response)
	return response, err
}
