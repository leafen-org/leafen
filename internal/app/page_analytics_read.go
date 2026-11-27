package app

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedPageAnalyticsTotals struct {
	TotalViews           int `db:"total_views"`
	UniqueMemberViews    int `db:"unique_member_views"`
	UniqueAnonymousViews int `db:"unique_anonymous_views"`
}

type storedPageAnalyticsRecentViewer struct {
	UserID       string         `db:"user_id"`
	DisplayName  sql.NullString `db:"display_name"`
	LastViewedAt string         `db:"last_viewed_at"`
}

const (
	pageAnalyticsDailyTotalsSQL = `
		SELECT COALESCE(SUM([[total_views]]), 0) AS [[total_views]],
			COALESCE(SUM([[unique_member_views]]), 0) AS [[unique_member_views]],
			COALESCE(SUM([[unique_anonymous_views]]), 0) AS [[unique_anonymous_views]]
		FROM {{leafen_page_analytics_daily}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:targetId} AND [[day]] >= {:startDay}`
	// A member who turned "내 조회 기록 표시" off is left out of who viewed,
	// earlier views too, and comes back on turning it on, as Notion's (seen
	// 2026-10-27); an organization that shows everyone shows them anyway, and
	// they go on seeing their own line, as in Notion (seen 2026-11-05).
	pageAnalyticsViewerShownSQL = `
		AND ({:everyone} = 1 OR [[recent]].[[user_id]] = {:readerId} OR NOT EXISTS (
			SELECT 1 FROM {{leafen_page_analytics_member_preferences}} [[preference]]
			WHERE [[preference]].[[workspace_id]] = [[recent]].[[workspace_id]]
				AND [[preference]].[[user_id]] = [[recent]].[[user_id]]
				AND [[preference]].[[identified_history_enabled]] = 0))`
	pageAnalyticsRecentViewersSQL = `
		SELECT [[recent]].[[user_id]], [[user]].[[name]] AS [[display_name]], [[recent]].[[last_viewed_at]]
		FROM {{leafen_page_analytics_recent_viewers}} [[recent]]
		INDEXED BY [[idx_leafen_page_analytics_recent_page_viewed]]
		LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[recent]].[[user_id]]
		WHERE [[recent]].[[workspace_id]] = {:workspaceId} AND [[recent]].[[page_id]] = {:targetId}` + pageAnalyticsViewerShownSQL + `
		ORDER BY [[recent]].[[last_viewed_at]] DESC, [[recent]].[[user_id]] ASC
		LIMIT {:limit}`
	rowPageAnalyticsDailyTotalsSQL = `
		SELECT COALESCE(SUM([[total_views]]), 0) AS [[total_views]],
			COALESCE(SUM([[unique_member_views]]), 0) AS [[unique_member_views]],
			0 AS [[unique_anonymous_views]]
		FROM {{leafen_row_page_analytics_daily}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[row_id]] = {:targetId} AND [[day]] >= {:startDay}`
	// A page's views by day, the days it had any (as Notion's getPageAnalytics).
	pageAnalyticsDaysSQL = `
		SELECT [[day]], [[total_views]], [[unique_member_views]] + [[unique_anonymous_views]] AS [[unique_views]]
		FROM {{leafen_page_analytics_daily}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:targetId} AND [[day]] >= {:startDay}
		ORDER BY [[day]]`
	rowPageAnalyticsDaysSQL = `
		SELECT [[day]], [[total_views]], [[unique_member_views]] AS [[unique_views]]
		FROM {{leafen_row_page_analytics_daily}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[row_id]] = {:targetId} AND [[day]] >= {:startDay}
		ORDER BY [[day]]`
	rowPageAnalyticsRecentViewersSQL = `
		SELECT [[recent]].[[user_id]], [[user]].[[name]] AS [[display_name]], [[recent]].[[last_viewed_at]]
		FROM {{leafen_row_page_analytics_recent_viewers}} [[recent]]
		INDEXED BY [[idx_leafen_row_page_analytics_recent_row_viewed]]
		LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[recent]].[[user_id]]
		WHERE [[recent]].[[workspace_id]] = {:workspaceId} AND [[recent]].[[row_id]] = {:targetId}` + pageAnalyticsViewerShownSQL + `
		ORDER BY [[recent]].[[last_viewed_at]] DESC, [[recent]].[[user_id]] ASC
		LIMIT {:limit}`
)

func readPageAnalytics(
	event *core.RequestEvent,
	user *core.Record,
	input pageAnalyticsReadRequest,
) error {
	input.PageID = strings.TrimSpace(input.PageID)
	input.RowID = strings.TrimSpace(input.RowID)
	input.ContainerID = strings.TrimSpace(input.ContainerID)
	rangeDays := defaultPageAnalyticsReadDays
	if input.RangeDays != nil {
		rangeDays = *input.RangeDays
	}
	if input.Action != pageAnalyticsActionGet ||
		!pageAnalyticsIdentityPattern.MatchString(input.PageID) ||
		(input.RowID != "" && !pageAnalyticsIdentityPattern.MatchString(input.RowID)) ||
		(input.ContainerID != "" && (input.RowID != "" || !pageAnalyticsIdentityPattern.MatchString(input.ContainerID))) ||
		rangeDays < 1 || rangeDays > maxPageAnalyticsReadDays {
		return pageAnalyticsInvalidRequest(event)
	}

	response := pageAnalyticsReadResponse{}
	notFound := false
	err := runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		authorization, err := authorizePage(
			txApp, user.Id, input.PageID, permissionWorkspaceWrite,
		)
		if isPermissionDenied(err) {
			notFound = true
			return nil
		}
		if err != nil {
			return err
		}
		// A guest reads none of the workspace's analytics (page_analytics_readers.go).
		if member, err := pageAnalyticsReader(txApp, user.Id, authorization.Workspace.WorkspaceID); err != nil {
			return err
		} else if !member {
			notFound = true
			return nil
		}
		// A database row page reads its own views, through the page that
		// holds it.
		dailyQuery, recentQuery, daysQuery, targetID := pageAnalyticsDailyTotalsSQL, pageAnalyticsRecentViewersSQL, pageAnalyticsDaysSQL, input.PageID
		if input.RowID != "" {
			held, err := rowPageAnalyticsRowsHeld(txApp, authorization.Workspace.WorkspaceID, rowPageAnalyticsObservations{
				IDs: []string{input.RowID}, Pages: map[string]string{input.RowID: input.PageID},
			})
			if err != nil {
				return err
			}
			if !held {
				notFound = true
				return nil
			}
			dailyQuery, recentQuery, daysQuery, targetID = rowPageAnalyticsDailyTotalsSQL, rowPageAnalyticsRecentViewersSQL, rowPageAnalyticsDaysSQL, input.RowID
		}
		// An inline database opened alone reads its own, through the page
		// that holds it (database_analytics.go).
		if input.ContainerID != "" {
			held, err := databaseAnalyticsHeld(txApp, authorization.Workspace.WorkspaceID, input.PageID, input.ContainerID)
			if err != nil {
				return err
			}
			if !held {
				notFound = true
				return nil
			}
			dailyQuery, recentQuery, daysQuery, targetID = databaseAnalyticsDailyTotalsSQL, databaseAnalyticsRecentViewersSQL, databaseAnalyticsDaysSQL, input.ContainerID
		}
		// The range ends on the reader's today, on their clock, as the views
		// fall on their viewers' days (page_analytics_clock.go).
		today := pageAnalyticsToday(txApp, user.Id, time.Now())
		startDay := pageAnalyticsDayOf(today.AddDate(0, 0, -(rangeDays - 1)))
		totals := storedPageAnalyticsTotals{}
		if err := txApp.DB().NewQuery(dailyQuery).Bind(dbx.Params{
			"workspaceId": authorization.Workspace.WorkspaceID,
			"targetId":    targetID, "startDay": startDay,
		}).One(&totals); err != nil {
			return err
		}
		display, err := workspaceViewDisplay(txApp, authorization.Workspace.WorkspaceID)
		if err != nil {
			return err
		}
		recentRows := []storedPageAnalyticsRecentViewer{}
		if err := txApp.DB().NewQuery(recentQuery).Bind(dbx.Params{
			"workspaceId": authorization.Workspace.WorkspaceID,
			"targetId":    targetID, "limit": maxPageAnalyticsRecentViewers + 1,
			"everyone": boolInt(display == viewDisplayShown), "readerId": user.Id,
		}).All(&recentRows); err != nil {
			return err
		}
		hasMore := len(recentRows) > maxPageAnalyticsRecentViewers
		if hasMore {
			recentRows = recentRows[:maxPageAnalyticsRecentViewers]
		}
		// Where the organization names no one, the views are only counted.
		if display == viewDisplayHidden {
			recentRows, hasMore = nil, false
		}
		recent := make([]pageAnalyticsRecentViewer, 0, len(recentRows))
		for _, row := range recentRows {
			var displayName *string
			if row.DisplayName.Valid && strings.TrimSpace(row.DisplayName.String) != "" {
				value := row.DisplayName.String
				displayName = &value
			}
			recent = append(recent, pageAnalyticsRecentViewer{
				UserID: row.UserID, DisplayName: displayName, LastViewedAt: row.LastViewedAt,
			})
		}
		days := []pageAnalyticsDay{}
		if err := txApp.DB().NewQuery(daysQuery).Bind(dbx.Params{
			"workspaceId": authorization.Workspace.WorkspaceID,
			"targetId":    targetID, "startDay": startDay,
		}).All(&days); err != nil {
			return err
		}
		var creator *pageAnalyticsEditor
		var editors []pageAnalyticsEditor
		if input.ContainerID != "" {
			creator, editors, err = readDatabaseAnalyticsEditors(txApp, authorization.Workspace.WorkspaceID, targetID)
		} else {
			creator, editors, err = readPageAnalyticsEditors(txApp, authorization.Workspace.WorkspaceID, targetID, input.RowID == "")
		}
		if err != nil {
			return err
		}
		response = pageAnalyticsReadResponse{
			PageID: input.PageID, RangeDays: rangeDays, Today: pageAnalyticsDayOf(today),
			TotalViews:           totals.TotalViews,
			UniqueViews:          totals.UniqueMemberViews + totals.UniqueAnonymousViews,
			UniqueMemberViews:    totals.UniqueMemberViews,
			UniqueAnonymousViews: totals.UniqueAnonymousViews,
			RecentViewers:        recent, RecentHasMore: hasMore,
			Days: days, Creator: creator, RecentEditors: editors, ViewersHidden: display == viewDisplayHidden,
		}
		// The periods offered go as far as the organization keeps.
		retention, err := workspaceActivityRetention(txApp, authorization.Workspace.WorkspaceID)
		if err != nil {
			return err
		}
		response.RetentionDays = retention.AnalyticsDays
		return nil
	})
	if notFound {
		return apiError(event, http.StatusNotFound, errorCodePageAnalyticsNotFound, "The page to analyze was not found.")
	}
	if err != nil {
		if mapped, ok := persistentWriteStorageAPIError(
			event, err, persistentPageAnalytics, "read",
		); ok {
			return mapped
		}
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodePageAnalyticsFailed,
			"Couldn't load the page analytics.", "page_analytics.read", "query",
			errorCodePageAnalyticsFailed, err,
		)
	}
	return event.JSON(http.StatusOK, response)
}
