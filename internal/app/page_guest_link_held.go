package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A guest invite link taken while an operation holds one of its pages for a
// while — its blocks being moved, a wiki's change, its copy — that its maker
// otherwise shares (page_access_hold.go) isn't taken: the take's transaction
// ends with nothing made — no account, no guest, no share, the link not
// spent — and says which page waits and for what, 423 LEAFEN_PAGE_BUSY with
// the page and the operation, for its screen to say to open the link again
// once that's done (lib/signupInvitation.ts; 2026-11-23: a one-person link
// taken then left that page out and was spent, and one on that page alone
// said it had no page left to share). Only someone holding the link's code
// hears it, of a page the link shares (invitation_ended.go).

// guestLinkPageHeld is a guest invite link's take refused while an
// operation holds the page pageID.
type guestLinkPageHeld struct {
	pageID, busy string
}

func (held guestLinkPageHeld) Error() string { return errorCodePageBusy }

// guestLinkHeldFailure is how a road that takes invitations answers one
// refused because one of its pages is held; false for any other failure.
func guestLinkHeldFailure(event *core.RequestEvent, err error) (apiFailureSpec, bool) {
	var held guestLinkPageHeld
	if !errors.As(err, &held) {
		return apiFailureSpec{}, false
	}
	apiErrorAbout(event, &apiErrorTarget{Kind: "page", ID: held.pageID})
	apiErrorBecause(event, "", held.busy)
	return apiFailureSpec{
		Status: http.StatusLocked, Code: errorCodePageBusy,
		Message: "A page this guest invite link shares is busy with another change for a while, so the link " +
			"wasn't used: open it again once that's done.",
		Stage: "guest_link_take", CauseCode: errorCodePageBusy,
	}, true
}
