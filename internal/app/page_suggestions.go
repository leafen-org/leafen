package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Someone who can comment on a page suggests edits to its blocks' text,
// and someone who can edit it accepts or rejects them; the suggester can
// reject their own. Each suggestion is a comment thread on its block whose
// root holds the suggestion's kind, text and state, and the block's text
// carries the suggestion under the thread's ID. The server changes the text
// and the thread together, in turn with the page's other text writers, and
// a locked page or one in Trash takes no suggestion. A database page takes
// suggestions as a page does, its threads among its row's comments.

var (
	errSuggestionTextChanged = errors.New(errorCodeCommentSuggestionTextChanged)
	errSuggestionSettled     = errors.New(errorCodeCommentSuggestionSettled)
)

const (
	suggestionStateAccepted  = "accepted"
	suggestionStateRejected  = "rejected"
	suggestionStateWithdrawn = "withdrawn"
	suggestionSummaryRunes   = 200
)

// suggestionBlockTypes are the blocks whose text takes suggestions.
var suggestionBlockTypes = map[string]struct{}{
	graphBlockTypeParagraph: {}, graphBlockTypeHeading1: {}, graphBlockTypeHeading2: {},
	graphBlockTypeHeading3: {}, graphBlockTypeHeading4: {}, graphBlockTypeToggleHeading1: {},
	graphBlockTypeToggleHeading2: {}, graphBlockTypeToggleHeading3: {}, graphBlockTypeToggleHeading4: {},
	graphBlockTypeBulletedListItem: {}, graphBlockTypeNumberedListItem: {}, graphBlockTypeToDo: {},
	graphBlockTypeToggle: {}, graphBlockTypeQuote: {}, graphBlockTypeCallout: {}, graphBlockTypeCode: {},
}

type suggestionThreadBody struct {
	Rich       []any                 `json:"rich"`
	Suggestion suggestionThreadFacts `json:"suggestion"`
}

// suggestionThreadFacts are a suggestion's kind, the text it holds (its
// start, for a long one; a replacement's new words, Replaced its old ones),
// a formatting suggestion's marks, the blocks it suggests in, and once it is
// settled, how.
type suggestionThreadFacts struct {
	Kind     string         `json:"kind"`
	Text     string         `json:"text"`
	Replaced string         `json:"replaced,omitempty"`
	Marks    map[string]any `json:"marks,omitempty"`
	Blocks   int            `json:"blocks,omitempty"`
	// Head is what a suggestion of new blocks holds at the end of the block
	// it started in, when it grew from text suggested there; Lines are its
	// text by its blocks (suggestionNewLines).
	Head  string   `json:"head,omitempty"`
	Lines []string `json:"lines,omitempty"`
	// Picked is a block "/" picked on the person's own suggested line, a
	// suggestion of its own; Turned is the break before it, which stays a
	// suggestion apart (page_suggestion_turns.go).
	Picked bool `json:"picked,omitempty"`
	Turned bool `json:"turned,omitempty"`
	// AtStart is a break suggested at the start of a block's text, which
	// the new block before the block ends; rejecting it keeps the block
	// (page_suggestion_split_join.go).
	AtStart bool `json:"atStart,omitzero"`
	// Removed are the blocks a removal suggests away, whose words Lines
	// lists by block (page_suggestion_removals.go).
	Removed []string `json:"removed,omitempty"`
	State   string   `json:"state,omitempty"`
}

// pageSuggestionThread reads a comment as a suggestion's thread root.
func pageSuggestionThread(row storedComment) (suggestionThreadBody, bool) {
	body := suggestionThreadBody{}
	if row.ParentID != "" || json.Unmarshal([]byte(row.BodyJSON), &body) != nil {
		return suggestionThreadBody{}, false
	}
	switch body.Suggestion.Kind {
	case textSuggestionInsert, textSuggestionDelete, textSuggestionFormat, textSuggestionReplace:
		if body.Rich == nil {
			body.Rich = []any{}
		}
		return body, true
	}
	return suggestionThreadBody{}, false
}

func suggestionSummary(text string) string {
	if runes := []rune(text); len(runes) > suggestionSummaryRunes {
		return string(runes[:suggestionSummaryRunes])
	}
	return text
}

func registerPageSuggestionRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	for _, route := range []struct {
		method, path string
		row, resolve bool
	}{
		{suggestionCreateMethod, suggestionCreatePath, false, false},
		{suggestionResolveMethod, suggestionResolvePath, false, true},
		{databaseRowSuggestionCreateMethod, databaseRowSuggestionCreatePath, true, false},
		{databaseRowSuggestionResolveMethod, databaseRowSuggestionResolvePath, true, true},
	} {
		// A page's path given a row's page id answers as the row's
		// (commentsOfRowPage), which names its rowId.
		surface := func(event *core.RequestEvent) suggestionSurface {
			if rowID := strings.TrimSpace(event.Request.PathValue("rowId")); rowID != "" {
				return suggestionSurface{Room: rowID, Row: true, request: event.Request.Context()}
			}
			return suggestionSurface{Room: strings.TrimSpace(event.Request.PathValue("pageId")), request: event.Request.Context()}
		}
		handle := func(event *core.RequestEvent) error { return ygo.createSuggestionRoute(event, surface(event)) }
		limit := int64(8*maxGraphTextCharacters + 16384)
		if route.resolve {
			handle = func(event *core.RequestEvent) error { return ygo.resolveSuggestionRoute(event, surface(event)) }
			limit = 8192
		}
		if !route.row {
			handle = commentsOfRowPage(handle, handle)
		}
		event.Router.Route(route.method, apiRoot+route.path, handle).Bind(apis.BodyLimit(limit))
	}
}

func suggestionAPIError(event *core.RequestEvent, err error) error {
	var requestError *graphRequestError
	switch {
	case errors.Is(err, errSuggestionTextChanged):
		return apiError(event, http.StatusConflict, errorCodeCommentSuggestionTextChanged, suggestionTextChangedMessage(err))
	case errors.Is(err, errSuggestionSettled):
		return apiError(event, http.StatusConflict, errorCodeCommentSuggestionSettled, "The suggestion was already accepted or rejected.")
	case isDLPPolicyDecisionError(err):
		return writeDLPPolicyError(event, err, "page-suggestion")
	case errors.As(err, &requestError):
		return apiError(event, requestError.status, requestError.code, requestError.message)
	}
	return commentAPIError(event, err, persistentWriteComment)
}

// unreadableSuggestionSurface answers a database page the person cannot
// read as missing, as its comments do; it reports whether it answered.
func unreadableSuggestionSurface(event *core.RequestEvent, userID string, surface suggestionSurface) (bool, error) {
	if !surface.Row {
		return false, nil
	}
	_, _, err := authorizeDatabaseRowCommentTarget(event.App, userID, surface.Room, permissionWorkspaceRead)
	if err == nil {
		return false, nil
	}
	return true, databaseRowCommentAPIError(event, err, persistentReadComment)
}

func suggestionSurfaceAPIError(event *core.RequestEvent, surface suggestionSurface, err error) error {
	if surface.Row && errors.Is(err, errDatabaseRowPageNotFound) {
		return databaseRowCommentAPIError(event, err, persistentWriteComment)
	}
	return suggestionAPIError(event, err)
}

func (p *pocketBaseYgoPersistence) createSuggestionRoute(event *core.RequestEvent, surface suggestionSurface) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	if answered, err := unreadableSuggestionSurface(event, user.Id, surface); answered {
		return err
	}
	input := suggestionCreateInput{}
	if err := event.BindBody(&input); err != nil {
		return commentAPIError(event, commentBodyRefused("suggestion request", err), persistentWriteComment)
	}
	response, err := p.createSuggestion(user.Id, surface, input)
	if err != nil {
		return suggestionSurfaceAPIError(event, surface, err)
	}
	status := http.StatusOK
	if !response.Duplicate && response.Comment.ID == strings.TrimSpace(input.ID) {
		status = http.StatusCreated
	}
	return event.JSON(status, response)
}

func (p *pocketBaseYgoPersistence) resolveSuggestionRoute(event *core.RequestEvent, surface suggestionSurface) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	if answered, err := unreadableSuggestionSurface(event, user.Id, surface); answered {
		return err
	}
	input := suggestionResolveInput{}
	if err := event.BindBody(&input); err != nil {
		return commentAPIError(event, commentBodyRefused("suggestion decision", err), persistentWriteComment)
	}
	response, err := p.resolveSuggestion(user.Id, surface, strings.TrimSpace(event.Request.PathValue("commentId")), input)
	if err != nil {
		return suggestionSurfaceAPIError(event, surface, err)
	}
	return event.JSON(http.StatusOK, response)
}

func (p *pocketBaseYgoPersistence) createSuggestion(
	actorID string, surface suggestionSurface, input suggestionCreateInput,
) (commentMutationResponse, error) {
	input.ID, input.RequestID = strings.TrimSpace(input.ID), strings.TrimSpace(input.RequestID)
	input.BlockID = strings.TrimSpace(input.BlockID)
	inserting := input.Kind == textSuggestionInsert || input.Kind == textSuggestionReplace
	marks, validMarks := suggestionMarkAttributes(input.Marks, inserting)
	units := utf16.Encode([]rune(input.ExpectedText))
	start, end := input.Offset, input.Offset+input.Length
	// What each kind takes, said when it is refused (comment_refusals.go).
	if !validCommentIdentifier(surface.Room) {
		return commentMutationResponse{}, errCommentInvalid
	}
	if problem := suggestionCreateProblem(input, units, validMarks); problem != "" {
		return commentMutationResponse{}, commentInvalid("%s", problem)
	}
	hash, err := commentRequestHash("suggest", surface.targetKey(), input.ID, input)
	if err != nil {
		return commentMutationResponse{}, err
	}
	if input.Kind == textSuggestionBlock {
		return p.createBlockSuggestion(actorID, surface, input, hash)
	}
	if input.Kind == textSuggestionSplit {
		return p.createSplitSuggestion(actorID, surface, input, hash)
	}
	if input.Kind == textSuggestionTurn {
		return p.turnBlockSuggestion(actorID, surface, input, hash)
	}
	if input.Kind == textSuggestionRemove {
		return p.createRemovalSuggestion(actorID, surface, input, hash)
	}
	response := commentMutationResponse{}
	err = p.runSuggestionWrite(surface, func(ctx context.Context, txApp core.App) error {
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
		blockSuggestion, err := checkSuggestionBlock(txApp, authorization, input.BlockID)
		if err != nil {
			return err
		}
		if inserting {
			if err := enforceOrganizationDLPContent(
				txApp, authorization.Page.Workspace.Organization.OrganizationID, input.Text,
			); err != nil {
				return err
			}
		}
		document, err := loadPageTextDocument(txApp, surface.Room)
		if err != nil {
			return err
		}
		root := ygoBlockTextRootPrefix + input.BlockID
		if current := document.GetText(root).ToString(); current != input.ExpectedText {
			return &suggestionTextChanged{current: current}
		}
		runs := suggestionTextRuns(document.GetText(root))
		threads, err := suggestionThreadsNear(txApp, authorization.Target, runs, start, end, blockSuggestion)
		if err != nil {
			return err
		}
		// A thread grows in its own block, a suggested block's thread in that
		// block too, and text suggested in where a break it went on with
		// moved its text (page_suggestion_lines.go).
		// A removal stays as it was made: text deleted beside it is a suggestion of its own.
		// A replacement's new words grow as its own suggested text does.
		own := func(id, kind string) bool {
			body, isSuggestion := pageSuggestionThread(threads[id])
			kindOf := body.Suggestion.Kind == kind || kind == textSuggestionInsert && body.Suggestion.Kind == textSuggestionReplace
			return isSuggestion && kindOf && body.Suggestion.State == "" && !threads[id].Resolved &&
				len(body.Suggestion.Removed) == 0 &&
				threads[id].AuthorID == actorID && (threads[id].BlockID == input.BlockID || id == blockSuggestion ||
				kind == textSuggestionInsert && suggestionRunsHold(runs, id))
		}
		edit := suggestionEdit{}
		update, changes := transactPageText(document, func(transaction *crdt.Transaction) {
			text := transaction.GetText(root)
			switch input.Kind {
			case textSuggestionInsert:
				edit = suggestInsert(transaction, text, runs, start, input.Text, marks, input.ID, blockSuggestion, own)
			case textSuggestionDelete:
				edit = suggestDelete(transaction, text, runs, start, end, input.ID, own)
			case textSuggestionReplace:
				edit = suggestReplace(transaction, text, runs, start, end, input.Text, marks, input.ID, blockSuggestion, own)
			default:
				edit = suggestFormat(transaction, text, runs, start, end, marks, input.ID, own)
			}
		})
		if !edit.Changed && len(edit.Revised) == 0 {
			return commentInvalid("the suggestion changes nothing: the text at offset and length is already as it suggests")
		}
		if err := storePageSuggestionText(ctx, txApp, authorization, actorID, input.RequestID, update, changes); err != nil {
			return err
		}
		texts, lines, err := suggestionThreadTexts(txApp, surface.Room, document, root,
			func(id string) string { return threads[id].BlockID }, edit.Revised)
		if err != nil {
			return err
		}
		// A replacement's facts hold its new words and, apart, the old ones.
		words := suggestionReplacementWords{
			added: suggestionWordsOfKind(document.GetText(root), textSuggestionInsert),
			away:  suggestionWordsOfKind(document.GetText(root), textSuggestionDelete),
		}
		thread, err := saveSuggestionThreads(txApp, authorization, actorID, input, edit, threads, texts, words, lines)
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

func (p *pocketBaseYgoPersistence) resolveSuggestion(
	actorID string, surface suggestionSurface, commentID string, input suggestionResolveInput,
) (commentMutationResponse, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	accept := input.Decision == "accept"
	switch {
	case !validCommentIdentifier(surface.Room) || !validCommentIdentifier(commentID):
		return commentMutationResponse{}, errCommentInvalid
	case commentIdentifierProblem("requestId", input.RequestID) != "":
		return commentMutationResponse{}, commentInvalid("%s", commentIdentifierProblem("requestId", input.RequestID))
	case commentRevisionProblem(input.ExpectedRevision) != "":
		return commentMutationResponse{}, commentInvalid("%s", commentRevisionProblem(input.ExpectedRevision))
	case !accept && input.Decision != "reject":
		return commentMutationResponse{}, commentInvalid("decision %q isn't accept or reject", input.Decision)
	}
	hash, err := commentRequestHash("settle", surface.targetKey(), commentID, input)
	if err != nil {
		return commentMutationResponse{}, err
	}
	// A removal takes its blocks away as it is accepted (page_suggestion_removals.go).
	if removed, err := p.suggestionRemovedBlocks(actorID, surface, commentID); err != nil {
		return commentMutationResponse{}, err
	} else if len(removed) > 0 {
		return p.resolveRemovalSuggestion(actorID, surface, commentID, removed, input, hash, accept)
	}
	// A suggested break or new blocks settle with the body's other content
	// changes; one whose blocks are gone settles its text alone.
	if split, err := pageHasSplitSuggestion(p.app, surface.Room, commentID); err != nil {
		return commentMutationResponse{}, err
	} else if split {
		response, err := p.resolveSplitSuggestion(actorID, surface, commentID, input, hash, accept)
		if !errors.Is(err, errNoSuggestedBlocks) {
			return response, err
		}
	}
	if blocks, err := pageHasSuggestedBlocks(p.app, surface.Room, commentID); err != nil {
		return commentMutationResponse{}, err
	} else if blocks {
		response, err := p.resolveBlockSuggestion(actorID, surface, commentID, input, hash, accept)
		if !errors.Is(err, errNoSuggestedBlocks) {
			return response, err
		}
	}
	response := commentMutationResponse{}
	err = p.runSuggestionWrite(surface, func(ctx context.Context, txApp core.App) error {
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
		if row.BlockID != "" {
			document, err := loadPageTextDocument(txApp, surface.Room)
			if err != nil {
				return err
			}
			root := ygoBlockTextRootPrefix + row.BlockID
			runs := suggestionTextRuns(document.GetText(root))
			update, changes := transactPageText(document, func(transaction *crdt.Transaction) {
				settleSuggestion(transaction, transaction.GetText(root), runs, row.ID, accept)
			})
			if err := storePageSuggestionText(ctx, txApp, authorization, actorID, input.RequestID, update, changes); err != nil {
				return err
			}
		}
		response, err = finishSuggestionSettle(txApp, authorization, actorID, row, body, input.RequestID, hash, accept)
		return err
	})
	return response, err
}

// checkSuggestionSettle loads a suggestion's thread to settle it: an
// editor accepts or rejects it, and the suggester can reject their own.
func checkSuggestionSettle(
	app core.App, authorization commentAuthorization, actorID, commentID string, expectedRevision int, accept bool,
) (storedComment, suggestionThreadBody, error) {
	row, err := loadCommentForTarget(app, authorization.Target, commentID)
	if err != nil {
		return storedComment{}, suggestionThreadBody{}, err
	}
	body, isSuggestion := pageSuggestionThread(row)
	if !isSuggestion {
		return storedComment{}, suggestionThreadBody{}, commentInvalid("comment %q isn't an edit suggestion", commentID)
	}
	if authorization.Page.AccessRole < pageAccessRoleEdit && (accept || row.AuthorID != actorID) {
		return storedComment{}, suggestionThreadBody{}, permissionDenied()
	}
	if row.Resolved || body.Suggestion.State != "" {
		return storedComment{}, suggestionThreadBody{}, errSuggestionSettled
	}
	if row.Revision != expectedRevision {
		return storedComment{}, suggestionThreadBody{}, errCommentRevisionConflict
	}
	return row, body, nil
}

// finishSuggestionSettle resolves a settled suggestion's thread with how it
// was settled, and answers the request.
func finishSuggestionSettle(
	app core.App, authorization commentAuthorization, actorID string, row storedComment,
	body suggestionThreadBody, requestID, hash string, accept bool,
) (commentMutationResponse, error) {
	body.Suggestion.State = suggestionStateRejected
	if accept {
		body.Suggestion.State = suggestionStateAccepted
	}
	row, err := rewriteSuggestionThread(app, row, body, time.Now().UTC())
	if err != nil {
		return commentMutationResponse{}, err
	}
	if err := advanceCommentPageRevision(app, authorization.Target.StoragePageID); err != nil {
		return commentMutationResponse{}, err
	}
	record, err := renderComment(row)
	if err != nil {
		return commentMutationResponse{}, err
	}
	if err := attachCommentDetail(app, actorID, &record); err != nil {
		return commentMutationResponse{}, err
	}
	response := commentMutationResponse{Comment: record}
	return response, insertCommentReceipt(
		app, actorID, row.WorkspaceID, authorization.Target.StoragePageID, requestID, hash, "update", response,
	)
}
