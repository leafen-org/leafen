package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Reading credit access policies and assignments. A list's next_cursor
// holds the workspace's credit access revision: a page asked for after a
// policy or an assignment there changed is refused, as the other Admin lists
// refuse theirs, rather than skipping or repeating rows.

const (
	notionCreditAccessDefaultPageSize = 100
	notionCreditAccessMaximumPageSize = 100
	notionCreditAccessCursorVersion   = 1
	notionCreditAccessCursorPrefix    = "hnaca1"
	notionCreditAccessCursorDomain    = "leafen:notion-admin-credit-access-cursor:v1"
	notionCreditAccessCursorMaxLength = 2048
	notionCreditAccessPolicyColumns   = `[[id]], [[workspace_id]], [[name]], [[limit_kind]], [[limit_credits]], [[version]]`
	notionCreditAccessAssignmentRow   = `[[id]], [[workspace_id]], [[policy_id]], [[selector_type]],
		ifnull([[user_id]], '') AS [[user_id]], ifnull([[group_id]], '') AS [[group_id]],
		ifnull([[scim_attribute]], '') AS [[scim_attribute]], ifnull([[scim_value]], '') AS [[scim_value]], [[version]]`
)

var errInvalidNotionCreditAccessCursor = notionAdminInvalid(errNotionCreditAccessInvalid, "start_cursor isn't one this list gave, or the list changed since.")

type notionCreditAccessCursor struct {
	Version        int    `json:"v"`
	OrganizationID string `json:"o"`
	TokenID        string `json:"t"`
	WorkspaceID    string `json:"w"`
	PolicyID       string `json:"p,omitempty"`
	Kind           string `json:"k"`
	Revision       int64  `json:"r"`
	AfterID        string `json:"a"`
}

type storedNotionCreditAccessPolicy struct {
	ID           string        `db:"id"`
	WorkspaceID  string        `db:"workspace_id"`
	Name         string        `db:"name"`
	LimitKind    string        `db:"limit_kind"`
	LimitCredits sql.NullInt64 `db:"limit_credits"`
	Version      int64         `db:"version"`
}

type storedNotionCreditAccessAssignment struct {
	ID           string `db:"id"`
	WorkspaceID  string `db:"workspace_id"`
	PolicyID     string `db:"policy_id"`
	SelectorType string `db:"selector_type"`
	UserID       string `db:"user_id"`
	GroupID      string `db:"group_id"`
	Attribute    string `db:"scim_attribute"`
	Value        string `db:"scim_value"`
	Version      int64  `db:"version"`
}

func notionCreditAccessListQuery(request *http.Request) (int, string, error) {
	query := request.URL.Query()
	for key, values := range query {
		if key != "page_size" && key != "start_cursor" || len(values) != 1 {
			return 0, "", notionAdminInvalid(errNotionCreditAccessInvalid, "This list takes the query page_size and start_cursor, each once.")
		}
	}
	pageSize := notionCreditAccessDefaultPageSize
	if raw := query.Get("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > notionCreditAccessMaximumPageSize {
			return 0, "", notionAdminInvalid(errNotionCreditAccessInvalid, "page_size should be a whole number from 1 to 100.")
		}
		pageSize = parsed
	}
	return pageSize, query.Get("start_cursor"), nil
}

func decodeNotionCreditAccessCursor(
	raw, secret string,
	identity notionAdminIdentity,
	workspaceID, policyID, kind string,
) (notionCreditAccessCursor, error) {
	if raw == "" {
		return notionCreditAccessCursor{}, nil
	}
	cursor := notionCreditAccessCursor{}
	if err := decodeNotionAdminLegalHoldScopeCursor(
		raw, secret, notionCreditAccessCursorPrefix, notionCreditAccessCursorDomain,
		notionCreditAccessCursorMaxLength, &cursor,
	); err != nil || cursor.Version != notionCreditAccessCursorVersion ||
		cursor.OrganizationID != identity.OrganizationID || cursor.TokenID != identity.TokenID ||
		cursor.WorkspaceID != workspaceID || cursor.PolicyID != policyID || cursor.Kind != kind ||
		cursor.Revision < 1 || !validNotionCompatibleInternalID(cursor.AfterID) {
		return notionCreditAccessCursor{}, errInvalidNotionCreditAccessCursor
	}
	return cursor, nil
}

func encodeNotionCreditAccessCursor(secret string, identity notionAdminIdentity, cursor notionCreditAccessCursor) (*string, error) {
	cursor.Version, cursor.OrganizationID, cursor.TokenID = notionCreditAccessCursorVersion, identity.OrganizationID, identity.TokenID
	encoded, err := encodeNotionAdminLegalHoldScopeCursor(
		secret, notionCreditAccessCursorPrefix, notionCreditAccessCursorDomain,
		notionCreditAccessCursorMaxLength, cursor,
	)
	return &encoded, err
}

func listNotionCreditAccessPolicies(
	ctx context.Context,
	app core.App,
	secret string,
	identity notionAdminIdentity,
	workspaceID string,
	cursor notionCreditAccessCursor,
	pageSize int,
) (notionCreditAccessListResponse[notionCreditAccessPolicyResponse], error) {
	response := notionCreditAccessListResponse[notionCreditAccessPolicyResponse]{
		Object: "list", Type: notionCreditAccessPolicyObject, Results: []notionCreditAccessPolicyResponse{},
	}
	next := notionCreditAccessCursor{WorkspaceID: workspaceID, Kind: notionCreditAccessPolicyObject}
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		revision, err := loadNotionCreditAccessRevision(ctx, txApp, identity.OrganizationID, workspaceID)
		if err != nil {
			return err
		}
		if cursor.Version != 0 && cursor.Revision != revision {
			return errInvalidNotionCreditAccessCursor
		}
		rows := []storedNotionCreditAccessPolicy{}
		if err := txApp.DB().NewQuery(`
			SELECT ` + notionCreditAccessPolicyColumns + ` FROM {{leafen_notion_credit_access_policies}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[id]] > {:after}
			ORDER BY [[id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"workspaceId": workspaceID, "after": cursor.AfterID, "limit": pageSize + 1,
		}).All(&rows); err != nil {
			return err
		}
		response.HasMore = len(rows) > pageSize
		if response.HasMore {
			rows = rows[:pageSize]
			next.Revision, next.AfterID = revision, rows[len(rows)-1].ID
		}
		for _, row := range rows {
			projected, err := projectNotionCreditAccessPolicy(row)
			if err != nil {
				return err
			}
			response.Results = append(response.Results, projected)
		}
		return nil
	})
	if err != nil || !response.HasMore {
		return response, err
	}
	response.NextCursor, err = encodeNotionCreditAccessCursor(secret, identity, next)
	return response, err
}

func listNotionCreditAccessAssignments(
	ctx context.Context,
	app core.App,
	secret string,
	identity notionAdminIdentity,
	workspaceID, policyID string,
	cursor notionCreditAccessCursor,
	pageSize int,
) (notionCreditAccessListResponse[notionCreditAccessAssignmentResponse], error) {
	response := notionCreditAccessListResponse[notionCreditAccessAssignmentResponse]{
		Object: "list", Type: notionCreditAccessAssignmentObject, Results: []notionCreditAccessAssignmentResponse{},
	}
	next := notionCreditAccessCursor{WorkspaceID: workspaceID, PolicyID: policyID, Kind: notionCreditAccessAssignmentObject}
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		revision, err := loadNotionCreditAccessRevision(ctx, txApp, identity.OrganizationID, workspaceID)
		if err != nil {
			return err
		}
		if _, err := loadStoredNotionCreditAccessPolicy(ctx, txApp, workspaceID, policyID); err != nil {
			return err
		}
		if cursor.Version != 0 && cursor.Revision != revision {
			return errInvalidNotionCreditAccessCursor
		}
		rows := []storedNotionCreditAccessAssignment{}
		if err := txApp.DB().NewQuery(`
			SELECT ` + notionCreditAccessAssignmentRow + ` FROM {{leafen_notion_credit_access_assignments}}
			WHERE [[policy_id]] = {:policyId} AND [[workspace_id]] = {:workspaceId} AND [[id]] > {:after}
			ORDER BY [[id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"policyId": policyID, "workspaceId": workspaceID, "after": cursor.AfterID, "limit": pageSize + 1,
		}).All(&rows); err != nil {
			return err
		}
		response.HasMore = len(rows) > pageSize
		if response.HasMore {
			rows = rows[:pageSize]
			next.Revision, next.AfterID = revision, rows[len(rows)-1].ID
		}
		for _, row := range rows {
			projected, err := projectNotionCreditAccessAssignment(row)
			if err != nil {
				return err
			}
			response.Results = append(response.Results, projected)
		}
		return nil
	})
	if err != nil || !response.HasMore {
		return response, err
	}
	response.NextCursor, err = encodeNotionCreditAccessCursor(secret, identity, next)
	return response, err
}

func retrieveNotionCreditAccessPolicy(
	ctx context.Context,
	app core.App,
	identity notionAdminIdentity,
	workspaceID, policyID string,
) (notionCreditAccessPolicyResponse, error) {
	if _, err := loadNotionCreditAccessRevision(ctx, app, identity.OrganizationID, workspaceID); err != nil {
		return notionCreditAccessPolicyResponse{}, err
	}
	stored, err := loadStoredNotionCreditAccessPolicy(ctx, app, workspaceID, policyID)
	if err != nil {
		return notionCreditAccessPolicyResponse{}, err
	}
	return projectNotionCreditAccessPolicy(stored)
}

func retrieveNotionCreditAccessAssignment(
	ctx context.Context,
	app core.App,
	identity notionAdminIdentity,
	workspaceID, assignmentID string,
) (notionCreditAccessAssignmentResponse, error) {
	if _, err := loadNotionCreditAccessRevision(ctx, app, identity.OrganizationID, workspaceID); err != nil {
		return notionCreditAccessAssignmentResponse{}, err
	}
	stored, err := loadStoredNotionCreditAccessAssignment(ctx, app, workspaceID, assignmentID)
	if err != nil {
		return notionCreditAccessAssignmentResponse{}, err
	}
	return projectNotionCreditAccessAssignment(stored)
}

// loadNotionCreditAccessRevision finds the workspace in the token's
// organization and its credit access revision.
func loadNotionCreditAccessRevision(ctx context.Context, app core.App, organizationID, workspaceID string) (int64, error) {
	var revision int64
	err := app.DB().NewQuery(`
		SELECT [[revision]].[[revision]] FROM {{leafen_workspaces}} [[workspace]]
		JOIN {{leafen_notion_credit_access_revisions}} [[revision]]
			ON [[revision]].[[workspace_id]] = [[workspace]].[[id]]
		WHERE [[workspace]].[[id]] = {:workspaceId} AND [[workspace]].[[organization]] = {:organizationId}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "organizationId": organizationID,
	}).Row(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errNotionCreditAccessNotFound
	}
	if err == nil && revision < 1 {
		err = errors.New("invalid Notion Admin credit access revision")
	}
	return revision, err
}

func loadStoredNotionCreditAccessPolicy(ctx context.Context, app core.App, workspaceID, policyID string) (storedNotionCreditAccessPolicy, error) {
	stored := storedNotionCreditAccessPolicy{}
	err := app.DB().NewQuery(`
		SELECT ` + notionCreditAccessPolicyColumns + ` FROM {{leafen_notion_credit_access_policies}}
		WHERE [[id]] = {:policyId} AND [[workspace_id]] = {:workspaceId}
	`).WithContext(ctx).Bind(dbx.Params{"policyId": policyID, "workspaceId": workspaceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, errNotionCreditAccessNotFound
	}
	return stored, err
}

func loadStoredNotionCreditAccessAssignment(ctx context.Context, app core.App, workspaceID, assignmentID string) (storedNotionCreditAccessAssignment, error) {
	stored := storedNotionCreditAccessAssignment{}
	err := app.DB().NewQuery(`
		SELECT ` + notionCreditAccessAssignmentRow + ` FROM {{leafen_notion_credit_access_assignments}}
		WHERE [[id]] = {:assignmentId} AND [[workspace_id]] = {:workspaceId}
	`).WithContext(ctx).Bind(dbx.Params{"assignmentId": assignmentID, "workspaceId": workspaceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, errNotionCreditAccessNotFound
	}
	return stored, err
}

func projectNotionCreditAccessPolicy(stored storedNotionCreditAccessPolicy) (notionCreditAccessPolicyResponse, error) {
	policyID, err := notionCreditAccessPublicID(notionCreditAccessPolicyIdentity, stored.ID)
	if err != nil {
		return notionCreditAccessPolicyResponse{}, err
	}
	spaceID, err := notionCompatibleWorkspaceID(stored.WorkspaceID)
	if err != nil {
		return notionCreditAccessPolicyResponse{}, err
	}
	limit := notionCreditAccessLimit{Kind: stored.LimitKind}
	if stored.LimitCredits.Valid {
		credits := stored.LimitCredits.Int64
		limit.Credits = &credits
	}
	return notionCreditAccessPolicyResponse{
		Object: notionCreditAccessPolicyObject, ID: policyID, SpaceID: spaceID,
		Version: strconv.FormatInt(stored.Version, 10), Limit: limit, Name: stored.Name,
	}, nil
}

func projectNotionCreditAccessAssignment(stored storedNotionCreditAccessAssignment) (notionCreditAccessAssignmentResponse, error) {
	assignmentID, err := notionCreditAccessPublicID(notionCreditAccessAssignmentIdentity, stored.ID)
	if err != nil {
		return notionCreditAccessAssignmentResponse{}, err
	}
	policyID, err := notionCreditAccessPublicID(notionCreditAccessPolicyIdentity, stored.PolicyID)
	if err != nil {
		return notionCreditAccessAssignmentResponse{}, err
	}
	spaceID, err := notionCompatibleWorkspaceID(stored.WorkspaceID)
	if err != nil {
		return notionCreditAccessAssignmentResponse{}, err
	}
	selector := notionCreditAccessSelector{Type: stored.SelectorType}
	switch stored.SelectorType {
	case notionCreditAccessSelectorUser:
		selector.UserID, err = notionCompatibleUserID(stored.UserID)
	case notionCreditAccessSelectorGroup:
		selector.GroupID, err = notionCompatibleGroupID(stored.GroupID)
	case notionCreditAccessSelectorSCIM:
		selector.Attribute, selector.Value = stored.Attribute, stored.Value
	}
	if err != nil {
		return notionCreditAccessAssignmentResponse{}, err
	}
	return notionCreditAccessAssignmentResponse{
		Object: notionCreditAccessAssignmentObject, ID: assignmentID, SpaceID: spaceID,
		Version: strconv.FormatInt(stored.Version, 10), PolicyID: policyID, Selector: selector,
	}, nil
}
