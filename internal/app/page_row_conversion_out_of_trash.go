package app

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A database page in the trash, as a cut leaves it, comes out of the trash as
// it moves out of its database when the move says so (OutOfTrash, the row
// move's fromTrash): a cut row pasted on a page moves there as a sub-page, its
// ID, body and comments with it, its values kept should it go back, as
// Notion's does on the page it was cut from (seen 2026-11-27 in Chrome, two
// tabs at two times: a table's, a board's, a list's and a gallery's items
// cut and pasted there, or pasted with another database's item chosen, stood
// as sub-pages by the same IDs; Notion pastes a copy on another page, which
// Leafen moves too). One and the same change: it is never left back
// in its database, nor out of the trash without having moved. A move that
// doesn't say so leaves one in the trash refused, as before.

// rowToPageAuthorizer reads the row a move out of its database takes: one in
// a data source in the trash going to private pages, one in the trash itself
// when the move takes it out of it, or else a live one.
func rowToPageAuthorizer(request databaseRowToPageRequest) func(core.App, string, string, permissionRequirement) (databaseRowPageAuthority, error) {
	switch {
	case request.FromTrashedSource:
		return trashedSourceRowAuthority
	case request.OutOfTrash:
		return authorizeDatabaseRowPageInTrash
	}
	return authorizeDatabaseRowPage
}

// rowToPageTrashRefused refuses a row in the trash a move doesn't take out of
// it, or one in the trash with another item, which comes back with that.
func rowToPageTrashRefused(request databaseRowToPageRequest, authority databaseRowPageAuthority) error {
	if authority.TrashedAt == 0 || request.FromTrashedSource {
		return nil
	}
	if !request.OutOfTrash || authority.TrashRootID != authority.RowID {
		return rowInTrashRefused()
	}
	return nil
}

// takeRowOutOfTrashToMove brings the row, and the sub-items that went to the
// trash with it, out of the trash in the move's own change, before it leaves
// its database: its sub-items stay as rows of their own (databaseRowLeaveOperations).
func takeRowOutOfTrashToMove(app core.App, request databaseRowToPageRequest, authority *databaseRowPageAuthority) error {
	if authority.TrashedAt == 0 || !request.OutOfTrash {
		return nil
	}
	if _, err := restoreDatabaseRowEvent(app, authority.RowID, request.PrincipalID, time.Now().UTC()); err != nil {
		return err
	}
	authority.TrashedAt, authority.TrashRootID = 0, ""
	return nil
}

// moveDatabaseRowPageToPlace puts the database page, now a page at the top,
// and the pages under it in the place chosen; at the top of private pages
// from elsewhere it is shared with no one, as a moved page is.
func moveDatabaseRowPageToPlace(app core.App, authority databaseRowPageAuthority, principalID string, place pagePlace) error {
	record, err := app.FindRecordById(pagesCollection, authority.RowID)
	if err != nil || pagePlaceOf(record) == place {
		return err
	}
	if err := movePageTreeToPlace(app, authority.OwnerID, authority.WorkspaceID, authority.RowID, place); err != nil {
		return err
	}
	if place.TeamspaceID != "" {
		return nil
	}
	return unsharePrivatePage(app, authority.WorkspaceID, authority.RowID, principalID, time.Now())
}
