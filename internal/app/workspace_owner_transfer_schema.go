package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const workspaceOwnerTransferNotificationsTable = "leafen_workspace_owner_transfer_notifications"

func workspacePermissionOwnerSQL(app core.App, workspaceAlias string) string {
	workspaces, err := app.FindCollectionByNameOrId(workspacesCollection)
	if err == nil && workspaces.Fields.GetByName("workspace_owner") != nil {
		return workspaceAlias + ".[[workspace_owner]]"
	}
	return workspaceAlias + ".[[owner]]"
}

func workspacePermissionOwnerID(workspace *core.Record) string {
	ownerID := workspace.GetString("workspace_owner")
	if ownerID == "" {
		return workspace.GetString("owner")
	}
	return ownerID
}

func loadWorkspacePermissionOwner(app core.App, workspaceID string) (string, error) {
	var ownerID string
	err := app.DB().NewQuery(`
		SELECT [[workspace_owner]] FROM {{leafen_workspaces}} WHERE [[id]]={:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&ownerID)
	return ownerID, err
}

// workspaceOwnerIs is whether a person owns a workspace; a workspace no
// longer there has no owner.
func workspaceOwnerIs(app core.App, workspaceID, principalID string) (bool, error) {
	workspace, err := findRecordRemembered(app, workspacesCollection, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return workspacePermissionOwnerID(workspace) == principalID, nil
}
