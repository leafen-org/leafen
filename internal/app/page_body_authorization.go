package app

import (
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's body is its blocks, text, sub-pages and databases. A database
// page's body changes through its page's content graph, under the row page's
// permission, and moves the row page's body revision on.

// databaseRowBodyRejectsType names the blocks a database page body does not
// hold: a full-page database. Its sub-pages are page blocks, as a page's are.
func databaseRowBodyRejectsType(blockType string) bool {
	return blockType == "child_database"
}

// databaseRowPageBodyOperationsAllowed reports whether changes to a database
// page's body hold only blocks it may hold; its databases change as a
// page's do.
func databaseRowPageBodyOperationsAllowed(operations []graphOperation) bool {
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertBlock &&
			(operation.Block == nil || databaseRowBodyRejectsType(operation.Block.Type)) {
			return false
		}
	}
	return true
}

// authorizePageBody authorizes a page's body: its blocks, text, sub-pages
// and databases. A database page's body is written under its row page's
// permission, while neither the row nor its database's page is in the trash;
// page structure writes (move, title, lock, trash, copy) keep authorizePage,
// which a database page does not pass for writing.
func authorizePageBody(
	app core.App, principalID, pageID string, requirement permissionRequirement,
) (pageAuthorization, error) {
	return authorizePageBodyWithOwnedCopy(app, principalID, pageID, requirement, "")
}

type blockMoveFenceMemoKey struct{ principalID, pageID, ownedCopyJobID string }

func authorizePageBodyWithOwnedCopy(
	app core.App, principalID, pageID string, requirement permissionRequirement, ownedCopyJobID string,
) (pageAuthorization, error) {
	// One decision, its questions asked once (authorizePageAccessRoleWithOwnedCopy);
	// within a read scope the page's move fence too, asked once for all the
	// rows of a database an agent's answer names (2026-11-12: once a row).
	app = withAccessMemo(app)
	fenced, err := remembered(app, blockMoveFenceMemoKey{principalID, pageID, ownedCopyJobID}, func() (bool, error) {
		return blockMovePageFenceExceptOwnedCopy(app, principalID, pageID, ownedCopyJobID)
	})
	if err != nil {
		return pageAuthorization{}, err
	}
	if fenced {
		return pageAuthorization{}, permissionDenied()
	}
	if requirement != permissionWorkspaceRead {
		templatePage, err := isDatabaseTemplatePageID(app, pageID)
		if err != nil {
			return pageAuthorization{}, err
		}
		if templatePage {
			return authorizeDatabaseTemplateBody(
				app, principalID, pageID, pageAccessRoleForWorkspaceRequirement(requirement),
			)
		}
		rowPage, err := isDatabaseRowPageRoom(app, pageID)
		if err != nil {
			return pageAuthorization{}, err
		}
		if rowPage {
			authorization, err := authorizeDatabaseRowBodyAt(
				app, principalID, pageID, pageAccessRoleForWorkspaceRequirement(requirement),
			)
			requestError := &graphRequestError{}
			if errors.Is(err, errDatabaseRowPageNotFound) || errors.Is(err, errDatabaseRowPageInTrash) ||
				errors.As(err, &requestError) {
				return pageAuthorization{}, permissionDenied()
			}
			return authorization, err
		}
	}
	return authorizePageAccessRoleWithOwnedCopy(app, principalID, pageID, pageAccessRoleForWorkspaceRequirement(requirement), ownedCopyJobID)
}

// authorizeDatabaseRowBodyAt admits body changes that need the given role
// on the row page.
func authorizeDatabaseRowBodyAt(
	app core.App, principalID, rowID string, minimum pageAccessRole,
) (pageAuthorization, error) {
	authority, err := authorizeDatabaseRowPage(app, principalID, rowID, permissionWorkspaceWrite)
	if err != nil {
		return pageAuthorization{}, err
	}
	if trashed, err := pageInTrashPath(app, authority.ContainerPageID); err != nil {
		return pageAuthorization{}, err
	} else if trashed {
		return pageAuthorization{}, pageInTrashError()
	}
	rowPage, err := findRecordRemembered(app, pagesCollection, rowID)
	if err != nil {
		return pageAuthorization{}, refusedUnlessStorage(errDatabaseRowPageNotFound, err)
	}
	return authorizeRowPageRecord(app, principalID, rowPage, minimum)
}

// touchDatabaseRowPageBody moves a row page's body revision on after its
// body changed, with who changed it.
func touchDatabaseRowPageBody(app core.App, rowID, actorID string, now int64) error {
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_row_pages}}
		SET [[body_snapshot_revision]]=[[body_snapshot_revision]]+1,
			[[authority_revision]]=[[authority_revision]]+1, [[updated]]={:now}
		WHERE [[row_id]]={:rowId}
	`).Bind(dbx.Params{"rowId": rowID, "now": now}).Execute(); err != nil {
		return err
	}
	if actorID != "" {
		if _, err := setRowPageRecordEditor(app, rowID, "", actorID); err != nil {
			return err
		}
	}
	return markDatabaseRowSearchDirty(app, rowID)
}
