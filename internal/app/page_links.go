package app

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page URL from the Notion-compatible API opens the page in the web app:
// an ordinary page and a database row page both use /p/ with their public
// ID. The web app asks page links which of its routes a public ID opens, for
// a reader who may open it: an ordinary page opens as itself, a row page
// opens over the page holding its database, and a database — by its ID, its
// full-page page's, or the page a full-page one left as it turned inline —
// opens where the database is now, at its one address (databaseAddress.link);
// People's database and its rows open as People and its people
// (peoplePageLink). A page's public URL is the published site address that shows it, when
// there is one.

const (
	pageLinkPath            = "/page-links/{id}"
	errorCodePageLinkFailed = "LEAFEN_PAGE_LINK_FAILED"
)

func registerPageLinkRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+pageLinkPath, readPageLink)
}

func notionPageURL(origin, publicID string) string {
	return origin + "/p/" + publicID
}

// notionMentionURL is the address a mention of a page, a database or a
// block reads by through the public API, its JSON and page Markdown alike,
// and a child page or database in page Markdown (<page url>, <database
// url>), as Notion's: /p/ and the ID's 32 hexadecimal digits, whatever
// dashes it was written with, found or not (notionMentionID; seen 2026-11-11:
// https://app.notion.com/p/4141fd67973881309a2faaaaaaaaaaaa for
// 4141fd67-9738-…). A page object's url names it after its title too
// (notionPageObjectURL).
// The MCP tools write theirs the same way by Leafen's IDs — a page's and a
// database's own address (mcpPageURL, mcpDatabaseAddress), and a mention's
// and a sub-page's in the Markdown they read, at a block by its ID without
// dashes too (mcpBlockAnchor) — as Notion's MCP does (seen 2026-11-22); the
// dashed form they wrote before still reads as the same page.
func notionMentionURL(origin, publicID string) string {
	return origin + "/p/" + notionMentionID(publicID)
}

// notionMentionID is an ID as a /p/ address names it: a UUID without its
// dashes, as Notion's, which a reader puts back (normalizeNotionUUIDRequest);
// any other ID as it is, whose dashes no reader could put back (an ID given
// through the API, such as one-database-2).
func notionMentionID(publicID string) string {
	if dashed, uuid := normalizeNotionUUIDRequest(publicID); uuid && dashed == strings.ToLower(publicID) && strings.Contains(publicID, "-") {
		return strings.ReplaceAll(publicID, "-", "")
	}
	return publicID
}

// databaseAppAddress is a database's address by Leafen's IDs, as the MCP
// tools and the Markdown they read give it — fetch, search, the lists,
// create-database, a page's <database> tag and a <mention> of it: its own
// ID's (notionMentionURL), full-page or in a page. One database, one
// address, as Notion's, which turning it inline or into a page keeps (seen
// 2026-11-13: Notion's is_inline left its url as it was); a full-page
// database's page moves away with the turn, so its ID can't be it. The app
// names it so too (databaseAddress.link), in its own IDs' spelling; the
// page's address still opens the database while it holds it (fetch, the
// app's page links). The public API names it by that ID too, when it is a
// UUID (databaseOwnPublicID).
func databaseAppAddress(origin, containerID string) string {
	return notionMentionURL(origin, containerID)
}

func readPageLink(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	id := strings.TrimSpace(event.Request.PathValue("id"))
	// People's database and its rows open as People and its people (peoplePageLink).
	answer, people, err := peoplePageLink(event.App, user.Id, id)
	if err == nil && !people {
		// The page a full-page database left as it turned inline is its
		// database's address still (database_page_signposts.go).
		var address databaseAddress
		address, err = loadSignpostDatabaseAddress(event.App, user.Id, id)
		answer = address.link()
	}
	if err == nil && answer["path"] == "" {
		answer, err = resolvePageLink(event.App, user.Id, id)
	}
	if err == nil && answer["path"] == "" {
		var address databaseAddress
		address, err = loadDatabaseAddress(event.App, user.Id, id)
		answer = address.link()
	}
	// A page's API url names it after its title (notion_page_url.go).
	if slugged := notionSlugAddressID(id); err == nil && answer["path"] == "" && slugged != "" {
		answer, err = resolvePageLink(event.App, user.Id, slugged)
	}
	// A block's own address opens its page at it (page_link_blocks.go).
	if err == nil && answer["path"] == "" {
		answer, err = resolveBlockPageLink(event.App, user.Id, id)
	}
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodePageLinkFailed, "Couldn't check the page address.",
			"page-link", "resolve", errorCodePageLinkFailed, err,
		)
	}
	if answer["path"] == "" {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	if people {
		return event.JSON(http.StatusOK, answer)
	}
	if answer["path"], err = pageLinkWithView(event.App, user.Id, answer["path"], event.Request.URL.Query().Get("v")); err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodePageLinkFailed, "Couldn't check the page address.",
			"page-link", "resolve", errorCodePageLinkFailed, err,
		)
	}
	return event.JSON(http.StatusOK, answer)
}

// pageLinkWithView names the view an address asks for (`?v=`) by the ID the
// app knows it by: the API's view url names it by its public ID — its own
// ID without its dashes, or for a view without a UUID of its own the
// identity made for the API (viewOwnPublicID) — and the app's own addresses
// by its own; either opens it.
// A view the reader may not read, or none, leaves the address as it was.
func pageLinkWithView(app core.App, principalID, path, view string) (string, error) {
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityView, strings.TrimSpace(view))
	if errors.Is(err, errInvalidNotionObjectIdentity) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	var pageID string
	err = app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_database_views}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": identity.InternalID}).Row(&pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	if _, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead); isPermissionDenied(err) {
		return path, nil
	} else if err != nil {
		return "", err
	}
	return path + "?" + url.Values{"v": {identity.InternalID}}.Encode(), nil
}

// databaseAddress is the database an address names, for someone who may read
// the page holding it.
type databaseAddress struct {
	pageID, blockID string
	fullPage        bool
}

// link answers where the address opens: at the database's own ID, as
// Notion's one address, which turning it inline or into a page keeps (seen
// 2026-11-14: /p/<its ID>?v= in the address bar and "링크 복사" alike, its
// page's or not), and the screen is told which page shows it — a full-page
// one's own page, or the page one in a page is held by, which shows it alone
// (as Notion's, seen 2026-11-05).
func (address databaseAddress) link() map[string]string {
	switch {
	case address.pageID == "":
		return map[string]string{}
	case address.fullPage:
		return map[string]string{"path": "/p/" + url.PathEscape(address.blockID), "databasePageId": address.pageID}
	}
	return map[string]string{"path": "/p/" + url.PathEscape(address.blockID), "holderPageId": address.pageID}
}

// loadFullPageDatabaseBlock is the block of a full-page database page's own
// database, or "" for any other page (fullPageDatabaseBlockSQL).
func loadFullPageDatabaseBlock(app core.App, pageID string) (string, error) {
	var blockID string
	err := app.DB().NewQuery(`SELECT ` + fullPageDatabaseBlockSQL + ` FROM {{leafen_pages}} AS [[page]] WHERE [[page]].[[id]]={:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&blockID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return blockID, err
}

// loadDatabaseAddress finds a database by its block's, its own or its public
// ID — its own UUID without its dashes too, as the MCP tools' address names
// it (databaseAppAddress) — for a reader who may read its page.
func loadDatabaseAddress(app core.App, userID, id string) (databaseAddress, error) {
	found, err := databaseAddressRecord(app, id)
	if err != nil || found.PageID == "" {
		return databaseAddress{}, err
	}
	authorization, err := authorizePage(app, userID, found.PageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return databaseAddress{}, nil
	}
	if err != nil {
		return databaseAddress{}, err
	}
	return databaseAddress{
		pageID: found.PageID, blockID: found.ID, fullPage: authorization.Page.GetString("kind") == pageKindDatabase,
	}, nil
}

// databaseAddressRecord is the database an address's ID names and the page
// it is on, whoever reads (empty for none): its block's, its own or its
// public ID, its own UUID without its dashes too. The page link route asks
// it for its reader (loadDatabaseAddress), and a link preview for a page
// published to anyone (link_preview_page.go).
func databaseAddressRecord(app core.App, id string) (databaseAddressFound, error) {
	if identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityDatabase, id); err == nil {
		id = identity.InternalID
	} else if dashed, uuid := normalizeNotionUUIDRequest(id); uuid && !strings.Contains(id, "-") {
		id = dashed
	}
	found := databaseAddressFound{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]] FROM {{leafen_blocks}}
		WHERE [[id]]={:id} AND [[type]]='inline_database' AND ` + blockStandsSQL("") + ` LIMIT 1
	`).Bind(dbx.Params{"id": id}).One(&found)
	if errors.Is(err, sql.ErrNoRows) {
		err = app.DB().NewQuery(`
			SELECT [[id]],[[page_id]] FROM {{leafen_database_containers}} WHERE [[id]]={:id} AND [[kind]]='source' LIMIT 1
		`).Bind(dbx.Params{"id": id}).One(&found)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return databaseAddressFound{}, nil
	}
	return found, err
}

type databaseAddressFound struct {
	ID     string `db:"id"`
	PageID string `db:"page_id"`
}

// loadSignpostDatabaseAddress is the database a page's ID names while the
// page is a full-page database's signpost, for someone who may read the page
// and the page the database is in now: as Notion's, a full-page database's
// ID opens it alone once it is inline and as its page once it is a page
// again (seen 2026-11-13). None otherwise.
func loadSignpostDatabaseAddress(app core.App, userID, id string) (databaseAddress, error) {
	pageID, err := resolveNotionPageRecordID(app, id)
	if err != nil {
		return databaseAddress{}, nil
	}
	place, signpost, err := loadDatabasePagePlace(app, pageID)
	if err != nil || !signpost {
		return databaseAddress{}, err
	}
	if _, err := authorizePage(app, userID, pageID, permissionWorkspaceRead); isPermissionDenied(err) {
		return databaseAddress{}, nil
	} else if err != nil {
		return databaseAddress{}, err
	}
	return loadDatabaseAddress(app, userID, place.BlockID)
}

// resolvePageLink answers where a page's public ID, or a row page's own ID,
// opens for this principal — a full-page database's page at its database's
// address — with no path when there is none they may open.
func resolvePageLink(app core.App, principalID, id string) (map[string]string, error) {
	path, err := resolvePageLinkPath(app, principalID, id)
	pageID, page := strings.CutPrefix(path, "/p/")
	if err != nil || !page {
		return map[string]string{"path": path}, err
	}
	if pageID, err = url.PathUnescape(pageID); err != nil {
		return nil, err
	}
	blockID, err := loadFullPageDatabaseBlock(app, pageID)
	if err != nil || blockID == "" {
		return map[string]string{"path": path}, err
	}
	return databaseAddress{pageID: pageID, blockID: blockID, fullPage: true}.link(), nil
}

// resolvePageLinkPath returns the web route a page's public ID, or a row
// page's own ID, opens for this principal, or "" when there is none they may
// open.
func resolvePageLinkPath(app core.App, principalID, id string) (string, error) {
	rowID := id
	if pageID, err := resolveNotionPageRecordID(app, id); err == nil {
		// A row's page has a record too, and opens over its database below.
		_, rowErr := loadDatabaseRowPageAuthority(app, pageID)
		if rowErr != nil && !errors.Is(rowErr, errDatabaseRowPageNotFound) {
			return "", rowErr
		}
		if rowErr != nil {
			// One an operation holds a while opens, and says so there (page_access_hold.go).
			_, err := pageAccessDespiteHold(authorizePage(app, principalID, pageID, permissionWorkspaceRead))
			if err == nil {
				return "/p/" + url.PathEscape(pageID), nil
			}
			if !isPermissionDenied(err) {
				return "", err
			}
			return "", nil
		}
		rowID = pageID
	} else if identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityRowPage, id); err == nil {
		rowID = identity.InternalID
	} else if !errors.Is(err, errInvalidNotionObjectIdentity) {
		return "", err
	}
	authority, err := authorizeDatabaseRowPage(app, principalID, rowID, permissionWorkspaceRead)
	if pageAccessHeld(err) != "" {
		return "/p/" + url.PathEscape(rowID), nil
	}
	// The page of a data source in the trash keeps its address there.
	authority, _, err = readTrashedSourceRowPage(app, principalID, rowID, authority, err)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// A row page has an address of its own, as Notion's (seen 2026-11-04).
	return "/p/" + url.PathEscape(authority.RowID), nil
}

// notionPagePublicURL returns the address of the published site that shows
// a page, or nil when no published site does.
func notionPagePublicURL(app core.App, origin, workspaceID, pageID string) (any, error) {
	sites := []struct {
		Slug       string `db:"slug"`
		RootPageID string `db:"root_page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[slug]],[[root_page_id]] FROM {{leafen_public_sites}}
		WHERE [[workspace_id]]={:workspaceId} AND [[published]]=1 ORDER BY [[slug]]
	`).Bind(dbx.Params{"workspaceId": workspaceID}).All(&sites); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, site := range sites {
		_, err := authorizePublicSitePages(app, site.Slug, []string{pageID}, now)
		if errors.Is(err, errPublicVisibilityNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		address := origin + "/site/" + url.PathEscape(site.Slug)
		if pageID != site.RootPageID {
			address += "?" + url.Values{"page": {pageID}}.Encode()
		}
		return address, nil
	}
	return nil, nil
}
