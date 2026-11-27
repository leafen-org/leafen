package organization

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionAdminAgentPermissionPrincipal struct {
	Type    string `json:"type"`
	GroupID string `json:"group_id,omitempty"`
	UserID  string `json:"user_id,omitempty"`
}

type notionAdminAgentPermissionSet struct {
	Principal notionAdminAgentPermissionPrincipal `json:"principal"`
	Role      string                              `json:"role"`
}

type notionAdminAgentPermissionsRequest struct {
	Set    []notionAdminAgentPermissionSet       `json:"set,omitempty"`
	Remove []notionAdminAgentPermissionPrincipal `json:"remove,omitempty"`
}

type notionAdminAgentPermissionResponse struct {
	Principal    notionAdminAgentPermissionPrincipal `json:"principal"`
	ResolvedRole string                              `json:"resolved_role"`
	Role         string                              `json:"role"`
}

type notionAdminAgentPermissionsResponse struct {
	Permissions []notionAdminAgentPermissionResponse `json:"permissions"`
	NextCursor  *string                              `json:"next_cursor,omitempty"`
}

type storedNotionAdminAgentPermission struct {
	AgentID       string `db:"agent_id"`
	PrincipalType string `db:"principal_type"`
	PrincipalID   string `db:"principal_id"`
	Role          string `db:"role"`
	UpdatedAtMS   int64  `db:"updated_at_ms"`
}

func serveNotionAdminAgentPermissions(
	event *core.RequestEvent, secret string, identity leafen.NotionAdminIdentity, workspaceID, agentID string,
) error {
	if event.Request.Method == http.MethodPatch {
		if len(event.Request.URL.Query()) != 0 {
			return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "This request takes no query parameters."))
		}
		request := notionAdminAgentPermissionsRequest{}
		if decodeNotionAdminAgentBody(event, &request) != nil {
			return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "The body isn't a permissions change: set and remove are lists of principals, each type workspace, group or user."))
		}
		response, err := updateNotionAdminAgentPermissions(
			event.Request.Context(), event.App, secret, identity, workspaceID, agentID, request,
		)
		if err != nil {
			return notionAdminAgentError(event, err)
		}
		return event.JSON(http.StatusOK, response)
	}
	if event.Request.ContentLength > 0 {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "This request takes no body."))
	}
	_, rawCursor, err := notionAdminAgentSimpleQuery(event.Request, "cursor", false)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	filterHash := notionAdminAgentCanonicalQueryHash(event.Request.URL.Query(), "cursor")
	cursor, err := decodeNotionAdminAgentCursor(
		rawCursor, secret, identity, workspaceID, agentID, "agent_permissions", filterHash,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	response, err := readNotionAdminAgentPermissions(
		event.Request.Context(), event.App, secret, identity, workspaceID, agentID,
		cursor, filterHash, notionAdminAgentPermissionPage,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func readNotionAdminAgentPermissions(
	ctx context.Context, app core.App, secret string, identity leafen.NotionAdminIdentity,
	workspaceID, agentID string, cursor notionAdminAgentCursor, filterHash string, pageSize int,
) (notionAdminAgentPermissionsResponse, error) {
	response := notionAdminAgentPermissionsResponse{}
	authority := notionAdminAgentAuthority{}
	last := ""
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		authority, err = loadNotionAdminAgentAuthority(ctx, txApp, identity.OrganizationID, workspaceID)
		if err != nil || !notionAdminAgentCursorMatchesAuthority(cursor, authority) {
			if err == nil {
				err = errInvalidNotionAdminAgentCursor
			}
			return err
		}
		agent, err := loadStoredNotionAdminAgent(ctx, txApp, workspaceID, agentID)
		if err != nil || agent.DeletedAtMS.Valid || agent.AgentType != "custom" {
			return errors.Join(errNotionAdminAgentNotFound, err)
		}
		response, last, err = loadNotionAdminAgentPermissionsPage(
			ctx, txApp, workspaceID, agentID, cursor.AfterID, pageSize,
		)
		return err
	})
	if err != nil {
		return notionAdminAgentPermissionsResponse{}, err
	}
	if len(response.Permissions) == pageSize && last != "" {
		next, err := encodeNotionAdminAgentCursor(
			secret, identity, workspaceID, agentID, "agent_permissions", authority, filterHash, last,
		)
		if err != nil {
			return notionAdminAgentPermissionsResponse{}, err
		}
		response.NextCursor = &next
	}
	return response, nil
}

func loadNotionAdminAgentPermissionsPage(
	ctx context.Context, app core.App, workspaceID, agentID, after string, pageSize int,
) (notionAdminAgentPermissionsResponse, string, error) {
	rows := []storedNotionAdminAgentPermission{}
	err := app.DB().NewQuery(`SELECT [[agent_id]], [[principal_type]], [[principal_id]], [[role]], [[updated_at_ms]]
		FROM {{leafen_notion_agent_permissions}}
		WHERE [[agent_id]] = {:agentId} AND
			(printf('%d', CASE [[principal_type]] WHEN 'workspace' THEN 0 WHEN 'group' THEN 1 ELSE 2 END)
			 || ':' || [[principal_id]]) > {:after}
		ORDER BY CASE [[principal_type]] WHEN 'workspace' THEN 0 WHEN 'group' THEN 1 ELSE 2 END,
			[[principal_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"agentId": agentID, "after": after, "limit": pageSize + 1,
	}).All(&rows)
	if err != nil {
		return notionAdminAgentPermissionsResponse{}, "", err
	}
	hasMore := len(rows) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}
	response := notionAdminAgentPermissionsResponse{
		Permissions: make([]notionAdminAgentPermissionResponse, 0, len(rows)),
	}
	resolvedRoles, err := resolvedNotionAdminAgentPermissionRoles(
		ctx, app, workspaceID, agentID, rows,
	)
	if err != nil {
		return notionAdminAgentPermissionsResponse{}, "", err
	}
	last := ""
	for _, row := range rows {
		principal := notionAdminAgentPermissionPrincipal{Type: row.PrincipalType}
		switch row.PrincipalType {
		case "group":
			principal.GroupID, err = leafen.NotionCompatibleGroupID(row.PrincipalID)
		case "user":
			principal.UserID, err = leafen.NotionCompatibleUserID(row.PrincipalID)
		}
		if err != nil {
			return notionAdminAgentPermissionsResponse{}, "", err
		}
		response.Permissions = append(response.Permissions, notionAdminAgentPermissionResponse{
			Principal: principal, ResolvedRole: resolvedRoles[row.PrincipalType+":"+row.PrincipalID], Role: row.Role,
		})
		rank := "2"
		if row.PrincipalType == "workspace" {
			rank = "0"
		} else if row.PrincipalType == "group" {
			rank = "1"
		}
		last = rank + ":" + row.PrincipalID
	}
	if !hasMore {
		last = ""
	}
	return response, last, nil
}

func resolvedNotionAdminAgentPermissionRoles(
	ctx context.Context, app core.App, workspaceID, agentID string,
	rows []storedNotionAdminAgentPermission,
) (map[string]string, error) {
	resolved := map[string]string{}
	workspaceRole := ""
	if err := app.DB().NewQuery(`SELECT [[role]] FROM {{leafen_notion_agent_permissions}}
		WHERE [[agent_id]] = {:agentId} AND [[principal_type]] = 'workspace'
	`).WithContext(ctx).Bind(dbx.Params{"agentId": agentID}).Row(&workspaceRole); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	userIDs := []string{}
	for _, row := range rows {
		role := row.Role
		if row.PrincipalType == "group" {
			role = maxNotionAdminAgentPermissionRole(role, workspaceRole)
		}
		resolved[row.PrincipalType+":"+row.PrincipalID] = role
		if row.PrincipalType == "user" {
			userIDs = append(userIDs, row.PrincipalID)
		}
	}
	if len(userIDs) == 0 {
		return resolved, nil
	}
	params := dbx.Params{"agentId": agentID, "workspaceId": workspaceID}
	in := notionAdminAgentInClause("[[member]].[[user_id]]", "resolvedUser", userIDs, params)
	type effectiveGroupRole struct {
		UserID            string `db:"user_id"`
		IsWorkspaceMember int    `db:"is_workspace_member"`
		Rank              int    `db:"rank"`
	}
	effective := []effectiveGroupRole{}
	if err := app.DB().NewQuery(`SELECT [[user]].[[id]] AS [[user_id]],
		CASE WHEN [[workspace]].[[workspace_owner]] = [[user]].[[id]] OR EXISTS (
			SELECT 1 FROM {{leafen_workspace_memberships}} [[workspace_member]]
			WHERE [[workspace_member]].[[workspace_id]] = {:workspaceId}
				AND [[workspace_member]].[[user_id]] = [[user]].[[id]]
				AND [[workspace_member]].[[role]] = 'member'
		) THEN 1 ELSE 0 END AS [[is_workspace_member]],
		COALESCE(MAX(CASE [[permission]].[[role]]
			WHEN 'full_access' THEN 3 WHEN 'edit' THEN 2 WHEN 'view_and_interact' THEN 1 ELSE 0 END), 0)
			AS [[rank]]
		FROM {{users}} [[user]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = {:workspaceId}
		LEFT JOIN {{leafen_workspace_permission_group_members}} [[member]]
			ON [[member]].[[user_id]] = [[user]].[[id]]
		LEFT JOIN {{leafen_workspace_permission_groups}} [[group]]
			ON [[group]].[[id]] = [[member]].[[group_id]] AND [[group]].[[workspace_id]] = {:workspaceId}
			AND [[group]].[[deleted_at]] IS NULL
		LEFT JOIN {{leafen_notion_agent_permissions}} [[permission]]
			ON [[permission]].[[agent_id]] = {:agentId} AND [[permission]].[[principal_type]] = 'group'
			AND [[permission]].[[principal_id]] = [[group]].[[id]]
		WHERE ` + in + ` GROUP BY [[user]].[[id]], [[workspace]].[[workspace_owner]]
	`).WithContext(ctx).Bind(params).All(&effective); err != nil {
		return nil, err
	}
	for _, item := range effective {
		key := "user:" + item.UserID
		if item.IsWorkspaceMember == 1 {
			resolved[key] = maxNotionAdminAgentPermissionRole(resolved[key], workspaceRole)
			if item.Rank > 0 {
				resolved[key] = maxNotionAdminAgentPermissionRole(
					resolved[key], notionAdminAgentPermissionRoleFromRank(item.Rank),
				)
			}
		}
	}
	return resolved, nil
}

func maxNotionAdminAgentPermissionRole(left, right string) string {
	if notionAdminAgentPermissionRoleRank(right) > notionAdminAgentPermissionRoleRank(left) {
		return right
	}
	return left
}

func notionAdminAgentPermissionRoleRank(role string) int {
	switch role {
	case "full_access":
		return 3
	case "edit":
		return 2
	case "view_and_interact":
		return 1
	default:
		return 0
	}
}

func notionAdminAgentPermissionRoleFromRank(rank int) string {
	switch rank {
	case 3:
		return "full_access"
	case 2:
		return "edit"
	default:
		return "view_and_interact"
	}
}
