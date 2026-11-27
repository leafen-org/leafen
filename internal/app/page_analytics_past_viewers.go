package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	// The dimmed faces a page's bar has room for, and how far down who viewed
	// it to look for people who may still edit it.
	pagePastViewersMost   = 4
	pagePastViewersLooked = 24
)

type pagePastViewersRequest struct {
	PageID      string `json:"pageId"`
	RowID       string `json:"rowId"`
	ContainerID string `json:"containerId"`
}

// readPagePastViewers names others who viewed a page and may edit it, newest
// first, for the dimmed faces in its bar, as Notion's (seen 2026-11-05: a face
// goes once its person may only read). Only someone who may edit it asks, and
// someone who may not hears of no one.
func readPagePastViewers(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	var input pagePastViewersRequest
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil ||
		!pageAnalyticsIdentityPattern.MatchString(input.PageID) ||
		(input.RowID != "" && !pageAnalyticsIdentityPattern.MatchString(input.RowID)) ||
		(input.ContainerID != "" && (input.RowID != "" || !pageAnalyticsIdentityPattern.MatchString(input.ContainerID))) {
		return pageAnalyticsInvalidRequest(event)
	}
	viewers := []pageAnalyticsRecentViewer{}
	answer := func() error {
		return event.JSON(http.StatusOK, map[string]any{"viewers": viewers})
	}
	failed := func(err error) error {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodePageAnalyticsFailed,
			"Couldn't load the page analytics.", "page_analytics.read", "query",
			errorCodePageAnalyticsFailed, err,
		)
	}
	authorization, err := authorizePage(event.App, user.Id, input.PageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return answer()
	}
	if err != nil {
		return failed(err)
	}
	// A database row page reads its own viewers through the page that holds
	// it, and only a row that page holds (as page_analytics_read.go does).
	query, targetID := pageAnalyticsRecentViewersSQL, input.PageID
	if input.RowID != "" {
		held, err := rowPageAnalyticsRowsHeld(event.App, authorization.Workspace.WorkspaceID, rowPageAnalyticsObservations{
			IDs: []string{input.RowID}, Pages: map[string]string{input.RowID: input.PageID},
		})
		if err != nil {
			return failed(err)
		}
		if !held {
			return answer()
		}
		query, targetID = rowPageAnalyticsRecentViewersSQL, input.RowID
	}
	// An inline database opened alone reads its own, the same way.
	if input.ContainerID != "" {
		held, err := databaseAnalyticsHeld(event.App, authorization.Workspace.WorkspaceID, input.PageID, input.ContainerID)
		if err != nil {
			return failed(err)
		}
		if !held {
			return answer()
		}
		query, targetID = databaseAnalyticsRecentViewersSQL, input.ContainerID
	}
	display, err := workspaceViewDisplay(event.App, authorization.Workspace.WorkspaceID)
	if err != nil {
		return failed(err)
	}
	if display == viewDisplayHidden {
		return answer()
	}
	rows := []storedPageAnalyticsRecentViewer{}
	if err := event.App.DB().NewQuery(query).Bind(dbx.Params{
		"workspaceId": authorization.Workspace.WorkspaceID, "targetId": targetID, "limit": pagePastViewersLooked,
		"everyone": boolInt(display == viewDisplayShown), "readerId": user.Id,
	}).All(&rows); err != nil {
		return failed(err)
	}
	for _, row := range rows {
		if row.UserID == user.Id {
			continue
		}
		if _, err := authorizePage(event.App, row.UserID, input.PageID, permissionWorkspaceWrite); err != nil {
			continue
		}
		viewers = append(viewers, pageAnalyticsRecentViewer{
			UserID: row.UserID, DisplayName: pastViewerName(row.DisplayName), LastViewedAt: row.LastViewedAt,
		})
		if len(viewers) == pagePastViewersMost {
			break
		}
	}
	return answer()
}

func pastViewerName(name sql.NullString) *string {
	if !name.Valid || strings.TrimSpace(name.String) == "" {
		return nil
	}
	return &name.String
}
