package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The settings' "공개 페이지", as Notion's (seen 2026-10-02, and its help): a
// workspace's published sites, public forms and pages anyone with the link
// can open, each with who first made it public and when, for every member
// but only the pages they can open. A share link or site keeps who first
// made it public (markPublicFirstPublish); a form keeps who made its link.

const (
	workspacePublicContentPath       = "/workspaces/{workspaceId}/public-content"
	workspacePublicContentWindowSize = 200
)

type publicContentItem struct {
	PageID string `json:"pageId"`
	Title  string `json:"title"`
	// The title's spans as its reader reads them, while its name holds a
	// mention (page_title_spans.go).
	TitleSpans []graphTextSpan `json:"titleSpans,omitempty"`
	// A site's address on this server, as /site/<slug>.
	Path        string                    `json:"path,omitempty"`
	PublishedBy workspaceConnectionPerson `json:"publishedBy"`
	PublishedAt string                    `json:"publishedAt"`
}

// publicContentResponse is a window of each kind asked for; with a kind's
// next cursor, more of it follow (asked for by that kind).
type publicContentResponse struct {
	Sites           []publicContentItem `json:"sites"`
	Forms           []publicContentItem `json:"forms"`
	Links           []publicContentItem `json:"links"`
	SitesNextCursor string              `json:"sitesNextCursor,omitempty"`
	FormsNextCursor string              `json:"formsNextCursor,omitempty"`
	LinksNextCursor string              `json:"linksNextCursor,omitempty"`
}

type storedPublicContentItem struct {
	ItemID      string `db:"item_id"`
	PageID      string `db:"page_id"`
	Title       string `db:"title"`
	Slug        string `db:"slug"`
	PublisherID string `db:"publisher_id"`
	Publisher   string `db:"publisher"`
	PublishedAt string `db:"published_at"`
	ExpiresAt   string `db:"expires_at"`
	InTrash     bool   `db:"in_trash"`
}

// markPublicFirstPublish keeps who made a share link or site public, and
// when, the first time only.
func markPublicFirstPublish(app core.App, table, id, actorID string, now time.Time) error {
	if id == "" {
		return nil
	}
	if table != "leafen_public_share_links" && table != "leafen_public_sites" {
		return errPublicMutationInvalid
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{` + table + `}} SET [[first_published_by]] = {:actor}, [[first_published_at]] = {:at}
		WHERE [[id]] = {:id} AND [[first_published_at]] = ''
	`).Bind(dbx.Params{"actor": actorID, "at": now.UTC().Format(time.RFC3339Nano), "id": id}).Execute()
	return err
}

func registerWorkspacePublicContentRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+workspacePublicContentPath, listWorkspacePublicContent)
}

func listWorkspacePublicContent(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead); err != nil {
		return workspaceConnectionError(event, err, "public-content")
	}
	query := event.Request.URL.Query()
	kinds := []string{"sites", "forms", "links"}
	if kind := strings.TrimSpace(query.Get("kind")); kind != "" {
		kinds = []string{kind}
	}
	cursor, err := decodeTextIDCursor(query.Get("cursor"))
	if err != nil || (cursor.ID != "" && len(kinds) != 1) {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceConnectionInvalid, "Give a kind with the nextCursor of its last list, or no cursor to start.")
	}
	response := publicContentResponse{Sites: []publicContentItem{}, Forms: []publicContentItem{}, Links: []publicContentItem{}}
	for _, kind := range kinds {
		items, next, err := workspacePublicContentWindow(event.App, user.Id, workspaceID, kind,
			strings.TrimSpace(query.Get("pageId")), cursor)
		if errors.Is(err, errListWindowCursor) {
			return apiError(event, http.StatusBadRequest, errorCodeWorkspaceConnectionInvalid, "Ask for sites, forms or links.")
		}
		if err != nil {
			return workspaceConnectionError(event, err, "public-content")
		}
		switch kind {
		case "sites":
			response.Sites, response.SitesNextCursor = items, next
		case "links":
			response.Links, response.LinksNextCursor = items, next
		default:
			response.Forms, response.FormsNextCursor = items, next
		}
	}
	return event.JSON(http.StatusOK, response)
}

// workspacePublicContentWindow is a window of one kind of public content
// the reader can open, newest first, from the cursor on (or of one page's
// alone). It looks at a bounded batch each time; next goes on from the last
// one looked at, "" once all are. A page in the trash, or under one
// (pageOutOfTrashPathSQL), is in none of them but its own site's, for those
// who edit it: Notion keeps a published page's
// bar under its trash band (seen 2026-11-24, PublishedSiteBanner), while
// its site isn't read by anyone (authorizePublicContentPage).
func workspacePublicContentWindow(app core.App, readerID, workspaceID, kind, pageID string, after textIDCursor) ([]publicContentItem, string, error) {
	items, _, next, err := workspacePublicContentWindowOf(app, readerID, workspaceID, kind, pageID, after)
	return items, next, err
}

// workspacePublicContentWindowOf is workspacePublicContentWindow with each
// item's own ID — its site's, link's or form's — and with no reader, all of
// them: a server administrator's (administrator_workspace_content_actions.go).
// Each title is as the reader reads it (page_name_mentions.go); with no
// reader, as written, for the caller to read.
func workspacePublicContentWindowOf(app core.App, readerID, workspaceID, kind, pageID string, after textIDCursor) ([]publicContentItem, []string, string, error) {
	items, ids, next, err := workspacePublicContentRowsOf(app, readerID, workspaceID, kind, pageID, after)
	if err == nil && readerID != "" {
		publicContentItemsAsRead(app, readerID, items)
	}
	return items, ids, next, err
}

// publicContentItemsAsRead puts public items' titles as a reader reads them.
func publicContentItemsAsRead(app core.App, readerID string, items []publicContentItem) {
	titles := make(map[string]*string, len(items))
	for index := range items {
		titles[items[index].PageID] = &items[index].Title
	}
	pageTitlesAsRead(app, readerID, titles, true)
	spans := pageTitlesSpansAsRead(app, readerID, titles)
	for index := range items {
		items[index].TitleSpans = spans[items[index].PageID]
	}
}

func workspacePublicContentRowsOf(app core.App, readerID, workspaceID, kind, pageID string, after textIDCursor) ([]publicContentItem, []string, string, error) {
	queries := map[string]string{
		"sites": `
			SELECT [[site]].[[id]] AS [[item_id]], [[site]].[[root_page_id]] AS [[page_id]], [[page]].[[title]], [[site]].[[slug]],
				[[site]].[[first_published_by]] AS [[publisher_id]], COALESCE([[person]].[[name]], '') AS [[publisher]],
				[[site]].[[first_published_at]] AS [[published_at]], '' AS [[expires_at]],
				NOT ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + ` AS [[in_trash]]
			FROM {{leafen_public_sites}} [[site]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[site]].[[root_page_id]]
				AND ([[site]].[[root_page_id]] = {:pageId} OR ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `)
			LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[site]].[[first_published_by]]
			WHERE [[site]].[[workspace_id]] = {:workspaceId} AND [[site]].[[published]] = 1
				AND ({:pageId} = '' OR [[site]].[[root_page_id]] = {:pageId})
				AND ({:afterId} = '' OR [[site]].[[first_published_at]] < {:afterAt}
					OR ([[site]].[[first_published_at]] = {:afterAt} AND [[site]].[[id]] < {:afterId}))
			ORDER BY [[site]].[[first_published_at]] DESC, [[site]].[[id]] DESC LIMIT {:limit}`,
		"links": `
			SELECT [[link]].[[id]] AS [[item_id]], [[link]].[[root_page_id]] AS [[page_id]], [[page]].[[title]], '' AS [[slug]],
				[[link]].[[first_published_by]] AS [[publisher_id]], COALESCE([[person]].[[name]], '') AS [[publisher]],
				[[link]].[[first_published_at]] AS [[published_at]], [[link]].[[expires_at]]
			FROM {{leafen_public_share_links}} [[link]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[link]].[[root_page_id]] AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
			LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[link]].[[first_published_by]]
			WHERE [[link]].[[workspace_id]] = {:workspaceId} AND [[link]].[[web_enabled]] = 1
				AND ({:pageId} = '' OR [[link]].[[root_page_id]] = {:pageId})
				AND ({:afterId} = '' OR [[link]].[[first_published_at]] < {:afterAt}
					OR ([[link]].[[first_published_at]] = {:afterAt} AND [[link]].[[id]] < {:afterId}))
			ORDER BY [[link]].[[first_published_at]] DESC, [[link]].[[id]] DESC LIMIT {:limit}`,
		"forms": `
			SELECT [[form]].[[id]] AS [[item_id]], [[form]].[[page_id]], [[page]].[[title]], '' AS [[slug]],
				[[form]].[[created_by]] AS [[publisher_id]], COALESCE([[person]].[[name]], '') AS [[publisher]],
				[[form]].[[created_at]] AS [[published_at]], '' AS [[expires_at]]
			FROM {{leafen_form_links}} [[form]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[form]].[[page_id]] AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
			LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[form]].[[created_by]]
			WHERE [[form]].[[workspace_id]] = {:workspaceId} AND [[form]].[[enabled]] = 1
				AND [[form]].[[audience]] = {:web}
				AND ({:pageId} = '' OR [[form]].[[page_id]] = {:pageId})
				AND ({:afterId} = '' OR [[form]].[[created_at]] < {:afterAt}
					OR ([[form]].[[created_at]] = {:afterAt} AND [[form]].[[id]] < {:afterId}))
			ORDER BY [[form]].[[created_at]] DESC, [[form]].[[id]] DESC LIMIT {:limit}`,
	}
	query, known := queries[kind]
	if !known {
		return nil, nil, "", errListWindowCursor
	}
	scan := workspacePublicContentWindowSize * 4
	rows := []storedPublicContentItem{}
	if err := app.DB().NewQuery(query).Bind(dbx.Params{
		"workspaceId": workspaceID, "web": formAudienceWeb, "pageId": pageID,
		"afterAt": after.At, "afterId": after.ID, "limit": scan + 1,
	}).All(&rows); err != nil {
		return nil, nil, "", err
	}
	more := len(rows) > scan
	if more {
		rows = rows[:scan]
	}
	now := time.Now().UTC()
	items, ids := []publicContentItem{}, []string{}
	for index, row := range rows {
		if expired, err := time.Parse(time.RFC3339Nano, row.ExpiresAt); err == nil && !expired.After(now) {
			continue
		}
		// Only the pages the reader can open, as Notion's list shows; a page
		// in the trash only to those who edit it.
		if readerID != "" {
			requirement := permissionWorkspaceRead
			if row.InTrash {
				requirement = permissionWorkspaceWrite
			}
			if _, err := authorizePage(app, readerID, row.PageID, requirement); err != nil {
				if isPermissionDenied(err) {
					continue
				}
				return nil, nil, "", err
			}
		}
		item := publicContentItem{
			PageID: row.PageID, Title: row.Title, PublishedAt: row.PublishedAt,
			PublishedBy: workspaceConnectionPerson{ID: row.PublisherID, Name: row.Publisher},
		}
		if row.Slug != "" {
			item.Path = "/site/" + row.Slug
		}
		items, ids = append(items, item), append(ids, row.ItemID)
		if len(items) == workspacePublicContentWindowSize && (index+1 < len(rows) || more) {
			return items, ids, encodeTextIDCursor(row.PublishedAt, row.ItemID), nil
		}
	}
	if more {
		last := rows[len(rows)-1]
		return items, ids, encodeTextIDCursor(last.PublishedAt, last.ItemID), nil
	}
	return items, ids, "", nil
}
