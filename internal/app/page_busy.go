package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A page under an operation that holds it for a while — its blocks being
// moved, its permanent deletion, a wiki's change, its copy — takes no edits
// until it is done. That isn't a matter of permission: the page says why
// and that it will take edits again, rather than that the person may no
// longer edit (2026-10-16: an owner's page being copied read as read-only
// for them, with "request edit access").
const (
	pageBusyMoving   = "moving"
	pageBusyDeleting = "deleting"
	pageBusyWiki     = "wiki"
	pageBusyCopying  = "copying"
	// The page's workspace moved to another server (workspace_move_fence.go).
	pageBusyWorkspaceMoved = "workspaceMoved"
	// The page's workspace is being moved: it takes edits again if the move
	// stops, so it isn't said to have moved (2026-11-07: "옮겨져" while the
	// move was still bundling it).
	pageBusyWorkspaceSending = "workspaceSending"
	// The page is moving to another workspace on this server
	// (page_workspace_move.go): it takes edits again there once it arrives,
	// or here if the move goes back.
	pageBusyWorkspaceMoving = "workspaceMoving"
)

// pageBusyReason says which operation holds the page, or the page above it,
// or "" when none does.
func pageBusyReason(app core.App, pageID string) (string, error) {
	return pageBusyReasonOf(app, pageID, true)
}

// pageBusyReasonOf is pageBusyReason, its databases being copied into the
// page counted when copies is set (database_row_page_database_copies.go).
func pageBusyReasonOf(app core.App, pageID string, copies bool) (string, error) {
	page, err := findRecordRemembered(app, pagesCollection, pageID)
	if err != nil {
		return "", err
	}
	if _, moved := workspaceMovedTo(app, page.GetString("workspace")); moved {
		if readWorkspaceMoveOut(app, page.GetString("workspace")).State == workspaceMoveSending {
			return pageBusyWorkspaceSending, nil
		}
		return pageBusyWorkspaceMoved, nil
	}
	underWay, err := pageDuplicationTargetUnderWay(app, page)
	if err != nil {
		return "", err
	}
	if underWay {
		return pageBusyCopying, nil
	}
	// Its databases being copied into it, by their copies' moves too.
	if copies {
		if copying, err := rowPageDatabasesCopying(app, pageID); err != nil || copying {
			return pageBusyCopyingDatabases, err
		}
	}
	fences, err := workspacePageFences(app, page.GetString("workspace"))
	if err != nil {
		return "", err
	}
	checks := []struct {
		on     bool
		reason string
		active func(core.App, string) (bool, error)
	}{
		{fences.Moving, pageBusyMoving, blockMovePageFenceActive},
		{fences.Deleting, pageBusyDeleting, pagePermanentDeleteFenceActive},
		{fences.Wiki, pageBusyWiki, wikiMutationFenceActive},
		{fences.Duplicating, pageBusyCopying, pageDuplicationHeldFenceActive},
		{fences.WorkspaceMoving, pageBusyWorkspaceMoving, pageWorkspaceMoveHeldPage},
	}
	for _, check := range checks {
		if !check.on {
			continue
		}
		if active, err := check.active(app, pageID); err != nil || active {
			return check.reason, err
		}
	}
	return "", nil
}

// pageBusyForMember is the operation holding the page, told only to someone
// for whom a refusal there is about the operation; to anyone else it stays
// "not found". Blocks being moved, a wiki's change or a copy are told to
// whoever may open the page apart from them — a member it opens to, or a
// guest it is shared with (page_access_hold.go; 2026-11-23: a member a
// private page wasn't shared with heard it held, and a guest it was shared
// with heard it not found); a deletion for good, its workspace moving, or its
// databases being copied into it, which the page's decision doesn't tell
// apart, to a member of its workspace.
func pageBusyForMember(app core.App, principalID, pageID string) string {
	return pageBusyForMemberOf(app, principalID, pageID, true)
}

// pageBusyForMemberOf is pageBusyForMember, its databases being copied into
// the page counted when copies is set (pageBusyReasonOf).
func pageBusyForMemberOf(app core.App, principalID, pageID string, copies bool) string {
	busy, err := pageBusyReasonOf(app, pageID, copies)
	if err != nil || busy == "" {
		return ""
	}
	switch busy {
	case pageBusyMoving, pageBusyWiki, pageBusyCopying, pageBusyWorkspaceMoving:
		if _, err := pageAccessDespiteHold(authorizePageAccessRole(app, principalID, pageID, pageAccessRoleView)); err != nil {
			return ""
		}
		return busy
	}
	page, err := findRecordRemembered(app, pagesCollection, pageID)
	if err != nil {
		return ""
	}
	if _, err := authorizeWorkspace(app, principalID, page.GetString("workspace"), permissionWorkspaceRead); err != nil {
		return ""
	}
	return busy
}

// pageSetBusyRefusal is a read of pages refused as busy when an operation
// holds one of them, told only to a member of its workspace, as a page's own
// read is (content_graph.go): its databases being copied into a row page
// fence it off for a while, and a visit to it, or a look at its room's
// storage, would otherwise read as a page not found (2026-11-14). nil when
// none is held, the refusal staying "not found".
func pageSetBusyRefusal(app core.App, principalID string, pageIDs []string) error {
	for _, pageID := range pageIDs {
		if busy := pageBusyForMember(app, principalID, pageID); busy != "" {
			return pageBusyError(busy)
		}
	}
	return nil
}

// pageHeldForMember is the operation holding a page for a while, told only
// to a member of its workspace (pageBusyForMemberOf): one that ends with the
// page back, so a road that says a page is held says to try again. A
// permanent deletion ends with the page gone, out of everyone's sight at
// once and kept 30 days only for its owner's recovery (page_deletion_hold.go),
// so such a road tells it as gone; databases being copied into a row page
// refuse no one, so they don't count. "" for any other page.
func pageHeldForMember(app core.App, principalID, pageID string) string {
	if busy := pageBusyForMemberOf(app, principalID, pageID, false); busy != pageBusyDeleting {
		return busy
	}
	return ""
}

// pageBusyRefusal is a write refused while an operation holds the page,
// naming the operation (pageBusyReasonOf); it answers as its graph refusal.
type pageBusyRefusal struct {
	*graphRequestError
	reason string
}

func (refusal *pageBusyRefusal) Unwrap() error { return refusal.graphRequestError }

// pageBusyError is a write refused while an operation holds the page.
func pageBusyError(reason string) error {
	return &pageBusyRefusal{
		graphRequestError: &graphRequestError{status: http.StatusLocked, code: errorCodePageBusy, message: pageBusyMessage(reason)},
		reason:            reason,
	}
}

// pageBusyMessage says which operation holds the page and when it takes
// changes again, as every road that tells a held page says it.
func pageBusyMessage(reason string) string {
	switch reason {
	case pageBusyCopying, pageBusyCopyingDatabases:
		return "This page is being copied; it takes edits again once the copy is done."
	case pageBusyMoving:
		return "Blocks of this page are being moved; it takes edits again once that's done."
	case pageBusyWiki:
		return "This page is turning into a wiki or back; it takes edits again once that's done."
	case pageBusyDeleting:
		return "This page is being deleted permanently."
	case pageBusyWorkspaceMoved:
		return "This page's workspace moved to another server; it takes no changes here."
	case pageBusyWorkspaceSending:
		return "This page's workspace is moving to another server; it takes no changes here meanwhile."
	case pageBusyWorkspaceMoving:
		return "This page is being moved to another workspace; it opens again once it's there."
	}
	return "This page is busy with another change; it takes edits again once that's done."
}

// pageHeldRefusal is the operation that err, or the error it carries, says
// holds its page for a while (pageBusyError), which a road that words
// refusals its own way tells as busy rather than as a failure; "" when it
// says none, or says the page is being deleted for good (pageHeldForMember).
func pageHeldRefusal(err error) string {
	var refusal *pageBusyRefusal
	if !errors.As(err, &refusal) || refusal.reason == pageBusyDeleting {
		return ""
	}
	return refusal.reason
}
