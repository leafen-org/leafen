package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An app signs in through the browser (the multi-server decision's
// 2026-10-20 "같은 날 다듬음"): the packaged app opens the server's
// "register this app" page in the system's browser with where to come back
// and a PKCE challenge (auth_app_return.go). A browser signed in there — or
// after signing in as usual — registers the app with one press; the page
// sends a code, used once within ten minutes, to the app's return, and the
// app takes its own device's sign-in for it with the verifier the challenge
// was made from, so a code caught on the way is no use. A person presses
// "register": another program on the computer can't register a device by
// itself, and the page reads none of the browser's cookies or storage for
// the app.

const (
	appSignInLifetime   = 10 * time.Minute
	appSignInCodeBytes  = 32
	appSignInCodeName   = "appSignIn"
	maxAppSignInNameLen = 120
)

var errAppSignInInvalid = errors.New(errorCodeDeviceCodeInvalid)

func registerAppSignInRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/account/app-sign-in", registerAppByBrowser)
	event.Router.POST(apiRoot+"/auth/app-sign-in/exchange", exchangeAppSignIn)
}

// registerAppByBrowser registers the app that opened this page, on the
// browser's sign-in, answering where the browser sends its code.
func registerAppByBrowser(event *core.RequestEvent) error {
	user, ok, err := signedInPerson(event)
	if !ok {
		return err
	}
	if err := authorizeDeviceSignIn(event.App, user.Id); err != nil {
		return deviceCodeAPIError(event, err)
	}
	input := authAppSignInInput{}
	_ = event.BindBody(&input)
	appReturn, ok := parseAuthAppReturn(input.AppReturn, input.AppChallenge)
	if !ok || appReturn.Address == "" {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "The app's redirect URI isn't one an app can use.")
	}
	factor, err := browserSessionSecondFactor(event.App, currentBrowserSessionID(event), user.Id)
	if err != nil {
		return deviceCodeAPIError(event, err)
	}
	if factor != "totp" && factor != "passkey" {
		factor = ""
	}
	random := make([]byte, appSignInCodeBytes)
	if _, err := rand.Read(random); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeDeviceCodeFailed, "Couldn't register the app.", err)
	}
	code := base64.RawURLEncoding.EncodeToString(random)
	now := time.Now().UTC().UnixMilli()
	if err := event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_auth_app_sign_ins}} WHERE [[owner_id]] = {:ownerId} AND [[expires_at]] <= {:now}`).
			Bind(dbx.Params{"ownerId": user.Id, "now": now}).Execute(); err != nil {
			return err
		}
		_, err := txApp.DB().NewQuery(`INSERT INTO {{leafen_auth_app_sign_ins}}
			([[code_hash]], [[owner_id]], [[challenge]], [[second_factor]], [[expires_at]], [[created_at]])
			VALUES ({:hash}, {:ownerId}, {:challenge}, {:factor}, {:expires}, {:now})`).
			Bind(dbx.Params{"hash": mcpTokenHash(code), "ownerId": user.Id, "challenge": appReturn.Challenge, "factor": factor,
				"expires": now + appSignInLifetime.Milliseconds(), "now": now}).Execute()
		return err
	}); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeDeviceCodeFailed, "Couldn't register the app.", err)
	}
	destination, err := authAppReturnDestination(appReturn.Address, appSignInCodeName, code)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeDeviceCodeFailed, "Couldn't register the app.", err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusCreated, authAppSignInResponse{ReturnURL: destination})
}

// exchangeAppSignIn gives the app that holds a code and its verifier its own
// device's sign-in, once.
func exchangeAppSignIn(event *core.RequestEvent) error {
	input := authProviderExchangeInput{}
	_ = event.BindBody(&input)
	if strings.TrimSpace(input.Code) == "" {
		return refuseMissingInput(event, errorCodeDeviceCodeInvalid, "code", "the code the app's login page sent back to it.")
	}
	if strings.TrimSpace(input.Verifier) == "" {
		return refuseMissingInput(event, errorCodeDeviceCodeInvalid, "verifier", "the verifier the app made its login's challenge from.")
	}
	code := strings.TrimSpace(input.Code)
	metadata := browserSessionMetadataFromEvent(event)
	session := sessionResponse{}
	err := errAppSignInInvalid
	stored := struct {
		OwnerID      string `db:"owner_id"`
		Challenge    string `db:"challenge"`
		SecondFactor string `db:"second_factor"`
	}{}
	if code != "" && len(code) <= 128 {
		// The code is spent first, so a wrong verifier spends it too.
		err = event.App.RunInTransaction(func(txApp core.App) error {
			now := time.Now().UTC().UnixMilli()
			err := txApp.DB().NewQuery(`SELECT [[owner_id]], [[challenge]], [[second_factor]] FROM {{leafen_auth_app_sign_ins}}
				WHERE [[code_hash]] = {:hash} AND [[used_at]] = 0 AND [[expires_at]] > {:now}`).
				Bind(dbx.Params{"hash": mcpTokenHash(code), "now": now}).One(&stored)
			if errors.Is(err, sql.ErrNoRows) {
				return errAppSignInInvalid
			}
			if err != nil {
				return err
			}
			_, err = txApp.DB().NewQuery(`UPDATE {{leafen_auth_app_sign_ins}} SET [[used_at]] = {:now}
				WHERE [[code_hash]] = {:hash} AND [[used_at]] = 0`).Bind(dbx.Params{"hash": mcpTokenHash(code), "now": now}).Execute()
			return err
		})
	}
	if err == nil && !authAppVerifierMatches(stored.Challenge, input.Verifier) {
		err = errAppSignInInvalid
	}
	if err == nil {
		err = event.App.RunInTransaction(func(txApp core.App) error {
			user, err := txApp.FindRecordById("users", stored.OwnerID)
			if err != nil {
				return refusedUnlessStorage(errAppSignInInvalid, err)
			}
			if err := authorizeDeviceSignIn(txApp, user.Id); err != nil {
				return err
			}
			session, err = issueAuthenticatedBrowserSessionInTransaction(
				txApp, user, metadata, false, authSessionMethodDevice, "", stored.SecondFactor,
			)
			if err != nil {
				return err
			}
			return recordAuthAttemptInTransactionSuccessWithMetadata(
				txApp, user.Id, user.Email(), authAuditMethodDeviceCode, authAuditPhaseVerify, metadata,
			)
		})
	}
	if err != nil {
		if errors.Is(err, errAppSignInInvalid) {
			return apiError(event, http.StatusBadRequest, errorCodeDeviceCodeInvalid, "That code isn't one this server gave, or it was used or ran out.")
		}
		return deviceCodeAPIError(event, err)
	}
	recheckCollaborationAccessAfterCommit(event.App)
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusCreated, session)
}
