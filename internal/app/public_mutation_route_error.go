package app

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// How the sharing routes say a change or a read of a page's sharing wasn't
// made. A page an operation holds for a while — its blocks being moved, a
// wiki's change, its copy, its workspace moving — refuses changes to its
// sharing to everyone for that while, which is said as the page busy (423
// LEAFEN_PAGE_BUSY), as its content's routes say it, to a member of its
// workspace (page_held_refusal.go): its owner sharing it while it turned into
// a wiki heard "Public sharing settings not found." (2026-11-19). A page
// being deleted for good stays not found.

func publicMutationRouteError(event *core.RequestEvent, err error, stage string) error {
	if pageID, busy := routeRefusedPageHeld(event, err); busy != "" {
		return answerPageHeld(event, pageID, busy)
	}
	switch {
	case errors.Is(err, errPublicMutationInvalid):
		// The fields it is about, each with why, so the share window says
		// why under the field (public_mutation_refusals.go).
		if fields := refusalFields(err); len(fields) > 0 {
			apiErrorAbout(event, nil, fields...)
		}
		return apiError(event, http.StatusBadRequest, errorCodePublicMutationInvalid, publicMutationRefusalMessage(err))
	case errors.Is(err, errPublicMutationNotFound), isPermissionDenied(err), errors.Is(err, sql.ErrNoRows):
		return apiError(event, http.StatusNotFound, errorCodePublicMutationNotFound, "Public sharing settings not found.")
	case errors.Is(err, errPublicMutationRevisionConflict):
		return apiError(event, http.StatusConflict, errorCodePublicMutationRevisionConflict, "The public sharing settings changed. Check them again.")
	case errors.Is(err, errPublicMutationRequestReused):
		return apiError(event, http.StatusConflict, errorCodePublicMutationRequestReused, "This request was already used for a different change.")
	case errors.Is(err, errPublicMutationInTrash):
		return apiError(event, http.StatusConflict, errorCodePageInTrash, "A page in Trash shares as it is until it's restored.")
	case errors.Is(err, errPublicMutationSiteAddressTaken):
		return apiError(event, http.StatusConflict, errorCodePublicMutationSiteAddressTaken, "Another site already uses this address.")
	case isDLPPolicyDecisionError(err):
		return writeDLPPolicyError(event, err, "public-mutation-"+stage)
	case errors.Is(err, errOrganizationSharingSecurityBlocked):
		return writeSharingSecurityBlockedError(event)
	case errors.Is(err, errGuestInviteRequiresApproval):
		return apiError(event, http.StatusForbidden, errorCodeGuestInviteRequiresApproval, "Only the workspace's owners invite guests. Ask them to add this guest.")
	case errors.Is(err, errPersonPageRestrictedInvite), errors.Is(err, errPersonPageShareLinked):
		return personPageRefusal(event, err)
	case isSQLiteBusyError(err):
		return apiErrorWithCause(event, http.StatusServiceUnavailable, errorCodePublicMutationDatabaseBusy,
			"The public sharing store is busy. Try again.", "public_mutation", stage, errorCodePublicMutationDatabaseBusy, err)
	default:
		return apiErrorWithCause(event, http.StatusServiceUnavailable, errorCodePublicMutationStorageUnavailable,
			"The public sharing store is unavailable.", "public_mutation", stage,
			errorCodePublicMutationStorageUnavailable, err)
	}
}
