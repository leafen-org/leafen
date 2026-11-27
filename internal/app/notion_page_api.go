package app

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const notionAPIPagesPath = notionAPIRoot + "/pages"

func serveNotionPageCompatibility(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
) (bool, error) {
	if event.Request.Method == http.MethodPost && event.Request.URL.Path == notionAPIPagesPath {
		return true, serveNotionPageCreate(event, ygo)
	}
	if !strings.HasPrefix(event.Request.URL.Path, notionAPIPagesPath+"/") {
		return false, nil
	}
	remainder := strings.TrimPrefix(event.Request.URL.Path, notionAPIPagesPath+"/")
	segments := strings.Split(remainder, "/")
	if handled, err := serveNotionPageAskedForDatabase(event, segments); handled {
		return true, err
	}
	if event.Request.Method == http.MethodGet && len(segments) == 1 && segments[0] != "" {
		return true, serveNotionPageRetrieve(event, segments[0])
	}
	if event.Request.Method == http.MethodGet && len(segments) == 2 && segments[0] != "" && segments[1] == "markdown" {
		return true, serveNotionPageMarkdownRetrieve(event, segments[0])
	}
	if event.Request.Method == http.MethodPatch && len(segments) == 2 && segments[0] != "" && segments[1] == "markdown" {
		return true, serveNotionPageMarkdownUpdate(event, ygo, segments[0])
	}
	if event.Request.Method == http.MethodGet && len(segments) == 3 && segments[0] != "" && segments[1] == "properties" && segments[2] != "" {
		return true, serveNotionPagePropertyRetrieve(event, segments[0], segments[2])
	}
	if event.Request.Method == http.MethodPatch && len(segments) == 1 && segments[0] != "" {
		return true, serveNotionPageUpdate(event, ygo, segments[0])
	}
	if event.Request.Method == http.MethodPost && len(segments) == 2 && segments[0] != "" && segments[1] == "move" {
		return true, serveNotionPageMove(event, ygo, segments[0])
	}
	return false, nil
}

func serveNotionPageRetrieve(event *core.RequestEvent, publicPageID string) error {
	if !notionPublicIDNamesPage(event.App, strings.TrimSpace(publicPageID)) {
		return serveNotionDatabaseRowPageRetrieve(event, publicPageID)
	}
	authorization, authorized, err := authorizeNotionPage(
		event, publicPageID, "pages:read", permissionWorkspaceRead,
	)
	if err != nil || !authorized {
		return err
	}
	response, err := notionPageObject(
		event.App, event.Request, authorization.Identity, authorization.Page.Page,
	)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	recordAPIReads(event.App, authorization.Identity, []contentRead{contentReadOfPage(authorization.Page.Page)})
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func notionPageObject(
	app core.App,
	request *http.Request,
	identity notionOAuthBearerIdentity,
	page *core.Record,
) (map[string]any, error) {
	origin, err := outwardServerOrigin(request)
	if err != nil {
		return nil, err
	}
	return notionPageObjectAt(app, origin, newNotionBlockFileCapabilityIssuer(request, identity), identity, page)
}

// notionPageObjectAt is a page as the Public API gives it, its links at
// origin; an automation's webhook sends the same object.
func notionPageObjectAt(
	app core.App,
	origin string,
	fileIssuer *notionBlockFileCapabilityIssuer,
	identity notionOAuthBearerIdentity,
	page *core.Record,
) (map[string]any, error) {
	publicID, err := notionPagePublicID(app, page.Id)
	if err != nil {
		return nil, err
	}
	createdBy, lastEditedBy, err := notionPageAuthorIDs(app, page)
	if err != nil {
		return nil, err
	}
	createdTime, err := notionPageMinuteTime(page.GetString("created"))
	if err != nil {
		return nil, err
	}
	lastEditedTime, err := notionPageMinuteTime(page.GetString("updated"))
	if err != nil {
		return nil, err
	}
	parent := map[string]any{"type": "workspace", "workspace": true}
	if internalParentID := strings.TrimSpace(page.GetString("parent_id")); internalParentID != "" {
		// A page whose block sits in another block has that block as parent.
		parent, err = notionChildPageParent(app, page)
		if err != nil {
			return nil, err
		}
	}
	cover, err := notionPageCoverWithManagedFile(
		app, fileIssuer, identity.WorkspaceID, page.GetString("cover"),
	)
	if err != nil {
		return nil, err
	}
	icon, err := notionPageIconWithManagedFile(
		app, fileIssuer, identity.WorkspaceID,
		page.GetString("icon_type"), page.GetString("icon"),
	)
	if err != nil {
		return nil, err
	}
	publicURL, err := notionPagePublicURL(app, origin, identity.WorkspaceID, page.Id)
	if err != nil {
		return nil, err
	}
	_, archived, err := loadPageArchiveEffect(app, page.Id)
	if err != nil {
		return nil, err
	}
	title, err := notionPageTitleAsRead(app, fileIssuer, identity, page)
	if err != nil {
		return nil, err
	}
	// A sub-page of a trashed page is in the trash with it.
	_, trashed, err := loadPageTrashEffect(app, page.Id)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"object":           "page",
		"id":               publicID,
		"created_time":     createdTime,
		"last_edited_time": lastEditedTime,
		"created_by":       map[string]any{"object": "user", "id": createdBy},
		"last_edited_by":   map[string]any{"object": "user", "id": lastEditedBy},
		"cover":            cover,
		"icon":             icon,
		"parent":           parent,
		"in_trash":         trashed,
		"is_archived":      archived,
		"is_locked":        page.GetBool("is_locked"),
		"properties": map[string]any{
			"title": map[string]any{
				"id": "title", "type": "title", "title": title,
			},
		},
		"url":        notionPageObjectURL(origin, publicID, title),
		"public_url": publicURL,
	}, nil
}

// notionPageTitleAsRead is a page's title as Notion's API gives it to its
// reader: an ordinary page's with its mentions, equations, links and marks
// (page_title_spans.go), each mention by its target's name now and one the
// connection can't reach "Untitled", as a database's name reads
// (notionFormattedTitle); another page's as it stands.
func notionPageTitleAsRead(
	app core.App, files *notionBlockFileCapabilityIssuer, identity notionOAuthBearerIdentity, page *core.Record,
) ([]map[string]any, error) {
	title := page.GetString("title")
	spans := ordinaryPageTitleSpans(page)
	if len(spans) == 0 {
		return notionPageTitleRichText(title), nil
	}
	reader := notionBlockAuthorization{Identity: identity, Page: pageAuthorization{Page: page, Workspace: workspaceAuthorization{
		PrincipalID: identity.PrincipalID, WorkspaceID: page.GetString("workspace"), OwnerID: page.GetString("owner"),
	}}}
	return notionFormattedTitle(app, reader, files, title, spans)
}

func notionPageTitleRichText(title string) []map[string]any {
	if title == "" {
		return []map[string]any{}
	}
	return notionRichTextItemPieces([]map[string]any{{
		"type": "text",
		"text": map[string]any{"content": title, "link": nil},
		"annotations": map[string]any{
			"bold": false, "italic": false, "strikethrough": false,
			"underline": false, "code": false, "color": "default",
		},
		"plain_text": title,
		"href":       nil,
	}})
}

func notionPageIcon(kind, value string) any {
	value = strings.TrimSpace(value)
	if value == "" || kind == "none" {
		return nil
	}
	if kind == "emoji" {
		return map[string]any{"type": "emoji", "emoji": value}
	}
	if kind == nativeIconType {
		return nativeIconObject(value)
	}
	return notionPageExternalFile(value)
}

func notionPageExternalFile(value string) any {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if value == "" || err != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil
	}
	return map[string]any{"type": "external", "external": map[string]any{"url": value}}
}

func notionPageTime(value string) (string, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.000Z",
		"2006-01-02 15:04:05Z",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339Nano), nil
		}
	}
	return "", errInvalidNotionPageIdentity
}

// serveNotionPageAskedForDatabase answers a page request whose ID names a
// database — its own ID, a full-page database's page or a wiki's — as
// Notion's does (seen 2026-11-06, Notion-Version 2026-03-11; a wiki and a
// full-page database 2026-11-19): reading it or its Markdown, or changing its
// Markdown, says it is a database, once the reader may read it — the change
// with its tool's code beside the words; reading a property of it or
// updating it finds no page. Other page requests go on.
func serveNotionPageAskedForDatabase(event *core.RequestEvent, segments []string) (bool, error) {
	if len(segments) == 0 || segments[0] == "" {
		return false, nil
	}
	method, id := event.Request.Method, strings.TrimSpace(segments[0])
	markdownChange := method == http.MethodPatch && len(segments) == 2 && segments[1] == "markdown"
	read := method == http.MethodGet && (len(segments) == 1 || len(segments) == 2 && segments[1] == "markdown") ||
		markdownChange
	missing := method == http.MethodGet && len(segments) == 3 && segments[1] == "properties" ||
		method == http.MethodPatch && len(segments) == 1
	if !read && !missing {
		return false, nil
	}
	// Every page path first answers who is asking, with this scope; only a
	// caller who gets past that learns an ID is a database, so no one else
	// can tell databases from other IDs.
	scope, requirement := "pages:read", permissionWorkspaceRead
	if method == http.MethodPatch {
		scope, requirement = "pages:write", permissionWorkspaceWrite
	}
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, scope, requirement)
	if err != nil || !ok {
		return true, err
	}
	if !notionPublicIDNamesDatabase(event.App, id) {
		return false, nil
	}
	if missing {
		return true, notionPageNotFound(event)
	}
	// The reader must reach the database as the database API lets them:
	// by its own ID, through its container; by its page, through the page.
	if identity, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDatabase, id); err == nil {
		if identity.Workspace != workspace.Identity.WorkspaceID {
			return true, notionPageNotFound(event)
		}
		if _, _, err := loadNotionDatabaseContainer(event.App, workspace.Identity, identity.InternalID, permissionWorkspaceRead); err != nil {
			return true, notionDataSourceReadError(event, err)
		}
	} else if _, authorized, err := authorizeNotionPage(event, id, "pages:read", permissionWorkspaceRead); err != nil || !authorized {
		return true, err
	}
	const said = " is a database, not a page: read it with the retrieve database API."
	var data map[string]any
	if markdownChange {
		data = notionToolErrorData("invalid_input", "Provided ID {pageId}"+said)
	}
	return true, notionCompatibilityErrorWithData(event, http.StatusBadRequest, notionErrorValidation,
		"Provided ID "+canonicalNotionID(id)+said, data, nil)
}
