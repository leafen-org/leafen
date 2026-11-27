package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// Leafen registers itself with a service's authorization server before it
// names itself by its client ID metadata document, unlike MCP 2025-11-25's
// order: the service's server must read the document at this server's
// address, and a self-hosted Leafen on a tailnet or a LAN, or behind a port
// the service won't call, is out of its reach — Notion's sign-in then
// refuses it ("Invalid client_id", tried 2026-11-19) — while a registered
// client only sends the person's browser back here, which reaches it
// wherever they opened Leafen. The document is used where the service
// registers no clients or refused this one. A sign-in again goes by the
// same order (prepareMCPServiceSignIn).

// A registration the service answers and refuses is told apart from one
// that didn't reach it: a service that answered as an MCP server and then
// refused Leafen's registration (RFC 7591's error, or a 4xx such as asking
// for an initial access token) was told "the address didn't answer as an
// MCP server" and the person doubted the address (2026-11-20, a synthetic
// service). Where it reads no document Leafen can use instead, the person
// is told the service refused and to make a client there for Leafen — in
// settings beside the client's fields, and to an agent in the same words
// (mcpServiceClientNeedText). An endpoint that isn't there (404, 405, 410,
// 501) is a service that registers no clients; one that didn't answer, was
// busy (408, 429) or failed (5xx) is a sign-in server down for now
// (mcp_service_sign_in_server.go). The address check can't know a refusal
// before the press: it would register a client at the service each time an
// address is typed.

var errMCPServiceRegistrationRefused = errors.New(errorCodeMCPServiceRegistrationRefused)

// mcpServiceRegistrationRefused is errMCPServiceRegistrationRefused with
// the error code the service answered (RFC 7591 3.2.2), when it gave one,
// and the redirect Leafen asked to register.
type mcpServiceRegistrationRefused struct {
	status                    int
	said, described, redirect string
}

func (refused *mcpServiceRegistrationRefused) Error() string {
	return fmt.Sprintf("the service refused the registration (%d %s %s)", refused.status, refused.said, refused.described)
}

func (refused *mcpServiceRegistrationRefused) Is(target error) bool {
	return target == errMCPServiceRegistrationRefused
}

// registerMCPServiceClient registers this server as a client of the
// service's authorization server, coming back to redirect. Where the
// service's metadata wasn't found, the endpoint is only guessed (MCP
// 2025-03-26's defaults) and its refusal says nothing of a sign-in — a
// server that takes only tokens refuses everything — so it is a failure.
func registerMCPServiceClient(ctx context.Context, client *http.Client, server mcpServiceAuthServer, redirect, method string,
	scopes []string) (*oauthex.ClientRegistrationResponse, error) {
	serverMeta := server.meta
	if serverMeta.RegistrationEndpoint == "" {
		return nil, errMCPServiceNoRegistration
	}
	watched, seen := watchMCPServiceAnswers(client)
	registered, err := oauthex.RegisterClient(ctx, serverMeta.RegistrationEndpoint, &oauthex.ClientRegistrationMetadata{
		RedirectURIs: []string{redirect}, TokenEndpointAuthMethod: method, GrantTypes: []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"}, ClientName: "Leafen", Scope: strings.Join(scopes, " "),
	}, watched)
	answered := &oauthex.ClientRegistrationError{}
	switch {
	case err == nil:
		return registered, nil
	case !server.found:
		return nil, fmt.Errorf("%w: %v", errMCPServiceFailed, err)
	case errors.As(err, &answered):
	case seen.status == http.StatusNotFound || seen.status == http.StatusMethodNotAllowed || seen.status == http.StatusGone ||
		seen.status == http.StatusNotImplemented:
		return nil, errMCPServiceNoRegistration
	case seen.status < 400 || mcpServiceSignInDown(seen.status):
		return nil, mcpServiceSignInFailure(seen.down(), err)
	}
	refused := &mcpServiceRegistrationRefused{status: seen.status, described: appOneLine(answered.ErrorDescription, 200), redirect: redirect}
	// Only a code's own characters are repeated to the person's agent.
	if code := answered.ErrorCode; len(code) <= 64 && !strings.ContainsFunc(code, func(r rune) bool {
		return !(r == '_' || r == '-' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) {
		refused.said = code
	}
	return nil, refused
}

// chooseMCPServiceScopes is the scopes asked for: those of the service's the
// person kept (none chosen: all of them). offline_access, which Leafen adds
// itself where the authorization server has it, is not the person's to
// keep: a client made anew from a service's kept scopes leaves it out where
// the service doesn't offer it, and it is added again.
func chooseMCPServiceScopes(offered, chosen []string) ([]string, error) {
	if len(chosen) == 0 {
		return offered, nil
	}
	kept := []string{}
	for _, scope := range chosen {
		switch {
		case scope == "offline_access" && !slices.Contains(offered, scope):
		case !slices.Contains(offered, scope):
			return nil, errMCPServiceInvalid
		case !slices.Contains(kept, scope):
			kept = append(kept, scope)
		}
	}
	if len(kept) == 0 {
		return offered, nil
	}
	return kept, nil
}
