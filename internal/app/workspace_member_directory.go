package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const workspaceMemberListCursorVersion = 1

var (
	errWorkspaceMemberListCursorInvalid   = errors.New(errorCodeWorkspaceMemberListCursorInvalid)
	errWorkspaceMemberListRevisionChanged = errors.New(errorCodeWorkspaceMemberListRevisionChanged)
)

type workspaceMemberListCursor struct {
	Version           int    `json:"v"`
	PrincipalID       string `json:"p"`
	WorkspaceID       string `json:"w"`
	AuthorityRevision int    `json:"r"`
	AfterUserID       string `json:"u"`
	Limit             int    `json:"l"`
}

type workspaceMemberListQuery struct {
	Limit             int
	AuthorityRevision *int
}

type storedWorkspaceMemberDirectoryRow struct {
	ID          string  `db:"id"`
	WorkspaceID string  `db:"workspace_id"`
	UserID      string  `db:"user_id"`
	DisplayName string  `db:"display_name"`
	Email       string  `db:"email"`
	Avatar      *string `db:"avatar"`
	Role        string  `db:"role"`
	CreatedAt   int64   `db:"created_at"`
	UpdatedAt   int64   `db:"updated_at"`
}

func readWorkspaceMemberDirectory(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	query, err := parseWorkspaceMemberListQuery(event.Request)
	if err != nil || workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMemberListInvalid, "Check the member query range.")
	}
	response, err := loadWorkspaceMemberDirectoryPage(
		event.App, user.Id, workspaceID, event.Request.URL.Query().Get("cursor"), query,
	)
	switch {
	case isPermissionDenied(err):
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	case errors.Is(err, errWorkspaceMemberListCursorInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMemberListCursorInvalid, "Check the member query position.")
	case errors.Is(err, errWorkspaceMemberListRevisionChanged):
		return apiError(event, http.StatusConflict, errorCodeWorkspaceMemberListRevisionChanged, "The workspace members changed. Load them again from the start.")
	case err != nil:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeWorkspaceMemberListFailed,
			"Couldn't load the workspace members.", "workspace_member_directory", "read",
			errorCodeWorkspaceMemberListFailed, err,
		)
	default:
		event.Response.Header().Set("Cache-Control", "no-store")
		event.Response.Header().Set("Pragma", "no-cache")
		return event.JSON(http.StatusOK, response)
	}
}

func parseWorkspaceMemberListQuery(request *http.Request) (workspaceMemberListQuery, error) {
	query := request.URL.Query()
	for key, values := range query {
		if (key != "cursor" && key != "limit" && key != "authorityRevision") || len(values) != 1 {
			return workspaceMemberListQuery{}, errors.New(errorCodeWorkspaceMemberListInvalid)
		}
	}
	limit, err := workspaceMemberListPageLimit(query.Get("limit"))
	if err != nil {
		return workspaceMemberListQuery{}, err
	}
	result := workspaceMemberListQuery{Limit: limit}
	if raw := strings.TrimSpace(query.Get("authorityRevision")); raw != "" {
		revision, err := strconv.Atoi(raw)
		if err != nil || revision < 0 {
			return workspaceMemberListQuery{}, errors.New(errorCodeWorkspaceMemberListInvalid)
		}
		result.AuthorityRevision = &revision
	}
	if query.Get("cursor") != "" && result.AuthorityRevision == nil {
		return workspaceMemberListQuery{}, errors.New(errorCodeWorkspaceMemberListInvalid)
	}
	return result, nil
}

func workspaceMemberListPageLimit(raw string) (int, error) {
	if value := strings.TrimSpace(raw); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > workspaceMemberListMaximumPageSize {
			return 0, errors.New(errorCodeWorkspaceMemberListInvalid)
		}
		return limit, nil
	}
	return workspaceMemberListDefaultPageSize, nil
}

func loadWorkspaceMemberDirectoryPage(
	app core.App,
	principalID string,
	workspaceID string,
	rawCursor string,
	query workspaceMemberListQuery,
) (workspaceMemberDirectoryResponse, error) {
	response := workspaceMemberDirectoryResponse{
		Members: []workspaceMemberDirectoryItem{}, Invitations: []workspaceMemberInvitation{},
	}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		cursor, err := decodeWorkspaceMemberListCursor(
			rawCursor, principalID, workspaceID, query.Limit,
		)
		if err != nil {
			return err
		}
		authorization, err := authorizeWorkspace(txApp, principalID, workspaceID, permissionWorkspaceRead)
		if err != nil {
			return err
		}
		response.AuthorityRevision, err = currentWorkspacePermissionCursor(txApp, workspaceID)
		if err != nil {
			return err
		}
		response.MemberAddPolicy, response.MemberAddPolicyRevision, err = loadWorkspaceMemberDirectoryAddPolicy(txApp)
		if err != nil {
			return err
		}
		policy, err := workspaceSharingPolicy(txApp, workspaceID)
		if err != nil {
			return err
		}
		response.MemberRequests = authorization.Role == workspaceRoleMember && policy.MemberAdds == sharingSecurityMemberAddsApproval && response.MemberAddPolicy == administratorMemberAddPolicyEnabled
		if query.AuthorityRevision != nil && *query.AuthorityRevision != response.AuthorityRevision {
			return errWorkspaceMemberListRevisionChanged
		}
		if cursor.Version != 0 && cursor.AuthorityRevision != response.AuthorityRevision {
			return errWorkspaceMemberListRevisionChanged
		}
		response.CurrentMember, err = loadWorkspaceMemberDirectoryCurrentMember(
			txApp, workspaceID, principalID,
		)
		if err != nil {
			return err
		}
		response.CurrentMember.Restricted = authorization.Restricted
		// Whether the organization's identity provider keeps them here, so
		// leaving is the administrator's to do: said before they ask (2026-11-06).
		if response.CurrentMember.IdentityManaged, err = workspaceMemberProvisionedBySCIM(txApp, workspaceID, principalID); err != nil {
			return err
		}
		// The waiting invitations come with the first page.
		if cursor.Version == 0 && authorization.Role == workspaceRoleOwner {
			if response.Invitations, err = loadWorkspaceMemberInvitations(txApp, workspaceID); err != nil {
				return err
			}
		}
		rows, err := loadWorkspaceMemberDirectoryRows(
			txApp, workspaceID, principalID, authorization.Restricted, cursor.AfterUserID, query.Limit,
		)
		if err != nil {
			return err
		}
		hasMore := len(rows) > query.Limit
		if hasMore {
			rows = rows[:query.Limit]
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.UserID)
		}
		// An app's account is listed, marked as the app it is.
		apps, err := integrationAccountUsers(txApp, ids)
		if err != nil {
			return err
		}
		restricted, err := workspaceRestrictedMembers(txApp, workspaceID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			item := workspaceMemberDirectoryWireItem(row)
			item.App, item.Restricted = apps[row.UserID], restricted[row.UserID]
			item.Email = peopleEmailShown(authorization.Restricted, principalID, row.UserID, item.Email)
			response.Members = append(response.Members, item)
		}
		if err := withWorkspaceMemberDirectoryMemberships(
			txApp, workspaceID, principalID, authorization.Role == workspaceRoleOwner, response.Members,
		); err != nil {
			return err
		}
		if hasMore {
			last := rows[len(rows)-1]
			next, err := encodeWorkspaceMemberListCursor(workspaceMemberListCursor{
				Version: workspaceMemberListCursorVersion, PrincipalID: principalID,
				WorkspaceID: workspaceID, AuthorityRevision: response.AuthorityRevision,
				AfterUserID: last.UserID, Limit: query.Limit,
			})
			if err != nil {
				return err
			}
			response.NextCursor = &next
		}
		return nil
	})
	return response, err
}

func loadWorkspaceMemberDirectoryAddPolicy(app core.App) (string, int, error) {
	state, err := loadWorkspaceMemberAddPolicyState(app)
	if err != nil {
		return "", 0, err
	}
	return state.Policy, state.Revision, nil
}

func loadWorkspaceMemberDirectoryCurrentMember(
	app core.App,
	workspaceID string,
	principalID string,
) (workspaceMemberDirectoryItem, error) {
	row := storedWorkspaceMemberDirectoryRow{}
	err := app.DB().NewQuery(`SELECT * FROM (
		SELECT [[user]].[[id]] AS [[id]], [[workspace]].[[id]] AS [[workspace_id]], [[user]].[[id]] AS [[user_id]],
		COALESCE(NULLIF(trim([[user]].[[name]]),''),[[user]].[[email]]) AS [[display_name]], [[user]].[[email]],
		NULLIF([[user]].[[profile_avatar]],'') AS [[avatar]], 'owner' AS [[role]],
		CAST(unixepoch([[user]].[[created]])*1000 AS INTEGER) AS [[created_at]], CAST(unixepoch([[user]].[[updated]])*1000 AS INTEGER) AS [[updated_at]]
		FROM {{leafen_workspaces}} [[workspace]] JOIN {{users}} [[user]] ON [[user]].[[id]]=[[workspace]].[[workspace_owner]] WHERE [[workspace]].[[id]]={:workspaceId}
		UNION ALL
		SELECT [[user]].[[id]], [[membership]].[[workspace_id]], [[membership]].[[user_id]],
		COALESCE(NULLIF(trim([[user]].[[name]]),''),[[user]].[[email]]), [[user]].[[email]], NULLIF([[user]].[[profile_avatar]],''), [[membership]].[[role]], [[membership]].[[created]], [[membership]].[[updated]]
		FROM {{leafen_workspace_memberships}} [[membership]] JOIN {{users}} [[user]] ON [[user]].[[id]]=[[membership]].[[user_id]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]=[[membership]].[[workspace_id]]
		WHERE [[membership]].[[workspace_id]]={:workspaceId} AND [[membership]].[[user_id]] != [[workspace]].[[workspace_owner]]
	) WHERE [[user_id]]={:principalId}`).Bind(dbx.Params{"workspaceId": workspaceID, "principalId": principalID}).One(&row)
	return workspaceMemberDirectoryWireItem(row), err
}

// A restricted reader lists the people they share a place with
// (restricted_member_people.go).
func loadWorkspaceMemberDirectoryRows(
	app core.App,
	workspaceID string,
	readerID string,
	restricted bool,
	afterUserID string,
	limit int,
) ([]storedWorkspaceMemberDirectoryRow, error) {
	rows := []storedWorkspaceMemberDirectoryRow{}
	err := app.DB().NewQuery(`
		SELECT [[user]].[[id]] AS [[id]],[[workspace]].[[id]] AS [[workspace_id]],
			[[user]].[[id]] AS [[user_id]],
			COALESCE(NULLIF(trim([[user]].[[name]]),''),[[user]].[[email]]) AS [[display_name]],
			[[user]].[[email]],NULLIF([[user]].[[profile_avatar]],'') AS [[avatar]],
			'owner' AS [[role]],
			CAST(unixepoch([[user]].[[created]])*1000 AS INTEGER) AS [[created_at]],
			CAST(unixepoch([[user]].[[updated]])*1000 AS INTEGER) AS [[updated_at]]
		FROM {{leafen_workspaces}} [[workspace]]
		JOIN {{users}} [[user]] ON [[user]].[[id]]=[[workspace]].[[workspace_owner]]
		WHERE [[workspace]].[[id]]={:workspaceId} AND [[user]].[[id]]>{:afterUserId}
			-- People only: an app's account owns the app's own workspace and
			-- joins as a member where the app is installed (workspaceMemberCountSQL).
			AND NOT ` + integrationAccountSQL("[[user]].[[id]]") + `
		UNION ALL
		SELECT [[user]].[[id]],[[membership]].[[workspace_id]],[[user]].[[id]],
			COALESCE(NULLIF(trim([[user]].[[name]]),''),[[user]].[[email]]),
			[[user]].[[email]],NULLIF([[user]].[[profile_avatar]],''),
			[[membership]].[[role]],[[membership]].[[created]],[[membership]].[[updated]]
		FROM {{leafen_workspace_memberships}} [[membership]]
			INDEXED BY [[sqlite_autoindex_leafen_workspace_memberships_1]]
		JOIN {{users}} [[user]] ON [[user]].[[id]]=[[membership]].[[user_id]]
		WHERE [[membership]].[[workspace_id]]={:workspaceId}
			AND [[membership]].[[user_id]]>{:afterUserId}
			AND NOT ` + integrationAccountSQL("[[user]].[[id]]") + restrictedPeopleFilterSQL(restricted) + `
		ORDER BY [[user_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "readerId": readerID, "afterUserId": afterUserID, "limit": limit + 1,
	}).All(&rows)
	return rows, err
}

func workspaceMemberDirectoryWireItem(
	row storedWorkspaceMemberDirectoryRow,
) workspaceMemberDirectoryItem {
	return workspaceMemberDirectoryItem{
		ID: row.ID, WorkspaceID: row.WorkspaceID, UserID: row.UserID,
		DisplayName: row.DisplayName, Email: row.Email, Avatar: row.Avatar,
		Role: row.Role, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func encodeWorkspaceMemberListCursor(cursor workspaceMemberListCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	result := base64.RawURLEncoding.EncodeToString(encoded)
	if len(result) > workspaceMemberListCursorBytes {
		return "", errWorkspaceMemberListCursorInvalid
	}
	return result, nil
}

func decodeWorkspaceMemberListCursor(
	raw string,
	principalID string,
	workspaceID string,
	limit int,
) (workspaceMemberListCursor, error) {
	if raw == "" {
		return workspaceMemberListCursor{}, nil
	}
	if len(raw) > workspaceMemberListCursorBytes {
		return workspaceMemberListCursor{}, errWorkspaceMemberListCursorInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return workspaceMemberListCursor{}, errWorkspaceMemberListCursorInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	cursor := workspaceMemberListCursor{}
	if err := decoder.Decode(&cursor); err != nil {
		return workspaceMemberListCursor{}, errWorkspaceMemberListCursorInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return workspaceMemberListCursor{}, errWorkspaceMemberListCursorInvalid
	}
	canonical, err := encodeWorkspaceMemberListCursor(cursor)
	if err != nil || canonical != raw || cursor.Version != workspaceMemberListCursorVersion ||
		cursor.PrincipalID != principalID || cursor.WorkspaceID != workspaceID ||
		cursor.AuthorityRevision < 0 || cursor.AfterUserID == "" || cursor.Limit != limit {
		return workspaceMemberListCursor{}, errWorkspaceMemberListCursorInvalid
	}
	return cursor, nil
}
