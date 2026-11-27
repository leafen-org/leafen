package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A deleted workspace emoji leaves its last name behind, as Notion's does
// (seen 2026-11-07: text that used an emoji renamed and then deleted shows
// ":〈its last name〉: (삭제됨)" after a reload and to every reader): text
// keeps the name it had when the emoji went into it, so the server keeps
// the name it last had, by its ID, for as long as its workspace stays. A
// reader asks for it by reading the emoji with ?include=deleted.

// recordCustomEmojiTombstone keeps a deleted emoji's last name, in the
// transaction that deletes it.
func recordCustomEmojiTombstone(app core.App, emoji storedCustomEmoji) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_custom_emoji_tombstones}} ([[id]],[[workspace_id]],[[name]],[[deleted_at]])
		VALUES ({:id},{:workspaceId},{:name},{:deletedAt})
		ON CONFLICT ([[id]]) DO UPDATE SET [[name]]=excluded.[[name]], [[deleted_at]]=excluded.[[deleted_at]]
	`).Bind(dbx.Params{
		"id": emoji.ID, "workspaceId": emoji.WorkspaceID, "name": emoji.Name,
		"deletedAt": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

// deletedCustomEmojiResponse is a deleted emoji as a reader of its
// workspace sees it: its last name and that it was deleted. A workspace
// given (the workspace's own read) must be the emoji's; false when there is
// no such deleted emoji or the reader may not read its workspace.
func deletedCustomEmojiResponse(app core.App, principalID, workspaceID, emojiID string) (customEmojiResponse, bool, error) {
	row := struct {
		WorkspaceID string `db:"workspace_id"`
		Name        string `db:"name"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]],[[name]] FROM {{leafen_custom_emoji_tombstones}} WHERE [[id]]={:id} LIMIT 1
	`).Bind(dbx.Params{"id": emojiID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) || err == nil && workspaceID != "" && row.WorkspaceID != workspaceID {
		return customEmojiResponse{}, false, nil
	}
	if err != nil {
		return customEmojiResponse{}, false, err
	}
	if _, err := authorizeWorkspace(app, principalID, row.WorkspaceID, permissionWorkspaceRead); err != nil {
		if isPermissionDenied(err) {
			return customEmojiResponse{}, false, nil
		}
		return customEmojiResponse{}, false, err
	}
	return customEmojiResponse{ID: emojiID, WorkspaceID: row.WorkspaceID, Name: row.Name, Deleted: true}, true, nil
}

// customEmojiReadIncludesDeleted is a read that takes a deleted emoji's
// last name over "not found".
func customEmojiReadIncludesDeleted(event *core.RequestEvent) bool {
	return event.Request.URL.Query().Get("include") == "deleted"
}
