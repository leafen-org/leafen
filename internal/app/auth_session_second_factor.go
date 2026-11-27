package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A session's second factor: the 2-step code or passkey it signed in with,
// or one it showed since — turning 2-step verification on, or confirming
// the session with a code. A policy asking for 2-step verification trusts a
// session only with one.

func registerAuthSessionSecondFactorRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/account/sessions/current/second-factor", confirmCurrentSessionSecondFactor)
	event.Router.POST(apiRoot+"/account/sessions/without-second-factor/revoke", revokeSessionsWithoutSecondFactor)
}

// browserSessionSecondFactor is the second factor a live session of this
// owner went through, or "" for none or a session that isn't live.
func browserSessionSecondFactor(app core.App, sessionID string, ownerID string) (string, error) {
	if sessionID == "" || ownerID == "" {
		return "", nil
	}
	var factor string
	err := app.DB().NewQuery(`
		SELECT ` + authSessionSecondFactorSQL + ` FROM {{leafen_auth_sessions}}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0 AND [[expires_at]] > {:now}
	`).Bind(dbx.Params{"id": sessionID, "ownerId": ownerID, "now": time.Now().UTC().UnixMilli()}).Row(&factor)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return factor, err
}

// markBrowserSessionSecondFactor records a second factor a live session showed.
func markBrowserSessionSecondFactor(app core.App, sessionID string, ownerID string, factor string) error {
	if sessionID == "" || ownerID == "" {
		return nil
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_auth_sessions}} SET [[second_factor]] = {:factor}, [[updated_at]] = {:now}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0
	`).Bind(dbx.Params{"factor": factor, "now": time.Now().UTC().UnixMilli(), "id": sessionID, "ownerId": ownerID}).Execute()
	return err
}

// accountSessionsCarrySecondFactor is true of an account whose every live
// sign-in session went through a second factor: then whichever session asks
// has, without the request's session reaching the access check (which takes
// only the person). With no session at all that says nothing, so the account
// also has 2-step verification on, or a session its sign-in provider vouched for.
func accountSessionsCarrySecondFactor(app core.App, ownerID string) (bool, error) {
	var bare, vouched int
	err := app.DB().NewQuery(`
		SELECT
			COALESCE(SUM(CASE WHEN `+authSessionWithoutSecondFactorSQL+` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN [[provider_second_factor]] = 1 THEN 1 ELSE 0 END), 0)
		FROM {{leafen_auth_sessions}}
		WHERE [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0 AND [[expires_at]] > {:now}
	`).Bind(dbx.Params{"ownerId": ownerID, "now": time.Now().UTC().UnixMilli()}).Row(&bare, &vouched)
	if err != nil || bare > 0 {
		return false, err
	}
	if vouched > 0 {
		return true, nil
	}
	_, enabled, err := verifiedTOTPMFAFactor(app, ownerID)
	return err == nil && enabled, err
}

type authSessionSecondFactorInput struct {
	Code string `json:"code"`
}

// confirmCurrentSessionSecondFactor confirms the asking session with a
// current 2-step code.
func confirmCurrentSessionSecondFactor(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	sessionID := currentBrowserSessionID(event)
	if !ok || sessionID == "" {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := authSessionSecondFactorInput{}
	if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.Code) == "" {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the authentication code.")
	}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if err := consumeTOTPCode(txApp, user.Id, strings.TrimSpace(input.Code)); err != nil {
			return err
		}
		return markBrowserSessionSecondFactor(txApp, sessionID, user.Id, "totp")
	})
	if errors.Is(err, errMFACodeInvalid) {
		return apiError(event, http.StatusBadRequest, errorCodeMFACodeInvalid, "The authentication code is incorrect.")
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeMFAConfirmationFailed,
			"Couldn't confirm this login.", diagnosticOperation(event), "session-second-factor", "SESSION_SECOND_FACTOR_FAILED", err)
	}
	recheckCollaborationAccessAfterCommit(event.App)
	return event.NoContent(http.StatusNoContent)
}

// revokeSessionsWithoutSecondFactor signs out the account's other sessions
// that went through no second factor.
func revokeSessionsWithoutSecondFactor(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	now := time.Now().UTC().UnixMilli()
	_, err := event.App.DB().NewQuery(`
		UPDATE {{leafen_auth_sessions}} SET [[revoked_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0 AND ` + authSessionWithoutSecondFactorSQL + ` AND [[id]] != {:current}
	`).Bind(dbx.Params{"now": now, "ownerId": user.Id, "current": currentBrowserSessionID(event)}).Execute()
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAuthSessionRevokeFailed,
			"Couldn't log out those sessions.", diagnosticOperation(event), "session-revoke-bare", "SESSION_REVOKE_FAILED", err)
	}
	recheckCollaborationAccessAfterCommit(event.App)
	return event.NoContent(http.StatusNoContent)
}
