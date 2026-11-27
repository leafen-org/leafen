package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A signed-in device lets another join the same account with a one-time code
// (the multi-server decision's 2026-10-20 change: devices join by a QR or a
// code from a signed-in device, as Signal and WhatsApp link theirs). The
// code shows as a QR of <server>/device/<code> and as twelve digits to type;
// it lasts ten minutes and is used once. A device that joins stays signed in
// until it is cut off, and carries the second factor of the device that
// made its code. Failed tries are limited by address and across the server,
// so no code can be guessed in its ten minutes.

const (
	// Digits only, so that a phone opens its number pad for them: a page
	// can't choose the language of a letters keyboard, and an iPhone whose
	// keyboard was Korean typed letters as Hangul (2026-11-06 decision).
	// Twelve digits are more than the eight of thirty letters and digits
	// before (10^12 > 30^8); a typed O, I or L still reads as 0 or 1.
	deviceCodeLength   = 12
	deviceCodeAlphabet = "0123456789"
	deviceCodeLifetime = 10 * time.Minute
	// deviceCodeLiveLimit is how many codes an account holds at once; a new
	// one past it ends the oldest.
	deviceCodeLiveLimit = 5
	deviceCodePagePath  = "/device/"
	// authSessionMethodDevice is a sign-in a device made by joining with a
	// code; it lasts until it is cut off.
	authSessionMethodDevice = "device"
	// authDeviceSessionLifetime is a joined device's sign-in: until cut off.
	authDeviceSessionLifetime = 100 * 365 * 24 * time.Hour

	deviceCodeFailuresByAddress = 10
	deviceCodeFailureSpan       = 15 * time.Minute
	deviceCodeFailuresOnServer  = 200
	deviceCodeServerSpan        = time.Hour
	deviceCodeLimiterStoreKey   = "leafen.deviceCodeLimiter"
)

var (
	errDeviceCodeInvalid = errors.New(errorCodeDeviceCodeInvalid)
	errDeviceCodeLimited = errors.New(errorCodeDeviceCodeLimited)
)

type deviceCodeLimiter struct {
	byAddress *requestWindowLimiter
	server    *requestWindowLimiter
}

func deviceCodeLimiterOf(app core.App) *deviceCodeLimiter {
	return app.Store().GetOrSet(deviceCodeLimiterStoreKey, func() any {
		return &deviceCodeLimiter{
			byAddress: newRequestWindowLimiter(deviceCodeFailuresByAddress, deviceCodeFailureSpan),
			server:    newRequestWindowLimiter(deviceCodeFailuresOnServer, deviceCodeServerSpan),
		}
	}).(*deviceCodeLimiter)
}

func registerAccountDeviceCodeRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/account/device-codes", createAccountDeviceCode)
	event.Router.DELETE(apiRoot+"/account/device-codes/{codeId}", cancelAccountDeviceCode)
	event.Router.POST(apiRoot+"/auth/device", joinWithDeviceCode)
}

// readTypedCode reads a code of the device code's letters as typed: any
// case, with spaces, dashes or dots (a server's code stands before a code
// after a middle dot, server_code.go), with a Korean keyboard on
// (typed_code_keys.go), the letters that look like digits as those digits.
func readTypedCode(typed string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '\t', '·', '•', '・', '.':
			return -1
		case 'O', 'o':
			return '0'
		case 'I', 'i', 'L', 'l':
			return '1'
		}
		if r >= 'a' && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}, strings.TrimSpace(typedCodeKeys(typed)))
}

// normalizeDeviceCode reads a device code as typed (readTypedCode).
func normalizeDeviceCode(typed string) (string, bool) {
	code := readTypedCode(typed)
	if len(code) != deviceCodeLength {
		return "", false
	}
	for _, r := range code {
		if !strings.ContainsRune(deviceCodeAlphabet, r) {
			return "", false
		}
	}
	return code, true
}

// deviceCodeShown is a code as a person reads it, in fours.
func deviceCodeShown(code string) string {
	return codeInFours(code)
}

// codeInFours puts a hyphen between each four characters of a code.
func codeInFours(code string) string {
	var shown strings.Builder
	for start := 0; start < len(code); start += 4 {
		if start > 0 {
			shown.WriteByte('-')
		}
		shown.WriteString(code[start:min(start+4, len(code))])
	}
	return shown.String()
}

// createAccountDeviceCode makes a code for another device to join the
// signed-in account, on this device's sign-in: a key can't.
func createAccountDeviceCode(event *core.RequestEvent) error {
	user, ok, err := signedInPerson(event)
	if !ok {
		return err
	}
	// A code its organization would refuse is not made.
	if err := authorizeDeviceSignIn(event.App, user.Id); err != nil {
		return deviceCodeAPIError(event, err)
	}
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return deviceCodeAPIError(event, err)
	}
	factor, err := browserSessionSecondFactor(event.App, currentBrowserSessionID(event), user.Id)
	if err != nil {
		return deviceCodeAPIError(event, err)
	}
	if factor != "totp" && factor != "passkey" {
		factor = ""
	}
	response, err := issueAccountDeviceCode(event.App, user.Id, user.Id, factor, origin)
	if err != nil {
		return deviceCodeAPIError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusCreated, response)
}

// issueAccountDeviceCode makes a code that joins one device to the account
// ownerID, issued by issuedBy (the account itself, an administrator, or ""
// for the server's command line) and carrying secondFactor. Its address is
// at origin, or none where the server's address isn't known.
func issueAccountDeviceCode(app core.App, ownerID, issuedBy, secondFactor, origin string) (accountDeviceCodeResponse, error) {
	code := security.RandomStringWithAlphabet(deviceCodeLength, deviceCodeAlphabet)
	now := time.Now().UTC()
	response := accountDeviceCodeResponse{
		ID: "ldc_" + security.RandomString(20), Code: deviceCodeShown(code), ExpiresAt: now.Add(deviceCodeLifetime).UnixMilli(),
	}
	if origin != "" {
		response.URL = origin + deviceCodePagePath + deviceCodeShown(code)
	}
	err := app.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_account_device_codes}}
			WHERE [[owner_id]] = {:owner} AND ([[expires_at]] <= {:now} OR [[used_at]] > 0)`).
			Bind(dbx.Params{"owner": ownerID, "now": now.UnixMilli()}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_account_device_codes}} WHERE [[id]] IN (
			SELECT [[id]] FROM {{leafen_account_device_codes}} WHERE [[owner_id]] = {:owner}
			ORDER BY [[created_at]] DESC, [[id]] DESC LIMIT -1 OFFSET {:keep})`).
			Bind(dbx.Params{"owner": ownerID, "keep": deviceCodeLiveLimit - 1}).Execute(); err != nil {
			return err
		}
		_, err := txApp.DB().Insert(accountDeviceCodesTable, dbx.Params{
			"id": response.ID, "owner_id": ownerID, "code_hash": mcpTokenHash(code), "issued_by": issuedBy,
			"second_factor": secondFactor, "expires_at": response.ExpiresAt, "created_at": now.UnixMilli(),
		}).Execute()
		return err
	})
	return response, err
}

// cancelAccountDeviceCode ends a code not yet used, as when the window
// showing it closes.
func cancelAccountDeviceCode(event *core.RequestEvent) error {
	user, ok, err := signedInPerson(event)
	if !ok {
		return err
	}
	if _, err := event.App.DB().NewQuery(`DELETE FROM {{leafen_account_device_codes}}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:owner} AND [[used_at]] = 0`).
		Bind(dbx.Params{"id": event.Request.PathValue("codeId"), "owner": user.Id}).Execute(); err != nil {
		return deviceCodeAPIError(event, err)
	}
	return event.NoContent(http.StatusNoContent)
}

// joinWithDeviceCode signs this device in to the account whose code it
// brings, once. A try that fails counts against its address and the
// server; one that joins doesn't.
func joinWithDeviceCode(event *core.RequestEvent) error {
	input := authDeviceCodeInput{}
	_ = event.BindBody(&input)
	if strings.TrimSpace(input.Code) == "" {
		return refuseMissingInput(event, errorCodeDeviceCodeInvalid, "code", "the login code a logged-in device shows.")
	}
	metadata := browserSessionMetadataFromEvent(event)
	limiter := deviceCodeLimiterOf(event.App)
	address := "address:" + requestLimitAddress(event.RealIP())
	now := time.Now()
	if admitted, _ := limiter.byAddress.admit(address, now); !admitted {
		return deviceCodeAPIError(event, errDeviceCodeLimited)
	}
	if admitted, _ := limiter.server.admit("server", now); !admitted {
		limiter.byAddress.refund(address)
		return deviceCodeAPIError(event, errDeviceCodeLimited)
	}
	code, ok := normalizeDeviceCode(input.Code)
	session := sessionResponse{}
	ownerID := ""
	err := errDeviceCodeInvalid
	if ok {
		err = event.App.RunInTransaction(func(txApp core.App) error {
			nowMillis := time.Now().UTC().UnixMilli()
			stored := struct {
				ID           string `db:"id"`
				OwnerID      string `db:"owner_id"`
				SecondFactor string `db:"second_factor"`
			}{}
			err := txApp.DB().NewQuery(`SELECT [[id]], [[owner_id]], [[second_factor]] FROM {{leafen_account_device_codes}}
				WHERE [[code_hash]] = {:hash} AND [[used_at]] = 0 AND [[expires_at]] > {:now}`).
				Bind(dbx.Params{"hash": mcpTokenHash(code), "now": nowMillis}).One(&stored)
			if errors.Is(err, sql.ErrNoRows) {
				return errDeviceCodeInvalid
			}
			if err != nil {
				return err
			}
			user, err := txApp.FindRecordById("users", stored.OwnerID)
			if err != nil {
				return refusedUnlessStorage(errDeviceCodeInvalid, err)
			}
			ownerID = user.Id
			session, err = issueAuthenticatedBrowserSessionInTransaction(
				// At its account's limit, the person may end its oldest sign-in for this one, as signing in by password does.
				txApp, user, metadata, input.ReplaceOldestSession, authSessionMethodDevice, "", stored.SecondFactor,
			)
			if err != nil {
				return err
			}
			if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_account_device_codes}}
				SET [[used_at]] = {:now}, [[session_id]] = {:session} WHERE [[id]] = {:id} AND [[used_at]] = 0`).
				Bind(dbx.Params{"id": stored.ID, "now": nowMillis, "session": session.SessionID}).Execute(); err != nil {
				return err
			}
			return recordAuthAttemptInTransactionSuccessWithMetadata(
				txApp, user.Id, user.Email(), authAuditMethodDeviceCode, authAuditPhaseVerify, metadata,
			)
		})
	}
	if err != nil {
		if _, auditErr := recordAuthAttempt(event.App, authAuditAttempt{
			OwnerID: ownerID, Method: authAuditMethodDeviceCode, Phase: authAuditPhaseVerify,
			Outcome: authAuditOutcomeFailure, ReasonCode: deviceCodeFailureCode(err), ReasonStatus: deviceCodeFailureStatus(err),
			Metadata: metadata,
		}); auditErr != nil {
			event.App.Logger().Warn("leafen device code audit failed", "error", auditErr)
		}
		return deviceCodeAPIError(event, err)
	}
	limiter.byAddress.refund(address)
	limiter.server.refund("server")
	recheckCollaborationAccessAfterCommit(event.App)
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusCreated, session)
}

func deviceCodeFailureCode(err error) string {
	switch {
	case errors.Is(err, errDeviceCodeInvalid), errors.Is(err, errAdministratorAccountDisabled):
		return errorCodeDeviceCodeInvalid
	case errors.Is(err, errDeviceCodeLimited):
		return errorCodeDeviceCodeLimited
	case errors.Is(err, errAuthSessionCapacity):
		return errorCodeAuthSessionCapacity
	case errors.Is(err, errSAMLRequired):
		return errorCodeSAMLRequired
	case errors.Is(err, errDeviceSignInRestricted):
		return errorCodeDeviceSignInRestricted
	default:
		return errorCodeDeviceCodeFailed
	}
}

// deviceCodeFailureStatus is the status deviceCodeAPIError answers a failure with.
func deviceCodeFailureStatus(err error) int {
	switch {
	case errors.Is(err, errDeviceCodeInvalid):
		return http.StatusBadRequest
	case errors.Is(err, errDeviceCodeLimited):
		return http.StatusTooManyRequests
	case errors.Is(err, errAuthSessionCapacity):
		return http.StatusConflict
	case errors.Is(err, errSAMLRequired), errors.Is(err, errDeviceSignInRestricted), errors.Is(err, errAdministratorAccountDisabled):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func deviceCodeAPIError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errDeviceCodeInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeDeviceCodeInvalid, "That code isn't one this server gave, or it was used or ran out.")
	case errors.Is(err, errDeviceCodeLimited):
		event.Response.Header().Set("Retry-After", "900")
		return apiError(event, http.StatusTooManyRequests, errorCodeDeviceCodeLimited, "Too many codes were tried. Wait a while and try again.")
	case errors.Is(err, errAuthSessionCapacity):
		return apiError(event, http.StatusConflict, errorCodeAuthSessionCapacity, "This account is logged in on as many devices as it may be. Log one out on another device first.")
	case errors.Is(err, errSAMLRequired):
		return apiError(event, http.StatusForbidden, errorCodeSAMLRequired,
			"Your organization logs its people in with its single sign-on, not with a device code.")
	case errors.Is(err, errDeviceSignInRestricted):
		return apiError(event, http.StatusForbidden, errorCodeDeviceSignInRestricted,
			"Your organization asks its people to log in with a password and 2-step verification, not with a device code.")
	case errors.Is(err, errAdministratorAccountDisabled):
		return apiError(event, http.StatusForbidden, errorCodeDeviceCodeInvalid, "This account can't join a device this way.")
	default:
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeDeviceCodeFailed,
			"Couldn't join this device.", diagnosticOperation(event), "device-code", errorCodeDeviceCodeFailed, err)
	}
}
