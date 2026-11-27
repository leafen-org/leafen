package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func contentGraphCompoundEnabled(input graphMutationInput) bool {
	return input.BasePageCursor != nil || input.BaseGraphRevision != nil ||
		input.PageTitle != nil || len(input.TextUpdates) > 0
}

func validateContentGraphCompoundInput(input *graphMutationInput) error {
	// As many blocks as one Notion request holds (1,000); their text is
	// held to nothing more than one update's bytes (maxYgoUpdateBytes).
	if len(input.Operations) > maxGraphMutationOperations {
		return graphRefused(errorCodeGraphInvalid, "Reduce the content change size.",
			"operations holds %d changes, over %d: send them as more than one change", len(input.Operations), maxGraphMutationOperations)
	}
	if !contentGraphCompoundEnabled(*input) {
		// A change of the page's kind alone moves no blocks.
		if len(input.Operations) == 0 && input.PageKind == nil {
			return graphRefused(errorCodeGraphInvalid, "Check the content change details.",
				"operations is empty: give at least one change, each {\"kind\", and its one payload}")
		}
		return nil
	}
	// Text goes with the blocks it is written into and needs no title; a
	// title comes with the page cursor and graph revision it was read at, its
	// spaces kept as a page's title keeps them. A body the server writes for
	// the Notion-compatible API or MCP sends none: its change notes the edit
	// on the page (noteGraphPageEdited) and says what it changed.
	switch {
	case len(input.Operations) == 0:
		return graphRefused(errorCodeGraphInvalid, "Check the page paste details.",
			"operations is empty: textUpdates, pageTitle and the base cursors go with the blocks they write")
	case input.BasePageCursor != nil && *input.BasePageCursor < 0, input.BaseGraphRevision != nil && *input.BaseGraphRevision < 0:
		return graphRefused(errorCodeGraphInvalid, "Check the page paste details.",
			"%s are 0 or more", graphBaseNumbersNamed)
	case input.PageTitle != nil && (input.BasePageCursor == nil || input.BaseGraphRevision == nil):
		return graphRefused(errorCodeGraphInvalid, "Check the page paste details.",
			"pageTitle goes with %s", graphBaseNumbersNamed)
	case input.PageTitle != nil && len([]rune(*input.PageTitle)) > maxGraphTitleRunes:
		return graphRefused(errorCodeGraphInvalid, "Check the page paste details.",
			"pageTitle is %d characters, over %d", len([]rune(*input.PageTitle)), maxGraphTitleRunes)
	}
	upserts := make(map[string]string)
	for _, operation := range input.Operations {
		if operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil {
			upserts[operation.Block.ID] = operation.Block.Type
		}
	}
	seen := make(map[string]struct{}, len(input.TextUpdates))
	for index, update := range input.TextUpdates {
		blockType, ok := upserts[update.BlockID]
		if !ok || update.BlockID == "" || len(update.BlockID) > maxGraphIdentifierLength {
			return graphRefused(errorCodeGraphTextTargetConflict, "Check the text block target.",
				"textUpdates[%d].blockId %q names no block an upsertBlock of this change writes: a block's text goes with its upsertBlock", index, update.BlockID)
		}
		if _, duplicate := seen[update.BlockID]; duplicate {
			return graphRefused(errorCodeGraphTextTargetConflict, "The text block target is duplicated.",
				"textUpdates[%d].blockId %q has an earlier text update in this change: one update a block", index, update.BlockID)
		}
		if _, ok := graphTextBlockTypes[blockType]; !ok {
			return graphRefused(errorCodeGraphTextTargetConflict, "This block doesn't support text.",
				"textUpdates[%d].blockId %q is a %s block, which holds no text", index, update.BlockID, blockType)
		}
		seen[update.BlockID] = struct{}{}
		for spanIndex, span := range update.Spans {
			at := fmt.Sprintf("textUpdates[%d].spans[%d]", index, spanIndex)
			if span.Equation != "" && (span.Text != span.Equation ||
				len([]rune(span.Equation)) > maxNotionInlineEquationRunes ||
				span.Link != "" || span.Mention != "" || span.PageID != "" ||
				span.DatabaseID != "" || span.CustomEmojiID != "" ||
				span.Date != "" || span.DateEnd != "" || span.DateTimeZone != "" ||
				span.UserID != "" || span.IconURL != "") {
				return graphRefused(errorCodeGraphInvalid, "Check the text equation.",
					"%s.equation goes with text the same and nothing else (up to %d characters)", at, maxNotionInlineEquationRunes)
			}
			// An empty piece has no words, and an empty equation no link or
			// mention (graph_text_empty_pieces.go).
			if span.EmptyPiece != "" && (!graphTextEmptyPieceSpan(span) || span.Text != "" || span.Equation != "" ||
				span.Mention != "" || span.EmptyPiece == graphTextEmptyEquation && span.Link != "") {
				return graphRefused(errorCodeGraphInvalid, "Check the text formatting.", "%s.emptyPiece goes with no text, equation or mention", at)
			}
			if span.Mention != "" && span.Mention != "page" && span.Mention != "database" && span.Mention != "custom_emoji" && span.Mention != "date" &&
				span.Mention != "person" && span.Mention != "external" && span.Mention != graphTemplateMention {
				return graphRefused(errorCodeGraphInvalid, "Check the text formatting.",
					"%s.mention %q isn't page, database, custom_emoji, date, person, external or %s", at, span.Mention, graphTemplateMention)
			}
			if (span.Mention == graphTemplateMention) != (span.Template != "") ||
				span.Template != "" && !validGraphTemplateMention(span.Template) {
				return graphRefused(errorCodeGraphInvalid, "Check the text formatting.", "%s.template goes with mention %q, and it alone", at, graphTemplateMention)
			}
			if !validGraphDateStartUnset(span) {
				return graphRefused(errorCodeGraphInvalid, "Check the text formatting.", "%s.dateStartUnset goes with mention \"date\" whose date is empty or a time alone (\"T…\")", at)
			}
			if span.DateReminder != "" && (span.Mention != "date" || !validDateMentionReminder(span.Date, span.DateReminder)) {
				return graphRefused(errorCodeGraphInvalid, "Check the reminder.", "%s.dateReminder goes with a date mention it can remind of", at)
			}
			if span.DateFormat != "" && (span.Mention != "date" || !validDateMentionFormat(span.DateFormat)) {
				return graphRefused(errorCodeGraphInvalid, "Check the date format.", "%s.dateFormat goes with a date mention, in a format its menu names", at)
			}
			if span.DateTimeFormat != "" && (span.Mention != "date" || !validDateMentionTimeFormat(span.DateTimeFormat)) {
				return graphRefused(errorCodeGraphInvalid, "Check the time format.", "%s.dateTimeFormat goes with a date mention, in a format its menu names", at)
			}
			if !validGraphLinkMentionPreview(span) {
				return graphRefused(errorCodeGraphInvalid, "Check the link mention.", "%s's provider, description and imageUrl go with mention \"external\", imageUrl a public https address", at)
			}
			if problem := graphTextSpanColorProblem(span); problem != "" {
				return graphRefused(errorCodeGraphInvalid, "Check the text color.", "%s.%s", at, problem)
			}
		}
	}
	return nil
}

func contentGraphCompoundRequestHash(input graphMutationInput) (string, error) {
	value := any(input.Operations)
	prefix := ""
	if contentGraphCompoundEnabled(input) || input.PageKind != nil {
		value = input
		prefix = "compound-v1\x00"
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", graphInputError(errorCodeGraphInvalid, "Check the content change details.")
	}
	digest := sha256.Sum256(append([]byte(prefix), canonical...))
	return hex.EncodeToString(digest[:]), nil
}

func assertContentGraphRevision(
	app core.App,
	ownerID, pageID string,
	baseRevision *int,
) error {
	if baseRevision == nil {
		return nil
	}
	var current int
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[cursor]]), 0) FROM {{leafen_graph_mutations}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"owner": ownerID, "pageId": pageID}).Row(&current); err != nil {
		return err
	}
	if current != *baseRevision {
		return graphConflictError(errorCodeGraphRevisionConflict, "Load the latest page content again.")
	}
	return nil
}

func assertContentGraphPageCursor(page *core.Record, baseCursor *int) error {
	if baseCursor != nil && page.GetInt("cursor") != *baseCursor {
		return graphConflictError(errorCodeGraphPageCursorConflict, "Load the latest page state again.")
	}
	return nil
}

func applyContentGraphPageTitle(
	app core.App,
	authorization pageAuthorization,
	actorID string,
	input graphMutationInput,
) (*graphPageMutationReceipt, error) {
	if input.PageTitle == nil {
		return nil, nil
	}
	// The mutation owner checked the requested cursor before applying the
	// page kind under the same room transaction. Checking it again here
	// would mistake that command's own kind change for a concurrent edit.
	committed, err := commitPageMetadata(
		app, authorization, actorID,
		func(page *core.Record, _ int64) { setPageTitle(page, *input.PageTitle, nil) },
		func(app core.App, page *core.Record) error {
			return markSearchPageFieldsDirty(context.Background(), app, page)
		}, false,
	)
	if err != nil {
		return nil, err
	}
	return contentGraphPageMutationReceipt(committed.Page), nil
}

func contentGraphPageMutationReceipt(page *core.Record) *graphPageMutationReceipt {
	return &graphPageMutationReceipt{
		Title:        page.GetString("title"),
		Revision:     page.GetInt("revision"),
		Cursor:       page.GetInt("cursor"),
		UpdatedAt:    page.GetString("updated"),
		LastEditedBy: page.GetString("last_edited_by"),
	}
}

func graphTextAttributes(span graphTextSpan) crdt.Attributes {
	attributes := crdt.Attributes{
		"bold": nil, "italic": nil, "underline": nil, "strikethrough": nil, "code": nil,
		"color": nil, "background": nil, "link": nil, "commentId": nil, "mention": nil,
		"pageId": nil, "blockId": nil, "databaseId": nil, "customEmojiId": nil,
		"date": nil, "dateStartUnset": nil, "dateEnd": nil, "dateTimeZone": nil, "dateReminder": nil, "dateFormat": nil, "dateTimeFormat": nil,
		"userId": nil, "iconUrl": nil, "provider": nil, "description": nil, "imageUrl": nil, "equation": nil, "template": nil,
		"suggestion": nil, "suggestionId": nil, "suggestionBefore": nil, graphTextPieceKey: nil,
	}
	if span.Bold {
		attributes["bold"] = true
	}
	if span.Italic {
		attributes["italic"] = true
	}
	if span.Underline {
		attributes["underline"] = true
	}
	if span.Strikethrough {
		attributes["strikethrough"] = true
	}
	if span.Code {
		attributes["code"] = true
	}
	if span.Color != "" {
		attributes["color"] = span.Color
	}
	if span.Background != "" {
		attributes["background"] = span.Background
	}
	if span.Link != "" {
		attributes["link"] = span.Link
	}
	if span.CommentID != "" {
		attributes["commentId"] = span.CommentID
	}
	if span.Mention != "" {
		attributes["mention"] = span.Mention
	}
	if span.PageID != "" {
		attributes["pageId"] = span.PageID
	}
	if span.BlockID != "" {
		attributes["blockId"] = span.BlockID
	}
	if span.DatabaseID != "" {
		attributes["databaseId"] = span.DatabaseID
	}
	if span.CustomEmojiID != "" {
		attributes["customEmojiId"] = span.CustomEmojiID
	}
	if span.Date != "" || span.Mention == "date" {
		// A date mention keeps its date even empty, as Markdown may write it
		// (notionMarkdownDateSpan).
		attributes["date"] = span.Date
	}
	if span.DateStartUnset {
		attributes["dateStartUnset"] = true
	}
	if span.DateEnd != "" {
		attributes["dateEnd"] = span.DateEnd
	}
	if span.DateTimeZone != "" {
		attributes["dateTimeZone"] = span.DateTimeZone
	}
	if span.DateReminder != "" {
		attributes["dateReminder"] = span.DateReminder
	}
	if span.DateFormat != "" {
		attributes["dateFormat"] = span.DateFormat
	}
	if span.DateTimeFormat != "" {
		attributes["dateTimeFormat"] = span.DateTimeFormat
	}
	if span.UserID != "" {
		attributes["userId"] = span.UserID
	}
	if span.IconURL != "" {
		attributes["iconUrl"] = span.IconURL
	}
	for key, value := range map[string]string{"provider": span.Provider, "description": span.Description, "imageUrl": span.ImageURL} {
		if value != "" {
			attributes[key] = value
		}
	}
	if span.Equation != "" {
		attributes["equation"] = span.Equation
	}
	if span.Template != "" {
		attributes["template"] = span.Template
	}
	if span.Suggestion != "" {
		attributes["suggestion"] = span.Suggestion
	}
	if span.SuggestionID != "" {
		attributes["suggestionId"] = span.SuggestionID
	}
	if span.SuggestionBefore != "" {
		attributes["suggestionBefore"] = span.SuggestionBefore
	}
	return attributes
}

func applyContentGraphTextUpdates(
	app core.App,
	pageID, actorID string,
	updates []graphTextUpdate,
	lifted map[string]struct{},
	deletedBlockIDs []string,
	pieces []graphTextUpdate,
	replaceExisting bool,
	sourceTextCopies ...map[string]string,
) (string, error) {
	if len(updates) == 0 && len(deletedBlockIDs) == 0 && len(pieces) == 0 {
		return "", nil
	}
	// The text document the page's last change left, when nothing changed it
	// since (ygo_structural_document_cache.go): read again from its stored
	// parts for every change, an agent's batches on a long page grew slower
	// with it (2026-10-15). Its mark says so (ygo_document_marks.go).
	mark, marked, err := loadYgoStreamMark(app, ygoCollaborationStream, pageID)
	if err != nil {
		return "", err
	}
	writer := ygoServerWriters.lease(app, ygoCollaborationStream, pageID)
	var document *crdt.Doc
	bytes := 0
	if marked {
		document, bytes = writer.take(ygoTextDocuments, pageID, mark)
	}
	if document == nil {
		_, parts, _, err := loadYgoStreamParts(app, ygoCollaborationStream, pageID)
		if err != nil {
			return "", err
		}
		if document, err = writer.document(parts); err != nil {
			return "", err
		}
		bytes = ygoStreamPartsBytes(parts)
	}
	updates = withoutLiftedTextOverWords(document, updates, lifted)
	for _, update := range updates {
		if !replaceExisting && document.GetText(ygoBlockTextRootPrefix+update.BlockID).Len() != 0 {
			return "", graphConflictError(errorCodeGraphTextTargetConflict, "The text block already changed.")
		}
	}
	effectiveDeletedBlockIDs := make([]string, 0, len(deletedBlockIDs))
	deletedBlockSet := make(map[string]struct{}, len(deletedBlockIDs))
	for _, blockID := range deletedBlockIDs {
		deletedBlockSet[blockID] = struct{}{}
		if document.GetText(ygoBlockTextRootPrefix+blockID).Len() > 0 {
			effectiveDeletedBlockIDs = append(effectiveDeletedBlockIDs, blockID)
		}
	}
	effectiveUpdates := make([]graphTextUpdate, 0, len(updates))
	for _, update := range updates {
		if _, deleted := deletedBlockSet[update.BlockID]; !deleted {
			effectiveUpdates = append(effectiveUpdates, update)
		}
	}
	// A text left as it is keeps its words and takes only its pieces' marks.
	pieceFormats, emptyPieces := map[string][]graphTextPieceFormat{}, map[string][]graphTextEmptyPieceEdit{}
	for _, kept := range pieces {
		if _, deleted := deletedBlockSet[kept.BlockID]; deleted || kept.BlockID == "" {
			continue
		}
		graphTextKeptPieceChanges(document.GetText(ygoBlockTextRootPrefix+kept.BlockID).ToDelta(), kept, pieceFormats, emptyPieces)
	}
	// A document left as it is goes back as it is.
	unchanged := func() {
		if marked {
			ygoTextDocuments.put(pageID, mark, document, bytes)
		}
	}
	if len(effectiveUpdates) == 0 && len(effectiveDeletedBlockIDs) == 0 && len(pieceFormats) == 0 && len(emptyPieces) == 0 {
		unchanged()
		return "", nil
	}
	// A copied block's text is read before the change: reading a text takes
	// the document's lock, which Transact holds for its callback
	// (YText.ToDelta), so a read inside it waits on itself for good.
	copiedDeltas := map[string][]crdt.Delta{}
	if len(sourceTextCopies) == 1 {
		for _, update := range effectiveUpdates {
			if source := sourceTextCopies[0][update.BlockID]; source != "" {
				copiedDeltas[update.BlockID] = document.GetText(ygoBlockTextRootPrefix + source).ToDelta()
			}
		}
	}
	var details crdt.UpdateDetails
	unsubscribe := document.OnUpdateDetails(func(value crdt.UpdateDetails) { details = value })
	document.Transact(func(transaction *crdt.Transaction) {
		for _, blockID := range effectiveDeletedBlockIDs {
			text := transaction.GetText(ygoBlockTextRootPrefix + blockID)
			if text.Len() > 0 {
				text.Delete(transaction, 0, text.Len())
			}
		}
		for _, update := range effectiveUpdates {
			text := transaction.GetText(ygoBlockTextRootPrefix + update.BlockID)
			if replaceExisting && text.Len() > 0 {
				text.Delete(transaction, 0, text.Len())
			}
			if delta, copied := copiedDeltas[update.BlockID]; copied {
				text.ApplyDelta(transaction, delta)
				continue
			}
			insertGraphTextSpans(transaction, text, update.Spans)
		}
		graphTextPutEmptyPieces(transaction, emptyPieces)
		graphTextApplyPieceFormats(transaction, pieceFormats)
	})
	unsubscribe()
	if len(details.Update) == 0 {
		unchanged()
		return "", nil
	}
	if len(details.Update) > maxYgoUpdateBytes {
		return "", graphInputError(errorCodeGraphChangeTooLarge, "The pasted text is too large for one change (64 MB). Paste it in parts.")
	}
	if err := appendYgoUpdate(app, pageID, details.Update); err != nil {
		return "", err
	}
	writer.keep(app, document)
	// The document stays for the page's next change, once this one is kept,
	// under the mark the stored one has with it.
	if transaction := app.TxInfo(); transaction != nil && marked {
		after, stillMarked, err := loadYgoStreamMark(app, ygoCollaborationStream, pageID)
		if err != nil {
			return "", err
		}
		bytes += len(details.Update)
		transaction.OnComplete(func(txErr error) error {
			if txErr == nil && stillMarked {
				ygoTextDocuments.put(pageID, after, document, bytes)
			}
			return nil
		})
	}
	publishStoredYgoUpdateAfterCommit(app, pageID, details.Update)
	if err := recordPageHistoryCollaborationUpdate(
		app, pageID, actorID, details.Update, details.TextChanges,
	); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(details.Update), nil
}

func contentGraphDeletedTextBlockIDs(
	document *crdt.Doc,
	operations []graphOperation,
) ([]string, error) {
	// A change deleting nothing reads no block's metadata.
	deletes := false
	// A block its writer took the text of itself (keepText: the page's editor
	// does, in its collaborative text) leaves that text to the writer. Taken
	// here too, it took the text a quick ⌘Z had put back before the slower
	// deletion came, and the line came back empty (2026-11-08).
	writerTook := map[string]struct{}{}
	for _, operation := range operations {
		deletes = deletes || operation.Kind == graphOperationKindDeleteBlock && operation.Deletion != nil
		if operation.Kind == graphOperationKindDeleteBlock && operation.Deletion != nil && operation.Deletion.KeepText {
			writerTook[operation.Deletion.BlockID] = struct{}{}
		}
	}
	if !deletes {
		return []string{}, nil
	}
	metadata, err := structuralMetadata(document)
	if err != nil {
		return nil, err
	}
	// The blocks go as the change takes them, each operation after the ones
	// before it: a block moved out of one before it goes stays, with its
	// text (2026-10-29: Markdown that rewrote a list item and the item under
	// it left the moved item empty).
	parents := make(map[string]structuralBlockMetadata, len(metadata))
	for id, entry := range metadata {
		parents[id] = structuralBlockMetadata{ParentID: entry.ParentID}
	}
	parentOf := func(parentID *string) string {
		if parentID == nil {
			return ""
		}
		return *parentID
	}
	result := []string{}
	seen := map[string]struct{}{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil:
			parents[operation.Block.ID] = structuralBlockMetadata{ParentID: parentOf(operation.Block.ParentID)}
		case operation.Kind == graphOperationKindMoveBlock && operation.Move != nil:
			if _, exists := parents[operation.Move.BlockID]; exists {
				parents[operation.Move.BlockID] = structuralBlockMetadata{ParentID: parentOf(operation.Move.ParentID)}
			}
		case operation.Kind == graphOperationKindDeleteBlock && operation.Deletion != nil:
			ids, err := collectStructuralSubtree(parents, operation.Deletion.BlockID)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				delete(parents, id)
				if _, duplicate := seen[id]; duplicate {
					continue
				}
				seen[id] = struct{}{}
				if _, kept := writerTook[id]; !kept {
					result = append(result, id)
				}
			}
		}
	}
	return result, nil
}
