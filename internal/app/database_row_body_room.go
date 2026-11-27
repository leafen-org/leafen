package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A database page's body is its page's body: its text lives in the page's
// collaborative document, whose room is the row's ID, and its blocks among
// the page's blocks. A socket to it is admitted by the row page's permission
// and its changes move the row page's body revision on.

// isDatabaseRowPageRoom reports whether a room is a database page's body.
func isDatabaseRowPageRoom(app core.App, room string) (bool, error) {
	// Asked once a read scope: a graph read asked it for editing and for
	// suggesting (2026-10-17).
	return remembered(app, rowPageRoomMemoKey{room}, func() (bool, error) {
		var count int
		err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]]={:room} AND [[data_source_id]]!=''`).
			Bind(dbx.Params{"room": room}).Row(&count)
		return count == 1, err
	})
}

type rowPageRoomMemoKey struct{ room string }

type databaseRowBodyBlockText struct {
	ID      string `db:"id"`
	Content string `db:"content"`
}

// databaseRowBlockTextSpans reads a row block's stored text in any of the
// forms its writers used: API spans, archived spans, or plain text.
func databaseRowBlockTextSpans(content map[string]any) []graphTextSpan {
	for _, key := range []string{"notionRichTextSpans", "rich"} {
		raw, present := content[key]
		if !present {
			continue
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		spans := []graphTextSpan{}
		if json.Unmarshal(encoded, &spans) == nil && len(spans) > 0 {
			return spans
		}
	}
	for _, key := range []string{"text", "plainText"} {
		if text, _ := content[key].(string); text != "" {
			return []graphTextSpan{{Text: text}}
		}
	}
	return nil
}

// graphTextSpansFromYText reads a block's collaborative text back into spans.
func graphTextSpansFromYText(text *crdt.YText) []graphTextSpan {
	return graphTextSpansFromDelta(text.ToDelta())
}

// graphTextSpansFromDelta reads a text's inserts, with their formatting, as
// spans, an empty piece's embed as its span (graph_text_empty_pieces.go).
func graphTextSpansFromDelta(deltas []crdt.Delta) []graphTextSpan {
	spans := []graphTextSpan{}
	for _, delta := range deltas {
		content, ok := delta.Insert.(string)
		empty := graphTextEmptyPieceKind(delta.Insert)
		if delta.Op != crdt.DeltaOpInsert || (!ok || content == "") && empty == "" {
			continue
		}
		attributes := map[string]any(delta.Attributes)
		flag := func(name string) bool { value, _ := attributes[name].(bool); return value }
		value := func(name string) string { text, _ := attributes[name].(string); return text }
		spans = append(spans, graphTextSpan{
			Text: content, Bold: flag("bold"), Italic: flag("italic"), Underline: flag("underline"),
			Strikethrough: flag("strikethrough"), Code: flag("code"), Color: value("color"), Background: value("background"), Link: value("link"),
			CommentID: value("commentId"), Mention: value("mention"), PageID: value("pageId"), BlockID: value("blockId"),
			DatabaseID: value("databaseId"), CustomEmojiID: value("customEmojiId"), Date: value("date"), DateStartUnset: flag("dateStartUnset"),
			DateEnd: value("dateEnd"), DateTimeZone: value("dateTimeZone"), DateReminder: value("dateReminder"),
			DateFormat: value("dateFormat"), DateTimeFormat: value("dateTimeFormat"), UserID: value("userId"), IconURL: value("iconUrl"),
			Provider: value("provider"), Description: value("description"), ImageURL: value("imageUrl"), Equation: value("equation"),
			Template: value("template"), Suggestion: value("suggestion"), SuggestionID: value("suggestionId"),
			SuggestionBefore: value("suggestionBefore"), EmptyPiece: empty,
		})
	}
	return spans
}

// loadRowPageTextDocument reads a database page's text document; a body
// never opened or written has none and reads as empty. While the page's room
// is open its text is the room's, as a read of the page gives it
// (content_graph_words.go), copied: what is stored comes behind the room's
// updates (ygo_persistence_settlement.go), and a template's body copied into
// a row made a moment after the template was written left its last letters
// out (2026-11-16, twice).
func loadRowPageTextDocument(app core.App, rowID string) (*crdt.Doc, error) {
	if room := openPageTextRoom(app, rowID); room != nil {
		doc := crdt.New()
		if err := crdt.ApplyUpdateV1(doc, crdt.EncodeStateAsUpdateV1(room, nil), nil); err != nil {
			return nil, err
		}
		return doc, nil
	}
	doc, _, err := loadYgoDoc(app, rowID)
	return doc, err
}

// rowPageBlockText reads one block's text from its page's document, as
// spans and as plain text, with its suggested edits.
func rowPageBlockText(doc *crdt.Doc, blockID string) ([]graphTextSpan, string) {
	return graphTextSpansAndText(graphTextSpansFromYText(doc.GetText(ygoBlockTextRootPrefix + blockID)))
}

// rowPageBlockTextAsItStands reads one block's text as it stands, suggested
// text kept only when keepSuggested.
func rowPageBlockTextAsItStands(doc *crdt.Doc, blockID string, keepSuggested bool) ([]graphTextSpan, string) {
	spans := graphTextSpansFromYText(doc.GetText(ygoBlockTextRootPrefix + blockID))
	return graphTextSpansAndText(graphTextSpansAsTheyStand(spans, keepSuggested))
}

// graphBlockWordless says whether a block of the type holds no words, so
// that what its page's document keeps under it is none of its text: a
// divider a line was made into by "---" kept the line's last letters there
// until 2026-11-13 (the editor now empties them with the change), and its
// readers leave them out.
func graphBlockWordless(blockType string) bool {
	return blockType == graphBlockTypeDivider
}

func graphTextSpansAndText(spans []graphTextSpan) ([]graphTextSpan, string) {
	plain := strings.Builder{}
	for _, span := range spans {
		plain.WriteString(span.Text)
	}
	return spans, plain.String()
}

// storeDatabaseRowBodyUpdates keeps the socket updates of a database page's
// body and moves its body revision on, with who changed it.
func storeDatabaseRowBodyUpdates(
	ctx context.Context,
	txApp core.App,
	room string,
	updates []attributedYgoUpdate,
) (bool, error) {
	changed := false
	actorID := ""
	var editedAt time.Time
	if len(updates) > 0 {
		if err := ensurePageHistoryBaseline(txApp, room); err != nil {
			return false, err
		}
	}
	for _, update := range updates {
		effective, err := appendYgoUpdateIfChangedContext(ctx, txApp, room, update.update)
		if err != nil {
			return false, err
		}
		if !effective {
			continue
		}
		if err := recordPageHistoryCollaborationUpdate(
			txApp, room, update.actorID, update.update, update.textChanges,
		); err != nil {
			return false, err
		}
		changed = true
		editedAt = laterYgoEditTime(editedAt, update.at)
		if update.actorID != "" {
			actorID = update.actorID
		}
	}
	if !changed {
		return false, nil
	}
	return true, noteDatabaseRowBodyChangeAt(txApp, room, actorID, editedAt)
}

// noteDatabaseRowBodyMarks moves a database page's body revision on after a
// discussion marked its text: no edit, so its last edited time and editor
// stay, as Notion's (seen 2026-11-11).
func noteDatabaseRowBodyMarks(app core.App, rowID string) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_row_pages}} SET [[body_snapshot_revision]]=[[body_snapshot_revision]]+1
		WHERE [[row_id]]={:rowId}
	`).Bind(dbx.Params{"rowId": rowID}).Execute()
	return err
}

// noteDatabaseRowBodyChange moves a database page's body revision on after
// its text changed, with who changed it.
func noteDatabaseRowBodyChange(app core.App, rowID, actorID string) error {
	return noteDatabaseRowBodyChangeAt(app, rowID, actorID, time.Time{})
}

// noteDatabaseRowBodyChangeAt notes the change as made when it came in, now
// where that isn't known, keeping a later edit time the page has.
func noteDatabaseRowBodyChangeAt(app core.App, rowID, actorID string, editedAt time.Time) error {
	if editedAt.IsZero() {
		editedAt = time.Now()
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_row_pages}}
		SET [[body_snapshot_revision]]=[[body_snapshot_revision]]+1, [[updated]]=MAX([[updated]], {:edited})
		WHERE [[row_id]]={:rowId}
	`).Bind(dbx.Params{"rowId": rowID, "edited": editedAt.UTC().UnixMilli()}).Execute(); err != nil {
		return err
	}
	if actorID != "" {
		if _, err := setRowPageRecordEditor(app, rowID, "", actorID); err != nil {
			return err
		}
	}
	return markDatabaseRowSearchDirty(app, rowID)
}

// authorizeDatabaseRowBodyRoom admits a socket to a row's body when the
// person can read the row; it takes edits only from someone who can write the
// row, and none for a row in the trash, or on a page in the trash.
func authorizeDatabaseRowBodyRoom(app core.App, userID, rowID string) (string, bool, error) {
	if fenced, err := blockMovePageFenceActive(app, rowID); err != nil {
		return "", false, err
	} else if fenced {
		return "", false, permissionDenied()
	}
	// A template's page opens as its database's editors edit the template.
	if template, err := isDatabaseTemplatePageID(app, rowID); err != nil {
		return "", false, err
	} else if template {
		authorization, err := authorizeDatabaseTemplateBody(app, userID, rowID, pageAccessRoleEdit)
		readOnly := err != nil
		if readOnly {
			authorization, err = authorizeDatabaseTemplateBody(app, userID, rowID, pageAccessRoleView)
		}
		if err != nil {
			return "", false, err
		}
		// A locked template takes no edits until this person unlocks it, as a database page.
		if !readOnly {
			authority, err := authorizeDatabaseTemplatePage(app, userID, rowID, pageAccessRoleView)
			if err != nil {
				return "", false, err
			}
			if readOnly, err = databaseRowPageLockedFor(app, authority, userID); err != nil {
				return "", false, err
			}
		}
		return authorization.Workspace.WorkspaceID, readOnly, nil
	}
	viewOnly := false
	authority, err := authorizeDatabaseRowPageInTrash(app, userID, rowID, permissionWorkspaceWrite)
	// The row authority answers a refused write as a row it cannot find, so
	// a reader is tried next either way.
	if isPermissionDenied(err) || errors.Is(err, errDatabaseRowPageNotFound) {
		viewOnly = true
		authority, err = authorizeDatabaseRowPageInTrash(app, userID, rowID, permissionWorkspaceRead)
	}
	if err != nil {
		return "", false, err
	}
	readOnly := authority.TrashedAt != 0 || viewOnly
	if !readOnly {
		readOnly, err = pageInTrashPath(app, authority.ContainerPageID)
		if err != nil {
			return "", false, err
		}
	}
	// A locked database page takes no edits until this person unlocks it.
	if !readOnly {
		readOnly, err = databaseRowPageLockedFor(app, authority, userID)
		if err != nil {
			return "", false, err
		}
	}
	return authority.WorkspaceID, readOnly, nil
}

// databaseRowBodyRoomTables hold a room's collaborative documents.
var databaseRowBodyRoomTables = []string{
	"leafen_ygo_updates", "leafen_ygo_structural_updates", "leafen_ygo_structural_quarantine",
	"leafen_ygo_update_quarantine", "leafen_ygo_structural_documents", "leafen_ygo_documents",
}

// deleteDatabaseRowBodyRoom removes a deleted row's body documents with the
// row, as a page's room goes with the page.
func deleteDatabaseRowBodyRoom(app core.App, rowID string) error {
	for _, table := range databaseRowBodyRoomTables {
		if _, err := app.DB().NewQuery(`DELETE FROM {{` + table + `}} WHERE [[room]] = {:room}`).
			Bind(dbx.Params{"room": rowID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
