package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A road refused on a page an operation holds for a while (page_busy.go)
// says it held — 423 LEAFEN_PAGE_BUSY, which operation and when the page
// takes changes again — to a member of its workspace (pageHeldForMember),
// whether its address names the page or not: a road whose page comes in its
// body (a guest invite link's pages) or from what it decides (a request for
// a page's access, a public link a server administrator turns off) learns it
// from the refusal itself, which names the page it was made on (2026-11-23:
// a guest invite link made on a page whose blocks were being moved answered
// that the page wasn't one the person could give a link to, approving or
// declining a request for it said "Request not found.", and the
// administrator's turning off its link "Public sharing settings not found.",
// where the page's own sharing routes said it busy).
// The answer names the page, the operation as its cause, for the screen to
// say in its reader's words which page waits for what (lib/settingsErrors.ts,
// TopBarShareGuestLinks.tsx).

// pageNamingRefusal is a refusal that names the page it was made on.
type pageNamingRefusal interface {
	refusedPage() string
}

// refusedOnPage is a refusal made on a page a road's address doesn't name,
// to the person a road acts as when that isn't the one asking (a server
// administrator turning off a page's public link as its workspace's owner).
type refusedOnPage struct {
	pageID, actingAs string
	cause            error
}

func (refusal refusedOnPage) Error() string       { return refusal.cause.Error() }
func (refusal refusedOnPage) Unwrap() error       { return refusal.cause }
func (refusal refusedOnPage) refusedPage() string { return refusal.pageID }

// refusalOnPage is err as made on the page pageID; nil stays nil.
func refusalOnPage(err error, pageID string) error {
	return refusalOnPageAs(err, pageID, "")
}

// refusalOnPageAs is err as made on the page pageID to the person actingAs.
func refusalOnPageAs(err error, pageID, actingAs string) error {
	if err == nil {
		return nil
	}
	return refusedOnPage{pageID: pageID, actingAs: actingAs, cause: err}
}

// routeRefusedPageHeld is the page a route was refused on — the one the
// refusal names, else the one the route's address names — and the operation
// holding it, when the refusal is one a held page makes (not found, not the
// person's), told only to a member of its workspace (the person asking, or
// the one the road acts as); busy is "" otherwise.
func routeRefusedPageHeld(event *core.RequestEvent, err error) (pageID, busy string) {
	if event.Auth == nil || !isPermissionDenied(err) && !errors.Is(err, errPublicMutationNotFound) {
		return "", ""
	}
	pageID, principalID := event.Request.PathValue("pageId"), event.Auth.Id
	var named pageNamingRefusal
	if errors.As(err, &named) && named.refusedPage() != "" {
		pageID = named.refusedPage()
	}
	var acting refusedOnPage
	if errors.As(err, &acting) && acting.actingAs != "" {
		principalID = acting.actingAs
	}
	if pageID == "" {
		return "", ""
	}
	return pageID, pageHeldForMember(event.App, principalID, pageID)
}

// answerPageHeld answers a road refused on the page pageID, which the
// operation busy holds.
func answerPageHeld(event *core.RequestEvent, pageID, busy string) error {
	apiErrorAbout(event, &apiErrorTarget{Kind: "page", ID: pageID})
	apiErrorBecause(event, "", busy)
	return apiError(event, http.StatusLocked, errorCodePageBusy, pageBusyMessage(busy))
}
