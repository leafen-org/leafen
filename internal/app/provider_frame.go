package app

import (
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The Mac and iPhone apps' own page has a wails:// address, from which
// a frame sends no Referer, and YouTube answers one without it with "error
// 153" (seen 2026-11-05). This page of the server frames a known provider's
// player for those apps: the player sees the server's address, as it sees the
// address of a page in a browser.
const providerFramePath = "/provider-frame"

// providerFrames are the players the page frames, as the client's
// streamingVideoEmbed names them; it frames nothing else.
var providerFrames = map[string]string{
	"www.youtube.com":          "/embed/",
	"youtube.com":              "/embed/",
	"m.youtube.com":            "/embed/",
	"www.youtube-nocookie.com": "/embed/",
	"youtube-nocookie.com":     "/embed/",
	"player.vimeo.com":         "/video/",
	"www.loom.com":             "/embed/",
	"loom.com":                 "/embed/",
}

func registerProviderFrameRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+providerFramePath, serveProviderFrame)
}

// providerFrameSource is the player a request names, if it is one of those.
func providerFrameSource(raw string) (*url.URL, bool) {
	source, err := url.Parse(raw)
	if err != nil || source.Scheme != "https" || source.User != nil || source.Port() != "" {
		return nil, false
	}
	prefix, known := providerFrames[strings.ToLower(source.Hostname())]
	if !known || !strings.HasPrefix(source.EscapedPath(), prefix) {
		return nil, false
	}
	return source, true
}

func serveProviderFrame(event *core.RequestEvent) error {
	source, ok := providerFrameSource(event.Request.URL.Query().Get("src"))
	if !ok {
		return event.NotFoundError("", nil)
	}
	header := event.Response.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	header.Set("X-Content-Type-Options", "nosniff")
	// The server's SAMEORIGIN would refuse the app's page that frames it.
	header.Del("X-Frame-Options")
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-src https://"+source.Host+
		"; base-uri 'none'; form-action 'none'; frame-ancestors "+strings.Join(packagedClientOrigins, " "))
	event.Response.WriteHeader(http.StatusOK)
	_, err := event.Response.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>Leafen</title>` +
		`<style>html,body,iframe{display:block;width:100%;height:100%;margin:0;border:0;overflow:hidden;background:#000}</style></head>` +
		`<body><iframe src="` + html.EscapeString(source.String()) + `" title="Leafen" ` +
		`allow="accelerometer; autoplay; clipboard-write; encrypted-media; fullscreen; gyroscope; picture-in-picture; web-share" allowfullscreen>` +
		`</iframe></body></html>`))
	return err
}
