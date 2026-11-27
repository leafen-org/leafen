package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database row's page is a page whose id is its row's id, and its own
// paths are /database-rows/{rowId}/…. A page's path given that id answers
// as one of three: as the page's (reading it, sharing it, favoriting it,
// copying it into private pages), as the row's own path where the two take
// the same request (its comments, reactions and suggestions — Notion's API
// takes a row's comments by its page id), or, where they don't (deleting it
// for good), naming the row's own path. Anyone who can't read the row is
// answered as the page path answers anyone, so nothing tells them a row is
// there.

// commentsOfRowPage answers a page's comment path for a database row's page
// as the row's own path does (/database-rows/{rowId}/…): a row's page id is
// its row id, and Notion's API takes a row's comments by its page id. One
// who can't read the row is answered as the page path answers anyone.
func commentsOfRowPage(page, row func(*core.RequestEvent) error) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		id := strings.TrimSpace(event.Request.PathValue("pageId"))
		if user, ok := authenticatedUser(event); ok && readableDatabaseRowPage(event.App, user.Id, id) {
			event.Request.SetPathValue("rowId", id)
			return row(event)
		}
		return page(event)
	}
}

// readableDatabaseRowPage says the id names a database row's page (or a
// template's) the person reads.
func readableDatabaseRowPage(app core.App, personID, id string) bool {
	if !databaseRowPageID(app, id) {
		return false
	}
	_, _, err := authorizeDatabaseRowPageRead(app, personID, id)
	return err == nil
}

// databaseRowPageID says the id is a database row's page.
func databaseRowPageID(app core.App, id string) bool {
	var dataSourceID string
	return id != "" && app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": id}).Row(&dataSourceID) == nil && dataSourceID != ""
}

// rowPageDeletedOnItsOwnPath answers a page's permanent deletion given a
// row's page — which the row's own path does, with its authority's revision —
// by naming that path, for one who reads the row, in the trash too; it says
// whether it answered.
func rowPageDeletedOnItsOwnPath(event *core.RequestEvent, personID, id string) (bool, error) {
	if !databaseRowPageID(event.App, id) {
		return false, nil
	}
	if _, _, err := authorizeDatabaseRowPageRead(event.App, personID, id); err != nil {
		if _, err := authorizeDatabaseRowPageInTrash(event.App, personID, id, permissionWorkspaceRead); err != nil {
			return false, nil
		}
	}
	page := strings.Replace(databaseRowPageReadPath, "{rowId}", id, 1)
	return true, apiError(event, http.StatusNotFound, errorCodePageNotFound, "This is a database row's page: delete it for good with POST "+
		strings.Replace(databaseRowPageDeletePermanentlyPath, "{rowId}", id, 1)+", with the expectedAuthorityRevision GET "+page+
		" reads (row.authorityRevision).")
}
