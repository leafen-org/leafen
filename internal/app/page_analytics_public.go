package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type pageAnalyticsPublicInput struct {
	Action         string
	AnonymousToken string
	BatchID        string
	Observations   []pageAnalyticsObservation
	SiteSlug       string
	Token          string
}

type publicAnalyticsAuthorizer func(
	core.App,
	[]string,
	time.Time,
) (publicVisibilityAuthorization, error)

func recordPageAnalyticsPublic(event *core.RequestEvent, input pageAnalyticsPublicRecordRequest) error {
	normalized := pageAnalyticsPublicInput{
		Action: input.Action, AnonymousToken: input.AnonymousToken,
		BatchID: input.BatchID, Observations: input.Observations, Token: input.Token,
	}
	return recordPageAnalyticsAnonymous(event, &normalized, func(
		app core.App, pageIDs []string, now time.Time,
	) (publicVisibilityAuthorization, error) {
		return authorizePublicSharePages(app, normalized.Token, pageIDs, now)
	})
}

func recordPageAnalyticsPublicSite(event *core.RequestEvent, input pageAnalyticsPublicSiteRecordRequest) error {
	normalized := pageAnalyticsPublicInput{
		Action: input.Action, AnonymousToken: input.AnonymousToken,
		BatchID: input.BatchID, Observations: input.Observations, SiteSlug: input.SiteSlug,
	}
	return recordPageAnalyticsAnonymous(event, &normalized, func(
		app core.App, pageIDs []string, now time.Time,
	) (publicVisibilityAuthorization, error) {
		return authorizePublicSitePages(app, normalized.SiteSlug, pageIDs, now)
	})
}

func recordPageAnalyticsAnonymous(
	event *core.RequestEvent,
	input *pageAnalyticsPublicInput,
	authorize publicAnalyticsAuthorizer,
) error {
	if len(input.Observations) > maxPageAnalyticsBatchObservations {
		return apiError(event, http.StatusRequestEntityTooLarge, errorCodePageAnalyticsBatchLimitExceeded, "The number of pages recorded at once was exceeded.")
	}
	if !normalizePageAnalyticsPublicInput(input) {
		return pageAnalyticsInvalidRequest(event)
	}
	response, status, err := recordPageAnalyticsAnonymousBatch(event.App, *input, authorize)
	if status == pageAnalyticsRecordNotFound {
		return apiError(event, http.StatusNotFound, errorCodePageAnalyticsNotFound, "The page to analyze was not found.")
	}
	if status == pageAnalyticsRecordIdentityReused {
		return apiError(event, http.StatusConflict, errorCodePageAnalyticsBatchIdentityReused, "This ID was used for a different analytics record.")
	}
	if err != nil {
		if mapped, ok := persistentWriteStorageAPIError(
			event, err, persistentPageAnalytics, "record-public",
		); ok {
			return mapped
		}
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodePageAnalyticsFailed,
			"Couldn't save the page analytics record.", "page_analytics.public",
			"transaction", errorCodePageAnalyticsFailed, err,
		)
	}
	return event.JSON(http.StatusOK, response)
}

func normalizePageAnalyticsPublicInput(input *pageAnalyticsPublicInput) bool {
	input.Action = strings.TrimSpace(input.Action)
	input.AnonymousToken = strings.TrimSpace(input.AnonymousToken)
	input.BatchID = strings.TrimSpace(input.BatchID)
	input.SiteSlug = strings.TrimSpace(input.SiteSlug)
	input.Token = strings.TrimSpace(input.Token)
	validAuthority := input.Action == pageAnalyticsActionRecordPublic &&
		pageAnalyticsIdentityPattern.MatchString(input.Token) && input.SiteSlug == ""
	if input.Action == pageAnalyticsActionRecordPublicSite {
		validAuthority = len(input.SiteSlug) <= maxPageAnalyticsSiteSlugCharacters &&
			publicSiteSlugPattern.MatchString(input.SiteSlug) && input.Token == ""
	}
	if !validAuthority || !pageAnalyticsIdentityPattern.MatchString(input.AnonymousToken) ||
		!pageAnalyticsIdentityPattern.MatchString(input.BatchID) || len(input.Observations) == 0 {
		return false
	}
	for index := range input.Observations {
		input.Observations[index].PageID = strings.TrimSpace(input.Observations[index].PageID)
		// Public batches observe pages only.
		if !pageAnalyticsIdentityPattern.MatchString(input.Observations[index].PageID) ||
			input.Observations[index].RowID != "" || input.Observations[index].ContainerID != "" {
			return false
		}
	}
	return true
}

func applyPageAnalyticsAnonymousBatch(
	app core.App,
	input pageAnalyticsPublicInput,
	authorize publicAnalyticsAuthorizer,
) (pageAnalyticsRecordResponse, pageAnalyticsRecordStatus, error) {
	pageCounts := map[string]int{}
	for _, observation := range input.Observations {
		pageCounts[observation.PageID]++
	}
	pageIDs := make([]string, 0, len(pageCounts))
	for pageID := range pageCounts {
		pageIDs = append(pageIDs, pageID)
	}
	sort.Strings(pageIDs)

	response := pageAnalyticsRecordResponse{}
	status := pageAnalyticsRecordOK
	undoViews := func() {}
	err := app.RunInTransaction(func(txApp core.App) error {
		// A visitor without an account has the server's clock (page_analytics_clock.go).
		now, viewed := pageAnalyticsNow(txApp, "")
		authority, err := authorize(txApp, pageIDs, now)
		if errors.Is(err, errPublicVisibilityNotFound) {
			status = pageAnalyticsRecordNotFound
			return nil
		} else if err != nil {
			return err
		}
		policy := pageAnalyticsPolicy(authority.Workspace)
		if !policy.AnalyticsEnabled || !policy.AnonymousTrackingEnabled {
			response = pageAnalyticsRecordResponse{BatchID: input.BatchID}
			return nil
		}

		principalID := pageAnalyticsAnonymousPrincipal(authority, input.AnonymousToken)
		requestHash, err := pageAnalyticsAnonymousRequestHash(authority, principalID, input)
		if err != nil {
			return err
		}
		batchInput := pageAnalyticsMemberRecordRequest{
			WorkspaceID: authority.WorkspaceID, BatchID: input.BatchID,
		}
		inserted, err := admitPageAnalyticsBatch(txApp, principalID, batchInput, requestHash, now)
		if err != nil {
			return err
		}
		if !inserted {
			stored, err := readPageAnalyticsBatch(txApp, authority.WorkspaceID, input.BatchID)
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

		// A visitor's opens within a minute of their last counted view of a
		// page add no view, as a member's (page_analytics_view_window.go).
		countedPageIDs, _, _, undo := pageAnalyticsViewWindowOf(app).countViews(
			authority.WorkspaceID, principalID, pageIDs, rowPageAnalyticsObservations{}, rowPageAnalyticsObservations{}, now,
		)
		undoViews = undo
		countedPages := map[string]int{}
		for _, pageID := range countedPageIDs {
			countedPages[pageID] = 1
		}
		uniquePageIDs := []string{}
		if len(countedPageIDs) > 0 {
			uniquePageIDs, err = insertPageAnalyticsUniqueAnonymous(
				txApp, authority.WorkspaceID, principalID, countedPageIDs, viewed,
			)
			if err != nil {
				return err
			}
			uniquePages := make(map[string]struct{}, len(uniquePageIDs))
			for _, pageID := range uniquePageIDs {
				uniquePages[pageID] = struct{}{}
			}
			if err := upsertPageAnalyticsDailyAnonymous(
				txApp, authority.WorkspaceID, countedPageIDs, countedPages, uniquePages, viewed,
			); err != nil {
				return err
			}
		}
		response = pageAnalyticsRecordResponse{
			Accepted: len(input.Observations), BatchID: input.BatchID,
			DailyUpdated: len(countedPageIDs), RecentUpdated: 0,
			Replayed: false, UniqueAdded: len(uniquePageIDs),
		}
		return completePageAnalyticsBatch(txApp, authority.WorkspaceID, response)
	})
	if err != nil {
		undoViews()
	}
	return response, status, err
}

func pageAnalyticsAnonymousPrincipal(
	authority publicVisibilityAuthorization,
	anonymousToken string,
) string {
	digest := sha256.Sum256([]byte(
		"page-analytics-anonymous\x00" + authority.WorkspaceID + "\x00" +
			authority.AuthorityID + "\x00" + anonymousToken,
	))
	return "anonymous:" + hex.EncodeToString(digest[:])
}

func pageAnalyticsAnonymousRequestHash(
	authority publicVisibilityAuthorization,
	principalID string,
	input pageAnalyticsPublicInput,
) (string, error) {
	encoded, err := json.Marshal(struct {
		Action       string                     `json:"action"`
		AuthorityID  string                     `json:"authorityId"`
		PrincipalID  string                     `json:"principalId"`
		Observations []pageAnalyticsObservation `json:"observations"`
	}{input.Action, authority.AuthorityID, principalID, input.Observations})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func insertPageAnalyticsUniqueAnonymous(
	app core.App,
	workspaceID string,
	viewerKey string,
	pageIDs []string,
	now time.Time,
) ([]string, error) {
	params := dbx.Params{
		"workspaceId": workspaceID, "day": pageAnalyticsDayOf(now),
		"viewerKey": viewerKey, "observedAt": pageAnalyticsTimestamp(now),
	}
	values := make([]string, 0, len(pageIDs))
	for index, pageID := range pageIDs {
		key := fmt.Sprintf("page%d", index)
		params[key] = pageID
		values = append(values, fmt.Sprintf("({:workspaceId}, {:%s}, {:day}, 'anonymous', {:viewerKey}, {:observedAt})", key))
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

func upsertPageAnalyticsDailyAnonymous(
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
		values = append(values, fmt.Sprintf("({:workspaceId}, {:%s}, {:day}, {:%s}, 0, {:%s}, {:lastViewedAt}, {:retentionAt})", pageKey, countKey, uniqueKey))
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_analytics_daily}} (
			[[workspace_id]], [[page_id]], [[day]], [[total_views]], [[unique_member_views]],
			[[unique_anonymous_views]], [[last_viewed_at]], [[retention_at]]
		) VALUES ` + joinSQL(values) + `
		ON CONFLICT ([[workspace_id]], [[page_id]], [[day]]) DO UPDATE SET
			[[total_views]] = [[total_views]] + excluded.[[total_views]],
			[[unique_anonymous_views]] = [[unique_anonymous_views]] + excluded.[[unique_anonymous_views]],
			[[last_viewed_at]] = excluded.[[last_viewed_at]],
			[[retention_at]] = excluded.[[retention_at]]
	`).Bind(params).Execute()
	return err
}
