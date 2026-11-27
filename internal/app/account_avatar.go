package app

import (
	"context"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// People's pictures: a person without a photo shows the first letter of
// their name on a circle, as in Notion, until they upload a photo or pick a
// line icon (the decision of 2026-11-09, which ended giving each new account
// an icon). An account's profile_avatar is what it shows — a photo,
// "avatar:<icon>", an emoji kept from before, or "" for the first letter.
// profile_icon keeps the icon an account was given before then; nothing
// shows it.

const (
	accountAvatarIconPrefix = "avatar:"
)

// avatarIconKnown holds every icon drawn, a retired one too: an account that
// has one keeps it, though it is no longer given out or offered.
var avatarIconKnown = func() map[string]bool {
	known := make(map[string]bool, len(avatarIconPaths))
	for name := range avatarIconPaths {
		known[name] = true
	}
	return known
}()

// accountAvatarIconName is the icon an avatar value shows, if it shows one.
func accountAvatarIconName(avatar string) (string, bool) {
	name, ok := strings.CutPrefix(avatar, accountAvatarIconPrefix)
	return name, ok && avatarIconKnown[name]
}

// validAccountAvatar refuses an icon that isn't one of the drawings.
func validAccountAvatar(avatar string) bool {
	if !strings.HasPrefix(avatar, accountAvatarIconPrefix) {
		return true
	}
	_, ok := accountAvatarIconName(avatar)
	return ok
}

func accountAvatarIconPath(icon string) string {
	return apiRoot + "/avatars/" + icon + ".svg"
}

// registerAccountAvatarRoutes serves each line icon as an image, for those
// who read people's avatar_url through the API: the glyph on a light circle,
// as a light page shows it.
func registerAccountAvatarRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/avatars/{file}", func(event *core.RequestEvent) error {
		name, svg := strings.CutSuffix(event.Request.PathValue("file"), ".svg")
		paths, known := avatarIconPaths[name]
		if !svg || !known {
			return event.NoContent(http.StatusNotFound)
		}
		var body strings.Builder
		body.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" width="96" height="96">` +
			`<circle cx="128" cy="128" r="127" fill="#ffffff" stroke="#ecebe9" stroke-width="2"/>` +
			`<g transform="translate(17.92 17.92) scale(0.86)" fill="#55534e">`)
		for _, d := range paths {
			body.WriteString(`<path d="` + d + `"/>`)
		}
		body.WriteString(`</g></svg>`)
		header := event.Response.Header()
		header.Set("Cache-Control", "public, max-age=86400")
		header.Set("Content-Security-Policy", "default-src 'none'")
		header.Set("X-Content-Type-Options", "nosniff")
		return event.Blob(http.StatusOK, "image/svg+xml", []byte(body.String()))
	})
}

// requestOriginContextKey carries the server's public address to work done
// for a request that has no request at hand (the MCP tools).
type requestOriginContextKey struct{}

func requestOrigin(request *http.Request) string {
	origin, err := publicServerOrigin(request)
	if err != nil {
		return ""
	}
	return origin
}

func requestOriginFromContext(ctx context.Context) string {
	origin, _ := ctx.Value(requestOriginContextKey{}).(string)
	return origin
}
