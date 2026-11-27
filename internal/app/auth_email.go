package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func requestEmailVerification(event *core.RequestEvent) error {
	input, ok := bindAuthEmail(event)
	if !ok {
		return nil
	}
	user, err := findAccountByEmail(event.App, input.Email)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && matchedAccountEmailVerified(user, input.Email)) {
		return event.JSON(http.StatusAccepted, authAcceptedResponse{Accepted: true})
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeEmailVerificationFailed, "Couldn't request email verification.", err)
	}
	if err := createAndDeliverAuthMessage(event, user, authMessageVerification); err != nil {
		return authEmailDeliveryAPIError(event, "Couldn't send the verification email.", err)
	}
	return event.JSON(http.StatusAccepted, authAcceptedResponse{Accepted: true})
}

func confirmEmailVerification(event *core.RequestEvent) error {
	input := authTokenInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAuthTokenInvalid, "The verification link is invalid.")
	}
	if strings.TrimSpace(input.Token) == "" {
		return refuseMissingInput(event, errorCodeAuthTokenInvalid, "token", "the token at the end of the verification link.")
	}
	response := sessionResponse{}
	var err error
	_, err = consumeAuthMessage(event.App, strings.TrimSpace(input.Token), authMessageVerification, func(txApp core.App, user *core.Record) error {
		user.SetVerified(true)
		if err := txApp.Save(user); err != nil {
			return err
		}
		// Confirming the address proves it (account_email_proof.go).
		if err := markAccountEmailProven(txApp, user.Email()); err != nil {
			return err
		}
		if err := admitVerifiedEmailInvitations(txApp, user); err != nil {
			return err
		}
		response, err = issueAuthenticatedBrowserSessionInTransaction(
			txApp, user, browserSessionMetadataFromEvent(event), false,
			samlAuthenticationMethodPassword, "", "",
		)
		return err
	})
	if errors.Is(err, errAuthMessageConsumed) {
		return apiError(event, http.StatusConflict, errorCodeAuthTokenConsumed, "This verification link was already used.")
	}
	if errors.Is(err, errAuthMessageInvalid) {
		return apiError(event, http.StatusConflict, errorCodeAuthTokenInvalid, "The verification link expired or is invalid.")
	}
	if errors.Is(err, errSAMLRequired) || errors.Is(err, errAuthSessionCapacity) || errors.Is(err, errAdministratorAccountDisabled) {
		return browserSessionAPIError(event, err)
	}
	if err != nil {
		if storageError, ok := authStorageAPIError(
			event, err, "auth.email_verification", "storage-write",
		); ok {
			return storageError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeEmailVerificationFailed, "Couldn't verify the email.", err)
	}
	return event.JSON(http.StatusOK, response)
}

func requestPasswordReset(event *core.RequestEvent) error {
	input, ok := bindAuthEmail(event)
	if !ok {
		return nil
	}
	user, err := findAccountByEmail(event.App, input.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return event.JSON(http.StatusAccepted, authAcceptedResponse{Accepted: true})
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePasswordResetFailed, "Couldn't request a password reset.", err)
	}
	// An app's account has no mailbox and never signs in.
	if _, integration, err := integrationAccountOf(event.App, user.Id); err != nil || integration {
		return event.JSON(http.StatusAccepted, authAcceptedResponse{Accepted: true})
	}
	if err := createAndDeliverAuthMessageToRecipient(event, user, authMessagePasswordReset, input.Email); err != nil {
		return authEmailDeliveryAPIError(event, "Couldn't send the reset email.", err)
	}
	return event.JSON(http.StatusAccepted, authAcceptedResponse{Accepted: true})
}

func confirmPasswordReset(event *core.RequestEvent) error {
	input := passwordResetConfirmInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the new password.")
	}
	if strings.TrimSpace(input.Token) == "" {
		return refuseMissingInput(event, errorCodeInvalidInput, "token", "the token at the end of the reset link.")
	}
	if input.Password == "" {
		return refuseMissingInput(event, errorCodeInvalidInput, "password", "the new password, and passwordConfirm the same.")
	}
	if !authPasswordAcceptable(input.Password) || input.Password != input.PasswordConfirm {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the new password.")
	}
	// Hashed before the transaction (password_outside_transaction.go).
	password := preparePassword(event.App, "users", input.Password)
	resetUser, err := consumeAuthMessage(event.App, strings.TrimSpace(input.Token), authMessagePasswordReset, func(txApp core.App, user *core.Record) error {
		password.set(user)
		if err := txApp.Save(user); err != nil {
			return err
		}
		if err := markAccountPasswordKnown(txApp, user.Id); err != nil {
			return err
		}
		// The new password renews the account's token key, which ends every
		// sign-in; their sessions end too, so the devices list shows none of
		// them (2026-10-25: a reset left them listed, with "로그아웃").
		return revokeAllBrowserSessions(txApp, user.Id)
	})
	if errors.Is(err, errAuthMessageConsumed) {
		return apiError(event, http.StatusConflict, errorCodeAuthTokenConsumed, "This reset link was already used.")
	}
	if errors.Is(err, errAuthMessageInvalid) {
		return apiError(event, http.StatusConflict, errorCodeAuthTokenInvalid, "The reset link expired or is invalid.")
	}
	if err != nil {
		if storageError, ok := authStorageAPIError(
			event, err, "auth.password_reset", "storage-write",
		); ok {
			return storageError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePasswordResetFailed, "Couldn't reset the password.", err)
	}
	// Which account it was: a tab signed in to it knows its sign-in just
	// ended and offers to sign in again (2026-10-26: it offered the workspace).
	// The link's holder already has the address it was sent to.
	result := passwordResetConfirmResponse{}
	if resetUser != nil {
		noticeAccountSecurityChange(event, resetUser.Id, securityChangePasswordReset, "")
		result = passwordResetConfirmResponse{UserID: resetUser.Id, Email: resetUser.Email()}
	}
	return event.JSON(http.StatusOK, result)
}

// checkPasswordReset says whether a reset link can still be used, without
// using it, so the reset page says so as it opens (2026-10-16: a used link
// was refused only after both password fields were typed).
func checkPasswordReset(event *core.RequestEvent) error {
	input := struct {
		Token string `json:"token"`
	}{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the reset link.")
	}
	if strings.TrimSpace(input.Token) == "" {
		return refuseMissingInput(event, errorCodeInvalidInput, "token", "the token at the end of the reset link.")
	}
	err := checkAuthMessage(event.App, strings.TrimSpace(input.Token), authMessagePasswordReset)
	if errors.Is(err, errAuthMessageConsumed) {
		return apiError(event, http.StatusConflict, errorCodeAuthTokenConsumed, "This reset link was already used.")
	}
	if errors.Is(err, errAuthMessageInvalid) {
		return apiError(event, http.StatusConflict, errorCodeAuthTokenInvalid, "The reset link expired or is invalid.")
	}
	if err != nil {
		if storageError, ok := authStorageAPIError(event, err, "auth.password_reset", "storage-read"); ok {
			return storageError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePasswordResetFailed, "Couldn't check the reset link.", err)
	}
	return event.NoContent(http.StatusNoContent)
}

func bindAuthEmail(event *core.RequestEvent) (authEmailInput, bool) {
	input := authEmailInput{}
	if err := event.BindBody(&input); err != nil {
		_ = apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the email.")
		return input, false
	}
	email, emailOK := normalizeEmailIdentity(input.Email)
	input.Email = email
	if !emailOK {
		_ = apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the email.")
		return input, false
	}
	return input, true
}

func createAndDeliverAuthMessage(event *core.RequestEvent, user *core.Record, kind string) error {
	return createAndDeliverAuthMessageToRecipient(event, user, kind, normalizePageShareEmail(user.Email()))
}

func createAndDeliverAuthMessageToRecipient(event *core.RequestEvent, user *core.Record, kind, recipient string) error {
	settings, err := loadAdministratorSettings(event.App)
	if err != nil || !settings.SMTPEnabled {
		return errorCode(errorCodeEmailDeliveryFailed)
	}
	ttl := time.Duration(authVerificationTokenTTLSeconds) * time.Second
	if kind == authMessagePasswordReset {
		ttl = time.Duration(authPasswordResetTokenTTLSeconds) * time.Second
	}
	claim, err := claimAuthMessageDeliveryForRecipient(event.App, user.Id, kind, ttl, recipient)
	if err != nil {
		return err
	}
	if claim.Recovered {
		logAuthMessageStateRecovered(event)
	}
	if claim.ReconciledUncertain {
		logAuthMessageSendingUncertain(event)
	}
	if !claim.Dispatch {
		return nil
	}
	outcome := smtpDeliverySettledFailure
	language, languageErr := serverTextLanguage(event.App, user.Id, event.Request)
	if languageErr != nil {
		language = requestTextLanguageIn(event.App, event.Request)
	}
	origin := mailServerOrigin(event.App, event.Request)
	if origin == "" {
		// With no address to link to, the mail waits for an administrator to
		// open the server. The answer stays the one a request about no account
		// gets, so a made-up Host can't tell which accounts exist.
		logDiagnostic(event.App, http.StatusServiceUnavailable, errorCodeEmailDeliveryFailed, newDiagnosticReference(),
			diagnosticOperation(event), "auth-message-address", errorCodeEmailDeliveryFailed, errServerAddressUnknown, diagnosticSourcePosition(1))
		return recordAuthMessageDelivery(event.App, claim.ID, authMessageFailed, claim.Token)
	}
	outcome, err = deliverAuthMessage(
		event.App, settings, origin, recipient, kind, language, claim.Token,
	)
	switch outcome {
	case smtpDeliveryAccepted:
		if receiptErr := recordAuthMessageDelivery(event.App, claim.ID, authMessageDelivered, claim.Token); receiptErr != nil {
			fallbackErr := recordAuthMessageDelivery(event.App, claim.ID, authMessageUncertain, claim.Token)
			logAuthMessageDeliveryUncertain(event, errors.Join(receiptErr, fallbackErr))
		}
		return nil
	case smtpDeliveryUncertain:
		receiptErr := recordAuthMessageDelivery(event.App, claim.ID, authMessageUncertain, claim.Token)
		logAuthMessageDeliveryUncertain(event, errors.Join(err, receiptErr))
		return nil
	default:
		if receiptErr := recordAuthMessageDelivery(event.App, claim.ID, authMessageFailed, claim.Token); receiptErr != nil {
			logAuthMessageDeliveryUncertain(event, errors.Join(err, receiptErr))
		}
		return err
	}
}

func logAuthMessageStateRecovered(event *core.RequestEvent) {
	logDiagnostic(
		event.App,
		http.StatusInternalServerError,
		errorCodeAuthMessageStateRecovered,
		newDiagnosticReference(),
		diagnosticOperation(event),
		"auth-message-state-recovery",
		errorCodeAuthMessageStateRecovered,
		errorCode(errorCodeAuthMessageStateRecovered),
		diagnosticSourcePosition(1),
	)
}

func logAuthMessageDeliveryUncertain(event *core.RequestEvent, cause error) {
	logDiagnostic(
		event.App,
		http.StatusInternalServerError,
		errorCodeEmailDeliveryUncertain,
		newDiagnosticReference(),
		diagnosticOperation(event),
		"auth-message-receipt",
		errorCodeEmailDeliveryUncertain,
		cause,
		diagnosticSourcePosition(1),
	)
}

func logAuthMessageSendingUncertain(event *core.RequestEvent) {
	logDiagnostic(
		event.App,
		http.StatusInternalServerError,
		errorCodeEmailDeliveryUncertain,
		newDiagnosticReference(),
		diagnosticOperation(event),
		"auth-message-stale-sending",
		errorCodeEmailDeliveryUncertain,
		errorCode(errorCodeEmailDeliveryUncertain),
		diagnosticSourcePosition(1),
	)
}

func authEmailDeliveryAPIError(event *core.RequestEvent, message string, cause error) error {
	if storageError, ok := authStorageAPIError(
		event, cause, diagnosticOperation(event), "auth-message-storage",
	); ok {
		return storageError
	}
	return apiErrorWithCause(
		event, http.StatusServiceUnavailable, errorCodeEmailDeliveryFailed, message,
		diagnosticOperation(event), "auth-message-delivery", errorCodeEmailDeliveryFailed, cause,
	)
}

type errorCode string

func (code errorCode) Error() string { return string(code) }
