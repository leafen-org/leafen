package organization

import (
	"database/sql"
	"encoding/json"
	"errors"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedMCPGovernancePolicy struct {
	OrganizationID string `db:"organization_id"`
	WorkspaceID    string `db:"workspace_id"`
	Enabled        int    `db:"enabled"`
	Revision       int    `db:"revision"`
	UpdatedAt      int64  `db:"updated_at"`
}

type storedMCPGovernanceClient struct {
	ClientID string `db:"client_id"`
	Name     string `db:"name"`
}

type storedMCPGovernanceDecision struct {
	WorkspaceID    string `db:"workspace_id"`
	OrganizationID string `db:"organization_id"`
	PolicyPresent  int    `db:"policy_present"`
	PolicyEnabled  int    `db:"policy_enabled"`
	PolicyRevision int    `db:"policy_revision"`
	PolicyUpdated  int64  `db:"policy_updated"`
	ClientPresent  int    `db:"client_present"`
	ClientName     string `db:"client_name"`
	Approved       int    `db:"approved"`
}

func loadMCPGovernancePolicy(
	app core.App,
	organizationID string,
	workspaceID string,
) (leafen.MCPGovernancePolicyResponse, error) {
	response := leafen.MCPGovernancePolicyResponse{ApprovedClients: []leafen.MCPGovernanceApprovedClientResponse{}}
	policy := storedMCPGovernancePolicy{}
	err := app.DB().NewQuery(`
		SELECT [[organization_id]], [[workspace_id]], [[enabled]], [[revision]], [[updated_at]]
		FROM {{leafen_organization_mcp_workspace_policies}}
		WHERE [[organization_id]] = {:organizationId} AND [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"organizationId": organizationID, "workspaceId": workspaceID}).One(&policy)
	if errors.Is(err, sql.ErrNoRows) {
		return response, nil
	}
	if err != nil {
		return leafen.MCPGovernancePolicyResponse{}, err
	}
	if policy.OrganizationID != organizationID || policy.WorkspaceID != workspaceID ||
		(policy.Enabled != 0 && policy.Enabled != 1) || policy.Revision < 1 || policy.UpdatedAt < 0 {
		return leafen.MCPGovernancePolicyResponse{}, leafen.ErrMCPGovernanceUnavailable
	}
	clients := []storedMCPGovernanceClient{}
	if err := app.DB().NewQuery(`
		SELECT [[client_id]], [[name]]
		FROM {{leafen_organization_mcp_approved_clients}}
		WHERE [[organization_id]] = {:organizationId} AND [[workspace_id]] = {:workspaceId}
		ORDER BY [[client_id]]
	`).Bind(dbx.Params{"organizationId": organizationID, "workspaceId": workspaceID}).All(&clients); err != nil {
		return leafen.MCPGovernancePolicyResponse{}, err
	}
	response.Enabled = policy.Enabled == 1
	response.Revision = policy.Revision
	for _, client := range clients {
		clientID, idErr := leafen.NormalizeMCPGovernanceClientID(client.ClientID)
		name, nameErr := normalizeMCPGovernanceClientName(client.Name)
		if idErr != nil || nameErr != nil || clientID != client.ClientID || name != client.Name {
			return leafen.MCPGovernancePolicyResponse{}, leafen.ErrMCPGovernanceUnavailable
		}
		response.ApprovedClients = append(response.ApprovedClients, leafen.MCPGovernanceApprovedClientResponse{
			ClientID: client.ClientID,
			Name:     client.Name,
		})
	}
	return response, nil
}

func loadMCPGovernanceDecisions(
	app core.App,
	workspaces []leafen.MCPGovernanceWorkspace,
	clientID string,
) ([]storedMCPGovernanceDecision, error) {
	clientID, err := leafen.NormalizeMCPGovernanceClientID(clientID)
	if err != nil {
		return nil, err
	}
	if len(workspaces) == 0 {
		return nil, leafen.ErrMCPGovernanceInvalid
	}
	params := dbx.Params{"clientId": clientID}
	ids := make([]string, 0, len(workspaces))
	expected := make(map[string]string, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.ID == "" || workspace.OrganizationID == "" {
			return nil, leafen.ErrMCPGovernanceInvalid
		}
		if existing, duplicate := expected[workspace.ID]; duplicate {
			if existing != workspace.OrganizationID {
				return nil, leafen.ErrMCPGovernanceInvalid
			}
			continue
		}
		expected[workspace.ID] = workspace.OrganizationID
		ids = append(ids, workspace.ID)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	params["workspaceIds"] = string(encoded)
	rows := []storedMCPGovernanceDecision{}
	err = app.DB().NewQuery(`
		SELECT [[workspace]].[[id]] AS [[workspace_id]],
			[[workspace]].[[organization]] AS [[organization_id]],
			CASE WHEN [[policy]].[[workspace_id]] IS NULL THEN 0 ELSE 1 END AS [[policy_present]],
			COALESCE([[policy]].[[enabled]], -1) AS [[policy_enabled]],
			COALESCE([[policy]].[[revision]], -1) AS [[policy_revision]],
			COALESCE([[policy]].[[updated_at]], -1) AS [[policy_updated]],
			CASE WHEN [[client]].[[client_id]] IS NULL THEN 0 ELSE 1 END AS [[client_present]],
			COALESCE([[client]].[[name]], '') AS [[client_name]],
			CASE WHEN [[policy]].[[enabled]] = 1 AND [[client]].[[client_id]] IS NULL
				THEN 0 ELSE 1 END AS [[approved]]
		FROM {{leafen_workspaces}} [[workspace]]
		LEFT JOIN {{leafen_organization_mcp_workspace_policies}} [[policy]]
			ON [[policy]].[[workspace_id]] = [[workspace]].[[id]]
			AND [[policy]].[[organization_id]] = [[workspace]].[[organization]]
		LEFT JOIN {{leafen_organization_mcp_approved_clients}} [[client]]
			ON [[client]].[[workspace_id]] = [[workspace]].[[id]]
			AND [[client]].[[organization_id]] = [[workspace]].[[organization]]
			AND [[client]].[[client_id]] = {:clientId}
		WHERE [[workspace]].[[id]] IN (SELECT [[value]] FROM json_each({:workspaceIds}))
		ORDER BY [[workspace]].[[id]]
	`).Bind(params).All(&rows)
	if err != nil {
		return nil, errors.Join(leafen.ErrMCPGovernanceUnavailable, err)
	}
	if len(rows) != len(expected) {
		return nil, leafen.ErrMCPGovernanceUnavailable
	}
	for _, row := range rows {
		validPolicy := row.PolicyPresent == 0 && row.PolicyEnabled == -1 &&
			row.PolicyRevision == -1 && row.PolicyUpdated == -1 ||
			row.PolicyPresent == 1 && (row.PolicyEnabled == 0 || row.PolicyEnabled == 1) &&
				row.PolicyRevision >= 1 && row.PolicyUpdated >= 0
		validClient := row.ClientPresent == 0 && row.ClientName == ""
		if row.ClientPresent == 1 {
			name, nameErr := normalizeMCPGovernanceClientName(row.ClientName)
			validClient = nameErr == nil && name == row.ClientName
		}
		if expected[row.WorkspaceID] != row.OrganizationID || !validPolicy ||
			!validClient || row.Approved < 0 || row.Approved > 1 {
			return nil, leafen.ErrMCPGovernanceUnavailable
		}
	}
	return rows, nil
}

func loadStoredMCPGovernanceClient(
	app core.App,
	organizationID string,
	workspaceID string,
	clientID string,
) (storedMCPGovernanceClient, bool, error) {
	client := storedMCPGovernanceClient{}
	err := app.DB().NewQuery(`
		SELECT [[client_id]], [[name]] FROM {{leafen_organization_mcp_approved_clients}}
		WHERE [[organization_id]] = {:organizationId} AND [[workspace_id]] = {:workspaceId}
			AND [[client_id]] = {:clientId}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "workspaceId": workspaceID, "clientId": clientID,
	}).One(&client)
	if errors.Is(err, sql.ErrNoRows) {
		return storedMCPGovernanceClient{}, false, nil
	}
	return client, err == nil, err
}

func insertMCPGovernanceClient(
	app core.App,
	organizationID string,
	workspaceID string,
	clientID string,
	name string,
	actorID string,
	now int64,
) error {
	_, err := app.DB().Insert(leafen.OrganizationMCPApprovedClientsTable, dbx.Params{
		"organization_id": organizationID, "workspace_id": workspaceID,
		"client_id": clientID, "name": name, "approved_at": now, "approved_by": actorID,
		"updated_at": now, "updated_by": actorID,
	}).Execute()
	return err
}

func updateMCPGovernanceClientName(
	app core.App,
	organizationID string,
	workspaceID string,
	clientID string,
	name string,
	actorID string,
	now int64,
) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_organization_mcp_approved_clients}}
		SET [[name]] = {:name}, [[updated_at]] = {:updatedAt}, [[updated_by]] = {:updatedBy}
		WHERE [[organization_id]] = {:organizationId} AND [[workspace_id]] = {:workspaceId}
			AND [[client_id]] = {:clientId}
	`).Bind(dbx.Params{
		"name": name, "updatedAt": now, "updatedBy": actorID,
		"organizationId": organizationID, "workspaceId": workspaceID, "clientId": clientID,
	}).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return leafen.ErrMCPGovernanceUnavailable
	}
	return nil
}

func deleteMCPGovernanceClient(
	app core.App,
	organizationID string,
	workspaceID string,
	clientID string,
) error {
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_organization_mcp_approved_clients}}
		WHERE [[organization_id]] = {:organizationId} AND [[workspace_id]] = {:workspaceId}
			AND [[client_id]] = {:clientId}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "workspaceId": workspaceID, "clientId": clientID,
	}).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return leafen.ErrMCPGovernanceUnavailable
	}
	return nil
}

func assertMCPGovernanceWorkspace(app core.App, organizationID string, workspaceID string) error {
	var count int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_workspaces}}
		WHERE [[id]] = {:workspaceId} AND [[organization]] = {:organizationId}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "organizationId": organizationID}).Row(&count); err != nil {
		return err
	}
	if count != 1 {
		return leafen.PermissionDenied()
	}
	return nil
}

func upsertMCPGovernancePolicy(
	app core.App,
	organizationID string,
	workspaceID string,
	enabled bool,
	revision int,
	now int64,
) error {
	result, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_organization_mcp_workspace_policies}}
			([[organization_id]], [[workspace_id]], [[enabled]], [[revision]], [[updated_at]])
		VALUES ({:organizationId}, {:workspaceId}, {:enabled}, {:revision}, {:updatedAt})
		ON CONFLICT ([[workspace_id]]) DO UPDATE SET
			[[enabled]] = excluded.[[enabled]], [[revision]] = excluded.[[revision]],
			[[updated_at]] = excluded.[[updated_at]]
		WHERE [[organization_id]] = excluded.[[organization_id]]
	`).Bind(dbx.Params{
		"organizationId": organizationID, "workspaceId": workspaceID,
		"enabled": leafen.BoolInt(enabled), "revision": revision, "updatedAt": now,
	}).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return leafen.ErrMCPGovernanceUnavailable
	}
	return nil
}
