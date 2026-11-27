package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/auth"
	"golang.org/x/oauth2"
)

type authProviderPlatformAdapter struct {
	config   authProviderConfig
	provider auth.Provider
	cancel   context.CancelFunc
}

func newAuthProviderPlatformAdapter(
	ctx context.Context,
	config authProviderConfig,
	metadata authProviderDiscovery,
	redirectURI string,
) (*authProviderPlatformAdapter, error) {
	if len(metadata.TokenEndpointAuthMethods) > 0 &&
		!containsString(metadata.TokenEndpointAuthMethods, "client_secret_basic") &&
		!containsString(metadata.TokenEndpointAuthMethods, "client_secret_post") {
		return nil, errors.New(errorCodeAuthProviderResponseInvalid)
	}
	pkce := true
	providerContext, cancel := context.WithTimeout(ctx, authProviderHTTPClient.Timeout)
	provider, err := (core.OAuth2ProviderConfig{
		Name:         auth.NameOIDC,
		ClientId:     config.ClientID,
		ClientSecret: config.ClientSecret,
		AuthURL:      metadata.AuthorizationEndpoint,
		TokenURL:     metadata.TokenEndpoint,
		DisplayName:  config.Name,
		PKCE:         &pkce,
		Extra: map[string]any{
			"jwksURL": metadata.JWKSURI,
			"issuers": []string{config.Issuer},
		},
	}).InitProvider()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s: %w", errorCodeAuthProviderResponseInvalid, err)
	}
	provider.SetContext(context.WithValue(providerContext, oauth2.HTTPClient, authProviderHTTPClient))
	provider.SetRedirectURL(redirectURI)
	provider.SetScopes(config.Scopes)
	return &authProviderPlatformAdapter{config: config, provider: provider, cancel: cancel}, nil
}

func (adapter *authProviderPlatformAdapter) close() {
	adapter.cancel()
}

func (adapter *authProviderPlatformAdapter) authorizationURL(state, nonce, verifier string) (string, error) {
	raw := adapter.provider.BuildAuthURL(
		state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", errorCodeAuthProviderResponseInvalid, err)
	}
	if !validOIDCEndpoint(raw) {
		return "", errors.New(errorCodeAuthProviderResponseInvalid)
	}
	query := parsed.Query()
	if query.Get("state") != state || query.Get("nonce") != nonce ||
		query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
		return "", errors.New(errorCodeAuthProviderResponseInvalid)
	}
	return raw, nil
}

func (adapter *authProviderPlatformAdapter) fetchToken(code, verifier string) (*oauth2.Token, error) {
	token, err := adapter.provider.FetchToken(code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errorCodeAuthProviderResponseInvalid, err)
	}
	if token == nil {
		return nil, errors.New(errorCodeAuthProviderResponseInvalid)
	}
	idToken, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(idToken) == "" {
		return nil, errors.New(errorCodeAuthProviderResponseInvalid)
	}
	return token, nil
}

func (adapter *authProviderPlatformAdapter) validateProfile(
	token *oauth2.Token,
	nonceHash string,
) (authProviderClaims, error) {
	user, err := adapter.provider.FetchAuthUser(token)
	if err != nil {
		return authProviderClaims{}, fmt.Errorf("%s: %w", errorCodeAuthProviderClaimsInvalid, err)
	}
	if user == nil {
		return authProviderClaims{}, errors.New(errorCodeAuthProviderClaimsInvalid)
	}
	issuer, _ := user.RawUser["iss"].(string)
	nonce, _ := user.RawUser["nonce"].(string)
	email, emailOK := normalizeEmailIdentity(user.Email)
	name := strings.TrimSpace(user.Name)
	if name == "" && email != "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	if issuer != adapter.config.Issuer || user.Id == "" || len(user.Id) > 512 ||
		!emailOK || name == "" || len(name) > maxAuthNameLength ||
		!authProviderNumericClaimPresent(user.RawUser, "iat") ||
		!authProviderNumericClaimPresent(user.RawUser, "exp") ||
		hashAuthProviderToken(nonce) != nonceHash {
		return authProviderClaims{}, errors.New(errorCodeAuthProviderClaimsInvalid)
	}
	return authProviderClaims{
		Issuer: issuer, Subject: user.Id, Email: email, EmailVerified: true, Name: name,
		SecondFactor: oidcAMRAssertsMFA(user.RawUser["amr"]),
	}, nil
}

func authProviderNumericClaimPresent(claims map[string]any, key string) bool {
	switch value := claims[key].(type) {
	case float64:
		return value > 0
	case float32:
		return value > 0
	case int:
		return value > 0
	case int64:
		return value > 0
	case int32:
		return value > 0
	default:
		return false
	}
}
