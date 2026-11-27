package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An inline database opened alone is viewed as itself, as Notion's (seen
// 2026-11-11: its analytics tab counts its own views, and the page holding
// it counts none of them): the analytics batch's observation names its
// container beside the page holding it. Reading it needs that page, so the
// batch authorizes it; a database counts only its views opened alone. It is
// named in who viewed when its page would be and the person hasn't declined
// it on the database itself, and its view reaches the legal hold log as a
// view of that page.

// splitDatabaseAnalyticsObservations is a batch's views of databases opened
// alone, by container, each with the page holding it.
func splitDatabaseAnalyticsObservations(observations []pageAnalyticsObservation) rowPageAnalyticsObservations {
	databases := rowPageAnalyticsObservations{Counts: map[string]int{}, Pages: map[string]string{}}
	for _, observation := range observations {
		if observation.ContainerID == "" {
			continue
		}
		if databases.Counts[observation.ContainerID] == 0 {
			databases.IDs = append(databases.IDs, observation.ContainerID)
		}
		databases.Counts[observation.ContainerID]++
		databases.Pages[observation.ContainerID] = observation.PageID
	}
	sort.Strings(databases.IDs)
	return databases
}

// databaseAnalyticsHeldObservations is the observed databases that stand in
// the workspace on the page the observation names. One gone since it was
// viewed leaves the rest of its batch to count.
func databaseAnalyticsHeldObservations(app core.App, workspaceID string, databases rowPageAnalyticsObservations) (rowPageAnalyticsObservations, error) {
	held := rowPageAnalyticsObservations{Counts: map[string]int{}, Pages: map[string]string{}}
	if len(databases.IDs) == 0 {
		return held, nil
	}
	encoded, err := json.Marshal(databases.IDs)
	if err != nil {
		return held, err
	}
	stored := []struct {
		ID     string `db:"id"`
		PageID string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[page_id]] FROM {{leafen_database_containers}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).All(&stored); err != nil {
		return held, err
	}
	for _, database := range stored {
		if databases.Pages[database.ID] == database.PageID {
			held.IDs = append(held.IDs, database.ID)
			held.Counts[database.ID], held.Pages[database.ID] = databases.Counts[database.ID], database.PageID
		}
	}
	sort.Strings(held.IDs)
	return held, nil
}

// databaseAnalyticsPages is the pages holding the batch's databases that it
// doesn't view as pages, each once.
func databaseAnalyticsPages(pageIDs []string, databases rowPageAnalyticsObservations) []string {
	seen := map[string]struct{}{}
	for _, pageID := range pageIDs {
		seen[pageID] = struct{}{}
	}
	holding := []string{}
	for _, containerID := range databases.IDs {
		pageID := databases.Pages[containerID]
		if _, ok := seen[pageID]; !ok {
			seen[pageID] = struct{}{}
			holding = append(holding, pageID)
		}
	}
	return holding
}

// databaseAnalyticsHeld reports whether a database stands in the workspace on
// the page named.
func databaseAnalyticsHeld(app core.App, workspaceID, pageID, containerID string) (bool, error) {
	held, err := databaseAnalyticsHeldObservations(app, workspaceID, rowPageAnalyticsObservations{
		IDs: []string{containerID}, Pages: map[string]string{containerID: pageID}, Counts: map[string]int{containerID: 1},
	})
	return len(held.IDs) == 1, err
}

// databaseAnalyticsOptedOut is the databases among those named on which the
// person declined "내 조회 기록 표시"; none where the organization shows
// everyone.
func databaseAnalyticsOptedOut(app core.App, workspaceID, userID string, containerIDs []string) (map[string]struct{}, error) {
	optedOut := map[string]struct{}{}
	if len(containerIDs) == 0 {
		return optedOut, nil
	}
	if display, err := workspaceViewDisplay(app, workspaceID); err != nil || display == viewDisplayShown {
		return optedOut, err
	}
	encoded, err := json.Marshal(containerIDs)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[container_id]] FROM {{leafen_database_analytics_opt_outs}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}
			AND [[container_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID, "ids": string(encoded)}).Column(&ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		optedOut[id] = struct{}{}
	}
	return optedOut, nil
}

// saveDatabaseAnalyticsOptOut keeps or drops a person's "허용하지 않음" on a
// database.
func saveDatabaseAnalyticsOptOut(app core.App, workspaceID, pageID, containerID, userID string, allowed bool) error {
	params := dbx.Params{
		"workspaceId": workspaceID, "containerId": containerID, "pageId": pageID, "userId": userID,
		"createdAt": time.Now().UTC().Format(time.RFC3339Nano),
	}
	query := `
		INSERT INTO {{leafen_database_analytics_opt_outs}} (
			[[workspace_id]], [[container_id]], [[page_id]], [[user_id]], [[created_at]]
		) VALUES ({:workspaceId}, {:containerId}, {:pageId}, {:userId}, {:createdAt})
		ON CONFLICT ([[workspace_id]], [[user_id]], [[container_id]]) DO UPDATE SET [[page_id]] = excluded.[[page_id]]`
	if allowed {
		query = `DELETE FROM {{leafen_database_analytics_opt_outs}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId} AND [[container_id]] = {:containerId}`
	}
	_, err := app.DB().NewQuery(query).Bind(params).Execute()
	return err
}

// databaseAnalyticsViewers is the batch's databases whose viewer is named and
// those that keep their line in who viewed: their page allows it and the
// person hasn't declined it on the database.
func databaseAnalyticsViewers(
	app core.App,
	workspaceID, principalID string,
	databases rowPageAnalyticsObservations,
	identifiedPages, keptPages map[string]struct{},
) (identified map[string]struct{}, kept map[string]struct{}, err error) {
	identified, kept = map[string]struct{}{}, map[string]struct{}{}
	optedOut, err := databaseAnalyticsOptedOut(app, workspaceID, principalID, databases.IDs)
	if err != nil {
		return nil, nil, err
	}
	for _, containerID := range databases.IDs {
		if _, declined := optedOut[containerID]; declined {
			continue
		}
		if _, ok := identifiedPages[databases.Pages[containerID]]; ok {
			identified[containerID] = struct{}{}
		}
		if _, ok := keptPages[databases.Pages[containerID]]; ok {
			kept[containerID] = struct{}{}
		}
	}
	return identified, kept, nil
}

// recordDatabaseAnalytics counts the batch's database views that count now
// (counted, page_analytics_view_window.go), moves every viewed database's
// recent viewer, and returns how many databases gained a unique viewer and
// how many kept a recent viewer; now is on the viewer's clock, whose date is
// the day the views fall on (page_analytics_clock.go).
func recordDatabaseAnalytics(
	app core.App,
	workspaceID, principalID string,
	databases, counted rowPageAnalyticsObservations,
	identifiedDatabases, keptDatabases map[string]struct{},
	now time.Time,
) (int, int, error) {
	if len(databases.IDs) == 0 {
		return 0, 0, nil
	}
	day, observedAt := pageAnalyticsDayOf(now), pageAnalyticsTimestamp(now)
	unique := map[string]struct{}{}
	identified, kept := []string{}, []string{}
	for _, containerID := range counted.IDs {
		if _, ok := identifiedDatabases[containerID]; ok {
			identified = append(identified, containerID)
		}
	}
	for _, containerID := range databases.IDs {
		if _, ok := keptDatabases[containerID]; ok {
			kept = append(kept, containerID)
		}
	}
	if len(identified) > 0 {
		params := dbx.Params{"workspaceId": workspaceID, "day": day, "viewerKey": principalID, "observedAt": observedAt}
		values := make([]string, 0, len(identified))
		for index, containerID := range identified {
			containerKey, pageKey := fmt.Sprintf("container%d", index), fmt.Sprintf("page%d", index)
			params[containerKey], params[pageKey] = containerID, databases.Pages[containerID]
			values = append(values, fmt.Sprintf("({:workspaceId}, {:%s}, {:%s}, {:day}, {:viewerKey}, {:observedAt})", containerKey, pageKey))
		}
		inserted := []string{}
		if err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_analytics_unique_viewers}} (
				[[workspace_id]], [[container_id]], [[page_id]], [[day]], [[viewer_key]], [[observed_at]]
			) VALUES ` + joinSQL(values) + `
			ON CONFLICT ([[workspace_id]], [[container_id]], [[day]], [[viewer_key]]) DO NOTHING
			RETURNING [[container_id]]
		`).Bind(params).Column(&inserted); err != nil {
			return 0, 0, err
		}
		for _, containerID := range inserted {
			unique[containerID] = struct{}{}
		}
	}
	params := dbx.Params{
		"workspaceId": workspaceID, "day": day, "lastViewedAt": observedAt,
		"retentionAt": now.UTC().AddDate(0, 0, maxPageAnalyticsReadDays).Format(pageAnalyticsTimeLayout),
	}
	values := make([]string, 0, len(counted.IDs))
	for index, containerID := range counted.IDs {
		containerKey, pageKey := fmt.Sprintf("container%d", index), fmt.Sprintf("page%d", index)
		countKey, uniqueKey := fmt.Sprintf("count%d", index), fmt.Sprintf("unique%d", index)
		_, isUnique := unique[containerID]
		params[containerKey], params[pageKey] = containerID, databases.Pages[containerID]
		params[countKey], params[uniqueKey] = counted.Counts[containerID], boolInt(isUnique)
		values = append(values, fmt.Sprintf("({:workspaceId}, {:%s}, {:%s}, {:day}, {:%s}, {:%s}, {:lastViewedAt}, {:retentionAt})",
			containerKey, pageKey, countKey, uniqueKey))
	}
	// A database moved to another page takes its views along.
	if len(values) > 0 {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_analytics_daily}} (
				[[workspace_id]], [[container_id]], [[page_id]], [[day]], [[total_views]], [[unique_member_views]],
				[[last_viewed_at]], [[retention_at]]
			) VALUES ` + joinSQL(values) + `
			ON CONFLICT ([[workspace_id]], [[container_id]], [[day]]) DO UPDATE SET
				[[page_id]] = excluded.[[page_id]],
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
		for index, containerID := range kept {
			containerKey, pageKey := fmt.Sprintf("container%d", index), fmt.Sprintf("page%d", index)
			params[containerKey], params[pageKey] = containerID, databases.Pages[containerID]
			values = append(values, fmt.Sprintf("({:workspaceId}, {:%s}, {:%s}, {:userId}, {:lastViewedAt})", containerKey, pageKey))
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_analytics_recent_viewers}} (
				[[workspace_id]], [[container_id]], [[page_id]], [[user_id]], [[last_viewed_at]]
			) VALUES ` + joinSQL(values) + `
			ON CONFLICT ([[workspace_id]], [[container_id]], [[user_id]]) DO UPDATE SET
				[[page_id]] = excluded.[[page_id]],
				[[last_viewed_at]] = excluded.[[last_viewed_at]]
		`).Bind(params).Execute(); err != nil {
			return 0, 0, err
		}
	}
	return len(unique), len(kept), nil
}

const (
	databaseAnalyticsDailyTotalsSQL = `
		SELECT COALESCE(SUM([[total_views]]), 0) AS [[total_views]],
			COALESCE(SUM([[unique_member_views]]), 0) AS [[unique_member_views]],
			0 AS [[unique_anonymous_views]]
		FROM {{leafen_database_analytics_daily}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[container_id]] = {:targetId} AND [[day]] >= {:startDay}`
	databaseAnalyticsDaysSQL = `
		SELECT [[day]], [[total_views]], [[unique_member_views]] AS [[unique_views]]
		FROM {{leafen_database_analytics_daily}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[container_id]] = {:targetId} AND [[day]] >= {:startDay}
		ORDER BY [[day]]`
	// Who viewed goes by each member's account-wide "내 조회 기록 표시" as a
	// page's does; the database's own "허용하지 않음" keeps a view out of it
	// when the view is recorded.
	databaseAnalyticsRecentViewersSQL = `
		SELECT [[recent]].[[user_id]], [[user]].[[name]] AS [[display_name]], [[recent]].[[last_viewed_at]]
		FROM {{leafen_database_analytics_recent_viewers}} [[recent]]
		INDEXED BY [[idx_leafen_database_analytics_recent_viewed]]
		LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[recent]].[[user_id]]
		WHERE [[recent]].[[workspace_id]] = {:workspaceId} AND [[recent]].[[container_id]] = {:targetId}` + pageAnalyticsViewerShownSQL + `
		ORDER BY [[recent]].[[last_viewed_at]] DESC, [[recent]].[[user_id]] ASC
		LIMIT {:limit}`
	// A database's changes are its rows, views and properties, as its
	// updates list them (the workspace activity of its sources,
	// workspace_activity_read.go).
	databaseAnalyticsActivitySQL = `
		FROM {{leafen_workspace_activity}} AS [[activity]]
		JOIN {{users}} AS [[user]] ON [[user]].[[id]] = [[activity]].[[actor_id]]
		WHERE [[activity]].[[workspace_id]] = {:workspaceId}
			AND json_extract([[activity]].[[metadata_json]], '$.sourceId') IN (
				SELECT [[data_source_id]] FROM {{leafen_database_container_sources}} WHERE [[container_id]] = {:targetId})`
)

// readDatabaseAnalyticsEditors is who made a database opened alone and when,
// from its first update. It names no recent editors: Notion's lists only
// "생성자" for one (seen 2026-11-11 on a synthetic inline database whose
// rows another person added and whose title they changed, 16 minutes on),
// where a page or a full-page database lists its maker among them too.
func readDatabaseAnalyticsEditors(app core.App, workspaceID, containerID string) (*pageAnalyticsEditor, []pageAnalyticsEditor, error) {
	params := dbx.Params{"workspaceId": workspaceID, "targetId": containerID}
	editors := []pageAnalyticsEditor{}
	first := storedPageAnalyticsEditor{}
	err := app.DB().NewQuery(`
		SELECT [[activity]].[[actor_id]] AS [[user_id]], [[user]].[[name]] AS [[display_name]], [[activity]].[[created]] AS [[at]]
		` + databaseAnalyticsActivitySQL + `
		ORDER BY [[activity]].[[created]] ASC, [[activity]].[[id]] ASC LIMIT 1
	`).Bind(params).One(&first)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, editors, nil
	}
	if err != nil {
		return nil, nil, err
	}
	creator := first.wire()
	return &creator, editors, nil
}
