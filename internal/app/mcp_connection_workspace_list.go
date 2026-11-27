package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The workspaces a connection reaches, as its own key reads them: an agent
// connection's are all its person's, an MCP token's its one
// (connection_person_rights.go).

// connectionWorkspaceRow is a workspace a connection reaches, as it is kept.
type connectionWorkspaceRow struct {
	ID      string `db:"id"`
	Name    string `db:"name"`
	Created string `db:"created"`
}

// loadConnectionWorkspaceRows reads the workspaces a connection reaches, by
// name and then ID.
func loadConnectionWorkspaceRows(app core.App, connectionID string) ([]connectionWorkspaceRow, error) {
	rows := []connectionWorkspaceRow{}
	err := app.DB().NewQuery(`SELECT [[workspace]].[[id]], [[workspace]].[[name]], [[workspace]].[[created]]
		FROM {{` + mcpConnectionWorkspacesTable + `}} [[scope]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[scope]].[[workspace_id]]
		WHERE [[scope]].[[connection_id]] = {:connection} ORDER BY [[workspace]].[[name]], [[workspace]].[[id]]`).
		Bind(dbx.Params{"connection": connectionID}).All(&rows)
	return rows, err
}

// connectionWorkspaceList is what a connection reaches, as
// leafen_list_workspaces lists it.
func connectionWorkspaceList(app core.App, connectionID string) ([]mcpWorkspaceListItem, error) {
	rows, err := loadConnectionWorkspaceRows(app, connectionID)
	if err != nil {
		return nil, err
	}
	list := make([]mcpWorkspaceListItem, 0, len(rows))
	for _, row := range rows {
		list = append(list, mcpWorkspaceListItem{ID: row.ID, Name: row.Name,
			Created: strings.Replace(strings.TrimSpace(row.Created), " ", "T", 1)})
	}
	return list, nil
}

// ownMCPConnectionKey is the key a request about its own connection comes
// with, or answered says it was refused.
func ownMCPConnectionKey(event *core.RequestEvent) (userKeyAuthority, bool, error) {
	key, keyed := userKeyOf(event)
	if !keyed || key.AppInstallID != "" || key.ConnectionID == "" {
		return key, true, apiError(event, http.StatusNotFound, errorCodeMCPConnectionMissing,
			"No MCP token sends this: an agent sends it with its own token.")
	}
	return key, false, nil
}

// mcpWorkspaceListItem is a workspace a connection reaches, as
// leafen_list_workspaces lists it.
type mcpWorkspaceListItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Created string `json:"created" jsonschema:"When the workspace was made: workspaces of the same name differ by it and by id"`
}

// listMCPWorkspaces lists the workspaces a connection reaches.
func listMCPWorkspaces(app core.App, authority mcpAuthority) ([]mcpWorkspaceListItem, error) {
	made, err := mcpWorkspacesMade(app, authority)
	if err != nil {
		return nil, err
	}
	workspaces := make([]mcpWorkspaceListItem, 0, len(authority.ApprovedWorkspaces))
	for _, workspace := range authority.ApprovedWorkspaces {
		workspaces = append(workspaces, mcpWorkspaceListItem{ID: workspace.ID, Name: workspace.Name, Created: made[workspace.ID]})
	}
	return workspaces, nil
}

// mcpWorkspacesMade is when each workspace a connection reaches was made, as
// RFC 3339 writes it.
func mcpWorkspacesMade(app core.App, authority mcpAuthority) (map[string]string, error) {
	rows := []struct {
		ID      string `db:"id"`
		Created string `db:"created"`
	}{}
	if len(authority.ApprovedWorkspaces) > 0 {
		ids := make([]any, 0, len(authority.ApprovedWorkspaces))
		for _, workspace := range authority.ApprovedWorkspaces {
			ids = append(ids, workspace.ID)
		}
		if err := app.DB().Select("id", "created").From(workspacesCollection).
			Where(dbx.In("id", ids...)).All(&rows); err != nil {
			return nil, err
		}
	}
	made := map[string]string{}
	for _, row := range rows {
		made[row.ID] = strings.Replace(strings.TrimSpace(row.Created), " ", "T", 1)
	}
	return made, nil
}

// mcpWorkspacesListedOn is where an agent finds the workspaces its
// connection reaches: leafen_list_workspaces on Leafen's own endpoint,
// notion-get-teams (under workspaces) on the Notion-compatible one, which
// has no leafen_ tools.
func mcpWorkspacesListedOn(authority mcpAuthority) string {
	if strings.HasSuffix(authority.Resource, nativeMCPPath) {
		return "leafen_list_workspaces lists them"
	}
	return "notion-get-teams lists them under workspaces"
}
