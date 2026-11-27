package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const notionAdminAgentMutationLockNamespace = "notion-admin-agent-governance-mutation"

func serveNotionAdminAgentList(
	event *core.RequestEvent, secret string, identity leafen.NotionAdminIdentity, workspaceID string,
) error {
	if event.Request.ContentLength > 0 {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "This request takes no body."))
	}
	pageSize, rawCursor, filter, err := notionAdminAgentListQuery(event.Request)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	filterHash, err := notionAdminAgentFilterHash(filter)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	cursor, err := decodeNotionAdminAgentCursor(
		rawCursor, secret, identity, workspaceID, "", "agents", filterHash,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	response, err := listNotionAdminAgents(
		event.Request.Context(), event.App, secret, identity, workspaceID,
		cursor, filter, filterHash, pageSize,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	return event.JSON(200, response)
}

func loadNotionAdminAgentAuthority(
	ctx context.Context, app core.App, organizationID, workspaceID string,
) (notionAdminAgentAuthority, error) {
	authority := notionAdminAgentAuthority{}
	err := app.DB().NewQuery(`
		SELECT [[workspace]].[[name]] AS [[workspace_name]], [[workspace]].[[workspace_owner]] AS [[owner_id]],
			[[agent_revision]].[[revision]] AS [[agent_revision]],
			[[group_revision]].[[revision]] AS [[group_revision]],
			COALESCE([[permission]].[[cursor]], 0) AS [[permission_revision]],
			[[settings]].[[creation_policy]], [[settings]].[[default_agent_credit_limit]],
			[[settings]].[[disable_generation]]
		FROM {{leafen_workspaces}} [[workspace]]
		JOIN {{leafen_notion_agent_settings}} [[settings]]
			ON [[settings]].[[workspace_id]] = [[workspace]].[[id]]
		JOIN {{leafen_notion_agent_revisions}} [[agent_revision]]
			ON [[agent_revision]].[[workspace_id]] = [[workspace]].[[id]]
		JOIN {{leafen_workspace_permission_group_revisions}} [[group_revision]]
			ON [[group_revision]].[[workspace_id]] = [[workspace]].[[id]]
		LEFT JOIN {{leafen_workspace_permission_state}} [[permission]]
			ON [[permission]].[[workspace_id]] = [[workspace]].[[id]]
		WHERE [[workspace]].[[id]] = {:workspaceId} AND [[workspace]].[[organization]] = {:organizationId}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "organizationId": organizationID,
	}).One(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return notionAdminAgentAuthority{}, errNotionAdminAgentNotFound
	}
	if err != nil {
		return notionAdminAgentAuthority{}, err
	}
	if authority.WorkspaceName == "" || authority.OwnerID == "" ||
		authority.AgentRevision < 1 || authority.GroupRevision < 1 ||
		authority.PermissionRevision < 0 || authority.DisableGeneration < 0 {
		return notionAdminAgentAuthority{}, errors.New("invalid Notion Admin agent authority")
	}
	return authority, nil
}

func loadStoredNotionAdminAgent(
	ctx context.Context, app core.App, workspaceID, agentID string,
) (storedNotionAdminAgent, error) {
	agent := storedNotionAdminAgent{}
	err := app.DB().NewQuery(notionAdminAgentStatusProjectionSQL() + `
		SELECT * FROM [[projected_agent]]
		WHERE [[workspace_id]] = {:workspaceId} AND [[id]] = {:agentId}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "agentId": agentID, "statusNow": time.Now().UTC().UnixMilli(),
	}).One(&agent)
	if errors.Is(err, sql.ErrNoRows) {
		return storedNotionAdminAgent{}, errNotionAdminAgentNotFound
	}
	return agent, err
}

func projectNotionAdminAgent(agent storedNotionAdminAgent) (notionAdminAgentResponse, error) {
	publicAgentID, err := notionCompatibleAgentID(agent.ID)
	if err != nil {
		return notionAdminAgentResponse{}, err
	}
	publicSpaceID, err := leafen.NotionCompatibleWorkspaceID(agent.WorkspaceID)
	if err != nil {
		return notionAdminAgentResponse{}, err
	}
	publicCreatorID, err := leafen.NotionCompatibleUserID(agent.CreatedBy)
	if err != nil {
		return notionAdminAgentResponse{}, err
	}
	response := notionAdminAgentResponse{
		Alive: !agent.DeletedAtMS.Valid, CreditSource: "workspace", ID: publicAgentID, SpaceID: publicSpaceID,
		Status: agent.EffectiveStatus, Type: agent.AgentType, Name: agent.Name,
		CreatedByID: publicCreatorID, CreatedByTable: "notion_user",
		CreatedTime: agent.CreatedTimeMS, LastEditedTime: agent.LastEditedTimeMS,
	}
	if agent.AgentType == "custom" {
		response.ParentID = publicSpaceID
		response.ParentTable = "space"
	}
	if agent.Description.Valid {
		response.Description = agent.Description.String
	}
	if agent.LastCreditUsageTimeMS.Valid {
		value := agent.LastCreditUsageTimeMS.Int64
		response.LastCreditUsageTime = &value
	}
	return response, nil
}

func listNotionAdminAgents(
	ctx context.Context, app core.App, secret string, identity leafen.NotionAdminIdentity,
	workspaceID string, cursor notionAdminAgentCursor, filter notionAdminAgentListFilter,
	filterHash string, pageSize int,
) (notionAdminAgentListResponse, error) {
	response := notionAdminAgentListResponse{Results: []notionAdminAgentResponse{}}
	authority := notionAdminAgentAuthority{}
	lastID := ""
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		authority, err = loadNotionAdminAgentAuthority(ctx, txApp, identity.OrganizationID, workspaceID)
		if err != nil {
			return err
		}
		if !notionAdminAgentCursorMatchesAuthority(cursor, authority) {
			return errInvalidNotionAdminAgentCursor
		}
		query, params, err := notionAdminAgentListSQL(workspaceID, cursor.AfterID, filter, pageSize+1)
		if err != nil {
			return err
		}
		rows := []storedNotionAdminAgent{}
		if err := txApp.DB().NewQuery(query).WithContext(ctx).Bind(params).All(&rows); err != nil {
			return err
		}
		response.HasMore = len(rows) > pageSize
		if response.HasMore {
			rows = rows[:pageSize]
		}
		for _, row := range rows {
			projected, err := projectNotionAdminAgent(row)
			if err != nil {
				return err
			}
			response.Results = append(response.Results, projected)
			lastID = row.ID
		}
		return nil
	})
	if err != nil {
		return notionAdminAgentListResponse{}, err
	}
	if response.HasMore {
		next, err := encodeNotionAdminAgentCursor(
			secret, identity, workspaceID, "", "agents", authority, filterHash, lastID,
		)
		if err != nil {
			return notionAdminAgentListResponse{}, err
		}
		response.NextCursor = &next
	}
	return response, nil
}

func notionAdminAgentListSQL(
	workspaceID, afterID string, filter notionAdminAgentListFilter, limit int,
) (string, dbx.Params, error) {
	clauses := []string{"[[agent]].[[workspace_id]] = {:workspaceId}", "[[agent]].[[id]] > {:afterId}"}
	params := dbx.Params{
		"workspaceId": workspaceID, "afterId": afterID, "limit": limit,
		"now": time.Now().UTC().UnixMilli(),
	}
	if filter.Name != "" {
		clauses = append(clauses, "(lower([[agent]].[[name]]) LIKE {:name} OR lower(ifnull([[agent]].[[description]],'')) LIKE {:name})")
		params["name"] = "%" + strings.ToLower(filter.Name) + "%"
	}
	for _, entry := range []struct {
		name, column, operator string
		bound                  *int64
	}{
		{"created_time_from", "created_time_ms", ">=", filter.CreatedTimeFrom},
		{"created_time_to", "created_time_ms", "<=", filter.CreatedTimeTo},
		{"last_edited_time_from", "last_edited_time_ms", ">=", filter.LastEditedTimeFrom},
		{"last_edited_time_to", "last_edited_time_ms", "<=", filter.LastEditedTimeTo},
		{"credit_usage_time_from", "last_credit_usage_time_ms", ">=", filter.CreditUsageTimeFrom},
		{"credit_usage_time_to", "last_credit_usage_time_ms", "<=", filter.CreditUsageTimeTo},
	} {
		bound := entry.bound
		if bound == nil {
			continue
		}
		clauses = append(clauses, fmt.Sprintf(
			"[[agent]].[[%s]] %s {:%s}", entry.column, entry.operator, entry.name,
		))
		params[entry.name] = *bound
	}
	if len(filter.CreatedByIDs) > 0 {
		values, err := resolveNotionAdminPublicIDs(filter.CreatedByIDs, leafen.ResolveNotionCompatibleUserID)
		if err != nil {
			return "", nil, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "created_by_ids has an entry that isn't a user ID.")
		}
		clauses = append(clauses, notionAdminAgentInClause("[[agent]].[[created_by]]", "creator", values, params))
	}
	if len(filter.Statuses) > 0 {
		clauses = append(clauses, notionAdminAgentStatusClause(filter.Statuses, params))
	} else {
		clauses = append(clauses, "[[agent]].[[deleted_at_ms]] IS NULL")
	}
	if len(filter.IntegrationTypes)+len(filter.MCPServerNames)+len(filter.Models) > 0 {
		clauses = append(clauses, "0")
	}
	for _, shared := range []struct {
		values  []string
		kind    string
		resolve func(string) (string, error)
	}{{filter.SharedWithUserIDs, "user", leafen.ResolveNotionCompatibleUserID},
		{filter.SharedWithGroupIDs, "group", leafen.ResolveNotionCompatibleGroupID}} {
		if len(shared.values) == 0 {
			continue
		}
		values, err := resolveNotionAdminPublicIDs(shared.values, shared.resolve)
		if err != nil {
			return "", nil, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "A shared-with ID in the filter isn't a user or group ID.")
		}
		in := notionAdminAgentInClause("[[permission]].[[principal_id]]", shared.kind, values, params)
		clauses = append(clauses, "[[agent]].[[agent_type]] = 'custom'")
		clauses = append(clauses, "EXISTS (SELECT 1 FROM {{leafen_notion_agent_permissions}} [[permission]] WHERE [[permission]].[[agent_id]] = [[agent]].[[id]] AND [[permission]].[[principal_type]] = '"+shared.kind+"' AND "+in+")")
	}
	params["statusNow"] = params["now"]
	return notionAdminAgentStatusProjectionSQL() + `SELECT [[agent]].*
		FROM [[projected_agent]] [[agent]] WHERE ` +
		strings.Join(clauses, " AND ") + ` ORDER BY [[agent]].[[id]] LIMIT {:limit}`, params, nil
}

func notionAdminAgentInClause(column, prefix string, values []string, params dbx.Params) string {
	placeholders := make([]string, 0, len(values))
	for index, value := range values {
		key := fmt.Sprintf("%s%d", prefix, index)
		params[key] = value
		placeholders = append(placeholders, "{:"+key+"}")
	}
	return column + " IN (" + leafen.JoinSQL(placeholders) + ")"
}

func resolveNotionAdminPublicIDs(values []string, resolve func(string) (string, error)) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		resolved, err := resolve(strings.TrimSpace(value))
		if err != nil {
			return nil, err
		}
		result = append(result, resolved)
	}
	return result, nil
}

func notionAdminAgentCursorMatchesAuthority(cursor notionAdminAgentCursor, authority notionAdminAgentAuthority) bool {
	return cursor.Version == 0 || cursor.AgentRevision == authority.AgentRevision &&
		cursor.GroupRevision == authority.GroupRevision &&
		cursor.PermissionRevision == authority.PermissionRevision
}
