//go:build !leafen_noapps

package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"path"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An installed app's own picture, where Leafen lists it (2026-10-16: Server ›
// Apps showed the name's first letter for an app with ui/icon.svg). An <img>
// carries no sign-in, so the address carries a value signed for the install
// and its package: a new version is a new address, and the picture serves
// whatever the install's state, like the name beside it.

func registerAppIconRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appIconPath, appIconRoute)
}

func appIconToken(app core.App, install storedAppInstall) (string, error) {
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("leafen-app-icon\n" + install.ID + "\n" + install.PackageHash))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:18]), nil
}

// appIconKinds are the pictures an app's icon may be, by its file's
// extension: PNG, JPEG, WebP or SVG (docs/apps.md). Another file is no icon,
// and Leafen draws the name's first letter instead.
var appIconKinds = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".svg": "image/svg+xml"}

// appIconKind is the picture type an icon file names, or "".
func appIconKind(file string) string {
	return appIconKinds[strings.ToLower(path.Ext(file))]
}

// appInlineIconMaxBytes bounds an icon sent inside a listing or a review as
// a data address.
const appInlineIconMaxBytes = 64 << 10

// appIconDataURL is an icon file's picture as a data address, for a list or
// a review that shows an app nothing serves the files of yet, or "" for a
// file that is no icon, empty or over limit bytes.
func appIconDataURL(file string, data []byte, limit int) string {
	kind := appIconKind(file)
	if kind == "" || len(data) == 0 || len(data) > limit {
		return ""
	}
	return "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// appPackageIcon is a kept package's icon as a data address, for its review
// before it is installed, or "" (the review draws the name's first letter).
func appPackageIcon(app core.App, packageHash string, manifest appManifest) string {
	if appIconKind(manifest.Icon) == "" {
		return ""
	}
	if file, err := findAppPackageFile(app, packageHash, manifest.Icon); err != nil || file.Size > appInlineIconMaxBytes {
		return ""
	}
	data, err := loadAppPackageFile(app, packageHash, manifest.Icon)
	if err != nil {
		return ""
	}
	return appIconDataURL(manifest.Icon, data, appInlineIconMaxBytes)
}

// appIconURL is the picture's address when the app names a PNG, JPEG, WebP or
// SVG icon, else nothing (the list draws its first letter).
func appIconURL(app core.App, install storedAppInstall, manifest appManifest) string {
	if appIconKind(manifest.Icon) == "" {
		return ""
	}
	token, err := appIconToken(app, install)
	if err != nil {
		return ""
	}
	return apiRoot + strings.Replace(appIconPath, "{install}", install.ID, 1) + "?t=" + token
}

func appIconRoute(event *core.RequestEvent) error {
	installs, err := loadAppInstalls(event.App, "[[id]] = {:id}", dbx.Params{"id": event.Request.PathValue("install")})
	if err != nil || len(installs) != 1 {
		return appHomeScreenNotFound(event)
	}
	token, err := appIconToken(event.App, installs[0])
	if err != nil || !hmac.Equal([]byte(token), []byte(event.Request.URL.Query().Get("t"))) {
		return appHomeScreenNotFound(event)
	}
	_, manifest, err := loadAppPackage(event.App, installs[0].PackageHash)
	if err != nil {
		return appHomeScreenNotFound(event)
	}
	data, kind := appHomeScreenIcon(event.App, installs[0], manifest)
	header := event.Response.Header()
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	header.Set("X-Content-Type-Options", "nosniff")
	// The address names the package, so its picture never changes.
	header.Set("Cache-Control", "private, max-age=86400")
	return event.Blob(http.StatusOK, kind, data)
}
