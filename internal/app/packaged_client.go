package app

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// packagedClientOrigins are the origins of the packaged client's own page
// (Wails' asset server): a custom scheme on macOS and iOS, a reserved local
// host name on Windows and Android. A server answers that page as it
// answers its own; the page carries its sign-in as a bearer token, never a
// cookie, so another page using these origins gains nothing without one.
var packagedClientOrigins = []string{"wails://localhost", "http://wails.localhost", "https://wails.localhost"}

// frameAncestorsDirective names the pages that may frame the server's own
// sandboxed frames (an app's screen, a document app, an HTML file): the
// server's addresses (framingOrigins) and the packaged client's page, whose
// wails:// address the server's own addresses never match — the Mac app showed
// every app's screen blank (2026-11-05). Each origin is named once, however
// many of them the caller's list already holds (framingOrigins names the
// packaged client's too). The directive, not the server's SAMEORIGIN,
// decides, so the frames drop that header.
func frameAncestorsDirective(origins []string) string {
	all := make([]string, 0, len(origins)+len(packagedClientOrigins))
	for _, origin := range append(append([]string{}, origins...), packagedClientOrigins...) {
		if !slices.ContainsFunc(all, func(listed string) bool { return strings.EqualFold(listed, origin) }) {
			all = append(all, origin)
		}
	}
	return "frame-ancestors " + strings.Join(all, " ")
}

func isPackagedClientOrigin(origin string) bool {
	for _, packaged := range packagedClientOrigins {
		if strings.EqualFold(origin, packaged) {
			return true
		}
	}
	return false
}

// socketOriginAllowed is the origin a page's socket may come from: none (not
// a browser), the server's own — upstream Ygo's same-origin check — or one
// of also, the packaged client's page among them. Leafen asks it where its
// sockets come in, the Ygo servers taking any origin (AllowedOrigins "*"),
// so the fork doesn't carry an origin list beside the same-origin check.
func socketOriginAllowed(request *http.Request, also []string) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if parsed, err := url.Parse(origin); err == nil && strings.EqualFold(parsed.Host, request.Host) {
		return true
	}
	for _, allowed := range also {
		if strings.EqualFold(origin, allowed) {
			return true
		}
	}
	return false
}

// The response headers Leafen's client reads, which a page on another origin
// — the packaged client's — sees only when they are exposed.
var exposedClientHeaders = strings.Join([]string{
	authSessionTokenHeader, "Content-Disposition", "Content-Length", "Content-Range", "ETag",
}, ", ")

// registerClientHeaderExposure exposes those headers wherever the CORS
// middleware, which runs just before it, answered the request's origin.
func registerClientHeaderExposure(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id:       "leafenExposeClientHeaders",
		Priority: apis.DefaultCorsMiddlewarePriority + 1,
		Func: func(request *core.RequestEvent) error {
			if request.Request.Method != http.MethodOptions &&
				request.Response.Header().Get("Access-Control-Allow-Origin") != "" {
				request.Response.Header().Set("Access-Control-Expose-Headers", exposedClientHeaders)
			}
			return request.Next()
		},
	})
}
