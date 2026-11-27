package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's Admin API credit usage per person, read with user:read:
// GET /v1/spaces/{space_id}/users/credit_usage lists the workspace's current
// members, as its people list does, each with the credits used in the
// current billing period and all told and the limit its credit access gives;
// GET /v1/spaces/{space_id}/users/{user_id}/credit_usage lists one member's
// billing periods. Leafen runs no AI, so nobody uses credits: each
// member has used none, in the one period there is, the calendar month (UTC)
// the request is in.

const (
	notionAdminUserCreditUsageObject       = "user_credit_usage"
	notionAdminUserCreditUsagePeriodObject = "user_credit_usage_period"
)

var notionAdminUserCreditUsageCursorKind = notionAdminMemberCursorKind{
	prefix: "hnauc1", domain: "leafen:notion-admin-user-credit-usage-cursor:v1",
}

type notionAdminCreditsByType struct {
	NotionCredits   int64  `json:"notion_credits"`
	SelectCredits   *int64 `json:"select_credits"`
	StandardCredits *int64 `json:"standard_credits"`
}

type notionAdminUserCreditUsagePeriod struct {
	MetricsPeriodStartMS int64                    `json:"metrics_period_start_ms"`
	MetricsPeriodEndMS   int64                    `json:"metrics_period_end_ms"`
	CreditsUsed          int64                    `json:"credits_used"`
	CreditsUsedByType    notionAdminCreditsByType `json:"credits_used_by_type"`
}

type notionAdminUserCreditUsage struct {
	Object           string `json:"object"`
	ID               string `json:"id"`
	CreditLimit      *int64 `json:"credit_limit"`
	TotalCreditsUsed int64  `json:"total_credits_used"`
	notionAdminUserCreditUsagePeriod
}

// notionAdminUserCreditUsageRoute reads .../users/credit_usage (no user) and
// .../users/{user_id}/credit_usage.
func notionAdminUserCreditUsageRoute(path string) (string, string, bool) {
	value, found := strings.CutPrefix(path, notionAdminAPISpacesPath+"/")
	parts := strings.Split(value, "/")
	if !found || parts[0] == "" || len(parts) < 3 || parts[1] != "users" || parts[len(parts)-1] != "credit_usage" {
		return "", "", false
	}
	if len(parts) == 3 {
		return parts[0], "", true
	}
	if len(parts) == 4 && parts[2] != "" {
		return parts[0], parts[2], true
	}
	return "", "", false
}

func serveNotionAdminUserCreditUsage(event *core.RequestEvent, rawWorkspaceID, rawUserID string) error {
	secret := bearerToken(event.Request)
	release := acquireNotionAdminTokenLifecycleLock(notionAdminTokenHash(secret))
	defer release()
	identity, authorized := authorizeNotionAdminPublicAPI(event, secret, notionAdminCapabilityUserRead)
	if !authorized {
		return nil
	}
	if event.Request.ContentLength > 0 {
		return notionAdminSpaceUsersValidationError(event, notionAdminInvalid(nil, "This request takes no body."))
	}
	workspaceID, err := resolveNotionCompatibleWorkspaceID(strings.TrimSpace(rawWorkspaceID))
	if err != nil {
		return notionAdminSpaceUsersValidationError(event, notionAdminInvalid(err, "The workspace ID should be a UUID."))
	}
	if !identity.CanAccessWorkspace(workspaceID) {
		return notionAdminSpaceUsersNotFoundError(event, nil)
	}
	pageSize, rawCursor, err := notionAdminSpaceUsersQuery(event.Request)
	if err != nil {
		return notionAdminSpaceUsersValidationError(event, err)
	}
	period := notionAdminCurrentCreditPeriod(time.Now().UTC())
	event.Response.Header().Set("Cache-Control", "no-store")
	if rawUserID != "" {
		userID, err := resolveNotionCompatibleUserID(strings.TrimSpace(rawUserID))
		if err != nil {
			return notionAdminSpaceUsersValidationError(event, notionAdminInvalid(err, "The user ID isn't a user ID."))
		}
		// One period only: a cursor is never one this list gave.
		if rawCursor != "" {
			return notionAdminSpaceUsersValidationError(event, errInvalidNotionAdminSpaceUsersCursor)
		}
		member, err := isNotionAdminWorkspaceMember(event.Request.Context(), event.App, identity, workspaceID, userID)
		if err != nil {
			return notionAdminPublicAPIError(event, http.StatusInternalServerError, notionAdminErrorInternal, notionAdminInternalErrorMessage, err)
		}
		if !member {
			return notionAdminSpaceUsersNotFoundError(event, nil)
		}
		return event.JSON(http.StatusOK, map[string]any{
			"object": "list", "type": notionAdminUserCreditUsagePeriodObject,
			"results": []notionAdminUserCreditUsagePeriod{period}, "has_more": false, "next_cursor": nil,
		})
	}
	cursor, err := decodeNotionAdminSpaceUsersCursor(notionAdminUserCreditUsageCursorKind, rawCursor, secret, identity, workspaceID)
	if err != nil {
		return notionAdminSpaceUsersValidationError(event, err)
	}
	ctx := context.WithValue(event.Request.Context(), requestOriginContextKey{}, requestOrigin(event.Request))
	page, err := listNotionAdminSpaceUsers(ctx, event.App, identity, workspaceID, cursor, pageSize)
	if errors.Is(err, errInvalidNotionAdminSpaceUsersCursor) {
		return notionAdminSpaceUsersValidationError(event, err)
	}
	if errors.Is(err, errNotionAdminSpaceUsersNotFound) {
		return notionAdminSpaceUsersNotFoundError(event, err)
	}
	var limits map[string]*int64
	if err == nil {
		limits, err = notionCreditAccessMemberLimits(ctx, event.App, workspaceID, page.InternalUserIDs)
	}
	if err != nil {
		return notionAdminPublicAPIError(event, http.StatusInternalServerError, notionAdminErrorInternal, notionAdminInternalErrorMessage, err)
	}
	results := make([]notionAdminUserCreditUsage, 0, len(page.Users))
	for index, user := range page.Users {
		publicID, _ := user["id"].(string)
		results = append(results, notionAdminUserCreditUsage{
			Object: notionAdminUserCreditUsageObject, ID: publicID,
			CreditLimit: limits[page.InternalUserIDs[index]], notionAdminUserCreditUsagePeriod: period,
		})
	}
	var nextCursor *string
	if page.HasMore {
		encoded, err := encodeNotionAdminSpaceUsersCursor(
			notionAdminUserCreditUsageCursorKind, secret, identity, workspaceID, page.PermissionRevision,
			page.InternalUserIDs[len(page.InternalUserIDs)-1],
		)
		if err != nil {
			return notionAdminPublicAPIError(event, http.StatusInternalServerError, notionAdminErrorInternal, notionAdminInternalErrorMessage, err)
		}
		nextCursor = &encoded
	}
	return event.JSON(http.StatusOK, map[string]any{
		"object": "list", "type": notionAdminUserCreditUsageObject, "results": results,
		"has_more": page.HasMore, "next_cursor": nextCursor,
	})
}

// notionAdminCurrentCreditPeriod is the month now is in, with no credits used.
func notionAdminCurrentCreditPeriod(now time.Time) notionAdminUserCreditUsagePeriod {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return notionAdminUserCreditUsagePeriod{
		MetricsPeriodStartMS: start.UnixMilli(), MetricsPeriodEndMS: start.AddDate(0, 1, 0).UnixMilli(),
	}
}

// isNotionAdminWorkspaceMember says whether a person is a current member of
// a workspace of the token's organization, as the people list counts them.
func isNotionAdminWorkspaceMember(ctx context.Context, app core.App, identity notionAdminIdentity, workspaceID, userID string) (bool, error) {
	var member int
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_workspaces}} [[workspace]]
		WHERE [[workspace]].[[id]] = {:workspaceId} AND [[workspace]].[[organization]] = {:organizationId}
			AND ([[workspace]].[[workspace_owner]] = {:userId} OR EXISTS(
				SELECT 1 FROM {{leafen_workspace_memberships}} [[membership]]
				WHERE [[membership]].[[workspace_id]] = [[workspace]].[[id]]
					AND [[membership]].[[user_id]] = {:userId} AND [[membership]].[[role]] = 'member')))
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "organizationId": identity.OrganizationID, "userId": userID,
	}).Row(&member)
	return member == 1, err
}

// notionCreditAccessMemberLimits is the credit limit each member's credit
// access gives, in Notion's order: a user assignment wins over every other;
// otherwise the highest limit among the member's permission group and SCIM
// attribute assignments (unlimited highest, no_access lowest) — Leafen holds
// no SCIM attributes, so those cover nobody; otherwise the all_space_members
// one. no_access is 0, limited its credits, and unlimited, or no assignment
// at all, no limit (nil).
func notionCreditAccessMemberLimits(ctx context.Context, app core.App, workspaceID string, userIDs []string) (map[string]*int64, error) {
	limits := map[string]*int64{}
	if len(userIDs) == 0 {
		return limits, nil
	}
	encoded, err := json.Marshal(userIDs)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		UserID  string `db:"user_id"`
		Kind    string `db:"limit_kind"`
		Credits *int64 `db:"limit_credits"`
	}{}
	err = app.DB().NewQuery(`
		WITH [[member]] AS (SELECT [[value]] AS [[user_id]] FROM json_each({:users})),
		[[covering]] AS (
			SELECT [[member]].[[user_id]], [[policy]].[[limit_kind]], [[policy]].[[limit_credits]],
				CASE [[assignment]].[[selector_type]] WHEN 'user' THEN 0 WHEN 'all_space_members' THEN 2 ELSE 1 END AS [[tier]],
				CASE [[policy]].[[limit_kind]] WHEN 'unlimited' THEN 2 WHEN 'limited' THEN 1 ELSE 0 END AS [[height]]
			FROM [[member]]
			JOIN {{leafen_notion_credit_access_assignments}} [[assignment]]
				ON [[assignment]].[[workspace_id]] = {:workspaceId} AND (
					[[assignment]].[[selector_type]] = 'all_space_members'
					OR [[assignment]].[[selector_type]] = 'user' AND [[assignment]].[[user_id]] = [[member]].[[user_id]]
					OR [[assignment]].[[selector_type]] = 'space_permission_group' AND EXISTS(
						SELECT 1 FROM {{leafen_workspace_permission_group_members}} [[group_member]]
						WHERE [[group_member]].[[group_id]] = [[assignment]].[[group_id]]
							AND [[group_member]].[[user_id]] = [[member]].[[user_id]]))
			JOIN {{leafen_notion_credit_access_policies}} [[policy]] ON [[policy]].[[id]] = [[assignment]].[[policy_id]]
		), [[ranked]] AS (
			SELECT [[user_id]], [[limit_kind]], [[limit_credits]], ROW_NUMBER() OVER (
				PARTITION BY [[user_id]] ORDER BY [[tier]], [[height]] DESC, [[limit_credits]] DESC
			) AS [[rank]] FROM [[covering]]
		)
		SELECT [[user_id]], [[limit_kind]], [[limit_credits]] FROM [[ranked]] WHERE [[rank]] = 1
	`).WithContext(ctx).Bind(dbx.Params{"users": string(encoded), "workspaceId": workspaceID}).All(&rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		switch row.Kind {
		case notionCreditAccessNoAccess:
			none := int64(0)
			limits[row.UserID] = &none
		case notionCreditAccessLimited:
			if row.Credits == nil {
				return nil, fmt.Errorf("credit access policy limited without credits for %s", strconv.Quote(row.UserID))
			}
			limits[row.UserID] = row.Credits
		}
	}
	return limits, nil
}
