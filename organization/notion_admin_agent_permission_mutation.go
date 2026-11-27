package organization

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type normalizedNotionAdminAgentPermission struct {
	PrincipalType string
	PrincipalID   string
	Role          string
}

func updateNotionAdminAgentPermissions(
	ctx context.Context, app core.App, secret string, expected leafen.NotionAdminIdentity,
	workspaceID, agentID string, request notionAdminAgentPermissionsRequest,
) (notionAdminAgentPermissionsResponse, error) {
	normalizedSet, normalizedRemove, err := normalizeNotionAdminAgentPermissionRequest(workspaceID, request)
	if err != nil {
		return notionAdminAgentPermissionsResponse{}, err
	}
	release := leafen.AcquireProductOperationLock(notionAdminAgentMutationLockNamespace, workspaceID)
	defer release()
	response := notionAdminAgentPermissionsResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		current, _, err := authorizeCurrentNotionAdminAgentMutation(ctx, txApp, secret, expected, workspaceID)
		if err != nil {
			return err
		}
		agent, err := loadStoredNotionAdminAgent(ctx, txApp, workspaceID, agentID)
		if err != nil || agent.DeletedAtMS.Valid || agent.AgentType != "custom" {
			return errors.Join(errNotionAdminAgentNotFound, err)
		}
		if err := validateNotionAdminAgentPermissionPrincipals(
			ctx, txApp, current.OrganizationID, workspaceID, normalizedSet,
		); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		setChanged, err := setNotionAdminAgentPermissions(ctx, txApp, agentID, normalizedSet, now)
		if err != nil {
			return err
		}
		removeChanged, err := removeNotionAdminAgentPermissions(ctx, txApp, agentID, normalizedRemove)
		if err != nil {
			return err
		}
		if setChanged || removeChanged {
			if err := finalizeNotionAdminAgentMutation(
				txApp, current, "permissions.update", "agent", agentID,
			); err != nil {
				return err
			}
		}
		response, _, err = loadNotionAdminAgentPermissionsPage(
			ctx, txApp, workspaceID, agentID, "", notionAdminAgentPermissionPage,
		)
		return err
	})
	return response, err
}

func setNotionAdminAgentPermissions(
	ctx context.Context, app core.App, agentID string,
	permissions []normalizedNotionAdminAgentPermission, now int64,
) (bool, error) {
	if len(permissions) == 0 {
		return false, nil
	}
	params := dbx.Params{"agentId": agentID, "now": now}
	values := make([]string, 0, len(permissions))
	for index, permission := range permissions {
		typeKey := fmt.Sprintf("setType%d", index)
		idKey := fmt.Sprintf("setId%d", index)
		roleKey := fmt.Sprintf("setRole%d", index)
		params[typeKey] = permission.PrincipalType
		params[idKey] = permission.PrincipalID
		params[roleKey] = permission.Role
		values = append(values, fmt.Sprintf(
			"({:agentId}, {:%s}, {:%s}, {:%s}, {:now})", typeKey, idKey, roleKey,
		))
	}
	result, err := app.DB().NewQuery(`INSERT INTO {{leafen_notion_agent_permissions}}
		([[agent_id]], [[principal_type]], [[principal_id]], [[role]], [[updated_at_ms]]) VALUES ` +
		strings.Join(values, ",") + `
		ON CONFLICT ([[agent_id]], [[principal_type]], [[principal_id]]) DO UPDATE
		SET [[role]] = excluded.[[role]], [[updated_at_ms]] = excluded.[[updated_at_ms]]
		WHERE [[role]] != excluded.[[role]]
	`).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func removeNotionAdminAgentPermissions(
	ctx context.Context, app core.App, agentID string,
	permissions []normalizedNotionAdminAgentPermission,
) (bool, error) {
	if len(permissions) == 0 {
		return false, nil
	}
	params := dbx.Params{"agentId": agentID}
	predicates := make([]string, 0, len(permissions))
	for index, permission := range permissions {
		typeKey := fmt.Sprintf("removeType%d", index)
		idKey := fmt.Sprintf("removeId%d", index)
		params[typeKey] = permission.PrincipalType
		params[idKey] = permission.PrincipalID
		predicates = append(predicates, fmt.Sprintf(
			"([[principal_type]] = {:%s} AND [[principal_id]] = {:%s})", typeKey, idKey,
		))
	}
	result, err := app.DB().NewQuery(`DELETE FROM {{leafen_notion_agent_permissions}}
		WHERE [[agent_id]] = {:agentId} AND (` + strings.Join(predicates, " OR ") + `)
	`).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func normalizeNotionAdminAgentPermissionRequest(
	workspaceID string, request notionAdminAgentPermissionsRequest,
) ([]normalizedNotionAdminAgentPermission, []normalizedNotionAdminAgentPermission, error) {
	sets := map[string]normalizedNotionAdminAgentPermission{}
	removes := map[string]normalizedNotionAdminAgentPermission{}
	for _, entry := range request.Set {
		permission, err := normalizeNotionAdminAgentPrincipal(workspaceID, entry.Principal)
		if err != nil || !governanceOneOf(entry.Role, "view_and_interact", "edit", "full_access") {
			return nil, nil, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "Each set entry needs a valid principal and a role of view_and_interact, edit or full_access.")
		}
		permission.Role = entry.Role
		key := permission.PrincipalType + ":" + permission.PrincipalID
		if prior, exists := sets[key]; exists && prior.Role != permission.Role {
			return nil, nil, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "The same principal is set twice with different roles.")
		}
		sets[key] = permission
	}
	for _, principal := range request.Remove {
		permission, err := normalizeNotionAdminAgentPrincipal(workspaceID, principal)
		if err != nil {
			return nil, nil, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "Each remove entry needs a valid principal.")
		}
		key := permission.PrincipalType + ":" + permission.PrincipalID
		if _, exists := sets[key]; exists {
			return nil, nil, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "The same principal is both set and removed.")
		}
		removes[key] = permission
	}
	toSlice := func(values map[string]normalizedNotionAdminAgentPermission) []normalizedNotionAdminAgentPermission {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		result := make([]normalizedNotionAdminAgentPermission, 0, len(keys))
		for _, key := range keys {
			result = append(result, values[key])
		}
		return result
	}
	return toSlice(sets), toSlice(removes), nil
}

func normalizeNotionAdminAgentPrincipal(
	workspaceID string, principal notionAdminAgentPermissionPrincipal,
) (normalizedNotionAdminAgentPermission, error) {
	result := normalizedNotionAdminAgentPermission{PrincipalType: principal.Type}
	var err error
	switch principal.Type {
	case "workspace":
		if principal.GroupID != "" || principal.UserID != "" {
			return result, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "A workspace principal takes no group_id or user_id.")
		}
		result.PrincipalID = workspaceID
	case "group":
		if principal.GroupID == "" || principal.UserID != "" {
			return result, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "A group principal takes group_id and no user_id.")
		}
		result.PrincipalID, err = leafen.ResolveNotionCompatibleGroupID(strings.TrimSpace(principal.GroupID))
	case "user":
		if principal.UserID == "" || principal.GroupID != "" {
			return result, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "A user principal takes user_id and no group_id.")
		}
		result.PrincipalID, err = leafen.ResolveNotionCompatibleUserID(strings.TrimSpace(principal.UserID))
	default:
		return result, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "principal.type takes workspace, group or user.")
	}
	if err != nil {
		return result, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "The principal's ID isn't a group or user ID.")
	}
	return result, nil
}

func validateNotionAdminAgentPermissionPrincipals(
	ctx context.Context, app core.App, organizationID, workspaceID string,
	sets []normalizedNotionAdminAgentPermission,
) error {
	groups := map[string]struct{}{}
	users := map[string]struct{}{}
	for _, permission := range sets {
		switch permission.PrincipalType {
		case "group":
			groups[permission.PrincipalID] = struct{}{}
		case "user":
			users[permission.PrincipalID] = struct{}{}
		}
	}
	for values, kind := range map[*map[string]struct{}]string{&groups: "group", &users: "user"} {
		if len(*values) == 0 {
			continue
		}
		ids := make([]string, 0, len(*values))
		for id := range *values {
			ids = append(ids, id)
		}
		params := dbx.Params{"workspaceId": workspaceID, "organizationId": organizationID}
		in := notionAdminAgentInClause("[[id]]", kind, ids, params)
		query := `SELECT COUNT(*) FROM {{leafen_workspace_permission_groups}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[deleted_at]] IS NULL AND ` + in
		if kind == "user" {
			in = notionAdminAgentInClause("[[user]].[[id]]", kind, ids, params)
			query = `SELECT COUNT(*) FROM {{users}} [[user]]
				JOIN {{leafen_organization_memberships}} [[organization_member]]
					ON [[organization_member]].[[user]] = [[user]].[[id]]
					AND [[organization_member]].[[organization]] = {:organizationId}
					AND [[organization_member]].[[status]] = 'active'
				JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = {:workspaceId}
				WHERE ` + in + ` AND ([[workspace]].[[workspace_owner]] = [[user]].[[id]] OR EXISTS (
					SELECT 1 FROM {{leafen_workspace_memberships}} [[member]]
					WHERE [[member]].[[workspace_id]] = [[workspace]].[[id]]
						AND [[member]].[[user_id]] = [[user]].[[id]] AND [[member]].[[role]] = 'member'))`
		}
		var count int
		if err := app.DB().NewQuery(query).WithContext(ctx).Bind(params).Row(&count); err != nil {
			return err
		}
		if count != len(ids) {
			return errNotionAdminAgentNotFound
		}
	}
	return nil
}
