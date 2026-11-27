package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A callout made a page gives the page its icon, as Notion's does (seen
// 2026-11-11: a callout turned into a page, or moved into a full-page
// database as its row, has the callout's icon there): its emoji, built-in
// icon, workspace emoji or picture, the bulb a callout shows with no icon
// set, and none when its icon was taken away.

// calloutDefaultIcon is the icon a callout shows with none set.
const calloutDefaultIcon = "💡"

// calloutPageIcon is the page icon a callout's content gives; an empty type
// is none.
func calloutPageIcon(content map[string]any) (icon, iconType string) {
	stored, _ := content["icon"].(string)
	stored = strings.TrimSpace(stored)
	kind, _ := content["iconType"].(string)
	switch kind {
	case "none":
		return "", ""
	case "custom_emoji":
		id, _ := content["customEmojiId"].(string)
		if validCustomEmojiID(id) {
			return id, "custom_emoji"
		}
		return "", ""
	case nativeIconType:
		if validNativeIconValue(stored) {
			return stored, nativeIconType
		}
		return "", ""
	case "image", "external":
		// An uploaded picture keeps its file's ID, a linked one its address,
		// as a page's picture icon does.
		if stored != "" && len([]rune(stored)) <= maxPageIconRunes {
			return stored, "image"
		}
		return "", ""
	}
	if stored == "" {
		return calloutDefaultIcon, "emoji"
	}
	if len([]rune(stored)) > maxPageIconRunes {
		return "", ""
	}
	return stored, "emoji"
}

// giveChildPageCalloutIcon sets the icon of the page a callout became, with
// the page's file references when the icon is an uploaded picture.
func giveChildPageCalloutIcon(app core.App, actorID, workspaceID, pageID string, content map[string]any) error {
	icon, iconType := calloutPageIcon(content)
	if iconType == "" {
		return nil
	}
	page, err := authorizePage(app, actorID, pageID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	committed, err := commitPageMetadataRecord(app, page, actorID, func(record *core.Record, _ int64) {
		record.Set("icon", icon)
		record.Set("icon_type", iconType)
	})
	if err != nil || iconType != "image" {
		return err
	}
	return replaceProductFilePageReferences(app, workspaceID, pageID, icon, iconType, committed.Page.GetString("cover"))
}
