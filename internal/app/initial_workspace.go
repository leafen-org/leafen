package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// First-language submissions from different tabs reuse an already-created
// personal workspace. This uses current native ownership, inside the creation
// transaction, rather than a possibly older bootstrap visibility projection.
func existingInitialWorkspace(app core.App, principalID string) (workspaceCreateResponse, bool, error) {
	rows, err := app.FindRecordsByFilter(workspacesCollection, "workspace_owner = {:owner}", "id", 1, 0,
		dbx.Params{"owner": principalID})
	if err != nil || len(rows) == 0 {
		return workspaceCreateResponse{}, false, err
	}
	workspace := rows[0]
	if _, err := authorizeWorkspace(app, principalID, workspace.Id, permissionWorkspaceRead); err != nil {
		return workspaceCreateResponse{}, false, err
	}
	// No existing page or welcome payload is replayed for a different request.
	return workspaceCreateResponse{ID: workspace.Id, OrganizationID: workspace.GetString("organization"),
		Name: workspace.GetString("name"), Duplicate: true}, true, nil
}
