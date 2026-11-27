package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Where a copy or a move of a page to another workspace goes is refused for
// what it is (api_error_refused_not_changed.go) — a workspace or teamspace
// the person can't add pages to, a parent there they can't add to, a place
// the organization keeps from restricted members, a place that holds no
// pages — never as the page busy or changed (409 "can't be duplicated at the
// current location", "can't be moved there now", 2026-11-26), which an app
// read as a copy already under way. Each names the field that chose the
// place, under the route's own invalid code (code) or permission denied.

// destinationWorkspaceRefused refuses a workspace the person can't add pages
// to: one they read as such, any other as no workspace of theirs.
func destinationWorkspaceRefused(app core.App, actorID, workspaceID, code string) error {
	field := apiErrorField{Name: "destinationWorkspaceId", Problem: "not one you can add pages to"}
	if _, err := authorizeWorkspace(app, actorID, workspaceID, permissionWorkspaceRead); err == nil {
		return refusedFor(http.StatusForbidden, errorCodePermissionDenied, "Check destinationWorkspaceId: you can read that workspace "+
			"but not add pages to it; one of its owners can let you.", field)
	}
	return refusedFor(http.StatusBadRequest, code, "Check destinationWorkspaceId: no workspace you belong to has that ID.", field)
}

// destinationTeamspaceRefused is why a teamspace of the workspace a page goes
// to doesn't take it, or nil: one of that workspace whose pages the person
// may add to does.
func destinationTeamspaceRefused(app core.App, actorID, workspaceID, teamspaceID, code string) error {
	teamspaceWorkspace, role, err := effectiveTeamspacePageAccessRole(app, actorID, workspaceID, teamspaceID)
	field := apiErrorField{Name: "destinationTeamspaceId", Problem: "not one you can add pages to"}
	switch {
	case errors.Is(err, errTeamspaceNotFound) || err == nil && teamspaceWorkspace.WorkspaceID != workspaceID:
		return refusedFor(http.StatusBadRequest, code, "Check destinationTeamspaceId: no teamspace of the workspace it goes to has that ID.", field)
	case isPermissionDenied(err) || err == nil && role < pageAccessRoleEdit:
		return refusedFor(http.StatusForbidden, errorCodePermissionDenied, "Check destinationTeamspaceId: it isn't a teamspace of that "+
			"workspace you can add pages to; name one you edit in, or ask its owners to let you.", field)
	}
	return err
}

// destinationPrivatePagesRefused refuses the top of a restricted member's
// private pages, which their organization keeps them from making.
func destinationPrivatePagesRefused(field string) error {
	return refusedFor(http.StatusForbidden, errorCodePermissionDenied, "Check "+field+": the organization keeps its restricted members "+
		"from making private pages there; name a teamspace (destinationTeamspaceId) or a page (destinationParentId) to go in.",
		apiErrorField{Name: field, Problem: "private pages not allowed"})
}

// destinationParentRefused is why a page there doesn't take the page that
// goes under it, or nil: one of that workspace the person may add pages to —
// for a page moved over (moved), outside the trash and no database's item
// (a copy's place says those itself, pageDuplicationPlacementFailure).
func destinationParentRefused(parent pageAuthorization, err error, workspaceID, code string, moved bool) error {
	field := apiErrorField{Name: "destinationParentId", Problem: "not a page this can go under"}
	switch {
	case isPermissionDenied(err):
		return refusedFor(http.StatusForbidden, errorCodePermissionDenied, "Check destinationParentId: you can't add pages under that page.", field)
	case err != nil:
		return err
	case parent.Workspace.WorkspaceID != workspaceID:
		return refusedFor(http.StatusBadRequest, code, "Check destinationParentId: that page is in another workspace than the one "+
			"it goes to; name its workspace as destinationWorkspaceId.", field)
	case moved && parent.Page.GetBool("in_trash"):
		return refusedFor(http.StatusConflict, errorCodePageInTrash, "Check destinationParentId: that page is in the Trash; "+
			"restore it first, or name another.", field)
	case moved && isDatabaseRowPageRecord(parent.Page):
		return refusedFor(http.StatusBadRequest, code, "Check destinationParentId: that page is a database's item, "+
			"which takes no page moved over; name a page, or a teamspace (destinationTeamspaceId).", field)
	}
	return nil
}

// destinationRefused says err is such a refusal: a move under way whose place
// refuses it now has lost it (page_workspace_move_runner.go).
func destinationRefused(err error) bool {
	var refusal *refusedForReason
	return errors.As(err, &refusal)
}

// rowUnderPageRefused refuses a database's item moved out under a page that
// takes no page now: a locked one, until it is unlocked, or a full-page
// database, which holds only its database (2026-11-26: both were 409 "The
// page can't be moved there.").
func rowUnderPageRefused(parentID string, parent *core.Record) error {
	field := apiErrorField{Name: "targetPageId", Problem: "not a page this can go under"}
	if parent.GetBool("is_locked") {
		return refusedFor(http.StatusLocked, errorCodePageLocked, "Check targetPageId: the page "+parentID+" is locked; "+
			"unlock it first, or name another.", field)
	}
	return refusedFor(http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check targetPageId: the page "+parentID+" is a full-page "+
		"database, which holds only its database; move the row into it by its data source (targetDataSourceId).", field)
}

// rowUnderPageInsideRefused refuses, when within, a database's item moved
// under a page inside itself; nil otherwise.
func rowUnderPageInsideRefused(parentID string, within bool) error {
	if !within {
		return nil
	}
	return refusedFor(http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check targetPageId: the page "+parentID+
		" is inside this row's page; name a page outside it.", apiErrorField{Name: "targetPageId", Problem: "inside the row"})
}

// refusalWords are a refusal's words without the field it names first, for
// a caller that names its fields otherwise (an MCP tool, Notion's API).
func refusalWords(err error) (string, bool) {
	var refusal *refusedForReason
	if !errors.As(err, &refusal) {
		return "", false
	}
	if _, words, named := strings.Cut(refusal.message, ": "); named && strings.HasPrefix(refusal.message, "Check ") {
		return words, true
	}
	return refusal.message, true
}
