package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A member's analytics batch may observe database row pages as well as
// pages: an observation with a row names the row and the page that holds
// it. Reading a row needs its page, so the batch authorizes the holding
// pages; a row counts only its own views, never its page's. A row is
// identified in history when its page would be and the person hasn't
// declined it on the row itself, and its view reaches the legal hold log
// as a view of that page.

type rowPageAnalyticsObservations struct {
	Counts map[string]int
	Pages  map[string]string
	IDs    []string
}

func splitPageAnalyticsObservations(observations []pageAnalyticsObservation) (map[string]int, rowPageAnalyticsObservations) {
	pageCounts := map[string]int{}
	rows := rowPageAnalyticsObservations{Counts: map[string]int{}, Pages: map[string]string{}}
	for _, observation := range observations {
		// A database opened alone counts as itself (database_analytics.go).
		if observation.ContainerID != "" {
			continue
		}
		if observation.RowID == "" {
			pageCounts[observation.PageID]++
			continue
		}
		if rows.Counts[observation.RowID] == 0 {
			rows.IDs = append(rows.IDs, observation.RowID)
		}
		rows.Counts[observation.RowID]++
		rows.Pages[observation.RowID] = observation.PageID
	}
	sort.Strings(rows.IDs)
	return pageCounts, rows
}

// rowPageAnalyticsRowsHeld reports whether every observed row lives in the
// workspace on the page the observation names.
func rowPageAnalyticsRowsHeld(app core.App, workspaceID string, rows rowPageAnalyticsObservations) (bool, error) {
	held, err := rowPageAnalyticsHeldObservations(app, workspaceID, rows)
	if err != nil {
		return false, err
	}
	return len(held.IDs) == len(rows.IDs), nil
}

// rowPageAnalyticsHeldObservations is the observed rows that live in the
// workspace on the page the observation names. A row or template gone since it
// was viewed leaves the rest of its batch to count.
func rowPageAnalyticsHeldObservations(app core.App, workspaceID string, rows rowPageAnalyticsObservations) (rowPageAnalyticsObservations, error) {
	held := rowPageAnalyticsObservations{Counts: map[string]int{}, Pages: map[string]string{}}
	if len(rows.IDs) == 0 {
		return held, nil
	}
	encoded, err := json.Marshal(rows.IDs)
	if err != nil {
		return held, err
	}
	stored := []struct {
		ID     string `db:"id"`
		PageID string `db:"page_id"`
	}{}
	// The rows are found by id; the workspace is read after (+), or SQLite,
	// which keeps no statistics, reads the workspace's every row.
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[page_id]] FROM {{leafen_database_rows}}
		WHERE +[[workspace_id]] = {:workspaceId} AND [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).All(&stored); err != nil {
		return held, err
	}
	// A live template's page counts its views as a database page does
	// (database_template_row_page.go).
	if len(stored) < len(rows.IDs) {
		templates := []struct {
			ID     string `db:"id"`
			PageID string `db:"page_id"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[template]].[[id]], [[template]].[[page_id]] FROM {{leafen_database_templates}} [[template]]
			LEFT JOIN {{leafen_database_toolbar_template_state}} [[state]] ON [[state]].[[template_id]] = [[template]].[[id]]
			WHERE +[[template]].[[workspace_id]] = {:workspaceId} AND COALESCE([[state]].[[deleted_at]], '') = ''
				AND [[template]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
		`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).All(&templates); err != nil {
			return held, err
		}
		stored = append(stored, templates...)
	}
	for _, row := range stored {
		if rows.Pages[row.ID] == row.PageID {
			held.IDs = append(held.IDs, row.ID)
			held.Counts[row.ID], held.Pages[row.ID] = rows.Counts[row.ID], row.PageID
		}
	}
	sort.Strings(held.IDs)
	return held, nil
}

// rowPageAnalyticsViewerRows is the batch's rows whose viewer is named and
// those that keep their line in who viewed (pageAnalyticsViewerPages): their
// page allows it and so does the row's own "내 조회 기록 표시", kept for its
// page record (of the row's ID) as a page's is.
func rowPageAnalyticsViewerRows(
	app core.App,
	workspaceID, principalID string,
	rows rowPageAnalyticsObservations,
	identifiedPages, keptPages map[string]struct{},
) (identified map[string]struct{}, kept map[string]struct{}, err error) {
	identified, kept = map[string]struct{}{}, map[string]struct{}{}
	if len(rows.IDs) == 0 {
		return identified, kept, nil
	}
	allowed, keptIDs, err := pageAnalyticsViewerPages(app, workspaceID, principalID, rows.IDs)
	if err != nil {
		return nil, nil, err
	}
	for _, rowID := range allowed {
		if _, ok := identifiedPages[rows.Pages[rowID]]; ok {
			identified[rowID] = struct{}{}
		}
	}
	for _, rowID := range keptIDs {
		if _, ok := keptPages[rows.Pages[rowID]]; ok {
			kept[rowID] = struct{}{}
		}
	}
	return identified, kept, nil
}

// recordRowPageAnalytics counts the batch's row views that count now
// (counted, page_analytics_view_window.go), moves every viewed row's
// recent viewer, and returns how many rows gained a unique viewer and how
// many kept a recent viewer; now is on the viewer's clock, whose date is
// the day the views fall on (page_analytics_clock.go).
func recordRowPageAnalytics(
	app core.App,
	workspaceID, principalID string,
	rows, counted rowPageAnalyticsObservations,
	identifiedRows, keptRows map[string]struct{},
	now time.Time,
) (int, int, error) {
	// A template's page is held as a row is (rowPageAnalyticsRowsHeld) but
	// has no row record to count against; only rows count here.
	rows, err := rowPageAnalyticsStoredRows(app, workspaceID, rows)
	if err != nil {
		return 0, 0, err
	}
	if len(rows.IDs) == 0 {
		return 0, 0, nil
	}
	identified, kept, countedIDs := []string{}, []string{}, []string{}
	for _, rowID := range rows.IDs {
		if counted.Counts[rowID] == 0 {
			continue
		}
		countedIDs = append(countedIDs, rowID)
		if _, ok := identifiedRows[rowID]; ok {
			identified = append(identified, rowID)
		}
	}
	for _, rowID := range rows.IDs {
		if _, ok := keptRows[rowID]; ok {
			kept = append(kept, rowID)
		}
	}
	day, observedAt := pageAnalyticsDayOf(now), pageAnalyticsTimestamp(now)
	unique := map[string]struct{}{}
	if len(identified) > 0 {
		params := dbx.Params{"workspaceId": workspaceID, "day": day, "viewerKey": principalID, "observedAt": observedAt}
		values := make([]string, 0, len(identified))
		for index, rowID := range identified {
			key := fmt.Sprintf("row%d", index)
			params[key] = rowID
			values = append(values, fmt.Sprintf("({:workspaceId}, {:%s}, {:day}, {:viewerKey}, {:observedAt})", key))
		}
		inserted := []struct {
			RowID string `db:"row_id"`
		}{}
		if err := app.DB().NewQuery(`
			INSERT INTO {{leafen_row_page_analytics_unique_viewers}} (
				[[workspace_id]], [[row_id]], [[day]], [[viewer_key]], [[observed_at]]
			) VALUES ` + joinSQL(values) + `
			ON CONFLICT ([[workspace_id]], [[row_id]], [[day]], [[viewer_key]]) DO NOTHING
			RETURNING [[row_id]]
		`).Bind(params).All(&inserted); err != nil {
			return 0, 0, err
		}
		for _, row := range inserted {
			unique[row.RowID] = struct{}{}
		}
	}
	params := dbx.Params{
		"workspaceId": workspaceID, "day": day, "lastViewedAt": observedAt,
		"retentionAt": now.UTC().AddDate(0, 0, maxPageAnalyticsReadDays).Format(pageAnalyticsTimeLayout),
	}
	values := make([]string, 0, len(countedIDs))
	for index, rowID := range countedIDs {
		rowKey, countKey, uniqueKey := fmt.Sprintf("row%d", index), fmt.Sprintf("count%d", index), fmt.Sprintf("unique%d", index)
		_, isUnique := unique[rowID]
		params[rowKey], params[countKey], params[uniqueKey] = rowID, counted.Counts[rowID], boolInt(isUnique)
		values = append(values, fmt.Sprintf("({:workspaceId}, {:%s}, {:day}, {:%s}, {:%s}, {:lastViewedAt}, {:retentionAt})",
			rowKey, countKey, uniqueKey))
	}
	if len(values) > 0 {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_row_page_analytics_daily}} (
				[[workspace_id]], [[row_id]], [[day]], [[total_views]], [[unique_member_views]],
				[[last_viewed_at]], [[retention_at]]
			) VALUES ` + joinSQL(values) + `
			ON CONFLICT ([[workspace_id]], [[row_id]], [[day]]) DO UPDATE SET
				[[total_views]] = [[total_views]] + excluded.[[total_views]],
				[[unique_member_views]] = [[unique_member_views]] + excluded.[[unique_member_views]],
				[[last_viewed_at]] = excluded.[[last_viewed_at]],
				[[retention_at]] = excluded.[[retention_at]]
		`).Bind(params).Execute(); err != nil {
			return 0, 0, err
		}
	}
	if len(kept) > 0 {
		params := dbx.Params{"workspaceId": workspaceID, "userId": principalID, "lastViewedAt": observedAt}
		values := make([]string, 0, len(kept))
		for index, rowID := range kept {
			key := fmt.Sprintf("row%d", index)
			params[key] = rowID
			values = append(values, fmt.Sprintf("({:workspaceId}, {:%s}, {:userId}, {:lastViewedAt})", key))
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_row_page_analytics_recent_viewers}} (
				[[workspace_id]], [[row_id]], [[user_id]], [[last_viewed_at]]
			) VALUES ` + joinSQL(values) + `
			ON CONFLICT ([[workspace_id]], [[row_id]], [[user_id]]) DO UPDATE SET
				[[last_viewed_at]] = excluded.[[last_viewed_at]]
		`).Bind(params).Execute(); err != nil {
			return 0, 0, err
		}
	}
	return len(unique), len(kept), nil
}

// readRowPageAnalyticsViews returns the views of the rows whose pages the
// member may read analytics for (editors, as for pages), over the retained
// history, with each row's last viewers for a feed post's faces. Rows the
// member may not read are left out.
func readRowPageAnalyticsViews(event *core.RequestEvent, user *core.Record, input pageAnalyticsRowViewsRequest) error {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	if input.Action != pageAnalyticsActionGetRowViews || !pageAnalyticsIdentityPattern.MatchString(input.WorkspaceID) ||
		len(input.RowIDs) == 0 || len(input.RowIDs) > maxPageAnalyticsBatchObservations {
		return pageAnalyticsInvalidRequest(event)
	}
	for index := range input.RowIDs {
		input.RowIDs[index] = strings.TrimSpace(input.RowIDs[index])
		if !pageAnalyticsIdentityPattern.MatchString(input.RowIDs[index]) {
			return pageAnalyticsInvalidRequest(event)
		}
	}
	response := pageAnalyticsRowViewsResponse{Rows: []pageAnalyticsRowViews{}}
	err := runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		encoded, err := json.Marshal(input.RowIDs)
		if err != nil {
			return err
		}
		rows := []struct {
			ID          string `db:"id"`
			PageID      string `db:"page_id"`
			TotalViews  int    `db:"total_views"`
			UniqueViews int    `db:"unique_views"`
		}{}
		if err := txApp.DB().NewQuery(`
			SELECT [[row]].[[id]], [[row]].[[page_id]],
				COALESCE(SUM([[daily]].[[total_views]]), 0) AS [[total_views]],
				COALESCE(SUM([[daily]].[[unique_member_views]]), 0) AS [[unique_views]]
			FROM {{leafen_database_rows}} AS [[row]]
			LEFT JOIN {{leafen_row_page_analytics_daily}} AS [[daily]]
				ON [[daily]].[[row_id]] = [[row]].[[id]] AND [[daily]].[[workspace_id]] = [[row]].[[workspace_id]]
			WHERE [[row]].[[workspace_id]] = {:workspaceId}
				AND [[row]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
			GROUP BY [[row]].[[id]], [[row]].[[page_id]]
			ORDER BY [[row]].[[id]]
		`).Bind(dbx.Params{"workspaceId": input.WorkspaceID, "ids": string(encoded)}).All(&rows); err != nil {
			return err
		}
		// A guest reads none of the workspace's analytics (page_analytics_readers.go).
		if member, err := pageAnalyticsReader(txApp, user.Id, input.WorkspaceID); err != nil || !member {
			return err
		}
		// Where the organization names no one, the views are only counted.
		display, err := workspaceViewDisplay(txApp, input.WorkspaceID)
		if err != nil {
			return err
		}
		editable := map[string]bool{}
		for _, row := range rows {
			allowed, checked := editable[row.PageID]
			if !checked {
				authorization, err := authorizePageBody(txApp, user.Id, row.PageID, permissionWorkspaceWrite)
				if err != nil && !isPermissionDenied(err) {
					return err
				}
				allowed = err == nil && authorization.Workspace.WorkspaceID == input.WorkspaceID &&
					pageAnalyticsPolicy(authorization.Workspace.Workspace).AnalyticsEnabled
				editable[row.PageID] = allowed
			}
			if allowed {
				recent := []pageAnalyticsRecentViewer{}
				if display != viewDisplayHidden {
					if recent, err = rowPageAnalyticsLastViewers(txApp, input.WorkspaceID, row.ID, user.Id, display == viewDisplayShown); err != nil {
						return err
					}
				}
				response.Rows = append(response.Rows, pageAnalyticsRowViews{
					RowID: row.ID, TotalViews: row.TotalViews, UniqueViews: row.UniqueViews, RecentViewers: recent,
				})
			}
		}
		return nil
	})
	if err != nil {
		if mapped, ok := persistentWriteStorageAPIError(event, err, persistentPageAnalytics, "read-rows"); ok {
			return mapped
		}
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodePageAnalyticsFailed,
			"Couldn't load the page analytics.", "page_analytics.read_rows", "query",
			errorCodePageAnalyticsFailed, err,
		)
	}
	return event.JSON(http.StatusOK, response)
}

// rowPageAnalyticsLastViewersShown is how many faces a feed post shows.
const rowPageAnalyticsLastViewersShown = 3

// rowPageAnalyticsLastViewers are a row page's latest viewers, newest first;
// everyone when the organization shows everyone who viewed.
func rowPageAnalyticsLastViewers(app core.App, workspaceID, rowID, readerID string, everyone bool) ([]pageAnalyticsRecentViewer, error) {
	rows := []storedPageAnalyticsRecentViewer{}
	if err := app.DB().NewQuery(rowPageAnalyticsRecentViewersSQL).Bind(dbx.Params{
		"workspaceId": workspaceID, "targetId": rowID, "limit": rowPageAnalyticsLastViewersShown,
		"everyone": boolInt(everyone), "readerId": readerID,
	}).All(&rows); err != nil {
		return nil, err
	}
	recent := make([]pageAnalyticsRecentViewer, 0, len(rows))
	for _, row := range rows {
		var displayName *string
		if row.DisplayName.Valid && strings.TrimSpace(row.DisplayName.String) != "" {
			value := row.DisplayName.String
			displayName = &value
		}
		recent = append(recent, pageAnalyticsRecentViewer{UserID: row.UserID, DisplayName: displayName, LastViewedAt: row.LastViewedAt})
	}
	return recent, nil
}

// rowPageAnalyticsStoredRows is the observed rows that are database rows,
// a template's page left out.
func rowPageAnalyticsStoredRows(app core.App, workspaceID string, rows rowPageAnalyticsObservations) (rowPageAnalyticsObservations, error) {
	if len(rows.IDs) == 0 {
		return rows, nil
	}
	encoded, err := json.Marshal(rows.IDs)
	if err != nil {
		return rows, err
	}
	stored := []string{}
	// The rows are found by id, the workspace read after (+), as above.
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_rows}}
		WHERE +[[workspace_id]] = {:workspaceId} AND [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).Column(&stored); err != nil {
		return rows, err
	}
	kept := rowPageAnalyticsObservations{Counts: map[string]int{}, Pages: map[string]string{}, IDs: stored}
	for _, id := range stored {
		kept.Counts[id], kept.Pages[id] = rows.Counts[id], rows.Pages[id]
	}
	return kept, nil
}
