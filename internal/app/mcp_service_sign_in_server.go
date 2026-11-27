package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// A service's sign-in server — the metadata that names it, its registration
// and its token endpoint — is found here, and a sign-in server that can't
// serve for now is told apart from an address that isn't an MCP server: a
// service that answered as an MCP server, asked to sign in and then had its
// registration answer 503 was told "the service didn't answer as an MCP
// server", and the person read "이 주소가 MCP 서버로 답하지 않았습니다.
// 주소를 확인하세요." beside the address's check mark, and the same under the
// row's 다시 로그인 (2026-11-20, a synthetic service). By one rule: a sign-in
// server the service named that gave no answer, or answered that it failed or
// was busy (5xx, 408, 429), is errMCPServiceSignInUnavailable, said as "the
// address is right, its login server isn't answering now, try again in a
// while" — at the address check, when connecting, signing in again, coming
// back from the sign-in page, renewing a sign-in and checking the service, and
// to an agent in the same words. Only a server the service named counts:
// without its metadata the endpoints are only guessed, and a server that takes
// only tokens may fail at any address it doesn't serve, so those stay
// failures, as does a server the server doesn't connect to (a private address).

const errorCodeMCPServiceSignInUnavailable = "LEAFEN_MCP_SERVICE_SIGN_IN_UNAVAILABLE"

var errMCPServiceSignInUnavailable = errors.New(errorCodeMCPServiceSignInUnavailable)

// mcpServiceSignInUnavailableText says it to the person's agent and in the
// API's answer; the screens say it in the person's language.
const mcpServiceSignInUnavailableText = "The address is right: the service answered as an MCP server, " +
	"but its login server isn't answering right now, so the login couldn't go on. Try again in a while."

// mcpServiceSignInDown says whether a sign-in server's answer (0: none came)
// means it can't serve for now.
func mcpServiceSignInDown(status int) bool {
	return status == 0 || status >= http.StatusInternalServerError || status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests
}

// mcpServiceAnswerStatus keeps how the last request through it went: the
// answer's status (0: none came), and whether it went where the server
// doesn't connect.
type mcpServiceAnswerStatus struct {
	base   http.RoundTripper
	asked  bool
	status int
	unsafe bool
}

func (seen *mcpServiceAnswerStatus) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := seen.base.RoundTrip(request)
	seen.asked, seen.status, seen.unsafe = true, 0, errors.Is(err, errOutboundUnsafeDestination)
	if err == nil {
		seen.status = response.StatusCode
	}
	return response, err
}

// down says whether the last request found the sign-in server unable to
// serve for now.
func (seen *mcpServiceAnswerStatus) down() bool {
	return seen.asked && !seen.unsafe && mcpServiceSignInDown(seen.status)
}

// watchMCPServiceAnswers is client, keeping how its last request went.
func watchMCPServiceAnswers(client *http.Client) (*http.Client, *mcpServiceAnswerStatus) {
	watched, seen := *client, &mcpServiceAnswerStatus{base: client.Transport}
	if seen.base == nil {
		seen.base = http.DefaultTransport
	}
	watched.Transport = seen
	return &watched, seen
}

// mcpServiceSignInFailure is err from a sign-in server the service named:
// unavailable where it was down, else a failure.
func mcpServiceSignInFailure(down bool, err error) error {
	if down {
		return fmt.Errorf("%w: %v", errMCPServiceSignInUnavailable, err)
	}
	return fmt.Errorf("%w: %v", errMCPServiceFailed, err)
}

// mcpServiceAuthServer is the service's authorization server as its
// metadata says (found: it said; else MCP 2025-03-26's defaults on the
// service's origin), and the scopes the service asks for.
type mcpServiceAuthServer struct {
	plan   mcpServiceOAuthPlan
	meta   *oauthex.AuthServerMeta
	scopes []string
	found  bool
}

// findMCPServiceAuthServer reads the service's protected-resource metadata
// and its authorization server's. The metadata its challenge named, or the
// authorization server the metadata named, being down is said so.
func findMCPServiceAuthServer(ctx context.Context, client *http.Client, serviceURL string, challenges []oauthex.Challenge) (mcpServiceAuthServer, error) {
	server := mcpServiceAuthServer{}
	plan := &server.plan
	target, err := url.Parse(serviceURL)
	if err != nil {
		return server, errMCPServiceInvalid
	}
	client, seen := watchMCPServiceAnswers(client)
	scopes := []string{}
	metadataURL := ""
	for _, challenge := range challenges {
		if challenge.Scheme != "bearer" {
			continue
		}
		if scope := challenge.Params["scope"]; scope != "" && len(scopes) == 0 {
			scopes = strings.Fields(scope)
		}
		if metadataURL == "" {
			metadataURL = challenge.Params["resource_metadata"]
		}
	}
	type candidate struct{ metadata, resource string }
	resources := []string{serviceURL}
	if trimmed := strings.TrimSuffix(serviceURL, "/"); trimmed != serviceURL {
		resources = append(resources, trimmed)
	}
	candidates := []candidate{}
	for _, resource := range resources {
		if metadataURL != "" {
			candidates = append(candidates, candidate{metadataURL, resource})
		}
		inserted := *target
		inserted.RawQuery, inserted.Path = "", "/.well-known/oauth-protected-resource/"+strings.TrimLeft(target.Path, "/")
		candidates = append(candidates, candidate{strings.TrimSuffix(inserted.String(), "/"), resource})
	}
	root := url.URL{Scheme: target.Scheme, Host: target.Host}
	candidates = append(candidates, candidate{root.String() + "/.well-known/oauth-protected-resource", root.String()})
	var resourceMeta *oauthex.ProtectedResourceMetadata
	namedDown := false
	for _, each := range candidates {
		seen.asked = false
		found, err := oauthex.GetProtectedResourceMetadata(ctx, each.metadata, each.resource, client)
		if err == nil && found != nil {
			resourceMeta = found
			break
		}
		namedDown = namedDown || each.metadata == metadataURL && seen.down()
	}
	// The metadata the challenge named was down and none other answered.
	namedDown = namedDown && resourceMeta == nil
	// Without the metadata, the service's own origin is its authorization
	// server (MCP 2025-03-26).
	plan.Issuer, plan.Resource = root.String(), serviceURL
	if resourceMeta != nil {
		if len(resourceMeta.AuthorizationServers) == 0 {
			return server, fmt.Errorf("%w: the service names no authorization server", errMCPServiceFailed)
		}
		plan.Issuer, plan.Resource = resourceMeta.AuthorizationServers[0], resourceMeta.Resource
		if len(scopes) == 0 {
			scopes = resourceMeta.ScopesSupported
		}
	}
	seen.asked = false
	serverMeta, err := mcpauth.GetAuthServerMetadata(ctx, plan.Issuer, client)
	if err != nil {
		// The library refuses a server without PKCE itself, saying so only in
		// its message; the person is told to use a token instead.
		if strings.Contains(err.Error(), "does not implement PKCE") {
			return server, errMCPServiceNoPKCE
		}
		return server, mcpServiceSignInFailure(namedDown || resourceMeta != nil && seen.down(), err)
	}
	server.found = resourceMeta != nil || serverMeta != nil
	if !server.found && namedDown {
		return server, fmt.Errorf("%w: the metadata its challenge named didn't answer", errMCPServiceSignInUnavailable)
	}
	if serverMeta == nil {
		issuer := strings.TrimSuffix(plan.Issuer, "/")
		serverMeta = &oauthex.AuthServerMeta{Issuer: plan.Issuer, AuthorizationEndpoint: issuer + "/authorize",
			TokenEndpoint: issuer + "/token", RegistrationEndpoint: issuer + "/register", CodeChallengeMethodsSupported: []string{"S256"}}
	}
	if !slices.Contains(serverMeta.CodeChallengeMethodsSupported, "S256") {
		return server, errMCPServiceNoPKCE
	}
	server.meta, server.scopes = serverMeta, scopes
	return server, nil
}
