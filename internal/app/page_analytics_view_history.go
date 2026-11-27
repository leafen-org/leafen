package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A member's "내 조회 기록 표시" for a workspace, as Notion's settings hold it
// (seen 2026-10-02): whether the workspace's pages keep the member's views by
// name, for those who may see who viewed them. A page's own opt-out stays
// with the page's analytics. Where the organization shows everyone or no
// one (view_display_policy.go), the member's choice waits unchanged.

func getPageAnalyticsViewHistory(event *core.RequestEvent, user *core.Record, input pageAnalyticsViewHistoryRequest) error {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	if input.Action != pageAnalyticsActionGetViewHistory || !pageAnalyticsIdentityPattern.MatchString(input.WorkspaceID) {
		return pageAnalyticsInvalidRequest(event)
	}
	response := pageAnalyticsViewHistoryResponse{}
	notFound := false
	err := runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		if _, err := authorizeWorkspace(txApp, user.Id, input.WorkspaceID, permissionWorkspaceRead); isPermissionDenied(err) {
			notFound = true
			return nil
		} else if err != nil {
			return err
		}
		privacy, err := readPageAnalyticsPrivacy(txApp, input.WorkspaceID, user.Id, nil)
		if err != nil {
			return err
		}
		response.IdentifiedHistoryEnabled = privacy.IdentifiedHistoryEnabled
		response.ViewDisplay = privacy.Display
		return nil
	})
	if notFound {
		return apiError(event, http.StatusNotFound, errorCodePageAnalyticsNotFound, "The workspace was not found.")
	}
	if err != nil {
		return pageAnalyticsPreferenceFailure(event, "read", err)
	}
	return event.JSON(http.StatusOK, response)
}

func updatePageAnalyticsViewHistory(
	event *core.RequestEvent,
	user *core.Record,
	input pageAnalyticsUpdateViewHistoryRequest,
) error {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	if input.Action != pageAnalyticsActionUpdateViewHistory || !pageAnalyticsIdentityPattern.MatchString(input.WorkspaceID) {
		return pageAnalyticsInvalidRequest(event)
	}
	notFound, decided := false, false
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizeWorkspace(txApp, user.Id, input.WorkspaceID, permissionWorkspaceRead); isPermissionDenied(err) {
			notFound = true
			return nil
		} else if err != nil {
			return err
		}
		var err error
		if decided, err = pageAnalyticsOrganizationDecides(txApp, input.WorkspaceID); err != nil || decided {
			return err
		}
		return savePageAnalyticsIdentifiedHistory(txApp, input.WorkspaceID, user.Id, input.IdentifiedHistoryEnabled)
	})
	if notFound {
		return apiError(event, http.StatusNotFound, errorCodePageAnalyticsNotFound, "The workspace was not found.")
	}
	if err == nil && decided {
		return pageAnalyticsOrganizationDecidesError(event)
	}
	if err != nil {
		return pageAnalyticsPreferenceFailure(event, "update", err)
	}
	return event.JSON(http.StatusOK, pageAnalyticsViewHistoryResponse{IdentifiedHistoryEnabled: input.IdentifiedHistoryEnabled, ViewDisplay: viewDisplayPerson})
}
