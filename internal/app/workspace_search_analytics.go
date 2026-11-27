package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Workspace search analytics records, as Notion's does, what members search
// for in a workspace and whether a search opened a result, and shows the
// workspace owner the queries more than one member searched more than once,
// with their searches, unique searchers and clicks, never who searched.

const (
	maxWorkspaceSearchAnalyticsQueryRunes = 200
	// maxWorkspaceSearchAnalyticsDailyQueries bounds the queries one member
	// adds to a workspace's counts in a day.
	maxWorkspaceSearchAnalyticsDailyQueries = 500
	workspaceSearchAnalyticsLimit           = 200
)

type workspaceAnalyticsSearch struct {
	Query          string `json:"query" db:"query"`
	Searches       int    `json:"searches" db:"searches"`
	UniqueSearches int    `json:"uniqueSearches" db:"unique_searches"`
	Clicks         int    `json:"clicks" db:"clicks"`
}

// recordWorkspaceSearchEvent takes a member's finished search: its query
// and whether it opened a result.
func recordWorkspaceSearchEvent(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := workspaceSearchEventInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceAnalyticsInvalid, "Check the search.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return workspaceAnalyticsFailure(event, err, "search-authorize")
	}
	query := normalizeWorkspaceSearchAnalyticsQuery(input.Query)
	if query != "" {
		if err := recordWorkspaceSearch(event.App, authorization, user.Id, query, input.Clicked, time.Now().UTC()); err != nil {
			return workspaceAnalyticsFailure(event, err, "search-record")
		}
	}
	return event.NoContent(http.StatusNoContent)
}

// normalizeWorkspaceSearchAnalyticsQuery is the form searches are counted
// in: spaces collapsed, letters folded to lower case, and bounded.
func normalizeWorkspaceSearchAnalyticsQuery(value string) string {
	query := foldSearchText(strings.Join(strings.Fields(value), " "))
	if runes := []rune(query); len(runes) > maxWorkspaceSearchAnalyticsQueryRunes {
		query = strings.TrimSpace(string(runes[:maxWorkspaceSearchAnalyticsQueryRunes]))
	}
	return query
}

// recordWorkspaceSearch counts a member's search of a query for the day on
// their clock (page_analytics_clock.go), unless the workspace collects no
// analytics or the member keeps their history out of analytics.
func recordWorkspaceSearch(
	app core.App, authorization workspaceAuthorization, userID, query string, clicked bool, now time.Time,
) error {
	if !pageAnalyticsPolicy(authorization.Workspace).AnalyticsEnabled {
		return nil
	}
	privacy, err := readPageAnalyticsPrivacy(app, authorization.WorkspaceID, userID, nil)
	if err != nil {
		return err
	}
	if !privacy.IdentifiedHistoryEnabled {
		return nil
	}
	day := pageAnalyticsDayOf(now.In(personLocation(app, userID)))
	params := dbx.Params{
		"workspaceId": authorization.WorkspaceID, "day": day, "query": query, "userId": userID,
	}
	var counted, queries int
	if err := app.DB().NewQuery(`
		SELECT
			(SELECT COUNT(*) FROM {{leafen_workspace_search_daily}}
				WHERE [[workspace_id]] = {:workspaceId} AND [[day]] = {:day}
					AND [[query]] = {:query} AND [[user_id]] = {:userId}),
			(SELECT COUNT(*) FROM {{leafen_workspace_search_daily}} INDEXED BY [[idx_leafen_workspace_search_daily_member]]
				WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId} AND [[day]] = {:day})
	`).Bind(params).Row(&counted, &queries); err != nil {
		return err
	}
	if counted == 0 && queries >= maxWorkspaceSearchAnalyticsDailyQueries {
		return nil
	}
	params["clicks"] = 0
	if clicked {
		params["clicks"] = 1
	}
	params["retentionAt"] = now.AddDate(0, 0, maxPageAnalyticsReadDays).Format(pageAnalyticsTimeLayout)
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_search_daily}}
			([[workspace_id]], [[day]], [[query]], [[user_id]], [[searches]], [[clicks]], [[retention_at]])
		VALUES ({:workspaceId}, {:day}, {:query}, {:userId}, 1, {:clicks}, {:retentionAt})
		ON CONFLICT ([[workspace_id]], [[day]], [[query]], [[user_id]]) DO UPDATE
		SET [[searches]] = [[searches]] + 1, [[clicks]] = [[clicks]] + excluded.[[clicks]]
	`).Bind(params).Execute()
	return err
}

// loadWorkspaceAnalyticsSearches reads the window's queries that at least
// two members searched at least twice in all, most searched first.
func loadWorkspaceAnalyticsSearches(
	app core.App, workspaceID string, window workspaceAnalyticsWindow,
) ([]workspaceAnalyticsSearch, bool, error) {
	rows := []workspaceAnalyticsSearch{}
	err := app.DB().NewQuery(`
		SELECT [[query]], SUM([[searches]]) AS [[searches]],
			COUNT(DISTINCT [[user_id]]) AS [[unique_searches]], SUM([[clicks]]) AS [[clicks]]
		FROM {{leafen_workspace_search_daily}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[day]] >= {:fromDay} AND [[day]] <= {:toDay}
		GROUP BY [[query]]
		HAVING COUNT(DISTINCT [[user_id]]) >= 2 AND SUM([[searches]]) >= 2
		ORDER BY [[searches]] DESC, [[query]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "fromDay": window.FromDay, "toDay": window.ToDay,
		"limit": workspaceSearchAnalyticsLimit + 1,
	}).All(&rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > workspaceSearchAnalyticsLimit
	if hasMore {
		rows = rows[:workspaceSearchAnalyticsLimit]
	}
	return rows, hasMore, nil
}
