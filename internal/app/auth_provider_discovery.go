package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const googleOIDCIssuer = "https://accounts.google.com"

type authProviderConfig struct {
	ID               string
	Name             string
	Issuer           string
	ClientID         string
	ClientSecret     string
	Scopes           []string
	SettingsRevision int
}

type authProviderDiscovery struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	JWKSURI                       string   `json:"jwks_uri"`
	ResponseTypesSupported        []string `json:"response_types_supported"`
	IDTokenSigningAlgorithms      []string `json:"id_token_signing_alg_values_supported"`
	TokenEndpointAuthMethods      []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
}

var authProviderHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

var (
	errAuthProviderNotFound    = errors.New(errorCodeAuthProviderNotFound)
	errAuthProviderUnavailable = errors.New(errorCodeAuthProviderUnavailable)
)

func listConfiguredAuthProviders(event *core.RequestEvent) error {
	settings, err := loadAdministratorSettings(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeAuthProviderUnavailable, "Couldn't load the login provider settings.", err)
	}
	providers := make([]authProviderSummary, 0, 2)
	if settings.GoogleEnabled && settings.GoogleClientID != "" && settings.GoogleSecretCipher != "" {
		providers = append(providers, authProviderSummary{ID: authProviderGoogle, Name: "Google"})
	}
	if settings.OIDCEnabled && settings.OIDCClientID != "" && settings.OIDCSecretCipher != "" {
		providers = append(providers, authProviderSummary{ID: authProviderOIDC, Name: settings.OIDCName})
	}
	return event.JSON(http.StatusOK, authProviderListResponse{Providers: providers})
}

func loadAuthProviderConfig(app core.App, provider string) (authProviderConfig, error) {
	settings, err := loadAdministratorSettings(app)
	if err != nil {
		return authProviderConfig{}, err
	}
	config := authProviderConfig{ID: provider, SettingsRevision: settings.Revision}
	switch provider {
	case authProviderGoogle:
		if !settings.GoogleEnabled || settings.GoogleClientID == "" || settings.GoogleSecretCipher == "" {
			return authProviderConfig{}, errAuthProviderNotFound
		}
		config.Name = "Google"
		config.Issuer = googleOIDCIssuer
		if testIssuer := strings.TrimSpace(os.Getenv("LEAFEN_TEST_GOOGLE_OIDC_ISSUER")); testIssuer != "" {
			if !isLoopbackIssuer(testIssuer) {
				return authProviderConfig{}, errAuthProviderUnavailable
			}
			config.Issuer = strings.TrimRight(testIssuer, "/")
		}
		config.ClientID = settings.GoogleClientID
		config.ClientSecret, err = decryptAdministratorSetting(app, settings.GoogleSecretCipher)
		config.Scopes = []string{"openid", "email", "profile"}
	case authProviderOIDC:
		if !settings.OIDCEnabled || settings.OIDCClientID == "" || settings.OIDCSecretCipher == "" {
			return authProviderConfig{}, errAuthProviderNotFound
		}
		config.Name = settings.OIDCName
		config.Issuer = strings.TrimRight(settings.OIDCIssuer, "/")
		config.ClientID = settings.OIDCClientID
		config.ClientSecret, err = decryptAdministratorSetting(app, settings.OIDCSecretCipher)
		config.Scopes = strings.Fields(settings.OIDCScopes)
	default:
		return authProviderConfig{}, errAuthProviderNotFound
	}
	if err != nil || config.ClientSecret == "" || !validOIDCIssuer(config.Issuer) {
		return authProviderConfig{}, failedBecause(errAuthProviderUnavailable, err)
	}
	return config, nil
}

func discoverAuthProvider(ctx context.Context, config authProviderConfig) (authProviderDiscovery, error) {
	discoveryURL := strings.TrimRight(config.Issuer, "/") + "/.well-known/openid-configuration"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return authProviderDiscovery{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := authProviderHTTPClient.Do(request)
	if err != nil {
		return authProviderDiscovery{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return authProviderDiscovery{}, errors.New(errorCodeAuthProviderUnavailable)
	}
	var metadata authProviderDiscovery
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxAuthProviderResponseBytes+1))
	if err := decoder.Decode(&metadata); err != nil {
		return authProviderDiscovery{}, err
	}
	if metadata.Issuer != config.Issuer || !validOIDCEndpoint(metadata.AuthorizationEndpoint) ||
		!validOIDCEndpoint(metadata.TokenEndpoint) || !validOIDCEndpoint(metadata.JWKSURI) ||
		!containsString(metadata.ResponseTypesSupported, "code") ||
		!containsString(metadata.IDTokenSigningAlgorithms, "RS256") ||
		!containsString(metadata.CodeChallengeMethodsSupported, "S256") {
		return authProviderDiscovery{}, errors.New(errorCodeAuthProviderResponseInvalid)
	}
	return metadata, nil
}

func validOIDCIssuer(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" &&
		parsed.Fragment == "" && (parsed.Scheme == "https" || isLoopbackURL(parsed))
}

func validOIDCEndpoint(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Host != "" && parsed.User == nil && parsed.Fragment == "" &&
		(parsed.Scheme == "https" || isLoopbackURL(parsed))
}

func isLoopbackIssuer(raw string) bool {
	parsed, err := url.Parse(strings.TrimRight(raw, "/"))
	return err == nil && parsed.RawQuery == "" && parsed.Fragment == "" && isLoopbackURL(parsed)
}

func isLoopbackURL(parsed *url.URL) bool {
	if parsed.Scheme != "http" || parsed.User != nil {
		return false
	}
	host := parsed.Hostname()
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
