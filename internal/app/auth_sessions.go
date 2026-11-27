package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	authSessionIDPrefix       = "lse_"
	authSessionIDRandomLength = 24
	authSessionClaim          = "sessionId"
	authSessionRequestKey     = "leafenAuthSessionId"
)

var (
	errAuthSessionCapacity = errors.New(errorCodeAuthSessionCapacity)
	errAuthSessionInvalid  = errors.New(errorCodeAuthSessionInvalid)
)

type browserSessionMetadata struct {
	UserAgent string
	IP        string
	// The connection whose key signed in, when one did: the session ends with it.
	CreatedByConnection string
}

type storedAuthSession struct {
	ID        string `db:"id"`
	OwnerID   string `db:"owner_id"`
	UserAgent string `db:"user_agent"`
	IP        string `db:"ip"`
	ExpiresAt int64  `db:"expires_at"`
	RevokedAt int64  `db:"revoked_at"`
	CreatedAt int64  `db:"created_at"`
	// LastUsedAt is when the sign-in was last used (auth_session_last_used.go).
	LastUsedAt int64 `db:"last_used_at"`
	// SecondFactor is the second factor the sign-in went through, if any.
	SecondFactor string `db:"second_factor"`
	// SignInMethod is how the sign-in was made ("device" for a device that
	// joined with a code).
	SignInMethod string `db:"sign_in_method"`
}

func registerBrowserSessionAuthority(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenSignInReadFailure", Priority: apis.DefaultLoadAuthTokenMiddlewarePriority + 2, Func: holdSignInThroughReadFailure,
	})
	event.Router.BindFunc(validateBrowserSession)
	registerAuthSessionRoutes(event)
}

func registerAuthSessionRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/account/sessions", listAuthSessions)
	event.Router.DELETE(apiRoot+"/account/sessions/{sessionId}", revokeAuthSession)
	registerAuthSessionSecondFactorRoutes(event)
	registerAccountDeviceCodeRoutes(event)
	registerAgentSignInRoutes(event)
	registerAddressHandoffRoutes(event)
	registerProviderFrameRoutes(event)
	registerDocumentPreviewRoutes(event)
	registerPageTextStoredRoutes(event)
	registerAppSignInRoutes(event)
}

func browserSessionMetadataFromEvent(event *core.RequestEvent) browserSessionMetadata {
	metadata := browserSessionMetadata{
		UserAgent: boundedAuthSessionMetadata(event.Request.UserAgent(), maxAuthSessionUserAgentLength),
		IP:        boundedAuthSessionMetadata(event.RemoteIP(), maxAuthSessionIPLength),
	}
	if key, keyed := userKeyOf(event); keyed {
		metadata.CreatedByConnection = key.ConnectionID
	}
	return metadata
}

func boundedAuthSessionMetadata(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	runes := []rune(value)
	return string(runes[:maximum])
}

func issueAuthenticatedBrowserSession(
	app core.App,
	user *core.Record,
	metadata browserSessionMetadata,
	replaceOldest bool,
	method string,
	providerID string,
) (sessionResponse, error) {
	response := sessionResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := txApp.FindRecordById("users", user.Id)
		if err != nil {
			return err
		}
		response, err = issueAuthenticatedBrowserSessionInTransaction(
			txApp, current, metadata, replaceOldest, method, providerID, "",
		)
		return err
	})
	return response, err
}

func issueAuthenticatedBrowserSessionInTransaction(
	app core.App,
	user *core.Record,
	metadata browserSessionMetadata,
	replaceOldest bool,
	method string,
	providerID string,
	secondFactor string,
) (sessionResponse, error) {
	return issueAuthenticatedBrowserSessionWithReplayInTransaction(
		app, user, metadata, replaceOldest, method, providerID, secondFactor, authSessionReplayIdentity{},
	)
}

func newBrowserSessionToken(user *core.Record, sessionID string, duration time.Duration) (string, error) {
	if !user.Collection().IsAuth() {
		return "", core.ErrNotAuthRecord
	}
	key := user.TokenKey() + user.Collection().AuthToken.Secret
	if key == "" {
		return "", core.ErrMissingSigningKey
	}
	return security.NewJWT(jwt.MapClaims{
		core.TokenClaimType:         core.TokenTypeAuth,
		core.TokenClaimId:           user.Id,
		core.TokenClaimCollectionId: user.Collection().Id,
		core.TokenClaimRefreshable:  true,
		authSessionClaim:            sessionID,
	}, key, duration)
}

// holdSignInThroughReadFailure keeps a failed read from ending a sign-in.
// PocketBase's token step takes any failure for no sign-in, a busy database
// included, and "Login required" makes the browser forget a token that is
// still good. A token that can't be read says the server can't tell yet, as a
// failed session read does below; one that is truly no good goes on without
// a person, and a read that works this time signs the request in.
func holdSignInThroughReadFailure(event *core.RequestEvent) error {
	token := bearerToken(event.Request)
	if event.Auth != nil || token == "" || !strings.HasPrefix(event.Request.URL.Path, apiRoot+"/") {
		return event.Next()
	}
	record, err := event.App.FindAuthRecordByToken(token, core.TokenTypeAuth)
	if err == nil {
		event.Auth = record
		return event.Next()
	}
	claims, parseErr := security.ParseUnverifiedJWT(token)
	if parseErr != nil {
		return event.Next()
	}
	id, _ := claims[core.TokenClaimId].(string)
	collectionID, _ := claims[core.TokenClaimCollectionId].(string)
	if id == "" || collectionID == "" {
		return event.Next()
	}
	if _, readErr := event.App.FindRecordById(collectionID, id); readErr == nil || errors.Is(readErr, sql.ErrNoRows) {
		return event.Next()
	} else {
		return apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeAuthSessionUnavailable,
			"Couldn't check the login state.", diagnosticOperation(event),
			"token-validation", "SESSION_AUTHORITY_READ_FAILED", readErr,
		)
	}
}

func validateBrowserSession(event *core.RequestEvent) error {
	if !strings.HasPrefix(event.Request.URL.Path, apiRoot+"/") ||
		strings.HasPrefix(event.Request.URL.Path, apiRoot+"/auth/") ||
		strings.HasPrefix(event.Request.URL.Path, apiRoot+"/setup/") ||
		event.Auth == nil || event.Auth.Collection().Name != "users" {
		return event.Next()
	}
	// A call made with a connection's key has no browser session.
	if _, keyed := userKeyOf(event); keyed {
		return event.Next()
	}
	sessionID, err := activeBrowserSessionID(event.App, bearerToken(event.Request), event.Auth.Id)
	if errors.Is(err, errAuthSessionInvalid) {
		return authSessionInvalidAPIError(event)
	}
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeAuthSessionUnavailable,
			"Couldn't check the login state.", diagnosticOperation(event),
			"session-validation", "SESSION_AUTHORITY_READ_FAILED", err,
		)
	}
	event.Set(authSessionRequestKey, sessionID)
	// A failed renewal leaves the sign-in as it was; the request goes on.
	if err := renewBrowserSession(event, sessionID); err != nil {
		event.App.Logger().Warn("leafen session renewal failed", "error", err)
	}
	if err := noteAuthSessionUse(event.App, sessionID, time.Now()); err != nil {
		event.App.Logger().Warn("leafen session use not noted", "error", err)
	}
	return event.Next()
}

// activeBrowserSessionID returns the session a browser token names while that
// session is still open: owned by ownerID, not revoked or expired, and held by
// an active account. Every surface that accepts a browser token admits it
// through this one read, so a signed-out or revoked token is refused on HTTP
// and on the collaboration socket alike. A token that names no open session
// returns errAuthSessionInvalid; a failed read returns its storage error.
func activeBrowserSessionID(app core.App, token string, ownerID string) (string, error) {
	claims, err := security.ParseUnverifiedJWT(token)
	if err != nil {
		return "", errAuthSessionInvalid
	}
	sessionID, _ := claims[authSessionClaim].(string)
	if !validAuthSessionID(sessionID) || ownerID == "" {
		return "", errAuthSessionInvalid
	}
	if err := openAuthSession(app, sessionID, ownerID); err != nil {
		return "", err
	}
	return sessionID, nil
}

// openAuthSession says whether a sign-in session is still open, the one
// read every surface admits a person through — a browser token here —
// returning errAuthSessionInvalid
// for one that isn't, or a failed read's storage error.
func openAuthSession(app core.App, sessionID string, ownerID string) error {
	if !validAuthSessionID(sessionID) || ownerID == "" {
		return errAuthSessionInvalid
	}
	makers := []string{}
	err := app.DB().NewQuery(`
		SELECT [[session]].[[created_by_connection]] FROM {{leafen_auth_sessions}} [[session]]
		JOIN {{leafen_administrator_account_states}} [[account]]
			ON [[account]].[[user_id]]=[[session]].[[owner_id]] AND [[account]].[[status]]='active'
		WHERE [[session]].[[id]] = {:id} AND [[session]].[[owner_id]] = {:ownerId}
			AND [[session]].[[revoked_at]] = 0 AND [[session]].[[expires_at]] > {:now}
			AND NOT ` + organizationSuspendedSQL("[[session]].[[owner_id]]") + `
	`).Bind(dbx.Params{
		"id": sessionID, "ownerId": ownerID, "now": time.Now().UTC().UnixMilli(),
	}).Column(&makers)
	if err != nil {
		return err
	}
	if len(makers) != 1 {
		return errAuthSessionInvalid
	}
	// A session a connection's key signed in holds only while it does.
	if holds, err := connectionLineageHolds(app, makers[0]); err != nil {
		return err
	} else if !holds {
		return errAuthSessionInvalid
	}
	// administratorAccountCanAuthenticate and an organization's suspension are
	// enforced in the same bounded authority read as session validity so every
	// authenticated request stays fail-closed without a second round-trip.
	return nil
}

func authSessionInvalidAPIError(event *core.RequestEvent) error {
	return apiError(
		event, http.StatusUnauthorized, errorCodeAuthSessionInvalid,
		"The login session expired or was revoked.",
	)
}

func browserSessionAPIError(event *core.RequestEvent, err error) error {
	failure := browserSessionFailure(err)
	if errors.Is(err, errAuthSessionCapacity) || errors.Is(err, errSAMLRequired) || errors.Is(err, errAdministratorAccountDisabled) {
		return apiError(
			event, failure.Status, failure.Code, failure.Message,
		)
	}
	return apiErrorWithCause(
		event, failure.Status, failure.Code, failure.Message, diagnosticOperation(event),
		failure.Stage, failure.CauseCode, err,
	)
}

func browserSessionFailure(err error) apiFailureSpec {
	if failure, ok := classifyAuthStorageError(err); ok {
		return failure
	}
	if errors.Is(err, errSAMLRequired) {
		return apiFailureSpec{
			Status: http.StatusForbidden, Code: errorCodeSAMLRequired,
			Message: "This organization requires SAML login.",
			Stage:   "enterprise-sso", CauseCode: errorCodeSAMLRequired,
		}
	}
	if errors.Is(err, errAdministratorAccountDisabled) {
		return apiFailureSpec{
			Status: http.StatusForbidden, Code: errorCodeAdministratorAccountDisabled,
			Message: "This account is deactivated.",
			Stage:   "account-state", CauseCode: errorCodeAdministratorAccountDisabled,
		}
	}
	if errors.Is(err, errAuthSessionCapacity) {
		return apiFailureSpec{
			Status: http.StatusConflict, Code: errorCodeAuthSessionCapacity,
			Message: "This account reached its login session limit.",
			Stage:   "session-issuance", CauseCode: errorCodeAuthSessionCapacity,
		}
	}
	if errors.Is(err, errAuthSessionRequestIDReused) {
		return apiFailureSpec{
			Status: http.StatusConflict, Code: errorCodeAuthSessionRequestIDReused,
			Message: "This login attempt ID was already used for different login conditions.",
			Stage:   "session-replay", CauseCode: errorCodeAuthSessionRequestIDReused,
		}
	}
	return apiFailureSpec{
		Status: http.StatusInternalServerError, Code: errorCodeSessionFailed,
		Message: "Couldn't create the session.",
		Stage:   "session-issuance", CauseCode: "SESSION_AUTHORITY_WRITE_FAILED",
	}
}

func validAuthSessionID(sessionID string) bool {
	return strings.HasPrefix(sessionID, authSessionIDPrefix) &&
		len(sessionID) == len(authSessionIDPrefix)+authSessionIDRandomLength
}

func currentBrowserSessionID(event *core.RequestEvent) string {
	value, _ := event.Get(authSessionRequestKey).(string)
	return value
}

func listAuthSessions(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	rows := []storedAuthSession{}
	err := event.App.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[user_agent]], [[ip]], [[expires_at]],
			[[revoked_at]], [[created_at]], ` + authSessionLastUsedSQL + ` AS [[last_used_at]], ` + authSessionSecondFactorSQL + ` AS [[second_factor]],
			[[sign_in_method]]
		FROM {{leafen_auth_sessions}}
		WHERE [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0 AND [[expires_at]] > {:now}
		ORDER BY ` + authSessionLastUsedSQL + ` DESC, [[id]] DESC
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"ownerId": user.Id, "now": time.Now().UTC().UnixMilli(), "limit": authSessionLimit + 1,
	}).All(&rows)
	if err != nil || len(rows) > authSessionLimit {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAuthSessionListFailed,
			"Couldn't load the login sessions.", diagnosticOperation(event),
			"session-list", "SESSION_AUTHORITY_READ_FAILED", err,
		)
	}
	sortAuthSessionsByUse(rows)
	currentID := currentBrowserSessionID(event)
	result := make([]authSessionResponse, 0, len(rows))
	for _, row := range rows {
		result = append(result, authSessionResponse{
			ID:           row.ID,
			CreatedAt:    time.UnixMilli(row.CreatedAt).UTC().Format(time.RFC3339Nano),
			LastUsedAt:   time.UnixMilli(row.LastUsedAt).UTC().Format(time.RFC3339Nano),
			UserAgent:    row.UserAgent,
			IP:           row.IP,
			Country:      deviceCountry(row.IP),
			Current:      row.ID == currentID,
			SecondFactor: row.SecondFactor,
			Registered:   row.SignInMethod == authSessionMethodDevice,
		})
	}
	return event.JSON(http.StatusOK, result)
}

func revokeAuthSession(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	sessionID := strings.TrimSpace(event.Request.PathValue("sessionId"))
	if !validAuthSessionID(sessionID) {
		return authSessionNotFoundAPIError(event)
	}

	row := storedAuthSession{}
	err := event.App.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[user_agent]], [[ip]], [[expires_at]],
			[[revoked_at]], [[created_at]]
		FROM {{leafen_auth_sessions}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": sessionID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && row.OwnerID != user.Id) {
		return authSessionNotFoundAPIError(event)
	}
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAuthSessionRevokeFailed,
			"Couldn't revoke the login session.", diagnosticOperation(event),
			"session-revoke", "SESSION_AUTHORITY_READ_FAILED", err,
		)
	}
	if row.RevokedAt != 0 {
		return event.NoContent(http.StatusNoContent)
	}

	now := time.Now().UTC().UnixMilli()
	result, err := event.App.DB().NewQuery(`
		UPDATE {{leafen_auth_sessions}}
		SET [[revoked_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0
	`).Bind(dbx.Params{"id": sessionID, "ownerId": user.Id, "now": now}).Execute()
	if err != nil {
		if storageError, ok := authStorageAPIError(
			event, err, diagnosticOperation(event), "session-revoke",
		); ok {
			return storageError
		}
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAuthSessionRevokeFailed,
			"Couldn't revoke the login session.", diagnosticOperation(event),
			"session-revoke", "SESSION_AUTHORITY_WRITE_FAILED", err,
		)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAuthSessionRevokeFailed,
			"Couldn't revoke the login session.", diagnosticOperation(event),
			"session-revoke", "SESSION_AUTHORITY_WRITE_CONFLICT", err,
		)
	}
	recheckCollaborationAccessAfterCommit(event.App)
	endSignInWaits(event.App, user.Id)
	return event.NoContent(http.StatusNoContent)
}

func authSessionNotFoundAPIError(event *core.RequestEvent) error {
	return apiError(
		event, http.StatusNotFound, errorCodeAuthSessionNotFound,
		"Login session not found.",
	)
}

func revokeAllBrowserSessions(app core.App, ownerID string) error {
	_, err := revokeAllBrowserSessionsCount(app, ownerID)
	return err
}

func revokeAllBrowserSessionsCount(app core.App, ownerID string) (int64, error) {
	now := time.Now().UTC().UnixMilli()
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_auth_sessions}}
		SET [[revoked_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0
	`).Bind(dbx.Params{"ownerId": ownerID, "now": now}).Execute()
	if err != nil {
		return 0, err
	}
	recheckCollaborationAccessAfterCommit(app)
	endSignInWaits(app, ownerID)
	return result.RowsAffected()
}
