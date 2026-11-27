package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

var (
	errAccountPasswordCredentials = errors.New(errorCodeInvalidCredentials)
	errAccountPasswordKnown       = errors.New("the account's password is known")
)

// A password is a backup sign-in (the multi-server decision of 2026-10-20):
// one no one knows is set without a current password and ends no sign-in;
// a known one is changed with it, which ends the other sign-ins; and it can
// be forgotten again, leaving the account to its devices. The person's agent
// sets, changes or forgets it too, as Leafen's own rules for a key say
// (user_key_routes.go) — the password best put in by `leafen mcp secret`,
// outside its conversation; changed by an agent, the other sign-ins end as
// they do, and no browser sign-in is made for the agent's key.
func registerAccountPasswordRoutes(event *core.ServeEvent) {
	event.Router.PUT(apiRoot+"/account/password", changeAccountPassword)
	event.Router.POST(apiRoot+"/account/password", setAccountPassword)
	event.Router.DELETE(apiRoot+"/account/password", forgetAccountPassword)
}

// setAccountPassword gives a password to an account whose person knows none,
// as a backup sign-in; the devices stay signed in.
func setAccountPassword(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := accountPasswordChangeInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the password.")
	}
	newPassword, made, ok := accountNewPassword(input)
	if !ok {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the password.")
	}
	password := preparePassword(event.App, "users", newPassword)
	err := event.App.RunInTransaction(func(txApp core.App) error {
		known, err := accountPasswordKnown(txApp, user.Id)
		if err != nil {
			return err
		}
		if known {
			return errAccountPasswordKnown
		}
		current, err := txApp.FindRecordById("users", user.Id)
		if err != nil {
			return err
		}
		if err := setPasswordKeepingSignIns(txApp, current, password); err != nil {
			return err
		}
		return markAccountPasswordKnown(txApp, current.Id)
	})
	if errors.Is(err, errAccountPasswordKnown) {
		return apiError(event, http.StatusConflict, errorCodePasswordChangeFailed, "This account has a password already: change it by PUT /account/password with currentPassword.")
	}
	if err != nil {
		return apiError(event, http.StatusConflict, errorCodePasswordChangeFailed, "Couldn't set the password.")
	}
	noticeAccountSecurityChange(event, user.Id, securityChangePasswordSet, "")
	if made != nil {
		return event.JSON(http.StatusOK, accountPasswordMade{Password: *made})
	}
	return event.NoContent(http.StatusNoContent)
}

// accountPasswordMade answers the password Leafen made for its person, this
// once, beside the new sign-in of the browser that changed it.
type accountPasswordMade struct {
	*sessionResponse
	Password string `json:"password"`
}

// accountNewPassword is the new password a request gives, typed twice, or —
// both left out — the one Leafen makes for its person, answered once (user
// 2026-11-23: their agent sets one without seeing it, agent_secret_notes.go).
func accountNewPassword(input accountPasswordChangeInput) (string, *string, bool) {
	if input.NewPassword == "" && input.NewPasswordConfirm == "" {
		password, err := newMadePassword("")
		return password, &password, err == nil
	}
	if !authPasswordAcceptable(input.NewPassword) || input.NewPassword != input.NewPasswordConfirm {
		return "", nil, false
	}
	return input.NewPassword, nil, true
}

// forgetAccountPassword makes the account's password one no one knows: it
// signs in by its devices. Sign-ins already made stay until they end.
func forgetAccountPassword(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	unknown := preparePassword(event.App, "users", security.RandomString(48))
	err := event.App.RunInTransaction(func(txApp core.App) error {
		current, err := txApp.FindRecordById("users", user.Id)
		if err != nil {
			return err
		}
		if err := setPasswordKeepingSignIns(txApp, current, unknown); err != nil {
			return err
		}
		return markAccountPasswordUnknown(txApp, current.Id)
	})
	if err != nil {
		return apiError(event, http.StatusConflict, errorCodePasswordChangeFailed, "Couldn't remove the password.")
	}
	noticeAccountSecurityChange(event, user.Id, securityChangePasswordGone, "")
	return event.NoContent(http.StatusNoContent)
}

func changeAccountPassword(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}

	input := accountPasswordChangeInput{}
	if err := event.BindBody(&input); err != nil || input.CurrentPassword == "" {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the password.")
	}
	chosen, made, ok := accountNewPassword(input)
	if !ok {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the password.")
	}

	response := sessionResponse{}
	_, keyed := userKeyOf(event)
	// The current password is checked, and the new one hashed, before the
	// transaction (password_outside_transaction.go).
	var currentPassword passwordCheck
	newPassword := preparedPassword{plain: chosen}
	if current, err := event.App.FindRecordById("users", user.Id); err == nil {
		if currentPassword = checkPassword(current, input.CurrentPassword); currentPassword.valid {
			newPassword = preparePassword(event.App, "users", chosen)
		}
	}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		current, err := txApp.FindRecordById("users", user.Id)
		if err != nil {
			return err
		}
		if !currentPassword.holds(current, input.CurrentPassword) {
			return errAccountPasswordCredentials
		}
		newPassword.set(current)
		if err := txApp.Save(current); err != nil {
			return err
		}
		if err := markAccountPasswordKnown(txApp, current.Id); err != nil {
			return err
		}
		// The device that changed the password keeps its session's second
		// factor, and stays registered if it was (a password sign-in otherwise).
		secondFactor, err := browserSessionSecondFactor(txApp, currentBrowserSessionID(event), current.Id)
		if err != nil {
			return err
		}
		method, err := browserSessionSignInMethod(txApp, currentBrowserSessionID(event), current.Id)
		if err != nil {
			return err
		}
		if method != authSessionMethodDevice {
			method = samlAuthenticationMethodPassword
		}
		if err := revokeAllBrowserSessions(txApp, current.Id); err != nil {
			return err
		}
		if err := clearMustChangePasswordInTransaction(txApp, current.Id); err != nil {
			return err
		}
		if keyed {
			return nil
		}
		response, err = issueAuthenticatedBrowserSessionInTransaction(
			txApp, current, browserSessionMetadataFromEvent(event), false,
			method, "", secondFactor,
		)
		return err
	})
	if errors.Is(err, errAccountPasswordCredentials) {
		return apiError(event, http.StatusUnauthorized, errorCodeInvalidCredentials, "The current password is incorrect.")
	}
	if errors.Is(err, errSAMLRequired) {
		return browserSessionAPIError(event, err)
	}
	if err == nil && keyed {
		noticeAccountSecurityChange(event, user.Id, securityChangePassword, "")
		if made != nil {
			return event.JSON(http.StatusOK, accountPasswordMade{Password: *made})
		}
		return event.NoContent(http.StatusNoContent)
	}
	if err != nil || response.Token == "" {
		if storageError, ok := authStorageAPIError(
			event, err, "auth.password_change", "storage-write",
		); ok {
			return storageError
		}
		return apiError(event, http.StatusConflict, errorCodePasswordChangeFailed, "Couldn't change the password.")
	}
	noticeAccountSecurityChange(event, user.Id, securityChangePassword, "")
	if made != nil {
		return event.JSON(http.StatusOK, accountPasswordMade{sessionResponse: &response, Password: *made})
	}
	return event.JSON(http.StatusOK, response)
}
