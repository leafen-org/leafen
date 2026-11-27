package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Every database row gets a page record of the same ID whose parent is its
// data source, as Notion's database pages are pages. In this step the page
// records follow the rows (the rows stay where they are authored), carry the
// title, icon, archive and provenance of their row pages, and stay out of
// what lists workspace pages: the sidebar's visibility, export counts and
// share graphs count only pages without a data source. Page authority does
// not yet open them; a row page opens by its row page authority.

// isDatabaseRowPageRecord reports a page record that is a database row's.
func isDatabaseRowPageRecord(page *core.Record) bool {
	return page != nil && page.GetString("data_source_id") != ""
}

// pageAncestryParentID names the page above a page: its parent page, for a
// page of a wiki the page it sits under in the wiki, or for another database
// page the page holding its database.
func pageAncestryParentID(app core.App, page *core.Record) (string, error) {
	if isDatabaseRowPageRecord(page) {
		return databaseRowPageAbove(app, page)
	}
	return strings.TrimSpace(page.GetString("parent_id")), nil
}

// databaseRowPageAbove reads the page above a database page: the page it
// sits under in its wiki, or the page holding its database.
func databaseRowPageAbove(app core.App, rowPage *core.Record) (string, error) {
	containerID, err := databaseRowPageContainer(app, rowPage)
	if err != nil {
		return "", err
	}
	wikiParentID, err := databaseRowWikiParent(app, rowPage)
	if err == nil && wikiParentID == "" {
		return containerID, nil
	}
	return wikiParentID, err
}

// databaseRowPageContainer reads the page holding the database a row page's
// data source belongs to.
func databaseRowPageContainer(app core.App, rowPage *core.Record) (string, error) {
	key := rowPageContainerMemoKey{
		sourceID: rowPage.GetString("data_source_id"), ownerID: rowPage.GetString("owner"),
		workspaceID: rowPage.GetString("workspace"),
	}
	// A database's items share it: read once a read scope (2026-10-17: a
	// search read it again for each item it found).
	return remembered(app, key, func() (string, error) {
		var containerID string
		err := app.DB().NewQuery(`
			SELECT [[page_id]] FROM {{leafen_data_sources}}
			WHERE [[id]] = {:sourceId} AND [[owner]] = {:ownerId} AND [[workspace_id]] = {:workspaceId}
				AND ` + databaseSourceNotTrashedSQL("{:sourceId}") + `
		`).Bind(dbx.Params{"sourceId": key.sourceID, "ownerId": key.ownerID, "workspaceId": key.workspaceID}).Row(&containerID)
		if errors.Is(err, sql.ErrNoRows) || err == nil && containerID == "" {
			return "", permissionDenied()
		}
		return containerID, err
	})
}

type rowPageContainerMemoKey struct{ sourceID, ownerID, workspaceID string }

// authorizeRowPageRecord authorizes a row page by the higher of the role the
// page above it gives — its database's page, or for a page of a wiki the page
// it sits under — and the role the row page is shared with; an unlinked one
// by its own (database_row_unlinked_access.go). The higher is its role
// whatever minimum was asked, which only refuses (decidePersonPageRole).
func authorizeRowPageRecord(
	app core.App, principalID string, rowPage *core.Record, minimum pageAccessRole,
) (pageAuthorization, error) {
	if principalID == "" || minimum <= pageAccessRoleNone {
		return pageAuthorization{}, permissionDenied()
	}
	if err := workspaceMoveFence(app, rowPage.GetString("workspace"), minimum); err != nil {
		return pageAuthorization{}, err
	}
	if decided, own, err := authorizeRowPageByOwnRule(app, principalID, rowPage, minimum); own {
		return decided, err
	}
	containerID, err := databaseRowPageAbove(app, rowPage)
	if isPermissionDenied(err) && minimum <= pageAccessRoleView {
		// The page of a data source in the trash, which has no place
		// above it while its source is there, reads as it opens from the
		// trash: through the page holding its database.
		if trashed, trashedErr := authorizeTrashedSourceRowPageRecord(app, principalID, rowPage); !isPermissionDenied(trashedErr) {
			return trashed, trashedErr
		}
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	// The page above held a while gives its role as held (page_access_hold.go).
	decided, err := authorizePageAccessRole(app, principalID, containerID, pageAccessRoleView)
	containerHeld := pageAccessHeld(err)
	container, err := pageAccessDespiteHold(decided, err)
	if err != nil && !isPermissionDenied(err) {
		return pageAuthorization{}, err
	}
	containerRole := pageAccessRoleNone
	if err == nil {
		containerRole = container.AccessRole
	}
	grantRole := pageAccessRoleNone
	workspace := container.Workspace
	// The row's own shares count where the page above gives less than full
	// access: needed where it gives less than asked, else only to widen it.
	if containerRole < pageAccessRoleFullAccess {
		granted, grantWorkspace, grantErr := rowPageShareRole(app, principalID, rowPage)
		if grantErr != nil && (containerRole < minimum || !isPermissionDenied(grantErr) && !isGuestMFARequired(grantErr)) {
			return pageAuthorization{}, grantErr
		}
		if grantErr == nil {
			grantRole = granted
			if containerRole == pageAccessRoleNone {
				workspace = grantWorkspace
			}
		}
	}
	role := max(containerRole, grantRole)
	if role < minimum {
		return pageAuthorization{}, permissionDenied()
	}
	authorization := pageAuthorization{Workspace: workspace, Page: rowPage, AccessRole: role}
	if grantRole < minimum {
		return heldDecision(containerHeld, authorization, nil)
	}
	return authorization, nil
}

// rowPageShareRole is the role a row page's own shares, and a web link its
// reader came in by, give them, with the workspace as a page's shares
// admit them to it.
func rowPageShareRole(app core.App, principalID string, rowPage *core.Record) (pageAccessRole, workspaceAuthorization, error) {
	workspaceID := rowPage.GetString("workspace")
	// Once a read scope: a list's items share their workspace.
	record, err := findRecordRemembered(app, workspacesCollection, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return pageAccessRoleNone, workspaceAuthorization{}, permissionDenied()
	}
	if err != nil {
		return pageAccessRoleNone, workspaceAuthorization{}, err
	}
	if record.GetString("owner") != rowPage.GetString("owner") {
		return pageAccessRoleNone, workspaceAuthorization{}, permissionDenied()
	}
	grantWorkspace, err := authorizePageGrantWorkspace(app, principalID, record)
	if err != nil {
		return pageAccessRoleNone, workspaceAuthorization{}, err
	}
	role, err := effectiveUserPageAccessRole(app, workspaceID, rowPage.Id, principalID)
	return role, grantWorkspace, err
}
