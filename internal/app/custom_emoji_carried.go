package app

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page brought to another workspace — its archive imported there, or the
// page moved there on this server — brings the workspace emoji it uses:
// their mentions, its icons and its reactions keep showing their pictures
// there, as Notion's copy of a page in another workspace does, and that
// workspace's emoji stay as they were (2026-11-11). Each such emoji comes
// as a carried one: it keeps its name and picture for what names it, by its
// ID, and is no emoji of the workspace's own — its list, the picker, a
// :name: and the Notion API's list leave it out, its name takes no one
// else's, and no one renames or deletes it. It goes with the workspace, in
// its archive too, as a carried one.

// A page copy into another workspace keeps, beside its other identities,
// the carried emoji it brings for each emoji of its source
// (page_duplication_custom_emoji.go).
const pageDuplicationCustomEmojisTable = "leafen_page_duplication_custom_emojis"

// customEmojiCarriedNameKey is a carried emoji's name key: its own, which
// no name folds to (a name holds no control character), so it takes no
// name of the workspace's emoji and no :name: finds it.
func customEmojiCarriedNameKey(emojiID string) string {
	return "\x00carried\x00" + emojiID
}

// customEmojiNameKeyFor is the name key an emoji keeps: its name's, or a
// carried one's own.
func customEmojiNameKeyFor(emojiID, name string, carried bool) string {
	if carried {
		return customEmojiCarriedNameKey(emojiID)
	}
	_, key, _ := normalizeCustomEmojiName(name)
	return key
}

// usedCustomEmojiIDs adds to ids the workspace emoji an archived value
// names: a mention's or a callout's, button's or tab's customEmojiId, an
// icon whose kind is a workspace emoji, a template's icon and a reaction.
// It may name more than the value shows, never fewer; what isn't one of the
// workspace's emoji is found to be none.
func usedCustomEmojiIDs(value any, ids map[string]struct{}) {
	add := func(id string) {
		if validCustomEmojiID(id) {
			ids[id] = struct{}{}
		}
	}
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			usedCustomEmojiIDs(child, ids)
		}
	case map[string]any:
		for _, key := range []string{"customEmojiId", "CustomEmojiID"} {
			if id, ok := typed[key].(string); ok {
				add(id)
			}
		}
		if typed["iconType"] == "custom_emoji" {
			if id, ok := typed["icon"].(string); ok {
				add(id)
			}
		}
		if icon, ok := typed["icon"].(string); ok {
			if id, custom := databaseTemplateCustomEmojiID(icon); custom {
				add(id)
			}
		}
		if emoji, ok := typed["emoji"].(string); ok {
			if id, custom := strings.CutPrefix(emoji, commentReactionCustomEmojiPrefix); custom {
				add(id)
			}
		}
		for _, child := range typed {
			usedCustomEmojiIDs(child, ids)
		}
	}
}

// customEmojiMentionKey is the format key a workspace emoji mention's text
// carries, as a document update writes it (a length, then the key).
var customEmojiMentionKey = []byte("\x0dcustomEmojiId")

// usedDocumentCustomEmojiIDs adds the workspace emoji a page's text
// document mentions, read from its update's bytes as
// collaborationMentionTargets reads page mentions.
func usedDocumentCustomEmojiIDs(stateHex string, ids map[string]struct{}) {
	rest, err := hex.DecodeString(stateHex)
	if err != nil {
		return
	}
	for {
		at := bytes.Index(rest, customEmojiMentionKey)
		if at < 0 {
			return
		}
		rest = rest[at+len(customEmojiMentionKey):]
		if len(rest) == 0 || rest[0] >= 0x80 || int(rest[0]) >= len(rest) {
			continue
		}
		id := ""
		if json.Unmarshal(rest[1:1+int(rest[0])], &id) == nil && validCustomEmojiID(id) {
			ids[id] = struct{}{}
		}
	}
}

// loadCarriableCustomEmojis reads the workspace's emoji among ids whose
// pictures are there, by ID: its own and those it carries, none still being
// imported.
func loadCarriableCustomEmojis(app core.App, workspaceID string, ids []string) ([]storedCustomEmoji, error) {
	rows := []storedCustomEmoji{}
	if len(ids) == 0 {
		return rows, nil
	}
	values := make([]any, len(ids))
	for index, id := range ids {
		values[index] = id
	}
	err := app.DB().Select(
		"emoji.id", "emoji.workspace_id", "emoji.name", "emoji.file_id",
		"emoji.created_at", "emoji.updated_at",
	).From(customEmojisTable+" emoji").
		InnerJoin("leafen_files file", dbx.NewExp("file.id=emoji.file_id AND file.workspace=emoji.workspace_id")).
		Where(dbx.HashExp{"emoji.workspace_id": workspaceID, "emoji.id": values, "emoji.workspace_import_job_id": ""}).
		OrderBy("emoji.id").All(&rows)
	return rows, err
}

// workspaceExportCarriedCustomEmojis gathers, for a page's archive, the
// workspace emoji its items use, so the archive carries them.
type workspaceExportCarriedCustomEmojis struct {
	carries bool
	ids     map[string]struct{}
}

func newWorkspaceExportCarriedCustomEmojis(payload workspaceExportJobPayload) *workspaceExportCarriedCustomEmojis {
	return &workspaceExportCarriedCustomEmojis{
		carries: payload.Format == workspaceExportFormatPageNativeArchive, ids: map[string]struct{}{},
	}
}

// use notes the emoji an item uses: in its values, and in its text document.
func (carried *workspaceExportCarriedCustomEmojis) use(item workspaceExportInventoryItem) {
	if !carried.carries {
		return
	}
	if item.Kind == workspaceExportItemYgo {
		usedDocumentCustomEmojiIDs(mapString(item.Value, "stateHex"), carried.ids)
		return
	}
	usedCustomEmojiIDs(item.Value, carried.ids)
}

// write puts the emoji the archive's items use, with their pictures, in the
// archive as carried ones (each came back carried, custom_emoji_carried.go):
// those of the workspace, its own or carried, whose pictures it has.
func (carried *workspaceExportCarriedCustomEmojis) write(
	app core.App, job storedJob, entities *workspaceExportStreamWriter, counts map[string]int,
) error {
	if !carried.carries || len(carried.ids) == 0 {
		return nil
	}
	ids := referenceIDs(carried.ids)
	emojis, err := loadCarriableCustomEmojis(app, job.WorkspaceID, ids)
	if err != nil || len(emojis) == 0 {
		return err
	}
	items := make([]workspaceExportInventoryItem, 0, len(emojis))
	for _, emoji := range emojis {
		items = append(items, workspaceExportInventoryItem{Kind: workspaceExportItemCustomEmoji, Value: map[string]any{
			"id": emoji.ID, "name": emoji.Name, "fileId": emoji.FileID,
			"createdAt": emoji.CreatedAt, "updatedAt": emoji.UpdatedAt, "carried": true,
		}})
	}
	files, _, err := snapshotWorkspaceExportFiles(app, job.WorkspaceID, items)
	if err != nil {
		return err
	}
	if err := storeWorkspaceExportFileSnapshots(app, job.ID, files); err != nil {
		return err
	}
	for _, item := range items {
		// One whose picture is gone or still arriving stays out, as its
		// text then reads it.
		if fileID, _ := item.Value["fileId"].(string); fileID == "" {
			continue
		}
		rewriteWorkspaceExportReferencesV2(item.Value, "")
		counts[item.Kind]++
		if err := entities.writeJSONLine(workspaceExportEntityLine{Kind: item.Kind, Value: item.Value}); err != nil {
			return err
		}
	}
	return nil
}
