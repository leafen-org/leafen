package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A guest invite link's routes (page_guest_links.go) say a refusal in the
// link's own words (2026-11-23: turning off a link no one had answered
// "Public sharing settings not found.", and a request it couldn't take
// "Check the public sharing request."): what they couldn't find by its kind
// and ID — the link, a page, the workspace — as every not-found says it
// (api_error_details.go), a request they can't take as the link's, and a
// failure as theirs. What the sharing routes say of their own things — an
// organization's policy, a page an operation holds (the pages in a link's
// body too), the owners inviting guests — they say as those routes do.

// pageGuestLinkMissing is what a guest invite link route couldn't find, or
// isn't the person's: its kind and ID.
type pageGuestLinkMissing struct {
	kind, id string
	cause    error
}

func (missing pageGuestLinkMissing) Error() string {
	return missing.kind + " " + missing.id + " not found"
}

func (missing pageGuestLinkMissing) Unwrap() error { return missing.cause }

// refusedPage is the page it couldn't reach, for a page an operation holds
// to say so (page_held_refusal.go): the pages a link is made for come in its
// body, not its address.
func (missing pageGuestLinkMissing) refusedPage() string {
	if missing.kind == pageGuestLinkPageKind {
		return missing.id
	}
	return ""
}

const (
	pageGuestLinkKind          = "guest invite link"
	pageGuestLinkPageKind      = "page"
	pageGuestLinkWorkspaceKind = "workspace"
)

// pageGuestLinkMissingIf is err as the thing of kind and id missing, where it
// says the person can't reach it or it isn't there.
func pageGuestLinkMissingIf(err error, kind, id string) error {
	if isPermissionDenied(err) || errors.Is(err, errPublicMutationNotFound) || errors.Is(err, sql.ErrNoRows) {
		return pageGuestLinkMissing{kind: kind, id: id, cause: err}
	}
	return err
}

// pageGuestLinkRouteError answers a guest invite link route's refusal.
func pageGuestLinkRouteError(event *core.RequestEvent, err error, failed string) error {
	// A page the link's list or its body names, held a while by an operation, says so (page_held_refusal.go).
	if pageID, busy := routeRefusedPageHeld(event, err); busy != "" {
		return answerPageHeld(event, pageID, busy)
	}
	var missing pageGuestLinkMissing
	switch {
	case errors.As(err, &missing):
		apiErrorAbout(event, &apiErrorTarget{Kind: missing.kind, ID: missing.id})
		return apiError(event, http.StatusNotFound, pageGuestLinkMissingCode(missing.kind), pageGuestLinkMissingSaid(missing))
	case errors.Is(err, errPublicMutationInvalid):
		return apiError(event, http.StatusBadRequest, errorCodePageGuestLinkRequestInvalid,
			refusalMessage("Check the guest invite link.", refusalReason(err)))
	case errors.Is(err, errPublicMutationInTrash):
		return apiError(event, http.StatusConflict, errorCodePageInTrash, "A page in the trash gets no guest invite link until it's restored.")
	case isDLPPolicyDecisionError(err), errors.Is(err, errOrganizationSharingSecurityBlocked), errors.Is(err, errGuestInviteRequiresApproval),
		errors.Is(err, errPersonPageRestrictedInvite), errors.Is(err, errPersonPageShareLinked):
		return publicMutationRouteError(event, err, "guest-link")
	}
	return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageGuestLinkFailed, failed, err)
}

func pageGuestLinkMissingCode(kind string) string {
	switch kind {
	case pageGuestLinkPageKind:
		return errorCodePageNotFound
	case pageGuestLinkWorkspaceKind:
		return errorCodeWorkspaceNotFound
	}
	return errorCodePageGuestLinkNotFound
}

func pageGuestLinkMissingSaid(missing pageGuestLinkMissing) string {
	switch missing.kind {
	case pageGuestLinkPageKind:
		return fmt.Sprintf("Page %s isn't one you can give a guest invite link to: it isn't there, or you haven't full access to it.", missing.id)
	case pageGuestLinkWorkspaceKind:
		return fmt.Sprintf("Workspace %s isn't one of yours, or isn't there.", missing.id)
	}
	return fmt.Sprintf("No guest invite link you may turn off has the ID %s: only its maker or an owner of its workspace turns one off "+
		"(GET /pages/{pageId}/page-guest-links lists a page's).", missing.id)
}
