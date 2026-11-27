package app

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A comment on text marks that text, as Notion's discussions do (seen
// 2026-09-30): a new thread's first comment names the text it was left on
// by its quote and offsets, and the text takes the thread's id in the
// page's collaborative text, which the editor underlines while the thread
// is open. Words chosen across blocks are one thread whose mark lies on the
// chosen words of every block, and a media block's caption words take a
// thread too (both seen 2026-11-09): the thread names its own block's part
// and the blocks it runs on into, and a caption's words take the mark in the
// caption's runs (comment_caption_anchors.go). Whoever may comment may
// mark; if the text changed before the comment was stored, the thread is
// kept without the mark.

type commentQuoteAnchor struct {
	Quote       string              `json:"quote"`
	QuoteStart  *int                `json:"quoteStart"`
	QuoteEnd    *int                `json:"quoteEnd"`
	QuoteField  string              `json:"quoteField"`
	QuoteBlocks []commentQuoteBlock `json:"quoteBlocks"`
}

// onWords reports whether a thread's quote marks words: it names where they
// lie. A quote with no place is the block's words a discussion on the block
// itself carries, as Notion's on a code block or a callout (seen 2026-11-22:
// context block with their text-context, nothing marked).
func (anchor commentQuoteAnchor) onWords() bool {
	return strings.TrimSpace(anchor.Quote) != "" && anchor.QuoteStart != nil && anchor.QuoteEnd != nil
}

// commentQuoteCaption is the field a quote's place names for a block's caption.
const commentQuoteCaption = "caption"

// commentTextPlace is a stretch of the text a thread was left on: a block's
// text, its caption, or one of a table row's cells (cell, from 1), from start
// to end in UTF-16 units (comment_table_cell_anchors.go).
type commentTextPlace struct {
	blockID    string
	caption    bool
	cell       int
	start, end int
}

// textPlaces are where a thread's quote lies, in the page's order: its own
// block's part, then the blocks it runs on into. None when a place is not
// one a quote can name, or names a block's text or caption twice.
func (anchor commentQuoteAnchor) textPlaces(blockID string) []commentTextPlace {
	if anchor.QuoteStart == nil || anchor.QuoteEnd == nil {
		return nil
	}
	caption, cell, named := commentQuoteFieldPlace(anchor.QuoteField)
	places := []commentTextPlace{{blockID: blockID, caption: caption, cell: cell, start: *anchor.QuoteStart, end: *anchor.QuoteEnd}}
	if !named || cell > 0 && len(anchor.QuoteBlocks) > 0 {
		return nil
	}
	for _, block := range anchor.QuoteBlocks {
		if block.Field != "" && block.Field != commentQuoteCaption || !validCommentIdentifier(block.BlockID) {
			return nil
		}
		places = append(places, commentTextPlace{blockID: block.BlockID, caption: block.Field == commentQuoteCaption,
			start: block.Start, end: block.End})
	}
	seen := map[commentTextPlace]bool{}
	for _, place := range places {
		field := commentTextPlace{blockID: place.blockID, caption: place.caption, cell: place.cell}
		if place.start < 0 || place.end <= place.start || seen[field] {
			return nil
		}
		seen[field] = true
	}
	return places
}

// anchorCommentText marks the text a new thread was left on with the
// thread's id, in the text of the page or database page that holds it; the
// mark's version names the connection the request came through, as a
// change's does (namePageHistoryConnection), so the tools that count it name
// an agent's thread as its own (mcp_version_discussion_edits.go).
func (p *pocketBaseYgoPersistence) anchorCommentText(
	request context.Context,
	actorID string,
	comment commentRecord,
	authorize func(core.App) (commentAuthorization, error),
) error {
	anchor := commentQuoteAnchor{}
	if p == nil || comment.ParentID != "" || comment.BlockID == "" || json.Unmarshal(comment.Body, &anchor) != nil {
		return nil
	}
	places := anchor.textPlaces(comment.BlockID)
	quote := strings.Join(strings.Fields(anchor.Quote), " ")
	if len(places) == 0 || quote == "" {
		return nil
	}
	current, err := authorize(p.app)
	if err != nil {
		return err
	}
	room := current.Target.ID
	return p.runPageTextWrite(room, func(ctx context.Context, txApp core.App) error {
		defer namePageHistoryConnection(txApp, request)()
		authorization, err := authorize(txApp)
		if err != nil {
			return err
		}
		document, err := loadPageTextDocument(txApp, room)
		if err != nil {
			return err
		}
		captions := &commentCaptionMarks{pageID: room}
		chosen := make([]string, 0, len(places))
		for _, place := range places {
			var units []uint16
			if place.caption || place.cell > 0 {
				if units, err = captions.units(txApp, place); err != nil || units == nil {
					return err
				}
			} else {
				units = utf16.Encode([]rune(document.GetText(ygoBlockTextRootPrefix + place.blockID).ToString()))
			}
			if place.end > len(units) || !suggestionTextBoundary(units, place.start) || !suggestionTextBoundary(units, place.end) {
				return nil
			}
			chosen = append(chosen, string(utf16.Decode(units[place.start:place.end])))
		}
		if strings.Join(strings.Fields(strings.Join(chosen, " ")), " ") != quote {
			return nil
		}
		type textMark struct {
			root string
			runs []commentAnchorRun
		}
		marks := []textMark{}
		for _, place := range places {
			if !place.caption && place.cell == 0 {
				root := ygoBlockTextRootPrefix + place.blockID
				marks = append(marks, textMark{root: root, runs: commentAnchorRuns(document.GetText(root).ToDelta(), place.start, place.end, comment.ID)})
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
		return captions.mark(txApp, authorization, actorID, places, comment.ID)
	})
}

// commentAnchorRun is a stretch of text and the threads marked on it, which a
// text's commentId holds separated by spaces: text can have several, as
// Notion's (seen 2026-11-04), and a mark of one thread is a list of one.
type commentAnchorRun struct {
	start, length int
	threads       string
}

// commentThreadsWith is the threads marked on text with one more added.
func commentThreadsWith(existing, threadID string) string {
	if existing == "" {
		return threadID
	}
	if slices.Contains(strings.Fields(existing), threadID) {
		return existing
	}
	return existing + " " + threadID
}

// commentAnchorRuns adds a thread to what is already marked on [start, end),
// run by run, so the threads there keep their marks.
func commentAnchorRuns(delta []crdt.Delta, start, end int, threadID string) []commentAnchorRun {
	runs := []commentAnchorRun{}
	at := 0
	for _, part := range delta {
		length := 1
		if text, isText := part.Insert.(string); isText {
			length = len(utf16.Encode([]rune(text)))
		}
		from, to := max(at, start), min(at+length, end)
		at += length
		if from >= to {
			continue
		}
		existing, _ := part.Attributes["commentId"].(string)
		threads := commentThreadsWith(existing, threadID)
		if last := len(runs) - 1; last >= 0 && runs[last].threads == threads && runs[last].start+runs[last].length == from {
			runs[last].length += to - from
			continue
		}
		runs = append(runs, commentAnchorRun{start: from, length: to - from, threads: threads})
	}
	return runs
}

// storeCommentAnchorText stores a thread's mark in a page's text as its
// other text changes are stored — in its history and in its open room once
// committed — as no edit: the commenter is not its last editor, a database
// page keeps its last edited time, and its history keeps the mark as comment
// marks (pageHistoryVersionCommentMarks).
func storeCommentAnchorText(
	ctx context.Context, app core.App, authorization commentAuthorization, actorID string,
	update []byte, changes []crdt.TextChange,
) error {
	if len(update) == 0 {
		return nil
	}
	pageID := authorization.Target.ID
	if err := ensurePageHistoryBaseline(app, pageID); err != nil {
		return err
	}
	if err := appendYgoUpdate(app, pageID, update); err != nil {
		return err
	}
	publishStoredYgoUpdateAfterCommit(app, pageID, update)
	if err := recordPageHistoryCommentMarks(app, pageID, actorID, update, changes); err != nil {
		return err
	}
	if authorization.Target.Kind == commentTargetDatabaseRow {
		return noteDatabaseRowBodyMarks(app, pageID)
	}
	return ctx.Err()
}

// anchorNewCommentText marks a new thread's text once its comment is stored;
// a mark that can't be made leaves the comment as it is.
func anchorNewCommentText(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	actorID string,
	response commentMutationResponse,
	authorize func(core.App) (commentAuthorization, error),
) {
	if response.Duplicate {
		return
	}
	if err := ygo.anchorCommentText(event.Request.Context(), actorID, response.Comment, authorize); err != nil {
		event.App.Logger().Warn("comment text anchor skipped", "comment", response.Comment.ID, "error", err)
	}
}
