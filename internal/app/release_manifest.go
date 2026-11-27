package app

import (
	"bytes"
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/image/webp"
	"golang.org/x/text/width"
)

// Once a day the server asks whether two signed static lists changed, each from a
// file named by its format (release-v1.json, sponsors-v1.json) under one
// address: the release notice — the latest version and whether it is a
// security release — and the sponsors Leafen thanks. A later format is
// published under its own name beside the earlier ones, so a server keeps
// reading the format it knows; each list refuses any field it doesn't know
// (2026-10-17: one combined list with one name would have left every
// installed server without notices the day a field was added). Each list has
// its own key, trusted for that list only: the key that signs the sponsor list
// each month can't make an update notice. Nothing about the instance is sent.
// A list counts only when its key signed it and every field passes the checks
// here; otherwise the release keeps the lists bundled with it.

const (
	releaseNoticeEnvelopeFormat = "leafen-release-notice/1"
	sponsorListEnvelopeFormat   = "leafen-sponsor-list/1"
	// The files a server of this format reads, under the list address.
	releaseNoticeFile = "release-v1.json"
	sponsorListFile   = "sponsors-v1.json"
)

// The keys each list may be signed with are a build's own: development
// builds trust the development keys (release_keys_development.go), release
// builds only the production keys (release_keys_production.go, checked in
// release_keys.go).

// officialReleasePages are where an update notice may send an administrator:
// the release notes and upgrade guide are Leafen's own, so a stolen key can't
// point every server at a download of its choosing.
var officialReleasePages = []string{"https://github.com/leafen-org/leafen/releases"}

var (
	//go:embed release_notice_bundled.json
	bundledReleaseNotice []byte
	//go:embed sponsor_list_bundled.json
	bundledSponsorList []byte
)

const (
	maxReleaseManifestBytes     = 1 << 20
	maxReleaseManifestSponsors  = 200
	maxReleaseSponsorLogoBytes  = 32 << 10
	maxReleaseSponsorLogoPixels = 256
	minReleaseSponsorLogoPixels = 16
	maxReleaseSponsorNameRunes  = 60
	// A list's shares are whole basis points out of releaseShareWhole, so a
	// list adds up to exactly one: no sponsor past a third, Leafen's own line
	// at least 5% (revenue decision of 2026-10-17, 3). A sponsor of a
	// hundredth of a percent still shows.
	releaseShareWhole       = 10_000
	releaseSponsorMostShare = releaseShareWhole / 3
	releaseHouseLeastShare  = releaseShareWhole / 20
	// A slogan is one line the sidebar's second line can show: about thirty
	// characters of a script whose characters are wide (Korean, Chinese,
	// Japanese), sixty of one whose are narrow (decision of 2026-10-20).
	maxReleaseSloganWidth     = 60
	maxReleaseSloganLanguages = 80
)

var (
	errReleaseManifestInvalid   = errors.New("LEAFEN_RELEASE_MANIFEST_INVALID")
	errReleaseManifestSignature = errors.New("LEAFEN_RELEASE_MANIFEST_SIGNATURE")
	releaseSponsorIDPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	releaseLanguagePattern      = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)
	// A slogan carries no link or address: a scheme, a "www.", a host's dot
	// before letters, or an "@".
	releaseSloganAddress = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://|www\.|[\p{L}\p{N}-]\.[a-z]{2,}\b|@`)
)

type releaseManifestEnvelope struct {
	Format    string `json:"format"`
	KeyID     string `json:"keyId"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// releaseNotice is the release notice list.
type releaseNotice struct {
	Format   int                   `json:"format"`
	IssuedAt string                `json:"issuedAt"`
	Latest   releaseManifestLatest `json:"latest"`
}

type releaseManifestLatest struct {
	Version    string `json:"version"`
	Security   bool   `json:"security"`
	NotesURL   string `json:"notesUrl"`
	UpgradeURL string `json:"upgradeUrl"`
}

// sponsorList is the sponsor list. It carries each sponsor's share of the
// showings, worked out where the list is made, and no amount: the list is a
// public file, and neither what a sponsor pays nor the month's total is
// published (revenue decision, 2026-10-19 change of 3).
type sponsorList struct {
	Format   int                      `json:"format"`
	IssuedAt string                   `json:"issuedAt"`
	House    releaseManifestHouse     `json:"house"`
	Sponsors []releaseManifestSponsor `json:"sponsors"`
}

// releaseManifestHouse is Leafen's own credit slot, linking to the page that
// thanks the people backing Leafen, with its share.
type releaseManifestHouse struct {
	URL   string `json:"url"`
	Share int    `json:"share"`
}

// A sponsor's credit links to the sponsor through Leafen's site
// (sponsor_credit.go), so the list names no address of the sponsor's.
type releaseManifestSponsor struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	Logo   string                 `json:"logo"`
	Share  int                    `json:"share"`
	Slogan *releaseManifestSlogan `json:"slogan,omitempty"`
}

// releaseManifestSlogan is a sponsor's one-line slogan in the language it was
// written in and the languages it was translated to (decision of 2026-10-20).
type releaseManifestSlogan struct {
	Language string            `json:"language"`
	Text     map[string]string `json:"text"`
}

// verifyReleaseEnvelope checks a fetched envelope of a format against that
// list's keys, returning the key that signed it and the signed payload.
func verifyReleaseEnvelope(raw []byte, format string, keys map[string]string) (string, []byte, error) {
	envelope := releaseManifestEnvelope{}
	if len(raw) > maxReleaseManifestBytes || json.Unmarshal(raw, &envelope) != nil || envelope.Format != format {
		return "", nil, errReleaseManifestInvalid
	}
	encodedKey, trusted := keys[envelope.KeyID]
	publicKey, keyErr := base64.RawURLEncoding.DecodeString(encodedKey)
	payload, payloadErr := base64.StdEncoding.DecodeString(envelope.Payload)
	signature, signatureErr := base64.StdEncoding.DecodeString(envelope.Signature)
	if !trusted || keyErr != nil || len(publicKey) != ed25519.PublicKeySize || payloadErr != nil ||
		signatureErr != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature) {
		return "", nil, errReleaseManifestSignature
	}
	return envelope.KeyID, payload, nil
}

// decodeReleaseList reads a list's payload, refusing a field it doesn't know,
// a format other than 1 and an issue time in the future.
func decodeReleaseList(payload []byte, target any, format *int, issuedAt *string, now time.Time) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || *format != 1 {
		return errReleaseManifestInvalid
	}
	issued, err := time.Parse(time.RFC3339, *issuedAt)
	if err != nil || issued.After(now.Add(24*time.Hour)) {
		return errReleaseManifestInvalid
	}
	return nil
}

func parseReleaseNotice(payload []byte, now time.Time) (releaseNotice, error) {
	notice := releaseNotice{}
	if err := decodeReleaseList(payload, &notice, &notice.Format, &notice.IssuedAt, now); err != nil {
		return releaseNotice{}, err
	}
	if _, ok := parseReleaseVersion(notice.Latest.Version); !ok ||
		!officialReleasePage(notice.Latest.NotesURL) || !officialReleasePage(notice.Latest.UpgradeURL) {
		return releaseNotice{}, errReleaseManifestInvalid
	}
	return notice, nil
}

func parseSponsorList(payload []byte, now time.Time) (sponsorList, error) {
	list := sponsorList{}
	if err := decodeReleaseList(payload, &list, &list.Format, &list.IssuedAt, now); err != nil {
		return sponsorList{}, err
	}
	if !validReleaseLink(list.House.URL) || list.House.Share < releaseHouseLeastShare ||
		len(list.Sponsors) > maxReleaseManifestSponsors {
		return sponsorList{}, errReleaseManifestInvalid
	}
	seen := map[string]bool{}
	total := list.House.Share
	for _, sponsor := range list.Sponsors {
		name := []rune(sponsor.Name)
		if !releaseSponsorIDPattern.MatchString(sponsor.ID) || seen[sponsor.ID] || len(name) == 0 ||
			len(name) > maxReleaseSponsorNameRunes || strings.TrimSpace(sponsor.Name) != sponsor.Name ||
			strings.IndexFunc(sponsor.Name, unicode.IsControl) >= 0 ||
			!validReleaseSponsorLogo(sponsor.Logo) || sponsor.Share < 1 || sponsor.Share > releaseSponsorMostShare ||
			sponsor.Slogan != nil && !validReleaseSlogan(*sponsor.Slogan) {
			return sponsorList{}, errReleaseManifestInvalid
		}
		seen[sponsor.ID] = true
		total += sponsor.Share
	}
	if total != releaseShareWhole {
		return sponsorList{}, errReleaseManifestInvalid
	}
	if list.Sponsors == nil {
		list.Sponsors = []releaseManifestSponsor{}
	}
	return list, nil
}

// bundledReleaseNoticePayload and bundledSponsorListPayload are the lists
// this release shipped with.
func bundledReleaseNoticePayload() releaseNotice {
	notice, err := parseReleaseNotice(bundledReleaseNotice, releaseListTrusted)
	if err != nil {
		panic("bundled release notice is invalid")
	}
	return notice
}

func bundledSponsorListPayload() sponsorList {
	list, err := parseSponsorList(bundledSponsorList, releaseListTrusted)
	if err != nil {
		panic("bundled sponsor list is invalid")
	}
	return list
}

// officialReleasePage reports a link to one of Leafen's own release pages.
func officialReleasePage(raw string) bool {
	if !validReleaseLink(raw) {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	for _, page := range officialReleasePages {
		base, err := url.Parse(page)
		if err == nil && parsed.Scheme == base.Scheme && strings.EqualFold(parsed.Host, base.Host) &&
			(parsed.Path == base.Path || strings.HasPrefix(parsed.Path, base.Path+"/")) {
			return true
		}
	}
	return false
}

// validReleaseSlogan takes one line per language, the language it was written
// in among them, each no wider than the sidebar's second line takes and
// with no link or address.
func validReleaseSlogan(slogan releaseManifestSlogan) bool {
	if len(slogan.Text) == 0 || len(slogan.Text) > maxReleaseSloganLanguages || slogan.Text[slogan.Language] == "" {
		return false
	}
	for language, text := range slogan.Text {
		if !releaseLanguagePattern.MatchString(language) || text == "" || strings.TrimSpace(text) != text ||
			strings.IndexFunc(text, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) >= 0 ||
			releaseSloganAddress.MatchString(text) || releaseSloganWidth(text) > maxReleaseSloganWidth {
			return false
		}
	}
	return true
}

// releaseSloganWidth counts a wide character (Hangul, Han, kana, full-width
// forms) as two and any other as one, as a line of text takes room.
func releaseSloganWidth(text string) int {
	total := 0
	for _, r := range text {
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			total += 2
		default:
			total++
		}
	}
	return total
}

func validReleaseLink(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && len(raw) <= 512 && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		strings.IndexFunc(raw, unicode.IsSpace) < 0
}

// validReleaseSponsorLogo takes a small square-ish PNG or WebP data URI.
func validReleaseSponsorLogo(logo string) bool {
	var decode func([]byte) (image.Config, error)
	var encoded string
	switch {
	case strings.HasPrefix(logo, "data:image/png;base64,"):
		encoded = strings.TrimPrefix(logo, "data:image/png;base64,")
		decode = func(data []byte) (image.Config, error) { return png.DecodeConfig(bytes.NewReader(data)) }
	case strings.HasPrefix(logo, "data:image/webp;base64,"):
		encoded = strings.TrimPrefix(logo, "data:image/webp;base64,")
		decode = func(data []byte) (image.Config, error) { return webp.DecodeConfig(bytes.NewReader(data)) }
	default:
		return false
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > maxReleaseSponsorLogoBytes {
		return false
	}
	config, err := decode(data)
	return err == nil && config.Width >= minReleaseSponsorLogoPixels && config.Height >= minReleaseSponsorLogoPixels &&
		config.Width <= maxReleaseSponsorLogoPixels && config.Height <= maxReleaseSponsorLogoPixels
}
