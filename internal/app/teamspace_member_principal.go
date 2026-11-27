package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// validTeamspacePrincipal says whether a teamspace may take the group or
// person: a group of its workspace, or the workspace's owner or a member
// who isn't being deleted (account_new_places.go).
func validTeamspacePrincipal(app core.App, authority teamspaceAuthority, kind, id string) (bool, error) {
	var count int
	if kind == "group" {
		err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_workspace_permission_groups}}
			WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[deleted_at]] IS NULL
		`).Bind(dbx.Params{"id": id, "workspaceId": authority.Workspace.WorkspaceID}).Row(&count)
		return count == 1, err
	}
	place := `(SELECT COUNT(*) FROM {{leafen_workspace_memberships}}
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:id} AND [[role]]='member') = 1`
	if id == authority.Workspace.WorkspaceOwnerID {
		place = `1`
	}
	err := app.DB().NewQuery(`SELECT ` + place + ` AND ` + accountTakesNewPlacesSQL("{:id}")).
		Bind(dbx.Params{"workspaceId": authority.Workspace.WorkspaceID, "id": id}).Row(&count)
	return count == 1, err
}
