package organization

import (
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func registerEnterpriseMCPGovernanceRoutes(event *core.ServeEvent) {
	event.Router.GET(leafen.APIRoot+leafen.MCPGovernanceReadPolicyPath, getMCPGovernancePolicy)
	event.Router.PUT(leafen.APIRoot+leafen.MCPGovernanceUpdatePolicyPath, updateMCPGovernancePolicy).
		Bind(apis.BodyLimit(leafen.MCPGovernanceMaxBodyBytes))
	event.Router.POST(leafen.APIRoot+leafen.MCPGovernanceApproveClientPath, approveMCPGovernanceClientRoute).
		Bind(apis.BodyLimit(leafen.MCPGovernanceMaxBodyBytes))
	event.Router.PATCH(leafen.APIRoot+leafen.MCPGovernanceRenameClientPath, renameMCPGovernanceClientRoute).
		Bind(apis.BodyLimit(leafen.MCPGovernanceMaxBodyBytes))
	event.Router.DELETE(leafen.APIRoot+leafen.MCPGovernanceRemoveClientPath, removeMCPGovernanceClientRoute)
}

func getMCPGovernancePolicy(event *core.RequestEvent) error {
	authorization, workspaceID, ok := authorizeMCPGovernanceManagement(event)
	if !ok {
		return nil
	}
	policy, err := loadMCPGovernancePolicy(event.App, authorization.OrganizationID, workspaceID)
	if err != nil {
		return leafen.MCPGovernanceManagementError(event, err, "policy-read")
	}
	return event.JSON(http.StatusOK, policy)
}

func updateMCPGovernancePolicy(event *core.RequestEvent) error {
	authorization, workspaceID, ok := authorizeMCPGovernanceManagement(event)
	if !ok {
		return nil
	}
	input := leafen.MCPGovernancePolicyInput{}
	if err := event.BindBody(&input); err != nil {
		return leafen.MCPGovernanceManagementError(event, leafen.ErrMCPGovernanceInvalid, "policy-bind")
	}
	// A key's call asks its person only for a change that would be made
	// (internal/app/user_key_acting_ask.go).
	if err := leafen.RehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := saveMCPGovernanceEnabled(txApp, authorization, workspaceID, input.Enabled)
		return err
	}); err != nil {
		return leafen.MCPGovernanceManagementError(event, err, "policy-write")
	}
	if asked, err := leafen.AskBeforeActing(event); asked {
		return err
	}
	policy, err := saveMCPGovernanceEnabled(event.App, authorization, workspaceID, input.Enabled)
	if err != nil {
		return leafen.MCPGovernanceManagementError(event, err, "policy-write")
	}
	return event.JSON(http.StatusOK, policy)
}

func approveMCPGovernanceClientRoute(event *core.RequestEvent) error {
	authorization, workspaceID, ok := authorizeMCPGovernanceManagement(event)
	if !ok {
		return nil
	}
	input := leafen.MCPGovernanceApprovedClientInput{}
	if err := event.BindBody(&input); err != nil {
		return leafen.MCPGovernanceManagementError(event, leafen.ErrMCPGovernanceInvalid, "client-bind")
	}
	// A key's call asks its person only for a change that would be made
	// (internal/app/user_key_acting_ask.go).
	if err := leafen.RehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := approveMCPGovernanceClient(txApp, authorization, workspaceID, input)
		return err
	}); err != nil {
		return leafen.MCPGovernanceManagementError(event, err, "client-approve")
	}
	if asked, err := leafen.AskBeforeActing(event); asked {
		return err
	}
	policy, err := approveMCPGovernanceClient(event.App, authorization, workspaceID, input)
	if err != nil {
		return leafen.MCPGovernanceManagementError(event, err, "client-approve")
	}
	return event.JSON(http.StatusOK, policy)
}

func renameMCPGovernanceClientRoute(event *core.RequestEvent) error {
	authorization, workspaceID, ok := authorizeMCPGovernanceManagement(event)
	if !ok {
		return nil
	}
	input := leafen.MCPGovernanceRenameClientInput{}
	if err := event.BindBody(&input); err != nil {
		return leafen.MCPGovernanceManagementError(event, leafen.ErrMCPGovernanceInvalid, "client-bind")
	}
	policy, err := renameMCPGovernanceClient(
		event.App, authorization, workspaceID, event.Request.PathValue("clientId"), input,
	)
	if err != nil {
		return leafen.MCPGovernanceManagementError(event, err, "client-rename")
	}
	return event.JSON(http.StatusOK, policy)
}

func removeMCPGovernanceClientRoute(event *core.RequestEvent) error {
	authorization, workspaceID, ok := authorizeMCPGovernanceManagement(event)
	if !ok {
		return nil
	}
	// A key's call asks its person only for a change that would be made
	// (internal/app/user_key_acting_ask.go).
	if err := leafen.RehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := removeMCPGovernanceClient(
			txApp, authorization, workspaceID, event.Request.PathValue("clientId"),
		)
		return err
	}); err != nil {
		return leafen.MCPGovernanceManagementError(event, err, "client-remove")
	}
	if asked, err := leafen.AskBeforeActing(event); asked {
		return err
	}
	policy, err := removeMCPGovernanceClient(
		event.App, authorization, workspaceID, event.Request.PathValue("clientId"),
	)
	if err != nil {
		return leafen.MCPGovernanceManagementError(event, err, "client-remove")
	}
	return event.JSON(http.StatusOK, policy)
}

func authorizeMCPGovernanceManagement(
	event *core.RequestEvent,
) (leafen.OrganizationAuthorization, string, bool) {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		_ = leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
		return leafen.OrganizationAuthorization{}, "", false
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	authorization, err := leafen.AuthorizeOrganization(
		event.App, user.Id, organizationID, leafen.PermissionOrganizationIntegrationsAdmin,
	)
	if err == nil {
		err = assertMCPGovernanceWorkspace(event.App, organizationID, workspaceID)
	}
	if leafen.IsPermissionDenied(err) {
		_ = leafen.APIError(event, http.StatusNotFound, leafen.ErrorCodeOrganizationNotFound, "Organization not found.")
		return leafen.OrganizationAuthorization{}, "", false
	}
	if err != nil {
		_ = leafen.APIErrorWithCause(
			event, http.StatusInternalServerError, leafen.ErrorCodeOrganizationAuthorityInvalid,
			"Couldn't check organization permission.", "enterprise_mcp_governance", "management-authorization",
			leafen.ErrorCodeOrganizationAuthorityInvalid, err,
		)
		return leafen.OrganizationAuthorization{}, "", false
	}
	return authorization, workspaceID, true
}
