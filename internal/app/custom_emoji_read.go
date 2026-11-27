package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func getCustomEmoji(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	emojiID := strings.TrimSpace(event.Request.PathValue("emojiId"))
	if !validCustomEmojiID(emojiID) {
		return customEmojiError(event, errCustomEmojiIDRefused, "read-validate")
	}
	emoji, accessible, err := loadNotionCustomEmojiMention(
		event.App, user.Id, workspaceID, emojiID,
	)
	if (errors.Is(err, errCustomEmojiNotFound) || err == nil && !accessible) && customEmojiReadIncludesDeleted(event) {
		return readDeletedCustomEmoji(event, user.Id, workspaceID, emojiID)
	}
	if err != nil {
		return customEmojiError(event, err, "read")
	}
	if !accessible {
		return customEmojiError(event, errCustomEmojiNotFound, "read")
	}
	return event.JSON(http.StatusOK, nativeCustomEmojiResponse(emoji, false))
}

// readDeletedCustomEmoji answers a read that takes a deleted emoji's last
// name, or "not found" as before.
func readDeletedCustomEmoji(event *core.RequestEvent, principalID, workspaceID, emojiID string) error {
	deleted, found, err := deletedCustomEmojiResponse(event.App, principalID, workspaceID, emojiID)
	if err != nil {
		return customEmojiError(event, err, "read")
	}
	if !found {
		return customEmojiError(event, errCustomEmojiNotFound, "read")
	}
	return event.JSON(http.StatusOK, deleted)
}

// getCustomEmojiByID reads an emoji by its id alone, for text that names an
// emoji without its workspace (a comment's, a database cell's): the reader
// sees it when they may read its workspace, and an emoji they may not read
// is as missing as one that isn't there.
func getCustomEmojiByID(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	emojiID := strings.TrimSpace(event.Request.PathValue("emojiId"))
	if !validCustomEmojiID(emojiID) {
		return customEmojiError(event, errCustomEmojiIDRefused, "read-validate")
	}
	var workspaceID string
	err := event.App.DB().Select("workspace_id").From(customEmojisTable).
		Where(dbx.HashExp{"id": emojiID, "workspace_import_job_id": ""}).Limit(1).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		if customEmojiReadIncludesDeleted(event) {
			return readDeletedCustomEmoji(event, user.Id, "", emojiID)
		}
		return customEmojiError(event, errCustomEmojiNotFound, "read")
	}
	if err != nil {
		return customEmojiError(event, err, "read")
	}
	emoji, accessible, err := loadNotionCustomEmojiMention(event.App, user.Id, workspaceID, emojiID)
	if err != nil {
		return customEmojiError(event, err, "read")
	}
	if !accessible {
		return customEmojiError(event, errCustomEmojiNotFound, "read")
	}
	return event.JSON(http.StatusOK, nativeCustomEmojiResponse(emoji, false))
}

const errorCodeCustomEmojiForbidden = "LEAFEN_CUSTOM_EMOJI_FORBIDDEN"

var errCustomEmojiForbidden = errors.New(errorCodeCustomEmojiForbidden)

// authorizeCustomEmojiChange lets whoever added a custom emoji, or the
// workspace's owner, rename or delete it, as Notion's settings do ("Workspace
// owners can edit or delete any emoji").
func authorizeCustomEmojiChange(authorization workspaceAuthorization, emoji storedCustomEmoji) error {
	if emoji.CreatedBy == authorization.PrincipalID || authorization.Role == workspaceRoleOwner {
		return nil
	}
	return errCustomEmojiForbidden
}

// loadCustomEmojiByID reads one of the workspace's own emoji, which may be
// renamed or deleted: a carried one (custom_emoji_carried.go) is none.
func loadCustomEmojiByID(app core.App, workspaceID, emojiID string) (storedCustomEmoji, error) {
	row := storedCustomEmoji{}
	err := app.DB().NewQuery(`
		SELECT [[emoji]].[[id]],[[emoji]].[[workspace_id]],[[emoji]].[[name]],
			[[emoji]].[[name_key]],[[emoji]].[[file_id]],[[emoji]].[[created_by]],
			[[emoji]].[[revision]],[[emoji]].[[created_at]],[[emoji]].[[updated_at]],
			[[file]].[[content_type]]
		FROM {{leafen_custom_emojis}} AS [[emoji]]
		JOIN {{leafen_files}} AS [[file]] ON [[file]].[[id]]=[[emoji]].[[file_id]]
		WHERE [[emoji]].[[workspace_id]]={:workspaceId} AND [[emoji]].[[id]]={:emojiId}
			AND [[emoji]].[[workspace_import_job_id]]='' AND [[emoji]].[[carried]]=0
		LIMIT 1
	`).Bind(dbx.Params{"workspaceId": workspaceID, "emojiId": emojiID}).One(&row)
	return row, err
}

type customEmojiListResponse struct {
	Items      []customEmojiResponse `json:"items"`
	NextCursor *string               `json:"nextCursor"`
	HasMore    bool                  `json:"hasMore"`
	// The reader may add emoji: a workspace may keep that to its owners.
	CanAdd bool `json:"canAdd"`
}

func listCustomEmojis(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	pageSize, err := customEmojiPageSize(event.Request.URL.Query().Get("limit"))
	if err != nil {
		return customEmojiError(event, err, "list-validate")
	}
	cursor := strings.TrimSpace(event.Request.URL.Query().Get("cursor"))
	if cursor != "" {
		if parsed, err := uuid.Parse(cursor); err != nil || parsed.String() != strings.ToLower(cursor) {
			return customEmojiError(event, customEmojiRefused("cursor isn't one this list gave: leave it out to read from the start"), "list-validate")
		}
	}
	name := strings.TrimSpace(event.Request.URL.Query().Get("name"))
	if name != "" {
		if name, _, err = normalizeCustomEmojiName(name); err != nil {
			return customEmojiError(event, err, "list-validate")
		}
	}

	rows, err := loadCustomEmojiPage(
		event.Request.Context(), event.App, user.Id, workspaceID, cursor, name, false, pageSize+1,
	)
	if err != nil {
		return customEmojiError(event, err, "list")
	}
	hasMore := len(rows) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if err != nil {
		return customEmojiError(event, err, "list")
	}
	// Whether the reader may add emoji, as the workspace's policy says.
	added := requireWorkspacePolicy(event.App, workspaceID, user.Id, authorization.Role, customEmojiLimit, errCustomEmojiForbidden)
	if added != nil && !errors.Is(added, errCustomEmojiForbidden) {
		return customEmojiError(event, added, "list")
	}
	items := make([]customEmojiResponse, 0, len(rows))
	for _, row := range rows {
		item := nativeCustomEmojiResponse(row, false)
		item.CanChange = authorizeCustomEmojiChange(authorization, row) == nil
		items = append(items, item)
	}
	var nextCursor *string
	if hasMore {
		cursor := rows[len(rows)-1].ID
		nextCursor = &cursor
	}
	return event.JSON(http.StatusOK, customEmojiListResponse{
		Items: items, NextCursor: nextCursor, HasMore: hasMore, CanAdd: added == nil,
	})
}
