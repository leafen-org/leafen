package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// answerPageMetadataRefused answers a change to a page's metadata that the
// page's state refused (status), as the page's own route does — the page
// held a while by an operation said busy to a member of its workspace
// (answerPageMetadataMissing) — for each road that changes it for someone:
// the page's route, and a server administrator putting it in the trash as
// its workspace's owner (administrator_workspace_content_actions.go). A
// refused place is said without its reason here; the page's route, which
// reads it from the change, says it first.
func answerPageMetadataRefused(event *core.RequestEvent, principalID, pageID string, status pageMetadataMutationStatus) error {
	switch status {
	case pageMetadataPageLocked:
		return answerLockRefusal(event, pageLockedError())
	case pageMetadataPageMissing:
		return answerPageMetadataMissing(event, principalID, pageID)
	case pageMetadataParentConflict:
		return apiError(event, http.StatusConflict, errorCodePageMetadataConflict, "The page location can't be changed.")
	case pageMetadataIdentityReused:
		return apiError(event, http.StatusConflict, errorCodeMutationIdentityReused, "This ID was used for a different change.")
	case pageMetadataReplacementLocked:
		return apiError(event, http.StatusConflict, errorCodeWorkspaceImportTargetNotFresh,
			"Other changes can't be applied while the page content is being replaced.")
	}
	return apiError(event, http.StatusConflict, errorCodeCursorConflict, pageCursorConflictMessage)
}

// answerPageMetadataMissing answers a page's metadata change the page's
// state refused. One refused while an operation holds the page — a wiki made
// of it or undone, its blocks being moved, its copy — is said as the page
// busy (423 LEAFEN_PAGE_BUSY) to a member of its workspace
// (pageHeldForMember), as its content's and its sharing's routes say it: the
// app keeps the change and sends it again once the operation is done
// (2026-11-21: a title typed and at once turned into a wiki reached the page
// while its job held it, heard "Page not found." and was dropped). A page
// being deleted for good, or one the person can't reach, stays not found.
func answerPageMetadataMissing(event *core.RequestEvent, principalID, pageID string) error {
	if busy := pageHeldForMember(event.App, principalID, pageID); busy != "" {
		return answerPageHeld(event, pageID, busy)
	}
	return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
}
