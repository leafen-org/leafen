package organization

import (
	"context"
	"database/sql"
	"errors"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func updateNotionAdminAgentCreationPolicy(
	ctx context.Context, app core.App, secret string, expected leafen.NotionAdminIdentity,
	workspaceID string, request notionAdminAgentCreationPolicyRequest,
) (notionAdminAgentCreationPolicyResponse, error) {
	release := leafen.AcquireProductOperationLock(notionAdminAgentMutationLockNamespace, workspaceID)
	defer release()
	response := notionAdminAgentCreationPolicyResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, authority, err := authorizeCurrentNotionAdminAgentMutation(
			ctx, txApp, secret, expected, workspaceID,
		)
		if err != nil {
			return err
		}
		changed := !authority.CreationPolicy.Valid || authority.CreationPolicy.String != request.Policy
		disable := false
		if request.DisableExistingAgents && request.Policy == "disabled" {
			var activeCustomAgentExists int
			err = txApp.DB().NewQuery(`SELECT EXISTS(
				SELECT 1 FROM {{leafen_notion_agents}}
				WHERE [[workspace_id]] = {:workspaceId} AND [[agent_type]] = 'custom'
					AND [[deleted_at_ms]] IS NULL AND [[admin_status]] = 'active'
					AND [[acknowledged_disable_generation]] >= {:generation}
			)`).WithContext(ctx).Bind(dbx.Params{
				"workspaceId": workspaceID, "generation": authority.DisableGeneration,
			}).Row(&activeCustomAgentExists)
			if err != nil {
				return err
			}
			disable = activeCustomAgentExists == 1
		}
		if changed || disable {
			now := time.Now().UTC().UnixMilli()
			increment := 0
			if disable {
				increment = 1
			}
			_, err = txApp.DB().NewQuery(`
				UPDATE {{leafen_notion_agent_settings}}
				SET [[creation_policy]] = {:policy},
					[[disable_generation]] = [[disable_generation]] + {:increment},
					[[updated_at_ms]] = {:now}
				WHERE [[workspace_id]] = {:workspaceId}
			`).WithContext(ctx).Bind(dbx.Params{
				"policy": request.Policy, "increment": increment, "now": now,
				"workspaceId": workspaceID,
			}).Execute()
			if err != nil {
				return err
			}
			if err := finalizeNotionAdminAgentMutation(
				txApp, current, "creation_policy.update", "workspace", workspaceID,
			); err != nil {
				return err
			}
			authority, err = loadNotionAdminAgentAuthority(ctx, txApp, current.OrganizationID, workspaceID)
			if err != nil {
				return err
			}
		}
		counts, err := countNotionAdminAgentStatuses(ctx, txApp, authority, workspaceID)
		if err != nil {
			return err
		}
		spaceID, err := leafen.NotionCompatibleWorkspaceID(workspaceID)
		if err != nil {
			return err
		}
		response = notionAdminAgentCreationPolicyResponse{
			AgentCounts: counts, CurrentPolicy: request.Policy,
			CurrentPolicyDescription: notionAdminAgentCreationPolicyDescription(request.Policy),
			SpaceID:                  spaceID, SpaceName: authority.WorkspaceName,
		}
		return nil
	})
	return response, err
}

func updateNotionAdminWorkspaceCreditLimit(
	ctx context.Context, app core.App, secret string, expected leafen.NotionAdminIdentity,
	workspaceID string, limit *int64,
) (notionAdminAgentWorkspaceCreditLimitResponse, error) {
	release := leafen.AcquireProductOperationLock(notionAdminAgentMutationLockNamespace, workspaceID)
	defer release()
	response := notionAdminAgentWorkspaceCreditLimitResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, authority, err := authorizeCurrentNotionAdminAgentMutation(ctx, txApp, secret, expected, workspaceID)
		if err != nil {
			return err
		}
		if !nullableInt64Equal(authority.DefaultCreditLimit, limit) {
			_, err = txApp.DB().NewQuery(`UPDATE {{leafen_notion_agent_settings}}
				SET [[default_agent_credit_limit]] = {:limit}, [[updated_at_ms]] = {:now}
				WHERE [[workspace_id]] = {:workspaceId}
			`).WithContext(ctx).Bind(dbx.Params{
				"limit": limit, "now": time.Now().UTC().UnixMilli(), "workspaceId": workspaceID,
			}).Execute()
			if err != nil {
				return err
			}
			if err := finalizeNotionAdminAgentMutation(
				txApp, current, "workspace_credit_limit.update", "workspace", workspaceID,
			); err != nil {
				return err
			}
		}
		spaceID, err := leafen.NotionCompatibleWorkspaceID(workspaceID)
		response = notionAdminAgentWorkspaceCreditLimitResponse{SpaceID: spaceID, DefaultAgentCreditLimit: limit}
		return err
	})
	return response, err
}

func updateNotionAdminAgentStatus(
	ctx context.Context, app core.App, secret string, expected leafen.NotionAdminIdentity,
	workspaceID, agentID, adminStatus string,
) (notionAdminAgentStatusResponse, error) {
	release := leafen.AcquireProductOperationLock(notionAdminAgentMutationLockNamespace, workspaceID)
	defer release()
	response := notionAdminAgentStatusResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, authority, err := authorizeCurrentNotionAdminAgentMutation(ctx, txApp, secret, expected, workspaceID)
		if err != nil {
			return err
		}
		agent, err := loadStoredNotionAdminAgent(ctx, txApp, workspaceID, agentID)
		if err != nil || agent.DeletedAtMS.Valid {
			return errors.Join(errNotionAdminAgentNotFound, err)
		}
		acknowledged := agent.AcknowledgedDisableGeneration
		if adminStatus == "active" {
			acknowledged = authority.DisableGeneration
		}
		if agent.AdminStatus != adminStatus || agent.AcknowledgedDisableGeneration != acknowledged {
			now := time.Now().UTC().UnixMilli()
			_, err = txApp.DB().NewQuery(`UPDATE {{leafen_notion_agents}}
				SET [[admin_status]] = {:status}, [[acknowledged_disable_generation]] = {:acknowledged},
					[[last_edited_time_ms]] = max([[last_edited_time_ms]], {:now}), [[revision]] = [[revision]] + 1
				WHERE [[workspace_id]] = {:workspaceId} AND [[id]] = {:agentId} AND [[deleted_at_ms]] IS NULL
			`).WithContext(ctx).Bind(dbx.Params{
				"status": adminStatus, "acknowledged": acknowledged, "now": now,
				"workspaceId": workspaceID, "agentId": agentID,
			}).Execute()
			if err != nil {
				return err
			}
			if err := finalizeNotionAdminAgentMutation(txApp, current, "status.update", "agent", agentID); err != nil {
				return err
			}
			agent.AdminStatus = adminStatus
			agent.AcknowledgedDisableGeneration = acknowledged
		}
		agent, err = loadStoredNotionAdminAgent(ctx, txApp, workspaceID, agentID)
		if err != nil {
			return err
		}
		response = notionAdminAgentStatusResponseFor(agent)
		return nil
	})
	return response, err
}

func deleteNotionAdminAgent(
	ctx context.Context, app core.App, secret string, expected leafen.NotionAdminIdentity,
	workspaceID, agentID string,
) error {
	release := leafen.AcquireProductOperationLock(notionAdminAgentMutationLockNamespace, workspaceID)
	defer release()
	return app.RunInTransaction(func(txApp core.App) error {
		current, _, err := authorizeCurrentNotionAdminAgentMutation(ctx, txApp, secret, expected, workspaceID)
		if err != nil {
			return err
		}
		agent, err := loadStoredNotionAdminAgent(ctx, txApp, workspaceID, agentID)
		if err != nil {
			return err
		}
		if agent.DeletedAtMS.Valid {
			return nil
		}
		now := time.Now().UTC().UnixMilli()
		_, err = txApp.DB().NewQuery(`UPDATE {{leafen_notion_agents}}
			SET [[deleted_at_ms]] = {:now}, [[last_edited_time_ms]] = max([[last_edited_time_ms]], {:now}),
				[[revision]] = [[revision]] + 1
			WHERE [[workspace_id]] = {:workspaceId} AND [[id]] = {:agentId} AND [[deleted_at_ms]] IS NULL
		`).WithContext(ctx).Bind(dbx.Params{"now": now, "workspaceId": workspaceID, "agentId": agentID}).Execute()
		if err != nil {
			return err
		}
		return finalizeNotionAdminAgentMutation(txApp, current, "delete", "agent", agentID)
	})
}

func updateNotionAdminAgentCreditLimit(
	ctx context.Context, app core.App, secret string, expected leafen.NotionAdminIdentity,
	workspaceID, agentID string, limit *int64,
) (notionAdminAgentCreditLimitResponse, error) {
	release := leafen.AcquireProductOperationLock(notionAdminAgentMutationLockNamespace, workspaceID)
	defer release()
	response := notionAdminAgentCreditLimitResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, _, err := authorizeCurrentNotionAdminAgentMutation(ctx, txApp, secret, expected, workspaceID)
		if err != nil {
			return err
		}
		agent, err := loadStoredNotionAdminAgent(ctx, txApp, workspaceID, agentID)
		if err != nil || agent.DeletedAtMS.Valid {
			return errors.Join(errNotionAdminAgentNotFound, err)
		}
		usage := agent.CurrentCreditUsage
		oldLimit := nullableInt64Pointer(agent.EnforcedCreditLimit)
		if !nullableInt64Equal(agent.AdminCreditLimit, limit) {
			_, err = txApp.DB().NewQuery(`UPDATE {{leafen_notion_agents}}
				SET [[admin_credit_limit]] = {:limit}, [[revision]] = [[revision]] + 1,
					[[last_edited_time_ms]] = max([[last_edited_time_ms]], {:now})
				WHERE [[workspace_id]] = {:workspaceId} AND [[id]] = {:agentId} AND [[deleted_at_ms]] IS NULL
			`).WithContext(ctx).Bind(dbx.Params{
				"limit": limit, "now": time.Now().UTC().UnixMilli(),
				"workspaceId": workspaceID, "agentId": agentID,
			}).Execute()
			if err != nil {
				return err
			}
			if err := finalizeNotionAdminAgentMutation(txApp, current, "credit_limit.update", "agent", agentID); err != nil {
				return err
			}
			agent.AdminCreditLimit = sql.NullInt64{}
			if limit != nil {
				agent.AdminCreditLimit = sql.NullInt64{Int64: *limit, Valid: true}
			}
		}
		agent, err = loadStoredNotionAdminAgent(ctx, txApp, workspaceID, agentID)
		if err != nil {
			return err
		}
		newLimit := nullableInt64Pointer(agent.EnforcedCreditLimit)
		status := notionAdminAgentStatusResponseFor(agent)
		response = notionAdminAgentCreditLimitResponse{
			CreditLimitByWorkspaceAdmin: limit, EnforcedCreditLimit: newLimit,
			CurrentCreditUsage: nullableInt64Pointer(usage), IsActive: status.RunStatus == "active",
		}
		response.DidClearPausedState = usage.Valid && oldLimit != nil && usage.Int64 >= *oldLimit &&
			(newLimit == nil || usage.Int64 < *newLimit)
		return nil
	})
	return response, err
}

func authorizeCurrentNotionAdminAgentMutation(
	ctx context.Context, app core.App, secret string, expected leafen.NotionAdminIdentity, workspaceID string,
) (leafen.NotionAdminIdentity, notionAdminAgentAuthority, error) {
	current, err := leafen.AuthenticateNotionAdminToken(ctx, app, secret, time.Now().UTC())
	if err != nil {
		return leafen.NotionAdminIdentity{}, notionAdminAgentAuthority{}, err
	}
	if current.OrganizationID != expected.OrganizationID || current.TokenID != expected.TokenID ||
		current.CreatedBy != expected.CreatedBy || !current.HasCapability(leafen.NotionAdminCapabilityWorkflowsWrite) ||
		!current.CanAccessWorkspace(workspaceID) {
		return leafen.NotionAdminIdentity{}, notionAdminAgentAuthority{}, errNotionAdminAgentForbidden
	}
	authority, err := loadNotionAdminAgentAuthority(ctx, app, current.OrganizationID, workspaceID)
	return current, authority, err
}

func countNotionAdminAgentStatuses(
	ctx context.Context, app core.App, authority notionAdminAgentAuthority, workspaceID string,
) (notionAdminAgentCounts, error) {
	counts := notionAdminAgentCounts{}
	err := app.DB().NewQuery(`SELECT
		COALESCE(SUM(CASE WHEN [[deleted_at_ms]] IS NULL AND [[admin_status]] = 'active'
			AND ([[agent_type]] != 'custom' OR [[acknowledged_disable_generation]] >= {:generation})
			THEN 1 ELSE 0 END), 0) AS [[active]],
		COALESCE(SUM(CASE WHEN [[deleted_at_ms]] IS NOT NULL THEN 1 ELSE 0 END), 0) AS [[deleted]],
		COALESCE(SUM(CASE WHEN [[deleted_at_ms]] IS NULL AND ([[admin_status]] = 'disabled'
			OR ([[agent_type]] = 'custom' AND [[acknowledged_disable_generation]] < {:generation}))
			THEN 1 ELSE 0 END), 0) AS [[disabled]]
		FROM {{leafen_notion_agents}} WHERE [[workspace_id]] = {:workspaceId}
	`).WithContext(ctx).Bind(dbx.Params{
		"generation": authority.DisableGeneration, "workspaceId": workspaceID,
	}).One(&counts)
	return counts, err
}

func nullableInt64Equal(stored sql.NullInt64, value *int64) bool {
	return !stored.Valid && value == nil || stored.Valid && value != nil && stored.Int64 == *value
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func notionAdminAgentCreationPolicyDescription(policy string) string {
	switch policy {
	case "disabled":
		return "Agent creation is disabled for this workspace."
	case "workspace_owners_only":
		return "Only workspace owners can create agents."
	default:
		return "All workspace members can create agents."
	}
}

func finalizeNotionAdminAgentMutation(
	app core.App, identity leafen.NotionAdminIdentity, action, subjectType, subjectID string,
) error {
	if err := leafen.BumpOrganizationPolicy(app, identity.OrganizationID, time.Now().UTC().UnixMilli()); err != nil {
		return err
	}
	return leafen.RecordOrganizationAudit(
		app, identity.OrganizationID, identity.CreatedBy,
		"notion_admin.agent."+action, subjectType, subjectID,
	)
}
