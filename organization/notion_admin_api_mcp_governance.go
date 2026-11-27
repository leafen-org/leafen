package organization

import (
	"context"
	"errors"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func updateNotionAdminMCPEnterpriseAccessInTransaction(
	ctx context.Context,
	app core.App,
	secret string,
	expected leafen.NotionAdminIdentity,
	access string,
	userID string,
	workspaceID string,
) (int, error) {
	revoked := 0
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := authorizeCurrentNotionAdminMCPMutation(
			ctx, txApp, secret, expected, workspaceID,
		)
		if err != nil {
			return err
		}
		if err := assertNotionAdminMCPGovernanceTarget(
			ctx, txApp, current.OrganizationID, userID, workspaceID,
		); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_mcp_enterprise_member_access}} (
				[[organization_id]], [[workspace_id]], [[user_id]], [[access]],
				[[revision]], [[updated_at]], [[updated_by]]
			) VALUES (
				{:organizationId}, {:workspaceId}, {:userId}, {:access}, 1, {:now}, {:updatedBy}
			)
			ON CONFLICT ([[workspace_id]], [[user_id]]) DO UPDATE SET
				[[organization_id]] = excluded.[[organization_id]],
				[[access]] = excluded.[[access]],
				[[revision]] = [[revision]] + 1,
				[[updated_at]] = excluded.[[updated_at]],
				[[updated_by]] = excluded.[[updated_by]]
		`).WithContext(ctx).Bind(dbx.Params{
			"organizationId": current.OrganizationID, "workspaceId": workspaceID,
			"userId": userID, "access": access, "now": now, "updatedBy": current.CreatedBy,
		}).Execute(); err != nil {
			return err
		}
		if access == mcpEnterpriseAccessDenied {
			revoked, _, err = revokeNotionAdminMCPConnectionSet(
				txApp,
				leafen.MCPConnectionRevocationFilter{OwnerID: userID, WorkspaceID: workspaceID},
				now,
			)
			if err != nil {
				return err
			}
		}
		if err := leafen.BumpOrganizationPolicy(txApp, current.OrganizationID, now); err != nil {
			return err
		}
		return leafen.RecordOrganizationAudit(
			txApp, current.OrganizationID, current.CreatedBy,
			"notion_admin.mcp_client.enterprise_access."+access,
			"mcp_member_access", workspaceID+":"+userID,
		)
	})
	return revoked, err
}

func revokeNotionAdminMCPConnectionsInTransaction(
	ctx context.Context,
	app core.App,
	secret string,
	expected leafen.NotionAdminIdentity,
	clientKey string,
	userID string,
	workspaceID string,
) (int, error) {
	revoked := 0
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := authorizeCurrentNotionAdminMCPMutation(
			ctx, txApp, secret, expected, workspaceID,
		)
		if err != nil {
			return err
		}
		if err := assertNotionAdminMCPGovernanceTarget(
			ctx, txApp, current.OrganizationID, userID, workspaceID,
		); err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		revoked, _, err = revokeNotionAdminMCPConnectionSet(
			txApp,
			leafen.MCPConnectionRevocationFilter{
				ClientKey: clientKey, OwnerID: userID, WorkspaceID: workspaceID,
			},
			now,
		)
		if err != nil {
			return err
		}
		if revoked == 0 {
			return nil
		}
		return leafen.RecordOrganizationAudit(
			txApp, current.OrganizationID, current.CreatedBy,
			"notion_admin.mcp_client.revoke", "mcp_client", clientKey+":"+workspaceID+":"+userID,
		)
	})
	return revoked, err
}

func authorizeCurrentNotionAdminMCPMutation(
	ctx context.Context,
	app core.App,
	secret string,
	expected leafen.NotionAdminIdentity,
	workspaceID string,
) (leafen.NotionAdminIdentity, error) {
	current, err := leafen.AuthenticateNotionAdminToken(ctx, app, secret, time.Now().UTC())
	if err != nil {
		return leafen.NotionAdminIdentity{}, err
	}
	if current.OrganizationID != expected.OrganizationID || current.TokenID != expected.TokenID ||
		current.CreatedBy != expected.CreatedBy ||
		!current.HasCapability(leafen.NotionAdminCapabilityMCPClientConnectionWriteHighImpact) ||
		!current.CanAccessWorkspace(workspaceID) {
		return leafen.NotionAdminIdentity{}, errNotionAdminMCPConnectionsForbidden
	}
	return current, nil
}

func assertNotionAdminMCPGovernanceTarget(
	ctx context.Context,
	app core.App,
	organizationID string,
	userID string,
	workspaceID string,
) error {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*)
		FROM {{leafen_workspaces}} [[workspace]]
		JOIN {{leafen_organization_memberships}} [[organization_member]]
			ON [[organization_member]].[[organization]] = [[workspace]].[[organization]]
			AND [[organization_member]].[[user]] = {:userId}
			AND [[organization_member]].[[status]] = {:active}
		WHERE [[workspace]].[[id]] = {:workspaceId}
			AND [[workspace]].[[organization]] = {:organizationId}
			AND ([[workspace]].[[workspace_owner]] = {:userId} OR EXISTS (
				SELECT 1 FROM {{leafen_workspace_memberships}} [[workspace_member]]
				WHERE [[workspace_member]].[[workspace_id]] = [[workspace]].[[id]]
					AND [[workspace_member]].[[user_id]] = {:userId}
					AND [[workspace_member]].[[role]] = {:member}
			))
	`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID, "workspaceId": workspaceID, "userId": userID,
		"active": leafen.OrganizationMemberStatusActive, "member": leafen.WorkspaceRoleMember,
	}).Row(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return errNotionAdminMCPConnectionsNotFound
	}
	return nil
}

func notionAdminMCPGovernanceError(err error) error {
	if errors.Is(err, leafen.ErrMCPGovernanceInvalid) {
		return errNotionAdminMCPConnectionsInvalid
	}
	return err
}
