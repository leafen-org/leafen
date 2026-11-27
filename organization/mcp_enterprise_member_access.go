package organization

import (
	"encoding/json"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	mcpEnterpriseAccessAllowed = "allowed"
	mcpEnterpriseAccessDenied  = "denied"
)

type storedMCPEnterpriseMemberAccess struct {
	WorkspaceID    string `db:"workspace_id"`
	OrganizationID string `db:"organization_id"`
	Present        int    `db:"present"`
	Access         string `db:"access"`
}

func loadMCPEnterpriseMemberAccess(
	app core.App,
	workspaces []leafen.MCPGovernanceWorkspace,
	userID string,
) (map[string]storedMCPEnterpriseMemberAccess, error) {
	if userID == "" || len(workspaces) == 0 {
		return nil, leafen.ErrMCPGovernanceInvalid
	}
	params := dbx.Params{"userId": userID}
	ids := make([]string, 0, len(workspaces))
	expected := make(map[string]string, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.ID == "" || workspace.OrganizationID == "" {
			return nil, leafen.ErrMCPGovernanceInvalid
		}
		if organizationID, duplicate := expected[workspace.ID]; duplicate {
			if organizationID != workspace.OrganizationID {
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
	rows := []storedMCPEnterpriseMemberAccess{}
	err = app.DB().NewQuery(`
		SELECT [[workspace]].[[id]] AS [[workspace_id]],
			[[workspace]].[[organization]] AS [[organization_id]],
			CASE WHEN [[access]].[[workspace_id]] IS NULL THEN 0 ELSE 1 END AS [[present]],
			COALESCE([[access]].[[access]], '') AS [[access]]
		FROM {{leafen_workspaces}} [[workspace]]
		LEFT JOIN {{leafen_mcp_enterprise_member_access}} [[access]]
			ON [[access]].[[workspace_id]] = [[workspace]].[[id]]
			AND [[access]].[[organization_id]] = [[workspace]].[[organization]]
			AND [[access]].[[user_id]] = {:userId}
		WHERE [[workspace]].[[id]] IN (SELECT [[value]] FROM json_each({:workspaceIds}))
		ORDER BY [[workspace]].[[id]]
	`).Bind(params).All(&rows)
	if err != nil || len(rows) != len(expected) {
		return nil, leafen.ErrMCPGovernanceUnavailable
	}
	result := make(map[string]storedMCPEnterpriseMemberAccess, len(rows))
	for _, row := range rows {
		valid := row.Present == 0 && row.Access == "" ||
			row.Present == 1 && (row.Access == mcpEnterpriseAccessAllowed || row.Access == mcpEnterpriseAccessDenied)
		if expected[row.WorkspaceID] != row.OrganizationID || !valid {
			return nil, leafen.ErrMCPGovernanceUnavailable
		}
		result[row.WorkspaceID] = row
	}
	return result, nil
}
