package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const pageAnalyticsOperationRecord = "page_analytics.record"

var pageAnalyticsIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,160}$`)

type pageAnalyticsRecordStatus int

const (
	pageAnalyticsRecordOK pageAnalyticsRecordStatus = iota
	pageAnalyticsRecordNotFound
	pageAnalyticsRecordIdentityReused
)

type storedPageAnalyticsBatch struct {
	PrincipalID   string `db:"principal_id"`
	RequestHash   string `db:"request_hash"`
	Accepted      int    `db:"accepted"`
	DailyUpdated  int    `db:"daily_updated"`
	RecentUpdated int    `db:"recent_updated"`
	UniqueAdded   int    `db:"unique_added"`
}

type storedPageAnalyticsUnique struct {
	PageID string `db:"page_id"`
}

func recordPageAnalyticsMember(
	event *core.RequestEvent,
	user *core.Record,
	input pageAnalyticsMemberRecordRequest,
) error {
	if len(input.Observations) > maxPageAnalyticsBatchObservations {
		return apiError(event, http.StatusRequestEntityTooLarge, errorCodePageAnalyticsBatchLimitExceeded, "The number of pages recorded at once was exceeded.")
	}
	if !normalizePageAnalyticsMemberInput(&input) {
		return apiError(event, http.StatusBadRequest, errorCodePageAnalyticsInvalid, "Check the analytics record details.")
	}

	response, status, err := recordPageAnalyticsMemberBatch(event.App, user.Id, input)
	if status == pageAnalyticsRecordNotFound && err != nil {
		return writeGraphError(event, err, "record", errorCodePageAnalyticsFailed, "Couldn't save the page analytics record.")
	}
	if status == pageAnalyticsRecordNotFound {
		return apiError(event, http.StatusNotFound, errorCodePageAnalyticsNotFound, "The page to analyze was not found.")
	}
	if status == pageAnalyticsRecordIdentityReused {
		return apiError(event, http.StatusConflict, errorCodePageAnalyticsBatchIdentityReused, "This ID was used for a different analytics record.")
	}
	if err != nil {
		if mapped, ok := persistentWriteStorageAPIError(
			event, err, persistentPageAnalytics, "record-member",
		); ok {
			return mapped
		}
		return apiErrorWithCause(
			event,
			http.StatusInternalServerError,
			errorCodePageAnalyticsFailed,
			"Couldn't save the page analytics record.",
			pageAnalyticsOperationRecord,
			"transaction",
			errorCodePageAnalyticsFailed,
			err,
		)
	}
	return event.JSON(http.StatusOK, response)
}

func normalizePageAnalyticsMemberInput(input *pageAnalyticsMemberRecordRequest) bool {
	input.Action = strings.TrimSpace(input.Action)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.BatchID = strings.TrimSpace(input.BatchID)
	if input.Action != pageAnalyticsActionRecord ||
		!pageAnalyticsIdentityPattern.MatchString(input.WorkspaceID) ||
		!pageAnalyticsIdentityPattern.MatchString(input.BatchID) ||
		len(input.Observations) == 0 {
		return false
	}
	for index := range input.Observations {
		observation := &input.Observations[index]
		observation.PageID = strings.TrimSpace(observation.PageID)
		observation.RowID = strings.TrimSpace(observation.RowID)
		observation.ContainerID = strings.TrimSpace(observation.ContainerID)
		// A view is of a page, a row page or a database opened alone.
		if !pageAnalyticsIdentityPattern.MatchString(observation.PageID) ||
			(observation.RowID != "" && !pageAnalyticsIdentityPattern.MatchString(observation.RowID)) ||
			(observation.ContainerID != "" && (observation.RowID != "" || !pageAnalyticsIdentityPattern.MatchString(observation.ContainerID))) {
			return false
		}
	}
	return true
}

func applyPageAnalyticsMemberBatch(
	app core.App,
	principalID string,
	input pageAnalyticsMemberRecordRequest,
) (pageAnalyticsRecordResponse, pageAnalyticsRecordStatus, error) {
	requestHash, err := pageAnalyticsMemberRequestHash(principalID, input)
	if err != nil {
		return pageAnalyticsRecordResponse{}, pageAnalyticsRecordOK, err
	}
	pageCounts, rows := splitPageAnalyticsObservations(input.Observations)
	databases := splitDatabaseAnalyticsObservations(input.Observations)
	pageIDs := make([]string, 0, len(pageCounts))
	for pageID := range pageCounts {
		pageIDs = append(pageIDs, pageID)
	}
	sort.Strings(pageIDs)
	// Rows and databases are read through the pages that hold them.
	authorizedPages := map[string]struct{}{}
	for _, pageID := range pageIDs {
		authorizedPages[pageID] = struct{}{}
	}
	holdingPageIDs := []string{}
	for _, holding := range []rowPageAnalyticsObservations{rows, databases} {
		for _, id := range holding.IDs {
			pageID := holding.Pages[id]
			if _, seen := authorizedPages[pageID]; !seen {
				holdingPageIDs = append(holdingPageIDs, pageID)
			}
			authorizedPages[pageID] = struct{}{}
		}
	}
	authorizedPageIDs := append(append([]string{}, pageIDs...), holdingPageIDs...)
	sort.Strings(authorizedPageIDs)

	response := pageAnalyticsRecordResponse{}
	status := pageAnalyticsRecordOK
	undoViews := func() {}
	err = app.RunInTransaction(func(txApp core.App) error {
		// A row or template gone since it was viewed doesn't cost the batch the rest.
		rows, err := rowPageAnalyticsHeldObservations(txApp, input.WorkspaceID, rows)
		if err != nil {
			return err
		}
		databases, err := databaseAnalyticsHeldObservations(txApp, input.WorkspaceID, databases)
		if err != nil {
			return err
		}
		read := append(append(append([]string{}, pageIDs...), rows.IDs...), databaseAnalyticsPages(pageIDs, databases)...)
		authorization, err := authorizePageSet(txApp, principalID, input.WorkspaceID, read, permissionWorkspaceRead)
		if isPermissionDenied(err) {
			// What of it still counts, or whether it waits for an operation,
			// is asked page by page (page_analytics_readable.go).
			status = pageAnalyticsRecordNotFound
			return nil
		} else if err != nil {
			return err
		}
		if !pageAnalyticsPolicy(authorization.Workspace).AnalyticsEnabled {
			response = pageAnalyticsRecordResponse{BatchID: input.BatchID}
			return nil
		}

		now, viewed := pageAnalyticsNow(txApp, principalID)
		inserted, err := admitPageAnalyticsBatch(txApp, principalID, input, requestHash, now)
		if err != nil {
			return err
		}
		if !inserted {
			stored, err := readPageAnalyticsBatch(txApp, input.WorkspaceID, input.BatchID)
			if err != nil {
				return err
			}
			if stored.PrincipalID != principalID || stored.RequestHash != requestHash {
				status = pageAnalyticsRecordIdentityReused
				return nil
			}
			response = pageAnalyticsRecordResponse{
				Accepted: stored.Accepted, BatchID: input.BatchID,
				DailyUpdated: stored.DailyUpdated, RecentUpdated: stored.RecentUpdated,
				Replayed: true, UniqueAdded: stored.UniqueAdded,
			}
			return nil
		}

		identifiedAuthorizedPageIDs, keptAuthorizedPageIDs, err := pageAnalyticsViewerPages(
			txApp, input.WorkspaceID, principalID, authorizedPageIDs,
		)
		if err != nil {
			return err
		}
		identifiedPages := map[string]struct{}{}
		for _, pageID := range identifiedAuthorizedPageIDs {
			identifiedPages[pageID] = struct{}{}
		}
		keptPages := map[string]struct{}{}
		for _, pageID := range keptAuthorizedPageIDs {
			keptPages[pageID] = struct{}{}
		}
		identifiedPageIDs := make([]string, 0, len(pageIDs))
		keptPageIDs := make([]string, 0, len(pageIDs))
		for _, pageID := range pageIDs {
			if _, ok := identifiedPages[pageID]; ok {
				identifiedPageIDs = append(identifiedPageIDs, pageID)
			}
			if _, ok := keptPages[pageID]; ok {
				keptPageIDs = append(keptPageIDs, pageID)
			}
		}
		identifiedRows, keptRows, err := rowPageAnalyticsViewerRows(txApp, input.WorkspaceID, principalID, rows, identifiedPages, keptPages)
		if err != nil {
			return err
		}
		identifiedDatabases, keptDatabases, err := databaseAnalyticsViewers(txApp, input.WorkspaceID, principalID, databases, identifiedPages, keptPages)
		if err != nil {
			return err
		}
		// Opens within a minute of the viewer's last counted view of the same
		// thing add no view (page_analytics_view_window.go).
		countedPageIDs, countedRows, countedDatabases, undo := pageAnalyticsViewWindowOf(app).countViews(
			input.WorkspaceID, principalID, pageIDs, rows, databases, now,
		)
		undoViews = undo
		countedPages := map[string]int{}
		countedIdentifiedPageIDs := make([]string, 0, len(countedPageIDs))
		for _, pageID := range countedPageIDs {
			countedPages[pageID] = 1
			if _, ok := identifiedPages[pageID]; ok {
				countedIdentifiedPageIDs = append(countedIdentifiedPageIDs, pageID)
			}
		}
		visitedSharePageIDs := append([]string{}, identifiedPageIDs...)
		for _, rowID := range rows.IDs {
			if _, ok := identifiedRows[rowID]; ok {
				visitedSharePageIDs = append(visitedSharePageIDs, rowID)
			}
		}
		// A database opened alone is a visit to the page holding it.
		for _, containerID := range databases.IDs {
			pageID := databases.Pages[containerID]
			if _, ok := identifiedDatabases[containerID]; ok && !slices.Contains(visitedSharePageIDs, pageID) {
				visitedSharePageIDs = append(visitedSharePageIDs, pageID)
			}
		}
		if err := queuePageShareAccessNotices(txApp, input.WorkspaceID, principalID, visitedSharePageIDs, now); err != nil {
			return err
		}
		// A row's or database's view reaches the legal hold log as a view of its page.
		viewedPageIDs := append([]string{}, identifiedPageIDs...)
		for _, rowID := range rows.IDs {
			pageID := rows.Pages[rowID]
			if _, ok := identifiedRows[rowID]; ok && !slices.Contains(viewedPageIDs, pageID) {
				viewedPageIDs = append(viewedPageIDs, pageID)
			}
		}
		for _, containerID := range databases.IDs {
			pageID := databases.Pages[containerID]
			if _, ok := identifiedDatabases[containerID]; ok && !slices.Contains(viewedPageIDs, pageID) {
				viewedPageIDs = append(viewedPageIDs, pageID)
			}
		}
		if err := recordNotionAdminLegalHoldPageInteraction(
			txApp,
			pageViewedInteractions(
				input.BatchID, authorization.Organization.OrganizationID,
				input.WorkspaceID, principalID, viewedPageIDs, now,
			)...,
		); err != nil {
			return err
		}
		uniquePageIDs, err := insertPageAnalyticsUniqueMembers(
			txApp, input.WorkspaceID, principalID, countedIdentifiedPageIDs, viewed,
		)
		if err != nil {
			return err
		}
		uniquePages := make(map[string]struct{}, len(uniquePageIDs))
		for _, pageID := range uniquePageIDs {
			uniquePages[pageID] = struct{}{}
		}
		if len(countedPageIDs) > 0 {
			if err := upsertPageAnalyticsDaily(txApp, input.WorkspaceID, countedPageIDs, countedPages, uniquePages, viewed); err != nil {
				return err
			}
		}
		if err := upsertPageAnalyticsRecentMembers(txApp, input.WorkspaceID, principalID, keptPageIDs, now); err != nil {
			return err
		}
		uniqueRows, recentRows, err := recordRowPageAnalytics(
			txApp, input.WorkspaceID, principalID, rows, countedRows, identifiedRows, keptRows, viewed,
		)
		if err != nil {
			return err
		}
		uniqueDatabases, recentDatabases, err := recordDatabaseAnalytics(
			txApp, input.WorkspaceID, principalID, databases, countedDatabases, identifiedDatabases, keptDatabases, viewed,
		)
		if err != nil {
			return err
		}
		if err := recordWorkspaceAnalyticsMemberViews(
			txApp, input.WorkspaceID, principalID, countedMemberObservations(countedPageIDs, countedRows, countedDatabases),
			identifiedPages, identifiedRows, identifiedDatabases, viewed,
		); err != nil {
			return err
		}
		response = pageAnalyticsRecordResponse{
			Accepted: len(input.Observations), BatchID: input.BatchID,
			DailyUpdated:  len(countedPageIDs) + len(countedRows.IDs) + len(countedDatabases.IDs),
			RecentUpdated: len(keptPageIDs) + recentRows + recentDatabases,
			Replayed:      false, UniqueAdded: len(uniquePageIDs) + uniqueRows + uniqueDatabases,
		}
		return completePageAnalyticsBatch(txApp, input.WorkspaceID, response)
	})
	if err != nil {
		undoViews()
	}
	return response, status, err
}

func pageAnalyticsMemberRequestHash(principalID string, input pageAnalyticsMemberRecordRequest) (string, error) {
	encoded, err := json.Marshal(struct {
		Action       string                     `json:"action"`
		WorkspaceID  string                     `json:"workspaceId"`
		PrincipalID  string                     `json:"principalId"`
		Observations []pageAnalyticsObservation `json:"observations"`
	}{input.Action, input.WorkspaceID, principalID, input.Observations})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func admitPageAnalyticsBatch(
	app core.App,
	principalID string,
	input pageAnalyticsMemberRecordRequest,
	requestHash string,
	now time.Time,
) (bool, error) {
	result, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_analytics_batches}} (
			[[workspace_id]], [[batch_id]], [[principal_id]], [[request_hash]],
			[[accepted]], [[daily_updated]], [[recent_updated]], [[unique_added]], [[completed_at]]
		) VALUES ({:workspaceId}, {:batchId}, {:principalId}, {:requestHash}, 0, 0, 0, 0, {:completedAt})
		ON CONFLICT ([[workspace_id]], [[batch_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"workspaceId": input.WorkspaceID, "batchId": input.BatchID,
		"principalId": principalID, "requestHash": requestHash,
		"completedAt": pageAnalyticsTimestamp(now),
	}).Execute()
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

func readPageAnalyticsBatch(app core.App, workspaceID string, batchID string) (storedPageAnalyticsBatch, error) {
	rows := []storedPageAnalyticsBatch{}
	err := app.DB().NewQuery(`
		SELECT [[principal_id]], [[request_hash]], [[accepted]], [[daily_updated]],
			[[recent_updated]], [[unique_added]]
		FROM {{leafen_page_analytics_batches}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[batch_id]] = {:batchId}
		LIMIT 2
	`).Bind(dbx.Params{"workspaceId": workspaceID, "batchId": batchID}).All(&rows)
	if err != nil {
		return storedPageAnalyticsBatch{}, err
	}
	if len(rows) != 1 {
		return storedPageAnalyticsBatch{}, errors.New("page analytics batch receipt is ambiguous")
	}
	return rows[0], nil
}

func insertPageAnalyticsUniqueMembers(
	app core.App,
	workspaceID string,
	principalID string,
	pageIDs []string,
	now time.Time,
) ([]string, error) {
	if len(pageIDs) == 0 {
		return []string{}, nil
	}
	params := dbx.Params{
		"workspaceId": workspaceID, "day": pageAnalyticsDayOf(now),
		"viewerKey": principalID, "observedAt": pageAnalyticsTimestamp(now),
	}
	values := make([]string, 0, len(pageIDs))
	for index, pageID := range pageIDs {
		key := fmt.Sprintf("page%d", index)
		params[key] = pageID
		values = append(values, fmt.Sprintf("({:workspaceId}, {:%s}, {:day}, 'member', {:viewerKey}, {:observedAt})", key))
	}
	inserted := []storedPageAnalyticsUnique{}
	err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_analytics_unique_viewers}} (
			[[workspace_id]], [[page_id]], [[day]], [[viewer_kind]], [[viewer_key]], [[observed_at]]
		) VALUES ` + joinSQL(values) + `
		ON CONFLICT ([[workspace_id]], [[page_id]], [[day]], [[viewer_kind]], [[viewer_key]]) DO NOTHING
		RETURNING [[page_id]]
	`).Bind(params).All(&inserted)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(inserted))
	for _, row := range inserted {
		result = append(result, row.PageID)
	}
	return result, nil
}

func upsertPageAnalyticsDaily(
	app core.App,
	workspaceID string,
	pageIDs []string,
	pageCounts map[string]int,
	uniquePages map[string]struct{},
	now time.Time,
) error {
	params := dbx.Params{
		"workspaceId": workspaceID, "day": pageAnalyticsDayOf(now),
		"lastViewedAt": pageAnalyticsTimestamp(now),
		"retentionAt":  now.UTC().AddDate(0, 0, maxPageAnalyticsReadDays).Format(pageAnalyticsTimeLayout),
	}
	values := make([]string, 0, len(pageIDs))
	for index, pageID := range pageIDs {
		pageKey := fmt.Sprintf("page%d", index)
		countKey := fmt.Sprintf("count%d", index)
		uniqueKey := fmt.Sprintf("unique%d", index)
		params[pageKey] = pageID
		params[countKey] = pageCounts[pageID]
		_, isUnique := uniquePages[pageID]
		params[uniqueKey] = boolInt(isUnique)
		values = append(values, fmt.Sprintf("({:workspaceId}, {:%s}, {:day}, {:%s}, {:%s}, 0, {:lastViewedAt}, {:retentionAt})", pageKey, countKey, uniqueKey))
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_analytics_daily}} (
			[[workspace_id]], [[page_id]], [[day]], [[total_views]], [[unique_member_views]],
			[[unique_anonymous_views]], [[last_viewed_at]], [[retention_at]]
		) VALUES ` + joinSQL(values) + `
		ON CONFLICT ([[workspace_id]], [[page_id]], [[day]]) DO UPDATE SET
			[[total_views]] = [[total_views]] + excluded.[[total_views]],
			[[unique_member_views]] = [[unique_member_views]] + excluded.[[unique_member_views]],
			[[last_viewed_at]] = excluded.[[last_viewed_at]],
			[[retention_at]] = excluded.[[retention_at]]
	`).Bind(params).Execute()
	return err
}

func upsertPageAnalyticsRecentMembers(
	app core.App,
	workspaceID string,
	principalID string,
	pageIDs []string,
	now time.Time,
) error {
	if len(pageIDs) == 0 {
		return nil
	}
	params := dbx.Params{
		"workspaceId": workspaceID, "userId": principalID,
		"lastViewedAt": pageAnalyticsTimestamp(now),
	}
	values := make([]string, 0, len(pageIDs))
	for index, pageID := range pageIDs {
		key := fmt.Sprintf("page%d", index)
		params[key] = pageID
		values = append(values, fmt.Sprintf("({:workspaceId}, {:%s}, {:userId}, {:lastViewedAt})", key))
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_analytics_recent_viewers}} (
			[[workspace_id]], [[page_id]], [[user_id]], [[last_viewed_at]]
		) VALUES ` + joinSQL(values) + `
		ON CONFLICT ([[workspace_id]], [[page_id]], [[user_id]]) DO UPDATE SET
			[[last_viewed_at]] = excluded.[[last_viewed_at]]
	`).Bind(params).Execute()
	return err
}

func completePageAnalyticsBatch(app core.App, workspaceID string, response pageAnalyticsRecordResponse) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_analytics_batches}}
		SET [[accepted]] = {:accepted}, [[daily_updated]] = {:dailyUpdated},
			[[recent_updated]] = {:recentUpdated}, [[unique_added]] = {:uniqueAdded}
		WHERE [[workspace_id]] = {:workspaceId} AND [[batch_id]] = {:batchId}
	`).Bind(dbx.Params{
		"accepted": response.Accepted, "dailyUpdated": response.DailyUpdated,
		"recentUpdated": response.RecentUpdated, "uniqueAdded": response.UniqueAdded,
		"workspaceId": workspaceID, "batchId": response.BatchID,
	}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("page analytics batch receipt did not complete")
	}
	notifyPageAnalyticsRetentionAfterCommit(app)
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
