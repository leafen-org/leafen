package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"html"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"net/http"
	"strings"
	"sync"

	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"leafen.org/leafen/web"
)

// The server's own link preview: a messenger, chat app or search engine
// that reads the server's address — its first screen, its sign-in, any
// address of the app but a published page's — gets an Open Graph and
// Twitter card, as Notion's addresses head themselves with Notion's own card
// whatever page they name (seen 2026-11-23: app.notion.com/p/<private page>
// read with no session says "Notion | …", the product's line, og:type
// website, og:url the app's root and a 1200×625 picture). The card is the
// server's: its name, Leafen's one line in the server's language, and a
// wide picture the server draws from its icon (KakaoTalk had made the
// preview of the 180px touch icon, stretched and blurred, with its own
// words, 2026-11-23). The picture holds the icon, large, and Leafen's name,
// whatever the server is called or speaks: the server's own name is the
// card's title alone (user decision 2026-11-23 — no Korean font in the
// server for a name in Hangul). A reader of a preview is signed in to
// nothing: a page's address says nothing of the page unless the page is
// published to the web (link_preview_page.go).

const (
	linkPreviewCardPath   = "/link-preview.png"
	linkPreviewCardWidth  = 1200
	linkPreviewCardHeight = 630
	linkPreviewIconFile   = "icon-512.png"
	linkPreviewIconSide   = 400
)

// The line a server's card says under its name: Leafen's, as its own site
// and README say it.
var linkPreviewProductLine = map[string]string{
	accountLanguageKo: "노션을 오픈소스로 다시 만들었습니다.",
	accountLanguageEn: "Notion, rebuilt as open source.",
}

// linkPreviewNaming is what the server's card says it is.
type linkPreviewNaming struct {
	Name     string
	Alias    string
	Language string
}

// loadLinkPreviewNaming reads the server's name and the language its card
// speaks: that of the person who set the server up, else the reader's.
func loadLinkPreviewNaming(app core.App, request *http.Request) linkPreviewNaming {
	stored := struct {
		Name  string `db:"server_name"`
		Admin string `db:"setup_admin_id"`
		Alias string `db:"server_alias"`
	}{}
	_ = app.DB().NewQuery(`
		SELECT [[server_name]], [[setup_admin_id]],
			COALESCE((SELECT [[alias]] FROM {{leafen_server_identity}} WHERE [[id]] = 'current'), '') AS [[server_alias]]
		FROM {{leafen_server_settings}} WHERE [[id]] = 'default'
	`).One(&stored)
	language := requestTextLanguageIn(app, request)
	if stored.Admin != "" {
		if chosen, err := serverTextLanguage(app, stored.Admin, request); err == nil {
			language = chosen
		}
	}
	return linkPreviewNaming{Name: stored.Name, Alias: stored.Alias, Language: language}
}

// title is the server's name as its people read it: its administrators'
// name, or its alias in the card's language.
func (naming linkPreviewNaming) title() string {
	if naming.Name != "" {
		return naming.Name
	}
	if korean := serverAliasesKorean[naming.Alias]; korean != "" && naming.Language == accountLanguageKo {
		return korean
	}
	return serverDisplayName("", naming.Alias)
}

// withServerCard heads the app's document with the server's card, for an
// address that shows nothing else: the app's own, and a share link or site
// that is unknown, turned off or expired — which says nothing of its page,
// as a messenger had made a blurred preview of the touch icon for it too.
func withServerCard(event *core.RequestEvent, document string) string {
	origin, err := publicServerOrigin(event.Request)
	if err != nil {
		return document
	}
	return appDocumentWithServerCard(document, origin, loadLinkPreviewNaming(event.App, event.Request))
}

// appDocumentWithServerCard heads the app's document with the server's card.
func appDocumentWithServerCard(document, origin string, naming linkPreviewNaming) string {
	title := naming.title()
	line := linkPreviewProductLine[naming.Language]
	picture := linkPreviewCardURL(origin)
	alt := title + " — Leafen"
	locale := "en_US"
	if naming.Language == accountLanguageKo {
		locale = "ko_KR"
	}
	head := []string{
		`<meta property="og:type" content="website" />`,
		`<meta property="og:site_name" content="Leafen" />`,
		`<meta property="og:title" content="` + html.EscapeString(title) + `" />`,
		`<meta property="og:description" content="` + html.EscapeString(line) + `" />`,
		`<meta name="description" content="` + html.EscapeString(line) + `" />`,
		`<meta property="og:url" content="` + html.EscapeString(origin+"/") + `" />`,
		`<meta property="og:locale" content="` + locale + `" />`,
		`<meta property="og:image" content="` + html.EscapeString(picture) + `" />`,
		`<meta property="og:image:type" content="image/png" />`,
		`<meta property="og:image:width" content="1200" />`,
		`<meta property="og:image:height" content="630" />`,
		`<meta property="og:image:alt" content="` + html.EscapeString(alt) + `" />`,
		`<meta name="twitter:card" content="summary_large_image" />`,
		`<meta name="twitter:title" content="` + html.EscapeString(title) + `" />`,
		`<meta name="twitter:description" content="` + html.EscapeString(line) + `" />`,
		`<meta name="twitter:image" content="` + html.EscapeString(picture) + `" />`,
		`<meta name="twitter:image:alt" content="` + html.EscapeString(alt) + `" />`,
	}
	return strings.Replace(document, "</head>", "    "+strings.Join(head, "\n    ")+"\n  </head>", 1)
}

// The card's font: Go's own, for Leafen's name.
var linkPreviewFont = sync.OnceValue(func() *opentype.Font {
	bold, _ := opentype.Parse(gobold.TTF)
	return bold
})

// The card's picture is drawn once from the icon; its address names the
// icon, so a card read again after the icon changed is the new one (a
// messenger keeps a picture by its address).
var linkPreviewCardDrawn struct {
	sync.Mutex
	version string
	picture []byte
}

func linkPreviewIcon() []byte {
	icon, _ := fs.ReadFile(web.Dist(), linkPreviewIconFile)
	return icon
}

func linkPreviewCardVersion() string {
	digest := sha256.Sum256(linkPreviewIcon())
	return hex.EncodeToString(digest[:])[:16]
}

// linkPreviewCardURL is the card picture's address on the server at origin.
func linkPreviewCardURL(origin string) string {
	return origin + linkPreviewCardPath + "?v=" + linkPreviewCardVersion()
}

// serveLinkPreviewCard draws the server's card picture.
func serveLinkPreviewCard(event *core.RequestEvent) error {
	version := linkPreviewCardVersion()
	linkPreviewCardDrawn.Lock()
	defer linkPreviewCardDrawn.Unlock()
	if linkPreviewCardDrawn.version != version {
		picture, err := drawLinkPreviewCard()
		if err != nil {
			return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePublicContentFailed,
				"Couldn't draw the link preview.", "link_preview.card", "draw", errorCodePublicContentFailed, err)
		}
		linkPreviewCardDrawn.version, linkPreviewCardDrawn.picture = version, picture
	}
	event.Response.Header().Set("Cache-Control", "public, max-age=3600")
	return event.Blob(http.StatusOK, "image/png", linkPreviewCardDrawn.picture)
}

var (
	linkPreviewPaper = color.RGBA{0xF7, 0xF6, 0xF3, 0xFF}
	linkPreviewInk   = color.RGBA{0x37, 0x35, 0x2F, 0xFF}
)

// drawLinkPreviewCard draws the card: the server's icon, large, on Leafen's
// paper, and Leafen's name under it, centered, so a messenger that shows the
// middle of it as a square keeps both.
func drawLinkPreviewCard() ([]byte, error) {
	card := image.NewRGBA(image.Rect(0, 0, linkPreviewCardWidth, linkPreviewCardHeight))
	draw.Draw(card, card.Bounds(), image.NewUniform(linkPreviewPaper), image.Point{}, draw.Src)
	const iconTop = 66
	if icon, err := png.Decode(bytes.NewReader(linkPreviewIcon())); err == nil {
		left := (linkPreviewCardWidth - linkPreviewIconSide) / 2
		draw.CatmullRom.Scale(card, image.Rect(left, iconTop, left+linkPreviewIconSide, iconTop+linkPreviewIconSide),
			icon, icon.Bounds(), draw.Over, nil)
	}
	if bold := linkPreviewFont(); bold != nil {
		drawLinkPreviewLine(card, bold, "Leafen", 56, 552, linkPreviewInk)
	}
	return encodeLinkPreviewCard(card)
}

// drawLinkPreviewLine draws one centered line at baseline y, made smaller
// until it fits the card with room on each side.
func drawLinkPreviewLine(card *image.RGBA, typeface *opentype.Font, text string, size float64, baseline int, ink color.Color) {
	const room = linkPreviewCardWidth - 2*72
	for ; size >= 24; size -= 4 {
		face, err := opentype.NewFace(typeface, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return
		}
		width := font.MeasureString(face, text).Ceil()
		if width <= room || size-4 < 24 {
			drawer := font.Drawer{Dst: card, Src: image.NewUniform(ink), Face: face,
				Dot: fixed.P((linkPreviewCardWidth-width)/2, baseline)}
			drawer.DrawString(text)
			_ = face.Close()
			return
		}
		_ = face.Close()
	}
}

func encodeLinkPreviewCard(card image.Image) ([]byte, error) {
	encoded := bytes.Buffer{}
	err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&encoded, card)
	return encoded.Bytes(), err
}
