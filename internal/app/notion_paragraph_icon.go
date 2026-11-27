package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// notionParagraphIconRefused is what Notion says of an icon on a paragraph
// that isn't a tab's (seen 2026-11-10, made and changed alike).
const notionParagraphIconRefused = "Only a paragraph directly inside a tab block takes an icon."

func notionBlockFieldSpecified(raw json.RawMessage, key string) bool {
	content := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &content) != nil {
		return false
	}
	_, specified := content[key]
	return specified
}

func notionParagraphIconAllowed(app core.App, block storedNotionBlock) (bool, error) {
	if block.ParentID == "" {
		return false, nil
	}
	parent, found, err := loadStoredNotionBlock(app, block.ParentID)
	if err != nil || !found {
		return false, err
	}
	return parent.Type == graphBlockTypeTab && parent.Owner == block.Owner &&
		parent.WorkspaceID == block.WorkspaceID && parent.PageID == block.PageID, nil
}

func clearNotionBlockIcon(content map[string]any) {
	delete(content, "icon")
	delete(content, "iconType")
	delete(content, "customEmojiId")
}

func notionBlockIconStored(content map[string]any) bool {
	for _, key := range []string{"icon", "iconType", "customEmojiId"} {
		if _, present := content[key]; present {
			return true
		}
	}
	return false
}
