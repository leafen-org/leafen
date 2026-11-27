package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A discussion's marks name it by its first comment's ID: the words it was
// left on in its page's text (comment_text_anchors.go), in a block's caption
// (comment_caption_anchors.go) and in a database page's text value
// (comment_cell_anchors.go). When its first comment is deleted and a reply
// takes over (comment_thread_handover.go), its marks are moved to that
// reply's ID once the delete is stored, as a new discussion's marks are made
// once it is stored, so its words stay marked as Notion's do (seen
// 2026-11-21: the quoted words stayed highlighted and the discussion kept
// its quote). A mark that can't be moved leaves the discussion as it is.

// moveCommentThreadMarks moves a discussion's marks to its new first
// comment, whoever deleted its first comment and wherever.
func moveCommentThreadMarks(
	app core.App, actorID string, handover commentHandover,
	authorize func(core.App) (commentAuthorization, error),
) {
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return
	}
	thread, err := renderComment(handover.Thread)
	if err == nil {
		err = persistence.moveCommentThreadText(actorID, thread, handover.FormerID, authorize)
	}
	if err == nil {
		err = persistence.moveCommentThreadCell(thread, handover.FormerID, authorize)
	}
	if err != nil {
		app.Logger().Warn("comment thread marks not moved", "comment", handover.Thread.ID, "error", err)
	}
}

// moveCommentThreadText moves the marks in the text of the page or database
// page holding the discussion, on every block's text and on the captions it
// was left on.
func (p *pocketBaseYgoPersistence) moveCommentThreadText(
	actorID string, thread commentRecord, formerID string,
	authorize func(core.App) (commentAuthorization, error),
) error {
	current, err := authorize(p.app)
	if err != nil {
		return err
	}
	captions := []commentTextPlace{}
	anchor := commentQuoteAnchor{}
	if thread.BlockID != "" && json.Unmarshal(thread.Body, &anchor) == nil {
		for _, place := range anchor.textPlaces(thread.BlockID) {
			if place.caption || place.cell > 0 {
				captions = append(captions, place)
			}
		}
	}
	room := current.Target.ID
	return p.runPageTextWrite(room, func(ctx context.Context, txApp core.App) error {
		authorization, err := authorize(txApp)
		if err != nil {
			return err
		}
		document, err := loadPageTextDocument(txApp, room)
		if err != nil {
			return err
		}
		blockIDs := []string{}
		if err := txApp.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_blocks}} WHERE [[page_id]]={:room} ORDER BY [[id]]
		`).Bind(dbx.Params{"room": room}).Column(&blockIDs); err != nil {
			return err
		}
		type textMark struct {
			root string
			runs []commentAnchorRun
		}
		marks := []textMark{}
		for _, blockID := range blockIDs {
			root := ygoBlockTextRootPrefix + blockID
			if runs := movedCommentAnchorRuns(document.GetText(root).ToDelta(), formerID, thread.ID); len(runs) > 0 {
				marks = append(marks, textMark{root: root, runs: runs})
			}
		}
		if len(marks) > 0 {
			update, changes := transactPageText(document, func(transaction *crdt.Transaction) {
				for _, mark := range marks {
					text := transaction.GetText(mark.root)
					for _, run := range mark.runs {
						text.Format(transaction, run.start, run.length, crdt.Attributes{"commentId": run.threads})
					}
				}
			})
			if err := storeCommentAnchorText(ctx, txApp, authorization, actorID, update, changes); err != nil {
				return err
			}
		}
		changes := make([]commentCaptionChange, 0, len(captions))
		for _, place := range captions {
			changes = append(changes, commentCaptionChange{blockID: place.blockID, cell: place.cell, runs: func(runs []map[string]any) ([]any, bool) {
				return movedCaptionRuns(runs, formerID, thread.ID)
			}})
		}
		captionMarks := &commentCaptionMarks{pageID: room}
		return captionMarks.write(txApp, authorization, actorID, "comment-thread-move:"+thread.ID, changes)
	})
}

// moveCommentThreadCell moves the marks in the database page's value a
// discussion on a property was left on.
func (p *pocketBaseYgoPersistence) moveCommentThreadCell(
	thread commentRecord, formerID string, authorize func(core.App) (commentAuthorization, error),
) error {
	propertyID, valid := commentPropertyAnchor(string(thread.Body))
	if !valid || propertyID == "" {
		return nil
	}
	return p.writeCommentCell(context.Background(), authorize, "comment-thread-move:"+thread.ID,
		func(txApp core.App, owner, sourceID, rowID string) ([]graphOperation, error) {
			return commentCellMoveOperations(txApp, owner, sourceID, rowID, propertyID, formerID, thread.ID)
		})
}

// commentCellMoveOperations writes the row's value with the discussion's
// marks under its new first comment: none when nothing there names it.
func commentCellMoveOperations(
	app core.App, owner, sourceID, rowID, propertyID, formerID, threadID string,
) ([]graphOperation, error) {
	stored := struct {
		Type  string `db:"type"`
		Value string `db:"value"`
		Spans string `db:"spans"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[type]], [[value]], COALESCE([[spans]], '') AS [[spans]] FROM {{leafen_database_row_values}}
		WHERE [[row_id]] = {:row} AND [[property_id]] = {:property}
	`).Bind(dbx.Params{"row": rowID, "property": propertyID}).One(&stored); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	spans := []graphTextSpan{}
	if stored.Spans == "" || json.Unmarshal([]byte(stored.Spans), &spans) != nil {
		return nil, nil
	}
	moved := false
	for index := range spans {
		if threads, changed := commentThreadsMoved(spans[index].CommentID, formerID, threadID); changed {
			spans[index].CommentID, moved = threads, true
		}
	}
	if !moved {
		return nil, nil
	}
	writes := newDatabaseRowWrites(app, owner, nil)
	row, found, err := writes.row(sourceID, rowID)
	if err != nil || !found {
		return nil, err
	}
	row.Values[propertyID] = graphRowValue{Type: stored.Type, Value: json.RawMessage(stored.Value), Spans: spans}
	return writes.operations, nil
}

// commentThreadsMoved is the threads marked on text with one renamed, and
// whether it was there.
func commentThreadsMoved(existing, formerID, threadID string) (string, bool) {
	threads := strings.Fields(existing)
	at := slices.Index(threads, formerID)
	if at < 0 {
		return existing, false
	}
	if slices.Contains(threads, threadID) {
		threads = slices.Delete(threads, at, at+1)
	} else {
		threads[at] = threadID
	}
	return strings.Join(threads, " "), true
}

// movedCommentAnchorRuns are the stretches of a text marked with a former
// first comment's ID, with the threads they will be marked with.
func movedCommentAnchorRuns(delta []crdt.Delta, formerID, threadID string) []commentAnchorRun {
	runs := []commentAnchorRun{}
	at := 0
	for _, part := range delta {
		length := 1
		if text, isText := part.Insert.(string); isText {
			length = len(utf16.Encode([]rune(text)))
		}
		from := at
		at += length
		existing, _ := part.Attributes["commentId"].(string)
		threads, changed := commentThreadsMoved(existing, formerID, threadID)
		if !changed {
			continue
		}
		if last := len(runs) - 1; last >= 0 && runs[last].threads == threads && runs[last].start+runs[last].length == from {
			runs[last].length += length
			continue
		}
		runs = append(runs, commentAnchorRun{start: from, length: length, threads: threads})
	}
	return runs
}

// movedCaptionRuns are a caption's runs with a former first comment's ID
// renamed, and whether any held it.
func movedCaptionRuns(runs []map[string]any, formerID, threadID string) ([]any, bool) {
	moved, changed := make([]any, 0, len(runs)), false
	for _, run := range runs {
		existing, _ := run["commentId"].(string)
		threads, renamed := commentThreadsMoved(existing, formerID, threadID)
		if renamed {
			copied := map[string]any{}
			for key, value := range run {
				copied[key] = value
			}
			copied["commentId"] = threads
			run, changed = copied, true
		}
		moved = append(moved, run)
	}
	return moved, changed
}
