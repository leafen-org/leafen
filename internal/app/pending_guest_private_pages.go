package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A guest whose membership of a workspace is pending makes private pages
// there meanwhile, as Notion's security setting for pending guests' private
// pages has it (on by default; Notion's own guests
// otherwise make none, seen 2026-10-12). They own those pages as a member
// owns theirs and keep them once admitted. With the setting off, or once the
// request is declined or withdrawn, the pages stay but open to no one but
// through a share, as a removed member's do.

// pendingGuestPrivatePages is whether the person, not a member of the
// workspace, has asked to join it and may meanwhile keep private pages there.
func pendingGuestPrivatePages(app core.App, principalID string, workspace *core.Record) (bool, error) {
	if principalID == "" || workspace == nil {
		return false, nil
	}
	var pending int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_access_requests}}
		WHERE [[workspace_id]] = {:workspace} AND [[requester_id]] = {:principal}
			AND [[kind]] = {:kind} AND [[status]] = {:pending}`).
		Bind(dbx.Params{"workspace": workspace.Id, "principal": principalID,
			"kind": accessRequestKindMembership, "pending": accessRequestPending}).Row(&pending); err != nil || pending == 0 {
		return false, err
	}
	policy, err := loadEnforcedSharingSecurityPolicy(app, workspace.GetString("organization"))
	return err == nil && policy.PendingGuestPrivatePages == 1, err
}

// pendingGuestPageCreationWorkspace is the workspace a guest waiting to be a
// member makes a private page in, or permission denied.
func pendingGuestPageCreationWorkspace(app core.App, principalID, workspaceID string) (workspaceAuthorization, error) {
	workspace, err := app.FindRecordById(workspacesCollection, workspaceID)
	if err != nil {
		return workspaceAuthorization{}, permissionDenied()
	}
	keeps, err := pendingGuestPrivatePages(app, principalID, workspace)
	if err != nil {
		return workspaceAuthorization{}, err
	}
	if !keeps {
		return workspaceAuthorization{}, permissionDenied()
	}
	return authorizePageGrantWorkspace(app, principalID, workspace)
}

// privatelyOwnedPageIDs names, of the pages read, those the person owns as
// private pages.
func privatelyOwnedPageIDs(app core.App, principalID string, rows []storedBootstrapPage) (map[string]bool, error) {
	owned := map[string]bool{}
	if len(rows) == 0 {
		return owned, nil
	}
	ids := make([]string, len(rows))
	for index, row := range rows {
		ids[index] = row.ID
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	found := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_pages}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[private_owner_id]] = {:principal}`).
		Bind(dbx.Params{"ids": string(encoded), "principal": principalID}).Column(&found); err != nil {
		return nil, err
	}
	for _, id := range found {
		owned[id] = true
	}
	return owned, nil
}

// pageGrantCreationWorkspace is the workspace as a person holding page grants
// in it (a guest) sees it, for making a sub-page under a page they may edit.
func pageGrantCreationWorkspace(app core.App, principalID, workspaceID string) (workspaceAuthorization, error) {
	workspace, err := app.FindRecordById(workspacesCollection, workspaceID)
	if err != nil {
		return workspaceAuthorization{}, permissionDenied()
	}
	return authorizePageGrantWorkspace(app, principalID, workspace)
}
