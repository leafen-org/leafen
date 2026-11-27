package app

import (
	"html"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The shape the page a public address opens is drawn in — a full-page
// database laid on the whole canvas, a full-width page across the window,
// any other page in its column — and what stands at its head — an icon (a
// picture's box or an emoji's), a cover, the small text's title — said
// before the page itself comes, so the screen the web app draws while it
// waits for the page (SharedPageLoading, lib/publicOpeningShape.ts) lays its
// head by the page's own rules and nothing moves when the page is drawn. It
// is said as words with spaces between them, the shape first:
// "page icon cover". A share link's and a site's document say it in their
// head, for the page the address names; a view link, whose token its
// document's address doesn't carry, says it in its own address, after "#"
// beside the token, with the screen it was made for when that is a tablet's
// or a phone's, which a wider window shows in a frame of that size.
const (
	publicOpeningShapePage      = "page"
	publicOpeningShapeFullWidth = "full-width"
	publicOpeningShapeDatabase  = "database"
	publicOpeningHeadIcon       = "icon"
	publicOpeningHeadPicture    = "picture"
	publicOpeningHeadCover      = "cover"
	publicOpeningHeadSmallText  = "small-text"
	publicOpeningShapeMetaName  = "leafen-opening-shape"
	publicOpeningShapeViewParam = "shape"
	publicOpeningViewportParam  = "viewport"
)

// publicOpeningShape is the shape pageID is drawn in for a visitor and what
// heads it, or nothing for a page that can't be read.
func publicOpeningShape(app core.App, pageID string) string {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return ""
	}
	return publicOpeningWords(page.GetString("kind"), page.GetBool("full_width"),
		page.GetString("icon"), page.GetString("icon_type"), page.GetString("cover") != "", page.GetBool("small_text"))
}

// publicOpeningWords says a page's opening from what it is: what its
// address's document or view link says of it, and what a public read's list
// of pages says of each (publicContentPageSummaries), for a page opened
// from another.
func publicOpeningWords(kind string, fullWidth bool, icon, iconType string, cover, smallText bool) string {
	said := []string{publicOpeningShapePage}
	switch {
	case readPageKind(kind) == pageKindDatabase:
		said[0] = publicOpeningShapeDatabase
	case fullWidth:
		said[0] = publicOpeningShapeFullWidth
	}
	// An icon as the page's head draws one (PageIcon.tsx hasExplicitPageIcon,
	// isPagePictureIcon): a picture in its own box, a People row's person
	// and any other in an emoji's.
	switch {
	case icon == "" || iconType == "" || iconType == "none":
	case iconType == "image" && !strings.HasPrefix(icon, peopleRowPictureIcon):
		said = append(said, publicOpeningHeadPicture)
	default:
		said = append(said, publicOpeningHeadIcon)
	}
	if cover {
		said = append(said, publicOpeningHeadCover)
	}
	if smallText {
		said = append(said, publicOpeningHeadSmallText)
	}
	return strings.Join(said, " ")
}

// publicOpeningShapeHead is the line a public document's head says the
// shape of its page in, or none for a page that can't be read.
func publicOpeningShapeHead(app core.App, pageID string) []string {
	shape := publicOpeningShape(app, pageID)
	if shape == "" {
		return nil
	}
	return []string{`<meta name="` + publicOpeningShapeMetaName + `" content="` + html.EscapeString(shape) + `" />`}
}

// publicOpeningShapeViewSuffix is what a view link's address carries after
// its token for its page's shape and the screen it was made for.
func publicOpeningShapeViewSuffix(app core.App, pageID, viewport string) string {
	suffix := ""
	if shape := publicOpeningShape(app, pageID); shape != "" {
		suffix = "&" + publicOpeningShapeViewParam + "=" + url.QueryEscape(shape)
	}
	if viewport != "" && viewport != "desktop" {
		suffix += "&" + publicOpeningViewportParam + "=" + url.QueryEscape(viewport)
	}
	return suffix
}
