// The built-in Vault app (docs/app-platform.md 16): a password manager's
// server that Bitwarden's own apps — the browser extension, the phone and
// desktop apps — reach at the app's public endpoint, one vault for each
// person who prepared theirs in Leafen. The apps encrypt everything before
// it leaves them; this worker keeps what they send, signs them in with
// their master password's hash and hands back what they kept. Leafen, its
// administrators and agents never see the vault's contents.
//
// The server address a person gives their Bitwarden app is the endpoint
// with their organization after it:
// <leafen>/app-endpoints/vault/bw/<organization>, below which the apps add
// /api/… and /identity/….
package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"leafen.org/leafen/sdk/app/go/leafen"
)

// serverVersion is the Bitwarden server version the apps are told; they
// turn features on by it.
const serverVersion = "2025.6.0"

var organizationPattern = regexp.MustCompile(`^[a-z0-9]{1,40}$`)

func fail(message string) int32 { return leafen.Fail(message) }

// call is one request from a Bitwarden app.
type call struct {
	method, path string
	query        url.Values
	headers      map[string]string
	body         []byte
	store        *store
	// base is the server address the app was given.
	base string
}

// api answers a Bitwarden app's request to the public endpoint.
//
//go:wasmexport api
func api() int32 {
	request := leafen.EndpointRequest{}
	if err := leafen.InputJSON(&request); err != nil {
		return fail("the request isn't JSON")
	}
	_ = leafen.OutputJSON(serve(request))
	return 0
}

func serve(request leafen.EndpointRequest) leafen.EndpointAnswer {
	if request.Method == "OPTIONS" {
		return leafen.EndpointAnswer{Status: 204, Headers: map[string]string{"Allow": "GET, POST, PUT, DELETE, OPTIONS"}}
	}
	organization, rest, _ := strings.Cut(strings.TrimPrefix(request.Path, "/"), "/")
	if !organizationPattern.MatchString(organization) {
		return problem(404, "Give your Bitwarden app the server address Leafen's Vault app shows.")
	}
	if request.BodyFile != nil {
		return problem(413, "That request is too large.")
	}
	if page, ok := pageOf(request.Method, rest); ok {
		return page
	}
	body, err := base64.StdEncoding.DecodeString(request.Body)
	if err != nil {
		return problem(400, "The request couldn't be read.")
	}
	s, err := openStore("org:" + organization)
	if err != nil {
		return problem(404, "The Vault app isn't on in this organization.")
	}
	query, _ := url.ParseQuery(request.Query)
	c := &call{method: request.Method, path: "/" + strings.TrimSuffix(rest, "/"), query: query, headers: request.Headers, body: body, store: s,
		base: strings.TrimSuffix(request.Address, "/") + "/" + organization}
	answer, err := c.route()
	if err != nil {
		leafen.Log(c.method + " " + c.path + ": " + err.Error())
		return problem(500, "The vault couldn't answer just now; try again.")
	}
	return answer
}

// pageOf answers a request for the web vault's files — every path but the
// API's — with the package's file of that name, Leafen sending it as a page.
// Their names carry their content's hash, so they keep; the page itself
// is asked again each time.
func pageOf(method, rest string) (leafen.EndpointAnswer, bool) {
	if method != "GET" && method != "HEAD" {
		return leafen.EndpointAnswer{}, false
	}
	head, _, _ := strings.Cut(rest, "/")
	switch head {
	case "api", "identity", "notifications", "alive":
		return leafen.EndpointAnswer{}, false
	}
	name := strings.TrimSuffix(rest, "/")
	if name == "" {
		name = "index.html"
	}
	cache := "public, max-age=604800"
	if strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".json") {
		cache = "no-cache"
	}
	return leafen.EndpointAnswer{Status: 200, PackageFile: name, Headers: map[string]string{"Cache-Control": cache}}, true
}

// route sends a request to what answers it.
func (c *call) route() (leafen.EndpointAnswer, error) {
	switch c.method + " " + c.path {
	case "GET /api/config":
		return c.config(), nil
	case "GET /alive", "GET /api/alive":
		return reply(200, stamp(nowMs())), nil
	case "GET /api/version":
		return reply(200, serverVersion), nil
	case "POST /identity/accounts/prelogin", "POST /api/accounts/prelogin", "POST /identity/accounts/prelogin/password":
		return c.prelogin()
	case "POST /identity/accounts/register/send-verification-email":
		return c.verifyEmail()
	case "POST /identity/accounts/register/finish", "POST /identity/accounts/register", "POST /api/accounts/register":
		return c.register()
	case "POST /identity/connect/token":
		return c.token()
	case "GET /api/devices/knowndevice":
		return c.knownDevice()
	}
	if !strings.HasPrefix(c.path, "/api/") {
		return problem(404, "Not found."), nil
	}
	acct, ok, err := c.signedIn()
	if err != nil || !ok {
		return leafen.EndpointAnswer{Status: 401, Headers: jsonHeaders, Body: `{"message":"Log in again."}`}, err
	}
	return c.routeSignedIn(acct)
}

func (c *call) routeSignedIn(acct account) (leafen.EndpointAnswer, error) {
	parts := strings.Split(strings.TrimPrefix(c.path, "/api/"), "/")
	head, id, tail := parts[0], "", ""
	if len(parts) > 1 {
		id = parts[1]
	}
	if len(parts) > 2 {
		tail = strings.Join(parts[2:], "/")
	}
	switch head {
	case "sync":
		return c.sync(acct)
	case "accounts":
		return c.accountRoute(acct, id, tail)
	case "ciphers":
		return c.cipherRoute(acct, id, tail)
	case "folders":
		return c.folderRoute(acct, id, tail)
	case "devices":
		return c.deviceRoute(acct, id, tail)
	case "settings":
		if c.method == "GET" && id == "domains" {
			return reply(200, map[string]any{"equivalentDomains": []any{}, "globalEquivalentDomains": []any{}, "object": "domains"}), nil
		}
	case "organizations":
		return c.orgRoute(acct, id, tail)
	case "collections":
		if c.method == "GET" && id == "" {
			return c.allMyCollections(acct)
		}
	case "users":
		if c.method == "GET" && tail == "public-key" {
			return c.userKey(id)
		}
	case "plans":
		if c.method == "GET" {
			return list([]any{freePlan()}), nil
		}
	case "two-factor", "sends", "auth-requests", "emergency-access", "policies", "tasks":
		// What Leafen's vault doesn't have yet: nothing to list.
		if c.method == "GET" {
			return list([]any{}), nil
		}
	}
	return problem(404, "Not found."), nil
}

// config tells an app what the server is and where its parts are.
func (c *call) config() leafen.EndpointAnswer {
	origin := c.base
	return reply(200, map[string]any{
		"version": serverVersion, "gitHash": "", "object": "config",
		"server":        map[string]any{"name": "Leafen Vault", "url": "https://leafen.org"},
		"environment":   map[string]any{"vault": origin, "api": origin + "/api", "identity": origin + "/identity", "notifications": "", "sso": "", "cloudRegion": nil},
		"settings":      map[string]any{"disableUserRegistration": false},
		"featureStates": map[string]any{},
	})
}

var jsonHeaders = map[string]string{"Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store"}

// reply answers with a value as JSON.
func reply(status int, value any) leafen.EndpointAnswer {
	if value == nil {
		return leafen.EndpointAnswer{Status: status, Headers: jsonHeaders}
	}
	encoded, _ := json.Marshal(value)
	return leafen.EndpointAnswer{Status: status, Headers: jsonHeaders, Body: string(encoded)}
}

// list is Bitwarden's list answer.
func list(items []any) leafen.EndpointAnswer {
	return reply(200, map[string]any{"data": items, "object": "list", "continuationToken": nil})
}

// problem is Bitwarden's error answer, which its apps show as it says.
func problem(status int, message string) leafen.EndpointAnswer {
	return reply(status, map[string]any{
		"message": message, "validationErrors": map[string]any{"": []string{message}},
		"errorModel": map[string]any{"message": message, "object": "error"}, "object": "error",
	})
}

// decode reads a JSON body.
func (c *call) decode(target any) bool { return json.Unmarshal(c.body, target) == nil }

// signedIn checks the access token an app sends with each request.
func (c *call) signedIn() (account, bool, error) {
	token, ok := strings.CutPrefix(c.headers["authorization"], "Bearer ")
	if !ok {
		return account{}, false, nil
	}
	key, err := c.store.secret()
	if err != nil {
		return account{}, false, err
	}
	claims, ok := readToken(key, strings.TrimSpace(token), nowMs())
	if !ok {
		return account{}, false, nil
	}
	rowID, ok := rowOf(claims.Subject)
	if !ok {
		return account{}, false, nil
	}
	acct, err := c.store.accountByRow(rowID)
	if errors.Is(err, errMissing) {
		return account{}, false, nil
	}
	if err != nil {
		return account{}, false, err
	}
	// A new master password, a sign-out everywhere or a closed vault ends
	// every token made before.
	if acct.Status != statusActive || acct.Stamp != claims.Stamp {
		return account{}, false, nil
	}
	// So does losing the app: a token that lives on would keep bringing a
	// shared vault's new items to someone who left.
	allowed, err := c.mayUse(acct)
	if err != nil {
		return account{}, false, err
	}
	return acct, allowed, nil
}

func main() {}
