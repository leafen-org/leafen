//go:build !leafen_noapps

package app

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"leafen.org/leafen/web"
)

// Home screen apps (docs/app-platform.md 17): while a person has an app's
// screen open, Leafen names that app's web app manifest instead of its own,
// so the browser's "install" puts the app on the home screen — its name,
// icon and color, starting and staying within /apps/<slug>/, run by
// Leafen's own service worker and sign-in. A browser fetches a manifest and
// its icons without a sign-in, so their addresses carry a value signed for
// the install; administrators can turn home screen apps off.

// appHomeScreenThemeColor is the color an app that names none takes on the
// home screen: Leafen's own manifest's (web/public/manifest.webmanifest), so
// an app's screen opens on the ground Leafen's does. The small manifest is
// read each time, so a development build reading the web build from disk
// (web/dist_devweb.go) takes a changed one at once.
func appHomeScreenThemeColor() string {
	var manifest struct {
		ThemeColor string `json:"theme_color"`
	}
	if data, err := fs.ReadFile(web.Dist(), "manifest.webmanifest"); err == nil && json.Unmarshal(data, &manifest) == nil && manifest.ThemeColor != "" {
		return manifest.ThemeColor
	}
	return "#ffffff"
}

func registerAppHomeScreenRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appHomeScreenManifestPath, appHomeScreenManifestRoute)
	event.Router.GET(apiRoot+appHomeScreenIconPath, appHomeScreenIconRoute)
}

// appHomeScreenToken is the value an install's manifest and icon addresses carry.
func appHomeScreenToken(app core.App, installID string) (string, error) {
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("leafen-app-home-screen\n" + installID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:18]), nil
}

// appHomeScreenManifestURL is the manifest's address for an open app, or
// nothing while home screen apps are off.
func appHomeScreenManifestURL(app core.App, install storedAppInstall) (string, error) {
	settings, err := loadAppPlatformSettings(app)
	if err != nil || settings.HomeScreenAppsOff {
		return "", err
	}
	token, err := appHomeScreenToken(app, install.ID)
	if err != nil {
		return "", err
	}
	return apiRoot + strings.Replace(appHomeScreenManifestPath, "{slug}", install.Slug, 1) + "?t=" + token, nil
}

// appHomeScreenInstall is the running app a manifest or icon address names,
// when its value is right and home screen apps are on.
func appHomeScreenInstall(event *core.RequestEvent) (storedAppInstall, appManifest, string, bool) {
	installs, err := loadAppInstalls(event.App, "[[slug]] = {:slug} AND [[status]] = 'active' AND [[removed_at]] = 0",
		dbx.Params{"slug": event.Request.PathValue("slug")})
	if err != nil || len(installs) != 1 {
		return storedAppInstall{}, appManifest{}, "", false
	}
	settings, err := loadAppPlatformSettings(event.App)
	if err != nil || settings.HomeScreenAppsOff {
		return storedAppInstall{}, appManifest{}, "", false
	}
	token, err := appHomeScreenToken(event.App, installs[0].ID)
	if err != nil || !hmac.Equal([]byte(token), []byte(event.Request.URL.Query().Get("t"))) {
		return storedAppInstall{}, appManifest{}, "", false
	}
	_, manifest, err := loadAppPackage(event.App, installs[0].PackageHash)
	if err != nil {
		return storedAppInstall{}, appManifest{}, "", false
	}
	return installs[0], manifest, token, true
}

func appHomeScreenNotFound(event *core.RequestEvent) error {
	return apiError(event, http.StatusNotFound, errorCodeAppInstallNotFound, "App not found.")
}

func appHomeScreenManifestRoute(event *core.RequestEvent) error {
	install, manifest, token, ok := appHomeScreenInstall(event)
	if !ok {
		return appHomeScreenNotFound(event)
	}
	language := requestTextLanguageIn(event.App, event.Request)
	name := appLocalText(manifest.Name, language)
	shortName, color := name, appHomeScreenThemeColor()
	if manifest.PWA != nil && manifest.PWA.ShortName != "" {
		shortName = manifest.PWA.ShortName
	} else if utf8.RuneCountInString(shortName) > 12 {
		shortName = string([]rune(shortName)[:12])
	}
	if manifest.PWA != nil && manifest.PWA.ThemeColor != "" {
		color = manifest.PWA.ThemeColor
	}
	scope := "/apps/" + install.Slug + "/"
	icon := map[string]string{"src": apiRoot + strings.Replace(appHomeScreenIconPath, "{slug}", install.Slug, 1) + "?t=" + token, "purpose": "any"}
	if data, kind := appHomeScreenIcon(event.App, install, manifest); kind == "image/svg+xml" {
		icon["type"], icon["sizes"] = kind, "any"
	} else if config, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		icon["type"], icon["sizes"] = kind, strconv.Itoa(config.Width)+"x"+strconv.Itoa(config.Height)
	} else {
		icon["type"], icon["sizes"] = kind, "any"
	}
	body, err := json.Marshal(map[string]any{
		"id": scope, "name": name, "short_name": shortName, "description": appLocalText(appTextOrEmpty(manifest.Description), language),
		"lang": language, "start_url": scope, "scope": scope, "display": "standalone",
		"theme_color": color, "background_color": color, "icons": []map[string]string{icon},
	})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-cache")
	return event.Blob(http.StatusOK, "application/manifest+json", body)
}

func appHomeScreenIconRoute(event *core.RequestEvent) error {
	install, manifest, _, ok := appHomeScreenInstall(event)
	if !ok {
		return appHomeScreenNotFound(event)
	}
	data, kind := appHomeScreenIcon(event.App, install, manifest)
	header := event.Response.Header()
	// An app's picture never runs as Leafen's page, even opened on its own.
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cache-Control", "no-cache")
	return event.Blob(http.StatusOK, kind, data)
}

// appHomeScreenIcon is the app's own picture when it is a PNG, JPEG, WebP
// or SVG file, else its first letter on its color.
func appHomeScreenIcon(app core.App, install storedAppInstall, manifest appManifest) ([]byte, string) {
	if kind := appIconKind(manifest.Icon); kind != "" {
		if data, err := loadAppPackageFile(app, install.PackageHash, manifest.Icon); err == nil && len(data) > 0 {
			return data, kind
		}
	}
	color := appHomeScreenThemeColor()
	if manifest.PWA != nil && manifest.PWA.ThemeColor != "" {
		color = manifest.PWA.ThemeColor
	}
	initial := "?"
	if name := strings.TrimSpace(appDisplayName(manifest)); name != "" {
		first, _ := utf8.DecodeRuneInString(name)
		initial = strings.ToUpper(string(first))
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512"><rect width="512" height="512" rx="96" fill="` + color +
		`"/><text x="256" y="256" dy=".35em" text-anchor="middle" font-family="system-ui,sans-serif" font-size="280" font-weight="600" fill="#37352f">` +
		html.EscapeString(initial) + `</text></svg>`
	return []byte(svg), "image/svg+xml"
}

// appNameLooksLike is the name an app's would pass for — Leafen's own, or a
// built-in app's for an app that isn't it — so its review can say so.
func appNameLooksLike(manifest appManifest, trust string) string {
	if trust == appTrustBuiltin {
		return ""
	}
	fold := func(value string) string { return strings.ToLower(strings.Join(strings.Fields(value), "")) }
	for _, name := range manifest.Name {
		folded := fold(name)
		if strings.Contains(folded, "leafen") || strings.Contains(folded, "리펜") {
			return "Leafen"
		}
		for _, builtin := range loadAppBuiltins() {
			if builtin.Package.Manifest.ID == manifest.ID {
				continue
			}
			for _, other := range builtin.Package.Manifest.Name {
				if fold(other) != "" && fold(other) == folded {
					return appDisplayName(builtin.Package.Manifest)
				}
			}
		}
	}
	return ""
}
