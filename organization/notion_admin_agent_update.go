package organization

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's update-agent (PATCH /v1/spaces/{space_id}/agents/{agent_id})
// changes an agent's owner, whose credits it spends, its two monthly credit
// limits and its admin status in one request, the status last, and answers
// with the agent. Leafen holds no agents of its own: no member may
// own one, so an owner is refused and every agent spends the workspace's
// credits; its limits and status are kept as the operations beside it keep
// them.

type notionAdminAgentUpdateRequest struct {
	OwnerUserID                 *string                       `json:"owner_user_id"`
	CreditSource                *string                       `json:"credit_source"`
	CreditLimitByWorkspaceAdmin notionAdminNullableInt64Input `json:"credit_limit_by_workspace_admin"`
	CreditLimitByEditor         notionAdminNullableInt64Input `json:"credit_limit_by_editor"`
	AdminStatus                 *string                       `json:"admin_status"`
}

func serveNotionAdminAgentUpdate(
	event *core.RequestEvent, secret string, identity leafen.NotionAdminIdentity, workspaceID, agentID string,
) error {
	if len(event.Request.URL.Query()) != 0 {
		return notionAdminAgentError(event, leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "This request takes no query parameters."))
	}
	request := notionAdminAgentUpdateRequest{}
	if err := decodeNotionAdminAgentBody(event, &request); err != nil {
		return notionAdminAgentError(event, err)
	}
	if err := validateNotionAdminAgentUpdate(request); err != nil {
		return notionAdminAgentError(event, err)
	}
	response, err := updateNotionAdminAgent(
		event.Request.Context(), event.App, secret, identity, workspaceID, agentID, request,
	)
	if err != nil {
		return notionAdminAgentError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func validateNotionAdminAgentUpdate(request notionAdminAgentUpdateRequest) error {
	if request.OwnerUserID != nil {
		if _, err := leafen.ResolveNotionCompatibleUserID(strings.TrimSpace(*request.OwnerUserID)); err != nil {
			return leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "owner_user_id should be a user ID.")
		}
	}
	if request.CreditSource != nil && !governanceOneOf(*request.CreditSource, "owner", "workspace") {
		return leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "credit_source takes owner or workspace.")
	}
	for _, limit := range []notionAdminNullableInt64Input{request.CreditLimitByWorkspaceAdmin, request.CreditLimitByEditor} {
		if limit.Present && limit.Value != nil && *limit.Value < 0 {
			return leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "A credit limit is a number not below 0, or null.")
		}
	}
	if request.AdminStatus != nil && !governanceOneOf(*request.AdminStatus, "active", "disabled") {
		return leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "admin_status takes active or disabled.")
	}
	return nil
}

func updateNotionAdminAgent(
	ctx context.Context, app core.App, secret string, expected leafen.NotionAdminIdentity,
	workspaceID, agentID string, request notionAdminAgentUpdateRequest,
) (notionAdminAgentResponse, error) {
	release := leafen.AcquireProductOperationLock(notionAdminAgentMutationLockNamespace, workspaceID)
	defer release()
	response := notionAdminAgentResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, authority, err := authorizeCurrentNotionAdminAgentMutation(ctx, txApp, secret, expected, workspaceID)
		if err != nil {
			return err
		}
		agent, err := loadStoredNotionAdminAgent(ctx, txApp, workspaceID, agentID)
		if err != nil || agent.DeletedAtMS.Valid {
			return errors.Join(errNotionAdminAgentNotFound, err)
		}
		if request.OwnerUserID != nil {
			return leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "owner_user_id should be a member allowed to own custom agents; no member owns one in Leafen.")
		}
		if request.CreditSource != nil && *request.CreditSource == "owner" {
			return leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "credit_source owner needs the agent to have an owner, and it has none.")
		}
		adminLimit := nullableInt64Pointer(agent.AdminCreditLimit)
		if request.CreditLimitByWorkspaceAdmin.Present {
			adminLimit = request.CreditLimitByWorkspaceAdmin.Value
		}
		editorLimit := nullableInt64Pointer(agent.CreatorCreditLimit)
		if request.CreditLimitByEditor.Present {
			editorLimit = request.CreditLimitByEditor.Value
		}
		if adminLimit != nil && editorLimit != nil && *editorLimit > *adminLimit {
			return leafen.NotionAdminInvalid(errNotionAdminAgentInvalid, "credit_limit_by_editor can't be above credit_limit_by_workspace_admin.")
		}
		adminStatus, acknowledged := agent.AdminStatus, agent.AcknowledgedDisableGeneration
		if request.AdminStatus != nil {
			adminStatus = *request.AdminStatus
			if adminStatus == "active" {
				acknowledged = authority.DisableGeneration
			}
		}
		if !nullableInt64Equal(agent.AdminCreditLimit, adminLimit) || !nullableInt64Equal(agent.CreatorCreditLimit, editorLimit) ||
			agent.AdminStatus != adminStatus || agent.AcknowledgedDisableGeneration != acknowledged {
			_, err = txApp.DB().NewQuery(`UPDATE {{leafen_notion_agents}}
				SET [[admin_credit_limit]] = {:adminLimit}, [[creator_credit_limit]] = {:editorLimit},
					[[admin_status]] = {:status}, [[acknowledged_disable_generation]] = {:acknowledged},
					[[last_edited_time_ms]] = max([[last_edited_time_ms]], {:now}), [[revision]] = [[revision]] + 1
				WHERE [[workspace_id]] = {:workspaceId} AND [[id]] = {:agentId} AND [[deleted_at_ms]] IS NULL
			`).WithContext(ctx).Bind(dbx.Params{
				"adminLimit": adminLimit, "editorLimit": editorLimit, "status": adminStatus,
				"acknowledged": acknowledged, "now": time.Now().UTC().UnixMilli(),
				"workspaceId": workspaceID, "agentId": agentID,
			}).Execute()
			if err != nil {
				return err
			}
			if err := finalizeNotionAdminAgentMutation(txApp, current, "update", "agent", agentID); err != nil {
				return err
			}
		}
		agent, err = loadStoredNotionAdminAgent(ctx, txApp, workspaceID, agentID)
		if err != nil {
			return err
		}
		response, err = projectNotionAdminAgent(agent)
		return err
	})
	return response, err
}
