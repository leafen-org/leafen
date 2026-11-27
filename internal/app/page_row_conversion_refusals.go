package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Why a page can't become a database's item there, an item a page, or an
// item a row of another database, is said as that refusal (api_error_refused_not_changed.go) with what to do,
// in the same words on the app's routes, the agent's tool and Notion's API
// (refusalWords): the access it takes, a database that takes no pages, a
// database inside the page, a lock, the Trash, more to let go of than one
// change holds, a wiki being made there (2026-11-26: each was 409 "The page
// can't be moved there." or "The database row changed. Try again.", which
// sent an agent to send the same move again). Only a change met meanwhile
// stays the conflict it is (errPageRowConversionConflict,
// errDatabaseRowPageConflict).

// pageIntoDatabaseStateRefused refuses a page that is in the Trash or is a
// database's item already; nil for any other.
func pageIntoDatabaseStateRefused(page *core.Record) error {
	field := apiErrorField{Name: "pageId", Problem: "not a page this can move"}
	switch {
	case page.GetBool("in_trash"):
		return refusedFor(http.StatusConflict, errorCodePageInTrash, "Check pageId: the page is in the Trash; restore it first, then move it.", field)
	case page.GetString("data_source_id") != "":
		return refusedFor(http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check pageId: it is a database's item already; "+
			"move it to another database as a row, by that database's data source.", field)
	}
	return nil
}

// pageIntoDatabaseAccessRefused refuses a page moved into a database in
// another place than its own — a teamspace, or private pages — by someone
// without full access to it.
func pageIntoDatabaseAccessRefused() error {
	return refusedFor(http.StatusForbidden, errorCodeDatabaseRowPageMoveDenied, "Check pageId: the database is in another place than "+
		"the page (a teamspace or private pages), and moving a page to another place takes full access to it; ask someone with "+
		"full access to the page to move it, or to give you full access.", apiErrorField{Name: "pageId", Problem: "full access needed"})
}

// pageIntoDatabaseSourceRefused is why the data source a page moves into
// takes no page: in the Trash, or one the server keeps for itself
// (databaseSourceTakesAddedRowsSQL); errPageRowConversionConflict for one
// gone or moved to another page meanwhile.
func pageIntoDatabaseSourceRefused(app core.App, dataSourceID, containerPageID string) error {
	var pageID string
	var shown, takes bool
	err := app.DB().NewQuery(`
		SELECT [[page_id]], `+databaseSourceNotTrashedSQL("{:id}")+`, `+databaseSourceTakesAddedRowsSQL("{:id}")+`
		FROM {{leafen_data_sources}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": dataSourceID}).Row(&pageID, &shown, &takes)
	field := apiErrorField{Name: "dataSourceId", Problem: "takes no pages"}
	switch {
	case errors.Is(err, sql.ErrNoRows) || err == nil && pageID != containerPageID:
		return errPageRowConversionConflict
	case err != nil:
		return err
	case !shown:
		return refusedFor(http.StatusConflict, errorCodePageInTrash, "Check dataSourceId: that database is in the Trash; restore it first, "+
			"then move the page into it.", field)
	case !takes:
		return refusedFor(http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check dataSourceId: that data source is one Leafen "+
			"keeps — a workspace's People, or a view's own — which takes no pages; move the page into a database of its own.", field)
	}
	return nil
}

// pageIntoOwnDatabaseRefused refuses, when within, a page moved into a
// database inside it; nil otherwise.
func pageIntoOwnDatabaseRefused(within bool) error {
	if !within {
		return nil
	}
	return refusedFor(http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check dataSourceId: that database is inside this page, "+
		"and a page can't go into a database it holds; name a database outside it.", apiErrorField{Name: "dataSourceId", Problem: "inside the page"})
}

// pageIntoDatabaseLockedRefused refuses a page moved into a database while
// the page holding the database, or the page it is under, is locked
// (pageMoveLockConflict).
func pageIntoDatabaseLockedRefused(page *core.Record, container *core.Record) error {
	if container.GetBool("is_locked") {
		return refusedFor(http.StatusLocked, errorCodePageLocked, "Check dataSourceId: the page "+container.Id+", which holds that "+
			"database, is locked; unlock it first, then move the page into it.", apiErrorField{Name: "dataSourceId", Problem: "locked"})
	}
	return refusedFor(http.StatusLocked, errorCodePageLocked, "Check pageId: the page it is under, "+page.GetString("parent_id")+
		", is locked; unlock it first, then move it.", apiErrorField{Name: "pageId", Problem: "locked"})
}

// rowIntoDatabaseRefused refuses a database's item moved as a row into the
// database it is in (already), or into a data source Leafen keeps for
// itself (databaseSourceTakesAddedRowsSQL).
func rowIntoDatabaseRefused(already bool) error {
	field := apiErrorField{Name: "targetDataSourceId", Problem: "takes no move of this row"}
	if already {
		return refusedFor(http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check targetDataSourceId: this item is in that "+
			"database already; name another database to move it to, or a page to move it out under as a page of its own.", field)
	}
	return refusedFor(http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check targetDataSourceId: that data source is one "+
		"Leafen keeps — a workspace's People, or a view's own — which takes no pages; name a database of its own.", field)
}

// rowInTrashRefused refuses a change to a database's item in the Trash.
func rowInTrashRefused() error {
	return refusedFor(http.StatusConflict, errorCodeDatabaseRowPageInTrash, "Check rowId: this database item is in the Trash; restore it first.",
		apiErrorField{Name: "rowId", Problem: "in the Trash"})
}

// rowLeaveTooLarge refuses a database's item whose leaving its database
// would take more changes than one change holds: its sub-items, which stay
// as rows of their own, and the rows relating to it, which let it go.
func rowLeaveTooLarge(changes int) error {
	return refusedFor(http.StatusUnprocessableEntity, errorCodeDatabaseRowPageInvalid, fmt.Sprintf("Check rowId: taking this item out of "+
		"its database changes %d of its sub-items and the rows relating to it, more than one change holds (%d); move some of its "+
		"sub-items out of it first, then move it.", changes, maxGraphMutationOperations), apiErrorField{Name: "rowId", Problem: "too much to let go"})
}

// rowUnderPageAccessRefused refuses a database's item moved out under a page
// the mover can't add pages under.
func rowUnderPageAccessRefused(parentID string) error {
	return refusedFor(http.StatusForbidden, errorCodeDatabaseRowPageMoveDenied, "Check targetPageId: you can't add pages under the page "+
		parentID+"; name a page you can edit, or ask its owners to let you.", apiErrorField{Name: "targetPageId", Problem: "not one you can add pages to"})
}

// rowToPlaceAccessRefused refuses a database's item moved out to another
// place than its database's (field names it) by someone without full access
// to it (authorizeDatabaseRowPlaceMove).
func rowToPlaceAccessRefused(field string) error {
	return refusedFor(http.StatusForbidden, errorCodeDatabaseRowPageMoveDenied, "Check "+field+": that is another place than this "+
		"item's database (a teamspace or private pages), and moving it there takes full access to it; ask someone with full access "+
		"to the item to move it, or to give you full access.", apiErrorField{Name: field, Problem: "full access needed"})
}

// rowToTopAccessRefused refuses a database's item moved to the top of a
// teamspace the mover can't add pages to (teamspace), or of private pages
// in a workspace where they can't make pages of their own.
func rowToTopAccessRefused(teamspace bool) error {
	field := apiErrorField{Name: "targetTeamspaceId", Problem: "not one you can add pages to"}
	if teamspace {
		return refusedFor(http.StatusForbidden, errorCodeDatabaseRowPageMoveDenied, "Check targetTeamspaceId: it isn't a teamspace of "+
			"this item's workspace you can add pages to; name one you edit in, or ask its owners to let you.", field)
	}
	return refusedFor(http.StatusForbidden, errorCodeDatabaseRowPageMoveDenied, "Check targetTeamspaceId: you can't make pages of your "+
		"own in this workspace; name a teamspace you edit in, or a page (targetPageId), to move it to.", field)
}

// rowUnderTrashedPageRefused refuses, when trashed, a database's item moved
// out under a page in the Trash or under one that is; nil otherwise.
func rowUnderTrashedPageRefused(parentID string, trashed bool) error {
	if !trashed {
		return nil
	}
	return refusedFor(http.StatusConflict, errorCodePageInTrash, "Check targetPageId: the page "+parentID+" is in the Trash, or under "+
		"a page that is; restore it first, or name another.", apiErrorField{Name: "targetPageId", Problem: "in the Trash"})
}

// rowUnderWikiBusyRefused refuses, when fenced, a database's item moved
// under a page whose wiki is being made or undone, until that is done; nil
// otherwise.
func rowUnderWikiBusyRefused(parentID string, fenced bool) error {
	if !fenced {
		return nil
	}
	return refusedFor(http.StatusConflict, errorCodeDatabaseRowPageConflict, "Check targetPageId: the wiki the page "+parentID+
		" is in is being made or undone; move the item there once that is done.", apiErrorField{Name: "targetPageId", Problem: "wiki being changed"})
}
