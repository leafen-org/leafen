package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	authMFAReadOperation       = "auth_mfa.read"
	authMFAEnrollOperation     = "auth_mfa.enroll"
	authMFAConfirmOperation    = "auth_mfa.confirm"
	authMFACompleteOperation   = "auth_mfa.complete"
	authMFADisableOperation    = "auth_mfa.disable"
	authMFARegenerateOperation = "auth_mfa.recovery.regenerate"
)

func registerAuthMFARoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/account/mfa", readMFAState)
	event.Router.POST(apiRoot+"/account/mfa/totp/enroll", enrollTOTPFactor)
	event.Router.POST(apiRoot+"/account/mfa/totp/confirm", confirmTOTPFactor)
	event.Router.DELETE(apiRoot+"/account/mfa/totp", disableTOTPFactorRoute)
	event.Router.POST(apiRoot+"/account/mfa/recovery-codes", regenerateRecoveryCodesRoute)
	event.Router.POST(apiRoot+"/auth/mfa/totp", completeTOTPChallenge)
	event.Router.POST(apiRoot+"/auth/mfa/recovery", completeRecoveryChallenge)
}

func authenticatedMFAUser(event *core.RequestEvent) (*core.Record, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	return user, ok
}

func readMFAState(event *core.RequestEvent) error {
	user, ok := authenticatedMFAUser(event)
	if !ok {
		return nil
	}
	state, err := readAuthMFAState(event.App, user.Id)
	if err != nil {
		return authMFAAPIError(event, err, authMFAReadOperation, errorCodeMFAReadFailed, "Couldn't load the security settings.")
	}
	return event.JSON(http.StatusOK, state)
}

func enrollTOTPFactor(event *core.RequestEvent) error {
	user, ok := authenticatedMFAUser(event)
	if !ok {
		return nil
	}
	enrollment, err := startTOTPEnrollment(event.App, user)
	if err != nil {
		return authMFAAPIError(event, err, authMFAEnrollOperation, errorCodeMFAEnrollmentFailed, "Couldn't start the authenticator app setup.")
	}
	return event.JSON(http.StatusCreated, enrollment)
}

func confirmTOTPFactor(event *core.RequestEvent) error {
	user, ok := authenticatedMFAUser(event)
	if !ok {
		return nil
	}
	var input authMFAConfirmInput
	if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.FactorID) == "" || strings.TrimSpace(input.Code) == "" {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the authentication code.")
	}
	state, err := confirmTOTPEnrollment(event.App, user.Id, strings.TrimSpace(input.FactorID), input.Code)
	if err != nil {
		return authMFAAPIError(event, err, authMFAConfirmOperation, errorCodeMFAConfirmationFailed, "Couldn't verify the authenticator app setup.")
	}
	// The session that turned 2-step verification on just showed its code.
	if err := markBrowserSessionSecondFactor(event.App, currentBrowserSessionID(event), user.Id, "totp"); err != nil {
		return authMFAAPIError(event, err, authMFAConfirmOperation, errorCodeMFAConfirmationFailed, "Couldn't verify the authenticator app setup.")
	}
	noticeAccountSecurityChange(event, user.Id, securityChangeTwoStepOn, "")
	return event.JSON(http.StatusOK, state)
}

func completeTOTPChallenge(event *core.RequestEvent) error {
	var input authMFAChallengeInput
	if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.Ticket) == "" || strings.TrimSpace(input.Code) == "" {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the multi-factor authentication details.")
	}
	result, err := completeMFAChallenge(
		event.App, input.Ticket, input.Code, false, browserSessionMetadataFromEvent(event),
	)
	if err != nil {
		return authMFACompletionAPIError(event, err, authAuditMethodMFATOTP, result)
	}
	return event.JSON(http.StatusOK, result.Session)
}

func completeRecoveryChallenge(event *core.RequestEvent) error {
	var input authMFARecoveryInput
	if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.Ticket) == "" || strings.TrimSpace(input.RecoveryCode) == "" {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the recovery credentials.")
	}
	result, err := completeMFAChallenge(
		event.App, input.Ticket, input.RecoveryCode, true, browserSessionMetadataFromEvent(event),
	)
	if err != nil {
		return authMFACompletionAPIError(event, err, authAuditMethodMFARecovery, result)
	}
	noticeAccountSecurityChange(event, result.Session.User.ID, securityChangeRecoverySignIn, "")
	return event.JSON(http.StatusOK, result.Session)
}

func disableTOTPFactorRoute(event *core.RequestEvent) error {
	user, ok := authenticatedMFAUser(event)
	if !ok {
		return nil
	}
	var input authMFARevalidationInput
	if err := event.BindBody(&input); err != nil || (input.Password == "" && input.Code == "") {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Send password, the current password, or code, an authentication code.")
	}
	if asked, err := askBeforeTwoStepChange(event, user, authMFADisableOperation, errorCodeMFADisableFailed,
		"Couldn't turn off the authenticator app."); asked {
		return err
	}
	if err := disableTOTPFactor(event.App, user, input); err != nil {
		return authMFAAPIError(event, err, authMFADisableOperation, errorCodeMFADisableFailed, "Couldn't turn off the authenticator app.")
	}
	noticeAccountSecurityChange(event, user.Id, securityChangeTwoStepOff, "")
	return event.NoContent(http.StatusNoContent)
}

func regenerateRecoveryCodesRoute(event *core.RequestEvent) error {
	user, ok := authenticatedMFAUser(event)
	if !ok {
		return nil
	}
	var input authMFARevalidationInput
	if err := event.BindBody(&input); err != nil || (input.Password == "" && input.Code == "") {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Send password, the current password, or code, an authentication code.")
	}
	if asked, err := askBeforeTwoStepChange(event, user, authMFARegenerateOperation, errorCodeMFARecoveryRegenerationFailed,
		"Couldn't regenerate the recovery codes."); asked {
		return err
	}
	response, err := regenerateRecoveryCodes(event.App, user, input)
	if err != nil {
		return authMFAAPIError(event, err, authMFARegenerateOperation, errorCodeMFARecoveryRegenerationFailed, "Couldn't regenerate the recovery codes.")
	}
	noticeAccountSecurityChange(event, user.Id, securityChangeRecoveryCodes, "")
	return event.JSON(http.StatusOK, response)
}

// askBeforeTwoStepChange asks a key's call to turn 2-step verification off
// or make its recovery codes again (user_key_acting_ask.go) once what needs
// no secret reads: a password or code is sent, and 2-step verification is
// on. The password or code itself is checked once, after: checked twice, a
// code's one use would be spent before the change and a wrong password
// counted twice. So a wrong one is refused after its person was asked —
// there is no way to have them check a secret first.
func askBeforeTwoStepChange(event *core.RequestEvent, user *core.Record, operation, code, message string) (bool, error) {
	if !userKeyAsksWhereItActs(event) {
		return false, nil
	}
	if _, enabled, err := verifiedTOTPMFAFactor(event.App, user.Id); err != nil || !enabled {
		if err == nil {
			err = errMFAFactorNotFound
		}
		return true, authMFAAPIError(event, err, operation, code, message)
	}
	return askBeforeActing(event)
}

func authMFACompletionAPIError(
	event *core.RequestEvent,
	err error,
	method string,
	result authMFACompletionResult,
) error {
	status := http.StatusInternalServerError
	code := errorCodeSessionFailed
	message := "Couldn't complete multi-factor authentication."
	stage := "mfa-authority"
	causeCode := errorCodeSessionFailed
	switch {
	case errors.Is(err, errAuthSessionCapacity), errors.Is(err, errAdministratorAccountDisabled):
		failure := browserSessionFailure(err)
		status = failure.Status
		code = failure.Code
		message = failure.Message
		stage = failure.Stage
		causeCode = failure.CauseCode
	case errors.Is(err, errMFACodeInvalid):
		status = http.StatusUnauthorized
		code = errorCodeMFACodeInvalid
		message = "The authentication code is invalid or was already used."
		causeCode = errorCodeMFACodeInvalid
	case errors.Is(err, errMFARecoveryCodeInvalid):
		status = http.StatusUnauthorized
		code = errorCodeMFARecoveryCodeInvalid
		message = "The recovery code is invalid or was already used."
		causeCode = errorCodeMFARecoveryCodeInvalid
	case errors.Is(err, errMFAChallengeInvalid):
		status = http.StatusConflict
		code = errorCodeMFAChallengeInvalid
		message = "The multi-factor authentication request expired or was already completed."
		causeCode = errorCodeMFAChallengeInvalid
	case errors.Is(err, errSAMLRequired):
		status = http.StatusForbidden
		code = errorCodeSAMLRequired
		message = "This organization requires SAML login."
		stage = "enterprise-sso"
		causeCode = errorCodeSAMLRequired
	}
	if result.AuditRecorded {
		return writeAPIErrorWithReference(
			event, status, code, message, result.AuditReference,
			authMFACompleteOperation, stage, causeCode, err, diagnosticSourcePosition(1),
		)
	}
	return recordRejectedAuthAttemptWithReference(
		event, result.OwnerID, "", method, authAuditPhaseVerify,
		status, code, message, authMFACompleteOperation, stage, causeCode, err,
		result.AuditReference,
	)
}

func authMFAAPIError(event *core.RequestEvent, err error, operation string, fallbackCode string, fallbackMessage string) error {
	if storageError, ok := authStorageAPIError(event, err, operation, "mfa-authority"); ok {
		return storageError
	}
	switch {
	case errors.Is(err, errMFAAlreadyEnabled):
		return apiError(event, http.StatusConflict, errorCodeMFAAlreadyEnabled, "An authenticator app is already set up.")
	case errors.Is(err, errMFAFactorNotFound):
		return apiError(event, http.StatusNotFound, errorCodeMFAFactorNotFound, "Authentication settings not found.")
	case errors.Is(err, errMFACodeInvalid):
		return apiError(event, http.StatusUnauthorized, errorCodeMFACodeInvalid, "The authentication code is invalid or was already used.")
	case errors.Is(err, errMFAPasswordInvalid):
		return apiError(event, http.StatusUnauthorized, errorCodeInvalidCredentials, "The current password is incorrect.")
	case errors.Is(err, errMFARecoveryCodeInvalid):
		return apiError(event, http.StatusUnauthorized, errorCodeMFARecoveryCodeInvalid, "The recovery code is invalid or was already used.")
	case errors.Is(err, errMFAChallengeInvalid):
		return apiError(event, http.StatusConflict, errorCodeMFAChallengeInvalid, "The multi-factor authentication request expired or was already completed.")
	default:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, fallbackCode, fallbackMessage,
			operation, "mfa-authority", fallbackCode, err,
		)
	}
}
