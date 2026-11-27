package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// No page is made inside the trash: under a page in the trash, under a page
// whose page above is (pageInTrashPath), or in a database page whose row or
// database's page is, as Notion's API refuses one (seen 2026-11-20: 400,
// the block archived directly under a page in the trash, an archived
// ancestor further down). Someone who could add pages to the
// parent once it is restored is told it is in the trash, as the editor tells
// a change to it (pageInTrashError: 409 LEAFEN_PAGE_IN_TRASH) and an import
// placed there (workspaceImportTargetGone); anyone else meets the parent's
// own refusal. Every road that makes a page through admitPageCreation asks
// this one rule: the app's "+" and "/페이지", agents' notion-create-pages and
// notion-create-database, the Public API, an import batch's folders and
// pages, a block turned into a page. A copy asks it of where it goes
// (pageDuplicationPlacementFailure), and a move of the page it moves under
// (placePageMetadataInputAgainstTrash).

// pageCreationParentTrashed reports whether the parent a page is asked under
// is in the trash for someone who could add pages to it in workspaceID.
// refused is authorizePageBody's answer for them on the parent: it passes a
// page in the trash, and refuses a database page or template there, a
// template for the trash alone (pageInTrashError), a database page as one
// they may not change, which it isn't when its row would take their change.
func pageCreationParentTrashed(app core.App, principalID, workspaceID, parentID string, refused error) (bool, error) {
	trashed, err := pageInTrashPath(app, parentID)
	if err != nil || !trashed {
		return false, err
	}
	if refused == nil {
		return true, nil
	}
	if !isPageInTrashRefusal(refused) {
		if !isPermissionDenied(refused) {
			return false, nil
		}
		row, err := isDatabaseRowPageRoom(app, parentID)
		if err != nil || !row {
			return false, err
		}
		if _, err := authorizeDatabaseRowPageInTrash(app, principalID, parentID, permissionWorkspaceWrite); err != nil {
			if storageFailure(err) {
				return false, err
			}
			return false, nil
		}
	}
	// Told only in its own workspace, as a parent in another is a conflict.
	parent, err := app.FindRecordById(pagesCollection, parentID)
	if err != nil {
		return false, refusedUnlessStorage(nil, err)
	}
	return parent.GetString("workspace") == workspaceID, nil
}

// isPageInTrashRefusal reports err as the refusal of a change to a page in
// the trash (pageInTrashError).
func isPageInTrashRefusal(err error) bool {
	refusal := &graphRequestError{}
	return errors.As(err, &refusal) && refusal.code == errorCodePageInTrash
}

// pageInTrashErrorWhen is pageInTrashError for a page in the trash, nil for
// one that isn't.
func pageInTrashErrorWhen(trashed bool) error {
	if trashed {
		return pageInTrashError()
	}
	return nil
}
