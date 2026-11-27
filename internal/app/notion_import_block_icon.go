package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A Notion callout whose icon is a picture Notion keeps, one uploaded for it
// or a custom emoji's, comes over holding that picture as its uploaded icon,
// as a page's icon does (notion_import_presentation.go): the picture is
// staged as a file the callout owns, copied with the other files, and set on
// the callout when it is made. One that could not be copied leaves the callout
// its default icon, counted with the files left out.

// notionImportHostedIconContentKey marks, until the callout is made, a staged
// callout whose icon is a picture Notion keeps.
const notionImportHostedIconContentKey = "notionImportHostedIcon"

// notionImportBlockIconValue is a callout's icon as Notion gives it, or nil.
func notionImportBlockIconValue(block notionProviderObject) json.RawMessage {
	if block.Type != graphBlockTypeCallout || len(block.Payload) == 0 {
		return nil
	}
	fields := struct {
		Icon json.RawMessage `json:"icon"`
	}{}
	if json.Unmarshal(block.Payload, &fields) != nil {
		return nil
	}
	return fields.Icon
}

// notionImportHostedBlockIcon reports a callout icon that is a picture Notion keeps.
func notionImportHostedBlockIcon(blockType string, raw json.RawMessage) bool {
	if blockType != graphBlockTypeCallout {
		return false
	}
	rawURL, _ := notionImportPresentationFileURL(raw)
	return rawURL != ""
}

// notionImportBlockIconFileSources stages a callout's hosted icon picture.
func notionImportBlockIconFileSources(app core.App, block notionProviderObject) ([]stagedNotionSource, []string, error) {
	rawURL, name := notionImportPresentationFileURL(notionImportBlockIconValue(block))
	if rawURL == "" || strings.TrimSpace(block.ID) == "" {
		return nil, nil, nil
	}
	key, err := notionImportEncryptionKey(app)
	if err != nil {
		return nil, nil, err
	}
	cipher, err := security.Encrypt([]byte(rawURL), key)
	if err != nil {
		return nil, nil, err
	}
	sourceID := block.ID + notionPresentationBlockIcon
	return []stagedNotionSource{{
		SourceID: sourceID, Kind: graphBlockTypeFile, ParentSourceID: block.ID, ParentKind: "block",
		Title: name, Text: notionFileSourcePrefix + `{"caption":""}`, FileURLCipher: cipher,
	}}, []string{sourceID}, nil
}

// applyNotionImportBlockIcon sets a new callout's icon to the picture copied
// for it, when one was.
func applyNotionImportBlockIcon(app core.App, job storedJob, sourceID string, content map[string]any) error {
	if _, hosted := content[notionImportHostedIconContentKey]; !hosted {
		return nil
	}
	delete(content, notionImportHostedIconContentKey)
	fileID, err := notionImportCopiedPresentationFile(app, job.ID, job.WorkspaceID, sourceID+notionPresentationBlockIcon)
	if err != nil || fileID == "" {
		return err
	}
	content["icon"], content["iconType"] = fileID, graphCalloutIconTypeImage
	return nil
}

// keepNotionImportBlockIcon keeps, when a migration run again rewrites a
// callout, the picture an earlier run copied for its icon: a run again does
// not copy a block's files anew (notionImportFileMadeBefore).
func keepNotionImportBlockIcon(stored string, content map[string]any) {
	if _, hosted := content[notionImportHostedIconContentKey]; !hosted {
		return
	}
	delete(content, notionImportHostedIconContentKey)
	icon := struct {
		Icon     string `json:"icon"`
		IconType string `json:"iconType"`
	}{}
	if json.Unmarshal([]byte(stored), &icon) == nil && icon.IconType == graphCalloutIconTypeImage && icon.Icon != "" {
		content["icon"], content["iconType"] = icon.Icon, graphCalloutIconTypeImage
	}
}

// recoveredNotionBlockIconOperation sets a recovered picture as a callout's icon.
func recoveredNotionBlockIconOperation(app core.App, blockID string, record *core.Record) (graphOperation, error) {
	stored := struct {
		ParentID string `db:"parent_id"`
		Position string `db:"position"`
		Type     string `db:"type"`
		Content  string `db:"content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[parent_id]], [[position]], [[type]], [[content]] FROM {{leafen_blocks}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": blockID}).One(&stored); err != nil {
		return graphOperation{}, err
	}
	if stored.Type != graphBlockTypeCallout {
		return graphOperation{}, errNotionImportOwnedFileOwnerGone
	}
	content := map[string]any{}
	if stored.Content != "" && json.Unmarshal([]byte(stored.Content), &content) != nil {
		content = map[string]any{}
	}
	delete(content, "customEmojiId")
	content["icon"], content["iconType"] = record.Id, graphCalloutIconTypeImage
	encoded, err := json.Marshal(content)
	if err != nil {
		return graphOperation{}, err
	}
	block := &graphBlock{ID: blockID, Position: stored.Position, Type: stored.Type, Content: encoded}
	if stored.ParentID != "" {
		parentID := stored.ParentID
		block.ParentID = &parentID
	}
	return graphOperation{Kind: graphOperationKindUpsertBlock, Block: block}, nil
}
