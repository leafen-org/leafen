package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func registerAuthProviderRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/auth/providers", listConfiguredAuthProviders)
	event.Router.GET(apiRoot+"/auth/providers/{provider}/start", returnAuthProviderFailures(beginAuthProviderSignIn))
	event.Router.GET(apiRoot+"/auth/providers/{provider}/callback", returnAuthProviderFailures(completeAuthProviderCallback))
	event.Router.POST(apiRoot+"/auth/providers/exchange", exchangeAuthProviderSession)
}

func beginAuthProviderSignIn(event *core.RequestEvent) error {
	provider := strings.TrimSpace(event.Request.PathValue("provider"))
	query := event.Request.URL.Query()
	appReturn, ok := parseAuthAppReturn(query.Get(authAppReturnParam), query.Get(authAppChallengeParam))
	if !ok {
		return recordRejectedAuthAttempt(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseRequest,
			http.StatusBadRequest, errorCodeAuthProviderStateInvalid, "Check where the app's login returns to.",
		)
	}
	config, err := loadAuthProviderConfig(event.App, provider)
	if errors.Is(err, errAuthProviderNotFound) {
		return recordRejectedAuthAttempt(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseRequest,
			http.StatusNotFound, errorCodeAuthProviderNotFound, "Login provider not found.",
		)
	}
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseRequest,
			http.StatusServiceUnavailable, errorCodeAuthProviderUnavailable,
			"The login provider is unavailable.", "auth_provider.start", "configuration",
			errorCodeAuthProviderUnavailable, err,
		)
	}
	metadata, err := discoverAuthProvider(event.Request.Context(), config)
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseRequest,
			http.StatusServiceUnavailable, errorCodeAuthProviderUnavailable,
			"Couldn't connect to the login provider.", "auth_provider.start", "discovery",
			errorCodeAuthProviderUnavailable, err,
		)
	}
	origin, err := publicServerOrigin(event.Request)
	if err != nil {
		return recordRejectedAuthAttempt(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseRequest,
			http.StatusBadRequest, errorCodeAuthProviderResponseInvalid, "Check the server's public address.",
		)
	}
	// After an address change the provider may hold only the old return
	// address, which forwards here (auth_provider_redirects.go).
	redirectURI := authProviderReturnFor(event.App, config, authProviderRedirectURI(origin, provider))
	state, nonce, verifier, err := createAuthProviderState(event.App, config, redirectURI, appReturn)
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseRequest,
			http.StatusInternalServerError, errorCodeAuthProviderUnavailable,
			"Couldn't save the login request.", "auth_provider.start", "state-write",
			errorCodeAuthProviderUnavailable, err,
		)
	}
	adapter, err := newAuthProviderPlatformAdapter(
		event.Request.Context(), config, metadata, redirectURI,
	)
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseRequest,
			http.StatusServiceUnavailable, errorCodeAuthProviderUnavailable,
			"Couldn't check the login provider settings.", "auth_provider.start", "provider-build",
			errorCodeAuthProviderUnavailable, err,
		)
	}
	defer adapter.close()
	destination, err := adapter.authorizationURL(state, nonce, verifier)
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseRequest,
			http.StatusServiceUnavailable, errorCodeAuthProviderUnavailable,
			"Couldn't check the login provider address.", "auth_provider.start", "redirect-build",
			errorCodeAuthProviderUnavailable, err,
		)
	}
	if recorded, err := recordSuccessfulAuthAttempt(
		event, "", "", authAuditMethodOAuthSignin, authAuditPhaseRequest,
	); !recorded {
		return err
	}
	return event.Redirect(http.StatusFound, destination)
}

func completeAuthProviderCallback(event *core.RequestEvent) error {
	provider := strings.TrimSpace(event.Request.PathValue("provider"))
	query := event.Request.URL.Query()
	code := strings.TrimSpace(query.Get("code"))
	stateValue := strings.TrimSpace(query.Get("state"))
	if (provider != authProviderGoogle && provider != authProviderOIDC) || code == "" ||
		len(code) > 4096 || stateValue == "" || query.Get("error") != "" {
		return recordRejectedAuthAttempt(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusBadRequest, errorCodeAuthProviderStateInvalid, "Couldn't verify the login response.",
		)
	}
	state, err := claimAuthProviderState(event.App, provider, stateValue)
	if err != nil {
		return recordRejectedAuthAttempt(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusBadRequest, errorCodeAuthProviderStateInvalid, "The login request expired or was already used.",
		)
	}
	completed := false
	defer func() {
		if !completed {
			markAuthProviderStateFailed(event.App, state.ID)
		}
	}()
	config, err := loadAuthProviderConfig(event.App, provider)
	responseIssuer := strings.TrimSpace(query.Get("iss"))
	if err != nil || config.SettingsRevision != state.SettingsRevision || config.Issuer != state.Issuer ||
		(responseIssuer != "" && responseIssuer != config.Issuer) {
		return recordRejectedAuthAttempt(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusUnauthorized, errorCodeAuthProviderClaimsInvalid, "The login provider response doesn't match.",
		)
	}
	metadata, err := discoverAuthProvider(event.Request.Context(), config)
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusUnauthorized, errorCodeAuthProviderClaimsInvalid,
			"Couldn't verify the login provider response.", "auth_provider.callback", "discovery",
			errorCodeAuthProviderClaimsInvalid, err,
		)
	}
	verifier, err := decryptAuthProviderVerifier(event.App, state.VerifierCipher)
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusUnauthorized, errorCodeAuthProviderClaimsInvalid,
			"Couldn't verify the login request.", "auth_provider.callback", "state-decrypt",
			errorCodeAuthProviderClaimsInvalid, err,
		)
	}
	adapter, err := newAuthProviderPlatformAdapter(
		event.Request.Context(), config, metadata, state.RedirectURI,
	)
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusUnauthorized, errorCodeAuthProviderClaimsInvalid,
			"Couldn't verify the login provider settings.", "auth_provider.callback", "provider-build",
			errorCodeAuthProviderClaimsInvalid, err,
		)
	}
	defer adapter.close()
	token, err := adapter.fetchToken(code, verifier)
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusUnauthorized, errorCodeAuthProviderClaimsInvalid,
			"Couldn't verify the login provider exchange.", "auth_provider.callback", "token-exchange",
			errorCodeAuthProviderClaimsInvalid, err,
		)
	}
	claims, err := adapter.validateProfile(token, state.NonceHash)
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusUnauthorized, errorCodeAuthProviderClaimsInvalid,
			"Couldn't verify the login provider identity.", "auth_provider.callback", "claims-validation",
			errorCodeAuthProviderClaimsInvalid, err,
		)
	}
	exchangeCode, err := completeAuthProviderSignIn(event.App, state, claims)
	if errors.Is(err, errSignupRestricted) || errors.Is(err, errSignupPolicyUnavailable) {
		failure := signupPolicyFailure(event, err)
		return recordRejectedAuthAttemptWithCause(
			event, "", claims.Email, authAuditMethodOAuthSignin, authAuditPhaseVerify,
			failure.Status, failure.Code, failure.Message, "auth_provider.callback",
			failure.Stage, failure.CauseCode, err,
		)
	}
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", claims.Email, authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusInternalServerError, errorCodeAuthProviderAccountFailed,
			"Couldn't prepare the Leafen account.", "auth_provider.callback", "account-prepare",
			errorCodeAuthProviderAccountFailed, err,
		)
	}
	destination, err := authProviderCompletionDestination(event.Request, state, exchangeCode)
	if err != nil {
		return recordRejectedAuthAttemptWithCause(
			event, "", claims.Email, authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusInternalServerError, errorCodeAuthProviderAccountFailed,
			"Couldn't create the login completion URL.", "auth_provider.callback", "redirect-build",
			errorCodeAuthProviderAccountFailed, err,
		)
	}
	keepAuthProviderReturn(event.App, config, state.RedirectURI)
	completed = true
	return event.Redirect(http.StatusFound, destination)
}

// authProviderRedirectURI is the address a provider sends people back to,
// which its administrators register with it; none without the server's
// address.
func authProviderRedirectURI(origin, provider string) string {
	if origin == "" {
		return ""
	}
	return origin + apiRoot + "/auth/providers/" + provider + "/callback"
}

// authProviderReturnOrigin is the server's address its administrators
// register with a sign-in provider: the written address, else the chosen one
// (server_tunnel_chosen.go) — the one people outside reach, and the https a
// provider asks for — else the address the request came to; none when it
// can't be read.
func authProviderReturnOrigin(event *core.RequestEvent) string {
	if strings.TrimSpace(os.Getenv("LEAFEN_PUBLIC_URL")) == "" {
		if chosen := chosenServerOrigin(); chosen != "" {
			return chosen
		}
	}
	origin, err := publicServerOrigin(event.Request)
	if err != nil {
		return ""
	}
	return origin
}

// authProviderCompletionDestination is where a finished sign-in goes with its
// completion code: the app that started it, or the server's own page.
func authProviderCompletionDestination(request *http.Request, state storedAuthProviderState, code string) (string, error) {
	if state.AppReturn != "" {
		return authAppReturnDestination(state.AppReturn, "code", code)
	}
	origin, err := publicServerOrigin(request)
	if err != nil {
		return "", err
	}
	destination, err := url.Parse(origin + "/auth/callback")
	if err != nil {
		return "", err
	}
	destination.RawQuery = url.Values{"code": []string{code}}.Encode()
	return destination.String(), nil
}

func exchangeAuthProviderSession(event *core.RequestEvent) error {
	input := authProviderExchangeInput{}
	if err := event.BindBody(&input); err != nil {
		return recordRejectedAuthAttempt(
			event, "", "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusBadRequest, errorCodeAuthProviderExchangeInvalid, "Check the login completion code.",
		)
	}
	if strings.TrimSpace(input.Code) == "" {
		return refuseMissingInput(event, errorCodeAuthProviderExchangeInvalid, "code", "the completion code the login page sent back.")
	}
	response, challenge, ownerID, err := consumeAuthProviderExchange(
		event.App,
		strings.TrimSpace(input.Code),
		strings.TrimSpace(input.Verifier),
		browserSessionMetadataFromEvent(event),
	)
	if errors.Is(err, errAuthProviderExchangeUsed) {
		return recordRejectedAuthAttempt(
			event, ownerID, "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusConflict, errorCodeAuthProviderExchangeConsumed, "This login completion code was already used.",
		)
	}
	if errors.Is(err, errAuthProviderExchangeInvalid) {
		return recordRejectedAuthAttempt(
			event, ownerID, "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusBadRequest, errorCodeAuthProviderExchangeInvalid, "The login completion code expired or is invalid.",
		)
	}
	if errors.Is(err, errSAMLRequired) {
		return recordRejectedAuthAttempt(
			event, ownerID, "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			http.StatusForbidden, errorCodeSAMLRequired, "This organization requires SAML login.",
		)
	}
	if err != nil {
		failure := browserSessionFailure(err)
		return recordRejectedAuthAttemptWithCause(
			event, ownerID, "", authAuditMethodOAuthSignin, authAuditPhaseVerify,
			failure.Status, failure.Code, failure.Message, "auth_provider.exchange",
			failure.Stage, failure.CauseCode, err,
		)
	}
	if challenge.MFARequired {
		return event.JSON(http.StatusAccepted, challenge)
	}
	return event.JSON(http.StatusOK, response)
}

// returnAuthProviderFailures brings a refused provider step, which the
// browser reached by following a link, back to the sign-in page with the
// refusal's code and reference, recorded as before (2026-10-16: an
// unreachable provider showed its JSON error as the page).
func returnAuthProviderFailures(step func(*core.RequestEvent) error) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		original := event.Response
		capture := &authProviderResponseCapture{header: http.Header{}}
		event.Response = capture
		err := step(event)
		event.Response = original
		if err != nil {
			return err
		}
		if capture.status < http.StatusBadRequest {
			for key, values := range capture.header {
				original.Header()[key] = values
			}
			original.WriteHeader(capture.status)
			_, writeErr := original.Write(capture.body.Bytes())
			return writeErr
		}
		refusal := errorResponse{}
		origin, originErr := publicServerOrigin(event.Request)
		if json.Unmarshal(capture.body.Bytes(), &refusal) != nil || refusal.Error.Code == "" || originErr != nil {
			for key, values := range capture.header {
				original.Header()[key] = values
			}
			original.WriteHeader(capture.status)
			_, writeErr := original.Write(capture.body.Bytes())
			return writeErr
		}
		query := url.Values{"error": {refusal.Error.Code}}
		if refusal.Error.Reference != "" {
			query.Set("reference", refusal.Error.Reference)
		}
		return event.Redirect(http.StatusFound, origin+"/auth/callback?"+query.Encode())
	}
}

type authProviderResponseCapture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (capture *authProviderResponseCapture) Header() http.Header { return capture.header }

func (capture *authProviderResponseCapture) Write(data []byte) (int, error) {
	if capture.status == 0 {
		capture.status = http.StatusOK
	}
	return capture.body.Write(data)
}

func (capture *authProviderResponseCapture) WriteHeader(status int) {
	if capture.status == 0 {
		capture.status = status
	}
}
