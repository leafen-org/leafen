package app

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's file blocks — files, images, PDFs, videos, sounds and embeds
// (managedFileBlockTypesSQL) — a window at a time, and its last top-level
// block, after which a new one goes: what
// an app that shows a page's files as a folder reads (WebDAV), without
// reading every block of a long page through the content graph. Each block
// says when it was made and last changed, so a file's time is its own and
// not its page's, and when the file's sender last changed it, if it said
// (product_file_client_modified.go).

const contentGraphFileBlockWindow = 500

type contentGraphFileBlock struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	ParentID *string         `json:"parentId"`
	Position string          `json:"position"`
	Content  json.RawMessage `json:"content"`
	// CreatedAt and UpdatedAt are RFC 3339 times in UTC.
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	// ModifiedAt is the time the file's sender gave it, RFC 3339 in UTC.
	ModifiedAt string `json:"modifiedAt,omitempty"`
	// Version is a stored file's version, the baseVersion a save of a new
	// file into the block names (PUT /pages/{pageId}/blocks/{blockId}/file),
	// so it needs no other read first (2026-11-26); none for a block that
	// links to a file elsewhere or holds none yet.
	Version string `json:"version,omitempty"`
}

type contentGraphFileBlocksResponse struct {
	Blocks []contentGraphFileBlock `json:"blocks"`
	// Last is the page's last top-level block, in the first window.
	Last       *contentGraphFileBlock `json:"last,omitempty"`
	NextCursor string                 `json:"nextCursor,omitempty"`
}

type storedContentGraphFileBlock struct {
	ID       string `db:"id"`
	Type     string `db:"type"`
	ParentID string `db:"parent_id"`
	Position string `db:"position"`
	Content  string `db:"content"`
	Created  int64  `db:"created"`
	Updated  int64  `db:"updated"`
	Modified int64  `db:"modified"`
}

func (stored storedContentGraphFileBlock) wire() contentGraphFileBlock {
	block := contentGraphFileBlock{ID: stored.ID, Type: stored.Type, Position: stored.Position, Content: json.RawMessage(stored.Content)}
	if stored.ParentID != "" {
		parent := stored.ParentID
		block.ParentID = &parent
	}
	if !json.Valid(block.Content) {
		block.Content = json.RawMessage(`{}`)
	}
	if stored.Created > 0 {
		block.CreatedAt = time.UnixMilli(stored.Created).UTC().Format(time.RFC3339Nano)
	}
	if stored.Updated > 0 {
		block.UpdatedAt = time.UnixMilli(stored.Updated).UTC().Format(time.RFC3339Nano)
	}
	if stored.Modified > 0 {
		block.ModifiedAt = time.UnixMilli(stored.Modified).UTC().Format(time.RFC3339Nano)
	}
	if content, err := parseGraphFileBlockContent(block.Content); err == nil && content.FileID != "" {
		block.Version = mcpFileBaseVersion(content)
	}
	return block
}

// readContentGraphFileBlocks answers GET /pages/{id}/content-graph/file-blocks?cursor=.
func readContentGraphFileBlocks(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	after := struct{ Parent, Position, ID string }{}
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("cursor")); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		parts := strings.Split(string(decoded), "\x00")
		if err != nil || len(parts) != 3 || parts[2] == "" {
			return apiError(event, http.StatusBadRequest, errorCodeGraphCursorInvalid, "Check the content query position.")
		}
		after.Parent, after.Position, after.ID = parts[0], parts[1], parts[2]
	}
	authorization, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead)
	if isGuestMFARequired(err) {
		return apiError(event, http.StatusForbidden, errorCodeGuestMFARequired, "This organization asks its guests to turn on 2-step verification.")
	}
	if isPermissionDenied(err) {
		return writeGraphError(event, graphNotFoundError(), "file-blocks", errorCodeGraphReadFailed, "Couldn't load the content.")
	}
	if err != nil {
		return writeGraphError(event, err, "file-blocks", errorCodeGraphReadFailed, "Couldn't load the content.")
	}
	params := dbx.Params{"owner": authorization.Workspace.OwnerID, "pageId": pageID, "afterParent": after.Parent,
		"afterPosition": after.Position, "afterId": after.ID, "limit": contentGraphFileBlockWindow + 1}
	keyset := ""
	if after.ID != "" {
		keyset = `AND ([[block.parent_id]], [[block.position]], [[block.id]]) > ({:afterParent}, {:afterPosition}, {:afterId})`
	}
	rows := []storedContentGraphFileBlock{}
	if err := event.App.DB().NewQuery(`
		SELECT [[block.id]], [[block.type]], [[block.parent_id]], [[block.position]], [[block.content]], [[block.created]],
			[[block.updated]], COALESCE((SELECT [[file.client_modified]] FROM {{leafen_files}} [[file]]
				WHERE [[file.id]] = json_extract([[block.content]], '$.fileId')), 0) AS [[modified]]
		FROM {{leafen_blocks}} [[block]] INDEXED BY [[idx_leafen_blocks_page_order]]
		WHERE [[block.owner]] = {:owner} AND [[block.page_id]] = {:pageId}
			AND [[block.type]] ` + managedFileBlockTypesSQL + ` ` + keyset + `
		ORDER BY [[block.parent_id]], [[block.position]], [[block.id]] LIMIT {:limit}
	`).Bind(params).All(&rows); err != nil {
		return writeGraphError(event, err, "file-blocks", errorCodeGraphReadFailed, "Couldn't load the content.")
	}
	response := contentGraphFileBlocksResponse{Blocks: []contentGraphFileBlock{}}
	if len(rows) > contentGraphFileBlockWindow {
		rows = rows[:contentGraphFileBlockWindow]
		last := rows[len(rows)-1]
		response.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.ParentID + "\x00" + last.Position + "\x00" + last.ID))
	}
	for _, row := range rows {
		response.Blocks = append(response.Blocks, row.wire())
	}
	if after.ID == "" {
		last := storedContentGraphFileBlock{}
		err := event.App.DB().NewQuery(`
			SELECT [[id]], [[type]], [[parent_id]], [[position]], [[content]], [[created]], [[updated]]
			FROM {{leafen_blocks}} INDEXED BY [[idx_leafen_blocks_page_order]]
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[parent_id]] = ''
			ORDER BY [[position]] DESC, [[id]] DESC LIMIT 1
		`).Bind(params).One(&last)
		if err == nil {
			wire := last.wire()
			response.Last = &wire
		}
	}
	return event.JSON(http.StatusOK, response)
}
