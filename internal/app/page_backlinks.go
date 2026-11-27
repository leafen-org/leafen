package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

var (
	errPageBacklinksNotFound        = errors.New("page backlinks authority not found")
	errPageBacklinksCursorInvalid   = errors.New(errorCodePageBacklinksCursorInvalid)
	errPageBacklinksRevisionChanged = errors.New("page backlinks revision changed")
)

const (
	pageBacklinksReadWindow   = pageBacklinksPageSize + 1
	pageBacklinksCursorAfter  = "after"
	pageBacklinksCursorBefore = "before"
)

type storedPageBacklink struct {
	BlockID        string `db:"block_id"`
	SourcePageID   string `db:"source_page_id"`
	ParentID       string `db:"parent_id"`
	SourcePosition string `db:"source_position"`
	Type           string `db:"type"`
	Content        string `db:"content"`
	CreatedAt      int64  `db:"created_at"`
	UpdatedAt      int64  `db:"updated_at"`
	// A database page body names the page and database that hold the item.
	RowContainerPageID string `db:"row_container_page_id"`
	RowDataSourceID    string `db:"row_data_source_id"`
	// RowDatabase is the inline database between that page and the item in
	// the item's path; none for a full-page database (database_row_location.go).
	RowDatabase inlineDatabaseLocation `db:"-"`
}

type storedPageBacklinkAncestor struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
}

func registerPageBacklinksRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+pageBacklinksReadPath, readPageBacklinks)
}

func readPageBacklinks(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	targetPageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	limit, err := parsePageBacklinksLimit(event.Request.URL.Query().Get("limit"))
	if err != nil || targetPageID == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageBacklinksInvalid, "Check the backlink query range.")
	}
	response, err := loadPageBacklinksWindowFor(
		event.Request.Context(), event.App, user.Id, targetPageID, event.Request.URL.Query().Get("cursor"), limit,
	)
	switch {
	case errors.Is(err, errPageBacklinksNotFound) || isPermissionDenied(err):
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	case errors.Is(err, errPageBacklinksRevisionChanged):
		return apiError(event, http.StatusConflict, errorCodePageBacklinksRevisionChanged, "The backlink results changed. Load them again from the start.")
	case errors.Is(err, errPageBacklinksCursorInvalid):
		return apiError(event, http.StatusBadRequest, errorCodePageBacklinksCursorInvalid, "Check the backlink query position.")
	case err != nil:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodePageBacklinksFailed,
			"Couldn't load the backlinks.", "page_backlinks", "read",
			errorCodePageBacklinksFailed, err,
		)
	default:
		// What names a page closed to this reader (content_graph_reader_mentions.go).
		backlinksAsRead(event.App, user.Id, &response)
		return event.JSON(http.StatusOK, response)
	}
}

func parsePageBacklinksLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return pageBacklinksPageSize, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > pageBacklinksPageSize {
		return 0, errors.New(errorCodePageBacklinksInvalid)
	}
	return limit, nil
}

func loadPageBacklinksWindow(app core.App, principalID string, targetPageID string, rawCursor string, limit int) (pageBacklinksResponse, error) {
	return loadPageBacklinksWindowFor(context.Background(), app, principalID, targetPageID, rawCursor, limit)
}

// loadPageBacklinksWindowFor is the backlinks window for the reader whose own
// request ctx carries, its untitled items named in its language.
func loadPageBacklinksWindowFor(
	ctx context.Context,
	app core.App,
	principalID string,
	targetPageID string,
	rawCursor string,
	limit int,
) (pageBacklinksResponse, error) {
	rawCursor = strings.TrimSpace(rawCursor)
	response := pageBacklinksResponse{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		workspaceID, ownerID, err := authorizePageBacklinksTarget(txApp, principalID, targetPageID)
		if err != nil {
			return err
		}
		cursor, err := decodePageBacklinksCursor(
			rawCursor, principalID, workspaceID, targetPageID, limit,
		)
		if err != nil {
			return err
		}
		var revision int
		if err := txApp.DB().NewQuery(`
			SELECT [[revision]] FROM {{leafen_workspace_content_revisions}}
			WHERE [[workspace_id]]={:workspaceId}
		`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&revision); err != nil {
			return err
		}
		if cursor.Revision != 0 && cursor.Revision != revision {
			return errPageBacklinksRevisionChanged
		}
		rows, err := loadPageBacklinkRows(
			txApp, ownerID, workspaceID, targetPageID, cursor, limit,
		)
		if err != nil {
			return err
		}
		hasPrevious := false
		hasMore := false
		if cursor.Direction == pageBacklinksCursorBefore {
			hasPrevious = len(rows) > limit
			if hasPrevious {
				rows = rows[:limit]
			}
			reversePageBacklinkRows(rows)
			hasMore = len(rows) > 0
		} else {
			hasMore = len(rows) > limit
			if hasMore {
				rows = rows[:limit]
			}
			hasPrevious = rawCursor != "" && len(rows) > 0
		}
		// The window's positions come from every row it read, before rows
		// drop out below.
		window := slices.Clone(rows)
		// A source in the trash with a page above it — a sub-page of a
		// trashed page, a database page whose database's page is in the
		// trash — drops out.
		rows, err = withoutTrashedPageBacklinkSources(txApp, rows)
		if err != nil {
			return err
		}
		// Backlinks from pages the viewer cannot read show only that there
		// are such pages.
		access, err := loadPageBacklinkSourceAccess(
			txApp, principalID, workspaceID, pageBacklinkSourceIDs(rows),
		)
		if err != nil {
			return err
		}
		rows, privateSources := access.split(rows)
		readThrough := pageBacklinkPathIDs(rows)
		pages, err := loadPageBacklinkPages(
			txApp, ownerID, workspaceID,
			append(readThrough, targetPageID),
		)
		if err != nil {
			return err
		}
		if pages, err = access.readablePages(txApp, principalID, pages); err != nil {
			return err
		}
		rowPages, err := loadPageBacklinkRowPages(ctx, txApp, principalID, workspaceID, rows)
		if err != nil {
			return err
		}
		if err := withPageBacklinkRowDatabases(ctx, txApp, principalID, workspaceID, rows); err != nil {
			return err
		}
		pages = append(pages, rowPages...)
		listed := make([]*pageResponse, len(pages))
		for index := range pages {
			listed[index] = &pages[index]
		}
		if err := withPageContents(txApp, listed); err != nil {
			return err
		}
		if rows, err = withPageBacklinkText(txApp, rows); err != nil {
			return err
		}
		response = pageBacklinksResponse{
			WorkspaceID: workspaceID, TargetPageID: targetPageID,
			SnapshotRevision: revision, Blocks: pageBacklinkBlockWires(rows),
			Pages: pages, PrivateSources: privateSources, HasPrevious: hasPrevious, HasMore: hasMore,
		}
		if hasPrevious && len(window) > 0 {
			previous, err := encodePageBacklinksRowCursor(
				principalID, workspaceID, targetPageID, revision, window[0], limit,
				pageBacklinksCursorBefore,
			)
			if err != nil {
				return err
			}
			response.PreviousCursor = &previous
		}
		if hasMore && len(window) > 0 {
			last := window[len(window)-1]
			next, err := encodePageBacklinksRowCursor(
				principalID, workspaceID, targetPageID, revision, last, limit,
				pageBacklinksCursorAfter,
			)
			if err != nil {
				return err
			}
			response.NextCursor = &next
		}
		return nil
	})
	return response, err
}

func loadPageBacklinkRows(
	app core.App,
	ownerID string,
	workspaceID string,
	targetPageID string,
	cursor pageBacklinksCursor,
	limit int,
) ([]storedPageBacklink, error) {
	rows := []storedPageBacklink{}
	window := limit + 1
	if window > pageBacklinksReadWindow {
		return nil, errors.New(errorCodePageBacklinksInvalid)
	}
	comparison := `
		[[reference]].[[source_page_id]]>{:sourcePageId}
		OR ([[reference]].[[source_page_id]]={:sourcePageId}
			AND [[reference]].[[source_position]]>{:sourcePosition})
		OR ([[reference]].[[source_page_id]]={:sourcePageId}
			AND [[reference]].[[source_position]]={:sourcePosition}
			AND [[reference]].[[block_id]]>{:blockId})`
	ordering := `ORDER BY [[source_page_id]], [[source_position]], [[block_id]]`
	if cursor.Direction == pageBacklinksCursorBefore {
		comparison = `
		[[reference]].[[source_page_id]]<{:sourcePageId}
		OR ([[reference]].[[source_page_id]]={:sourcePageId}
			AND [[reference]].[[source_position]]<{:sourcePosition})
		OR ([[reference]].[[source_page_id]]={:sourcePageId}
			AND [[reference]].[[source_position]]={:sourcePosition}
			AND [[reference]].[[block_id]]<{:blockId})`
		ordering = `ORDER BY [[source_page_id]] DESC, [[source_position]] DESC, [[block_id]] DESC`
	}
	// Pages and database page bodies both reference pages, as do database
	// pages' cells and pages' titles (page_field_references_schema.go); they
	// are read in one order. A database page whose database is in the trash
	// or deleted with its block is no source. The page's own parent isn't one: its mentions
	// and page links of the page are no backlinks, as Notion's (seen
	// 2026-10-31).
	notParent := `[[reference]].[[source_page_id]] IS NOT (SELECT [[parent_id]] FROM {{leafen_pages}} WHERE [[id]]={:targetPageId})`
	query := `
		SELECT [[reference]].[[block_id]], [[reference]].[[source_page_id]],
			[[block]].[[parent_id]], [[reference]].[[source_position]], [[block]].[[type]],
			[[block]].[[content]], [[block]].[[created]] AS [[created_at]],
			[[block]].[[updated]] AS [[updated_at]],
			'' AS [[row_container_page_id]], '' AS [[row_data_source_id]]
		FROM {{leafen_block_page_references}} [[reference]]
			INDEXED BY [[idx_leafen_block_page_references_target]]
		JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]]=[[reference]].[[block_id]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]]=[[reference]].[[source_page_id]]
		WHERE [[reference]].[[workspace_id]]={:workspaceId}
			AND [[reference]].[[target_page_id]]={:targetPageId}
			AND [[block]].[[owner]]={:ownerId} AND [[block]].[[workspace_id]]={:workspaceId}
			AND [[block]].[[page_id]]=[[reference]].[[source_page_id]]
			AND [[block]].[[position]]=[[reference]].[[source_position]]
			AND [[page]].[[owner]]={:ownerId} AND [[page]].[[workspace]]={:workspaceId}
			AND [[page]].[[notion_import_staging]]=0 AND [[page]].[[in_trash]]=0
			AND [[page]].[[data_source_id]]=''
			AND ` + notWikiPageLinkSQL("[[block]]") + `
			AND ` + notParent + `
			AND (` + comparison + `)
		UNION ALL
		SELECT [[reference]].[[block_id]], [[reference]].[[source_page_id]],
			[[block]].[[parent_id]], [[reference]].[[source_position]], [[block]].[[type]],
			[[block]].[[content]], [[block]].[[created]] AS [[created_at]],
			[[block]].[[updated]] AS [[updated_at]],
			[[row_page]].[[container_page_id]] AS [[row_container_page_id]],
			[[row_page]].[[data_source_id]] AS [[row_data_source_id]]
		FROM {{leafen_block_page_references}} [[reference]]
			INDEXED BY [[idx_leafen_block_page_references_target]]
		JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]]=[[reference]].[[block_id]]
		JOIN {{leafen_database_row_pages}} [[row_page]] ON [[row_page]].[[row_id]]=[[reference]].[[source_page_id]]
		WHERE [[reference]].[[workspace_id]]={:workspaceId}
			AND [[reference]].[[target_page_id]]={:targetPageId}
			AND [[block]].[[workspace_id]]={:workspaceId}
			AND [[block]].[[page_id]]=[[reference]].[[source_page_id]]
			AND [[block]].[[position]]=[[reference]].[[source_position]]
			AND [[row_page]].[[owner]]={:ownerId} AND [[row_page]].[[workspace_id]]={:workspaceId}
			AND ` + databaseRowNotTrashedSQL("[[row_page]].[[row_id]]") + `
			AND ` + databaseSourceNotTrashedSQL("[[row_page]].[[data_source_id]]") + `
			AND ` + searchSourceNotDeletedSQL("[[row_page]].[[workspace_id]]", "[[row_page]].[[data_source_id]]") + `
			AND ` + notWikiPageLinkSQL("[[block]]") + `
			AND ` + notParent + `
			AND (` + comparison + `)
		UNION ALL` + pageBacklinkFieldRowsSQL(notParent, comparison) + `
		` + ordering + `
		LIMIT {:limit}`
	err := app.DB().NewQuery(query).Bind(dbx.Params{
		"ownerId": ownerID, "workspaceId": workspaceID, "targetPageId": targetPageID,
		"sourcePageId": cursor.SourcePageID, "sourcePosition": cursor.SourcePosition,
		"blockId": cursor.BlockID, "limit": window,
	}).All(&rows)
	return rows, err
}

func reversePageBacklinkRows(rows []storedPageBacklink) {
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
}

func uniquePageBacklinkSourceIDs(rows []storedPageBacklink) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, exists := seen[row.SourcePageID]; exists {
			continue
		}
		seen[row.SourcePageID] = struct{}{}
		ids = append(ids, row.SourcePageID)
	}
	return ids
}

// withPageBacklinkText gives each source block its text as it stands: a
// page's text lives in its collaborative document, not in the block's
// content, and the screen finds the mention in the text it is sent
// (2026-10-25: blocks came with content "{}", so no backlink showed).
func withPageBacklinkText(app core.App, rows []storedPageBacklink) ([]storedPageBacklink, error) {
	docs := map[string]*crdt.Doc{}
	for index, row := range rows {
		// A cell or a title holds its own spans.
		if row.Type == pageFieldReferenceCellType || row.Type == pageFieldReferenceTitleType {
			continue
		}
		doc, read := docs[row.SourcePageID]
		if !read {
			loaded, _, err := loadYgoDoc(app, row.SourcePageID)
			if err != nil {
				return nil, err
			}
			doc, docs[row.SourcePageID] = loaded, loaded
		}
		if doc == nil {
			continue
		}
		spans, text := rowPageBlockTextAsItStands(doc, row.BlockID, false)
		if len(spans) == 0 {
			continue
		}
		content := map[string]any{}
		if row.Content != "" {
			if err := json.Unmarshal([]byte(row.Content), &content); err != nil || content == nil {
				content = map[string]any{}
			}
		}
		rich := make([]any, 0, len(spans))
		for _, span := range spans {
			rich = append(rich, publicTextSpanValue(span))
		}
		content["rich"], content["text"] = rich, text
		encoded, err := json.Marshal(content)
		if err != nil {
			return nil, err
		}
		rows[index].Content = string(encoded)
	}
	return rows, nil
}

func pageBacklinkBlockWires(rows []storedPageBacklink) []pageBacklinksBlockWire {
	blocks := make([]pageBacklinksBlockWire, 0, len(rows))
	for _, row := range rows {
		block := pageBacklinksBlockWire{
			ID: row.BlockID, PageID: row.SourcePageID, ParentID: row.ParentID,
			Position: row.SourcePosition, Type: row.Type,
			Content: json.RawMessage(row.Content), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.RowContainerPageID != "" {
			block.SourceRow = &pageBacklinksSourceRowWire{
				ContainerPageID: row.RowContainerPageID, DataSourceID: row.RowDataSourceID,
				InlineDatabase: row.RowDatabase.Inline, DatabaseTitle: row.RowDatabase.Title,
			}
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func loadPageBacklinkPages(
	app core.App,
	ownerID string,
	workspaceID string,
	seedPageIDs []string,
) ([]pageResponse, error) {
	seedPageIDs = uniqueStrings(seedPageIDs)
	encodedSeeds, err := json.Marshal(seedPageIDs)
	if err != nil {
		return nil, err
	}
	ancestors := []storedPageBacklinkAncestor{}
	err = app.DB().NewQuery(`
		WITH RECURSIVE [[ancestors]] ([[id]], [[parent_id]]) AS (
			-- Each seed page is found by its id; its owner, workspace and
			-- import flag are read after (+), or SQLite reads the
			-- workspace's every page to find the seeds.
			SELECT [[page]].[[id]], [[page]].[[parent_id]]
			FROM json_each({:seedPageIds}) [[seed]] CROSS JOIN {{leafen_pages}} [[page]]
				ON [[page]].[[id]]=[[seed]].[[value]]
			WHERE +[[page]].[[owner]]={:ownerId} AND +[[page]].[[workspace]]={:workspaceId}
				AND +[[page]].[[notion_import_staging]]=0
			UNION
			-- CROSS JOIN climbs from the page by key (id): SQLite's own
			-- order read the workspace's every page for each ancestor.
			SELECT [[parent]].[[id]], [[parent]].[[parent_id]]
			FROM [[ancestors]] [[child]] CROSS JOIN {{leafen_pages}} [[parent]]
				ON [[parent]].[[id]]=[[child]].[[parent_id]]
			WHERE [[child]].[[parent_id]]!=''
				AND [[parent]].[[owner]]={:ownerId} AND [[parent]].[[workspace]]={:workspaceId}
				AND [[parent]].[[notion_import_staging]]=0
		)
		SELECT [[id]], [[parent_id]] FROM [[ancestors]] ORDER BY [[id]]
	`).Bind(dbx.Params{
		"seedPageIds": string(encodedSeeds), "ownerId": ownerID, "workspaceId": workspaceID,
	}).All(&ancestors)
	if err != nil {
		return nil, err
	}
	// Every ancestor to the top, each once, as page paths are read
	// elsewhere; a cycle ends because a page is visited once.
	ids := make([]string, 0, len(ancestors))
	for _, ancestor := range ancestors {
		ids = append(ids, ancestor.ID)
	}
	if len(ids) == 0 {
		return []pageResponse{}, nil
	}
	records, err := app.FindRecordsByIds(pagesCollection, ids)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	metadata, err := loadWikiPageMetadataSet(app, ids)
	if err != nil {
		return nil, err
	}
	responses := make([]pageResponse, 0, len(records))
	for _, record := range records {
		if record.GetString("owner") != ownerID || record.GetString("workspace") != workspaceID {
			return nil, errPageBacklinksNotFound
		}
		response := pageRecordResponse(record)
		if wiki, exists := metadata[record.Id]; exists {
			response = applyWikiMetadataToPageResponse(response, wiki)
		}
		responses = append(responses, response)
	}
	sort.Slice(responses, func(left, right int) bool { return responses[left].ID < responses[right].ID })
	return responses, nil
}
