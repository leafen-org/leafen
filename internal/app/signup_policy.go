package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

var (
	errSignupRestricted        = errors.New(errorCodeSignupRestricted)
	errSignupPolicyUnavailable = errors.New(errorCodeSignupPolicyUnavailable)
)

// authorizeSelfServiceAccountCreation says whether someone may make their
// own account: anyone where sign-up is open, and where it is closed only
// someone an invitation admits (signup_invitations.go).
func authorizeSelfServiceAccountCreation(app core.App, admission signupAdmission) (storedAdministratorSettings, error) {
	settings, err := loadAdministratorSettings(app)
	if err != nil || settings.FormatVersion != 3 {
		return storedAdministratorSettings{}, failedBecause(errSignupPolicyUnavailable, err)
	}
	switch settings.SignupPolicy {
	case administratorSignupPolicyPublic:
		return settings, nil
	case administratorSignupPolicyClosed:
		if admission.Invited {
			return settings, nil
		}
		return storedAdministratorSettings{}, errSignupRestricted
	default:
		return storedAdministratorSettings{}, errSignupPolicyUnavailable
	}
}

func signupPolicyAPIError(event *core.RequestEvent, err error) error {
	failure := signupPolicyFailure(event, err)
	return apiErrorWithCause(
		event, failure.Status, failure.Code, failure.Message, diagnosticOperation(event),
		failure.Stage, failure.CauseCode, err,
	)
}

// signupPolicyFailure is how a route that made or let in an account refuses
// for the sign-up policy. An invitation that can't be used also says why
// beside it (invitation_ended.go), on whichever of those routes met it.
func signupPolicyFailure(event *core.RequestEvent, err error) apiFailureSpec {
	if cause := invitationEndedCause(err); invitationEndedSaid[cause] != "" {
		apiErrorBecause(event, "", cause)
		return apiFailureSpec{
			Status: http.StatusForbidden, Code: errorCodeSignupInvitationInvalid, Message: invitationEndedSaid[cause],
			Stage: "signup_policy", CauseCode: errorCodeSignupInvitationInvalid,
		}
	}
	if errors.Is(err, errSignupInvitationInvalid) {
		return apiFailureSpec{
			Status: http.StatusForbidden, Code: errorCodeSignupInvitationInvalid,
			Message: "This invitation can't make an account: it is for another address, already used, withdrawn or older than 7 days.",
			Stage:   "signup_policy", CauseCode: errorCodeSignupInvitationInvalid,
		}
	}
	if errors.Is(err, errSignupRestricted) {
		return apiFailureSpec{
			Status: http.StatusForbidden, Code: errorCodeSignupRestricted,
			Message: "Sign-up on this server is closed by its administrator's policy. Ask an administrator, or whoever told you about this server, for an invitation.",
			Stage:   "signup_policy", CauseCode: errorCodeSignupRestricted,
		}
	}
	// The policy unread because the database stayed busy is the server's
	// failure, which the error writer tells busy (api_error_busy.go).
	status := http.StatusServiceUnavailable
	if isSQLiteBusyError(err) {
		status = http.StatusInternalServerError
	}
	return apiFailureSpec{
		Status: status, Code: errorCodeSignupPolicyUnavailable,
		Message: "Couldn't check the sign-up policy.",
		Stage:   "signup_policy", CauseCode: errorCodeSignupPolicyUnavailable,
	}
}
