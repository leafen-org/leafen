package app

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	accountPasswordStateTable         = "leafen_account_password_state"
	accountPasswordReasonCreated      = "admin_created"
	accountPasswordReasonReset        = "admin_reset"
	accountPasswordStateReadOperation = "account_password_state.read"
)

type storedAccountPasswordState struct {
	MustChangePassword int `db:"must_change_password"`
}

func registerAccountPasswordStateRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/account/state", readAccountPasswordState)
}

func readAccountPasswordState(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	// Whether the account administers the server comes with its state, so
	// that only an administrator reads the server's settings.
	administrator, err := isAdministrator(event.App, user.Id)
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAccountStateReadFailed,
			"Couldn't load the account security state.", accountPasswordStateReadOperation,
			"storage-read", errorCodeAccountStateReadFailed, err,
		)
	}
	stored := storedAccountPasswordState{}
	err = event.App.DB().NewQuery(`
		SELECT [[must_change_password]] FROM {{leafen_account_password_state}}
		WHERE [[owner_id]] = {:ownerId}
	`).Bind(dbx.Params{"ownerId": user.Id}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		known, err := accountPasswordKnown(event.App, user.Id)
		if err != nil {
			return apiErrorWithCause(
				event, http.StatusInternalServerError, errorCodeAccountStateReadFailed,
				"Couldn't load the account security state.", accountPasswordStateReadOperation,
				"storage-read", errorCodeAccountStateReadFailed, err,
			)
		}
		return event.JSON(http.StatusOK, accountPasswordStateResponse{Administrator: administrator, PasswordSet: known})
	}
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAccountStateReadFailed,
			"Couldn't load the account security state.", accountPasswordStateReadOperation,
			"storage-read", errorCodeAccountStateReadFailed, err,
		)
	}
	mustChange := stored.MustChangePassword == 1
	if mustChange {
		// The new password replaces a temporary one typed to sign in; a
		// device that joined with a code, a passkey or a provider has none
		// to replace (the multi-server decision of 2026-10-20: passwords are
		// a backup sign-in). The next sign-in with the password asks.
		method, err := browserSessionSignInMethod(event.App, currentBrowserSessionID(event), user.Id)
		if err != nil {
			return apiErrorWithCause(
				event, http.StatusInternalServerError, errorCodeAccountStateReadFailed,
				"Couldn't load the account security state.", accountPasswordStateReadOperation,
				"storage-read", errorCodeAccountStateReadFailed, err,
			)
		}
		mustChange = method == "" || method == samlAuthenticationMethodPassword
	}
	known, err := accountPasswordKnown(event.App, user.Id)
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAccountStateReadFailed,
			"Couldn't load the account security state.", accountPasswordStateReadOperation,
			"storage-read", errorCodeAccountStateReadFailed, err,
		)
	}
	return event.JSON(http.StatusOK, accountPasswordStateResponse{
		MustChangePassword: mustChange, Administrator: administrator, PasswordSet: known,
	})
}

// browserSessionSignInMethod is how a live browser session signed in, or ""
// for none (a key's request).
func browserSessionSignInMethod(app core.App, sessionID string, ownerID string) (string, error) {
	if sessionID == "" {
		return "", nil
	}
	var method string
	err := app.DB().NewQuery(`SELECT [[sign_in_method]] FROM {{leafen_auth_sessions}}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0`).
		Bind(dbx.Params{"id": sessionID, "ownerId": ownerID}).Row(&method)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return method, err
}

func setMustChangePasswordInTransaction(
	app core.App,
	ownerID string,
	issuedBy string,
	reason string,
) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_account_password_state}} (
			[[owner_id]], [[must_change_password]], [[reason]], [[issued_by]], [[updated_at]]
		) VALUES ({:ownerId}, 1, {:reason}, {:issuedBy}, {:updatedAt})
		ON CONFLICT ([[owner_id]]) DO UPDATE SET
			[[must_change_password]] = 1,
			[[reason]] = excluded.[[reason]],
			[[issued_by]] = excluded.[[issued_by]],
			[[updated_at]] = excluded.[[updated_at]]
	`).Bind(dbx.Params{
		"ownerId": ownerID, "reason": reason, "issuedBy": issuedBy,
		"updatedAt": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

func clearMustChangePasswordInTransaction(app core.App, ownerID string) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_account_password_state}}
		SET [[must_change_password]] = 0, [[updated_at]] = {:updatedAt}
		WHERE [[owner_id]] = {:ownerId}
	`).Bind(dbx.Params{
		"ownerId": ownerID, "updatedAt": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}
