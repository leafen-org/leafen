package app

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func registerAccountEmailRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/account/emails", readAccountEmails)
	event.Router.POST(apiRoot+"/account/emails", requestAccountEmail)
	event.Router.POST(apiRoot+"/account/emails/confirm", confirmAccountEmail)
	event.Router.PUT(apiRoot+"/account/emails/primary", promoteAccountEmail)
	event.Router.DELETE(apiRoot+"/account/emails", removeAccountEmail)
}

func accountEmailSnapshot(app core.App, user *core.Record) (accountEmailState, error) {
	state := accountEmailState{Emails: []accountEmailAddress{}, Limit: accountSecondaryEmailLimit}
	rows := []struct {
		Email    string `db:"email"`
		Verified bool   `db:"verified"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[email]], [[verified]] FROM {{leafen_account_emails}}
		WHERE [[user_id]] = {:userId} ORDER BY [[email]] LIMIT 7`).Bind(dbx.Params{"userId": user.Id}).All(&rows); err != nil {
		return state, err
	}
	if len(rows) > accountSecondaryEmailLimit+1 {
		return state, errAccountEmailLimit
	}
	material := user.Id + "\x00" + user.Email() + "\x00" + user.TokenKey()
	for _, row := range rows {
		state.Emails = append(state.Emails, accountEmailAddress{Email: row.Email, Primary: row.Email == normalizePageShareEmail(user.Email()), Verified: row.Verified})
		material += "\x00" + row.Email
		if row.Verified {
			material += "\x01"
		}
	}
	state.Revision = hashAuthMFAValue(material)
	var disabled int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_domains}} [[domain]]
		LEFT JOIN {{leafen_organization_managed_user_policies}} [[policy]] ON [[policy]].[[organization_id]] = [[domain]].[[organization_id]]
		WHERE [[domain]].[[status]] = 'verified' AND lower([[domain]].[[domain]]) IN (` + managedAccountDomainSQL("{:userId}") + `)
		AND COALESCE([[policy]].[[secondary_emails]], 'disabled') = 'disabled'`).Bind(dbx.Params{"userId": user.Id}).Row(&disabled)
	state.CanAdd = disabled == 0 && len(rows) < accountSecondaryEmailLimit+1
	return state, err
}

func readAccountEmails(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	state := accountEmailState{}
	err := runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		current, err := txApp.FindRecordById("users", user.Id)
		if err != nil {
			return err
		}
		state, err = accountEmailSnapshot(txApp, current)
		return err
	})
	if err != nil {
		return accountEmailAPIError(event, err)
	}
	return event.JSON(http.StatusOK, state)
}

func currentAccountEmailMutation(app core.App, userID, revision string) (*core.Record, error) {
	user, err := app.FindRecordById("users", userID)
	if err != nil {
		return nil, err
	}
	state, err := accountEmailSnapshot(app, user)
	if err != nil {
		return nil, err
	}
	if revision == "" || subtle.ConstantTimeCompare([]byte(state.Revision), []byte(revision)) != 1 {
		return nil, errAccountEmailRevisionChanged
	}
	return user, nil
}

func requestAccountEmail(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := accountEmailAddInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the email.")
	}
	email, valid := normalizeEmailIdentity(input.Email)
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the email.")
	}
	settings, err := loadAdministratorSettings(event.App)
	if err != nil {
		return authEmailDeliveryAPIError(event, "Couldn't send the verification email.", err)
	}
	// A server that sends no mail adds it at once, unproven (2026-10-20).
	if !notificationEmailConfigured(settings) {
		state, err := addUnprovenAccountEmail(event.App, user.Id, email, input.BaseRevision)
		if err != nil {
			return accountEmailAPIError(event, err)
		}
		noticeAccountSecurityChange(event, user.Id, securityChangeEmailAdded, email)
		return event.JSON(http.StatusOK, state)
	}
	claim, err := claimAccountEmailChallenge(event.App, user.Id, email, input.BaseRevision)
	if err != nil {
		return accountEmailAPIError(event, err)
	}
	if claim.Dispatch {
		language, _ := serverTextLanguage(event.App, user.Id, event.Request)
		if err := deliverAccountEmailChallenge(event, settings, claim, language); err != nil {
			return authEmailDeliveryAPIError(event, "Couldn't send the verification email.", err)
		}
	}
	return event.JSON(http.StatusAccepted, accountEmailPending{Email: email, Nonce: claim.Nonce, ExpiresAt: time.UnixMilli(claim.ExpiresAt).UTC().Format(time.RFC3339Nano), CurrentEmail: claim.CurrentEmail})
}

func confirmAccountEmail(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := accountEmailConfirmInput{}
	if err := event.BindBody(&input); err != nil {
		return accountEmailAPIError(event, errAccountEmailChallenge)
	}
	state, err := consumeAccountEmailChallenge(event.App, user.Id, input)
	if err != nil {
		return accountEmailAPIError(event, err)
	}
	noticeAccountSecurityChange(event, user.Id, securityChangeEmailAdded, strings.TrimSpace(input.Email))
	return event.JSON(http.StatusOK, state)
}

func promoteAccountEmail(event *core.RequestEvent) error { return mutateAccountEmail(event, true) }
func removeAccountEmail(event *core.RequestEvent) error  { return mutateAccountEmail(event, false) }

func mutateAccountEmail(event *core.RequestEvent, primary bool) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := accountEmailMutationInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the email.")
	}
	email, valid := normalizeEmailIdentity(input.Email)
	if !valid {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the email.")
	}
	state := accountEmailState{}
	changed, formerPrimary := false, ""
	change := func(txApp core.App) error {
		changed = false
		// An address the account no longer has says so before whether the
		// settings it was sent on are the latest: taken away on another
		// device, it still showed in the list here (2026-11-14: its removal
		// there said a code was wrong, and the address stayed listed).
		var verified int
		if err := txApp.DB().NewQuery(`SELECT [[verified]] FROM {{leafen_account_emails}} WHERE [[user_id]] = {:userId} AND [[email]] = {:email}`).
			Bind(dbx.Params{"userId": user.Id, "email": email}).Row(&verified); errors.Is(err, sql.ErrNoRows) {
			return errAccountEmailMissing
		} else if err != nil {
			return err
		}
		current, err := currentAccountEmailMutation(txApp, user.Id, input.BaseRevision)
		if err != nil {
			return err
		}
		if err := requireAccountEmailChange(txApp, current, email); err != nil {
			return err
		}
		if verified != 1 {
			return errAccountEmailNotVerified
		}
		if email == normalizePageShareEmail(current.Email()) {
			if !primary {
				return errAccountEmailPrimary
			}
			state, err = accountEmailSnapshot(txApp, current)
			return err
		}
		action := "remove"
		if primary {
			action = "primary"
		}
		changed, formerPrimary = true, current.Email()
		if err := auditAccountEmailChange(txApp, current, email, action); err != nil {
			return err
		}
		if primary {
			oldEmail, oldVerified := normalizePageShareEmail(current.Email()), current.Verified()
			// Both addresses already identify this user. Updating this projection
			// directly preserves the token key and existing sessions; PB Save would
			// rotate it as if this were an administrator's identity replacement.
			if _, err := txApp.DB().NewQuery(`UPDATE {{users}} SET [[email]] = {:email}, [[verified]] = 1, [[updated]] = {:updated} WHERE [[id]] = {:userId}`).
				Bind(dbx.Params{"email": email, "updated": time.Now().UTC().Format("2006-01-02 15:04:05.000Z"), "userId": user.Id}).Execute(); err != nil {
				return err
			}
			if oldEmail != "" && oldVerified {
				if _, err := txApp.DB().Insert("leafen_account_emails", dbx.Params{"email": oldEmail, "user_id": user.Id, "verified": 1}).Execute(); err != nil {
					return err
				}
			}
		} else {
			if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_account_emails}} WHERE [[user_id]] = {:userId} AND [[email]] = {:email}`).
				Bind(dbx.Params{"userId": user.Id, "email": email}).Execute(); err != nil {
				return err
			}
		}
		current, err = txApp.FindRecordById("users", user.Id)
		if err != nil {
			return err
		}
		if err := accountEmailsChanged(txApp, current); err != nil {
			return err
		}
		state, err = accountEmailSnapshot(txApp, current)
		return err
	}
	// A key's call to take a sign-in email away asks its person only for one
	// that would go (user_key_acting_ask.go).
	err := rehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return accountEmailAPIError(event, err)
	}
	switch {
	case changed && primary:
		noticeAccountSecurityChange(event, user.Id, securityChangePrimaryEmail, email, formerPrimary)
	case changed:
		noticeAccountSecurityChange(event, user.Id, securityChangeEmailRemoved, email, email)
	}
	return event.JSON(http.StatusOK, state)
}

func accountEmailAPIError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errRegistrationAccountExists):
		return apiError(event, http.StatusConflict, errorCodeAccountExists, "This email is already in use.")
	// Each of the email settings' refusals says its own reason: settings
	// changed since they were read, and an address the account doesn't have,
	// each with its own code, as neither is a code the person typed.
	case errors.Is(err, errAccountEmailRevisionChanged):
		return apiError(event, http.StatusConflict, errorCodeAccountEmailChanged,
			"The email settings changed: baseRevision isn't their revision now. Read them again (GET /account/emails), then send the change.")
	case errors.Is(err, errAccountEmailMissing):
		return apiError(event, http.StatusConflict, errorCodeAccountEmailMissing,
			"That email isn't on your account. Read the email settings again (GET /account/emails).")
	case errors.Is(err, errAccountEmailNotVerified):
		return apiError(event, http.StatusConflict, errAccountEmailChallenge.Error(), "That isn't a verified email of your account.")
	case errors.Is(err, errAccountEmailPrimary):
		return apiError(event, http.StatusConflict, errAccountEmailPrimary.Error(), "That's your primary email. Make another one primary first.")
	case errors.Is(err, errAccountEmailLimit):
		return apiError(event, http.StatusConflict, errAccountEmailLimit.Error(), "Your account has the most emails it keeps. Remove one first.")
	case errors.Is(err, errAccountEmailLimit), errors.Is(err, errAccountEmailPrimary), errors.Is(err, errAccountEmailChallenge), errors.Is(err, sql.ErrNoRows):
		code := errAccountEmailChallenge.Error()
		if !errors.Is(err, sql.ErrNoRows) {
			code = err.Error()
		}
		return apiError(event, http.StatusConflict, code, "Refresh the email settings and try again.")
	case errors.Is(err, errAccountEmailManaged):
		return apiError(event, http.StatusForbidden, err.Error(), "Your organization manages your email addresses.")
	case errors.Is(err, errMFACodeInvalid):
		return apiError(event, http.StatusUnauthorized, errorCodeMFACodeInvalid, "Enter the current password or an authentication code.")
	default:
		if storageError, ok := authStorageAPIError(event, err, "account.emails", "email-authority"); ok {
			return storageError
		}
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAccountEmailFailed, "Couldn't update the email addresses.", "account.emails", "email-authority", errorCodeAccountEmailFailed, err)
	}
}
