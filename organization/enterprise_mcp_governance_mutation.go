package organization

import (
	"errors"
	"strconv"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

func saveMCPGovernanceEnabled(
	app core.App,
	authorization leafen.OrganizationAuthorization,
	workspaceID string,
	enabled bool,
) (leafen.MCPGovernancePolicyResponse, error) {
	response := leafen.MCPGovernancePolicyResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		currentAuthorization, err := leafen.AuthorizeOrganization(
			txApp, authorization.PrincipalID, authorization.OrganizationID, leafen.PermissionOrganizationIntegrationsAdmin,
		)
		if err != nil {
			return err
		}
		if err := assertMCPGovernanceWorkspace(txApp, authorization.OrganizationID, workspaceID); err != nil {
			return err
		}
		current, err := loadMCPGovernancePolicy(txApp, authorization.OrganizationID, workspaceID)
		if err != nil {
			return err
		}
		if current.Enabled == enabled {
			response = current
			return nil
		}
		revision := current.Revision + 1
		now := time.Now().UTC().UnixMilli()
		if err := upsertMCPGovernancePolicy(txApp, authorization.OrganizationID, workspaceID, enabled, revision, now); err != nil {
			return err
		}
		if err := finishMCPGovernanceMutation(
			txApp, currentAuthorization, workspaceID, revision,
			"organization.mcp_governance.enabled", now,
		); err != nil {
			return err
		}
		response, err = loadMCPGovernancePolicy(txApp, authorization.OrganizationID, workspaceID)
		return err
	})
	return response, err
}

func approveMCPGovernanceClient(
	app core.App,
	authorization leafen.OrganizationAuthorization,
	workspaceID string,
	input leafen.MCPGovernanceApprovedClientInput,
) (leafen.MCPGovernancePolicyResponse, error) {
	clientID, err := leafen.NormalizeMCPGovernanceClientID(input.ClientID)
	if err != nil {
		return leafen.MCPGovernancePolicyResponse{}, err
	}
	name, err := normalizeMCPGovernanceClientName(input.Name)
	if err != nil {
		return leafen.MCPGovernancePolicyResponse{}, err
	}
	return mutateMCPGovernanceClient(app, authorization, workspaceID, "approve", clientID, name)
}

func renameMCPGovernanceClient(
	app core.App,
	authorization leafen.OrganizationAuthorization,
	workspaceID string,
	clientID string,
	input leafen.MCPGovernanceRenameClientInput,
) (leafen.MCPGovernancePolicyResponse, error) {
	clientID, err := leafen.NormalizeMCPGovernanceClientID(clientID)
	if err != nil {
		return leafen.MCPGovernancePolicyResponse{}, err
	}
	name, err := normalizeMCPGovernanceClientName(input.Name)
	if err != nil {
		return leafen.MCPGovernancePolicyResponse{}, err
	}
	return mutateMCPGovernanceClient(app, authorization, workspaceID, "rename", clientID, name)
}

func removeMCPGovernanceClient(
	app core.App,
	authorization leafen.OrganizationAuthorization,
	workspaceID string,
	clientID string,
) (leafen.MCPGovernancePolicyResponse, error) {
	clientID, err := leafen.NormalizeMCPGovernanceClientID(clientID)
	if err != nil {
		return leafen.MCPGovernancePolicyResponse{}, err
	}
	return mutateMCPGovernanceClient(app, authorization, workspaceID, "remove", clientID, "")
}

func mutateMCPGovernanceClient(
	app core.App,
	authorization leafen.OrganizationAuthorization,
	workspaceID string,
	operation string,
	clientID string,
	name string,
) (leafen.MCPGovernancePolicyResponse, error) {
	response := leafen.MCPGovernancePolicyResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		currentAuthorization, err := leafen.AuthorizeOrganization(
			txApp, authorization.PrincipalID, authorization.OrganizationID, leafen.PermissionOrganizationIntegrationsAdmin,
		)
		if err != nil {
			return err
		}
		if err := assertMCPGovernanceWorkspace(txApp, authorization.OrganizationID, workspaceID); err != nil {
			return err
		}
		current, err := loadMCPGovernancePolicy(txApp, authorization.OrganizationID, workspaceID)
		if err != nil {
			return err
		}
		existing, exists, err := loadStoredMCPGovernanceClient(
			txApp, authorization.OrganizationID, workspaceID, clientID,
		)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		revision := current.Revision + 1
		switch operation {
		case "approve":
			if exists {
				if existing.Name != name {
					return leafen.ErrMCPGovernanceInvalid
				}
				response = current
				return nil
			}
			if err := upsertMCPGovernancePolicy(txApp, authorization.OrganizationID, workspaceID, current.Enabled, revision, now); err != nil {
				return err
			}
			err = insertMCPGovernanceClient(
				txApp, authorization.OrganizationID, workspaceID, clientID, name,
				currentAuthorization.PrincipalID, now,
			)
		case "rename":
			if !exists {
				return leafen.ErrMCPGovernanceInvalid
			}
			if existing.Name == name {
				response = current
				return nil
			}
			if err := upsertMCPGovernancePolicy(txApp, authorization.OrganizationID, workspaceID, current.Enabled, revision, now); err != nil {
				return err
			}
			err = updateMCPGovernanceClientName(
				txApp, authorization.OrganizationID, workspaceID, clientID, name,
				currentAuthorization.PrincipalID, now,
			)
		case "remove":
			if !exists {
				response = current
				return nil
			}
			if err := upsertMCPGovernancePolicy(txApp, authorization.OrganizationID, workspaceID, current.Enabled, revision, now); err != nil {
				return err
			}
			err = deleteMCPGovernanceClient(txApp, authorization.OrganizationID, workspaceID, clientID)
		default:
			return leafen.ErrMCPGovernanceInvalid
		}
		if err != nil {
			return errors.Join(leafen.ErrMCPGovernanceUnavailable, err)
		}
		if err := finishMCPGovernanceMutation(
			txApp, currentAuthorization, workspaceID, revision,
			"organization.mcp_governance.client_"+operation, now,
		); err != nil {
			return err
		}
		response, err = loadMCPGovernancePolicy(txApp, authorization.OrganizationID, workspaceID)
		return err
	})
	return response, err
}

func finishMCPGovernanceMutation(
	app core.App,
	authorization leafen.OrganizationAuthorization,
	workspaceID string,
	revision int,
	action string,
	now int64,
) error {
	if err := leafen.BumpOrganizationPolicy(app, authorization.OrganizationID, now); err != nil {
		return err
	}
	return leafen.RecordOrganizationAudit(
		app, authorization.OrganizationID, authorization.PrincipalID,
		action, "mcp_workspace_policy", workspaceID+":"+strconv.Itoa(revision),
	)
}
