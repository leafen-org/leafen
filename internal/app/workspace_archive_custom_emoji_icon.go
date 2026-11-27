package app

import (
	"database/sql"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's archive carries no workspace emoji: one would join the emoji of
// the workspace it comes back in. A page, a database page, a database, a
// template or a callout whose icon is one shows it where its emoji is (a
// workspace's archive brings its emoji with it), and its picture elsewhere,
// which the archive carries — as Notion's copy of a page in another
// workspace keeps showing its workspace emoji icon without adding the emoji
// there (2026-11-11). An icon whose emoji came with neither is no icon.

// archiveCustomEmojiPicture is a workspace emoji icon's picture in a page's
// archive: the emoji's name and its file, written as an icon's file is.
type archiveCustomEmojiPicture struct {
	Name string `json:"name"`
	Icon string `json:"icon"`
}

// workspaceExportCustomEmojiIconID is the workspace emoji an archived page,
// database page, database or template shows as its icon.
func workspaceExportCustomEmojiIconID(item workspaceExportInventoryItem) string {
	switch item.Kind {
	case workspaceExportItemPage, workspaceExportItemRow, workspaceExportItemDataSource:
		if item.Value["iconType"] == "custom_emoji" {
			id, _ := item.Value["icon"].(string)
			return id
		}
	case workspaceExportItemTemplate:
		icon, _ := item.Value["icon"].(string)
		if id, custom := databaseTemplateCustomEmojiID(icon); custom {
			return id
		}
	}
	return ""
}

// attachWorkspaceExportCustomEmojiPictures gives each archived page,
// database page, database and template whose icon is one of the workspace's
// emoji that emoji's picture (customEmoji), which goes with the archive as
// an icon's file does.
func attachWorkspaceExportCustomEmojiPictures(
	app core.App, workspaceID string, items []workspaceExportInventoryItem,
) error {
	ids := []any{}
	for _, item := range items {
		if id := workspaceExportCustomEmojiIconID(item); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows := []struct {
		ID     string `db:"id"`
		Name   string `db:"name"`
		FileID string `db:"file_id"`
	}{}
	if err := app.DB().Select("id", "name", "file_id").From(customEmojisTable).
		Where(dbx.HashExp{"workspace_id": workspaceID, "id": ids}).All(&rows); err != nil {
		return err
	}
	pictures := map[string]map[string]any{}
	for _, row := range rows {
		pictures[row.ID] = map[string]any{"name": row.Name, "icon": productFilePresentationPrefix + row.FileID}
	}
	for _, item := range items {
		if picture := pictures[workspaceExportCustomEmojiIconID(item)]; picture != nil {
			item.Value["customEmoji"] = map[string]any{"name": picture["name"], "icon": picture["icon"]}
		}
	}
	return nil
}

// workspaceImportArchiveCustomEmojiIcon is an archived workspace emoji icon
// here: the emoji the archive brought, or its picture as an uploaded
// picture, or no icon.
func workspaceImportArchiveCustomEmojiIcon(
	db *sql.DB, emojiID string, picture *archiveCustomEmojiPicture,
) (icon, iconType string, err error) {
	if target, err := workspaceImportArchiveReference(db, emojiID, workspaceExportItemCustomEmoji); err == nil {
		return target, "custom_emoji", nil
	}
	if picture == nil || !strings.HasPrefix(picture.Icon, workspaceImportArchiveFileMarkerPrefix) {
		return "", "none", nil
	}
	fileID, err := workspaceImportArchiveMarkedFileReference(db, picture.Icon)
	if err != nil {
		return "", "", err
	}
	return fileID, "image", nil
}

// workspaceImportArchiveCalloutCustomEmoji makes a callout whose workspace
// emoji the archive didn't bring show the emoji's picture, which its icon
// names and the archive carries, or no icon: it names no emoji here.
func workspaceImportArchiveCalloutCustomEmoji(db *sql.DB, content map[string]any) {
	emojiID, named := content["customEmojiId"].(string)
	if !named || content["iconType"] != "custom_emoji" {
		return
	}
	if _, err := workspaceImportArchiveReference(db, emojiID, workspaceExportItemCustomEmoji); err == nil {
		return
	}
	delete(content, "customEmojiId")
	icon, _ := content["icon"].(string)
	if strings.HasPrefix(icon, workspaceImportArchiveFileMarkerPrefix) {
		content["iconType"] = graphCalloutIconTypeImage
		return
	}
	content["icon"], content["iconType"] = "", "none"
}
