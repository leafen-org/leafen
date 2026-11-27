package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A road whose address names a page — the page itself (its pageId, a page
// road's id, a page's collaboration room), a database item's page (rowId), or
// the page holding a database (its containerId) or a data source
// (dataSourceId) — that refuses the page as not found, the item or the
// database as not found, or the person as without permission, while an
// operation holds that page for a while (page_busy.go) is answered held
// instead, as the page's own content and sharing routes answer it
// (page_held_refusal.go): 423 LEAFEN_PAGE_BUSY naming the page and the
// operation, whatever road it was — its history, its lock, a comment, a file
// put on it, an item's values, a database query (2026-11-23: a page whose
// blocks were being moved answered "Page not found." on a dozen of its roads,
// its items "not found" and its writes "no permission", each its own
// refusal). It is said only to someone — a member or a guest — who has the
// role the road takes apart from the hold (page_access_hold.go): to read, for
// a road that reads or keeps the person's own (a favorite); to comment, for
// one on its comments; to edit, for any other write. One without it keeps the refusal they would have had: a page
// they can't open stays not found, an edit they may not make stays no
// permission. A page not there, or one being deleted for good, stays as it
// was. The error writer answers every failure through this rule
// (failureAnswer).

// heldPageFailureAnswer is the answer to a road refused while an operation
// holds the page its address names; the answer as it was otherwise.
func heldPageFailureAnswer(event *core.RequestEvent, status int, code, message string) (int, string, string) {
	if !heldPageRefusal(status, code) || event == nil || event.Request == nil || event.Auth == nil {
		return status, code, message
	}
	pageID := failurePageAddress(event)
	if pageID == "" {
		return status, code, message
	}
	busy := failureRoadHeld(event.App, event.Auth.Id, pageID, failureRoadRole(event.Request))
	if busy == "" {
		return status, code, message
	}
	apiErrorAbout(event, &apiErrorTarget{Kind: "page", ID: pageID})
	apiErrorBecause(event, "", busy)
	return http.StatusLocked, errorCodePageBusy, pageBusyMessage(busy)
}

// heldPageRefusal reports whether a failure is one a held page's roads
// answer: its page, item or database not found, or no permission.
func heldPageRefusal(status int, code string) bool {
	switch {
	case status == http.StatusNotFound:
		return code == errorCodePageNotFound || code == errorCodeDatabaseRowPageNotFound ||
			code == errorCodeDatabaseNotFound || code == errorCodeDatabaseDataSourceNotFound
	case status == http.StatusForbidden:
		return code == errorCodePermissionDenied
	}
	return false
}

// failurePageAddress is the page a road's address names, "" for none.
func failurePageAddress(event *core.RequestEvent) string {
	request := event.Request
	for _, name := range []string{"pageId", "rowId", "room"} {
		if id := strings.TrimSpace(request.PathValue(name)); id != "" {
			return id
		}
	}
	if strings.HasPrefix(request.URL.Path, apiRoot+"/pages/") {
		return strings.TrimSpace(request.PathValue("id"))
	}
	for _, holder := range []struct{ name, table string }{
		{"dataSourceId", "leafen_data_sources"}, {"containerId", "leafen_database_containers"},
	} {
		id := strings.TrimSpace(request.PathValue(holder.name))
		if id == "" {
			continue
		}
		pageID := ""
		err := event.App.DB().NewQuery(`SELECT [[page_id]] FROM {{` + holder.table + `}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": id}).Row(&pageID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ""
		}
		return pageID
	}
	return ""
}

// failureRoadHeld is the operation holding the page a road names for a while,
// from a person the page gives the road's role apart from the hold — a
// database item's page as its items are decided, through its database's page
// or its own shares (database_row_page.go); "" otherwise.
func failureRoadHeld(app core.App, principalID, pageID string, role pageAccessRole) string {
	if row, err := isDatabaseRowPageRoom(app, pageID); err == nil && row {
		requirement := permissionWorkspaceWrite
		if role == pageAccessRoleView {
			requirement = permissionWorkspaceRead
		}
		_, err := authorizeDatabaseRowPage(app, principalID, pageID, requirement)
		return pageAccessHeld(err)
	}
	_, err := authorizePageAccessRole(app, principalID, pageID, role)
	return pageAccessHeld(err)
}

// failureRoadRole is the role a road takes on its page: to read for a read,
// or for what is the person's own about it — their favorite, their notice
// setting —, to comment for one on comments, to edit for any other write.
func failureRoadRole(request *http.Request) pageAccessRole {
	path := request.URL.Path
	switch {
	case request.Method == http.MethodGet || request.Method == http.MethodHead,
		strings.HasSuffix(path, "/favorite"), strings.HasSuffix(path, "/notification-preference"):
		return pageAccessRoleView
	case strings.Contains(path, "/comments") || strings.Contains(path, "/discussions"):
		return pageAccessRoleComment
	}
	return pageAccessRoleEdit
}
