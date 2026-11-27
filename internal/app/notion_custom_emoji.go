package app

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const notionAPICustomEmojisPath = notionAPIRoot + "/custom_emojis"

func loadNotionCustomEmojiMention(
	app core.App,
	principalID, workspaceID, emojiID string,
) (storedCustomEmoji, bool, error) {
	if !validCustomEmojiID(emojiID) {
		return storedCustomEmoji{}, false, nil
	}
	if _, err := authorizeWorkspace(app, principalID, workspaceID, permissionWorkspaceRead); err != nil {
		if isPermissionDenied(err) {
			return storedCustomEmoji{}, false, nil
		}
		return storedCustomEmoji{}, false, err
	}
	rows := []storedCustomEmoji{}
	err := app.DB().NewQuery(`
		SELECT [[emoji]].[[id]],[[emoji]].[[workspace_id]],[[emoji]].[[name]],
			[[emoji]].[[name_key]],[[emoji]].[[file_id]],[[emoji]].[[created_by]],
			[[emoji]].[[revision]],[[emoji]].[[created_at]],[[emoji]].[[updated_at]],
			[[file]].[[content_type]]
		FROM {{leafen_custom_emojis}} AS [[emoji]]
		JOIN {{leafen_files}} AS [[file]] ON [[file]].[[id]]=[[emoji]].[[file_id]]
			AND [[file]].[[workspace]]=[[emoji]].[[workspace_id]]
			AND [[file]].[[authority_state]]={:authority} AND [[file]].[[created_by]]!=''
		WHERE [[emoji]].[[workspace_id]]={:workspaceId} AND [[emoji]].[[id]]={:emojiId}
			AND [[emoji]].[[workspace_import_job_id]]=''
			AND NOT EXISTS (SELECT 1 FROM {{leafen_file_delete_receipts}} AS [[deletion]]
				WHERE [[deletion]].[[file_id]]=[[emoji]].[[file_id]]
					AND [[deletion]].[[status]]={:deletePending})
			AND NOT EXISTS (SELECT 1 FROM {{leafen_file_uploads}} AS [[upload]]
				WHERE [[upload]].[[file_id]]=[[emoji]].[[file_id]]
					AND [[upload]].[[status]]!={:uploadCompleted})
		LIMIT 2
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "emojiId": emojiID,
		"authority": fileAuthorityWorkspace, "deletePending": fileDeleteStatusPending,
		"uploadCompleted": fileUploadStatusCompleted,
	}).All(&rows)
	if err != nil || len(rows) != 1 {
		return storedCustomEmoji{}, false, err
	}
	return rows[0], true, nil
}

func serveNotionCustomEmojiCompatibility(event *core.RequestEvent) (bool, error) {
	if event.Request.URL.Path != notionAPICustomEmojisPath {
		return false, nil
	}
	if event.Request.Method != http.MethodGet {
		return true, notionCompatibilityError(
			event, http.StatusBadRequest, notionErrorInvalidRequestURL, notionInvalidRequestURLMessage,
		)
	}
	return true, serveNotionCustomEmojiList(event)
}

func serveNotionCustomEmojiList(event *core.RequestEvent) error {
	authorization, authorized, err := authorizeNotionWorkspaceOperation(
		event, "workspace:read", permissionWorkspaceRead,
	)
	if err != nil || !authorized {
		return err
	}
	pageSize, err := customEmojiPageSize(notionQueryDigits(event.Request.URL.Query().Get("page_size")))
	if err != nil {
		return notionCompatibilityError(
			event, http.StatusBadRequest, notionErrorValidation,
			"page_size must be between 1 and 100.",
		)
	}
	cursor := strings.TrimSpace(event.Request.URL.Query().Get("start_cursor"))
	if cursor != "" {
		parsed, err := uuid.Parse(cursor)
		if err != nil || parsed.String() != strings.ToLower(cursor) {
			return notionStartCursorRefused(event, event.Request.URL.Query().Get("start_cursor"))
		}
		cursor = parsed.String()
	}
	name := strings.TrimSpace(event.Request.URL.Query().Get("name"))
	if name != "" {
		if name, _, err = normalizeCustomEmojiName(name); err != nil {
			return notionCompatibilityError(
				event, http.StatusBadRequest, notionErrorValidation, "name is invalid.",
			)
		}
	}
	rows, err := loadCustomEmojiPage(
		event.Request.Context(), event.App, authorization.Identity.PrincipalID, authorization.Identity.WorkspaceID,
		cursor, name, true, pageSize+1,
	)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	if organizationID := authorization.Workspace.Organization.OrganizationID; organizationID != "" {
		if err := enforceOrganizationDLPOutbound(
			event.App, organizationID, dlpOutboundFileDownloads,
		); err != nil {
			return notionCompatibilityError(
				event, http.StatusForbidden, notionErrorRestrictedResource,
				notionInsufficientPermissions,
			)
		}
	}
	secret, err := fileDownloadCapabilitySecret(event.App)
	if err != nil {
		return notionCompatibilityError(
			event, http.StatusServiceUnavailable, "service_unavailable", "The request failed on the server.",
		)
	}
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	// From the emoji a cursor names, which the list holds (notion_list_cursor.go).
	if cursor != "" && (len(rows) == 0 || rows[0].ID != cursor) {
		return notionStartCursorRefused(event, event.Request.URL.Query().Get("start_cursor"))
	}
	rows, next := notionListPage(rows, pageSize)
	results := make([]map[string]any, 0, len(rows))
	now := time.Now().UTC()
	for _, row := range rows {
		token, _, err := sealFileDownloadCapabilityToken(
			secret, authorization.Identity.PrincipalID, row.FileID, now,
		)
		if err != nil {
			return notionCompatibilityError(
				event, http.StatusServiceUnavailable, "service_unavailable", "The request failed on the server.",
			)
		}
		path := strings.Replace(fileTransferDownloadCapabilityPath, "{token}", url.PathEscape(token), 1)
		results = append(results, map[string]any{
			"id": row.ID, "name": row.Name, "url": origin + apiRoot + path,
		})
	}
	var nextCursor *string
	if next != nil {
		nextCursor = &next.ID
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{
		"object": "list", "type": "custom_emoji", "results": results,
		"has_more": next != nil, "next_cursor": nextCursor,
	})
}
