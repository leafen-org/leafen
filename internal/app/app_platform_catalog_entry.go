//go:build !leafen_noapps

package app

import (
	"encoding/base64"
	"strings"
)

// A catalog's app entry (docs/apps.md "Distribution"): what `leafen app
// publish` writes for a package and the index signs, what a server checks
// before it trusts the index, and the icon a list shows for an app that
// isn't installed yet.

type appCatalogApp struct {
	ID        string            `json:"id"`
	Slug      string            `json:"slug"`
	Name      map[string]string `json:"name"`
	Summary   map[string]string `json:"summary,omitempty"`
	Developer string            `json:"developer"`
	Category  string            `json:"category,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Version   string            `json:"version"`
	Reviewed  bool              `json:"reviewed,omitzero"`
	Score     float64           `json:"score,omitempty"`
	Downloads int64             `json:"downloads,omitzero"`
	Added     string            `json:"added,omitempty"`
	Updated   string            `json:"updated,omitempty"`
	Languages []string          `json:"languages,omitempty"`
	Paid      bool              `json:"paid,omitzero"`
	Network   string            `json:"network,omitempty"`
	Risk      string            `json:"risk,omitempty"`
	Example   bool              `json:"example,omitzero"`
	// Icon is the package's icon as a data address (appCatalogEntryIcon).
	Icon    string            `json:"icon,omitempty"`
	Package appCatalogPackage `json:"package"`
}

type appCatalogPackage struct {
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	PackageHash string `json:"packageHash"`
	Size        int64  `json:"size"`
}

// appCatalogEntryProblem is what an index's app lacks, or "". An icon Leafen
// can't draw is no problem: the list draws the name's first letter.
func appCatalogEntryProblem(entry appCatalogApp) string {
	switch {
	case !appKeyLike(entry.ID):
		return "id is empty, over 128 characters or holds a space or slash"
	case entry.Slug == "":
		return "slug is empty"
	case entry.Version == "":
		return "version is empty"
	case len(entry.Name) == 0:
		return "name is empty"
	case entry.Package.URL == "":
		return "package.url is empty"
	case !appDigestPattern.MatchString(entry.Package.SHA256):
		return "package.sha256 isn't a SHA-256 digest"
	case !appDigestPattern.MatchString(entry.Package.PackageHash):
		return "package.packageHash isn't a SHA-256 digest"
	}
	return ""
}

func appKeyLike(id string) bool {
	return id != "" && len(id) <= 128 && !strings.ContainsAny(id, " /\\\x00")
}

// appCatalogIconMaxBytes bounds the picture an entry carries, smaller than a
// built-in app's: an index holds up to 5000 apps in at most
// appCatalogIndexMaxBytes, and every list of the catalog carries them all.
const appCatalogIconMaxBytes = 16 << 10

// appCatalogEntryIcon is an entry's icon as the data address a list draws —
// a PNG, JPEG, WebP or SVG picture, base64, of at most appCatalogIconMaxBytes
// — or "" for none or one Leafen doesn't draw.
func appCatalogEntryIcon(icon string) string {
	kind, encoded, found := strings.Cut(strings.TrimPrefix(icon, "data:"), ";base64,")
	if !found || !strings.HasPrefix(icon, "data:") || len(encoded) > base64.StdEncoding.EncodedLen(appCatalogIconMaxBytes) {
		return ""
	}
	known := false
	for _, allowed := range appIconKinds {
		known = known || kind == allowed
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if !known || err != nil || len(data) == 0 || len(data) > appCatalogIconMaxBytes {
		return ""
	}
	return "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(data)
}
