package app

import (
	"context"
	"slices"
	"unicode/utf16"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// notionMarkdownKeptPieces is a block's text that page Markdown rewrites
// with its words and marks as they are, in the pieces Notion leaves it in:
// its reading's own, which it joins only for a block whose words or marks
// change (seen 2026-11-11, worker w319: "a :x: b" — a name no custom emoji
// has — is "a ", ":x:" and " b"; "**a****b**" and "<b>a</b><b>b</b>" two
// bold pieces, two like <span>s and two <code>s two; "[a](u)[b](u)", an
// escape, an entity, a <br> and "10:30:45" one piece; the words a "v1"
// mention tag reads as a piece of their own, and a person's tag of no
// address an empty one, which a block whose words change leaves out). A
// place its cuts name twice holds an empty piece of text
// (notionMarkdownTagCuts). Nil when no piece of it stands apart from a
// neighbour it reads like.
func notionMarkdownKeptPieces(item workspaceImportStagedItem) []graphTextSpan {
	named := false
	for _, span := range item.Spans {
		named = named || span.Mention == "custom_emoji" || span.EmptyPiece == graphTextEmptyText
	}
	if len(item.Cuts) == 0 && !named {
		return nil
	}
	spans := item.Spans
	if spans == nil && item.Text != "" {
		spans = []graphTextSpan{{Text: item.Text}}
	}
	empties, bounds := map[int]int{}, []int{}
	for index, cut := range item.Cuts {
		if index > 0 && item.Cuts[index-1] == cut {
			empties[cut]++
		} else {
			bounds = append(bounds, cut)
		}
	}
	pieces := make([]graphTextSpan, 0, len(spans)+len(item.Cuts))
	empty := func(place int) {
		for range empties[place] {
			pieces = append(pieces, graphTextSpan{EmptyPiece: graphTextEmptyText})
		}
		delete(empties, place)
	}
	offset, next := 0, 0
	for _, span := range spans {
		end := offset + len(span.Text)
		empty(offset)
		for next < len(bounds) && bounds[next] <= offset {
			next++
		}
		at := offset
		for ; next < len(bounds) && bounds[next] < end; next++ {
			part := span
			part.Text = span.Text[at-offset : bounds[next]-offset]
			pieces = append(pieces, part)
			at = bounds[next]
			empty(at)
		}
		part := span
		part.Text = span.Text[at-offset:]
		pieces = append(pieces, part)
		offset = end
	}
	empty(offset)
	if offset != len(item.Text) || len(empties) > 0 {
		return nil
	}
	return pieces
}

// graphTextPieceFormat is the piece mark one stretch of a block's text
// takes (graphTextPieceKey).
type graphTextPieceFormat struct {
	start, length int
	apart         bool
}

// graphTextKeptPieceFormats are the piece marks that keep a text's pieces
// apart as pieces reads it, without changing its words or other marks: the
// second, fourth … of a row of pieces that read alike carry one, as
// graphTextPieced gives a row of mentions. Nil when the text isn't the
// pieces' words, or when it takes no mark and holds none.
func graphTextKeptPieceFormats(deltas []crdt.Delta, pieces []graphTextSpan) []graphTextPieceFormat {
	written, marked := []any{}, false
	for _, delta := range deltas {
		if delta.Op != crdt.DeltaOpInsert {
			continue
		}
		marked = marked || delta.Attributes[graphTextPieceKey] == true
		if text, ok := delta.Insert.(string); ok {
			for _, unit := range utf16.Encode([]rune(text)) {
				written = append(written, unit)
			}
		} else {
			written = append(written, graphTextEmptyPieceKind(delta.Insert))
		}
	}
	formats := make([]graphTextPieceFormat, 0, len(pieces))
	var previous crdt.Attributes
	at, apart, needed := 0, false, false
	for _, piece := range pieces {
		units := []any{}
		if graphTextEmptyPieceSpan(piece) {
			units = append(units, piece.EmptyPiece)
		} else {
			for _, unit := range utf16.Encode([]rune(piece.Text)) {
				units = append(units, unit)
			}
		}
		if len(units) == 0 {
			continue
		}
		if at+len(units) > len(written) {
			return nil
		}
		for index, unit := range units {
			if written[at+index] != unit {
				return nil
			}
		}
		attributes := graphTextAttributes(piece)
		if previous != nil && !graphTextEmptyPieceSpan(piece) && graphTextSameFormatting(previous, attributes) {
			apart = !apart
		} else {
			apart = false
		}
		previous = attributes
		if graphTextEmptyPieceSpan(piece) {
			previous = nil
		}
		needed = needed || apart
		formats = append(formats, graphTextPieceFormat{start: at, length: len(units), apart: apart})
		at += len(units)
	}
	if at != len(written) || !needed && !marked {
		return nil
	}
	return formats
}

// keepNotionMarkdownTextPieces gives the texts page Markdown leaves as they
// are the piece marks of the pieces its lines read them in, when the
// Markdown changes no block: stored as the page's other text changes are,
// its words, editor and search as they were.
func keepNotionMarkdownTextPieces(ctx context.Context, app core.App, page *core.Record, actorID string, pieces []graphTextUpdate) error {
	if len(pieces) == 0 {
		return nil
	}
	return app.RunInTransaction(func(txApp core.App) error {
		if err := resolveNotionMarkdownCustomEmojis(txApp, page.GetString("workspace"), pieces); err != nil {
			return err
		}
		document, err := loadPageTextDocument(txApp, page.Id)
		if err != nil {
			return err
		}
		formats := map[string][]graphTextPieceFormat{}
		empties := map[string][]graphTextEmptyPieceEdit{}
		// No line here changes a block, so a :name: a custom emoji has is a
		// mention in its text already (notionMarkdownKeptTextUpdates).
		for _, kept := range pieces {
			graphTextKeptPieceChanges(document.GetText(ygoBlockTextRootPrefix+kept.BlockID).ToDelta(), kept, formats, empties)
		}
		if len(formats) == 0 && len(empties) == 0 {
			return nil
		}
		update, changes := transactPageText(document, func(transaction *crdt.Transaction) {
			graphTextPutEmptyPieces(transaction, empties)
			graphTextApplyPieceFormats(transaction, formats)
		})
		if len(update) == 0 {
			return nil
		}
		if err := ensurePageHistoryBaseline(txApp, page.Id); err != nil {
			return err
		}
		if err := appendYgoUpdate(txApp, page.Id, update); err != nil {
			return err
		}
		publishStoredYgoUpdateAfterCommit(txApp, page.Id, update)
		if err := recordPageHistoryCollaborationUpdate(txApp, page.Id, actorID, update, changes); err != nil {
			return err
		}
		if isDatabaseRowPageRecord(page) {
			return noteDatabaseRowBodyChange(txApp, page.Id, "")
		}
		return ctx.Err()
	})
}

// graphTextKeptPieceChanges notes what keeps a text page Markdown leaves as
// it is in its line's pieces: their piece marks in formats, or, for one
// whose empty pieces of text aren't its line's, where they go in or out in
// edits (graphTextEmptyPieceEdits), its marks given once they are
// (graphTextPutEmptyPieces).
func graphTextKeptPieceChanges(deltas []crdt.Delta, kept graphTextUpdate, formats map[string][]graphTextPieceFormat, edits map[string][]graphTextEmptyPieceEdit) {
	if marks := graphTextKeptPieceFormats(deltas, kept.Spans); marks != nil {
		formats[kept.BlockID] = marks
		return
	}
	changes := graphTextEmptyPieceEdits(deltas, kept.Spans)
	if changes == nil {
		return
	}
	edits[kept.BlockID] = changes
	if marks := graphTextKeptPieceFormats(graphTextEditedDeltas(deltas, changes), kept.Spans); marks != nil {
		formats[kept.BlockID] = marks
	}
}

// graphTextEditedDeltas is a text's inserts as its empty piece edits leave
// it, a character or an embed each.
func graphTextEditedDeltas(deltas []crdt.Delta, edits []graphTextEmptyPieceEdit) []crdt.Delta {
	units := []crdt.Delta{}
	for _, delta := range deltas {
		if delta.Op != crdt.DeltaOpInsert {
			continue
		}
		text, ok := delta.Insert.(string)
		if !ok {
			units = append(units, delta)
			continue
		}
		for _, character := range text {
			units = append(units, crdt.Delta{Op: crdt.DeltaOpInsert, Insert: string(character), Attributes: delta.Attributes})
		}
	}
	for _, edit := range edits {
		index, at := 0, 0
		for index < len(units) && at < edit.at {
			at += graphTextDeltaLength(units[index])
			index++
		}
		if edit.put {
			units = slices.Insert(units, index, crdt.Delta{Op: crdt.DeltaOpInsert, Insert: graphTextEmptyPieceEmbed(graphTextEmptyText)})
		} else if index < len(units) {
			units = slices.Delete(units, index, index+1)
		}
	}
	return units
}

// graphTextEmptyPieceEdit is an empty piece of text put in at a place of a
// text, or taken out, as the text stands when it is made.
type graphTextEmptyPieceEdit struct {
	at  int
	put bool
}

// graphTextEmptyPieceEdits are what makes a text whose words are the
// pieces' hold the empty pieces of text they do: each one they hold that it
// lacks put in, each one it holds that they don't taken out, in turn. Nil
// when there is none, or when its words aren't the pieces'.
func graphTextEmptyPieceEdits(deltas []crdt.Delta, pieces []graphTextSpan) []graphTextEmptyPieceEdit {
	written := []any{}
	for _, delta := range deltas {
		if delta.Op != crdt.DeltaOpInsert {
			continue
		}
		if text, ok := delta.Insert.(string); ok {
			for _, unit := range utf16.Encode([]rune(text)) {
				written = append(written, unit)
			}
		} else {
			written = append(written, graphTextEmptyPieceKind(delta.Insert))
		}
	}
	units := []any{}
	for _, piece := range pieces {
		if graphTextEmptyPieceSpan(piece) {
			units = append(units, piece.EmptyPiece)
			continue
		}
		for _, unit := range utf16.Encode([]rune(piece.Text)) {
			units = append(units, unit)
		}
	}
	edits, at, place := []graphTextEmptyPieceEdit{}, 0, 0
	for _, unit := range units {
		for at < len(written) && written[at] == any(graphTextEmptyText) && unit != any(graphTextEmptyText) {
			edits = append(edits, graphTextEmptyPieceEdit{at: place})
			at++
		}
		switch {
		case at < len(written) && written[at] == unit:
			at++
		case unit == any(graphTextEmptyText):
			edits = append(edits, graphTextEmptyPieceEdit{at: place, put: true})
		default:
			return nil
		}
		place++
	}
	for ; at < len(written) && written[at] == any(graphTextEmptyText); at++ {
		edits = append(edits, graphTextEmptyPieceEdit{at: place})
	}
	if at != len(written) || len(edits) == 0 {
		return nil
	}
	return edits
}

// graphTextPutEmptyPieces makes the kept texts' empty pieces of text their
// lines' (graphTextEmptyPieceEdits), of no marks, by block.
func graphTextPutEmptyPieces(transaction *crdt.Transaction, edits map[string][]graphTextEmptyPieceEdit) {
	for blockID, changes := range edits {
		written := transaction.GetText(ygoBlockTextRootPrefix + blockID)
		for _, change := range changes {
			if !change.put {
				written.Delete(transaction, change.at, 1)
				continue
			}
			attributes := graphTextAttributes(graphTextSpan{EmptyPiece: graphTextEmptyText})
			attributes[graphTextPieceKey] = nil
			written.InsertEmbed(transaction, change.at, graphTextEmptyPieceEmbed(graphTextEmptyText), attributes)
		}
	}
}

// graphTextApplyPieceFormats sets the piece marks of texts, by block.
func graphTextApplyPieceFormats(transaction *crdt.Transaction, formats map[string][]graphTextPieceFormat) {
	for blockID, marks := range formats {
		text := transaction.GetText(ygoBlockTextRootPrefix + blockID)
		for _, mark := range marks {
			var value any
			if mark.apart {
				value = true
			}
			text.Format(transaction, mark.start, mark.length, crdt.Attributes{graphTextPieceKey: value})
		}
	}
}

// notionMarkdownSameWords reports whether two readings of a line hold the
// same words and marks, as the block's text keeps them: neighbours alike
// joined, and a :name: read as its words, as one no custom emoji has
// (notionMarkdownKeptTextUpdates writes the line again when one has).
func notionMarkdownSameWords(leftText string, left []graphTextSpan, rightText string, right []graphTextSpan) bool {
	return leftText == rightText && sameGraphTextSpans(notionMarkdownWords(leftText, left), notionMarkdownWords(rightText, right))
}

func notionMarkdownWords(text string, spans []graphTextSpan) []graphTextSpan {
	if spans == nil && text != "" {
		spans = []graphTextSpan{{Text: text}}
	}
	plain := func(span graphTextSpan) bool {
		return span.Mention == "" && !graphTextSpanIsEquation(span) && !graphTextEmptyPieceSpan(span)
	}
	words := make([]graphTextSpan, 0, len(spans))
	for _, span := range spans {
		if span.Mention == "custom_emoji" && span.CustomEmojiID == "" {
			span.Mention = ""
		}
		// An empty piece of text holds none: one is the reading's to
		// keep (notionMarkdownKeptPieces).
		if span.Text == "" && span.EmptyPiece != graphTextEmptyEquation {
			continue
		}
		if last := len(words) - 1; last >= 0 && plain(span) && plain(words[last]) {
			alike := words[last]
			alike.Text = span.Text
			if alike == span {
				words[last].Text += span.Text
				continue
			}
		}
		words = append(words, span)
	}
	return words
}

// notionMarkdownKeptTextUpdates moves the kept texts whose :name: a custom
// emoji has, once resolved, to the text updates, when the change writes their
// block: a line written otherwise with the same words (an escaped :name:
// unescaped) makes a mention of it, which changes their text. A line left as
// it was, whose block the change doesn't write, holds that mention already
// and keeps its pieces, as a body no line of which changes does
// (keepNotionMarkdownTextPieces) — a text update goes with its block's
// upsert (validateContentGraphCompoundInput), so a line added after one with
// a custom emoji was refused (2026-11-12). A moved one is left with no block.
func notionMarkdownKeptTextUpdates(kept []graphTextUpdate, updates []graphTextUpdate, operations []graphOperation) []graphTextUpdate {
	written := map[string]bool{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil {
			written[operation.Block.ID] = true
		}
	}
	for index, text := range kept {
		if !written[text.BlockID] {
			continue
		}
		for _, span := range text.Spans {
			if span.Mention == "custom_emoji" && span.CustomEmojiID != "" {
				updates = append(updates, text)
				kept[index].BlockID = ""
				break
			}
		}
	}
	return updates
}
