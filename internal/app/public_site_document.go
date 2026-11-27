package app

import (
	"errors"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"leafen.org/leafen/web"
)

const (
	publicSiteAssetPreview = "preview"
	publicSiteAssetFavicon = "favicon"
)

type storedPublicSiteDocument struct {
	ID                 string `db:"id"`
	WorkspaceID        string `db:"workspace_id"`
	RootPageID         string `db:"root_page_id"`
	Slug               string `db:"slug"`
	Title              string `db:"title"`
	Description        string `db:"description"`
	PreviewImageFileID string `db:"preview_image_file_id"`
	FaviconFileID      string `db:"favicon_file_id"`
	GoogleAnalyticsID  string `db:"google_analytics_id"`
	AllowIndexing      bool   `db:"allow_indexing"`
}

func registerPublicSiteDocumentRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/public-sites/{slug}/assets/{asset}", servePublicSiteAsset)
	event.Router.GET("/site/{slug}", servePublicSiteDocument)
}

// loadPublishedSiteDocument reads a site only while it is published and its
// share link lets anyone see it.
func loadPublishedSiteDocument(app core.App, slug string, now time.Time) (storedPublicSiteDocument, bool, error) {
	authorization, err := authorizePublicSitePages(app, slug, nil, now)
	if errors.Is(err, errPublicVisibilityNotFound) || isPermissionDenied(err) {
		return storedPublicSiteDocument{}, false, nil
	}
	if err != nil {
		return storedPublicSiteDocument{}, false, err
	}
	site := storedPublicSiteDocument{}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[workspace_id]], [[root_page_id]], [[slug]], [[title]], [[description]],
			[[preview_image_file_id]], [[favicon_file_id]], [[google_analytics_id]], [[allow_indexing]]
		FROM {{leafen_public_sites}} WHERE [[id]] = {:id} AND [[published]] = 1
	`).Bind(dbx.Params{"id": authorization.SiteID}).One(&site)
	if err != nil {
		return storedPublicSiteDocument{}, false, err
	}
	return site, true, nil
}

// servePublicSiteAsset gives anyone the share preview image or favicon a
// published site chose, and nothing once the site is unpublished.
func servePublicSiteAsset(event *core.RequestEvent) error {
	notFound := func() error {
		return apiError(event, http.StatusNotFound, errorCodePublicContentNotFound, "Public page not found.")
	}
	site, found, err := loadPublishedSiteDocument(event.App, strings.TrimSpace(event.Request.PathValue("slug")), time.Now().UTC())
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePublicContentFailed,
			"Couldn't load the public page.", "public_site.asset", "load", errorCodePublicContentFailed, err)
	}
	fileID := ""
	switch event.Request.PathValue("asset") {
	case publicSiteAssetPreview:
		fileID = site.PreviewImageFileID
	case publicSiteAssetFavicon:
		fileID = site.FaviconFileID
	}
	if !found || fileID == "" {
		return notFound()
	}
	workspace, err := event.App.FindRecordById(workspacesCollection, site.WorkspaceID)
	if err != nil {
		return notFound()
	}
	return servePublicProductFile(event, workspace, fileID, publicSiteAssetContentTypes,
		[]string{dlpOutboundPublicSharing}, "public, max-age=300")
}

// servePublicSiteDocument serves the web app for a published site with the
// site's title, description, share preview (or its root page's cover),
// favicon and Google Analytics in the page head — a page under the site its
// "?page=" names with that page's title and cover — so link previews and
// search engines see them before any script runs.
func servePublicSiteDocument(event *core.RequestEvent) error {
	index, err := fs.ReadFile(web.Dist(), "index.html")
	if err != nil {
		return err
	}
	document := string(index)
	slug := strings.TrimSpace(event.Request.PathValue("slug"))
	nonce, scriptHosts := "", []string{}
	now := time.Now().UTC()
	site, found, err := loadPublishedSiteDocument(event.App, slug, now)
	// A site whose address was changed, opened at its first address (its
	// title and its page's id), opens at the address it has now, as Notion's
	// address bar turns to the new path (seen 2026-11-13); the first address
	// keeps working for the links people were given. Not for good: the
	// first address is the site's again once it is published anew.
	if err == nil && found && site.Slug != "" && site.Slug != slug {
		target := "/site/" + url.PathEscape(site.Slug)
		if query := event.Request.URL.RawQuery; query != "" {
			target += "?" + query
		}
		return event.Redirect(http.StatusFound, target)
	}
	if err == nil && found {
		origin, originErr := publicServerOrigin(event.Request)
		if originErr == nil {
			if googleAnalyticsMeasurementIDPattern.MatchString(site.GoogleAnalyticsID) {
				nonce, scriptHosts = newDocumentNonce(), googleAnalyticsScriptHosts
			}
			// A site that stands has its pages' reach; one that can't be
			// read names no mentioned page in its head (publicHeadTitle).
			authorization, _ := authorizePublicPreview(event.App, publicContentSourceSite, slug, nil, now)
			authorization.RootPageID = site.RootPageID
			pageID := publicHeadPageID(event.App, publicContentSourceSite, slug, authorization, event.Request.URL.Query().Get("page"), now)
			document = publicSiteDocumentHTML(document, site, origin, slug, pageID, event.App, nonce,
				authorization, requestTextLanguageIn(event.App, event.Request))
		}
	}
	// A site unknown, taken down or unreadable previews as the server (link_preview_card.go).
	if err != nil || !found {
		document = withServerCard(event, document)
	}
	// Search engines leave a site alone until its owner allows them.
	if !found || !site.AllowIndexing {
		event.Response.Header().Set("X-Robots-Tag", "noindex")
	}
	event.Response.Header().Set("Cache-Control", "no-cache")
	setLeafenDocumentPolicy(event.Response.Header(), nonce, scriptHosts...)
	// "이 페이지 임베드" (the publish tab): a published site opened as an embed
	// may sit in another site's frame, the packaged client's page among them
	// (its wails:// address is no network scheme, so "*" never matches it).
	// The site editor's preview sits in the editor, on the server's own page
	// or the packaged client's: the server's SAMEORIGIN refused the latter and
	// the Mac app showed the preview blank (2026-11-06). Every other Leafen page
	// stays SAMEORIGIN.
	if found && event.Request.URL.Query().Get("embed") == "1" {
		event.Response.Header().Del("X-Frame-Options")
		event.Response.Header().Set("Content-Security-Policy", leafenDocumentPolicy(nonce, scriptHosts...)+
			"; frame-ancestors * "+strings.Join(packagedClientOrigins, " "))
	} else if found && event.Request.URL.Query().Get("preview") == "1" {
		if origins, err := framingOrigins(event.Request); err == nil {
			event.Response.Header().Del("X-Frame-Options")
			event.Response.Header().Set("Content-Security-Policy", leafenDocumentPolicy(nonce, scriptHosts...)+
				"; "+frameAncestorsDirective(origins))
		}
	}
	return event.HTML(http.StatusOK, document)
}

// publicSiteDocumentHTML heads a site's document with the page it opens on,
// pageID: its first page, or a page under it, which heads itself.
func publicSiteDocumentHTML(
	document string, site storedPublicSiteDocument, origin, slug, pageID string, app core.App, nonce string,
	authorization publicVisibilityAuthorization, language string,
) string {
	// The link title the site was given under "검색 엔진 인덱싱" heads its
	// first page as it was written, else that page's, as Notion's link title
	// starts out "〈title〉 | Notion" (TopBarSiteIndexing); with its
	// description and preview image it is the first page's alone.
	first := pageID == site.RootPageID
	title, description := "", ""
	if first {
		title, description = strings.TrimSpace(site.Title), strings.TrimSpace(site.Description)
	}
	pageTitle, cover := publicPageHeadTitleAndCover(app, authorization, pageID)
	if title == "" {
		title = publicHeadTitleOrProduct(closedMentionsInTitle(pageTitle, language))
	}
	if description == "" {
		description = publicPageDescription(app, site.WorkspaceID, pageID)
	}
	siteURL, coverPage := origin+"/site/"+url.PathEscape(slug), ""
	if !first {
		siteURL, coverPage = siteURL+"?page="+url.QueryEscape(pageID), pageID
	}
	assetURL := origin + apiRoot + "/public-sites/" + url.PathEscape(slug) + "/assets/"
	// The site's own preview image comes first, then its page's cover.
	imageURL := publicCoverPreviewURL(cover, origin, publicContentSourceSite, slug, coverPage)
	if first && site.PreviewImageFileID != "" {
		imageURL = assetURL + publicSiteAssetPreview
	}
	if site.FaviconFileID != "" {
		document = strings.Replace(document, `<link rel="icon" href="/favicon.ico" />`,
			`<link rel="icon" href="`+html.EscapeString(assetURL+publicSiteAssetFavicon)+`" />`, 1)
	}
	// The shape its page is drawn in, for the screen shown while it comes (public_opening_shape.go).
	extra := publicOpeningShapeHead(app, pageID)
	if !site.AllowIndexing {
		extra = append(extra, `<meta name="robots" content="noindex" />`)
	}
	if googleAnalyticsMeasurementIDPattern.MatchString(site.GoogleAnalyticsID) {
		id := site.GoogleAnalyticsID
		extra = append(extra,
			`<script async src="https://www.googletagmanager.com/gtag/js?id=`+id+`"></script>`,
			`<script nonce="`+nonce+`">window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}`+
				`gtag('js',new Date());gtag('config','`+id+`');</script>`,
		)
	}
	return publicPreviewDocumentHTML(document, title, description, publicHeadSiteName(origin), siteURL, imageURL, extra...)
}

// publicSiteFaviconURL is where a published site's own favicon is served, or
// nothing when the site shows its root page icon.
func publicSiteFaviconURL(slug, fileID string) string {
	if fileID == "" {
		return ""
	}
	return apiRoot + "/public-sites/" + url.PathEscape(slug) + "/assets/" + publicSiteAssetFavicon
}
