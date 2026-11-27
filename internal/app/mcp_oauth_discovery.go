package app

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

type mcpOAuthAuthorizationServerMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	RegistrationEndpoint                       string   `json:"registration_endpoint"`
	RevocationEndpoint                         string   `json:"revocation_endpoint"`
	ResponseTypesSupported                     []string `json:"response_types_supported"`
	GrantTypesSupported                        []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                            []string `json:"scopes_supported"`
	AuthorizationResponseISSParameterSupported bool     `json:"authorization_response_iss_parameter_supported"`
	ResourceIndicatorsSupported                bool     `json:"resource_indicators_supported"`
	// A client may name itself by its metadata document's address
	// (mcp_oauth_client_metadata_document.go).
	ClientIDMetadataDocumentSupported bool `json:"client_id_metadata_document_supported"`
}

// publicServerOrigin is the server's address as a request sees it: the
// written address while it is the server's (server_tunnel_start_address.go),
// else the chosen one for a request made at it (a tunnel's, the
// administrator's own), else the address the request was made to, https as
// a proxy in front ended it (request_scheme.go) — so agents and apps
// connected here keep working while a tunnel opens and closes.
func publicServerOrigin(request *http.Request) (string, error) {
	if written := standingStartAddress(); written != "" {
		return written, nil
	}
	if chosen := requestChosenOrigin(request); chosen != "" {
		return chosen, nil
	}
	hostURL, err := url.Parse("http://" + request.Host)
	if err != nil || hostURL.Host == "" || hostURL.User != nil || hostURL.Path != "" {
		return "", errors.New(errorCodeMCPOAuthRequestInvalid)
	}
	return requestScheme(request) + "://" + hostURL.Host, nil
}

// requestArrivalOrigin is the address the request came in on: the written
// public address or a running tunnel's when the request names that host,
// else as the request sees it.
func requestArrivalOrigin(request *http.Request) (string, error) {
	public, err := publicServerOrigin(request)
	if err != nil {
		return "", err
	}
	if parsed, err := url.Parse(public); err == nil && strings.EqualFold(parsed.Host, request.Host) {
		return public, nil
	}
	if chosen := requestChosenOrigin(request); chosen != "" {
		return chosen, nil
	}
	hostURL, err := url.Parse("http://" + request.Host)
	if err != nil || hostURL.Host == "" || hostURL.User != nil || hostURL.Path != "" {
		return public, nil
	}
	return requestScheme(request) + "://" + hostURL.Host, nil
}

// framingOrigins are the addresses Leafen's pages frame the server's own
// sandboxed frames from: the public address and the one the request came in
// on, which differ when the server is opened at another of its addresses —
// 127.0.0.1 for a public localhost, a LAN address (2026-10-16: an app's
// frame showed only the browser's refusal there). A proxy that rewrites the
// host still finds the public address listed. The packaged client's page
// (packagedClientOrigins) frames them too: the desktop app's screen is
// wails://localhost, and its every app frame stayed a blank white page
// (2026-11-05).
func framingOrigins(request *http.Request) ([]string, error) {
	public, err := publicServerOrigin(request)
	if err != nil {
		return nil, err
	}
	origins := []string{public}
	if arrival, err := requestArrivalOrigin(request); err == nil && arrival != public {
		origins = append(origins, arrival)
	}
	return append(origins, packagedClientOrigins...), nil
}

// outwardServerOrigin is the address a person takes elsewhere — a link, a code
// an agent connects with, an address to register, each in the answer to their own
// signed-in request: the written address, else a running tunnel's, else as the
// request sees it (tunnel decision of 2026-10-13, 5). A mail, read by someone
// else than who asked for it, takes mailServerOrigin.
func outwardServerOrigin(request *http.Request) (string, error) {
	if origin := configuredPublicOrigin(); origin != "" {
		return origin, nil
	}
	return publicServerOrigin(request)
}

func mcpOAuthResource(request *http.Request) (string, error) {
	origin, err := answeredAtOrigin(request)
	if err != nil {
		return "", err
	}
	return origin + "/mcp", nil
}

// mcpEndpointResource is the OAuth resource of the MCP endpoint a request
// reached: the Notion-compatible /mcp (and its /sse) or the Leafen-native
// endpoint, each its own resource, as clients check the metadata's resource
// against the address they were given.
func mcpEndpointResource(request *http.Request) (string, error) {
	origin, err := answeredAtOrigin(request)
	if err != nil {
		return "", err
	}
	if request.URL.Path == nativeMCPPath || request.URL.Path == mcpProtectedResourceMetadataPath+nativeMCPPath {
		return origin + nativeMCPPath, nil
	}
	return origin + "/mcp", nil
}

const mcpProtectedResourceMetadataPath = "/.well-known/oauth-protected-resource"

func mcpProtectedResourceMetadataURL(request *http.Request) (string, error) {
	origin, err := answeredAtOrigin(request)
	if err != nil {
		return "", err
	}
	resource, err := mcpEndpointResource(request)
	if err != nil {
		return "", err
	}
	return origin + mcpProtectedResourceMetadataPath + strings.TrimPrefix(resource, origin), nil
}

func serveMCPProtectedResourceMetadata(event *core.RequestEvent) error {
	origin, err := answeredAtOrigin(event.Request)
	resource, resourceErr := mcpEndpointResource(event.Request)
	if err != nil || resourceErr != nil {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request", "invalid public server URL")
	}
	event.Response.Header().Set("Access-Control-Allow-Origin", "*")
	event.Response.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	event.Response.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if event.Request.Method == http.MethodOptions {
		return event.NoContent(http.StatusNoContent)
	}
	return event.JSON(http.StatusOK, map[string]any{
		"resource":                 resource,
		"authorization_servers":    []string{origin},
		"scopes_supported":         []string{mcpOAuthScope},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Leafen MCP",
	})
}

func serveMCPAuthorizationServerMetadata(event *core.RequestEvent) error {
	origin, err := answeredAtOrigin(event.Request)
	if err != nil {
		return oauthProtocolError(event, http.StatusBadRequest, "invalid_request", "invalid public server URL")
	}
	event.Response.Header().Set("Access-Control-Allow-Origin", "*")
	event.Response.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	if event.Request.Method == http.MethodOptions {
		return event.NoContent(http.StatusNoContent)
	}
	return event.JSON(http.StatusOK, mcpOAuthAuthorizationServerMetadata{
		Issuer: origin, AuthorizationEndpoint: origin + "/oauth/authorize",
		TokenEndpoint: origin + "/oauth/token", RegistrationEndpoint: origin + "/oauth/register",
		RevocationEndpoint: origin + "/oauth/revoke", ResponseTypesSupported: []string{"code"},
		GrantTypesSupported:           []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported: []string{"S256"}, TokenEndpointAuthMethodsSupported: []string{"none"},
		ScopesSupported: []string{mcpOAuthScope, "read", "content", "structure", "admin"}, AuthorizationResponseISSParameterSupported: true,
		ResourceIndicatorsSupported: true, ClientIDMetadataDocumentSupported: true,
	})
}

// nativeOAuthResource is the native API as an OAuth resource: an app asks
// for it with a key's scope, and its token then works as that key.
func nativeOAuthResource(request *http.Request) (string, error) {
	origin, err := answeredAtOrigin(request)
	if err != nil {
		return "", err
	}
	return origin + apiRoot, nil
}

// mcpOAuthResourceKnown reports whether a resource is one of this server's
// MCP endpoints or its native API.
func mcpOAuthResourceKnown(request *http.Request, resource string) bool {
	mcpResource, err := mcpOAuthResource(request)
	if err == nil && (resource == mcpResource || resource == strings.TrimSuffix(mcpResource, "/mcp")+nativeMCPPath) {
		return true
	}
	// A request made at the running tunnel's or the chosen address is this
	// server's too, as when a packaged app's window answers one
	// (local_owner_tunnel.go).
	for _, origin := range []string{runningTunnelOrigin(), chosenServerOrigin()} {
		if origin != "" && (resource == origin+mcpRoutePath || resource == origin+nativeMCPPath || resource == origin+apiRoot) {
			return true
		}
	}
	// One at an earlier address that still forwards here is this server's too.
	if moved := movedResource(resource); moved != "" && mcpOAuthResourceKnown(request, moved) {
		return true
	}
	native, err := nativeOAuthResource(request)
	return err == nil && resource == native
}

// grantedOAuthScope is the scope an authorization request for a resource is
// granted. MCP grants "mcp" whatever list the client sends, none included:
// clients pick it from WWW-Authenticate, the resource's metadata or this
// server's metadata (which also names the native API's key scopes), and each
// way must end alike — the server may narrow a request (RFC 6749 3.3) and the
// token response then names the scope granted. The native API grants the one
// key scope asked for.
func grantedOAuthScope(resource, requested string) (string, bool) {
	if strings.HasSuffix(resource, "/mcp") {
		for _, scope := range strings.Fields(requested) {
			if !oauthScopeTokenPattern.MatchString(scope) {
				return "", false
			}
		}
		return mcpOAuthScope, true
	}
	return requested, oauthScopeFitsResource(resource, requested)
}

// oauthScopeTokenPattern is one scope token's characters (RFC 6749 3.3).
var oauthScopeTokenPattern = regexp.MustCompile(`^[\x21\x23-\x5B\x5D-\x7E]+$`)

// oauthScopeFitsResource reports whether a granted scope belongs with a
// resource: MCP takes "mcp", the native API a key's scope.
func oauthScopeFitsResource(resource, scope string) bool {
	switch {
	case strings.HasSuffix(resource, "/mcp"):
		return scope == mcpOAuthScope
	case strings.HasSuffix(resource, apiRoot):
		_, known := parseUserKeyScope(scope)
		return known
	}
	return false
}

// oauthResourceIsNative reports whether a stored resource is the native API.
func oauthResourceIsNative(resource string) bool {
	return strings.HasSuffix(resource, apiRoot)
}
