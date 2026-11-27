package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionAdminPATAuthority struct {
	OwnerID  string `db:"owner_id"`
	Revision int64  `db:"revision"`
}

type storedNotionPersonalAccessToken struct {
	ID                   string        `db:"id"`
	WorkspaceID          string        `db:"workspace_id"`
	CreatorID            string        `db:"creator_id"`
	TokenHash            string        `db:"token_hash"`
	TokenPrefix          string        `db:"token_prefix"`
	Name                 string        `db:"name"`
	Status               string        `db:"status"`
	CapabilitiesJSON     string        `db:"capabilities_json"`
	CredentialGeneration int           `db:"credential_generation"`
	CreatedAt            int64         `db:"created_at"`
	ExpiresAt            int64         `db:"expires_at"`
	LastUsedAt           int64         `db:"last_used_at"`
	RevokedAt            sql.NullInt64 `db:"revoked_at"`
	UpdatedAt            int64         `db:"updated_at"`
	ProjectedStatus      string        `db:"projected_status"`
}

type notionAdminPATCreatorResponse struct {
	ID string `json:"id"`
}

type notionAdminPATResponse struct {
	ID         string                        `json:"id"`
	Creator    notionAdminPATCreatorResponse `json:"creator"`
	CreatedAt  int64                         `json:"created_at"`
	Status     string                        `json:"status"`
	Name       string                        `json:"name,omitempty"`
	ExpiresAt  *int64                        `json:"expires_at,omitempty"`
	LastUsedAt *int64                        `json:"last_used_at,omitempty"`
	RevokedAt  *int64                        `json:"revoked_at,omitempty"`
}

type notionAdminPATListResponse struct {
	Results    []notionAdminPATResponse `json:"results"`
	HasMore    bool                     `json:"has_more"`
	NextCursor *string                  `json:"next_cursor,omitempty"`
}

func listNotionAdminPersonalAccessTokens(
	ctx context.Context,
	app core.App,
	secret string,
	identity notionAdminIdentity,
	workspaceID string,
	cursor notionAdminPATCursor,
	filters notionAdminPATFilters,
	pageSize int,
) (notionAdminPATListResponse, error) {
	response := notionAdminPATListResponse{Results: []notionAdminPATResponse{}}
	asOf := cursor.AsOf
	if asOf == 0 {
		asOf = time.Now().UTC().UnixMilli()
	}
	authorityRevision := int64(0)
	afterID := cursor.AfterID
	lastID := ""
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		authority, err := loadNotionAdminPATAuthority(
			ctx, txApp, identity.OrganizationID, workspaceID,
		)
		if err != nil {
			return err
		}
		authorityRevision = authority.Revision
		if cursor.Version != 0 && cursor.Revision != authority.Revision {
			return errInvalidNotionAdminPATCursor
		}
		query, params := notionAdminPATListStatement(workspaceID, afterID, asOf, filters, pageSize+1)
		rows := []storedNotionPersonalAccessToken{}
		if err := txApp.DB().NewQuery(query).WithContext(ctx).Bind(params).All(&rows); err != nil {
			return err
		}
		response.HasMore = len(rows) > pageSize
		if response.HasMore {
			rows = rows[:pageSize]
		}
		for _, row := range rows {
			projected, err := projectNotionAdminPAT(row)
			if err != nil {
				return err
			}
			response.Results = append(response.Results, projected)
			lastID = row.ID
		}
		return nil
	})
	if err != nil {
		return notionAdminPATListResponse{}, err
	}
	if response.HasMore {
		next, err := encodeNotionAdminPATCursor(
			secret, identity, workspaceID, authorityRevision, asOf, lastID, filters,
		)
		if err != nil {
			return notionAdminPATListResponse{}, err
		}
		response.NextCursor = &next
	}
	return response, nil
}

func notionAdminPATListStatement(
	workspaceID, afterID string,
	asOf int64,
	filters notionAdminPATFilters,
	limit int,
) (string, dbx.Params) {
	params := dbx.Params{
		"workspaceId": workspaceID, "afterId": afterID, "asOf": asOf, "limit": limit,
	}
	conditions := []string{"[[workspace_id]] = {:workspaceId}", "[[id]] > {:afterId}"}
	statusExpression := `CASE WHEN [[status]] = 'revoked' THEN 'revoked'
		WHEN [[expires_at]] <= {:asOf} THEN 'expired' ELSE 'active' END`
	if len(filters.Statuses) > 0 {
		placeholders := make([]string, 0, len(filters.Statuses))
		for index, status := range filters.Statuses {
			key := fmt.Sprintf("status%d", index)
			params[key] = status
			placeholders = append(placeholders, "{:"+key+"}")
		}
		conditions = append(conditions, statusExpression+" IN ("+joinSQL(placeholders)+")")
	}
	if len(filters.CreatorIDs) > 0 {
		placeholders := make([]string, 0, len(filters.CreatorIDs))
		for index, creatorID := range filters.CreatorIDs {
			key := fmt.Sprintf("creator%d", index)
			params[key] = creatorID
			placeholders = append(placeholders, "{:"+key+"}")
		}
		conditions = append(conditions, "[[creator_id]] IN ("+joinSQL(placeholders)+")")
	}
	if filters.Search != "" {
		params["search"] = escapeNotionAdminPATLike(filters.Search)
		conditions = append(conditions, `lower([[name]]) LIKE '%' || lower({:search}) || '%' ESCAPE '\'`)
	}
	return `
		SELECT [[id]], [[workspace_id]], [[creator_id]], [[token_hash]], [[token_prefix]],
			[[name]], [[status]], [[capabilities_json]], [[credential_generation]],
			[[created_at]], [[expires_at]], [[last_used_at]], [[revoked_at]], [[updated_at]],
			` + statusExpression + ` AS [[projected_status]]
		FROM {{leafen_notion_personal_access_tokens}}
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY [[id]] LIMIT {:limit}
	`, params
}

func escapeNotionAdminPATLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func projectNotionAdminPAT(stored storedNotionPersonalAccessToken) (notionAdminPATResponse, error) {
	publicID, err := notionPersonalAccessTokenPublicID(stored.ID)
	if err != nil {
		return notionAdminPATResponse{}, err
	}
	creatorID, err := notionCompatibleUserID(stored.CreatorID)
	if err != nil {
		return notionAdminPATResponse{}, err
	}
	expiresAt := stored.ExpiresAt
	response := notionAdminPATResponse{
		ID: publicID, Creator: notionAdminPATCreatorResponse{ID: creatorID},
		CreatedAt: stored.CreatedAt, Status: stored.ProjectedStatus,
		Name: stored.Name, ExpiresAt: &expiresAt,
	}
	if stored.LastUsedAt > 0 {
		lastUsedAt := stored.LastUsedAt
		response.LastUsedAt = &lastUsedAt
	}
	if stored.RevokedAt.Valid {
		revokedAt := stored.RevokedAt.Int64
		response.RevokedAt = &revokedAt
	}
	return response, nil
}

func loadNotionAdminPATAuthority(
	ctx context.Context,
	app core.App,
	organizationID, workspaceID string,
) (notionAdminPATAuthority, error) {
	authority := notionAdminPATAuthority{}
	err := app.DB().NewQuery(`
		SELECT [[workspace]].[[owner]] AS [[owner_id]], [[revision]].[[revision]]
		FROM {{leafen_workspaces}} [[workspace]]
		JOIN {{leafen_notion_personal_access_token_revisions}} [[revision]]
			ON [[revision]].[[workspace_id]] = [[workspace]].[[id]]
		WHERE [[workspace]].[[id]] = {:workspaceId}
			AND [[workspace]].[[organization]] = {:organizationId}
		LIMIT 2
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "organizationId": organizationID,
	}).One(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return notionAdminPATAuthority{}, errNotionAdminPATNotFound
	}
	return authority, err
}

func revokeNotionAdminPersonalAccessToken(
	ctx context.Context,
	app core.App,
	secret string,
	identity notionAdminIdentity,
	workspaceID, tokenID string,
) error {
	release := acquireNotionPersonalAccessTokenLifecycleLock(tokenID)
	defer release()
	return app.RunInTransaction(func(txApp core.App) error {
		current, err := authenticateNotionAdminToken(ctx, txApp, secret, time.Now().UTC())
		if err != nil {
			return err
		}
		if current.TokenID != identity.TokenID || current.OrganizationID != identity.OrganizationID ||
			!current.HasCapability(notionAdminCapabilityPersonalAccessTokenWriteHighImpact) ||
			!current.CanAccessWorkspace(workspaceID) {
			return errNotionAdminPATForbidden
		}
		if _, err := loadNotionAdminPATAuthority(ctx, txApp, current.OrganizationID, workspaceID); err != nil {
			return err
		}
		stored := storedNotionPersonalAccessToken{}
		err = txApp.DB().NewQuery(`
			SELECT [[id]], [[workspace_id]], [[creator_id]], [[token_hash]], [[token_prefix]],
				[[name]], [[status]], [[capabilities_json]], [[credential_generation]],
				[[created_at]], [[expires_at]], [[last_used_at]], [[revoked_at]], [[updated_at]],
				[[status]] AS [[projected_status]]
			FROM {{leafen_notion_personal_access_tokens}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[id]] = {:tokenId} LIMIT 2
		`).WithContext(ctx).Bind(dbx.Params{"workspaceId": workspaceID, "tokenId": tokenID}).One(&stored)
		if errors.Is(err, sql.ErrNoRows) {
			return errNotionAdminPATNotFound
		}
		if err != nil || stored.Status == "revoked" {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_personal_access_tokens}}
			SET [[status]] = 'revoked', [[revoked_at]] = {:now}, [[updated_at]] = {:now}
			WHERE [[id]] = {:tokenId} AND [[workspace_id]] = {:workspaceId} AND [[status]] = 'active'
		`).WithContext(ctx).Bind(dbx.Params{
			"now": now, "tokenId": tokenID, "workspaceId": workspaceID,
		}).Execute()
		return err
	})
}
