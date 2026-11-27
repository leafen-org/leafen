package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// An organization may require the guests of some of its workspaces to use
// 2-step verification, as Notion's MFA enforcement does for Enterprise
// guests. The organization module decides that (GuestMFA); without it no
// guest is asked. A guest refused for it is refused as anyone without access
// is, so every path answers it as before; opening a page answers it apart so
// the guest sees why.

const (
	errorCodeGuestMFARequired = "LEAFEN_GUEST_MFA_REQUIRED"
	permissionReasonGuestMFA  = "guest-mfa"
)

func guestMFARequired() error {
	return &permissionError{code: errorCodePermissionDenied, reason: permissionReasonGuestMFA}
}

func isGuestMFARequired(err error) bool {
	var denied *permissionError
	return errors.As(err, &denied) && denied.reason == permissionReasonGuestMFA
}

// guestMFAAPIError answers a guest refused for 2-step verification apart,
// on the page itself and on what the page reads beside it (its comments, the
// inbox), so the page shows its verification screen without other errors.
// A guest hears it only where they would otherwise have access.
func guestMFAAPIError(event *core.RequestEvent) error {
	return apiError(event, http.StatusForbidden, errorCodeGuestMFARequired,
		"This organization asks its guests to turn on 2-step verification.")
}

// authorizeGuestWorkspaceMFA refuses a guest a workspace whose organization
// requires 2-step verification of its guests when they haven't turned it on.
func authorizeGuestWorkspaceMFA(app core.App, principalID string, workspace *core.Record) error {
	if check := organizationPolicy.GuestMFA; check != nil {
		return check(app, principalID, workspace)
	}
	return nil
}

// accountHasTwoStepVerification is true of an account with 2-step
// verification turned on.
func accountHasTwoStepVerification(app core.App, userID string) (bool, error) {
	_, enabled, err := verifiedTOTPMFAFactor(app, userID)
	return enabled, err
}
