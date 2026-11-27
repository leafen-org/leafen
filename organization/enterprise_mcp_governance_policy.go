package organization

import (
	stdcontext "context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

func normalizeMCPGovernanceClientName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > leafen.MCPGovernanceMaxClientNameRunes {
		return "", leafen.ErrMCPGovernanceInvalid
	}
	return value, nil
}

func filterApprovedMCPClientWorkspaces(
	app core.App,
	workspaces []leafen.MCPGovernanceWorkspace,
	clientID string,
	actorID string,
) ([]leafen.MCPGovernanceWorkspace, error) {
	if len(workspaces) == 0 {
		return []leafen.MCPGovernanceWorkspace{}, nil
	}
	approved := map[string]struct{}{}
	check := func(txApp core.App) error {
		decisions, err := loadMCPGovernanceDecisions(txApp, workspaces, clientID)
		if err != nil {
			return err
		}
		memberAccess, err := loadMCPEnterpriseMemberAccess(txApp, workspaces, actorID)
		if err != nil {
			return err
		}
		for _, decision := range decisions {
			access := memberAccess[decision.WorkspaceID]
			if decision.Approved == 1 && access.Access != mcpEnterpriseAccessDenied {
				approved[decision.WorkspaceID] = struct{}{}
			}
		}
		return nil
	}
	// It reads in a snapshot of its own, not on the one write connection:
	// every agent call is checked here, and a check that waited for a
	// long write held the call with it (2026-11-16).
	if err := leafen.RunReadSnapshot(stdcontext.Background(), app, check); err != nil {
		return nil, err
	}
	result := make([]leafen.MCPGovernanceWorkspace, 0, len(approved))
	for _, workspace := range workspaces {
		if _, ok := approved[workspace.ID]; ok {
			result = append(result, workspace)
		}
	}
	return result, nil
}

func assertApprovedMCPClient(
	app core.App,
	workspaces []leafen.MCPGovernanceWorkspace,
	context leafen.MCPGovernanceContext,
) error {
	if len(workspaces) == 0 {
		return leafen.ErrMCPGovernanceInvalid
	}
	clientID, err := leafen.NormalizeMCPGovernanceClientID(context.ClientID)
	if err != nil {
		return err
	}
	context.ClientID = clientID
	var denied *storedMCPGovernanceDecision
	check := func(txApp core.App) error {
		denied = nil
		decisions, err := loadMCPGovernanceDecisions(txApp, workspaces, clientID)
		if err != nil {
			return err
		}
		memberAccess, err := loadMCPEnterpriseMemberAccess(txApp, workspaces, context.ActorID)
		if err != nil {
			return err
		}
		for index := range decisions {
			access := memberAccess[decisions[index].WorkspaceID]
			if decisions[index].Approved == 0 || access.Access == mcpEnterpriseAccessDenied {
				denied = &decisions[index]
				break
			}
		}
		return nil
	}
	// It reads in a snapshot of its own, as the filter above; only a
	// refusal writes, its audit record.
	err = leafen.RunReadSnapshot(stdcontext.Background(), app, check)
	if err == nil && denied != nil {
		subject := strings.Join([]string{
			context.ClientID, denied.WorkspaceID, context.Stage,
			context.ConnectionID, strconv.FormatInt(time.Now().UTC().UnixNano(), 10),
		}, ":")
		record := func(txApp core.App) error {
			return leafen.RecordOrganizationAudit(
				txApp, denied.OrganizationID, context.ActorID,
				"organization.mcp_governance.blocked", "mcp_client", subject,
			)
		}
		if app.IsTransactional() {
			err = record(app)
		} else {
			err = app.RunInTransaction(record)
		}
	}
	if err != nil {
		return errors.Join(leafen.ErrMCPGovernanceUnavailable, err)
	}
	if denied != nil {
		return leafen.ErrMCPClientNotApproved
	}
	return nil
}
