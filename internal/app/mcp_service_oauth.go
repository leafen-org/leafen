package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"golang.org/x/oauth2"
)

// Signing in to a service for its person, as MCP's authorization says: the
// service's protected-resource metadata names its authorization server, and
// the server is a client there by the first way the server takes: an OAuth
// client the person made on the service for Leafen, registering itself
// (dynamic registration, before the document: mcp_service_oauth_register.go),
// or this server's client ID metadata document. The person approves on the
// service's own page with PKCE; the service sends them back here, the code
// is traded for tokens, and the tokens are renewed before they run out.
// Disconnecting asks the service to revoke them when it says how.

const (
	mcpServiceCallbackPath       = "/mcp-services/oauth/callback"
	mcpServiceClientMetadataPath = "/mcp-services/oauth/client-metadata"
	mcpServiceStateSeconds       = 600
	mcpServiceOAuthSeconds       = 20
	mcpServiceRenewalMargin      = time.Minute
)

var (
	mcpServiceRenewals          sync.Map
	errMCPServiceNoPKCE         = errors.New("the service's login doesn't take PKCE")
	errMCPServiceNoRegistration = errors.New("the service doesn't let clients register themselves")
)

// mcpServiceClient is an OAuth client the person made on a service for
// Leafen, for a service that lets no client register itself.
type mcpServiceClient struct{ ID, Secret string }

func (given mcpServiceClient) valid() bool {
	return len(given.ID) <= 255 && len(given.Secret) <= 1024 && !strings.ContainsAny(given.ID, " \t\r\n") &&
		!strings.ContainsAny(given.Secret, "\r\n") && (given.ID != "" || given.Secret == "")
}

// mcpServiceClientMetadata is this server's client ID metadata document,
// at its own address on a public https origin.
func mcpServiceClientMetadata(origin string) map[string]any {
	return map[string]any{
		"client_id": origin + apiRoot + mcpServiceClientMetadataPath, "client_name": "Leafen", "client_uri": origin,
		"redirect_uris": []string{origin + apiRoot + mcpServiceCallbackPath}, "grant_types": []string{"authorization_code", "refresh_token"},
		"response_types": []string{"code"}, "token_endpoint_auth_method": "none",
	}
}

type mcpServiceOAuthPlan struct {
	Issuer, AuthorizationEndpoint, TokenEndpoint, RevocationEndpoint, Resource, Scope string
	ClientID, ClientSecret                                                            string
	ClientAuth                                                                        oauth2.AuthStyle
	// ClientKind is how the server is a client (mcp_service_address_change.go).
	ClientKind string
}

func mcpServiceStateHash(state string) string {
	digest := sha256.Sum256([]byte("leafen-mcp-service-state\x00" + state))
	return hex.EncodeToString(digest[:])
}

// mcpServiceOAuthContext carries the client the token endpoint is asked
// with, and keeps how its last answer went (mcp_service_sign_in_server.go).
func mcpServiceOAuthContext(ctx context.Context) (context.Context, *mcpServiceAnswerStatus) {
	client, seen := watchMCPServiceAnswers(newMCPServiceHTTPClient())
	return context.WithValue(ctx, oauth2.HTTPClient, client), seen
}

// discoverMCPServiceOAuth finds the service's authorization server and is a
// client there — by the client given, by registering, or, where the service
// registers no clients or refused this one, by the metadata document at
// metadataDocument where it can be read (mcp_service_oauth_reach.go) — with
// the address the person comes back to, asking for the scopes chosen.
func discoverMCPServiceOAuth(ctx context.Context, serviceURL string, challenges []oauthex.Challenge, redirect string,
	given mcpServiceClient, metadataDocument string, chosen []string) (mcpServiceOAuthPlan, error) {
	client := newMCPServiceHTTPClient()
	ctx, cancel := context.WithTimeout(ctx, mcpServiceOAuthSeconds*time.Second)
	defer cancel()
	server, err := findMCPServiceAuthServer(ctx, client, serviceURL, challenges)
	plan, serverMeta := server.plan, server.meta
	if err != nil {
		return plan, err
	}
	scopes, err := chooseMCPServiceScopes(server.scopes, chosen)
	if err != nil {
		return plan, err
	}
	if slices.Contains(serverMeta.ScopesSupported, "offline_access") && !slices.Contains(scopes, "offline_access") {
		scopes = append(scopes, "offline_access")
	}
	supported := serverMeta.TokenEndpointAuthMethodsSupported
	method := "client_secret_basic"
	if len(supported) > 0 && !slices.Contains(supported, method) {
		method = "client_secret_post"
		if !slices.Contains(supported, method) {
			method = "none"
		}
	}
	switch {
	case given.ID != "":
		// The person's own client: its secret, when it has one, goes as the
		// server takes it; without one it is a public client.
		plan.ClientID, plan.ClientSecret, plan.ClientAuth, plan.ClientKind = given.ID, given.Secret, oauth2.AuthStyleInParams, mcpServiceClientGiven
		if given.Secret != "" && method == "client_secret_basic" {
			plan.ClientAuth = oauth2.AuthStyleInHeader
		}
	default:
		registered, err := registerMCPServiceClient(ctx, client, server, redirect, method, scopes)
		switch origin := strings.TrimSuffix(redirect, apiRoot+mcpServiceCallbackPath); {
		case err == nil:
			plan.ClientID, plan.ClientSecret, plan.ClientKind = registered.ClientID, registered.ClientSecret, mcpServiceClientRegistered
			plan.ClientAuth = oauth2.AuthStyleInHeader
			if registered.TokenEndpointAuthMethod == "client_secret_post" || registered.TokenEndpointAuthMethod == "none" ||
				registered.TokenEndpointAuthMethod == "" && method != "client_secret_basic" || registered.ClientSecret == "" {
				plan.ClientAuth = oauth2.AuthStyleInParams
			}
		case !serverMeta.ClientIDMetadataDocumentSupported:
			return plan, err
		case metadataDocument == "" || mcpServiceOutOfReach(ctx, mcpServiceDocumentAt(metadataDocument)):
			return plan, &mcpServiceUnreachable{origin: origin, public: mcpServicePublicOrigin(ctx, origin)}
		default:
			plan.ClientID, plan.ClientAuth, plan.ClientKind = metadataDocument, oauth2.AuthStyleInParams, mcpServiceClientDocument
		}
	}
	plan.AuthorizationEndpoint, plan.TokenEndpoint, plan.RevocationEndpoint = serverMeta.AuthorizationEndpoint,
		serverMeta.TokenEndpoint, serverMeta.RevocationEndpoint
	plan.Issuer, plan.Scope = firstNonempty(serverMeta.Issuer, plan.Issuer), strings.Join(scopes, " ")
	return plan, nil
}

func (stored storedMCPService) oauthConfig(app core.App) (*oauth2.Config, error) {
	secret := ""
	if stored.ClientSecret != "" {
		opened, err := decryptAdministratorSetting(app, stored.ClientSecret)
		if err != nil {
			return nil, errMCPServiceSignIn
		}
		secret = opened
	}
	return &oauth2.Config{
		ClientID: stored.ClientID, ClientSecret: secret, RedirectURL: stored.RedirectURI, Scopes: strings.Fields(stored.Scope),
		Endpoint: oauth2.Endpoint{AuthURL: stored.AuthorizationEndpoint, TokenURL: stored.TokenEndpoint, AuthStyle: oauth2.AuthStyle(stored.ClientAuth)},
	}, nil
}

// startMCPServiceSignIn keeps a one-time state and PKCE verifier on the
// service and returns the address of the service's sign-in page.
func startMCPServiceSignIn(app core.App, serviceID string) (string, error) {
	stored, err := loadMCPServiceByID(app, serviceID)
	if err != nil {
		return "", err
	}
	if err := refuseMCPServiceByPolicy(app, stored.UserID, stored.URL); err != nil {
		return "", err
	}
	// The client made anew for the address while one is (mcp_service_address_change.go).
	config, _, resource, err := stored.signInConfig(app)
	if err != nil {
		return "", err
	}
	state, verifier := security.RandomString(43), oauth2.GenerateVerifier()
	sealed, err := encryptAdministratorSetting(app, cipherMCPService, verifier)
	if err != nil {
		return "", err
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_mcp_services}} SET [[state_hash]] = {:state}, [[verifier]] = {:verifier},
		[[state_expires_at]] = {:expires}, [[updated_at]] = {:now} WHERE [[id]] = {:id}`).Bind(dbx.Params{
		"id": serviceID, "state": mcpServiceStateHash(state), "verifier": sealed,
		"expires": time.Now().Add(mcpServiceStateSeconds * time.Second).UnixMilli(), "now": time.Now().UnixMilli(),
	}).Execute(); err != nil {
		return "", err
	}
	return config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("resource", resource)), nil
}

func loadMCPServiceByID(app core.App, serviceID string) (storedMCPService, error) {
	stored := storedMCPService{}
	err := app.DB().NewQuery(`SELECT ` + storedMCPServiceColumns + ` FROM {{leafen_mcp_services}} [[service]] WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": serviceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, errMCPServiceNotFound
	}
	return stored, err
}

// CompleteMCPServiceSignIn takes the service's answer: the state, once,
// then the code traded for tokens, and the service read with them.
func CompleteMCPServiceSignIn(ctx context.Context, app core.App, state, code, issuer string) (storedMCPService, error) {
	row := struct {
		ID        string `db:"id"`
		Verifier  string `db:"verifier"`
		ExpiresAt int64  `db:"state_expires_at"`
	}{}
	hash := mcpServiceStateHash(state)
	if state == "" || app.DB().NewQuery(`SELECT [[id]], [[verifier]], [[state_expires_at]] FROM {{leafen_mcp_services}}
		WHERE [[state_hash]] = {:hash}`).Bind(dbx.Params{"hash": hash}).One(&row) != nil {
		return storedMCPService{}, fmt.Errorf("%w: the login expired or was used", errMCPServiceFailed)
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_mcp_services}} SET [[state_hash]] = '', [[verifier]] = '', [[state_expires_at]] = 0
		WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": row.ID}).Execute(); err != nil {
		return storedMCPService{}, err
	}
	stored, err := loadMCPServiceByID(app, row.ID)
	if err != nil {
		return stored, err
	}
	if row.ExpiresAt < time.Now().UnixMilli() || code == "" {
		return stored, fmt.Errorf("%w: the login expired or was refused", errMCPServiceFailed)
	}
	config, asked, resource, err := stored.signInConfig(app)
	if err != nil {
		return stored, err
	}
	// An authorization server that says who answered must be the one asked (RFC 9207).
	if issuer != "" && strings.TrimSuffix(issuer, "/") != strings.TrimSuffix(asked, "/") {
		return stored, fmt.Errorf("%w: another server answered the login", errMCPServiceFailed)
	}
	verifier, err := decryptAdministratorSetting(app, row.Verifier)
	if err != nil {
		return stored, err
	}
	tokenCtx, seen := mcpServiceOAuthContext(ctx)
	exchangeCtx, cancel := context.WithTimeout(tokenCtx, mcpServiceOAuthSeconds*time.Second)
	defer cancel()
	token, err := config.Exchange(exchangeCtx, code, oauth2.VerifierOption(verifier), oauth2.SetAuthURLParam("resource", resource))
	if err != nil {
		// A client made anew that failed leaves the one that worked.
		_ = clearMCPServicePendingPlan(app, stored.ID)
		return stored, mcpServiceSignInFailure(seen.down(), err)
	}
	if err := keepMCPServiceSignIn(app, stored); err != nil {
		return stored, err
	}
	if err := saveMCPServiceTokens(app, stored.ID, token); err != nil {
		return stored, err
	}
	reading, _, readErr := readMCPService(ctx, stored.URL, mcpServiceBearerHeaders(token.AccessToken))
	if err := saveMCPServiceCheck(app, stored.ID, reading, readErr); err != nil {
		return stored, err
	}
	return loadMCPServiceByID(app, stored.ID)
}

func saveMCPServiceTokens(app core.App, serviceID string, token *oauth2.Token) error {
	access, err := encryptAdministratorSetting(app, cipherMCPService, token.AccessToken)
	if err != nil {
		return err
	}
	refresh := ""
	if token.RefreshToken != "" {
		if refresh, err = encryptAdministratorSetting(app, cipherMCPService, token.RefreshToken); err != nil {
			return err
		}
	}
	expiresAt := int64(0)
	if !token.Expiry.IsZero() {
		expiresAt = token.Expiry.UnixMilli()
	}
	// A renewal without a new refresh token keeps the old one.
	_, err = app.DB().NewQuery(`UPDATE {{leafen_mcp_services}} SET [[access]] = {:access},
		[[refresh]] = CASE WHEN {:refresh} <> '' THEN {:refresh} ELSE [[refresh]] END, [[expires_at]] = {:expires},
		[[updated_at]] = {:now} WHERE [[id]] = {:id}`).Bind(dbx.Params{
		"id": serviceID, "access": access, "refresh": refresh, "expires": expiresAt, "now": time.Now().UnixMilli(),
	}).Execute()
	return err
}

// mcpServiceBearer is the sign-in to send the service, renewed first when
// it has run out or nearly, or when the service just refused it (refused);
// a refused renewal asks the person to sign in again.
func mcpServiceBearer(ctx context.Context, app core.App, stored storedMCPService, refused string) (string, error) {
	switch stored.Auth {
	case mcpServiceAuthNone:
		return "", nil
	case mcpServiceAuthToken:
		// The person's headers go on their own (requestHeaders).
		if stored.Access == "" {
			return "", nil
		}
		access, err := decryptAdministratorSetting(app, stored.Access)
		if err != nil {
			return "", errMCPServiceSignIn
		}
		return access, nil
	}
	fresh := func(service storedMCPService) bool {
		return service.ExpiresAt == 0 || service.ExpiresAt > time.Now().Add(mcpServiceRenewalMargin).UnixMilli()
	}
	if stored.Access == "" {
		return "", errMCPServiceSignIn
	}
	if refused == "" && fresh(stored) {
		access, err := decryptAdministratorSetting(app, stored.Access)
		if err != nil {
			return "", errMCPServiceSignIn
		}
		return access, nil
	}
	// One renewal at a time: services that turn refresh tokens over refuse the second.
	lock, _ := mcpServiceRenewals.LoadOrStore(stored.ID, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	current, err := loadMCPServiceByID(app, stored.ID)
	if err != nil {
		return "", err
	}
	access, err := decryptAdministratorSetting(app, current.Access)
	if current.Access == "" || err != nil {
		return "", errMCPServiceSignIn
	}
	if access != refused && fresh(current) {
		return access, nil
	}
	if current.Refresh == "" {
		return "", errMCPServiceSignIn
	}
	refresh, err := decryptAdministratorSetting(app, current.Refresh)
	if err != nil {
		return "", errMCPServiceSignIn
	}
	config, err := current.oauthConfig(app)
	if err != nil {
		return "", err
	}
	tokenCtx, seen := mcpServiceOAuthContext(ctx)
	renewCtx, cancel := context.WithTimeout(tokenCtx, mcpServiceOAuthSeconds*time.Second)
	defer cancel()
	token, err := config.TokenSource(renewCtx, &oauth2.Token{RefreshToken: refresh, Expiry: time.Unix(1, 0)}).Token()
	// A renewal the token endpoint refused asks to sign in again; one it
	// couldn't serve then is unavailable (mcp_service_sign_in_server.go).
	var answered *oauth2.RetrieveError
	if errors.As(err, &answered) && answered.Response != nil && !seen.down() {
		return "", errMCPServiceSignIn
	}
	if err != nil {
		return "", mcpServiceSignInFailure(seen.down(), err)
	}
	if err := saveMCPServiceTokens(app, current.ID, token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

// withMCPServiceSession runs work in a session with the service as its
// person, renewing a sign-in the service refuses once; a sign-in that stays
// refused marks the service to sign in again.
func withMCPServiceSession(ctx context.Context, app core.App, stored storedMCPService, work func(mcpServiceSession) error) error {
	// Each use follows the policies of the person's workspaces as they are now.
	if err := refuseMCPServiceByPolicy(app, stored.UserID, stored.URL); err != nil {
		return err
	}
	own, err := stored.requestHeaders(app)
	attempt := func(bearer string) error {
		session, _, err := openMCPServiceSession(ctx, stored.URL, append(mcpServiceBearerHeaders(bearer), own...))
		if err != nil {
			return err
		}
		defer session.Close()
		if err := work(session); err != nil {
			if _, signedOut := session.signIn.failure(err); errors.Is(signedOut, errMCPServiceSignIn) {
				return mcpRefusedUnlessStorage(errMCPServiceSignIn, err)
			}
			return err
		}
		return nil
	}
	bearer := ""
	if err == nil {
		bearer, err = mcpServiceBearer(ctx, app, stored, "")
	}
	if err == nil {
		err = attempt(bearer)
		if errors.Is(err, errMCPServiceSignIn) && stored.Auth == mcpServiceAuthOAuth {
			if bearer, err = mcpServiceBearer(ctx, app, stored, bearer); err == nil {
				err = attempt(bearer)
			}
		}
	}
	if errors.Is(err, errMCPServiceSignIn) {
		_ = saveMCPServiceCheck(app, stored.ID, mcpServiceReading{}, err)
	}
	return err
}

// mcpServiceContext is the context of Leafen's talk with a service: it ends
// when the call that asked for it ends, or after timeout, but carries none of
// that call's own values — an agent's MCP call holds the protocol version its
// client speaks, which the SDK would otherwise send the service as Leafen's.
func mcpServiceContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	stop := context.AfterFunc(parent, cancel)
	return ctx, func() { stop(); cancel() }
}

// readMCPServiceAs reads the service as its person.
func readMCPServiceAs(ctx context.Context, app core.App, stored storedMCPService) (mcpServiceReading, error) {
	ctx, cancel := mcpServiceContext(ctx, mcpServiceReadSeconds*time.Second)
	defer cancel()
	reading := mcpServiceReading{}
	err := withMCPServiceSession(ctx, app, stored, func(session mcpServiceSession) (err error) {
		reading, err = session.read(ctx)
		return err
	})
	return reading, err
}

// revokeMCPServiceTokens asks the service to forget the sign-in (RFC 7009),
// once and briefly; the server forgets it whatever the service answers.
func revokeMCPServiceTokens(app core.App, stored storedMCPService) {
	if stored.Auth != mcpServiceAuthOAuth || stored.RevocationEndpoint == "" {
		return
	}
	config, err := stored.oauthConfig(app)
	if err != nil {
		return
	}
	client := newMCPServiceHTTPClient()
	for _, sealed := range []struct{ value, hint string }{{stored.Refresh, "refresh_token"}, {stored.Access, "access_token"}} {
		token, err := decryptAdministratorSetting(app, sealed.value)
		if sealed.value == "" || err != nil {
			continue
		}
		form := url.Values{"token": {token}, "token_type_hint": {sealed.hint}}
		if config.Endpoint.AuthStyle != oauth2.AuthStyleInHeader {
			form.Set("client_id", config.ClientID)
			if config.ClientSecret != "" {
				form.Set("client_secret", config.ClientSecret)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, stored.RevocationEndpoint, strings.NewReader(form.Encode()))
		if err == nil {
			if config.Endpoint.AuthStyle == oauth2.AuthStyleInHeader {
				request.SetBasicAuth(url.QueryEscape(config.ClientID), url.QueryEscape(config.ClientSecret))
			}
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if response, err := client.Do(request); err == nil {
				response.Body.Close()
			} else {
				app.Logger().Warn("leafen service sign-in revocation failed", "service", stored.ID, "error", err)
			}
		}
		cancel()
	}
}
