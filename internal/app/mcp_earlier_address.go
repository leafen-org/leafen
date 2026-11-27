package app

import (
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pocketbase/pocketbase/core"
)

// An agent connected at an earlier address goes on there while it forwards
// (2026-11-22, the user: "응 다 넣어"): most MCP clients drop the
// Authorization header on a redirect to another origin, or don't follow
// one, so the 308 every other request there gets cut every agent off the
// moment the address changed, leaving its person to connect it again. Its
// requests — the endpoints, OAuth's discovery and endpoints, the routes an
// agent's connection uses — are answered where they came, as an outside
// program's are, and what it was handed there (its token, the OAuth
// resource and issuer it checks) stays this server's while the address
// forwards. Its use there keeps the address forwarding, as any use does, up
// to a year from the change (server_address_forwarding.go); the agents
// screen says which connections still come through an earlier address,
// that they can move to the new one, and "connect again" only once the
// address they came through no longer reaches the server. A program calling
// the API with its own key goes on there the same way (keyProgramRequest).

// forwardServedInPlace says whether a request at an earlier address is
// answered there rather than redirected: an outside program's call
// (programServedInPlace), one in an agent's connection
// (mcpAgentRequestPath) or one a program makes with its own key
// (keyProgramRequest).
func forwardServedInPlace(request *http.Request) bool {
	path := request.URL.Path
	return programServedInPlace(path) || mcpAgentRequestPath(path) || keyProgramRequest(request)
}

// keyProgramRequest says whether a request is a program's with a key of its
// own (2026-11-22): any call to the Notion-compatible API, which only
// integrations make (its OAuth tokens look like a sign-in's, so its path
// tells), and a call to Leafen's API with a key handed to a program — a
// person's or an agent's key, an app's OAuth token, an app's delegation.
// Such a program keeps the address beside its key and loses the key on a
// redirect to another origin (Node's fetch, Python's requests, Go and curl
// all drop Authorization there). What a key is decides, not an Origin: a
// program in a browser loses it too, and a sign-in — a browser's, the
// desktop and phone apps' window — still gets the redirect, from which the
// apps learn the new address, and a page the handoff.
func keyProgramRequest(request *http.Request) bool {
	path := request.URL.Path
	if path == notionAPIRoot || strings.HasPrefix(path, notionAPIRoot+"/") {
		return true
	}
	if path != apiRoot && !strings.HasPrefix(path, apiRoot+"/") {
		return false
	}
	token := bearerToken(request)
	return strings.HasPrefix(token, userKeyPrefix) || strings.HasPrefix(token, mcpOAuthAccessPrefix) ||
		strings.HasPrefix(token, appDelegationPrefix)
}

// mcpAgentRequestPath says whether a request at path is made in an agent's
// connection, from the paths its routes are served at: the MCP endpoints
// (Notion's, its SSE and Leafen's own), OAuth discovery, the OAuth endpoints
// an agent connects through, the routes under the API's "/mcp/" (the
// connections, their consent) and the file uploads an agent is handed — not
// the outside services' (a person's settings, and the sign-in a service
// sends back to their browser).
func mcpAgentRequestPath(path string) bool {
	for _, endpoint := range []string{mcpRoutePath, mcpSSERoutePath, nativeMCPPath} {
		if path == endpoint || strings.HasPrefix(path, endpoint+"/") {
			return true
		}
	}
	return strings.HasPrefix(path, "/.well-known/oauth") || strings.HasPrefix(path, "/oauth/") ||
		strings.HasPrefix(path, apiRoot+"/mcp/") || strings.HasPrefix(path, strings.TrimSuffix(mcpFileUploadCapabilityPath, "{token}"))
}

// answeredAtOrigin is the address a request is answered at: the earlier
// address it came to while that forwards here, else the server's
// (publicServerOrigin) — as an agent there checks the OAuth resource and
// issuer against the address it was given, and a client metadata document
// says the address it is read at.
func answeredAtOrigin(request *http.Request) (string, error) {
	if earlier := EarlierAddressOf(request); earlier != "" {
		return earlier, nil
	}
	return publicServerOrigin(request)
}

// notionOAuthIssuedHere says whether a Notion OAuth access token's issuer
// and audience are this server as the request reaches it (answeredAtOrigin):
// alike, or across an earlier address that forwards (sameServerResource) —
// issued at the old address and used at the new one, or the other way round.
func notionOAuthIssuedHere(claims jwt.RegisteredClaims, origin string) bool {
	if !sameServerResource(claims.Issuer, origin) {
		return false
	}
	for _, audience := range claims.Audience {
		if sameServerResource(audience, origin+notionAPIRoot) {
			return true
		}
	}
	return false
}

// sameServerResource says whether the resource a token was granted for and
// the one a request reaches are the same endpoint of this server: alike, or
// alike once an earlier address that forwards is read as the address it
// forwards to — granted at the old address and used at the new one, or
// renewed for the new one (rotateMCPOAuthAccessToken) and used at the old.
func sameServerResource(granted, current string) bool {
	return granted == current || strings.EqualFold(resourceNow(granted), resourceNow(current))
}

// resourceNow is a resource as it is at the address its own forwards to,
// itself at any other.
func resourceNow(resource string) string {
	if moved := movedResource(resource); moved != "" {
		return moved
	}
	return resource
}

// mcpReachedVia is the address a connection's request came through, kept
// to tell whether its agent can still reach the server
// (noteMCPConnectionReach): the chosen address or a tunnel's, or an earlier
// address that forwards here.
func mcpReachedVia(request *http.Request) string {
	if chosen := requestChosenOrigin(request); chosen != "" {
		return chosen
	}
	return EarlierAddressOf(request)
}

// notifyAgentsCutOff tells the people whose agents came through the
// previous address to connect them again, only when that address doesn't
// forward here: one that forwards keeps serving them. The forward is read
// once the change that started this is written (noteCanonicalChange holds
// forwardingMu until then).
func notifyAgentsCutOff(app core.App, previous, address string) {
	forwardingMu.Lock()
	forwards := forwardedOrigin(previous) != ""
	forwardingMu.Unlock()
	if !forwards {
		notifyAgentReconnect(app, previous, address)
	}
}
