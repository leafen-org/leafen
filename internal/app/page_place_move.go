package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page moves between teamspaces, and between private pages and a
// teamspace, as in Notion: under a page elsewhere, or to the top of a
// teamspace or of the mover's private pages. Only someone with full access
// to the page moves it to another place, and only where they may add pages:
// under a page they can edit, at the top of a teamspace they can edit in, or
// at the top of their own private pages. The pages under it go with it, its
// databases' pages and the pages under those included. A page moved to the
// top of private pages is shared with no one; a page moved under a private
// page, like the pages under a moved page, keeps its own shares.

// pagePlace is where a page lives: a teamspace, or one person's private
// pages.
type pagePlace struct {
	TeamspaceID    string
	PrivateOwnerID string
}

func pagePlaceOf(page *core.Record) pagePlace {
	return pagePlace{TeamspaceID: page.GetString("teamspace_id"), PrivateOwnerID: page.GetString(privatePageOwnerField)}
}

// authorizePagePlaceMove reports whether the mover may take the page to its
// new place.
func authorizePagePlaceMove(
	app core.App, actorID string, authorization pageAuthorization, parentID string, destination pagePlace,
) (bool, error) {
	if _, err := authorizePageAccessRole(app, actorID, authorization.Page.Id, pageAccessRoleFullAccess); err != nil {
		if isPermissionDenied(err) {
			return false, nil
		}
		return false, err
	}
	workspaceID := authorization.Workspace.WorkspaceID
	switch {
	case parentID != "":
		parent, err := authorizePageBody(app, actorID, parentID, permissionWorkspaceWrite)
		if isPermissionDenied(err) {
			return false, nil
		}
		return err == nil && parent.Workspace.WorkspaceID == workspaceID, err
	case destination.TeamspaceID != "":
		workspace, role, err := effectiveTeamspacePageAccessRole(app, actorID, workspaceID, destination.TeamspaceID)
		if isPermissionDenied(err) {
			return false, nil
		}
		return err == nil && role >= pageAccessRoleEdit && workspace.WorkspaceID == workspaceID, err
	default:
		workspace, err := authorizeWorkspace(app, actorID, workspaceID, permissionWorkspaceWrite)
		if isPermissionDenied(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		// A restricted member the organization keeps from making private
		// pages moves none there (organization_request_controls.go).
		refused, err := restrictedPrivatePagesRefused(app, workspace)
		return err == nil && !refused, err
	}
}

// authorizeDatabaseRowPlaceMove admits a database page leaving its database
// for another page or database only for someone with full access to it: it
// takes on the new parent's sharing, as any moved page does (2026-10-31).
func authorizeDatabaseRowPlaceMove(
	app core.App, actorID string, authority databaseRowPageAuthority, _ *core.Record,
) error {
	record, err := app.FindRecordById(pagesCollection, authority.RowID)
	if err != nil {
		return err
	}
	if _, err = authorizeRowPageRecord(app, actorID, record, pageAccessRoleFullAccess); isPermissionDenied(err) {
		return errDatabaseRowPageMoveDenied
	}
	return err
}

// movePageTreeToPlace puts the page and every page under it in the place.
func movePageTreeToPlace(app core.App, ownerID, workspaceID, pageID string, place pagePlace) error {
	// CROSS JOIN steps from the tree by key (parent, page): SQLite's own order
	// read the workspace's every page and the owner's every source for each
	// page of the tree.
	_, err := app.DB().NewQuery(`
		WITH RECURSIVE [[tree]]([[id]]) AS (
			SELECT {:pageId}
			UNION
			SELECT [[page]].[[id]] FROM [[tree]]
			CROSS JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[parent_id]] = [[tree]].[[id]]
			UNION
			SELECT [[source]].[[id]] FROM [[tree]]
			CROSS JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[owner]] = {:ownerId} AND [[source]].[[page_id]] = [[tree]].[[id]]
		)
		UPDATE {{leafen_pages}} SET [[teamspace_id]] = {:teamspaceId}, [[private_owner_id]] = {:privateOwnerId}
		WHERE [[workspace]] = {:workspaceId} AND [[id]] IN (SELECT [[id]] FROM [[tree]])
			AND ([[teamspace_id]] != {:teamspaceId} OR [[private_owner_id]] != {:privateOwnerId})
	`).Bind(dbx.Params{
		"pageId": pageID, "ownerId": ownerID, "workspaceId": workspaceID,
		"teamspaceId": place.TeamspaceID, "privateOwnerId": place.PrivateOwnerID,
	}).Execute()
	if err != nil {
		return err
	}
	// The roles a teamspace's people had on a page that left it go.
	_, err = app.DB().NewQuery(`
		DELETE FROM {{leafen_page_teamspace_roles}} WHERE [[workspace_id]] = {:workspaceId} AND [[teamspace_id]] != (
			SELECT [[page]].[[teamspace_id]] FROM {{leafen_pages}} AS [[page]] WHERE [[page]].[[id]] = {{leafen_page_teamspace_roles}}.[[page_id]])
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Execute()
	return err
}

// unsharePrivatePage removes everyone's shares of a page moved to the top of
// private pages, and its unlink from a parent it no longer has; a share
// dialog open on it reads the page again.
func unsharePrivatePage(app core.App, workspaceID, pageID, actorID string, now time.Time) error {
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_page_share_unlinks}} WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "pageId": pageID}).Execute(); err != nil {
		return err
	}
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_page_permissions}} WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "pageId": pageID}).Execute()
	if err != nil {
		return err
	}
	if removed, err := result.RowsAffected(); err != nil || removed == 0 {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_public_mutation_states}} SET [[revision]] = [[revision]] + 1, [[updated_by]] = {:actorId},
			[[updated_at]] = {:updatedAt}
		WHERE [[workspace_id]] = {:workspaceId} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "pageId": pageID, "actorId": actorID,
		"updatedAt": now.UnixMilli(),
	}).Execute()
	return err
}
