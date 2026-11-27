package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func serveNotionFileUploadList(event *core.RequestEvent) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(
		event, "files:read", permissionWorkspaceRead,
	)
	if err != nil || !ok {
		return err
	}
	// A name the list doesn't read is let be, as Notion lets it be; the rest
	// was checked before (refuseNotionRequest).
	query := event.Request.URL.Query()
	status := strings.TrimSpace(query.Get("status"))
	if status != "" && status != "pending" && status != "uploaded" &&
		status != "expired" && status != "failed" {
		return notionPageValidationError(event, "The file upload status filter is invalid.")
	}
	pageSize := 100
	if value := notionQueryDigits(query.Get("page_size")); value != "" {
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil || parsed < 1 || parsed > 100 {
			return notionPageValidationError(event, "The file upload page size is invalid.")
		}
		pageSize = parsed
	}
	var cursor *storedProductFileUpload
	if value := strings.TrimSpace(query.Get("start_cursor")); value != "" {
		loaded, loadErr := loadAuthorizedNotionFileUpload(event.App, workspace.Identity, value)
		if loadErr != nil {
			return notionStartCursorRefused(event, query.Get("start_cursor"))
		}
		cursor = &loaded
	}
	uploads, err := loadNotionFileUploadPage(
		event.App, workspace.Identity, status, cursor, pageSize+1, time.Now().UTC().UnixMilli(),
	)
	if err != nil {
		return notionFileUploadError(event, err)
	}
	// The list reads from the upload its cursor names, which a list of
	// another status doesn't hold (notion_list_cursor.go).
	if cursor != nil && (len(uploads) == 0 || uploads[0].UploadID != cursor.UploadID) {
		return notionStartCursorRefused(event, query.Get("start_cursor"))
	}
	uploads, next := notionListPage(uploads, pageSize)
	results := make([]map[string]any, 0, len(uploads))
	for _, upload := range uploads {
		upload, err = refreshNotionFileUploadAttachment(event.App, upload)
		if err != nil {
			return notionFileUploadError(event, err)
		}
		item, err := notionFileUploadObject(event, workspace.Identity, upload)
		if err != nil {
			return notionFileUploadError(event, err)
		}
		results = append(results, item)
	}
	var nextCursor any
	if next != nil {
		value, err := notionObjectIdentityPublicID(
			event.App, notionObjectIdentityFileUpload, next.WorkspaceID, next.UploadID,
		)
		if err != nil {
			return notionFileUploadError(event, err)
		}
		nextCursor = value
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(200, map[string]any{
		"object": "list", "next_cursor": nextCursor, "has_more": next != nil,
		"results": results, "type": "file_upload", "file_upload": map[string]any{},
	})
}

// loadNotionFileUploadPage reads at most limit of a connection's uploads of
// a status, newest first, from the one cursor names on.
func loadNotionFileUploadPage(
	app core.App,
	identity notionOAuthBearerIdentity,
	status string,
	cursor *storedProductFileUpload,
	limit int,
	now int64,
) ([]storedProductFileUpload, error) {
	query := productFileUploadSelect + `
		WHERE [[actor_id]]={:actorId} AND [[workspace_id]]={:workspaceId}
			AND [[notion_mode]]!=''
	`
	params := dbx.Params{
		"actorId": identity.PrincipalID, "workspaceId": identity.WorkspaceID,
		"now": now, "expiresBefore": now - notionFileUploadLifetime.Milliseconds(),
		"pending": fileUploadStatusPending, "settling": fileUploadStatusSettling,
		"completed": fileUploadStatusCompleted, "cleanupPending": fileUploadStatusCleanupPending,
		"expired": fileUploadStatusExpired, "failed": fileUploadStatusFailed,
		"limit": limit,
	}
	switch status {
	case "pending":
		query += ` AND [[status]] IN ({:pending},{:settling})
			AND [[created_at]]>{:expiresBefore}`
	case "uploaded":
		query += ` AND [[status]]={:completed}
			AND ([[notion_attached_at]]>0 OR [[created_at]]>{:expiresBefore})`
	case "expired":
		query += ` AND ([[status]] IN ({:cleanupPending},{:expired}) OR
			([[status]] IN ({:pending},{:settling},{:completed})
				AND [[notion_attached_at]]=0 AND [[created_at]]<={:expiresBefore}))`
	case "failed":
		query += ` AND [[status]]={:failed}`
	}
	if cursor != nil {
		query += ` AND ([[created_at]]<{:cursorCreated} OR
			([[created_at]]={:cursorCreated} AND [[upload_id]]>={:cursorUploadId}))`
		params["cursorCreated"] = cursor.CreatedAt
		params["cursorUploadId"] = cursor.UploadID
	}
	query += ` ORDER BY [[created_at]] DESC,[[upload_id]] ASC LIMIT {:limit}`
	result := []storedProductFileUpload{}
	err := app.DB().NewQuery(query).Bind(params).All(&result)
	return result, err
}
