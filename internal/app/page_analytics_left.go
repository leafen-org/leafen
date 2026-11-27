package app

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type pageLeftRequest struct {
	PageID string `json:"pageId"`
	RowID  string `json:"rowId,omitempty"`
	// An inline database opened alone, viewed as itself (database_analytics.go).
	ContainerID string `json:"containerId,omitempty"`
}

// recordPageLeft moves a reader's "last seen" on a page to when they left
// it, so whoever opens the page later sees when that person was last there
// rather than when they opened it, as Notion's past viewers read (seen
// 2026-11-06). It counts no view and keeps a reader who never had a view
// recorded out of the list.
func recordPageLeft(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	var input pageLeftRequest
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil ||
		!pageAnalyticsIdentityPattern.MatchString(input.PageID) ||
		(input.RowID != "" && !pageAnalyticsIdentityPattern.MatchString(input.RowID)) ||
		(input.ContainerID != "" && (input.RowID != "" || !pageAnalyticsIdentityPattern.MatchString(input.ContainerID))) {
		return pageAnalyticsInvalidRequest(event)
	}
	authorization, err := authorizePage(event.App, user.Id, input.PageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return event.NoContent(http.StatusNoContent)
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePageAnalyticsFailed,
			"Couldn't record the page analytics.", "page_analytics.left", "query", errorCodePageAnalyticsFailed, err)
	}
	table, column, target := "leafen_page_analytics_recent_viewers", "page_id", input.PageID
	if input.RowID != "" {
		table, column, target = "leafen_row_page_analytics_recent_viewers", "row_id", input.RowID
	}
	if input.ContainerID != "" {
		table, column, target = "leafen_database_analytics_recent_viewers", "container_id", input.ContainerID
	}
	if _, err := event.App.DB().NewQuery(`
		UPDATE {{` + table + `}} SET [[last_viewed_at]] = {:now}
		WHERE [[workspace_id]] = {:workspaceId} AND [[` + column + `]] = {:targetId} AND [[user_id]] = {:userId}
	`).Bind(dbx.Params{
		"now": pageAnalyticsTimestamp(time.Now()), "workspaceId": authorization.Workspace.WorkspaceID,
		"targetId": target, "userId": user.Id,
	}).Execute(); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePageAnalyticsFailed,
			"Couldn't record the page analytics.", "page_analytics.left", "query", errorCodePageAnalyticsFailed, err)
	}
	return event.NoContent(http.StatusNoContent)
}
