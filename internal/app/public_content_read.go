package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errPublicContentInvalid          = errors.New("public content invalid")
	errPublicContentNotFound         = errors.New("public content not found")
	errPublicContentRevisionConflict = errors.New("public content revision conflict")
)

const (
	publicContentCursorPages  = "pages"
	publicContentCursorGraph  = "graph"
	publicContentCursorSearch = "search"
)

type publicContentCursor struct {
	Scope          string `json:"scope"`
	AuthorityID    string `json:"authorityId"`
	SourceRevision int    `json:"sourceRevision"`
	PageID         string `json:"pageId,omitempty"`
	QueryHash      string `json:"queryHash,omitempty"`
	Value          string `json:"value"`
}

type storedPublicSiteConfiguration struct {
	ID                string `db:"id"`
	Slug              string `db:"slug"`
	Title             string `db:"title"`
	Description       string `db:"description"`
	Theme             string `db:"theme"`
	ShowBreadcrumbs   int    `db:"show_breadcrumbs"`
	FaviconFileID     string `db:"favicon_file_id"`
	ShowSearch        int    `db:"show_search"`
	ShowBranding      int    `db:"show_branding"`
	NavigationPageIDs string `db:"navigation_page_ids"`
	AllowDuplicate    int    `db:"allow_duplicate"`
	ShowShare         int    `db:"show_share"`
}

// readPublicContentSnapshot reads a public page's window; viewSecret is the
// secret a view link opens with, from the request's header.
func readPublicContentSnapshot(
	app core.App,
	input publicContentSnapshotRequest,
	viewSecret string,
) (publicContentSnapshotResponse, error) {
	response := publicContentSnapshotResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		response, err = readPublicContentSnapshotTransaction(txApp, input, viewSecret)
		return err
	})
	return response, err
}

// authorizePublicContentPage is what any read of public content checks first:
// the source is still public (a share or view link not removed or expired, a
// site still published), the page is in it and not in the trash. A visitor's
// open tab asks it again (the "check" action), so a link taken away closes
// the tab's page as Notion's does.
func authorizePublicContentPage(
	app core.App,
	input *publicContentSnapshotRequest,
	viewSecret string,
	now time.Time,
) (publicVisibilityAuthorization, *publicViewPresentation, *core.Record, error) {
	input.Action = strings.TrimSpace(input.Action)
	input.SourceKind = strings.TrimSpace(input.SourceKind)
	input.Token = strings.TrimSpace(input.Token)
	input.SiteSlug = strings.TrimSpace(input.SiteSlug)
	input.PageID = strings.TrimSpace(input.PageID)
	if input.SourceKind == publicContentSourceSite && input.SiteSlug == "" {
		return publicVisibilityAuthorization{}, nil, nil, errPublicContentNotFound
	}
	if err := publicContentSnapshotRefusal(*input); err != nil {
		return publicVisibilityAuthorization{}, nil, nil, err
	}

	requestedPageIDs := []string{}
	if input.PageID != "" {
		requestedPageIDs = append(requestedPageIDs, input.PageID)
	}
	var authorization publicVisibilityAuthorization
	var view *publicViewPresentation
	var err error
	if input.SourceKind == publicContentSourceView {
		authorization, view, err = authorizeViewLink(app, input.Token, viewSecret, requestedPageIDs, now)
	} else if input.SourceKind == publicContentSourceShare {
		authorization, err = authorizePublicSharePages(app, input.Token, requestedPageIDs, now)
	} else {
		authorization, err = authorizePublicSitePages(app, input.SiteSlug, requestedPageIDs, now)
	}
	if errors.Is(err, errPublicVisibilityNotFound) && input.PageID != "" && input.SourceKind != publicContentSourceView {
		authorization, err = authorizePublicLinkedContentPage(app, *input, now)
	}
	if err != nil {
		return publicVisibilityAuthorization{}, nil, nil, err
	}
	if input.SourceRevision != nil && *input.SourceRevision != authorization.SourceRevision {
		return publicVisibilityAuthorization{}, nil, nil, errPublicContentRevisionConflict
	}
	pageID := input.PageID
	if pageID == "" {
		pageID = authorization.RootPageID
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return publicVisibilityAuthorization{}, nil, nil, errPublicContentNotFound
		}
		return publicVisibilityAuthorization{}, nil, nil, err
	}
	// A page in the trash, or under one, is no longer published.
	if trashed, err := pageInTrashPath(app, pageID); err != nil {
		return publicVisibilityAuthorization{}, nil, nil, err
	} else if trashed {
		return publicVisibilityAuthorization{}, nil, nil, errPublicContentNotFound
	}
	return authorization, view, page, nil
}

// checkPublicContentSource answers a visitor's open tab: whether what it
// shows is still public, and at which revision. It reads no content and
// counts no read.
func checkPublicContentSource(app core.App, input publicContentCheckRequest, viewSecret string) (publicContentCheckResponse, error) {
	if action := strings.TrimSpace(input.Action); action != publicContentActionCheck {
		return publicContentCheckResponse{}, refuse(errPublicContentInvalid, "action is %s, not %q", publicContentActionCheck, action)
	}
	snapshot := publicContentSnapshotRequest{
		Action: publicContentActionSnapshot, SourceKind: input.SourceKind,
		Token: input.Token, SiteSlug: input.SiteSlug, PageID: input.PageID,
	}
	authorization, _, _, err := authorizePublicContentPage(app, &snapshot, viewSecret, time.Now().UTC())
	if err != nil {
		return publicContentCheckResponse{}, err
	}
	return publicContentCheckResponse{SourceRevision: authorization.SourceRevision}, nil
}

func readPublicContentSnapshotTransaction(
	app core.App,
	input publicContentSnapshotRequest,
	viewSecret string,
) (publicContentSnapshotResponse, error) {
	now := time.Now().UTC()
	authorization, view, page, err := authorizePublicContentPage(app, &input, viewSecret, now)
	if err != nil {
		return publicContentSnapshotResponse{}, err
	}
	pageID := page.Id

	pagesCursor, err := decodePublicContentCursor(
		input.PagesCursor, publicContentCursorPages, authorization, "", "",
	)
	if err != nil {
		return publicContentSnapshotResponse{}, refusalAt(err, "pagesCursor ")
	}
	pages, pagesNextCursor, pagesHasMore, err := loadPublicContentPages(app, authorization, pagesCursor, page)
	if err != nil {
		return publicContentSnapshotResponse{}, refusalAt(err, "pagesCursor ")
	}
	graphCursor, err := decodePublicContentCursor(
		input.GraphCursor, publicContentCursorGraph, authorization, pageID, "",
	)
	if err != nil {
		return publicContentSnapshotResponse{}, refusalAt(err, "graphCursor ")
	}
	graph, err := loadContentGraphWindow(
		app, authorization.Workspace.GetString("owner"), pageID, graphCursor, maxPublicContentGraphItems,
	)
	if err != nil {
		return publicContentSnapshotResponse{}, err
	}
	// A database page's first window carries its row, its properties and
	// the layout they are drawn in.
	var pageLayout *databasePageLayout
	if graphCursor == "" && page.GetString("data_source_id") != "" {
		rowItems, err := loadPublicDatabasePageItems(app, authorization.Workspace.GetString("owner"), pageID)
		if err != nil {
			return publicContentSnapshotResponse{}, err
		}
		graph.Items = append(rowItems, graph.Items...)
		var holders []string
		pageLayout, holders, err = loadPublicDatabasePageLayout(app, page.GetString("data_source_id"), func(pageIDs []string) bool {
			return publicContentPagesShown(app, input, viewSecret, pageIDs, now)
		})
		if err != nil {
			return publicContentSnapshotResponse{}, err
		}
		tabItems, err := loadPublicLayoutTabItems(app, authorization.Workspace.GetString("owner"), pageLayout, holders, graph.Items)
		if err != nil {
			return publicContentSnapshotResponse{}, err
		}
		graph.Items = append(graph.Items, tabItems...)
	}
	// A visitor sees each database property at its level for everyone
	// (database_property_access.go); no exception names them.
	graph.Items, err = redactGraphItemsForReader(app, "", graph.Items)
	if err != nil {
		return publicContentSnapshotResponse{}, err
	}
	graph.Items, err = projectPublicBlockText(app, pageID, graphItemsAsTheyStand(graph.Items))
	if err != nil {
		return publicContentSnapshotResponse{}, err
	}
	graph.Items, err = projectPublicPageLinks(app, authorization, pageID, graph.Items, now)
	if err != nil {
		return publicContentSnapshotResponse{}, err
	}
	graph.Items, err = projectPublicMentions(app, authorization, pageID, graph.Items, now)
	if err != nil {
		return publicContentSnapshotResponse{}, err
	}
	graph.Items, err = projectPublicCustomEmoji(app, input.SourceKind, authorization, graph.Items, now)
	if err != nil {
		return publicContentSnapshotResponse{}, err
	}
	graphNextCursor, err := wrapPublicContentGraphCursor(authorization, pageID, graph.NextCursor)
	if err != nil {
		return publicContentSnapshotResponse{}, err
	}

	pageResponse, err := pageRecordResponseWithWiki(app, page)
	if err != nil {
		return publicContentSnapshotResponse{}, err
	}
	// Its names as its visitor reads them (public_content_names.go).
	closer := publicVisitorCloser(app, authorization, now, pageID)
	publicPageAsRead(app, closer, &pageResponse)
	publicSummariesAsRead(app, closer, pages)
	publicGraphNamesAsRead(graph.Items, closer)
	// Their titles' emoji show to the visitor (public_content_custom_emoji.go).
	customEmoji, err := publicReadTitleCustomEmoji(app, input.SourceKind, authorization, now, &pageResponse, pages)
	if err != nil {
		return publicContentSnapshotResponse{}, err
	}
	response := publicContentSnapshotResponse{
		SourceKind: input.SourceKind, AuthorityID: authorization.AuthorityID,
		RootPageID: authorization.RootPageID, SourceRevision: authorization.SourceRevision,
		Page: pageResponse, Pages: pages, PagesNextCursor: pagesNextCursor,
		PagesHasMore: pagesHasMore, Graph: graph.Items,
		GraphNextCursor: graphNextCursor, GraphHasMore: graph.HasMore, View: view, CustomEmoji: customEmoji,
		PageLayout: pageLayout,
	}
	if input.SourceKind == publicContentSourceSite {
		response.Site, err = loadPublicSiteConfiguration(app, authorization)
		if err != nil {
			return publicContentSnapshotResponse{}, err
		}
	}
	// A web link that lets the organization's people comment or edit says
	// so, for them to work in the page in the app (page_link_access.go).
	if input.SourceKind == publicContentSourceShare {
		var role string
		if err := app.DB().NewQuery(`SELECT [[role]] FROM {{leafen_public_share_links}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": authorization.AuthorityID}).Row(&role); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return publicContentSnapshotResponse{}, err
		}
		if role == "comment" || role == "edit" {
			response.LinkRole = role
		}
	}
	return response, nil
}

func searchPublicSiteContent(
	app core.App,
	input publicContentSearchRequest,
) (publicContentSearchResponse, error) {
	response := publicContentSearchResponse{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		var err error
		response, err = searchPublicSiteContentTransaction(txApp, input)
		return err
	})
	return response, err
}

func searchPublicSiteContentTransaction(
	app core.App,
	input publicContentSearchRequest,
) (publicContentSearchResponse, error) {
	input.Action = strings.TrimSpace(input.Action)
	input.SiteSlug = strings.TrimSpace(input.SiteSlug)
	input.Query = strings.TrimSpace(input.Query)
	if input.SiteSlug == "" {
		return publicContentSearchResponse{}, errPublicContentNotFound
	}
	if err := publicContentSearchRefusal(input); err != nil {
		return publicContentSearchResponse{}, err
	}
	authorization, err := authorizePublicSitePages(app, input.SiteSlug, nil, time.Now().UTC())
	if err != nil {
		return publicContentSearchResponse{}, err
	}
	if input.SourceRevision != authorization.SourceRevision {
		return publicContentSearchResponse{}, errPublicContentRevisionConflict
	}
	configuration, err := loadPublicSiteConfiguration(app, authorization)
	if err != nil {
		return publicContentSearchResponse{}, err
	}
	if !configuration.ShowSearch {
		return publicContentSearchResponse{}, errPublicContentNotFound
	}
	queryHash := publicContentQueryHash(input.Query)
	cursor, err := decodePublicContentCursor(
		input.Cursor, publicContentCursorSearch, authorization, "", queryHash,
	)
	if err != nil {
		return publicContentSearchResponse{}, refusalAt(err, "cursor ")
	}
	rows, err := loadPublicContentSearchRows(app, authorization, input.Query, cursor)
	if err != nil {
		return publicContentSearchResponse{}, err
	}
	hasMore := len(rows) > maxPublicContentSearchResults
	if hasMore {
		rows = rows[:maxPublicContentSearchResults]
	}
	// Each found as its title is searched, named as the visitor reads it
	// (public_content_names.go).
	now := time.Now().UTC()
	results := publicSearchFound(app, publicContentPageSummaries(rows), rows, input.Query)
	publicSummariesAsRead(app, publicVisitorCloser(app, authorization, now), results)
	customEmoji, err := publicReadTitleCustomEmoji(app, publicContentSourceSite, authorization, now, nil, results)
	if err != nil {
		return publicContentSearchResponse{}, err
	}
	response := publicContentSearchResponse{
		SearchResults: results, HasMore: hasMore,
		SourceRevision: authorization.SourceRevision, CustomEmoji: customEmoji,
	}
	if hasMore {
		next, err := encodePublicContentCursor(publicContentCursor{
			Scope: publicContentCursorSearch, AuthorityID: authorization.AuthorityID,
			SourceRevision: authorization.SourceRevision, QueryHash: queryHash,
			Value: rows[len(rows)-1].SortKey,
		})
		if err != nil {
			return publicContentSearchResponse{}, err
		}
		response.NextCursor = &next
	}
	return response, nil
}

func loadPublicContentSearchRows(
	app core.App,
	authorization publicVisibilityAuthorization,
	query string,
	cursor string,
) ([]storedPublicContentPage, error) {
	rows := []storedPublicContentPage{}
	err := app.DB().NewQuery(`
		SELECT ` + publicContentPageSummaryColumns + `,
			[[member]].[[position]] || char(0) || [[member]].[[page_id]] AS [[sort_key]]
		FROM {{leafen_public_share_graph_memberships}} [[member]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[member]].[[page_id]]
		WHERE [[member]].[[share_link_id]] = {:authorityId}
			AND [[member]].[[root_page_id]] = {:rootPageId}
			AND [[member]].[[source_revision]] = {:sourceRevision}
			AND [[member]].[[state]] = 'complete'
			AND [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[owner]] = {:ownerId}
			AND [[page]].[[notion_import_staging]] = 0 AND [[page]].[[in_trash]] = 0
			AND instr(lower([[page]].[[title]]), lower({:query})) > 0
			AND ([[member]].[[position]] || char(0) || [[member]].[[page_id]]) > {:cursor}
		ORDER BY [[member]].[[position]], [[member]].[[page_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"authorityId": authorization.AuthorityID, "rootPageId": authorization.RootPageID,
		"sourceRevision": authorization.SourceRevision, "workspaceId": authorization.WorkspaceID,
		"ownerId": authorization.Workspace.GetString("owner"), "query": query,
		"cursor": cursor, "limit": maxPublicContentSearchResults + 1,
	}).All(&rows)
	return rows, err
}

func loadPublicSiteConfiguration(
	app core.App,
	authorization publicVisibilityAuthorization,
) (*publicSiteConfiguration, error) {
	if authorization.SiteID == "" {
		return nil, errPublicContentNotFound
	}
	stored := storedPublicSiteConfiguration{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[slug]], [[title]], [[description]], [[theme]],
			[[show_breadcrumbs]], [[show_search]], [[show_branding]], [[navigation_page_ids]],
			[[favicon_file_id]], [[allow_duplicate]], [[show_share]]
		FROM {{leafen_public_sites}}
		WHERE [[id]] = {:siteId} AND [[share_link_id]] = {:authorityId}
			AND [[workspace_id]] = {:workspaceId} AND [[root_page_id]] = {:rootPageId}
			AND [[published]] = 1
		LIMIT 2
	`).Bind(dbx.Params{
		"siteId": authorization.SiteID, "authorityId": authorization.AuthorityID,
		"workspaceId": authorization.WorkspaceID, "rootPageId": authorization.RootPageID,
	}).One(&stored)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errPublicContentNotFound
		}
		return nil, err
	}
	if len(stored.NavigationPageIDs) > maxPublicContentRequestBytes {
		return nil, errPublicContentNotFound
	}
	navigationPageIDs := []string{}
	if err := json.Unmarshal([]byte(stored.NavigationPageIDs), &navigationPageIDs); err != nil ||
		len(navigationPageIDs) > maxPublicContentSnapshotPages {
		return nil, errPublicContentNotFound
	}
	if len(navigationPageIDs) > 0 {
		_, err := authorizePublicVisibilityPages(app, storedPublicShareAuthority{
			LinkID: authorization.AuthorityID, WorkspaceID: authorization.WorkspaceID,
			RootPageID: authorization.RootPageID, SourceRevision: authorization.SourceRevision,
		}, navigationPageIDs)
		if err != nil {
			if errors.Is(err, errPublicVisibilityNotFound) {
				return nil, errPublicContentNotFound
			}
			return nil, err
		}
	}
	// A site with no title of its own goes by its page's, as the page is
	// named now (2026-10-16: it kept the name the page had when published).
	title := strings.TrimSpace(stored.Title)
	if title == "" {
		title, _ = publicPageTitleAndCover(app, authorization, authorization.RootPageID)
	}
	return &publicSiteConfiguration{
		ID: stored.ID, Slug: stored.Slug, Title: title,
		Description: stored.Description, Theme: stored.Theme,
		ShowBreadcrumbs: stored.ShowBreadcrumbs == 1, ShowSearch: stored.ShowSearch == 1,
		ShowBranding: stored.ShowBranding == 1, NavigationPageIDs: navigationPageIDs,
		FaviconURL:     publicSiteFaviconURL(stored.Slug, stored.FaviconFileID),
		AllowDuplicate: stored.AllowDuplicate == 1,
		ShowShare:      stored.ShowShare == 1,
	}, nil
}

func encodePublicContentCursor(cursor publicContentCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	if len(encoded) > maxPublicContentCursorBytes {
		return "", errPublicContentInvalid
	}
	return encoded, nil
}

// errPublicContentCursorNotGiven is a cursor the last answer for this page,
// at this revision and query, didn't give: one from another page, an older
// revision, or written by hand.
var errPublicContentCursorNotGiven = refuse(errPublicContentInvalid,
	"isn't one the last answer gave for this page, revision and query: start again without it")

func decodePublicContentCursor(
	input string,
	scope string,
	authorization publicVisibilityAuthorization,
	pageID string,
	queryHash string,
) (string, error) {
	if input == "" {
		return "", nil
	}
	if len(input) > maxPublicContentCursorBytes {
		return "", refuse(errPublicContentInvalid, "is at most %d characters", maxPublicContentCursorBytes)
	}
	payload, err := base64.RawURLEncoding.DecodeString(input)
	if err != nil {
		return "", errPublicContentCursorNotGiven
	}
	var cursor publicContentCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Value == "" ||
		cursor.Scope != scope || cursor.AuthorityID != authorization.AuthorityID ||
		cursor.SourceRevision != authorization.SourceRevision || cursor.PageID != pageID ||
		cursor.QueryHash != queryHash {
		return "", errPublicContentCursorNotGiven
	}
	canonical, err := encodePublicContentCursor(cursor)
	if err != nil || canonical != input {
		return "", errPublicContentCursorNotGiven
	}
	return cursor.Value, nil
}

func wrapPublicContentGraphCursor(
	authorization publicVisibilityAuthorization,
	pageID string,
	inner *string,
) (*string, error) {
	if inner == nil {
		return nil, nil
	}
	raw, err := decodeGraphCursor(*inner)
	if err != nil {
		return nil, err
	}
	encoded, err := encodePublicContentCursor(publicContentCursor{
		Scope: publicContentCursorGraph, AuthorityID: authorization.AuthorityID,
		SourceRevision: authorization.SourceRevision, PageID: pageID, Value: raw,
	})
	if err != nil {
		return nil, err
	}
	return &encoded, nil
}

func publicContentQueryHash(query string) string {
	digest := sha256.Sum256([]byte(query))
	return hex.EncodeToString(digest[:])
}
