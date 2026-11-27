package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

type mcpOAuthClientRegistration struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

type mcpOAuthClientResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

type storedMCPOAuthClient struct {
	ID               string `db:"id"`
	ClientName       string `db:"client_name"`
	RedirectURIsJSON string `db:"redirect_uris_json"`
	MetadataHash     string `db:"metadata_hash"`
	Created          int64  `db:"created"`
}

func registerMCPOAuthClient(event *core.RequestEvent) error {
	var input mcpOAuthClientRegistration
	if err := event.BindBody(&input); err != nil {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_client_metadata", "invalid client metadata")
	}
	normalized, err := normalizeMCPOAuthClient(input)
	if err != nil {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_client_metadata", err.Error())
	}
	metadata, _ := json.Marshal(normalized)
	digest := sha256.Sum256(metadata)
	metadataHash := hex.EncodeToString(digest[:])

	var response mcpOAuthClientResponse
	created := false
	full := false
	err = event.App.RunInTransaction(func(txApp core.App) error {
		client, err := findMCPOAuthClientByMetadata(txApp, metadataHash)
		if err == nil {
			if err := touchMCPOAuthClient(txApp, client.ID, time.Now().UnixMilli()); err != nil {
				return err
			}
			response, err = mcpOAuthClientResponseFor(client)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := time.Now()
		if err := admitMCPOAuthClientInTransaction(
			event.Request.Context(), txApp, now.UnixMilli(),
		); errors.Is(err, errMCPOAuthClientCapacity) {
			full = true
			return nil
		} else if err != nil {
			return err
		}
		redirects, _ := json.Marshal(normalized.RedirectURIs)
		client = storedMCPOAuthClient{
			ID: "lmc_client_" + security.RandomString(24), ClientName: normalized.ClientName,
			RedirectURIsJSON: string(redirects), MetadataHash: metadataHash, Created: now.Unix(),
		}
		if _, err := txApp.DB().Insert(mcpOAuthClientsTable, dbx.Params{
			"id": client.ID, "client_name": client.ClientName, "redirect_uris_json": client.RedirectURIsJSON,
			"metadata_hash": client.MetadataHash, "created": client.Created, "last_used_at": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		notifyMCPCredentialRegistryAfterCommit(txApp)
		response, err = mcpOAuthClientResponseFor(client)
		created = true
		return err
	})
	if full {
		return oauthProtocolErrorWithProductCode(
			event, http.StatusConflict, "invalid_client_metadata",
			errorCodeMCPOAuthClientCapacity, "This Leafen server has no room to register another agent app (it keeps 256 and frees ones unused for 90 days). "+
				"Connect with a connection code from Leafen's "+mcpScreen("", "settings", "features", "agents")+" instead, or ask the server's administrator.",
		)
	}
	if err != nil {
		return oauthServerError(event, "client registration failed", err)
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	return event.JSON(status, response)
}

// normalizeMCPOAuthClient is a registration as it is kept, or why it is
// refused, said so the client's maker knows what to change (2026-11-07: every
// refusal read "invalid client metadata"). What a registration leaves out
// takes RFC 7591's defaults; a client that asks for authorization codes
// alone is kept as one that may refresh them too, as every token here does.
func normalizeMCPOAuthClient(input mcpOAuthClientRegistration) (mcpOAuthClientRegistration, error) {
	input.ClientName = strings.TrimSpace(input.ClientName)
	if input.ClientName == "" || len([]rune(input.ClientName)) > 255 {
		return mcpOAuthClientRegistration{}, errors.New("client_name is required, up to 255 characters")
	}
	if len(input.RedirectURIs) == 0 || len(input.RedirectURIs) > 8 {
		return mcpOAuthClientRegistration{}, errors.New("redirect_uris takes 1 to 8 addresses")
	}
	redirects := uniqueNonemptyStrings(input.RedirectURIs)
	if len(redirects) != len(input.RedirectURIs) {
		return mcpOAuthClientRegistration{}, errors.New("redirect_uris has an empty or repeated address")
	}
	for _, redirect := range redirects {
		if !validMCPOAuthRedirectURI(redirect) {
			return mcpOAuthClientRegistration{}, errors.New("redirect_uris takes https addresses, or http on localhost or a loopback IP, without a fragment: " + redirect)
		}
	}
	grantTypes := uniqueNonemptyStrings(input.GrantTypes)
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code"}
	}
	for _, grant := range grantTypes {
		if grant != "authorization_code" && grant != "refresh_token" {
			return mcpOAuthClientRegistration{}, errors.New("grant_types takes authorization_code and refresh_token; " + grant + " isn't offered")
		}
	}
	if !slices.Contains(grantTypes, "authorization_code") {
		return mcpOAuthClientRegistration{}, errors.New("grant_types must include authorization_code")
	}
	responseTypes := uniqueNonemptyStrings(input.ResponseTypes)
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}
	if !slices.Equal(responseTypes, []string{"code"}) {
		return mcpOAuthClientRegistration{}, errors.New("response_types takes code alone")
	}
	if input.TokenEndpointAuthMethod != "none" {
		return mcpOAuthClientRegistration{}, errors.New("token_endpoint_auth_method must be none: clients here are public and prove themselves with PKCE (S256)")
	}
	input.RedirectURIs = redirects
	input.GrantTypes = []string{"authorization_code", "refresh_token"}
	input.ResponseTypes = responseTypes
	return input, nil
}

func validMCPOAuthRedirectURI(raw string) bool {
	if len(raw) > 2048 {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" {
		return false
	}
	host := strings.Trim(parsed.Hostname(), "[]")
	return strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func findMCPOAuthClient(app core.App, clientID string) (storedMCPOAuthClient, error) {
	var client storedMCPOAuthClient
	err := app.DB().NewQuery(`
		SELECT [[id]], [[client_name]], [[redirect_uris_json]], [[metadata_hash]], [[created]]
		FROM {{leafen_mcp_oauth_clients}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": clientID}).One(&client)
	if errors.Is(err, sql.ErrNoRows) && strings.HasPrefix(clientID, "lapp_") {
		// An app a person registered is a client too.
		return findRegisteredAppClient(app, clientID)
	}
	return client, err
}

func findMCPOAuthClientByMetadata(app core.App, metadataHash string) (storedMCPOAuthClient, error) {
	var client storedMCPOAuthClient
	err := app.DB().NewQuery(`
		SELECT [[id]], [[client_name]], [[redirect_uris_json]], [[metadata_hash]], [[created]]
		FROM {{leafen_mcp_oauth_clients}} WHERE [[metadata_hash]] = {:metadataHash}
	`).Bind(dbx.Params{"metadataHash": metadataHash}).One(&client)
	return client, err
}

func mcpOAuthClientRedirects(client storedMCPOAuthClient) ([]string, error) {
	redirects := []string{}
	err := json.Unmarshal([]byte(client.RedirectURIsJSON), &redirects)
	return redirects, err
}

func mcpOAuthClientResponseFor(client storedMCPOAuthClient) (mcpOAuthClientResponse, error) {
	redirects, err := mcpOAuthClientRedirects(client)
	return mcpOAuthClientResponse{
		ClientID: client.ID, ClientIDIssuedAt: client.Created, ClientName: client.ClientName,
		RedirectURIs: redirects, GrantTypes: []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"}, TokenEndpointAuthMethod: "none",
	}, err
}

// clientAllowsRedirect reports whether a client may be sent back to an
// address: one it named, or one of its loopback addresses at any port, as an
// app on the person's computer listens on whatever port is free when it asks
// (RFC 8252 7.3; Codex names http://127.0.0.1/callback in its metadata
// document and asks with the port it got).
func clientAllowsRedirect(client storedMCPOAuthClient, redirectURI string) bool {
	redirects, err := mcpOAuthClientRedirects(client)
	if err != nil {
		return false
	}
	if slices.Contains(redirects, redirectURI) {
		return true
	}
	asked, err := url.Parse(redirectURI)
	if err != nil || !loopbackRedirect(asked) {
		return false
	}
	for _, redirect := range redirects {
		named, err := url.Parse(redirect)
		if err == nil && loopbackRedirect(named) && strings.EqualFold(named.Hostname(), asked.Hostname()) &&
			named.EscapedPath() == asked.EscapedPath() && named.RawQuery == asked.RawQuery {
			return true
		}
	}
	return false
}

// loopbackRedirect reports whether an address is plain http on this computer.
func loopbackRedirect(address *url.URL) bool {
	return address.Scheme == "http" && address.User == nil && address.Fragment == "" && mcpLoopbackHost(address.Host)
}
