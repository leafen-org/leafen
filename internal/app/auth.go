package app

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

var (
	errRegistrationAccountExists = errors.New(errorCodeAccountExists)
	errLoginCredentials          = errors.New(errorCodeInvalidCredentials)
	errLoginVerificationRequired = errors.New(errorCodeEmailVerificationRequired)
)

func registerAuthRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/auth/register", registerAccount)
	event.Router.POST(apiRoot+"/auth/signup-invitation", readSignupInvitation)
	registerInvitationJoinRoutes(event)
	event.Router.POST(apiRoot+"/auth/login", loginAccount)
	event.Router.POST(apiRoot+"/auth/verification/request", requestEmailVerification)
	event.Router.POST(apiRoot+"/auth/verification/confirm", confirmEmailVerification)
	event.Router.POST(apiRoot+"/auth/password-reset/request", requestPasswordReset)
	event.Router.POST(apiRoot+"/auth/password-reset/confirm", confirmPasswordReset)
	event.Router.POST(apiRoot+"/auth/password-reset/check", checkPasswordReset)
	registerAccountPasswordRoutes(event)
	registerAccountEmailRoutes(event)
	registerPasskeyRoutes(event)
	registerAuthProviderRoutes(event)
	registerAuthMFARoutes(event)
	registerAdministratorSignInCodeRoute(event)
}

func registerAccount(event *core.RequestEvent) error {
	var input registrationInput
	if err := event.BindBody(&input); err != nil {
		return recordRejectedAuthAttempt(
			event, "", "", authAuditMethodPasswordSignup, authAuditPhaseVerify,
			http.StatusBadRequest, errorCodeInvalidInput, "Check the account details.",
		)
	}

	email, emailOK := normalizeEmailIdentity(input.Email)
	input.Email = email
	input.Name = strings.TrimSpace(input.Name)
	input.Invitation = strings.TrimSpace(input.Invitation)
	if !emailOK || input.Name == "" || len(input.Name) > maxAuthNameLength ||
		!authPasswordAcceptable(input.Password) {
		return recordRejectedAuthAttempt(
			event, "", input.Email, authAuditMethodPasswordSignup, authAuditPhaseVerify,
			http.StatusBadRequest, errorCodeInvalidInput, "Check the account details.",
		)
	}

	// Registration is one finite same-key transaction. The policy read and user
	// write share that transaction so a concurrent close cannot report success
	// before an account admitted under the preceding policy is durable.
	var record *core.Record
	var response sessionResponse
	existingOwnerID := ""
	requiresVerification := false
	// The password is checked against the address's unproven account, or
	// hashed for a new one, before the transaction (password_outside_transaction.go).
	var existingPassword passwordCheck
	password := preparedPassword{plain: input.Password}
	if existing, err := findAccountByEmail(event.App, input.Email); err == nil {
		if !matchedAccountEmailVerified(existing, input.Email) {
			existingPassword = checkPassword(existing, input.Password)
		}
	} else {
		password = preparePassword(event.App, "users", input.Password)
	}
	// A guest invite link shares its pages as it is taken, under their sharing locks.
	release := lockPageGuestLinkPages(event.App, input.Invitation)
	defer release()
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if existing, err := findAccountByEmail(txApp, input.Email); err == nil {
			existingOwnerID = existing.Id
			if matchedAccountEmailVerified(existing, input.Email) || !existingPassword.holds(existing, input.Password) {
				return errRegistrationAccountExists
			}
			record = existing
			requiresVerification = true
			return recordAuthAttemptInTransactionSuccess(
				txApp, event, existing.Id, input.Email,
				authAuditMethodPasswordSignup, authAuditPhaseVerify,
			)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		admission, err := resolveSignupAdmission(txApp, input.Invitation, input.Email, time.Now())
		if err != nil {
			return err
		}
		if !admission.Invited {
			if admission, err = signupAddressAdmission(txApp, input.Email); err != nil {
				return err
			}
		}
		settings, err := authorizeSelfServiceAccountCreation(txApp, admission)
		if err != nil {
			return err
		}
		users, err := txApp.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		record = core.NewRecord(users)
		record.SetEmail(input.Email)
		password.set(record)
		record.Set("name", input.Name)
		// An email invitation's token was sent to, or handed on for, this
		// address, which proves it; nothing else is asked.
		requiresVerification = (settings.RequireEmailVerification || admission.NeedsProof) && !admission.ProvesEmail
		record.SetVerified(!requiresVerification)
		if err := txApp.Save(record); err != nil {
			return err
		}
		if err := recordAccountJoin(txApp, record.Id, admission.Join, time.Now()); err != nil {
			return err
		}
		// Without confirmation nobody proved the address: it signs in, but
		// isn't trusted where an address decides access
		// (account_email_proof.go).
		if !requiresVerification && !admission.ProvesEmail {
			if err := markAccountEmailUnproven(txApp, record.Id, record.Email()); err != nil {
				return err
			}
		}
		if err := admitVerifiedEmailInvitations(txApp, record); err != nil {
			return err
		}
		// The invitation that let it in is taken here, as by its name step (auth_invitation_join.go).
		if err := takeJoinInvitation(event.Request.Context(), txApp, record, input.Invitation, time.Now()); err != nil {
			return err
		}
		if !requiresVerification {
			response, _, _, err = beginMFAChallengeInTransaction(
				txApp, record, browserSessionMetadataFromEvent(event), false,
				authSessionReplayIdentity{},
			)
			if err != nil {
				return err
			}
		}
		return recordAuthAttemptInTransactionSuccess(
			txApp, event, record.Id, input.Email,
			authAuditMethodPasswordSignup, authAuditPhaseVerify,
		)
	})
	if errors.Is(err, errRegistrationAccountExists) {
		return recordRejectedAuthAttempt(
			event, existingOwnerID, input.Email, authAuditMethodPasswordSignup, authAuditPhaseVerify,
			http.StatusConflict, errorCodeAccountExists, "This email is already in use.",
		)
	}
	// A guest invite link whose page is held says so (page_guest_link_held.go).
	failure, refused := guestLinkHeldFailure(event, err)
	if !refused && (errors.Is(err, errSignupRestricted) || errors.Is(err, errSignupPolicyUnavailable) || errors.Is(err, errSignupInvitationInvalid)) {
		failure, refused = signupPolicyFailure(event, err), true
	}
	if refused {
		return recordRejectedAuthAttemptWithCause(
			event, "", input.Email, authAuditMethodPasswordSignup, authAuditPhaseVerify,
			failure.Status, failure.Code, failure.Message, diagnosticOperation(event),
			failure.Stage, failure.CauseCode, err,
		)
	}
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", input.Email, authAuditMethodPasswordSignup, authAuditPhaseVerify,
			http.StatusConflict, errorCodeRegistrationFailed, "Couldn't create the account.",
			"auth.register", "account-transaction", errorCodeRegistrationFailed, err,
		)
	}
	if input.Invitation != "" {
		recheckCollaborationAccessAfterCommit(event.App)
	}
	if requiresVerification {
		if err := createAndDeliverAuthMessage(event, record, authMessageVerification); err != nil {
			return authEmailDeliveryAPIError(event, "Couldn't send the verification email.", err)
		}
		return event.JSON(http.StatusAccepted, registrationVerificationResponse{VerificationRequired: true})
	}
	return event.JSON(http.StatusCreated, response)
}

func loginAccount(event *core.RequestEvent) error {
	var input loginInput
	if err := event.BindBody(&input); err != nil {
		return recordRejectedAuthAttempt(
			event, "", "", authAuditMethodPasswordSignin, authAuditPhaseVerify,
			http.StatusBadRequest, errorCodeInvalidInput, "Check the login details.",
		)
	}

	email, emailOK := normalizeEmailIdentity(input.Email)
	input.Email = email
	if !emailOK {
		return recordRejectedAuthAttempt(
			event, "", "", authAuditMethodPasswordSignin, authAuditPhaseVerify,
			http.StatusUnauthorized, errorCodeInvalidCredentials, "The email or password is incorrect.",
		)
	}
	replay, replayOK := passwordLoginReplayIdentity(input)
	if !replayOK {
		return recordRejectedAuthAttempt(
			event, "", input.Email, authAuditMethodPasswordSignin, authAuditPhaseVerify,
			http.StatusBadRequest, errorCodeInvalidInput, "Check the login details.",
		)
	}
	metadata := browserSessionMetadataFromEvent(event)
	response := sessionResponse{}
	challenge := authMFAChallengeResponse{}
	ownerID := ""
	mfaRequired := false
	// Checked before the transaction (password_outside_transaction.go). An
	// account that guessing slowed waits first, answered without a hash
	// (password_backoff.go).
	var password passwordCheck
	if account, err := findAccountByEmail(event.App, input.Email); err == nil {
		wait, err := passwordSignInWait(event.App, account.Id, time.Now())
		if err != nil {
			return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeServerBusy, "Leafen couldn't check the login; try again.", err)
		}
		if wait > 0 {
			event.Response.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			return recordRejectedAuthAttempt(
				event, account.Id, input.Email, authAuditMethodPasswordSignin, authAuditPhaseVerify,
				http.StatusTooManyRequests, errorCodePasswordWait, "Too many wrong passwords for this account; try again later.",
			)
		}
		password = checkPassword(account, input.Password)
	}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		record, err := findAccountByEmail(txApp, input.Email)
		if err != nil {
			return refusedUnlessStorage(errLoginCredentials, err)
		}
		ownerID = record.Id
		if !password.holds(record, input.Password) {
			return errLoginCredentials
		}
		// An app's account never signs in.
		if _, integration, err := integrationAccountOf(txApp, record.Id); err != nil || integration {
			return refusedUnlessStorage(errLoginCredentials, err)
		}
		settings, err := loadAdministratorSettings(txApp)
		if err != nil {
			return err
		}
		if settings.RequireEmailVerification && !matchedAccountEmailVerified(record, input.Email) {
			return errLoginVerificationRequired
		}
		response, challenge, mfaRequired, err = beginMFAChallengeInTransaction(
			txApp, record, metadata, input.ReplaceOldestSession, replay,
		)
		if err != nil {
			return err
		}
		// The right password starts the count over.
		if err := clearPasswordBackoff(txApp, record.Id); err != nil {
			return err
		}
		return recordAuthAttemptInTransactionSuccess(
			txApp, event, record.Id, input.Email,
			authAuditMethodPasswordSignin, signInStepPhase(mfaRequired),
		)
	})
	if errors.Is(err, errAuthSessionReplayRace) {
		recovered, found, replayErr := recoverBrowserSessionReplay(event.App, ownerID, replay)
		if replayErr != nil {
			err = replayErr
		} else if found {
			response = recovered
			err = nil
		}
	}
	if errors.Is(err, errLoginCredentials) {
		if ownerID != "" {
			if err := notePasswordFailure(event.App, ownerID, time.Now()); err != nil {
				event.App.Logger().Warn("leafen: password sign-in backoff unrecorded", "error", err)
			}
		}
		return recordRejectedAuthAttempt(
			event, ownerID, input.Email, authAuditMethodPasswordSignin, authAuditPhaseVerify,
			http.StatusUnauthorized, errorCodeInvalidCredentials, "The email or password is incorrect.",
		)
	}
	if errors.Is(err, errLoginVerificationRequired) {
		return recordRejectedAuthAttempt(
			event, ownerID, input.Email, authAuditMethodPasswordSignin, authAuditPhaseVerify,
			http.StatusForbidden, errorCodeEmailVerificationRequired, "Email verification required.",
		)
	}
	if errors.Is(err, errSAMLRequired) {
		return recordRejectedAuthAttempt(
			event, ownerID, input.Email, authAuditMethodPasswordSignin, authAuditPhaseVerify,
			http.StatusForbidden, errorCodeSAMLRequired, "This organization requires SAML login.",
		)
	}
	if err != nil {
		failure := browserSessionFailure(err)
		return recordRejectedAuthAttemptWithCause(
			event, ownerID, input.Email, authAuditMethodPasswordSignin, authAuditPhaseVerify,
			failure.Status, failure.Code, failure.Message, diagnosticOperation(event),
			failure.Stage, failure.CauseCode, err,
		)
	}
	if mfaRequired {
		return event.JSON(http.StatusAccepted, challenge)
	}
	return event.JSON(http.StatusOK, response)
}
