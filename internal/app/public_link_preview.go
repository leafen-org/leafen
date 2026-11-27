package app

import (
	"bytes"
	"errors"
	"html"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"leafen.org/leafen/web"
)

// Link previews: a chat app or search engine that reads a shared page's or
// published site's address gets the page's title and, as its image, the
// page's cover — an uploaded image, an image address, or a gallery gradient
// drawn here — so the card looks like the page without the server running a
// browser. A site's own preview image still comes first.

const (
	publicCoverPreviewWidth  = 1200
	publicCoverPreviewHeight = 630
)

var publicCoverGradientPattern = regexp.MustCompile(
	`^linear-gradient\(\s*(-?\d+(?:\.\d+)?)deg\s*,\s*(#[0-9a-fA-F]{3}|#[0-9a-fA-F]{6})\s*,\s*(#[0-9a-fA-F]{3}|#[0-9a-fA-F]{6})\s*\)$`)

func registerPublicLinkPreviewRoutes(event *core.ServeEvent) {
	event.Router.GET("/share/{token}", servePublicShareDocument)
	event.Router.GET(apiRoot+"/public-content/{source}/{key}/cover", servePublicCoverPreview)
	registerLinkPreviewRoutes(event)
}

// authorizePublicPreview reads a share link or published site for its
// preview, and the pages under it asked for. View links have none: they are
// an agent's, and opening one uses it.
func authorizePublicPreview(app core.App, source, key string, pageIDs []string, now time.Time) (publicVisibilityAuthorization, error) {
	if key == "" || len(key) > maxPublicContentTokenCharacters {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}
	switch source {
	case publicContentSourceShare:
		return authorizePublicSharePages(app, key, pageIDs, now)
	case publicContentSourceSite:
		return authorizePublicSitePages(app, key, pageIDs, now)
	}
	return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
}

// publicHeadPageID is the page a public address heads its document with:
// the page under the link or site it names (its "?page="), as Notion's
// published sub-pages and database pages head themselves (seen 2026-11-12:
// "첫 행 | Notion" with the row's own cover), else — none named, or one the
// link doesn't show — its first page.
func publicHeadPageID(app core.App, source, key string, authorization publicVisibilityAuthorization, requested string, now time.Time) string {
	requested = strings.TrimSpace(requested)
	if requested == "" || requested == authorization.RootPageID {
		return authorization.RootPageID
	}
	if _, err := authorizePublicPreview(app, source, key, []string{requested}, now); err != nil {
		return authorization.RootPageID
	}
	return requested
}

// publicCoverPreviewURL is where a preview finds a public page's cover, or
// nothing when the page has none a card can show; pageID names a page under
// the link's first.
func publicCoverPreviewURL(cover, origin, source, key, pageID string) string {
	cover = strings.TrimSpace(cover)
	base := origin + apiRoot + "/public-content/" + source + "/" + url.PathEscape(key)
	fileID, managed := productFilePresentationID(cover)
	switch {
	case bundledGalleryCoverPaths[cover]:
		return origin + cover
	case managed:
		return base + "/files/" + fileID
	case publicCoverGradientPattern.MatchString(cover) && pageID != "":
		return base + "/cover?page=" + url.QueryEscape(pageID)
	case publicCoverGradientPattern.MatchString(cover):
		return base + "/cover"
	}
	if parsed, err := url.Parse(cover); err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" {
		return parsed.String()
	}
	return ""
}

// publicPageTitleAndCover is a public page's title, as its visitor reads it
// (public_content_names.go), and its cover.
func publicPageTitleAndCover(app core.App, authorization publicVisibilityAuthorization, pageID string) (string, string) {
	title, _, cover := publicPageRead(app, authorization, pageID)
	return title, cover
}

// publicPageHeadTitleAndCover is publicPageTitleAndCover with the title in
// the plain words a document's head says it in, as the app's window title
// says it once it runs: a custom emoji the workspace never had says nothing
// (titleWithoutNeverHadCustomEmoji).
func publicPageHeadTitleAndCover(app core.App, authorization publicVisibilityAuthorization, pageID string) (string, string) {
	title, spans, cover := publicPageRead(app, authorization, pageID)
	if len(spans) > 0 {
		title = strings.TrimSpace(titleWithoutNeverHadCustomEmoji(spelledName(spans), spans))
	}
	return title, cover
}

// publicPageRead is a public page's title as its visitor reads it, the
// spans spelling it while its name holds a mention
// (publicTitlesSpansAsRead), and its cover.
func publicPageRead(app core.App, authorization publicVisibilityAuthorization, pageID string) (string, []graphTextSpan, string) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return "", nil, ""
	}
	closer := publicVisitorCloser(app, authorization, time.Now().UTC())
	spans := publicTitlesSpansAsRead(app, closer, []string{page.Id})[page.Id]
	return strings.TrimSpace(publicTitleAsRead(app, closer, page.Id, page.GetString("title"))), spans, page.GetString("cover")
}

// servePublicShareDocument serves the web app for a share link with the
// page's title and cover in the page head, as a published site does, and
// the shape the page is drawn in (public_opening_shape.go).
func servePublicShareDocument(event *core.RequestEvent) error {
	index, err := fs.ReadFile(web.Dist(), "index.html")
	if err != nil {
		return err
	}
	document := string(index)
	token := strings.TrimSpace(event.Request.PathValue("token"))
	now := time.Now().UTC()
	authorization, err := authorizePublicPreview(event.App, publicContentSourceShare, token, nil, now)
	if err != nil {
		// A link unknown, off or expired previews as the server (link_preview_card.go).
		document = withServerCard(event, document)
	} else {
		if origin, originErr := publicServerOrigin(event.Request); originErr == nil {
			pageID := publicHeadPageID(event.App, publicContentSourceShare, token, authorization, event.Request.URL.Query().Get("page"), now)
			title, cover := publicPageHeadTitleAndCover(event.App, authorization, pageID)
			title = publicHeadTitleOrProduct(closedMentionsInTitle(title, requestTextLanguageIn(event.App, event.Request)))
			pageURL, coverPage := origin+"/share/"+url.PathEscape(token), ""
			if pageID != authorization.RootPageID {
				pageURL, coverPage = pageURL+"?page="+url.QueryEscape(pageID), pageID
			}
			document = publicPreviewDocumentHTML(document, title, publicPageDescription(event.App, authorization.WorkspaceID, pageID),
				publicHeadSiteName(origin), pageURL, publicCoverPreviewURL(cover, origin, publicContentSourceShare, token, coverPage),
				publicOpeningShapeHead(event.App, pageID)...)
		}
	}
	event.Response.Header().Set("Cache-Control", "no-cache")
	setLeafenDocumentPolicy(event.Response.Header(), "")
	return event.HTML(http.StatusOK, document)
}

// publicHeadTitleOrProduct is the title a public page's document head gives
// a link preview or search engine before any script runs, in Notion's form:
// the page's own with the product's name after it, "〈title〉 | Leafen", as
// Notion's published and shared pages head themselves "〈title〉 | Notion"
// (seen 2026-11-12, a page and a database's row, any language); for an
// untitled one the product's line, publicHeadUntitledTitle, in every
// language — as Notion's untitled page heads itself "Notion | …" whatever
// the visitor's language (seen 2026-11-11, again 2026-11-12). Its window
// says the title alone, or "새 페이지", once its script runs, as Notion's
// does (PageView).
func publicHeadTitleOrProduct(title string) string {
	if title == "" {
		return publicHeadUntitledTitle
	}
	return title + " | Leafen"
}

// The line an untitled public page heads itself with, which is also its
// site's link title placeholder (publicHeadUntitledTitle), and the sentence
// a public page with no words of its own says under its title in a link
// preview (publicHeadDefaultDescription), are Leafen's own words, kept in
// contracts/public-mutation-wire.json's strings so the server's head and the
// web's search-indexing panel say the same (publicMutationStrings), in every
// language — as Notion heads an untitled page and a page without words with
// lines of its own in every language (seen 2026-11-12).

// publicHeadSiteName names where a public page lives, as Notion's
// "〈site〉 on Notion" names its site's address (seen 2026-11-12:
// "beaded-joke-35b on Notion"): the server a Leafen page's address is on.
func publicHeadSiteName(origin string) string {
	host := origin
	if parsed, err := url.Parse(origin); err == nil && parsed.Host != "" {
		host = parsed.Host
	}
	return host + " on Leafen"
}

// publicPageDescription is the words a public page's head describes it with,
// as Notion's (seen 2026-11-12): the text of its first block that has any —
// a heading or a list item as well as a paragraph, the blocks before it
// without words passed over, all of it — and Leafen's sentence for a page
// with none, a database's page among them. A page or database the block
// names isn't its words, and what a visitor can't open reads as closed.
func publicPageDescription(app core.App, workspaceID, pageID string) string {
	blocks, err := pageBlocksWholeLines(app, pageID, workspaceID)
	if err != nil {
		return publicHeadDefaultDescription
	}
	for _, block := range blocks {
		// A block naming a page or database draws its name, not its words.
		if block.Type == graphBlockTypeChildPage || block.Type == graphBlockTypeLinkToPage || strings.Contains(block.Type, "database") {
			continue
		}
		if words := strings.Join(strings.Fields(block.Text), " "); words != "" {
			return words
		}
	}
	return publicHeadDefaultDescription
}

// publicPreviewDocumentHTML puts a public page's title, description, the
// site it is on and its preview image in the head of the web app's
// document, as Notion's published pages head themselves (an "article").
func publicPreviewDocumentHTML(document, title, description, siteName, pageURL, imageURL string, extra ...string) string {
	head := []string{
		`<meta property="og:type" content="article" />`,
		`<meta property="og:site_name" content="` + html.EscapeString(siteName) + `" />`,
		`<meta property="og:title" content="` + html.EscapeString(title) + `" />`,
		`<meta property="og:url" content="` + html.EscapeString(pageURL) + `" />`,
	}
	if description != "" {
		head = append(head,
			`<meta name="description" content="`+html.EscapeString(description)+`" />`,
			`<meta property="og:description" content="`+html.EscapeString(description)+`" />`,
		)
	}
	if imageURL != "" {
		head = append(head,
			`<meta property="og:image" content="`+html.EscapeString(imageURL)+`" />`,
			`<meta name="twitter:card" content="summary_large_image" />`,
		)
	} else {
		head = append(head, `<meta name="twitter:card" content="summary" />`)
	}
	head = append(head, extra...)
	document = strings.Replace(document, "<title>Leafen</title>", "<title>"+html.EscapeString(title)+"</title>", 1)
	return strings.Replace(document, "</head>", "    "+strings.Join(head, "\n    ")+"\n  </head>", 1)
}

// servePublicCoverPreview draws a shared page's gallery gradient cover as a
// preview-sized image: its first page's, or that of the page under it its
// "?page=" names.
func servePublicCoverPreview(event *core.RequestEvent) error {
	pageIDs := []string{}
	if pageID := strings.TrimSpace(event.Request.URL.Query().Get("page")); pageID != "" {
		pageIDs = append(pageIDs, pageID)
	}
	authorization, err := authorizePublicPreview(event.App, event.Request.PathValue("source"),
		strings.TrimSpace(event.Request.PathValue("key")), pageIDs, time.Now().UTC())
	if errors.Is(err, errPublicVisibilityNotFound) {
		return publicContentFileNotFound(event)
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePublicContentFailed,
			"Couldn't load the public page.", "public_content.cover", "authorize", errorCodePublicContentFailed, err)
	}
	pageID := authorization.RootPageID
	if len(pageIDs) > 0 {
		pageID = pageIDs[0]
	}
	_, cover := publicPageTitleAndCover(event.App, authorization, pageID)
	picture, ok := publicGradientCover(strings.TrimSpace(cover), publicCoverPreviewWidth, publicCoverPreviewHeight)
	if !ok {
		return publicContentFileNotFound(event)
	}
	encoded := bytes.Buffer{}
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&encoded, picture); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePublicContentFailed,
			"Couldn't load the public page.", "public_content.cover", "encode", errorCodePublicContentFailed, err)
	}
	event.Response.Header().Set("Cache-Control", "public, max-age=300")
	return event.Blob(http.StatusOK, "image/png", encoded.Bytes())
}

// publicGradientCover paints a two-color CSS linear gradient the way a
// browser does: the angle points from the first color to the second, and
// the gradient line spans the box's corners.
func publicGradientCover(cover string, width, height int) (*image.RGBA, bool) {
	match := publicCoverGradientPattern.FindStringSubmatch(cover)
	if match == nil {
		return nil, false
	}
	degrees, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return nil, false
	}
	from, to := publicCoverHexColor(match[2]), publicCoverHexColor(match[3])
	radians := degrees * math.Pi / 180
	dx, dy := math.Sin(radians), -math.Cos(radians)
	length := math.Abs(float64(width)*dx) + math.Abs(float64(height)*dy)
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	mix := func(a, b uint8, t float64) uint8 { return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t)) }
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			t := ((float64(x)+0.5-float64(width)/2)*dx+(float64(y)+0.5-float64(height)/2)*dy)/length + 0.5
			t = math.Max(0, math.Min(1, t))
			picture.SetRGBA(x, y, color.RGBA{mix(from.R, to.R, t), mix(from.G, to.G, t), mix(from.B, to.B, t), 255})
		}
	}
	return picture, true
}

func publicCoverHexColor(value string) color.RGBA {
	value = strings.TrimPrefix(value, "#")
	if len(value) == 3 {
		value = string([]byte{value[0], value[0], value[1], value[1], value[2], value[2]})
	}
	parsed, _ := strconv.ParseUint(value, 16, 32)
	return color.RGBA{uint8(parsed >> 16), uint8(parsed >> 8), uint8(parsed), 255}
}
