package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// The workspace owner's policies of Notion's "연결 → 관리 → 사용 권한
// 관리하기" and "이모지" (seen 2026-10-02): who may make personal access
// tokens — every member or the workspace's owners only, a member's tokens
// stopping in the API once members may not — whether automations and
// buttons may send webhooks, failing them when not, and whether only owners
// add custom emoji; and "공개 페이지 → 설정"'s "페이지가 Notion 사이트로
// 게시되었음을 항상 표시" (on by default), the banner over a page published as
// a site. Tokens may also be the owners' and chosen groups'
// (workspace_policy_groups.go). Its "사용자 지정 MCP 서버 활성화하기" and the
// servers its people may connect are workspace_custom_mcp_policy.go's.
// Notion's limits on installed and internal connections aren't Leafen's.

const (
	workspaceConnectionPoliciesPath     = "/workspaces/{workspaceId}/connection-policies"
	workspacePolicyMembers              = "members"
	workspacePolicyOwners               = "owners"
	errorCodeWorkspaceConnectionInvalid = "LEAFEN_WORKSPACE_CONNECTION_INVALID"
)

var errWorkspaceConnectionPolicyInvalid = errors.New(errorCodeWorkspaceConnectionInvalid)

type workspaceConnectionPolicies struct {
	PersonalAccessTokens string `json:"personalAccessTokens" db:"personal_access_tokens"`
	// The groups whose people may make tokens beside the owners.
	PersonalAccessTokenGroups []string `json:"personalAccessTokenGroups" db:"-"`
	AutomationWebhooks        bool     `json:"automationWebhooks" db:"automation_webhooks"`
	CustomEmojiOwnersOnly     bool     `json:"customEmojiOwnersOnly" db:"custom_emoji_owners_only"`
	// A page published as a site shows so in a banner over it.
	SiteBanner      bool   `json:"siteBanner" db:"site_banner"`
	TokenGroupsJSON string `json:"-" db:"personal_access_token_groups"`
	// The workspace's people may connect their agents to custom MCP
	// servers, those the list names where it names any.
	CustomMCPServers       bool     `json:"customMcpServers" db:"custom_mcp_servers"`
	CustomMCPAllowlist     []string `json:"customMcpAllowlist" db:"-"`
	CustomMCPAllowlistJSON string   `json:"-" db:"custom_mcp_allowlist"`
}

type workspaceConnectionPoliciesResponse struct {
	workspaceConnectionPolicies
	CanManage bool `json:"canManage"`
	// The reader may make personal access tokens.
	CanMakeTokens bool `json:"canMakeTokens"`
	// The organization's groups an owner may name, a window at a time: with
	// groupChoicesNextCursor, ?groupsCursor= reads the next.
	GroupChoices           []workspacePolicyGroup `json:"groupChoices,omitempty"`
	GroupChoicesNextCursor string                 `json:"groupChoicesNextCursor,omitempty"`
}

// A change names only the policies it sets.
type workspaceConnectionPoliciesInput struct {
	PersonalAccessTokens      *string   `json:"personalAccessTokens"`
	PersonalAccessTokenGroups *[]string `json:"personalAccessTokenGroups"`
	AutomationWebhooks        *bool     `json:"automationWebhooks"`
	CustomEmojiOwnersOnly     *bool     `json:"customEmojiOwnersOnly"`
	SiteBanner                *bool     `json:"siteBanner"`
	CustomMCPServers          *bool     `json:"customMcpServers"`
	CustomMCPAllowlist        *[]string `json:"customMcpAllowlist"`
}

// loadWorkspaceConnectionPolicies reads a workspace's policies; a workspace
// that set none has Notion's defaults.
func loadWorkspaceConnectionPolicies(app core.App, workspaceID string) (workspaceConnectionPolicies, error) {
	policies := workspaceConnectionPolicies{}
	err := app.DB().NewQuery(`
		SELECT [[personal_access_tokens]], [[personal_access_token_groups]], [[automation_webhooks]],
			[[custom_emoji_owners_only]], [[site_banner]], [[custom_mcp_servers]], [[custom_mcp_allowlist]]
		FROM {{leafen_workspace_connection_policies}} WHERE [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).One(&policies)
	if errors.Is(err, sql.ErrNoRows) {
		return workspaceConnectionPolicies{
			PersonalAccessTokens: workspacePolicyMembers, PersonalAccessTokenGroups: []string{}, AutomationWebhooks: true,
			SiteBanner: true, CustomMCPServers: true, CustomMCPAllowlist: []string{},
		}, nil
	}
	if err != nil {
		return policies, err
	}
	policies.PersonalAccessTokenGroups = []string{}
	if json.Unmarshal([]byte(policies.TokenGroupsJSON), &policies.PersonalAccessTokenGroups) != nil {
		policies.PersonalAccessTokenGroups = []string{}
	}
	policies.CustomMCPAllowlist = []string{}
	if json.Unmarshal([]byte(policies.CustomMCPAllowlistJSON), &policies.CustomMCPAllowlist) != nil {
		policies.CustomMCPAllowlist = []string{}
	}
	return policies, nil
}

func registerWorkspaceConnectionPolicyRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+workspaceConnectionPoliciesPath, readWorkspaceConnectionPolicies)
	event.Router.PATCH(apiRoot+workspaceConnectionPoliciesPath, changeWorkspaceConnectionPolicies).
		Bind(apis.BodyLimit(256 << 10))
}

func readWorkspaceConnectionPolicies(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if err != nil {
		return workspaceConnectionError(event, err, "policies-read")
	}
	policies, err := loadWorkspaceConnectionPolicies(event.App, workspaceID)
	if err != nil {
		return workspaceConnectionError(event, err, "policies-read")
	}
	allowed := requireWorkspacePolicy(event.App, workspaceID, user.Id, authorization.Role, personalAccessTokensLimit, errPersonalAccessTokenOwners)
	if allowed != nil && !errors.Is(allowed, errPersonalAccessTokenOwners) {
		return workspaceConnectionError(event, allowed, "policies-read")
	}
	response := workspaceConnectionPoliciesResponse{
		workspaceConnectionPolicies: policies, CanManage: authorization.Role == workspaceRoleOwner, CanMakeTokens: allowed == nil,
	}
	if response.CanManage {
		if response.GroupChoices, response.GroupChoicesNextCursor, err = workspacePolicyGroupChoices(
			event.App, authorization.Organization.OrganizationID, event.Request.URL.Query().Get("groupsCursor"),
		); errors.Is(err, errWorkspaceConnectionPolicyInvalid) {
			return apiError(event, http.StatusBadRequest, errorCodeWorkspaceConnectionInvalid, "Read the groups again from the start.")
		} else if err != nil {
			return workspaceConnectionError(event, err, "policies-read")
		}
	}
	return event.JSON(http.StatusOK, response)
}

func changeWorkspaceConnectionPolicies(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := workspaceConnectionPoliciesInput{}
	bound := event.BindBody(&input)
	// Each is a change of its own; one at least is sent (api_missing_input.go).
	if bound == nil && input.PersonalAccessTokens == nil && input.AutomationWebhooks == nil && input.CustomEmojiOwnersOnly == nil &&
		input.SiteBanner == nil && input.CustomMCPServers == nil && input.CustomMCPAllowlist == nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceConnectionInvalid, "Send at least one of personalAccessTokens, "+
			"automationWebhooks, customEmojiOwnersOnly, siteBanner, customMcpServers and customMcpAllowlist to change.")
	}
	if bound != nil ||
		input.PersonalAccessTokens != nil && *input.PersonalAccessTokens != workspacePolicyMembers &&
			*input.PersonalAccessTokens != workspacePolicyOwners &&
			*input.PersonalAccessTokens != workspacePolicyOwnersAndGroups {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceConnectionInvalid, "Check the policies.")
	}
	policies := workspaceConnectionPolicies{}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceOwner)
		if err != nil {
			return err
		}
		policies, err = loadWorkspaceConnectionPolicies(txApp, workspaceID)
		if err != nil {
			return err
		}
		if input.PersonalAccessTokens != nil {
			policies.PersonalAccessTokens = *input.PersonalAccessTokens
		}
		if input.PersonalAccessTokenGroups != nil {
			valid, err := validWorkspacePolicyGroups(txApp, authorization.Organization.OrganizationID, *input.PersonalAccessTokenGroups)
			if err != nil {
				return err
			}
			if !valid {
				return errWorkspaceConnectionPolicyInvalid
			}
			policies.PersonalAccessTokenGroups = *input.PersonalAccessTokenGroups
		}
		groups, err := json.Marshal(policies.PersonalAccessTokenGroups)
		if err != nil {
			return err
		}
		if input.AutomationWebhooks != nil {
			policies.AutomationWebhooks = *input.AutomationWebhooks
		}
		if input.CustomEmojiOwnersOnly != nil {
			policies.CustomEmojiOwnersOnly = *input.CustomEmojiOwnersOnly
		}
		if input.SiteBanner != nil {
			policies.SiteBanner = *input.SiteBanner
		}
		if input.CustomMCPServers != nil {
			policies.CustomMCPServers = *input.CustomMCPServers
		}
		if input.CustomMCPAllowlist != nil {
			allowlist, valid := normalizeCustomMCPAllowlist(*input.CustomMCPAllowlist)
			if !valid {
				return errWorkspaceConnectionPolicyInvalid
			}
			policies.CustomMCPAllowlist = allowlist
		}
		allowlist, err := json.Marshal(policies.CustomMCPAllowlist)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_connection_policies}}
				([[workspace_id]], [[personal_access_tokens]], [[personal_access_token_groups]], [[automation_webhooks]],
				 [[custom_emoji_owners_only]], [[site_banner]], [[custom_mcp_servers]], [[custom_mcp_allowlist]],
				 [[updated_by]], [[updated_at]])
			VALUES ({:workspaceId}, {:tokens}, {:groups}, {:webhooks}, {:emoji}, {:siteBanner}, {:customMCP}, {:allowlist},
				{:actor}, {:now})
			ON CONFLICT ([[workspace_id]]) DO UPDATE SET
				[[personal_access_tokens]] = excluded.[[personal_access_tokens]],
				[[personal_access_token_groups]] = excluded.[[personal_access_token_groups]],
				[[automation_webhooks]] = excluded.[[automation_webhooks]],
				[[custom_emoji_owners_only]] = excluded.[[custom_emoji_owners_only]],
				[[site_banner]] = excluded.[[site_banner]],
				[[custom_mcp_servers]] = excluded.[[custom_mcp_servers]],
				[[custom_mcp_allowlist]] = excluded.[[custom_mcp_allowlist]],
				[[updated_by]] = excluded.[[updated_by]], [[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{
			"workspaceId": workspaceID, "tokens": policies.PersonalAccessTokens, "groups": string(groups),
			"webhooks": policies.AutomationWebhooks, "emoji": policies.CustomEmojiOwnersOnly,
			"siteBanner": policies.SiteBanner, "customMCP": policies.CustomMCPServers, "allowlist": string(allowlist),
			"actor": user.Id, "now": now,
		}).Execute(); err != nil {
			return err
		}
		// Each change is its own event.
		changeID := workspaceID + "@" + strconv.FormatInt(now, 10)
		return insertOrganizationAuditEvent(txApp, organizationAuditEvent{
			ID:             organizationAuditID(authorization.Organization.OrganizationID, "workspace.connection_policies.update", changeID),
			OrganizationID: authorization.Organization.OrganizationID, WorkspaceID: workspaceID,
			ActorID: user.Id, Action: "workspace.connection_policies.update",
			TargetType: "workspace", TargetID: workspaceID,
			Metadata: map[string]any{
				"personalAccessTokens": policies.PersonalAccessTokens, "automationWebhooks": policies.AutomationWebhooks,
				"customEmojiOwnersOnly": policies.CustomEmojiOwnersOnly, "siteBanner": policies.SiteBanner,
				"customMcpServers": policies.CustomMCPServers, "customMcpAllowlist": policies.CustomMCPAllowlist,
				"changedAt": now,
			},
			OccurredAt: now,
		})
	})
	if errors.Is(err, errWorkspaceConnectionPolicyInvalid) {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceConnectionInvalid, "Check the policies.")
	}
	if err != nil {
		return workspaceConnectionError(event, err, "policies-change")
	}
	return event.JSON(http.StatusOK, workspaceConnectionPoliciesResponse{
		workspaceConnectionPolicies: policies, CanManage: true, CanMakeTokens: true,
	})
}
