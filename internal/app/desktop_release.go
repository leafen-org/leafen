package app

import (
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The desktop app's own update list (decided 2026-10-17: the Mac app fetches
// a new version by itself and switches to it the next time it starts; the
// Windows zip only says a newer version is out, decided 2026-11-17, and the
// Microsoft Store updates its app). It is a list of its own beside the
// release notice, signed by the same release key, naming the Mac's files with
// their SHA-256; a file may come only from Leafen's GitHub releases (or, in
// development, from the address the lists themselves come from), so a stolen
// key alone can't hand the apps a file of its choosing.

const (
	desktopReleaseEnvelopeFormat = "leafen-desktop-release/1"
	desktopReleaseFile           = "desktop-v1.json"
	officialDesktopAssetPrefix   = "https://github.com/leafen-org/leafen/releases/download/"
)

// DesktopRelease is the newest desktop app and its files.
type DesktopRelease struct {
	Format   int            `json:"format"`
	IssuedAt string         `json:"issuedAt"`
	Version  string         `json:"version"`
	Security bool           `json:"security"`
	NotesURL string         `json:"notesUrl"`
	Assets   []DesktopAsset `json:"assets"`
}

// DesktopAsset is one system's file: the macOS app bundle zipped
// ("app-zip"). No Windows file is listed, as no Windows app installs one.
type DesktopAsset struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// DesktopReleaseListURL is where the desktop app reads its update list, or
// "" while no list address is set.
func DesktopReleaseListURL() string {
	base := releaseListURL()
	if base == "" {
		return ""
	}
	return base + "/" + desktopReleaseFile
}

// ReleaseVersion is this build's version.
func ReleaseVersion() string { return leafenReleaseVersion }

// NewerReleaseVersion says whether a version comes after another.
func NewerReleaseVersion(candidate, current string) bool {
	next, nextOK := parseReleaseVersion(candidate)
	now, nowOK := parseReleaseVersion(current)
	return nextOK && nowOK && compareReleaseVersions(next, now) > 0
}

// ReadDesktopRelease checks a fetched desktop update list: the release
// key's signature, every field, and files only from where files may come.
func ReadDesktopRelease(raw []byte, now time.Time) (DesktopRelease, error) {
	_, payload, err := verifyReleaseEnvelope(raw, desktopReleaseEnvelopeFormat, releaseNoticeKeys)
	if err != nil {
		return DesktopRelease{}, err
	}
	release := DesktopRelease{}
	if err := decodeReleaseList(payload, &release, &release.Format, &release.IssuedAt, now); err != nil {
		return DesktopRelease{}, err
	}
	if _, ok := parseReleaseVersion(release.Version); !ok || !officialReleasePage(release.NotesURL) || len(release.Assets) == 0 {
		return DesktopRelease{}, errReleaseManifestInvalid
	}
	base := releaseListURL()
	for _, asset := range release.Assets {
		sum, err := hex.DecodeString(asset.SHA256)
		fromRelease := strings.HasPrefix(asset.URL, officialDesktopAssetPrefix)
		fromLists := base != "" && base != officialReleaseListURL && strings.HasPrefix(asset.URL, base+"/")
		if err != nil || len(sum) != 32 || asset.Size <= 0 || (!fromRelease && !fromLists) ||
			asset.Kind != "app-zip" {
			return DesktopRelease{}, errReleaseManifestInvalid
		}
	}
	return release, nil
}

// releaseListTrusted is the time a list already trusted is read at: one this
// release carries, or one fetched and kept, whose issue time was judged when
// it came. A clock set back never makes it refused (2026-10-18: the sponsor
// credit in every page's start-up read panicked on a machine whose clock was
// a day behind the list this release carries).
var releaseListTrusted = time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)

// ReleaseListNow is the time a fetched list is judged at: this machine's
// clock, or the list host's when this machine's is behind. A machine whose
// clock was set back — one without a clock battery before it syncs — would
// otherwise refuse every new list as issued in the future.
func ReleaseListNow(local time.Time, response *http.Response) time.Time {
	if response != nil {
		if served, err := http.ParseTime(response.Header.Get("Date")); err == nil && served.After(local) {
			return served.UTC()
		}
	}
	return local
}

// DesktopReleaseFromDevelopmentList says whether the lists come from an
// address other than Leafen's own, where the systems' publisher checks
// (notarization) are left to the developer.
func DesktopReleaseFromDevelopmentList() bool {
	base := releaseListURL()
	return base != "" && base != officialReleaseListURL
}

// ServesOthers says whether this server is reached from beyond the computer
// it runs on — its remote access is on — so the desktop app that runs it keeps
// running when its window closes and opens at login; an error says the
// setting wasn't read, which is not "off".
func ServesOthers(app core.App) (bool, error) {
	settings, err := loadServerTunnelSettings(app)
	if err != nil {
		return false, err
	}
	return settings.Mode != "" && settings.Mode != serverTunnelOff, nil
}
