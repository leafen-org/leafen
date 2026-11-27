package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Outside sign-in for apps (docs/app-platform.md 8): a person connects an
// app to an outside account through the provider's own page. The server
// makes a one-time state and PKCE pair bound to the person, the install and
// the provider; the provider sends the person back to the server, which
// trades the code at the token address — under the app's relay entries and
// their connection rules — and keeps the tokens sealed with the settings
// key, each person's own or one for the app. The app never sees a token:
// the relay fills {{oauth:KEY}} into headers bound for the addresses that
// name the sign-in, renewing an expired token first.

const (
	appOAuthStatesTable  = "leafen_app_oauth_states"
	appOAuthTokensTable  = "leafen_app_oauth_tokens"
	appOAuthCallbackPath = "/apps/oauth/callback"
	appOAuthOwnerApp     = "app"
	appOAuthOwnerPerson  = "person"
)

var (
	errAppOAuthNotReady = errors.New(errorCodeAppOAuthNotReady)
	errAppOAuthFailed   = errors.New(errorCodeAppOAuthFailed)
)

func appOAuthProvider(manifest appManifest, key string) (appManifestOAuth, bool) {
	for _, provider := range manifest.Network.OAuth {
		if provider.Key == key {
			return provider, true
		}
	}
	return appManifestOAuth{}, false
}

// appOAuthProviderUnknown refuses a sign-in the app doesn't declare by its
// name, not as a missing app: the app is there.
func appOAuthProviderUnknown(key string) error {
	return refuse(errAppInputInvalid, "the app has no login provider named %q", key)
}

// appOAuthHolder is whose token a sign-in keeps: the person's, or the app's.
func appOAuthHolder(provider appManifestOAuth, personID string) string {
	if provider.Owner == appOAuthOwnerApp {
		return ""
	}
	return personID
}

func appOAuthStateHash(state string) string {
	digest := sha256.Sum256([]byte("leafen-app-oauth-state\x00" + state))
	return hex.EncodeToString(digest[:])
}

// startAppOAuth makes a one-time state for the person and returns the
// provider's sign-in address.
func startAppOAuth(app core.App, install storedAppInstall, manifest appManifest, key, personID, origin string) (string, error) {
	provider, ok := appOAuthProvider(manifest, key)
	if !ok {
		// The app is found; what isn't is a sign-in it doesn't declare,
		// refused by its name as the administrator's routes refuse it.
		return "", appOAuthProviderUnknown(key)
	}
	secrets, err := appSecretValues(app, install.ID)
	if err != nil {
		return "", err
	}
	idName, _ := appOAuthClientSecretNames(key)
	clientID := secrets[idName]
	if clientID == "" {
		return "", errAppOAuthNotReady
	}
	authorize, err := url.Parse(provider.AuthorizeURL)
	if err != nil {
		return "", errAppOAuthNotReady
	}
	state, verifier := security.RandomString(43), security.RandomString(64)
	challenge := sha256.Sum256([]byte(verifier))
	sealed, err := encryptAdministratorSetting(app, cipherAppOAuthState, verifier)
	if err != nil {
		return "", err
	}
	// After an address change the provider may hold only the old return
	// address, which forwards here (app_platform_address_change.go).
	redirect := authProviderReturnFor(app, authProviderConfig{ID: appOAuthReturnKey(install.ID, key), ClientID: clientID},
		origin+apiRoot+appOAuthCallbackPath)
	now := time.Now()
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_app_oauth_states}} WHERE [[expires_at]] < {:now}`).
		Bind(dbx.Params{"now": now.UnixMilli()}).Execute(); err != nil {
		return "", err
	}
	if _, err := app.DB().Insert(appOAuthStatesTable, dbx.Params{
		"state_hash": appOAuthStateHash(state), "install_id": install.ID, "provider": key, "person_id": personID,
		"verifier": sealed, "redirect_uri": redirect, "expires_at": now.Add(appOAuthStateSeconds * time.Second).UnixMilli(),
	}).Execute(); err != nil {
		return "", err
	}
	query := authorize.Query()
	for name, value := range provider.AuthorizeParams {
		query.Set(name, value)
	}
	query.Set("response_type", "code")
	query.Set("client_id", clientID)
	query.Set("redirect_uri", redirect)
	query.Set("state", state)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	query.Set("code_challenge_method", "S256")
	if len(provider.Scopes) > 0 {
		query.Set("scope", strings.Join(provider.Scopes, " "))
	}
	authorize.RawQuery = query.Encode()
	return authorize.String(), nil
}

type appOAuthTokenAnswer struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    any    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// completeAppOAuth takes the provider's answer: the state, once, then the
// code traded for tokens kept for the state's person.
func completeAppOAuth(ctx context.Context, app core.App, state, code string) (storedAppInstall, string, error) {
	row := struct {
		InstallID   string `db:"install_id"`
		Provider    string `db:"provider"`
		PersonID    string `db:"person_id"`
		Verifier    string `db:"verifier"`
		RedirectURI string `db:"redirect_uri"`
		ExpiresAt   int64  `db:"expires_at"`
	}{}
	hash := appOAuthStateHash(state)
	err := app.DB().NewQuery(`SELECT [[install_id]], [[provider]], [[person_id]], [[verifier]], [[redirect_uri]], [[expires_at]]
		FROM {{leafen_app_oauth_states}} WHERE [[state_hash]] = {:hash}`).Bind(dbx.Params{"hash": hash}).One(&row)
	if err != nil {
		return storedAppInstall{}, "", fmt.Errorf("%w: the login expired or was used; start again", errAppOAuthFailed)
	}
	if _, err := app.DB().Delete(appOAuthStatesTable, dbx.HashExp{"state_hash": hash}).Execute(); err != nil {
		return storedAppInstall{}, "", err
	}
	if row.ExpiresAt < time.Now().UnixMilli() || code == "" {
		return storedAppInstall{}, row.Provider, fmt.Errorf("%w: the login expired or was refused; start again", errAppOAuthFailed)
	}
	install, err := loadAppInstall(app, row.InstallID)
	if err != nil || install.Status != "active" || install.RemovedAt != 0 {
		return storedAppInstall{}, row.Provider, refusedUnlessStorage(errAppInstallNotFound, err)
	}
	_, manifest, err := loadAppPackage(app, install.PackageHash)
	if err != nil {
		return install, row.Provider, err
	}
	provider, ok := appOAuthProvider(manifest, row.Provider)
	if !ok {
		return install, row.Provider, errAppInstallNotFound
	}
	verifier, err := decryptAdministratorSetting(app, row.Verifier)
	if err != nil {
		return install, row.Provider, err
	}
	answer, err := appOAuthTokenRequest(ctx, app, install, manifest, provider, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {row.RedirectURI}, "code_verifier": {verifier},
	})
	if err != nil {
		return install, row.Provider, err
	}
	if err := saveAppOAuthTokens(app, install.ID, provider, appOAuthHolder(provider, row.PersonID), answer, true); err != nil {
		return install, row.Provider, err
	}
	keepAuthProviderReturn(app, authProviderConfig{ID: appOAuthReturnKey(install.ID, row.Provider),
		ClientID: appOAuthClientID(app, install.ID, row.Provider)}, row.RedirectURI)
	return install, row.Provider, nil
}

// appOAuthTokenRequest asks the provider's token address, under the app's
// relay entries and their connection rules, with the app's client.
func appOAuthTokenRequest(ctx context.Context, app core.App, install storedAppInstall, manifest appManifest,
	provider appManifestOAuth, form url.Values) (appOAuthTokenAnswer, error) {
	answer := appOAuthTokenAnswer{}
	target, err := url.Parse(provider.TokenURL)
	if err != nil {
		return answer, errAppOAuthNotReady
	}
	relay, ok := appRelayEntryFor(manifest, target)
	if !ok {
		return answer, errAppOAuthNotReady
	}
	secrets, err := appSecretValues(app, install.ID)
	if err != nil {
		return answer, err
	}
	idName, secretName := appOAuthClientSecretNames(provider.Key)
	if secrets[idName] == "" {
		return answer, errAppOAuthNotReady
	}
	form.Set("client_id", secrets[idName])
	if secrets[secretName] != "" {
		form.Set("client_secret", secrets[secretName])
	}
	ctx, cancel := context.WithTimeout(ctx, appRelayTimeoutSeconds*time.Second)
	defer cancel()
	headers := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}, "Accept": {"application/json"}}
	response, err := appRelaySend(ctx, relay, "POST", target, headers, []byte(form.Encode()))
	if err != nil {
		return answer, fmt.Errorf("%w: %v", errAppOAuthFailed, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return answer, fmt.Errorf("%w: the provider's answer broke off", errAppOAuthFailed)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return answer, fmt.Errorf("%w: the provider answered %d", errAppOAuthFailed, response.StatusCode)
	}
	if json.Unmarshal(body, &answer) != nil {
		values, parseErr := url.ParseQuery(string(body))
		if parseErr != nil {
			return answer, fmt.Errorf("%w: the provider's answer isn't readable", errAppOAuthFailed)
		}
		answer = appOAuthTokenAnswer{AccessToken: values.Get("access_token"), RefreshToken: values.Get("refresh_token"),
			ExpiresIn: values.Get("expires_in"), Scope: values.Get("scope")}
	}
	if answer.AccessToken == "" {
		return answer, fmt.Errorf("%w: the provider sent no token", errAppOAuthFailed)
	}
	return answer, nil
}

func saveAppOAuthTokens(app core.App, installID string, provider appManifestOAuth, holder string, answer appOAuthTokenAnswer, connecting bool) error {
	access, err := encryptAdministratorSetting(app, cipherAppOAuthToken, answer.AccessToken)
	if err != nil {
		return err
	}
	refresh := ""
	if answer.RefreshToken != "" {
		if refresh, err = encryptAdministratorSetting(app, cipherAppOAuthToken, answer.RefreshToken); err != nil {
			return err
		}
	}
	expiresAt := int64(0)
	seconds := 0
	switch value := answer.ExpiresIn.(type) {
	case float64:
		seconds = int(value)
	case string:
		seconds, _ = strconv.Atoi(value)
	}
	now := time.Now()
	if seconds > 0 {
		expiresAt = now.Add(time.Duration(seconds) * time.Second).UnixMilli()
	}
	// A renewal without a new refresh token keeps the old one.
	_, err = app.DB().NewQuery(`INSERT INTO {{leafen_app_oauth_tokens}}
		([[install_id]], [[provider]], [[person_id]], [[access]], [[refresh]], [[expires_at]], [[scope]], [[connected_at]], [[updated_at]])
		VALUES ({:install}, {:provider}, {:person}, {:access}, {:refresh}, {:expires}, {:scope}, {:now}, {:now})
		ON CONFLICT ([[install_id]], [[provider]], [[person_id]]) DO UPDATE SET [[access]] = excluded.[[access]],
			[[refresh]] = CASE WHEN excluded.[[refresh]] <> '' THEN excluded.[[refresh]] ELSE [[refresh]] END,
			[[expires_at]] = excluded.[[expires_at]], [[scope]] = excluded.[[scope]], [[updated_at]] = excluded.[[updated_at]],
			[[connected_at]] = CASE WHEN {:connecting} THEN excluded.[[connected_at]] ELSE [[connected_at]] END`).Bind(dbx.Params{
		"install": installID, "provider": provider.Key, "person": holder, "access": access, "refresh": refresh,
		"expires": expiresAt, "scope": answer.Scope, "now": now.UnixMilli(), "connecting": connecting,
	}).Execute()
	return err
}

// appOAuthAccessToken is the person's (or the app's) token for a sign-in,
// renewed first when it has run out or nearly.
func appOAuthAccessToken(ctx context.Context, app core.App, install storedAppInstall, manifest appManifest, key, personID string) (string, error) {
	provider, ok := appOAuthProvider(manifest, key)
	if !ok {
		return "", fmt.Errorf("%w: the app has no login %s", errAppRelayDenied, key)
	}
	holder := appOAuthHolder(provider, personID)
	row := struct {
		Access    string `db:"access"`
		Refresh   string `db:"refresh"`
		ExpiresAt int64  `db:"expires_at"`
	}{}
	err := app.DB().NewQuery(`SELECT [[access]], [[refresh]], [[expires_at]] FROM {{leafen_app_oauth_tokens}}
		WHERE [[install_id]] = {:install} AND [[provider]] = {:provider} AND [[person_id]] = {:person}`).Bind(dbx.Params{
		"install": install.ID, "provider": key, "person": holder,
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: log in to %s first", errAppRelayDenied, key)
	}
	if err != nil {
		return "", err
	}
	if row.ExpiresAt == 0 || row.ExpiresAt > time.Now().Add(time.Minute).UnixMilli() {
		access, err := decryptAdministratorSetting(app, row.Access)
		if err != nil {
			return "", fmt.Errorf("%w: the %s login can't be read here; log in again", errAppRelayDenied, key)
		}
		return access, nil
	}
	if row.Refresh == "" {
		return "", fmt.Errorf("%w: the %s login ran out; log in again", errAppRelayDenied, key)
	}
	refresh, err := decryptAdministratorSetting(app, row.Refresh)
	if err != nil {
		return "", fmt.Errorf("%w: the %s login can't be read here; log in again", errAppRelayDenied, key)
	}
	answer, err := appOAuthTokenRequest(ctx, app, install, manifest, provider, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {refresh},
	})
	if err != nil {
		return "", err
	}
	if err := saveAppOAuthTokens(app, install.ID, provider, holder, answer, false); err != nil {
		return "", err
	}
	return answer.AccessToken, nil
}

func listAppOAuthAccounts(app core.App, install storedAppInstall, manifest appManifest, personID string) ([]appOAuthAccountWire, error) {
	accounts := []appOAuthAccountWire{}
	for _, provider := range manifest.Network.OAuth {
		owner := appOAuthOwnerPerson
		if provider.Owner == appOAuthOwnerApp {
			owner = appOAuthOwnerApp
		}
		account := appOAuthAccountWire{Key: provider.Key, Owner: owner}
		var access string
		err := app.DB().NewQuery(`SELECT [[connected_at]], [[access]] FROM {{leafen_app_oauth_tokens}}
			WHERE [[install_id]] = {:install} AND [[provider]] = {:provider} AND [[person_id]] = {:person}`).Bind(dbx.Params{
			"install": install.ID, "provider": provider.Key, "person": appOAuthHolder(provider, personID),
		}).Row(&account.ConnectedAt, &access)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		// A sign-in sealed under another key can't be used: connect again.
		if _, openErr := decryptAdministratorSetting(app, access); err == nil && openErr != nil {
			account.ConnectedAt = 0
		}
		account.Connected = account.ConnectedAt > 0
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func disconnectAppOAuth(app core.App, install storedAppInstall, manifest appManifest, key, personID string) error {
	provider, ok := appOAuthProvider(manifest, key)
	if !ok {
		return appOAuthProviderUnknown(key)
	}
	_, err := app.DB().Delete(appOAuthTokensTable, dbx.HashExp{
		"install_id": install.ID, "provider": key, "person_id": appOAuthHolder(provider, personID),
	}).Execute()
	return err
}
