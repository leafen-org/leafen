package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The path above a page, as Notion's bar, search and lists draw it (seen
// 2026-11-10): the pages from the top and, above a database's page, its
// database — a full-page one by its page, an inline one by its block — then,
// while the database holds several sources, the page's source ("… / DB /
// 소스 / 행"); a page under a database's page goes on up through it. The
// walk is fetch's (mcp_fetch_ancestors.go), so a path stops at the first
// piece its reader can't read. The app asks for the pages whose path its
// own page list can't draw: a database's pages and the pages under them.
// Each piece is named as its reader reads it (page_name_mentions.go): a
// mention in a title or a database's name closed to them in marks.

const (
	pagePathsPath = "/workspaces/{workspaceId}/page-paths"
	pagePathsMax  = 50

	errorCodePagePathFailed = "LEAFEN_PAGE_PATH_FAILED"

	pagePathPiecePage     = "page"
	pagePathPieceDatabase = "database"
	pagePathPieceSource   = "source"
)

// pagePathPieceWire is one piece of a path, from the top down.
type pagePathPieceWire struct {
	// Kind is page, database or source.
	Kind string `json:"kind"`
	// ID is a page's own, a full-page database's page's, an inline
	// database's block's or a source's.
	ID string `json:"id"`
	// PageID is the page itself, or the page holding the database.
	PageID string `json:"pageId"`
	// BlockID is an inline database's block, for a database or its source.
	BlockID string `json:"blockId,omitempty"`
	// ViewID is the view a database or source opens at.
	ViewID string `json:"viewId,omitempty"`
	Title  string `json:"title"`
	// TitleSpans are the title's, as its reader reads them, while it holds a
	// mention: a plain title says its mentions by them (closedMentions.ts).
	TitleSpans  []graphTextSpan `json:"titleSpans,omitempty"`
	Icon        string          `json:"icon"`
	IconType    string          `json:"iconType"`
	TeamspaceID string          `json:"teamspaceId,omitempty"`
	// Row says the page is a database's page.
	Row bool `json:"row,omitempty"`
	// UntitledName is what a database's page without a title is called, as
	// its own title's place calls it (pagePathUntitledName).
	UntitledName string `json:"untitledName,omitempty"`
	ArchivedAt   string `json:"archivedAt,omitempty"`
	// SourceTitles names an inline database's sources while it holds several.
	SourceTitles []databaseSourceTitle `json:"sourceTitles,omitempty"`
	// TemplateOf names, for a database template's page, the source it is a
	// template of: a page under it draws the band of a template being edited,
	// as Notion's sub-page of a template does (seen 2026-11-16).
	TemplateOf string `json:"templateOf,omitempty"`
}

type pagePathsWire struct {
	// Paths holds, by page, the pieces above each page the reader can read.
	Paths map[string][]pagePathPieceWire `json:"paths"`
}

func registerPagePathRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+pagePathsPath, readPagePathsRoute)
}

func readPagePathsRoute(event *core.RequestEvent) error {
	event.Response.Header().Set("Cache-Control", "no-store")
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range strings.Split(event.Request.URL.Query().Get("ids"), ",") {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if workspaceID == "" || len(ids) == 0 || len(ids) > pagePathsMax {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Ask for the paths of 1 to 50 pages.")
	}
	response := pagePathsWire{Paths: map[string][]pagePathPieceWire{}}
	for _, id := range ids {
		authorization, err := authorizePage(event.App, user.Id, id, permissionWorkspaceRead)
		if isPermissionDenied(err) {
			continue
		}
		if err == nil && authorization.Page.GetString("workspace") != workspaceID {
			continue
		}
		var pieces []pagePathPieceWire
		if err == nil {
			pieces, err = pagePathAboveFor(event.Request.Context(), event.App, user.Id, authorization.Page)
		}
		if err != nil {
			return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePagePathFailed,
				"Couldn't read where the page is.", "page_path", "read", errorCodePagePathFailed, err)
		}
		response.Paths[id] = pieces
	}
	return event.JSON(http.StatusOK, response)
}

// pagePathAbove lists the pieces above a page its reader can read, from the top down.
func pagePathAbove(app core.App, principalID string, page *core.Record) ([]pagePathPieceWire, error) {
	return pagePathAboveFor(context.Background(), app, principalID, page)
}

// pagePathAboveFor is pagePathAbove for the reader whose own request ctx
// carries, its untitled rows named in its language.
func pagePathAboveFor(ctx context.Context, app core.App, principalID string, page *core.Record) ([]pagePathPieceWire, error) {
	ancestors, err := mcpPageAncestors(app, mcpAuthority{PrincipalID: principalID}, page)
	if err != nil {
		return nil, err
	}
	closer := readerCloser(app, principalID)
	pieces := []pagePathPieceWire{}
	for index := len(ancestors) - 1; index >= 0; index-- {
		ancestor := ancestors[index]
		if ancestor.Page == nil {
			continue
		}
		switch ancestor.Kind {
		case mcpFetchAncestorPage:
			record := ancestor.Page
			title := pageTitleAsReadBy(app, closer, record.Id, record.GetString("title"))
			pieces = append(pieces, pagePathPieceWire{
				Kind: pagePathPiecePage, ID: record.Id, PageID: record.Id,
				Title: title, TitleSpans: pageTitleSpansAsReadBy(app, closer, record.Id, title),
				Icon: record.GetString("icon"), IconType: record.GetString("icon_type"),
				TeamspaceID: record.GetString("teamspace_id"), Row: isDatabaseRowPageRecord(record),
				ArchivedAt: strings.TrimSpace(record.GetString("archived_at")),
			})
			if isDatabaseTemplatePageRecord(record) {
				pieces[len(pieces)-1].TemplateOf = record.GetString("data_source_id")
			}
			if strings.TrimSpace(title) == "" && isDatabaseRowPageRecord(record) {
				if pieces[len(pieces)-1].UntitledName, err = pagePathUntitledName(ctx, app, principalID, record); err != nil {
					return nil, err
				}
			}
		case mcpFetchAncestorDatabase:
			// A wiki is its page: its database is no piece of its own, as
			// Notion's path reads "… / 〈wiki〉 / 〈page〉" (seen 2026-11-11).
			if wiki, err := pagePathWikiContainer(app, ancestor.ContainerID); err != nil || wiki {
				if err != nil {
					return nil, err
				}
				continue
			}
			piece, err := pagePathDatabasePiece(app, closer, ancestor)
			if err != nil {
				return nil, err
			}
			pieces = append(pieces, piece)
		case mcpFetchAncestorDataSource:
			piece, several, err := pagePathSourcePiece(app, closer, ancestor)
			if err != nil {
				return nil, err
			}
			if several {
				pieces = append(pieces, piece)
			}
		}
	}
	return pieces, nil
}

// pagePathContainerSource is a source a database holds, in its order.
type pagePathContainerSource struct {
	ID       string `db:"id"`
	Title    string `db:"title"`
	Spans    string `db:"title_spans"`
	Icon     string `db:"icon"`
	IconType string `db:"icon_type"`
	// Gone says it is in the trash with its views, Linked that another
	// database owns it (databaseNamingSources).
	Gone   bool `db:"gone"`
	Linked bool `db:"linked"`
}

// name is the source's name as a reader's closer reads it, with its spans
// so read.
func (source pagePathContainerSource) name(closer func() searchMentionCloser) databaseNameRead {
	return databaseNameSpansAsRead(closer, source.Title, decodeStoredTextSpans(source.Spans))
}

// pagePathContainer reads a database's block and its sources in order, a
// source it keeps in the trash where it stands among them: Notion's path
// names that source over its pages and counts it in its database's sources
// (seen 2026-11-12, a source sent to the trash through the API: "… / C one
// 및 C second / C second / C second row 1", and "… / C one 및 C second / C
// one / C one row 1" beside it). A database in the trash itself keeps its
// own sources there too, which name it over its pages as Notion's path does
// (seen 2026-11-24 in Chrome at 1440, two tabs: a page of an inline database
// whose block alone went there "… / 〈database〉 / 〈page〉", as one of a
// full-page database sent there and one under a page sent there read); a
// source it shows from another database leaves while that one is in the
// trash. One sent there with its views from 데이터 소스 관리 leaves too, as it
// leaves its database's name (databaseNamingSources): Notion's path names a
// database of two so left with one by that one (seen 2026-11-19).
func pagePathContainer(app core.App, containerID string) (string, []pagePathContainerSource, error) {
	var blockID string
	if err := app.DB().NewQuery(`SELECT [[block_id]] FROM {{leafen_database_containers}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": containerID}).Row(&blockID); err != nil {
		return "", nil, err
	}
	sources := []pagePathContainerSource{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]], [[source]].[[title]], [[source]].[[title_spans]], [[source]].[[icon]], [[source]].[[icon_type]],
			` + databaseSourceGoneWithViewsSQL("{:id}", "[[place]].[[data_source_id]]") + ` AS [[gone]],
			[[place]].[[source_container_id]] != '' AS [[linked]]
		FROM {{leafen_database_container_sources}} [[place]]
		JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]]=[[place]].[[data_source_id]]
		WHERE [[place]].[[container_id]]={:id}
			AND ([[place]].[[source_container_id]] = '' OR ` + databaseSourceShownSQL("[[source]].[[id]]") + `)
		ORDER BY [[place]].[[position]], [[place]].[[data_source_id]]
	`).Bind(dbx.Params{"id": containerID}).All(&sources)
	return blockID, databaseNamingSources(sources,
		func(source pagePathContainerSource) bool { return source.Gone }, func(source pagePathContainerSource) bool { return source.Linked }), err
}

// pagePathView is the first view of a database, or of one of its sources.
func pagePathView(app core.App, containerID, sourceID string) string {
	var viewID string
	_ = app.DB().NewQuery(`
		SELECT [[place]].[[view_id]] FROM {{leafen_database_container_views}} [[place]]
		JOIN {{leafen_database_views}} [[view]] ON [[view]].[[id]]=[[place]].[[view_id]]
		WHERE [[place]].[[container_id]]={:id} AND ({:source}='' OR [[view]].[[data_source_id]]={:source})
		ORDER BY [[place]].[[position]], [[place]].[[view_id]] LIMIT 1
	`).Bind(dbx.Params{"id": containerID, "source": sourceID}).Row(&viewID)
	return viewID
}

// pagePathDatabasePiece is a database as a piece: a full-page one by its
// page's title and icon, an inline one by its first source's, one of several
// sources by its own name, none while it has none
// (inline_database_own_name.go), as the sidebar names it
// (sidebar_inline_databases.go).
func pagePathDatabasePiece(app core.App, closer func() searchMentionCloser, ancestor mcpFetchAncestor) (pagePathPieceWire, error) {
	holder := ancestor.Page
	blockID, sources, err := pagePathContainer(app, ancestor.ContainerID)
	if err != nil {
		return pagePathPieceWire{}, err
	}
	title := pageTitleAsReadBy(app, closer, holder.Id, holder.GetString("title"))
	piece := pagePathPieceWire{
		Kind: pagePathPieceDatabase, ID: holder.Id, PageID: holder.Id, ViewID: pagePathView(app, ancestor.ContainerID, ""),
		Title: title, Icon: holder.GetString("icon"), IconType: holder.GetString("icon_type"),
	}
	if holder.GetString("kind") == pageKindDatabase {
		piece.TitleSpans = pageTitleSpansAsReadBy(app, closer, holder.Id, title)
		return piece, nil
	}
	piece.ID, piece.BlockID, piece.Title, piece.Icon, piece.IconType = blockID, blockID, "", "", "none"
	if len(sources) > 0 {
		name := sources[0].name(closer)
		piece.Title, piece.TitleSpans, piece.Icon, piece.IconType = name.Title, name.Spans, sources[0].Icon, sources[0].IconType
	}
	if len(sources) > 1 {
		for _, source := range sources {
			piece.SourceTitles = append(piece.SourceTitles, databaseSourceTitle{Title: source.name(closer).Title, Linked: source.Linked})
		}
		names, err := inlineDatabaseNamesOfSeveral(app, closer, []string{ancestor.ContainerID})
		if err != nil {
			return pagePathPieceWire{}, err
		}
		if name, named := names[ancestor.ContainerID]; named {
			piece.Title, piece.TitleSpans = name.Title, name.Spans
		}
	}
	return piece, nil
}

// pagePathSourcePiece is a page's source as a piece, and whether its
// database holds several, which alone draws it.
func pagePathSourcePiece(app core.App, closer func() searchMentionCloser, ancestor mcpFetchAncestor) (pagePathPieceWire, bool, error) {
	if ancestor.ContainerID == "" {
		return pagePathPieceWire{}, false, nil
	}
	blockID, sources, err := pagePathContainer(app, ancestor.ContainerID)
	if err != nil || len(sources) < 2 {
		return pagePathPieceWire{}, false, err
	}
	piece := pagePathPieceWire{
		Kind: pagePathPieceSource, ID: ancestor.Source.ID, PageID: ancestor.Page.Id,
		ViewID: pagePathView(app, ancestor.ContainerID, ancestor.Source.ID), Title: ancestor.Source.Title, IconType: "none",
	}
	if ancestor.Page.GetString("kind") != pageKindDatabase {
		piece.BlockID = blockID
	}
	for _, source := range sources {
		if source.ID == ancestor.Source.ID {
			name := source.name(closer)
			piece.Title, piece.TitleSpans, piece.Icon, piece.IconType = name.Title, name.Spans, source.Icon, source.IconType
		}
	}
	return piece, true, nil
}

// pagePathWikiContainer reports whether a database is a wiki's own.
func pagePathWikiContainer(app core.App, containerID string) (bool, error) {
	var wiki bool
	err := app.DB().NewQuery(`SELECT ` + wikiDatabaseContainerSQL("[[container]]") + `
		FROM {{leafen_database_containers}} [[container]] WHERE [[container]].[[id]] = {:id}`).
		Bind(dbx.Params{"id": containerID}).Row(&wiki)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return wiki, err
}

// pagePathUntitledName is what a database's page without a title is called
// in the path above a page under it, as its own title's place, window and
// path call it (web untitledRowPageName) — Notion's sub-page of an untitled
// row of a task database reads "… / 새 작업 / 새 페이지" (seen 2026-11-26): a
// row by its database's word for its untitled rows, its kind's last, in its
// reader's language (databaseUntitledRowNameFor); a template by the name it
// was made with, else by that word too.
func pagePathUntitledName(ctx context.Context, app core.App, principalID string, record *core.Record) (string, error) {
	if isDatabaseTemplatePageRecord(record) {
		if name := strings.TrimSpace(databaseTemplatePageTitle(app, record.Id)); name != "" {
			return name, nil
		}
	}
	return databaseUntitledRowNameFor(ctx, app, principalID, record.GetString("data_source_id"))
}
