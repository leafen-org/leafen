package app

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A full-page database is its page: the trash holds it as a page, not among
// its databases, whether notion-delete-pages, notion-delete-databases or the
// page's own move to the trash sent it there, or it went with a page above
// it. Restoring it or deleting it for good through the trash's databases
// names the page's own way instead of a database that isn't found, as an
// agent that put it in the trash looks for it there by the database's ID
// (2026-11-16: restore and delete said "Database not found in the trash." of
// such a database), as the deleted pages name the trash's own way for a page
// that is only in the trash (page_deletion_hold_trash.go). A database deleted
// from a page that is now in the trash comes back after its page does.

// deletedDatabaseNotFound is why the trash's databases don't hold one: an ID
// naming a full-page database whose page is in the trash, for a person who
// may edit that page, answers with that page's way back and its deletion for
// good; any other keeps err, a database that isn't found.
func deletedDatabaseNotFound(app core.App, userID, workspaceID, id string, err error) error {
	if !errors.Is(err, sql.ErrNoRows) && !isPermissionDenied(err) && !errors.Is(err, errDatabaseRowPageNotFound) {
		return err
	}
	pageID, found := fullPageDatabasePageIn(app, workspaceID, id)
	if !found {
		return err
	}
	if _, denied := authorizePage(app, userID, pageID, permissionWorkspaceWrite); denied != nil {
		return err
	}
	effect, trashed, readErr := loadPageTrashEffect(app, pageID)
	if readErr != nil {
		return readErr
	}
	if !trashed {
		return err
	}
	restore := fmt.Sprintf("notion-restore-pages or PATCH /pages/%s/metadata with inTrash false brings it back", pageID)
	where := "is in the trash"
	if effect.RootID != "" && effect.RootID != pageID {
		// One under a page in the trash comes back with that page.
		where = fmt.Sprintf("is in the trash with the page %s above it", effect.RootID)
		restore = fmt.Sprintf("notion-restore-pages or PATCH /pages/%s/metadata with inTrash false on that page brings them back", effect.RootID)
	}
	return graphConflictError(errorCodePageInTrash, fmt.Sprintf(
		"This is a full-page database whose page %s %s, not a database deleted from its page: %s, POST /pages/%s/permanent-delete deletes it for good.",
		pageID, where, restore, pageID,
	))
}

// deletedDatabasePageInTrash is why a database deleted from its page doesn't
// come back while that page is in the trash, or under a page that is: it
// names the page to bring back first. A page out of the trash keeps err.
func deletedDatabasePageInTrash(app core.App, pageID string, err error) error {
	effect, trashed, readErr := loadPageTrashEffect(app, pageID)
	if readErr != nil {
		return readErr
	}
	if !trashed {
		return err
	}
	rootID := pageID
	if effect.RootID != "" {
		rootID = effect.RootID
	}
	return graphConflictError(errorCodePageInTrash, fmt.Sprintf(
		"The database's page %s is in the trash: notion-restore-pages or PATCH /pages/%s/metadata with inTrash false brings the page back, then the database can be restored; it can be deleted for good as it is.",
		pageID, rootID,
	))
}

// fullPageDatabasePageIn is the page of the workspace's full-page database
// an ID names — the database's, its page's or its own data source's — in the
// trash or not.
func fullPageDatabasePageIn(app core.App, workspaceID, id string) (string, bool) {
	if id == "" {
		return "", false
	}
	pageID := ""
	err := app.DB().NewQuery(`
		SELECT [[container]].[[page_id]] FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[container]].[[page_id]]
		WHERE [[container]].[[workspace_id]] = {:workspaceId} AND [[container]].[[kind]] = 'source'
			AND [[container]].[[parent_block_id]] = '' AND [[page]].[[kind]] = {:databaseKind}
			AND [[container]].[[id]] IN (
				SELECT {:id}
				UNION ALL
				SELECT [[held]].[[id]] FROM {{leafen_database_containers}} AS [[held]] WHERE [[held]].[[page_id]] = {:id}
				UNION ALL
				SELECT [[membership]].[[container_id]] FROM {{leafen_database_container_sources}} AS [[membership]]
				WHERE [[membership]].[[data_source_id]] = {:id} AND [[membership]].[[source_container_id]] = ''
			)
		ORDER BY [[container]].[[position]], [[container]].[[id]] LIMIT 1
	`).Bind(dbx.Params{"workspaceId": workspaceID, "id": id, "databaseKind": pageKindDatabase}).Row(&pageID)
	return pageID, err == nil && pageID != ""
}
