package app

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// suggestionReplacementWords are the words each suggestion in a block
// suggests in (added) and away (away), which a replacement's facts hold
// apart.
type suggestionReplacementWords struct {
	added, away map[string]string
}

// saveSuggestionThreads brings the threads of the suggestions an edit made,
// grew or changed up to their text, withdrawing a suggestion left with none,
// and returns the edit's own thread. A replacement whose new words are all
// taken out again is a deletion of its old ones.
func saveSuggestionThreads(
	app core.App, authorization commentAuthorization, actorID string, input suggestionCreateInput,
	edit suggestionEdit, threads map[string]storedComment, texts map[string]string,
	words suggestionReplacementWords, lines map[string]suggestionLines,
) (storedComment, error) {
	now := time.Now().UTC()
	revise := func(id string) (storedComment, error) {
		body, _ := pageSuggestionThread(threads[id])
		body.Suggestion.Text = suggestionSummary(texts[id])
		if body.Suggestion.Kind == textSuggestionReplace {
			body.Suggestion.Text, body.Suggestion.Replaced = suggestionSummary(words.added[id]), suggestionSummary(words.away[id])
			if words.added[id] == "" {
				body.Suggestion.Kind, body.Suggestion.Text, body.Suggestion.Replaced = textSuggestionDelete, body.Suggestion.Replaced, ""
			}
		}
		if blockLines, found := lines[id]; found {
			body.Suggestion.Head, body.Suggestion.Lines = blockLines.Head, blockLines.Lines
		}
		// A suggestion of new blocks or a break stays open for them with no
		// text left: its lines are still listed (suggestionThreadTexts).
		if _, structural := lines[id]; texts[id] == "" && !structural {
			body.Suggestion.State = suggestionStateWithdrawn
		}
		return rewriteSuggestionThread(app, threads[id], body, now)
	}
	for _, id := range edit.Revised {
		row, err := revise(id)
		if err != nil {
			return storedComment{}, err
		}
		threads[id] = row
	}
	if len(edit.Revised) > 0 || edit.Grew {
		if err := advanceCommentPageRevision(app, authorization.Target.StoragePageID); err != nil {
			return storedComment{}, err
		}
	}
	switch {
	case !edit.Changed:
		return threads[edit.Revised[0]], nil
	case edit.Grew:
		return revise(edit.SuggestionID)
	}
	facts := suggestionThreadFacts{Kind: edit.Kind, Text: suggestionSummary(texts[input.ID])}
	switch edit.Kind {
	case textSuggestionFormat:
		facts.Marks = input.Marks
	case textSuggestionReplace:
		facts.Text, facts.Replaced = suggestionSummary(words.added[input.ID]), suggestionSummary(words.away[input.ID])
	}
	body, err := json.Marshal(suggestionThreadBody{Rich: []any{}, Suggestion: facts})
	if err != nil {
		return storedComment{}, err
	}
	return insertCommentRow(app, authorization, authorization.Target, actorID, commentCreateInput{
		ID: input.ID, RequestID: input.RequestID, BlockID: input.BlockID, Body: body,
	}, now)
}

// suggestionThreadsNear loads the threads of the suggestions whose text
// touches a range of a block, and of the suggestion that suggested the
// block: those an edit there can grow or change.
func suggestionThreadsNear(
	app core.App, target nativeCommentTarget, runs []suggestionTextRun, start, end int, blockSuggestion string,
) (map[string]storedComment, error) {
	threads := map[string]storedComment{}
	ids := []string{blockSuggestion}
	for _, run := range runs {
		if _, id := run.suggestion(); run.End >= start && run.Start <= end {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if _, loaded := threads[id]; id == "" || loaded {
			continue
		}
		row, err := loadCommentForTarget(app, target, id)
		if err != nil && !errors.Is(err, errCommentNotFound) {
			return nil, err
		}
		threads[id] = row
	}
	return threads, nil
}

// suggestionThreadTexts are the text each suggestion in a block holds, all
// of it for a suggestion of new blocks, and such a suggestion's text as its
// summary lists it (suggestionNewLines), from the block each one started in
// (anchorOf). A suggestion an edit revised (revised) is read too when the
// block holds none of its text any more: one of new blocks or a break still
// has its lines, empty as they may be.
func suggestionThreadTexts(
	app core.App, pageID string, document *crdt.Doc, root string, anchorOf func(string) string, revised []string,
) (map[string]string, map[string]suggestionLines, error) {
	texts := suggestionTexts(document.GetText(root))
	lines := map[string]suggestionLines{}
	ids := slices.Collect(maps.Keys(texts))
	for _, id := range revised {
		if _, held := texts[id]; !held {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		// Its blocks in the page's order, then the block after a break it
		// suggested in a block's text (structuralThreadBlocks).
		blocks, split := []string{}, []string{}
		if err := app.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_blocks}}
			WHERE [[page_id]]={:pageId} AND json_valid([[content]])
				AND json_extract([[content]], '$.suggestionId')={:suggestionId}
			ORDER BY [[parent_id]], [[position]], [[id]]
		`).Bind(dbx.Params{"pageId": pageID, "suggestionId": id}).Column(&blocks); err != nil {
			return nil, nil, err
		}
		if err := app.DB().NewQuery(`
			SELECT json_extract([[content]], '$.splitBlockId') FROM {{leafen_blocks}}
			WHERE [[page_id]]={:pageId} AND json_valid([[content]])
				AND json_extract([[content]], '$.splitSuggestionId')={:suggestionId}
		`).Bind(dbx.Params{"pageId": pageID, "suggestionId": id}).Column(&split); err != nil {
			return nil, nil, err
		}
		blocks = append(blocks, split...)
		if len(blocks) == 0 {
			continue
		}
		texts[id], lines[id] = suggestionNewLines(document, anchorOf(id), blocks, id)
	}
	return texts, lines, nil
}

// suggestionBlockLines is the text a suggestion of new blocks holds, all of
// it, and by its blocks in order — each one's suggested text, none for a
// block without — as its summary lists them (suggestionModel.ts): as much
// as a summary has left, at most left characters in all.
func suggestionBlockLines(document *crdt.Doc, blocks []string, suggestionID string, left int) (string, []string) {
	left = max(left, 0)
	text, lines := "", make([]string, 0, min(len(blocks), suggestionSummaryRunes))
	for _, blockID := range blocks {
		line := suggestionTexts(document.GetText(ygoBlockTextRootPrefix + blockID))[suggestionID]
		text += line
		if len(lines) == cap(lines) {
			continue
		}
		if runes := []rune(line); len(runes) > left {
			line = string(runes[:left])
		}
		left -= len([]rune(line))
		lines = append(lines, line)
	}
	return text, lines
}

// rewriteSuggestionThread saves a suggestion thread root's facts, resolving
// the thread once the suggestion is settled or withdrawn.
func rewriteSuggestionThread(
	app core.App, row storedComment, body suggestionThreadBody, now time.Time,
) (storedComment, error) {
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > commentBodyBytes {
		return storedComment{}, failedBecause(errCommentFailed, err)
	}
	expected := row.Revision
	row.BodyJSON, row.Resolved = string(encoded), body.Suggestion.State != ""
	row.Revision++
	row.UpdatedAt = now.Format(time.RFC3339Nano)
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_comments}} SET [[body_json]]={:body},[[resolved]]={:resolved},
			[[revision]]={:revision},[[updated_at]]={:updated}
		WHERE [[id]]={:id} AND [[revision]]={:expected}
	`).Bind(dbx.Params{
		"body": row.BodyJSON, "resolved": row.Resolved, "revision": row.Revision,
		"updated": row.UpdatedAt, "id": row.ID, "expected": expected,
	}).Execute()
	if err != nil {
		return storedComment{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return storedComment{}, refusedUnlessStorage(errCommentRevisionConflict, err)
	}
	// A new suggestion's notification, not yet delivered, grows with it.
	if body.Suggestion.State == "" {
		projection, err := commentNotificationBodyProjection(json.RawMessage(row.BodyJSON))
		if err != nil {
			return storedComment{}, err
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_notification_events}} SET [[preview]]={:preview},[[updated_at]]={:updated}
			WHERE [[source_kind]]='comment' AND [[source_id]]={:id} AND [[status]]='pending'
		`).Bind(dbx.Params{"preview": projection.Preview, "updated": row.UpdatedAt, "id": row.ID}).Execute(); err != nil {
			return storedComment{}, err
		}
	}
	return row, nil
}

// runPageTextWrite runs a change to a page's collaborative text in a
// transaction, in turn with the page's other text writers.
func (p *pocketBaseYgoPersistence) runPageTextWrite(room string, write func(context.Context, core.App) error) error {
	return p.runPageTextWriteFor(context.Background(), room, write)
}

// runSuggestionWrite runs a suggestion's change to its body's text as
// runPageTextWrite does, the versions it records naming the connection the
// request came through (suggestionSurface.request).
func (p *pocketBaseYgoPersistence) runSuggestionWrite(surface suggestionSurface, write func(context.Context, core.App) error) error {
	return p.runPageTextWriteFor(surface.requestContext(), surface.Room, write)
}

// runPageTextWriteFor runs a change to a page's collaborative text for a
// request: a change a connection makes names it in the page's history, as
// the page's structural writes do (ygo_persistence.go).
func (p *pocketBaseYgoPersistence) runPageTextWriteFor(parent context.Context, room string, write func(context.Context, core.App) error) error {
	ctx, cancel, startedBeforeShutdown, err := p.operationContext(parent, room)
	if err != nil {
		return err
	}
	defer cancel()
	stripe := p.stripe(room)
	stripe.Lock()
	defer stripe.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := p.operationStop(startedBeforeShutdown)
	return p.runOperationTransaction(stop, func(txApp core.App) error {
		defer namePageHistoryConnection(txApp, parent)()
		return write(ctx, txApp)
	})
}

// loadPageTextDocument reads a page's collaborative text to change it, as
// its open room holds it. A room stores what it takes a moment later (its
// writes are coalesced), so what the room holds and the store lacks is stored
// first: a change planned from this document depends only on stored text.
func loadPageTextDocument(app core.App, pageID string) (*crdt.Doc, error) {
	document, err := loadStoredPageTextDocument(app, pageID)
	if err != nil {
		return nil, err
	}
	room := openPageTextRoom(app, pageID)
	if room == nil {
		return document, nil
	}
	var taken crdt.UpdateDetails
	unsubscribe := document.OnUpdateDetails(func(value crdt.UpdateDetails) { taken = value })
	err = crdt.ApplyUpdateV1(document, crdt.EncodeStateAsUpdateV1(room, document.StateVector()), nil)
	unsubscribe()
	if err != nil {
		return nil, err
	}
	if len(taken.Update) > 0 {
		if _, err := appendYgoUpdateIfChanged(app, pageID, taken.Update); err != nil {
			return nil, err
		}
	}
	return document, nil
}

// openPageTextRoom is a page's collaborative text as its open room holds it,
// or nil when no room is open for the page.
func openPageTextRoom(app core.App, pageID string) *crdt.Doc {
	authorization, ok := app.Store().Get(ygoRoomAuthorizationStoreKey).(*ygoRoomAuthorization)
	if !ok || authorization.server == nil {
		return nil
	}
	return authorization.server.GetDoc(pageID)
}

// loadStoredPageTextDocument reads a page's collaborative text as stored.
func loadStoredPageTextDocument(app core.App, pageID string) (*crdt.Doc, error) {
	document, _, err := loadYgoDoc(app, pageID)
	return document, err
}

// transactPageText makes a change to a page's collaborative text, returning
// it as an update with the text changes it made.
func transactPageText(document *crdt.Doc, change func(*crdt.Transaction)) ([]byte, []crdt.TextChange) {
	var details crdt.UpdateDetails
	unsubscribe := document.OnUpdateDetails(func(value crdt.UpdateDetails) { details = value })
	document.Transact(change)
	unsubscribe()
	return details.Update, details.TextChanges
}

// storePageSuggestionText stores a suggestion's change to a page's text as
// the page's other text changes are stored: after the page's history
// baseline, in its history, in its open room once committed, and as the
// person's edit. A database page's is stored as its body's socket changes
// are: in its open room, moving its body revision on.
func storePageSuggestionText(
	ctx context.Context, app core.App, authorization commentAuthorization, actorID, requestID string,
	update []byte, changes []crdt.TextChange,
) error {
	if len(update) == 0 {
		return nil
	}
	if len(update) > maxYgoUpdateBytes {
		return graphInputError(errorCodeGraphChangeTooLarge, "The suggestion is too large for one change (64 MB). Make it in parts.")
	}
	pageID, workspace := authorization.Target.ID, authorization.Page.Workspace
	if authorization.Target.Kind == commentTargetDatabaseRow {
		if err := ensurePageHistoryBaseline(app, pageID); err != nil {
			return err
		}
		if err := appendYgoUpdate(app, pageID, update); err != nil {
			return err
		}
		publishStoredYgoUpdateAfterCommit(app, pageID, update)
		if err := recordPageHistoryCollaborationUpdate(app, pageID, actorID, update, changes); err != nil {
			return err
		}
		return noteDatabaseRowBodyChange(app, pageID, actorID)
	}
	if err := ensurePageHistoryBaseline(app, pageID); err != nil {
		return err
	}
	if err := appendYgoUpdate(app, pageID, update); err != nil {
		return err
	}
	publishStoredYgoUpdateAfterCommit(app, pageID, update)
	if err := recordPageHistoryCollaborationUpdate(app, pageID, actorID, update, changes); err != nil {
		return err
	}
	if err := setPageLastEditorByID(ctx, app, pageID, actorID); err != nil {
		return err
	}
	if err := recordNotionAdminLegalHoldPageInteraction(app, pageEditedInteraction(
		"suggestion", pageID+"\x00"+actorID+"\x00"+requestID, workspace.Organization.OrganizationID,
		workspace.WorkspaceID, pageID, actorID, time.Now().UTC().UnixMilli(),
	)); err != nil {
		return err
	}
	return markSearchDirtyByPageID(app, pageID)
}
