package organization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAdminMCPConnectionsDefaultPageSize = 100
	notionAdminMCPConnectionsMaximumPageSize = 100
	notionAdminMCPConnectionsMaximumFilters  = 100
	notionAdminMCPConnectionsCursorVersion   = 1
	notionAdminMCPConnectionsCursorPrefix    = "hnamc1"
	notionAdminMCPConnectionsCursorDomain    = "leafen:notion-admin-mcp-connections-cursor:v1"
	notionAdminMCPConnectionsCursorMaxLength = 2048
)

var errInvalidNotionAdminMCPConnectionsCursor = leafen.NotionAdminInvalid(errors.New("invalid Notion Admin MCP connections cursor"), "start_cursor isn't one this list gave.")

type notionAdminMCPConnectionsQuery struct {
	PageSize     int
	Cursor       string
	UserIDs      []string
	WorkspaceIDs []string
	FilterHash   string
}

type notionAdminMCPConnectionsCursor struct {
	Version           int    `json:"v"`
	OrganizationID    string `json:"o"`
	TokenID           string `json:"t"`
	AuthorityRevision int64  `json:"r"`
	FilterHash        string `json:"f"`
	AfterConnectionID string `json:"a"`
}

type notionAdminMCPConnectionRow struct {
	ID                string `db:"id"`
	OwnerID           string `db:"owner_id"`
	ClientName        string `db:"client_name"`
	OAuthClientID     string `db:"oauth_client_id"`
	Created           int64  `db:"created"`
	EnterpriseManaged int    `db:"enterprise_managed"`
}

type notionAdminMCPConnectionWorkspaceRow struct {
	ConnectionID string `db:"connection_id"`
	WorkspaceID  string `db:"workspace_id"`
}

type notionAdminMCPConnectionsPage struct {
	Results           []map[string]any
	AuthorityRevision int64
	LastConnectionID  string
	HasMore           bool
}

func parseNotionAdminMCPConnectionsQuery(
	request *http.Request,
	identity leafen.NotionAdminIdentity,
) (notionAdminMCPConnectionsQuery, error) {
	query := request.URL.Query()
	for key := range query {
		if key != "cursor" && key != "page_size" && key != "user_ids" && key != "workspace_ids" {
			return notionAdminMCPConnectionsQuery{}, leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, "The query takes cursor, page_size, user_ids and workspace_ids.")
		}
	}
	if len(query["cursor"]) > 1 || len(query["page_size"]) > 1 {
		return notionAdminMCPConnectionsQuery{}, leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, "cursor and page_size go once each.")
	}
	pageSize := notionAdminMCPConnectionsDefaultPageSize
	if raw := query.Get("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > notionAdminMCPConnectionsMaximumPageSize {
			return notionAdminMCPConnectionsQuery{}, leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, fmt.Sprintf("page_size is a whole number from 1 to %d.", notionAdminMCPConnectionsMaximumPageSize))
		}
		pageSize = parsed
	}
	users, err := parseNotionAdminMCPFilterValues(query["user_ids"], leafen.ResolveNotionCompatibleUserID)
	if err != nil {
		return notionAdminMCPConnectionsQuery{}, err
	}
	workspaces, err := parseNotionAdminMCPFilterValues(query["workspace_ids"], leafen.ResolveNotionCompatibleWorkspaceID)
	if err != nil {
		return notionAdminMCPConnectionsQuery{}, err
	}
	for _, workspaceID := range workspaces {
		if !identity.CanAccessWorkspace(workspaceID) {
			return notionAdminMCPConnectionsQuery{}, errNotionAdminMCPConnectionsNotFound
		}
	}
	filterJSON, err := json.Marshal(struct {
		Users      []string `json:"u"`
		Workspaces []string `json:"w"`
	}{users, workspaces})
	if err != nil {
		return notionAdminMCPConnectionsQuery{}, err
	}
	digest := sha256.Sum256(filterJSON)
	return notionAdminMCPConnectionsQuery{
		PageSize: pageSize, Cursor: query.Get("cursor"), UserIDs: users,
		WorkspaceIDs: workspaces, FilterHash: hex.EncodeToString(digest[:]),
	}, nil
}

func parseNotionAdminMCPFilterValues(
	values []string,
	resolve func(string) (string, error),
) ([]string, error) {
	result := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			publicID := strings.TrimSpace(raw)
			if publicID == "" {
				return nil, leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, "user_ids and workspace_ids are comma-separated IDs with none empty.")
			}
			internalID, err := resolve(publicID)
			if err != nil {
				return nil, leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, "user_ids or workspace_ids has an entry that isn't a valid ID.")
			}
			if _, duplicate := seen[internalID]; duplicate {
				continue
			}
			seen[internalID] = struct{}{}
			result = append(result, internalID)
			if len(result) > notionAdminMCPConnectionsMaximumFilters {
				return nil, leafen.NotionAdminInvalid(errNotionAdminMCPConnectionsInvalid, fmt.Sprintf("At most %d user_ids or workspace_ids.", notionAdminMCPConnectionsMaximumFilters))
			}
		}
	}
	sort.Strings(result)
	return result, nil
}

func encodeNotionAdminMCPConnectionsCursor(
	secret string,
	identity leafen.NotionAdminIdentity,
	query notionAdminMCPConnectionsQuery,
	authorityRevision int64,
	afterConnectionID string,
) (string, error) {
	return leafen.EncodeNotionAdminLegalHoldScopeCursor(
		secret, notionAdminMCPConnectionsCursorPrefix, notionAdminMCPConnectionsCursorDomain,
		notionAdminMCPConnectionsCursorMaxLength, notionAdminMCPConnectionsCursor{
			Version:        notionAdminMCPConnectionsCursorVersion,
			OrganizationID: identity.OrganizationID, TokenID: identity.TokenID,
			AuthorityRevision: authorityRevision, FilterHash: query.FilterHash,
			AfterConnectionID: afterConnectionID,
		},
	)
}

func decodeNotionAdminMCPConnectionsCursor(
	raw, secret string,
	identity leafen.NotionAdminIdentity,
	query notionAdminMCPConnectionsQuery,
) (notionAdminMCPConnectionsCursor, error) {
	if raw == "" {
		return notionAdminMCPConnectionsCursor{}, nil
	}
	cursor := notionAdminMCPConnectionsCursor{}
	if err := leafen.DecodeNotionAdminLegalHoldScopeCursor(
		raw, secret, notionAdminMCPConnectionsCursorPrefix, notionAdminMCPConnectionsCursorDomain,
		notionAdminMCPConnectionsCursorMaxLength, &cursor,
	); err != nil || cursor.Version != notionAdminMCPConnectionsCursorVersion ||
		cursor.OrganizationID != identity.OrganizationID || cursor.TokenID != identity.TokenID ||
		cursor.AuthorityRevision < 1 || cursor.FilterHash != query.FilterHash || cursor.AfterConnectionID == "" {
		return notionAdminMCPConnectionsCursor{}, errInvalidNotionAdminMCPConnectionsCursor
	}
	return cursor, nil
}

func listNotionAdminMCPConnections(
	ctx context.Context,
	app core.App,
	identity leafen.NotionAdminIdentity,
	query notionAdminMCPConnectionsQuery,
	cursor notionAdminMCPConnectionsCursor,
) (notionAdminMCPConnectionsPage, error) {
	page := notionAdminMCPConnectionsPage{Results: []map[string]any{}}
	err := app.RunInTransaction(func(txApp core.App) error {
		if err := validateNotionAdminMCPListFilters(ctx, txApp, identity, query); err != nil {
			return err
		}
		if err := txApp.DB().NewQuery(`
			SELECT [[revision]] FROM {{leafen_mcp_admin_authority_revisions}}
			WHERE [[organization_id]] = {:organizationId}
		`).WithContext(ctx).Bind(dbx.Params{"organizationId": identity.OrganizationID}).Row(&page.AuthorityRevision); err != nil {
			return err
		}
		if cursor.Version != 0 && cursor.AuthorityRevision != page.AuthorityRevision {
			return errInvalidNotionAdminMCPConnectionsCursor
		}
		rows, err := loadNotionAdminMCPConnectionRows(ctx, txApp, identity, query, cursor)
		if err != nil {
			return err
		}
		page.HasMore = len(rows) > query.PageSize
		if page.HasMore {
			rows = rows[:query.PageSize]
		}
		if len(rows) == 0 {
			return nil
		}
		workspaces, err := loadNotionAdminMCPConnectionWorkspaces(ctx, txApp, identity, rows)
		if err != nil {
			return err
		}
		for _, row := range rows {
			publicUserID, err := leafen.NotionCompatibleUserID(row.OwnerID)
			if err != nil || len(workspaces[row.ID]) == 0 || row.EnterpriseManaged < 0 || row.EnterpriseManaged > 1 {
				return leafen.ErrMCPGovernanceUnavailable
			}
			publicWorkspaces := make([]map[string]any, 0, len(workspaces[row.ID]))
			for _, workspaceID := range workspaces[row.ID] {
				publicWorkspaceID, err := leafen.NotionCompatibleWorkspaceID(workspaceID)
				if err != nil {
					return err
				}
				publicWorkspaces = append(publicWorkspaces, map[string]any{"id": publicWorkspaceID})
			}
			clientKey := row.ID
			if row.OAuthClientID != "" {
				clientKey = row.OAuthClientID
			}
			page.Results = append(page.Results, map[string]any{
				"client":     map[string]any{"key": clientKey, "name": row.ClientName, "type": "other"},
				"created_at": row.Created, "is_enterprise_managed": row.EnterpriseManaged == 1,
				"user": map[string]any{"id": publicUserID}, "workspaces": publicWorkspaces,
			})
			page.LastConnectionID = row.ID
		}
		return nil
	})
	return page, err
}

func validateNotionAdminMCPListFilters(
	ctx context.Context,
	app core.App,
	identity leafen.NotionAdminIdentity,
	query notionAdminMCPConnectionsQuery,
) error {
	if len(query.UserIDs) > 0 {
		params := dbx.Params{"organizationId": identity.OrganizationID, "active": leafen.OrganizationMemberStatusActive}
		placeholders := bindNotionAdminMCPListValues(params, "user", query.UserIDs)
		var count int
		if err := app.DB().NewQuery(`
			SELECT COUNT(DISTINCT [[user]]) FROM {{leafen_organization_memberships}}
			WHERE [[organization]] = {:organizationId} AND [[status]] = {:active}
				AND [[user]] IN (` + leafen.JoinSQL(placeholders) + `)
		`).WithContext(ctx).Bind(params).Row(&count); err != nil {
			return err
		}
		if count != len(query.UserIDs) {
			return errNotionAdminMCPConnectionsNotFound
		}
	}
	if len(query.WorkspaceIDs) > 0 {
		params := dbx.Params{"organizationId": identity.OrganizationID}
		placeholders := bindNotionAdminMCPListValues(params, "workspace", query.WorkspaceIDs)
		var count int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_workspaces}}
			WHERE [[organization]] = {:organizationId} AND [[id]] IN (` + leafen.JoinSQL(placeholders) + `)
		`).WithContext(ctx).Bind(params).Row(&count); err != nil {
			return err
		}
		if count != len(query.WorkspaceIDs) {
			return errNotionAdminMCPConnectionsNotFound
		}
	}
	return nil
}

func loadNotionAdminMCPConnectionRows(
	ctx context.Context,
	app core.App,
	identity leafen.NotionAdminIdentity,
	query notionAdminMCPConnectionsQuery,
	cursor notionAdminMCPConnectionsCursor,
) ([]notionAdminMCPConnectionRow, error) {
	params := dbx.Params{
		"organizationId": identity.OrganizationID, "active": leafen.OrganizationMemberStatusActive,
		"member": leafen.WorkspaceRoleMember, "denied": mcpEnterpriseAccessDenied,
		"after": cursor.AfterConnectionID, "limit": query.PageSize + 1,
	}
	userFilter := ""
	if len(query.UserIDs) > 0 {
		userFilter = " AND [[connection]].[[owner_id]] IN (" + leafen.JoinSQL(bindNotionAdminMCPListValues(params, "user", query.UserIDs)) + ")"
	}
	workspaceFilter := ""
	if len(query.WorkspaceIDs) > 0 {
		workspaceFilter = " AND [[workspace]].[[id]] IN (" + leafen.JoinSQL(bindNotionAdminMCPListValues(params, "filterWorkspace", query.WorkspaceIDs)) + ")"
	}
	adminScope := notionAdminMCPWorkspaceScopeSQL(params, identity, "workspace")
	rows := []notionAdminMCPConnectionRow{}
	err := app.DB().NewQuery(`
		SELECT [[connection]].[[id]], [[connection]].[[owner_id]], [[connection]].[[client_name]],
			[[connection]].[[oauth_client_id]], [[connection]].[[created]],
			CASE WHEN EXISTS (
				SELECT 1 FROM {{leafen_mcp_connection_workspaces}} [[managed_scope]]
				JOIN {{leafen_workspaces}} [[managed_workspace]]
					ON [[managed_workspace]].[[id]] = [[managed_scope]].[[workspace_id]]
				JOIN {{leafen_mcp_enterprise_member_access}} [[managed_access]]
					ON [[managed_access]].[[workspace_id]] = [[managed_workspace]].[[id]]
					AND [[managed_access]].[[organization_id]] = [[managed_workspace]].[[organization]]
					AND [[managed_access]].[[user_id]] = [[connection]].[[owner_id]]
				WHERE [[managed_scope]].[[connection_id]] = [[connection]].[[id]]
					AND [[managed_workspace]].[[organization]] = {:organizationId}
			) THEN 1 ELSE 0 END AS [[enterprise_managed]]
		FROM {{leafen_mcp_connections}} [[connection]]
		WHERE [[connection]].[[revoked_at]] = 0 AND [[connection]].[[app_install_id]] = '' AND (
			{:after} = '' OR lower([[connection]].[[id]]) > lower({:after}) OR
			(lower([[connection]].[[id]]) = lower({:after}) AND [[connection]].[[id]] < {:after})
		)` + userFilter + `
			AND EXISTS (
				SELECT 1 FROM {{leafen_mcp_connection_workspaces}} [[scope]]
				JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[scope]].[[workspace_id]]
				JOIN {{leafen_organization_memberships}} [[organization_member]]
					ON [[organization_member]].[[organization]] = [[workspace]].[[organization]]
					AND [[organization_member]].[[user]] = [[connection]].[[owner_id]]
					AND [[organization_member]].[[status]] = {:active}
				WHERE [[scope]].[[connection_id]] = [[connection]].[[id]]
					AND [[workspace]].[[organization]] = {:organizationId}` + workspaceFilter + adminScope + `
					AND ([[workspace]].[[workspace_owner]] = [[connection]].[[owner_id]] OR EXISTS (
						SELECT 1 FROM {{leafen_workspace_memberships}} [[workspace_member]]
						WHERE [[workspace_member]].[[workspace_id]] = [[workspace]].[[id]]
							AND [[workspace_member]].[[user_id]] = [[connection]].[[owner_id]]
							AND [[workspace_member]].[[role]] = {:member}
					))
					AND NOT EXISTS (
						SELECT 1 FROM {{leafen_mcp_enterprise_member_access}} [[access]]
						WHERE [[access]].[[workspace_id]] = [[workspace]].[[id]]
							AND [[access]].[[organization_id]] = [[workspace]].[[organization]]
							AND [[access]].[[user_id]] = [[connection]].[[owner_id]]
							AND [[access]].[[access]] = {:denied}
					)
			)
		ORDER BY lower([[connection]].[[id]]), [[connection]].[[id]] DESC LIMIT {:limit}
	`).WithContext(ctx).Bind(params).All(&rows)
	return rows, err
}

func loadNotionAdminMCPConnectionWorkspaces(
	ctx context.Context,
	app core.App,
	identity leafen.NotionAdminIdentity,
	connections []notionAdminMCPConnectionRow,
) (map[string][]string, error) {
	params := dbx.Params{
		"organizationId": identity.OrganizationID, "active": leafen.OrganizationMemberStatusActive,
		"member": leafen.WorkspaceRoleMember, "denied": mcpEnterpriseAccessDenied,
	}
	ids := make([]string, 0, len(connections))
	for _, connection := range connections {
		ids = append(ids, connection.ID)
	}
	connectionScope := bindNotionAdminMCPListValues(params, "connection", ids)
	adminScope := notionAdminMCPWorkspaceScopeSQL(params, identity, "workspace")
	rows := []notionAdminMCPConnectionWorkspaceRow{}
	err := app.DB().NewQuery(`
		SELECT [[scope]].[[connection_id]], [[workspace]].[[id]] AS [[workspace_id]]
		FROM {{leafen_mcp_connection_workspaces}} [[scope]]
		JOIN {{leafen_mcp_connections}} [[connection]] ON [[connection]].[[id]] = [[scope]].[[connection_id]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[scope]].[[workspace_id]]
		JOIN {{leafen_organization_memberships}} [[organization_member]]
			ON [[organization_member]].[[organization]] = [[workspace]].[[organization]]
			AND [[organization_member]].[[user]] = [[connection]].[[owner_id]]
			AND [[organization_member]].[[status]] = {:active}
		WHERE [[scope]].[[connection_id]] IN (` + leafen.JoinSQL(connectionScope) + `)
			AND [[workspace]].[[organization]] = {:organizationId}` + adminScope + `
			AND ([[workspace]].[[workspace_owner]] = [[connection]].[[owner_id]] OR EXISTS (
				SELECT 1 FROM {{leafen_workspace_memberships}} [[workspace_member]]
				WHERE [[workspace_member]].[[workspace_id]] = [[workspace]].[[id]]
					AND [[workspace_member]].[[user_id]] = [[connection]].[[owner_id]]
					AND [[workspace_member]].[[role]] = {:member}
			))
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_mcp_enterprise_member_access}} [[access]]
				WHERE [[access]].[[workspace_id]] = [[workspace]].[[id]]
					AND [[access]].[[user_id]] = [[connection]].[[owner_id]]
					AND [[access]].[[access]] = {:denied}
			)
		ORDER BY [[scope]].[[connection_id]], [[workspace]].[[id]]
	`).WithContext(ctx).Bind(params).All(&rows)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]string, len(connections))
	for _, row := range rows {
		result[row.ConnectionID] = append(result[row.ConnectionID], row.WorkspaceID)
	}
	return result, nil
}

func notionAdminMCPWorkspaceScopeSQL(params dbx.Params, identity leafen.NotionAdminIdentity, alias string) string {
	if _, wildcard := identity.WorkspaceIDs()["*"]; wildcard {
		return ""
	}
	workspaceIDs := make([]string, 0, len(identity.WorkspaceIDs()))
	for workspaceID := range identity.WorkspaceIDs() {
		workspaceIDs = append(workspaceIDs, workspaceID)
	}
	sort.Strings(workspaceIDs)
	if len(workspaceIDs) == 0 {
		return " AND FALSE"
	}
	placeholders := bindNotionAdminMCPListValues(params, "adminWorkspace", workspaceIDs)
	return fmt.Sprintf(" AND [[%s]].[[id]] IN (%s)", alias, leafen.JoinSQL(placeholders))
}

func bindNotionAdminMCPListValues(params dbx.Params, prefix string, values []string) []string {
	placeholders := make([]string, 0, len(values))
	for index, value := range values {
		key := fmt.Sprintf("%s%d", prefix, index)
		params[key] = value
		placeholders = append(placeholders, "{:"+key+"}")
	}
	return placeholders
}
