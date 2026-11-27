package app

import (
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"leafen.org/leafen/web"
)

// A page's own address (/p/…) previews as the page — its title, its first
// words and its cover — only while the page is published to the web, by its
// web link or a site, the same publishing that opens it to anyone
// (authorizePublicVisibilityPages: the organization lets its people publish,
// the page isn't in the trash or a teamspace that can't publish); any other
// page's address previews as the server (link_preview_card.go), its title and
// words never read for a reader signed in to nothing. Notion's previews a
// page published to the web by its own title and cover, and a private one as
// Notion (seen 2026-11-12 and 2026-11-23).

const linkPreviewPageCoverPath = "/link-preview/p/{pageId}/cover"

var (
	appDocumentPagePath = regexp.MustCompile(`^/p/([^/]{1,255})/?$`)
	compactPageID       = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

func registerLinkPreviewRoutes(event *core.ServeEvent) {
	event.Router.GET(linkPreviewCardPath, serveLinkPreviewCard)
	event.Router.GET(linkPreviewPageCoverPath, serveLinkPreviewPageCover)
}

// appDocumentPath says whether a request for path is for the app's own
// document, which the app's files fall back to: the root, or any path no
// file of the build answers.
func appDocumentPath(path string) bool {
	name := strings.TrimPrefix(path, "/")
	if name == "" || name == "index.html" {
		return true
	}
	_, err := fs.Stat(web.Dist(), name)
	return errors.Is(err, fs.ErrNotExist)
}

// serveAppDocument serves the app's document headed with its link preview:
// the published page its address names, or the server.
func serveAppDocument(event *core.RequestEvent) error {
	// A page named after its title goes to its own address first (page_address_title_form.go).
	if redirected, err := redirectPageAddressTitleForm(event); redirected || err != nil {
		return err
	}
	index, err := fs.ReadFile(web.Dist(), "index.html")
	if err != nil {
		return err
	}
	document := string(index)
	if origin, err := publicServerOrigin(event.Request); err == nil {
		if head, ok := publishedPageDocument(event, document, origin); ok {
			document = head
		} else {
			document = appDocumentWithServerCard(document, origin, loadLinkPreviewNaming(event.App, event.Request))
		}
	}
	event.Response.Header().Set("Cache-Control", "no-cache")
	return event.HTML(http.StatusOK, document)
}

// linkPreviewPageID is the page an address names, in the form its record
// keeps (an address may write the id without its dashes).
func linkPreviewPageID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if compactPageID.MatchString(raw) {
		return raw[0:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
	}
	return raw
}

// linkPreviewAddressPage is the page an address names for its preview, and
// the address it goes by: a page by its ID, its dashes left out or not, or a
// full-page database by its database's ID (its block's, its own or its
// public one), read as the page link route reads it (databaseAddressRecord)
// — a published full-page database at its own address previewed as the
// server (2026-11-23). A database in a page names no page of its own here.
func linkPreviewAddressPage(app core.App, raw string) (pageID, address string) {
	pageID = linkPreviewPageID(raw)
	if _, err := app.FindRecordById(pagesCollection, pageID); err == nil {
		return pageID, pageID
	}
	found, err := databaseAddressRecord(app, strings.TrimSpace(raw))
	if err != nil || found.PageID == "" {
		return pageID, pageID
	}
	if blockID, err := loadFullPageDatabaseBlock(app, found.PageID); err != nil || blockID != found.ID {
		return pageID, pageID
	}
	return found.PageID, found.ID
}

// publishedPageAuthorization reads the web link or site that publishes a
// page, the page itself or a page it is under. Every link that may publish
// it is read, the page's own first, before the ended ones are left out:
// eight read in no order and then the live ones kept, a page under nine
// links that had ended previewed as unpublished while a live one published
// it (2026-11-23). A page is under few links, one for each page above it
// published.
func publishedPageAuthorization(app core.App, pageID string, now time.Time) (publicVisibilityAuthorization, error) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
	}
	rows := []storedPublicShareAuthority{}
	err = app.DB().NewQuery(`
		SELECT [[link]].[[id]] AS [[link_id]], '' AS [[site_id]], [[link]].[[workspace_id]], [[link]].[[root_page_id]],
			[[link]].[[source_revision]], [[link]].[[expires_at]]
		FROM {{leafen_public_share_links}} [[link]]
		WHERE [[link]].[[workspace_id]] = {:workspaceId} AND [[link]].[[enabled]] = 1
			AND ([[link]].[[web_enabled]] = 1 OR EXISTS (
				SELECT 1 FROM {{leafen_public_sites}} [[site]]
				WHERE [[site]].[[share_link_id]] = [[link]].[[id]] AND [[site]].[[published]] = 1
					AND [[site]].[[workspace_id]] = [[link]].[[workspace_id]] AND [[site]].[[root_page_id]] = [[link]].[[root_page_id]]))
			AND ([[link]].[[root_page_id]] = {:pageId} OR EXISTS (
				SELECT 1 FROM {{leafen_public_share_graph_memberships}} [[membership]]
				WHERE [[membership]].[[share_link_id]] = [[link]].[[id]] AND [[membership]].[[page_id]] = {:pageId}
					AND [[membership]].[[root_page_id]] = [[link]].[[root_page_id]]
					AND [[membership]].[[source_revision]] = [[link]].[[source_revision]] AND [[membership]].[[state]] = 'complete'))
		ORDER BY [[link]].[[root_page_id]] = {:pageId} DESC, [[link]].[[id]]
	`).Bind(dbx.Params{"workspaceId": page.GetString("workspace"), "pageId": pageID}).All(&rows)
	if err != nil {
		return publicVisibilityAuthorization{}, err
	}
	for _, row := range rows {
		if publicVisibilityExpired(row.ExpiresAt, now) {
			continue
		}
		if authorization, err := authorizePublicVisibilityPages(app, row, []string{pageID}); err == nil {
			return authorization, nil
		}
	}
	return publicVisibilityAuthorization{}, errPublicVisibilityNotFound
}

// publishedPageDocument heads the app's document with the page its address
// names, when that page is published to the web.
func publishedPageDocument(event *core.RequestEvent, document, origin string) (string, bool) {
	match := appDocumentPagePath.FindStringSubmatch(event.Request.URL.Path)
	if match == nil {
		return "", false
	}
	pageID, address := linkPreviewAddressPage(event.App, match[1])
	authorization, err := publishedPageAuthorization(event.App, pageID, time.Now().UTC())
	if err != nil {
		return "", false
	}
	title, cover := publicPageHeadTitleAndCover(event.App, authorization, pageID)
	title = publicHeadTitleOrProduct(closedMentionsInTitle(title, requestTextLanguageIn(event.App, event.Request)))
	picture := linkPreviewPageCoverURL(cover, origin, pageID)
	if picture == "" {
		// With no cover, the server's card rather than none: a messenger had
		// made one of the touch icon, stretched and blurred.
		picture = linkPreviewCardURL(origin)
	}
	return publicPreviewDocumentHTML(document, title, publicPageDescription(event.App, authorization.WorkspaceID, pageID),
		publicHeadSiteName(origin), origin+"/p/"+url.PathEscape(address), picture), true
}

// linkPreviewPageCoverURL is where a published page's preview finds its
// cover: a gallery picture or an address as it is, a picture kept here or a
// gradient by the page's own preview address, which reads its publishing.
func linkPreviewPageCoverURL(cover, origin, pageID string) string {
	cover = strings.TrimSpace(cover)
	_, managed := productFilePresentationID(cover)
	switch {
	case bundledGalleryCoverPaths[cover]:
		return origin + cover
	case managed || publicCoverGradientPattern.MatchString(cover):
		return origin + strings.Replace(linkPreviewPageCoverPath, "{pageId}", url.PathEscape(pageID), 1)
	}
	if parsed, err := url.Parse(cover); err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" {
		return parsed.String()
	}
	return ""
}

// serveLinkPreviewPageCover serves a published page's cover for its
// preview: a picture kept here, as its public pages show it, or its
// gradient drawn; nothing for a page not published.
func serveLinkPreviewPageCover(event *core.RequestEvent) error {
	pageID := linkPreviewPageID(event.Request.PathValue("pageId"))
	now := time.Now().UTC()
	authorization, err := publishedPageAuthorization(event.App, pageID, now)
	if err != nil {
		return publicContentFileNotFound(event)
	}
	_, cover := publicPageTitleAndCover(event.App, authorization, pageID)
	cover = strings.TrimSpace(cover)
	if fileID, managed := productFilePresentationID(cover); managed {
		visible, err := publicContentFileVisible(event.App, authorization, fileID, now)
		if err != nil || !visible {
			return publicContentFileNotFound(event)
		}
		return servePublicProductFile(event, authorization.Workspace, fileID, nil,
			[]string{dlpOutboundPublicSharing, dlpOutboundFileDownloads}, "public, max-age=300")
	}
	picture, ok := publicGradientCover(cover, publicCoverPreviewWidth, publicCoverPreviewHeight)
	if !ok {
		return publicContentFileNotFound(event)
	}
	encoded, err := encodeLinkPreviewCard(picture)
	if err != nil {
		return publicContentFileNotFound(event)
	}
	event.Response.Header().Set("Cache-Control", "public, max-age=300")
	return event.Blob(http.StatusOK, "image/png", encoded)
}
