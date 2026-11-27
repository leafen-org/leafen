package app

import (
	"database/sql"
	"errors"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person's connection being made — an MCP token by hand, or an agent
// connection by a code, a device code or the command — is checked against
// its workspaces' organizations and given its workspaces
// (connection_person_rights.go).

// authorizeManualMCPCompletion checks a token or an agent connection being
// made against its workspaces: an MCP token's, each one's organization
// allowing it; an agent connection's — all its person's —, one of theirs at
// least that allows it, those that don't holding it out at each call as they
// do every key (userKeyPolicyDenial, filterMCPClientApprovedWorkspaces).
func authorizeManualMCPCompletion(app core.App, ownerID string, connectionID string, input mcpConnectionInput,
	created int64, expiresAt int64) error {
	if err := authorizeWorkspaceSet(app, ownerID, input.WorkspaceIDs, permissionWorkspaceRead); err != nil {
		return err
	}
	agent := agentConnectionRequest(input.RequestID)
	if !agent {
		storedIDs, err := loadMCPConnectionWorkspaces(app, connectionID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if len(storedIDs) > 0 && !slices.Equal(storedIDs, input.WorkspaceIDs) {
			return errMCPConnectionRequestIDReused
		}
	}
	var refused error
	for _, workspaceID := range input.WorkspaceIDs {
		err := mcpConnectionWorkspaceAdmits(app, ownerID, connectionID, input.ClientName, workspaceID, created, expiresAt)
		switch {
		case err == nil && agent:
			return nil
		case err == nil:
		case !agent:
			return err
		case refused == nil:
			refused = err
		}
	}
	return refused
}

// mcpConnectionWorkspaceAdmits says whether a workspace's organization
// admits a key of the person's made by hand or by code: one it allows keys
// of this life, and agents connected so.
func mcpConnectionWorkspaceAdmits(app core.App, ownerID, connectionID, clientName, workspaceID string, created, expiresAt int64) error {
	if denial, err := userKeyPolicyDenial(app, userKeyAuthority{Scope: userKeyScopeAdmin, Created: created, ExpiresAt: expiresAt},
		userKeyScopeAdmin, []string{workspaceID}); err != nil {
		return err
	} else if denial != "" {
		return userKeyPolicyError{denial}
	}
	workspaces, err := loadMCPGovernanceWorkspaces(app, []string{workspaceID})
	if err != nil {
		return err
	}
	return assertMCPClientApprovedForWorkspaces(app, workspaces, mcpGovernanceContext{
		ActorID: ownerID, ClientID: mcpGovernanceManualClientID, ClientName: clientName,
		ConnectionID: connectionID, Stage: "manual-admission",
	})
}

// How a new connection reaches its person's workspaces: an agent
// connection all of them, now and later; an MCP token the one it was made
// for alone, as Notion's; an app granted over OAuth those it was granted,
// and those made later too when they were all of them.
type mcpConnectionReach int

const (
	mcpConnectionReachesOne mcpConnectionReach = iota
	mcpConnectionReachesAll
	mcpConnectionReachesAsChosen
)

// insertMCPConnectionWorkspaces gives a person's new connection its
// workspaces, and the reach that keeps them (user_key_reach_gained.go).
func insertMCPConnectionWorkspaces(app core.App, connectionID, ownerID string, workspaceIDs []string, created int64,
	reach mcpConnectionReach) error {
	for _, workspaceID := range workspaceIDs {
		if _, err := app.DB().Insert(mcpConnectionWorkspacesTable, dbx.Params{
			"connection_id": connectionID, "workspace_id": workspaceID, "created": created,
		}).Execute(); err != nil {
			return err
		}
	}
	all := reach == mcpConnectionReachesAll
	if reach == mcpConnectionReachesAsChosen {
		var err error
		if all, err = userKeyChoosesAll(app, ownerID, workspaceIDs); err != nil {
			return err
		}
	}
	if !all {
		return nil
	}
	_, err := app.DB().Update(mcpConnectionsTable, dbx.Params{"all_workspaces": 1}, dbx.HashExp{"id": connectionID}).Execute()
	return err
}
