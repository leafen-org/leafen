package app

import (
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/pocketbase/pocketbase/core"
)

// How a request reached the server, https or http — one rule for the
// addresses its answers carry (links, OAuth's metadata, an agent's answers:
// publicServerOrigin, requestArrivalOrigin) and for the addresses an
// administrator's browser is noted at (requestSchemeSeen). A proxy in front
// that ends https — a NAS's, Caddy, Nginx — hands the request on in plain
// http, so the connection alone said http and links led to
// http://host:port, which such a proxy refuses (2026-11-22: "The plain HTTP
// request was sent to HTTPS port" on a Synology). It is https when the
// connection is; when a proxy in the server's own network says so
// (X-Forwarded-Proto, Forwarded's proto) — one elsewhere could say anything;
// when the browser's Origin names https at the same host; or when the server
// has known that host over https before — an administrator's browser opened
// it so (leafen_server_administrator_addresses) — which an agent's request,
// with no Origin and often no header, goes by. A host used over http only,
// as a home network's address, stays http.

// httpsHosts are the hosts the server has been opened at over https.
var httpsHosts sync.Map

// requestScheme is how the request reached the server.
func requestScheme(request *http.Request) string {
	if request.TLS != nil || forwardedHTTPS(request) {
		return "https"
	}
	if origin, err := url.Parse(request.Header.Get("Origin")); err == nil && origin.Scheme == "https" && strings.EqualFold(origin.Host, request.Host) {
		return "https"
	}
	if _, seen := httpsHosts.Load(strings.ToLower(request.Host)); seen {
		return "https"
	}
	return "http"
}

// forwardedHTTPS is whether a proxy in the server's own network handed the
// request on from https.
func forwardedHTTPS(request *http.Request) bool {
	if !addressInNetwork(parseForwardedAddress(request.RemoteAddr)) {
		return false
	}
	if proto, _, _ := strings.Cut(request.Header.Get("X-Forwarded-Proto"), ","); strings.EqualFold(strings.TrimSpace(proto), "https") {
		return true
	}
	for _, value := range request.Header.Values("Forwarded") {
		element, _, _ := strings.Cut(value, ",")
		for _, pair := range strings.Split(element, ";") {
			name, given, found := strings.Cut(strings.TrimSpace(pair), "=")
			if found && strings.EqualFold(strings.TrimSpace(name), "proto") && strings.EqualFold(strings.Trim(strings.TrimSpace(given), `"`), "https") {
				return true
			}
		}
	}
	return false
}

// noteHTTPSOrigin keeps an origin's host as one the server is opened at over https.
func noteHTTPSOrigin(origin string) {
	if parsed, err := url.Parse(origin); err == nil && parsed.Scheme == "https" && parsed.Host != "" {
		httpsHosts.Store(strings.ToLower(parsed.Host), true)
	}
}

// loadHTTPSHosts reads the hosts administrators' browsers opened the server
// at over https, as the server starts.
func loadHTTPSHosts(app core.App) {
	origins := []string{}
	if err := app.DB().NewQuery(`SELECT [[origin]] FROM {{leafen_server_administrator_addresses}}`).Column(&origins); err != nil {
		return
	}
	for _, origin := range origins {
		noteHTTPSOrigin(origin)
	}
}
