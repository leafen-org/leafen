package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf16"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A comment on some words of a database page's text value marks those words
// in the value, as Notion's (seen 2026-11-11: the value's text runs part at
// the words), and the mark is no edit of the row: its last edited time and
// editor, its history and updates, its activity, automations and webhooks
// stay as they were. The server makes the mark once the thread is stored, as
// a page's text takes its own (comment_text_anchors.go); a table draws the
// marks of open threads alone (CellCommentMarks), so resolving, reopening or
// deleting a thread writes nothing, and the value keeps a resolved thread's
// mark to show again when it is reopened, as Notion's. Whoever may comment
// may mark; if the words changed before the comment was stored, the thread
// is kept without the mark.

// anchorNewCommentCell marks a new thread's words in its row's value once
// its comment is stored; a mark that can't be made leaves the comment as it is.
func anchorNewCommentCell(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	response commentMutationResponse,
	authorize func(core.App) (commentAuthorization, error),
) {
	if response.Duplicate || ygo == nil {
		return
	}
	if err := ygo.anchorCommentCell(event.Request.Context(), response.Comment, authorize); err != nil {
		event.App.Logger().Warn("comment cell anchor skipped", "comment", response.Comment.ID, "error", err)
	}
}

// commentCellMark is what a thread's first comment names of the words it
// was left on in a row's value.
type commentCellMark struct {
	propertyID string
	quote      string
	start, end int
}

// commentCellMarkOf reads the words a thread's first comment was left on in
// a value: none for a reply, a comment on a block or on the whole value.
func commentCellMarkOf(comment commentRecord) (commentCellMark, bool) {
	propertyID, valid := commentPropertyAnchor(string(comment.Body))
	anchor := commentQuoteAnchor{}
	if !valid || propertyID == "" || comment.ParentID != "" || comment.BlockID != "" ||
		json.Unmarshal(comment.Body, &anchor) != nil || anchor.QuoteStart == nil || anchor.QuoteEnd == nil {
		return commentCellMark{}, false
	}
	quote := strings.Join(strings.Fields(anchor.Quote), " ")
	if quote == "" || *anchor.QuoteStart < 0 || *anchor.QuoteEnd <= *anchor.QuoteStart {
		return commentCellMark{}, false
	}
	return commentCellMark{propertyID: propertyID, quote: quote, start: *anchor.QuoteStart, end: *anchor.QuoteEnd}, true
}

// anchorCommentCell writes the thread's mark into its row's text value.
func (p *pocketBaseYgoPersistence) anchorCommentCell(
	ctx context.Context, comment commentRecord, authorize func(core.App) (commentAuthorization, error),
) error {
	mark, ok := commentCellMarkOf(comment)
	if !ok {
		return nil
	}
	return p.writeCommentCell(ctx, authorize, "comment-cell-mark:"+comment.ID,
		func(txApp core.App, owner, sourceID, rowID string) ([]graphOperation, error) {
			return commentCellMarkOperations(txApp, owner, sourceID, rowID, comment.ID, mark)
		})
}

// writeCommentCell writes a thread's marks in its row's value as a write of
// the system's, in the room of the page holding the row, with the operations
// that write them; none leave the value as it is.
func (p *pocketBaseYgoPersistence) writeCommentCell(
	ctx context.Context, authorize func(core.App) (commentAuthorization, error), mutationID string,
	operationsFor func(txApp core.App, owner, sourceID, rowID string) ([]graphOperation, error),
) error {
	current, err := authorize(p.app)
	if err != nil || current.Target.Kind != commentTargetDatabaseRow {
		return err
	}
	rowID := current.Target.ID
	row := struct {
		Owner    string `db:"owner"`
		PageID   string `db:"page_id"`
		SourceID string `db:"data_source_id"`
	}{}
	if err := p.app.DB().NewQuery(`
		SELECT [[owner]], [[page_id]], [[data_source_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": rowID}).One(&row); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	workspace := current.Page.Workspace.Workspace
	if workspace == nil {
		return nil
	}
	system := systemJobSubjectFor(workspace)
	digest := sha256.Sum256([]byte("leafen:comment-cell-mark\x00" + row.PageID + "\x00" + mutationID))
	_, err = p.applyContentGraphMutationContext(context.WithoutCancel(ctx), "", row.PageID, graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			CommentMarks: true, PreserveRowEditStamps: true, SkipRelationPairSync: true,
			SuppressAutomationEvents: true, SuppressNotifications: true,
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				// The commenter may still comment on the row as the mark is made.
				if _, err := authorize(txApp); err != nil {
					return pageAuthorization{}, err
				}
				return system.authorizePageBody(txApp, row.PageID, permissionWorkspaceWrite)
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				operations, err := operationsFor(txApp, row.Owner, row.SourceID, rowID)
				if err != nil || len(operations) == 0 {
					return contentGraphMutationPreparation{Deferred: true}, err
				}
				return contentGraphMutationPreparation{Operations: operations}, nil
			},
		})
	return err
}

// commentCellMarkOperations writes the row's value with the thread's words
// marked: none when the value is not text, or its words there are no longer
// the ones the thread quotes.
func commentCellMarkOperations(
	app core.App, owner, sourceID, rowID, threadID string, mark commentCellMark,
) ([]graphOperation, error) {
	stored := struct {
		Type  string `db:"type"`
		Value string `db:"value"`
		Spans string `db:"spans"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[type]], [[value]], COALESCE([[spans]], '') AS [[spans]] FROM {{leafen_database_row_values}}
		WHERE [[row_id]] = {:row} AND [[property_id]] = {:property}
	`).Bind(dbx.Params{"row": rowID, "property": mark.propertyID}).One(&stored); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var text string
	if stored.Type != graphValueTypeRichText && stored.Type != graphValueTypeTitle ||
		json.Unmarshal([]byte(stored.Value), &text) != nil {
		return nil, nil
	}
	spans := []graphTextSpan{}
	if stored.Spans != "" && json.Unmarshal([]byte(stored.Spans), &spans) != nil {
		return nil, nil
	}
	joined := ""
	for _, span := range spans {
		joined += span.Text
	}
	if len(spans) == 0 || joined != text {
		spans = []graphTextSpan{{Text: text}}
	}
	units := utf16.Encode([]rune(text))
	if mark.end > len(units) || !suggestionTextBoundary(units, mark.start) || !suggestionTextBoundary(units, mark.end) ||
		strings.Join(strings.Fields(string(utf16.Decode(units[mark.start:mark.end]))), " ") != mark.quote {
		return nil, nil
	}
	writes := newDatabaseRowWrites(app, owner, nil)
	row, found, err := writes.row(sourceID, rowID)
	if err != nil || !found {
		return nil, err
	}
	row.Values[mark.propertyID] = graphRowValue{
		Type: stored.Type, Value: json.RawMessage(stored.Value),
		Spans: commentCellSpansMarked(spans, mark.start, mark.end, threadID),
	}
	return writes.operations, nil
}

// commentCellSpansMarked gives a value's spans with [start, end) of its text,
// in UTF-16 units, marked with the thread, the threads already there kept; a
// mention or an equation the words reach is marked whole.
func commentCellSpansMarked(spans []graphTextSpan, start, end int, threadID string) []graphTextSpan {
	marked := make([]graphTextSpan, 0, len(spans)+2)
	at := 0
	for _, span := range spans {
		units := utf16.Encode([]rune(span.Text))
		from, to := at, at+len(units)
		at = to
		if to <= start || from >= end {
			marked = append(marked, span)
			continue
		}
		if span.Mention != "" || span.Equation != "" {
			span.CommentID = commentThreadsWith(span.CommentID, threadID)
			marked = append(marked, span)
			continue
		}
		piece := func(low, high int) graphTextSpan {
			part := span
			part.Text = string(utf16.Decode(units[low-from : high-from]))
			return part
		}
		if from < start {
			marked = append(marked, piece(from, start))
		}
		middle := piece(max(from, start), min(to, end))
		middle.CommentID = commentThreadsWith(middle.CommentID, threadID)
		marked = append(marked, middle)
		if to > end {
			marked = append(marked, piece(end, to))
		}
	}
	return marked
}
