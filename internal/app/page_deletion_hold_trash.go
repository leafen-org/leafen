package app

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The deleted pages (page_deletion_hold.go) are those deleted for good, in
// their 30 days; a page in the trash isn't one of them, whether it went there
// itself or with a page above it. Its recover or purge names the way back of
// the page whose trash holds it, and its own deletion for good, instead of a
// page that isn't found, as an agent that put it in the trash with
// notion-delete-pages looks for it there (2026-11-16: recover and purge said
// "Page not found." of such a page, and of one under a page in the trash),
// as the trash's databases do for a full-page database
// (deleted_database_page_trash.go). A page the workspace doesn't hold is
// still not found.

var errPageDeletionInTrash = errors.New("the page is in the trash, not deleted for good")

// pageDeletionInTrashError is a page in the trash, by itself (rootID is the
// page) or with the page rootID above it.
type pageDeletionInTrashError struct{ pageID, rootID string }

func (failure *pageDeletionInTrashError) Error() string { return errPageDeletionInTrash.Error() }

func (failure *pageDeletionInTrashError) Is(target error) bool {
	return target == errPageDeletionInTrash
}

// message says where the page is and its two ways: the restore of the page
// whose trash holds it, and its own deletion for good.
func (failure *pageDeletionInTrashError) message() string {
	if failure.rootID == "" || failure.rootID == failure.pageID {
		return fmt.Sprintf(
			"The page %s is in the trash, not deleted for good: notion-restore-pages or PATCH /pages/%s/metadata with inTrash false brings it back, POST /pages/%s/permanent-delete deletes it for good.",
			failure.pageID, failure.pageID, failure.pageID,
		)
	}
	return fmt.Sprintf(
		"The page %s is in the trash with the page %s above it, not deleted for good: notion-restore-pages or PATCH /pages/%s/metadata with inTrash false on that page brings them back, POST /pages/%s/permanent-delete deletes it for good.",
		failure.pageID, failure.rootID, failure.rootID, failure.pageID,
	)
}

// pageDeletionInTrashMessage is the message of a page in the trash that
// isn't deleted for good.
func pageDeletionInTrashMessage(err error) string {
	var failure *pageDeletionInTrashError
	if errors.As(err, &failure) {
		return failure.message()
	}
	return "The page is in the trash, not deleted for good: notion-restore-pages or PATCH /pages/{pageId}/metadata with inTrash false brings it back, POST /pages/{pageId}/permanent-delete deletes it for good."
}

// deletedPageNotHeld is why a page that isn't in its 30 days can't be
// recovered or purged: it is in the workspace's trash, itself or under a
// page that is, or isn't found.
func deletedPageNotHeld(app core.App, workspaceID, pageID string) error {
	held := false
	err := app.DB().NewQuery(`
		SELECT 1 FROM {{leafen_pages}} AS [[page]]
		WHERE [[page]].[[id]] = {:pageId} AND [[page]].[[workspace]] = {:workspaceId}
		LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "workspaceId": workspaceID}).Row(&held)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !held) {
		return permissionDenied()
	}
	if err != nil {
		return err
	}
	effect, trashed, err := loadPageTrashEffect(app, pageID)
	if err != nil {
		return err
	}
	if !trashed {
		return permissionDenied()
	}
	return &pageDeletionInTrashError{pageID: pageID, rootID: effect.RootID}
}
