package app

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type pageAnalyticsWorkspacePolicy struct {
	AnalyticsEnabled         bool
	AnonymousTrackingEnabled bool
}

type pageAnalyticsPrivacyState struct {
	IdentifiedHistoryEnabled bool
	OptedOutPages            map[string]struct{}
	// Display is whom the organization lets page analytics name
	// (view_display_policy.go); where it shows everyone, no one hides.
	Display string
}

type storedPageAnalyticsPrivacy struct {
	Kind    string `db:"kind"`
	PageID  string `db:"page_id"`
	Enabled int    `db:"enabled"`
}

func pageAnalyticsPolicy(workspace *core.Record) pageAnalyticsWorkspacePolicy {
	return pageAnalyticsWorkspacePolicy{
		AnalyticsEnabled:         !workspace.GetBool(pageAnalyticsDisabledField),
		AnonymousTrackingEnabled: !workspace.GetBool(pageAnalyticsAnonymousDisabledField),
	}
}

func readPageAnalyticsPrivacy(
	app core.App,
	workspaceID string,
	principalID string,
	pageIDs []string,
) (pageAnalyticsPrivacyState, error) {
	params := dbx.Params{"workspaceId": workspaceID, "userId": principalID}
	placeholders := make([]string, 0, len(pageIDs))
	for index, pageID := range pageIDs {
		key := fmt.Sprintf("privacyPage%d", index)
		params[key] = pageID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []storedPageAnalyticsPrivacy{}
	err := app.DB().NewQuery(`
		SELECT 'preference' AS [[kind]], '' AS [[page_id]], [[identified_history_enabled]] AS [[enabled]]
		FROM {{leafen_page_analytics_member_preferences}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}
		UNION ALL
		SELECT 'opt_out' AS [[kind]], [[page_id]], 0 AS [[enabled]]
		FROM {{leafen_page_analytics_page_opt_outs}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}
			AND [[page_id]] IN (` + joinSQL(placeholders) + `)
	`).Bind(params).All(&rows)
	if err != nil {
		return pageAnalyticsPrivacyState{}, err
	}
	state := pageAnalyticsPrivacyState{
		IdentifiedHistoryEnabled: true,
		OptedOutPages:            map[string]struct{}{},
	}
	preferenceRows := 0
	for _, row := range rows {
		switch row.Kind {
		case "preference":
			preferenceRows++
			state.IdentifiedHistoryEnabled = row.Enabled == 1
		case "opt_out":
			state.OptedOutPages[row.PageID] = struct{}{}
		default:
			return pageAnalyticsPrivacyState{}, errors.New("page analytics privacy row is invalid")
		}
	}
	if preferenceRows > 1 || len(state.OptedOutPages) > len(pageIDs) {
		return pageAnalyticsPrivacyState{}, errors.New("page analytics privacy identity is ambiguous")
	}
	if state.Display, err = workspaceViewDisplay(app, workspaceID); err != nil {
		return pageAnalyticsPrivacyState{}, err
	}
	if state.Display == viewDisplayShown {
		state.IdentifiedHistoryEnabled = true
		state.OptedOutPages = map[string]struct{}{}
	}
	return state, nil
}

func pageAnalyticsIdentifiedPages(
	app core.App,
	workspaceID string,
	principalID string,
	pageIDs []string,
) ([]string, error) {
	identified, _, err := pageAnalyticsViewerPages(app, workspaceID, principalID, pageIDs)
	return identified, err
}

// pageAnalyticsViewerPages splits a viewer's pages into those whose view names
// them wherever views are named and those that keep their line in who viewed.
// The account's "내 조회 기록 표시" off only hides that line from others: it
// goes on moving, and others see its latest time once the setting is back on,
// as in Notion (seen 2026-11-05). A page's own choice keeps the view out.
func pageAnalyticsViewerPages(
	app core.App,
	workspaceID string,
	principalID string,
	pageIDs []string,
) (identified []string, kept []string, err error) {
	privacy, err := readPageAnalyticsPrivacy(app, workspaceID, principalID, pageIDs)
	if err != nil {
		return nil, nil, err
	}
	kept = make([]string, 0, len(pageIDs))
	for _, pageID := range pageIDs {
		if _, excluded := privacy.OptedOutPages[pageID]; !excluded {
			kept = append(kept, pageID)
		}
	}
	if !privacy.IdentifiedHistoryEnabled {
		return []string{}, kept, nil
	}
	return kept, kept, nil
}

// pageAnalyticsPreferenceTargetValid says a choice names at most one of a
// row and a database on its page, each a valid ID.
func pageAnalyticsPreferenceTargetValid(rowID, containerID string) bool {
	return (rowID == "" || pageAnalyticsIdentityPattern.MatchString(rowID)) &&
		(containerID == "" || (rowID == "" && pageAnalyticsIdentityPattern.MatchString(containerID)))
}

// pageAnalyticsPreferenceTarget is what a "내 조회 기록 표시" choice is kept
// for: the page, or a database row on it, as Notion's row pages have their
// own (seen 2026-10-30) — kept for the row's page record, of its ID — or an
// inline database on it, which has its own too (seen 2026-11-11), kept by
// its container (database_analytics.go). A row or database must be on the
// page named.
func pageAnalyticsPreferenceTarget(app core.App, workspaceID, pageID, rowID, containerID string) (string, bool, error) {
	if containerID != "" {
		held, err := databaseAnalyticsHeld(app, workspaceID, pageID, containerID)
		return pageID, held, err
	}
	if rowID == "" {
		return pageID, true, nil
	}
	held, err := rowPageAnalyticsRowsHeld(app, workspaceID, rowPageAnalyticsObservations{
		IDs: []string{rowID}, Pages: map[string]string{rowID: pageID},
	})
	return rowID, held, err
}

func getPageAnalyticsPreferences(
	event *core.RequestEvent,
	user *core.Record,
	input pageAnalyticsPreferencesRequest,
) error {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.PageID = strings.TrimSpace(input.PageID)
	input.RowID = strings.TrimSpace(input.RowID)
	input.ContainerID = strings.TrimSpace(input.ContainerID)
	if input.Action != pageAnalyticsActionGetPreferences ||
		!pageAnalyticsIdentityPattern.MatchString(input.WorkspaceID) ||
		!pageAnalyticsIdentityPattern.MatchString(input.PageID) ||
		!pageAnalyticsPreferenceTargetValid(input.RowID, input.ContainerID) {
		return pageAnalyticsInvalidRequest(event)
	}
	response := pageAnalyticsPreferencesResponse{}
	notFound := false
	err := runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		authorization, err := authorizePageSet(
			txApp, user.Id, input.WorkspaceID, []string{input.PageID}, permissionWorkspaceRead,
		)
		if isPermissionDenied(err) {
			notFound = true
			return nil
		}
		if err != nil {
			return err
		}
		target, held, err := pageAnalyticsPreferenceTarget(txApp, input.WorkspaceID, input.PageID, input.RowID, input.ContainerID)
		if err != nil {
			return err
		}
		if !held {
			notFound = true
			return nil
		}
		privacy, err := readPageAnalyticsPrivacy(txApp, input.WorkspaceID, user.Id, []string{target})
		if err != nil {
			return err
		}
		_, optedOut := privacy.OptedOutPages[target]
		if input.ContainerID != "" {
			databasesOptedOut, err := databaseAnalyticsOptedOut(txApp, input.WorkspaceID, user.Id, []string{input.ContainerID})
			if err != nil {
				return err
			}
			_, optedOut = databasesOptedOut[input.ContainerID]
		}
		policy := pageAnalyticsPolicy(authorization.Workspace)
		response = pageAnalyticsPreferencesResponse{
			AnalyticsEnabled:             policy.AnalyticsEnabled,
			AnonymousTrackingEnabled:     policy.AnonymousTrackingEnabled,
			IdentifiedHistoryEnabled:     privacy.IdentifiedHistoryEnabled,
			PageIdentifiedHistoryEnabled: !optedOut,
			CanManagePolicy:              authorization.WorkspaceOwnerID == user.Id,
			ViewDisplay:                  privacy.Display,
		}
		return nil
	})
	if notFound {
		return apiError(event, http.StatusNotFound, errorCodePageAnalyticsNotFound, "The page to analyze was not found.")
	}
	if err != nil {
		return pageAnalyticsPreferenceFailure(event, "read", err)
	}
	return event.JSON(http.StatusOK, response)
}

func updatePageAnalyticsPreferences(
	event *core.RequestEvent,
	user *core.Record,
	input pageAnalyticsUpdatePreferencesRequest,
) error {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.PageID = strings.TrimSpace(input.PageID)
	input.RowID = strings.TrimSpace(input.RowID)
	input.ContainerID = strings.TrimSpace(input.ContainerID)
	if input.Action != pageAnalyticsActionUpdatePreferences ||
		!pageAnalyticsIdentityPattern.MatchString(input.WorkspaceID) ||
		!pageAnalyticsIdentityPattern.MatchString(input.PageID) ||
		!pageAnalyticsPreferenceTargetValid(input.RowID, input.ContainerID) {
		return pageAnalyticsInvalidRequest(event)
	}
	notFound, decided := false, false
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizePageSet(
			txApp, user.Id, input.WorkspaceID, []string{input.PageID}, permissionWorkspaceRead,
		); isPermissionDenied(err) {
			notFound = true
			return nil
		} else if err != nil {
			return err
		}
		target, held, err := pageAnalyticsPreferenceTarget(txApp, input.WorkspaceID, input.PageID, input.RowID, input.ContainerID)
		if err != nil {
			return err
		}
		if !held {
			notFound = true
			return nil
		}
		if decided, err = pageAnalyticsOrganizationDecides(txApp, input.WorkspaceID); err != nil || decided {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if err := savePageAnalyticsIdentifiedHistory(txApp, input.WorkspaceID, user.Id, input.IdentifiedHistoryEnabled); err != nil {
			return err
		}
		if input.ContainerID != "" {
			return saveDatabaseAnalyticsOptOut(
				txApp, input.WorkspaceID, input.PageID, input.ContainerID, user.Id, input.PageIdentifiedHistoryEnabled,
			)
		}
		if input.PageIdentifiedHistoryEnabled {
			_, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_page_analytics_page_opt_outs}}
				WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId} AND [[page_id]] = {:pageId}
			`).Bind(dbx.Params{
				"workspaceId": input.WorkspaceID, "userId": user.Id, "pageId": target,
			}).Execute()
			return err
		}
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_page_analytics_page_opt_outs}} (
				[[workspace_id]], [[page_id]], [[user_id]], [[created_at]]
			) VALUES ({:workspaceId}, {:pageId}, {:userId}, {:createdAt})
			ON CONFLICT ([[workspace_id]], [[user_id]], [[page_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"workspaceId": input.WorkspaceID, "pageId": target,
			"userId": user.Id, "createdAt": now,
		}).Execute()
		return err
	})
	if notFound {
		return apiError(event, http.StatusNotFound, errorCodePageAnalyticsNotFound, "The page to analyze was not found.")
	}
	if err == nil && decided {
		return pageAnalyticsOrganizationDecidesError(event)
	}
	if err != nil {
		return pageAnalyticsPreferenceFailure(event, "update", err)
	}
	return event.JSON(http.StatusOK, pageAnalyticsUpdatePreferencesResponse{
		IdentifiedHistoryEnabled:     input.IdentifiedHistoryEnabled,
		PageIdentifiedHistoryEnabled: input.PageIdentifiedHistoryEnabled,
	})
}

func updatePageAnalyticsPolicy(
	event *core.RequestEvent,
	user *core.Record,
	input pageAnalyticsUpdatePolicyRequest,
) error {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	if input.Action != pageAnalyticsActionUpdatePolicy ||
		!pageAnalyticsIdentityPattern.MatchString(input.WorkspaceID) {
		return pageAnalyticsInvalidRequest(event)
	}
	forbidden := false
	err := event.App.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, user.Id, input.WorkspaceID, permissionWorkspaceOwner)
		if isPermissionDenied(err) {
			forbidden = true
			return nil
		}
		if err != nil {
			return err
		}
		authorization.Workspace.Set(pageAnalyticsDisabledField, !input.AnalyticsEnabled)
		authorization.Workspace.Set(pageAnalyticsAnonymousDisabledField, !input.AnonymousTrackingEnabled)
		return txApp.Save(authorization.Workspace)
	})
	if forbidden {
		return apiError(event, http.StatusForbidden, errorCodePageAnalyticsForbidden, "You don't have permission to change the analytics policy.")
	}
	if err != nil {
		return pageAnalyticsPreferenceFailure(event, "policy", err)
	}
	return event.JSON(http.StatusOK, pageAnalyticsUpdatePolicyResponse{
		AnalyticsEnabled:         input.AnalyticsEnabled,
		AnonymousTrackingEnabled: input.AnonymousTrackingEnabled,
	})
}

func pageAnalyticsPreferenceFailure(event *core.RequestEvent, stage string, cause error) error {
	if mapped, ok := persistentWriteStorageAPIError(
		event, cause, persistentPageAnalytics, stage,
	); ok {
		return mapped
	}
	return apiErrorWithCause(
		event, http.StatusInternalServerError, errorCodePageAnalyticsFailed,
		"Couldn't process the analytics settings.", "page_analytics.preferences", stage,
		errorCodePageAnalyticsFailed, cause,
	)
}

// pageAnalyticsOrganizationDecides says the workspace's organization shows
// everyone or no one, which leaves a member's own choice nothing to change.
func pageAnalyticsOrganizationDecides(app core.App, workspaceID string) (bool, error) {
	display, err := workspaceViewDisplay(app, workspaceID)
	return display != viewDisplayPerson, err
}

func pageAnalyticsOrganizationDecidesError(event *core.RequestEvent) error {
	return apiError(event, http.StatusConflict, errorCodePageAnalyticsOrganizationDecides,
		"The organization decides whom page analytics shows as having viewed.")
}

// savePageAnalyticsIdentifiedHistory records whether a member's views of a
// workspace's pages are kept by name.
func savePageAnalyticsIdentifiedHistory(app core.App, workspaceID, userID string, enabled bool) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_analytics_member_preferences}} (
			[[workspace_id]], [[user_id]], [[identified_history_enabled]], [[updated_at]]
		) VALUES ({:workspaceId}, {:userId}, {:enabled}, {:updatedAt})
		ON CONFLICT ([[workspace_id]], [[user_id]]) DO UPDATE SET
			[[identified_history_enabled]] = excluded.[[identified_history_enabled]],
			[[updated_at]] = excluded.[[updated_at]]
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "userId": userID,
		"enabled": boolInt(enabled), "updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	}).Execute()
	return err
}
