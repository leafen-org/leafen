package app

import (
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The checks that admit a database page turned into a page under its new
// parent or at the top of a place, and a page turned into a database page
// (page_row_conversion.go).

// checkDatabaseRowToPageParent admits the page the database page moves
// under: one the mover may add pages to, in the same workspace, in another
// place only for someone with full access to the database page, outside the
// trash and not inside the database page, each refused for its reason
// (page_row_conversion_refusals.go).
func checkDatabaseRowToPageParent(app core.App, request databaseRowToPageRequest, authority databaseRowPageAuthority) error {
	if request.ParentID == "" {
		return checkDatabaseRowToPagePlace(app, request, authority)
	}
	parent, err := authorizePageBody(app, request.PrincipalID, request.ParentID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return rowUnderPageAccessRefused(request.ParentID)
	}
	if err != nil {
		return err
	}
	if parent.Workspace.WorkspaceID != authority.WorkspaceID {
		return rowUnderPageCrossing(app, authority, request.ParentID, parent.Workspace.WorkspaceID)
	}
	if err := authorizeDatabaseRowPlaceMove(app, request.PrincipalID, authority, parent.Page); errors.Is(err, errDatabaseRowPageMoveDenied) {
		return rowToPlaceAccessRefused("targetPageId")
	} else if err != nil {
		return err
	}
	if inTrash, err := pageInTrashPath(app, request.ParentID); err != nil || inTrash {
		return errors.Join(err, rowUnderTrashedPageRefused(request.ParentID, inTrash))
	}
	if within, err := pageAncestryIncludes(app, request.ParentID, authority.RowID); err != nil || within {
		return errors.Join(err, rowUnderPageInsideRefused(request.ParentID, within))
	}
	if fenced, err := wikiMutationFenceActive(app, request.ParentID); err != nil || fenced {
		return errors.Join(err, rowUnderWikiBusyRefused(request.ParentID, fenced))
	}
	if parent.Page.GetBool("is_locked") || isFullPageDatabase(parent.Page) {
		return rowUnderPageRefused(request.ParentID, parent.Page)
	}
	return nil
}

// checkDatabaseRowToPagePlace admits the place at whose top the database
// page goes, as a page's move to the top of a place is (authorizePagePlaceMove):
// a teamspace the mover may edit in, or the mover's private pages; another
// place than the one it is in only for someone with full access to it.
func checkDatabaseRowToPagePlace(app core.App, request databaseRowToPageRequest, authority databaseRowPageAuthority) error {
	if request.Place == nil {
		return nil
	}
	record, err := app.FindRecordById(pagesCollection, authority.RowID)
	if err != nil {
		return err
	}
	if *request.Place == pagePlaceOf(record) {
		return nil
	}
	if teamspaceID := request.Place.TeamspaceID; teamspaceID != "" {
		workspace, role, err := effectiveTeamspacePageAccessRole(app, request.PrincipalID, authority.WorkspaceID, teamspaceID)
		if isPermissionDenied(err) || err == nil && (role < pageAccessRoleEdit || workspace.WorkspaceID != authority.WorkspaceID) {
			return rowToTopAccessRefused(true)
		}
		if err != nil {
			return err
		}
	} else if _, err := authorizeWorkspace(app, request.PrincipalID, authority.WorkspaceID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return rowToTopAccessRefused(false)
	} else if err != nil {
		return err
	}
	if err := authorizeDatabaseRowPlaceMove(app, request.PrincipalID, authority, nil); errors.Is(err, errDatabaseRowPageMoveDenied) {
		return rowToPlaceAccessRefused("targetTeamspaceId")
	} else if err != nil {
		return err
	}
	return nil
}

// checkPageToDatabaseRow admits a page into a database: a live page of the
// database's workspace, from another place only for someone with full
// access to it, into a database that takes added rows (database_added_rows.go)
// outside the Trash and outside the page, holding no published site, moved
// as a locked page's move allows — each refused for its reason
// (page_row_conversion_refusals.go).
func checkPageToDatabaseRow(
	app core.App, request pageToDatabaseRowRequest, page, container pageAuthorization,
) error {
	record := page.Page
	if page.Workspace.WorkspaceID != container.Workspace.WorkspaceID {
		return pageIntoDatabaseRefusal(app, request, page.Workspace.WorkspaceID, container.Workspace.WorkspaceID)
	}
	if refusal := pageIntoDatabaseStateRefused(record); refusal != nil {
		return refusal
	}
	if pagePlaceOf(record) != pagePlaceOf(container.Page) {
		if _, err := authorizePageAccessRole(app, request.PrincipalID, record.Id, pageAccessRoleFullAccess); isPermissionDenied(err) {
			return pageIntoDatabaseAccessRefused()
		} else if err != nil {
			return err
		}
	}
	if err := pageIntoDatabaseSourceRefused(app, request.DataSourceID, request.ContainerPageID); err != nil {
		return err
	}
	if within, err := pageAncestryIncludes(app, request.ContainerPageID, record.Id); err != nil || within {
		return errors.Join(err, pageIntoOwnDatabaseRefused(within))
	}
	var published int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_public_share_links}} WHERE [[root_page_id]] = {:id} AND [[enabled]] = 1
	`).Bind(dbx.Params{"id": record.Id}).Row(&published); err != nil {
		return err
	}
	if published > 0 {
		return errPageRowConversionShared
	}
	locked, err := pageMoveLockConflict(app, page, container.Page)
	if err != nil {
		return err
	}
	if locked {
		return pageIntoDatabaseLockedRefused(record, container.Page)
	}
	return nil
}
