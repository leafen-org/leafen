package app

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const maxPageAnalyticsRequestBytes = 64 << 10

func registerPageAnalyticsRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/page-analytics", dispatchPageAnalyticsRequest).
		Bind(apis.BodyLimit(maxPageAnalyticsRequestBytes))
	event.Router.POST(apiRoot+"/page-past-viewers", readPagePastViewers).
		Bind(apis.BodyLimit(maxPageAnalyticsRequestBytes))
	event.Router.POST(apiRoot+"/page-left", recordPageLeft).
		Bind(apis.BodyLimit(maxPageAnalyticsRequestBytes))
}

func dispatchPageAnalyticsRequest(event *core.RequestEvent) error {
	body, err := io.ReadAll(event.Request.Body)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePageAnalyticsInvalid, "Check the analytics request.")
	}
	var envelope pageAnalyticsActionEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePageAnalyticsInvalid, "Check the analytics request.")
	}

	switch envelope.Action {
	case pageAnalyticsActionRecordPublic:
		var input pageAnalyticsPublicRecordRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return pageAnalyticsInvalidRequest(event)
		}
		return recordPageAnalyticsPublic(event, input)
	case pageAnalyticsActionRecordPublicSite:
		var input pageAnalyticsPublicSiteRecordRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return pageAnalyticsInvalidRequest(event)
		}
		return recordPageAnalyticsPublicSite(event, input)
	}

	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	switch envelope.Action {
	case pageAnalyticsActionRecord:
		var input pageAnalyticsMemberRecordRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return pageAnalyticsInvalidRequest(event)
		}
		return recordPageAnalyticsMember(event, user, input)
	case pageAnalyticsActionGet:
		var input pageAnalyticsReadRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return pageAnalyticsInvalidRequest(event)
		}
		input.PageID, input.RowID = rowPageAnalyticsTarget(event.App, input.PageID, input.RowID, input.ContainerID)
		return readPageAnalytics(event, user, input)
	case pageAnalyticsActionGetRowViews:
		var input pageAnalyticsRowViewsRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return pageAnalyticsInvalidRequest(event)
		}
		return readRowPageAnalyticsViews(event, user, input)
	case pageAnalyticsActionGetPreferences:
		var input pageAnalyticsPreferencesRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return pageAnalyticsInvalidRequest(event)
		}
		input.PageID, input.RowID = rowPageAnalyticsTarget(event.App, input.PageID, input.RowID, input.ContainerID)
		return getPageAnalyticsPreferences(event, user, input)
	case pageAnalyticsActionUpdatePreferences:
		var input pageAnalyticsUpdatePreferencesRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return pageAnalyticsInvalidRequest(event)
		}
		input.PageID, input.RowID = rowPageAnalyticsTarget(event.App, input.PageID, input.RowID, input.ContainerID)
		return updatePageAnalyticsPreferences(event, user, input)
	case pageAnalyticsActionUpdatePolicy:
		var input pageAnalyticsUpdatePolicyRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return pageAnalyticsInvalidRequest(event)
		}
		return updatePageAnalyticsPolicy(event, user, input)
	case pageAnalyticsActionGetViewHistory:
		var input pageAnalyticsViewHistoryRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return pageAnalyticsInvalidRequest(event)
		}
		return getPageAnalyticsViewHistory(event, user, input)
	case pageAnalyticsActionUpdateViewHistory:
		var input pageAnalyticsUpdateViewHistoryRequest
		if err := json.Unmarshal(body, &input); err != nil {
			return pageAnalyticsInvalidRequest(event)
		}
		return updatePageAnalyticsViewHistory(event, user, input)
	default:
		return pageAnalyticsInvalidRequest(event)
	}
}

// rowPageAnalyticsTarget reads a database row's page given alone as the page
// to analyze as that row on the page that holds it, as the app asks for it:
// an agent gave the row's page ID, which every other page path takes, and was
// told the page wasn't found (2026-11-22). Any other page, and a request
// naming its row or its database, is left as it is.
func rowPageAnalyticsTarget(app core.App, pageID, rowID, containerID string) (string, string) {
	pageID, rowID = strings.TrimSpace(pageID), strings.TrimSpace(rowID)
	if pageID == "" || rowID != "" || strings.TrimSpace(containerID) != "" {
		return pageID, rowID
	}
	holder := ""
	if err := app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}
		UNION ALL
		SELECT [[page_id]] FROM {{leafen_database_templates}} WHERE [[id]] = {:id}
		LIMIT 1
	`).Bind(dbx.Params{"id": pageID}).Row(&holder); err != nil || holder == "" {
		return pageID, rowID
	}
	return holder, pageID
}

func pageAnalyticsInvalidRequest(event *core.RequestEvent) error {
	return apiError(event, http.StatusBadRequest, errorCodePageAnalyticsInvalid, "Check the analytics request.")
}
