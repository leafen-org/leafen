package app

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's lists around pages (seen 2026-11-13): a page's backlinks, the
// person's favorites and recently visited pages — each read as the screen's
// backlinks, sidebar and "최근" read them, a page the person can't read left
// out — favorites added and taken out as a page's ☆ does, and a page or the
// workspace to open. Lists go on with Notion's "offset:N" cursor.

const (
	mcpPageListDefaultLimit = 50
	mcpPageListMaximumLimit = 200
)

type notionMCPBacklinksInput struct {
	PageID   string   `json:"page_id" jsonschema:"ID or address of the page or database to find backlinks for"`
	Cursor   string   `json:"cursor,omitempty" jsonschema:"next_cursor from the previous call"`
	PageSize int      `json:"page_size,omitempty" jsonschema:"Maximum backlinks to return, 1 to 100 (default 25)"`
	Types    []string `json:"types,omitempty" jsonschema:"Only these kinds: page_mention (an @-mention in text or a title), link_to_page (a link-to-page block), property_mention (a mention in a database property); left out, all"`
}

type notionMCPBacklinkSource struct {
	ID             string `json:"id"`
	URL            string `json:"url"`
	Title          string `json:"title"`
	LastEditedTime string `json:"last_edited_time,omitempty"`
}

// notionMCPBacklink names the block a link is in, or the property of a
// database page it is in (mcp_tool_notion_backlink_places.go).
type notionMCPBacklink struct {
	Type     string                     `json:"type"`
	Source   notionMCPBacklinkSource    `json:"source"`
	BlockID  string                     `json:"block_id,omitempty"`
	Property *notionMCPBacklinkProperty `json:"property,omitempty"`
}

type notionMCPBacklinksOutput struct {
	Backlinks  []notionMCPBacklink `json:"backlinks"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

type notionMCPPageListInput struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"The nextCursor of the previous answer"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum results to return, 1 to 200"`
}

// notionMCPPageListItem is a page or database as a list names it. An
// untitled page's title is left out, as Notion's lists leave it (seen
// 2026-11-13 in list-shared-pages); an untitled database reads as a new one
// (mcpDatabaseShownTitle).
type notionMCPPageListItem struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

type notionMCPPageListOutput struct {
	Results    []notionMCPPageListItem `json:"results"`
	NextCursor string                  `json:"nextCursor,omitempty"`
}

type notionMCPUpdateFavoritesInput struct {
	PageURLs  []string       `json:"page_urls" jsonschema:"Addresses or IDs of 1 to 100 pages or databases to favorite or unfavorite, in order"`
	Favorited bool           `json:"favorited" jsonschema:"true to favorite them, false to unfavorite them"`
	Position  map[string]any `json:"position,omitempty" jsonschema:"Leafen keeps the favorites' order in the person's sidebar, where they drag them: leave it out"`
}

type notionMCPPageViewInput struct {
	PageID string `json:"page_id,omitempty" jsonschema:"A page or database ID or address to open; left out, the workspace's start"`
}

type notionMCPPageViewOutput struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

func registerMCPPageListTools(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-list-page-backlinks", Title: "List a Leafen page's backlinks",
		Description: "List the pages that link to or mention a page or database, as its backlinks show them: each with its ID, title and address, and the block or the database property the link is in; fetch one to read it. Only pages you can open are listed. Follow next_cursor for more.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPBacklinksInput) (*mcp.CallToolResult, notionMCPBacklinksOutput, error) {
		return listMCPPageBacklinks(ctx, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-list-favorite-pages", Title: "List Leafen favorites",
		Description: "List the person's favorite pages and databases, the latest favorited first. Use it when the person names a favorite. Follow nextCursor for the whole list.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPPageListInput) (*mcp.CallToolResult, notionMCPPageListOutput, error) {
		return listMCPFavoritePages(ctx, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-list-recent-pages", Title: "List recently visited Leafen pages",
		Description: "List the pages and databases the person visited lately, the latest first, as Leafen's Recents list them. These are visits, not edits: to find pages they edited, use notion-search with a time sort. Follow nextCursor for the whole list.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPPageListInput) (*mcp.CallToolResult, notionMCPPageListOutput, error) {
		return listMCPRecentPages(ctx, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-update-page-favorites", Title: "Favorite Leafen pages",
		Description: "Favorite or unfavorite 1 to 100 pages or databases for the person, as a page's ☆ does; doing it again changes nothing. Every page to add must be one the person can open: if one isn't, nothing changes and the refusal names it. It changes only the person's favorites, not who can open the pages. Their order is the person's, set by dragging in the sidebar.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPUpdateFavoritesInput) (*mcp.CallToolResult, notionMCPPageListOutput, error) {
		return updateMCPPageFavorites(ctx, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-page-view", Title: "Open a Leafen page",
		Description: "The address that opens a page or database in Leafen, or the workspace's start without page_id, to show the person. It changes nothing.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPPageViewInput) (*mcp.CallToolResult, notionMCPPageViewOutput, error) {
		return viewMCPPage(ctx, app, input)
	})
}

// mcpOffsetCursor reads Notion's "offset:N" cursor and a list's limit.
func mcpOffsetCursor(cursor string, limit, fallback, maximum int) (int, int, error) {
	offset := 0
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		value, err := strconv.Atoi(strings.TrimPrefix(cursor, "offset:"))
		if err != nil || value < 0 || !strings.HasPrefix(cursor, "offset:") {
			return 0, 0, mcpInvalid("cursor is the next cursor the last answer gave")
		}
		offset = value
	}
	if limit == 0 {
		limit = fallback
	}
	if limit < 1 || limit > maximum {
		return 0, 0, mcpInvalid("the limit is from 1 to %d", maximum)
	}
	return offset, limit, nil
}

// mcpListedPage is a page the person may read and the connection reaches,
// as a list names it; false for one they can't read.
func mcpListedPage(app core.App, authority mcpAuthority, pageID string) (notionMCPPageListItem, *core.Record, bool) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil || page == nil || page.GetBool("in_trash") || !mcpAuthorityReachesPage(app, authority, page) {
		return notionMCPPageListItem{}, nil, false
	}
	workspaceID := page.GetString("workspace")
	if isDatabaseRowPageRecord(page) {
		row, err := authorizeDatabaseRowPage(app, authority.PrincipalID, pageID, permissionWorkspaceRead)
		if err != nil {
			return notionMCPPageListItem{}, nil, false
		}
		workspaceID = row.WorkspaceID
	} else if current, err := authorizePage(app, authority.PrincipalID, pageID, permissionWorkspaceRead); err != nil {
		return notionMCPPageListItem{}, nil, false
	} else {
		workspaceID = current.Workspace.WorkspaceID
	}
	if !authority.reaches(workspaceID) {
		return notionMCPPageListItem{}, nil, false
	}
	// Its title reads as its backlinks name it (mcpTitleAsShown).
	item := notionMCPPageListItem{Type: "page", URL: mcpPageURL(authority, pageID),
		Title: mcpTitleAsShown(app, authority, workspaceID, pageID, page.GetString("title"))}
	if isFullPageDatabase(page) {
		item.Type, item.Title = "database", mcpListedDatabaseTitle(app, authority, workspaceID, mcpFullPageDatabaseContainer(app, pageID), item.Title)
		item.URL = mcpPageAddress(app, authority, pageID)
	}
	return item, page, true
}

// mcpListedDatabase is an inline database as a list names it.
func mcpListedDatabase(app core.App, authority mcpAuthority, pageID, containerID string) notionMCPPageListItem {
	workspaceID := ""
	_ = app.DB().NewQuery(`SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": pageID}).Row(&workspaceID)
	return notionMCPPageListItem{Type: "database", URL: mcpDatabaseAddress(authority, containerID),
		Title: mcpListedDatabaseTitle(app, authority, workspaceID, containerID, "")}
}

// mcpListedDatabaseTitle is a database's name as a list names it, as it is
// named everywhere (mcpDatabaseNameShown), title where it can't be read;
// "New database" for none (mcpDatabaseShownTitle).
func mcpListedDatabaseTitle(app core.App, authority mcpAuthority, workspaceID, containerID, title string) string {
	names, err := databaseContainerNames(app, []string{containerID})
	if name, found := names[containerID]; err == nil && found {
		title = mcpDatabaseNameShown(app, authority, workspaceID, name)
	}
	return mcpDatabaseShownTitle(app, authority.PrincipalID, title)
}

// mcpPagedList is a list's results from an offset, with the cursor on.
func mcpPagedList(items []notionMCPPageListItem, offset, limit int) notionMCPPageListOutput {
	output := notionMCPPageListOutput{Results: []notionMCPPageListItem{}}
	if offset < len(items) {
		end := min(offset+limit, len(items))
		output.Results = append(output.Results, items[offset:end]...)
		if end < len(items) {
			output.NextCursor = "offset:" + strconv.Itoa(end)
		}
	}
	return output
}

func listMCPFavoritePages(ctx context.Context, app core.App, input notionMCPPageListInput) (*mcp.CallToolResult, notionMCPPageListOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPPageListOutput{}, err
	}
	offset, limit, err := mcpOffsetCursor(input.Cursor, input.Limit, mcpPageListDefaultLimit, mcpPageListMaximumLimit)
	if err != nil {
		return nil, notionMCPPageListOutput{}, err
	}
	stored := []struct {
		ID          string `db:"id"`
		ContainerID string `db:"container_id"`
		PageID      string `db:"page_id"`
		Updated     int64  `db:"updated"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]] AS [[id]], '' AS [[container_id]], [[page_id]], [[updated]] FROM {{leafen_page_favorites}}
		WHERE [[principal_id]] = {:principal} AND [[favorite]] = 1
		UNION ALL
		SELECT [[container_id]] AS [[id]], [[container_id]], [[page_id]], [[updated]] FROM {{leafen_database_favorites}}
		WHERE [[principal_id]] = {:principal} AND [[favorite]] = 1
		ORDER BY [[updated]] DESC, [[id]]
	`).Bind(dbx.Params{"principal": authority.PrincipalID}).All(&stored); err != nil {
		return nil, notionMCPPageListOutput{}, failedBecause(errMCPToolFailed, err)
	}
	items := []notionMCPPageListItem{}
	for _, favorite := range stored {
		item, _, readable := mcpListedPage(app, authority, favorite.PageID)
		if !readable {
			continue
		}
		if favorite.ContainerID != "" {
			if !mcpDatabaseContainerExists(app, favorite.ContainerID) {
				continue
			}
			item = mcpListedDatabase(app, authority, favorite.PageID, favorite.ContainerID)
		}
		items = append(items, item)
	}
	return nil, mcpPagedList(items, offset, limit), nil
}

func listMCPRecentPages(ctx context.Context, app core.App, input notionMCPPageListInput) (*mcp.CallToolResult, notionMCPPageListOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPPageListOutput{}, err
	}
	offset, limit, err := mcpOffsetCursor(input.Cursor, input.Limit, mcpPageListDefaultLimit, mcpPageListMaximumLimit)
	if err != nil {
		return nil, notionMCPPageListOutput{}, err
	}
	visits := []pageVisitWire{}
	for _, workspaceID := range authority.WorkspaceIDs {
		result, err := mcpNativeCall(ctx, "GET", "/workspaces/"+workspaceID+"/page-visits", "", nil)
		if err != nil {
			return nil, notionMCPPageListOutput{}, err
		}
		if mcpNativeRefusal(result) != nil {
			continue
		}
		listed := pageVisitsWire{}
		if encoded, err := json.Marshal(result.Body); err != nil || json.Unmarshal(encoded, &listed) != nil {
			return nil, notionMCPPageListOutput{}, failedBecause(errMCPToolFailed, err)
		}
		visits = append(visits, listed.Visits...)
	}
	visited := func(visit pageVisitWire) time.Time {
		at, _ := time.Parse(time.RFC3339Nano, visit.VisitedAt)
		return at
	}
	slices.SortStableFunc(visits, func(a, b pageVisitWire) int { return visited(b).Compare(visited(a)) })
	items := []notionMCPPageListItem{}
	for _, visit := range visits {
		if item, _, readable := mcpListedPage(app, authority, visit.PageID); readable {
			items = append(items, item)
		}
	}
	return nil, mcpPagedList(items, offset, limit), nil
}

func listMCPPageBacklinks(ctx context.Context, app core.App, input notionMCPBacklinksInput) (*mcp.CallToolResult, notionMCPBacklinksOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPBacklinksOutput{}, err
	}
	pageID := mcpLocalID(app, input.PageID)
	if containerID, found := resolveMCPDatabaseID(app, pageID); found {
		if location, err := loadMCPDatabaseContainerLocation(app, containerID); err == nil && mcpFullPageDatabaseContainer(app, location.PageID) == containerID {
			pageID = location.PageID
		}
	}
	if _, _, readable := mcpListedPage(app, authority, pageID); !readable {
		return nil, notionMCPBacklinksOutput{}, mcpNotFound("page", input.PageID)
	}
	limit := input.PageSize
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return nil, notionMCPBacklinksOutput{}, mcpInvalid("page_size is from 1 to 100")
	}
	for _, kind := range input.Types {
		if !slices.Contains([]string{"page_mention", "link_to_page", "property_mention", "comment_mention"}, kind) {
			return nil, notionMCPBacklinksOutput{}, mcpInvalid("types holds page_mention, link_to_page, property_mention or comment_mention")
		}
	}
	query := url.Values{"limit": {strconv.Itoa(min(limit, pageBacklinksPageSize))}}
	if cursor := strings.TrimSpace(input.Cursor); cursor != "" {
		query.Set("cursor", cursor)
	}
	result, err := mcpNativeCall(ctx, "GET", "/pages/"+pageID+"/backlinks", query.Encode(), nil)
	if err != nil {
		return nil, notionMCPBacklinksOutput{}, err
	}
	if err := mcpNativeRefusal(result); err != nil {
		return nil, notionMCPBacklinksOutput{}, err
	}
	listed := pageBacklinksResponse{}
	if encoded, err := json.Marshal(result.Body); err != nil || json.Unmarshal(encoded, &listed) != nil {
		return nil, notionMCPBacklinksOutput{}, failedBecause(errMCPToolFailed, err)
	}
	pages := map[string]pageResponse{}
	for _, page := range listed.Pages {
		pages[page.ID] = page
	}
	output := notionMCPBacklinksOutput{Backlinks: []notionMCPBacklink{}}
	for _, block := range listed.Blocks {
		kind := mcpBacklinkKind(block)
		if len(input.Types) > 0 && !slices.Contains(input.Types, kind) {
			continue
		}
		// A source's title reads as fetch names it at its top: a mention in
		// it as it shows (mcpTitleAsShown), an untitled one as a new page
		// (mcpPageShownTitle), as Notion's ("새 페이지", seen 2026-11-14;
		// "… @w2322 대상 끝", seen 2026-11-22).
		page := pages[block.PageID]
		source := notionMCPBacklinkSource{
			ID: block.PageID, URL: mcpPageURL(authority, block.PageID),
			Title: mcpPageShownTitle(app, authority.PrincipalID, mcpTitleAsShown(app, authority, page.WorkspaceID, block.PageID, page.Title)),
		}
		if page, found := pages[block.PageID]; found {
			source.LastEditedTime = personClockMillisOf(page.UpdatedAt, authority.clock())
		}
		output.Backlinks = append(output.Backlinks, notionMCPBacklink{Type: kind, Source: source, BlockID: block.ID})
	}
	if err := mcpBacklinkPlaces(app, output.Backlinks); err != nil {
		return nil, notionMCPBacklinksOutput{}, failedBecause(errMCPToolFailed, err)
	}
	if listed.HasMore && listed.NextCursor != nil {
		output.NextCursor = *listed.NextCursor
	}
	return nil, output, nil
}

func updateMCPPageFavorites(ctx context.Context, app core.App, input notionMCPUpdateFavoritesInput) (*mcp.CallToolResult, notionMCPPageListOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPPageListOutput{}, err
	}
	if input.Position != nil {
		return nil, notionMCPPageListOutput{}, mcpInvalid("Leafen keeps the favorites' order in the person's sidebar, where they drag them: leave position out")
	}
	if len(input.PageURLs) == 0 || len(input.PageURLs) > 100 {
		return nil, notionMCPPageListOutput{}, mcpInvalid("page_urls names 1 to 100 pages or databases")
	}
	type favorite struct {
		raw, pageID, containerID string
		item                     notionMCPPageListItem
		page                     *core.Record
	}
	chosen, failed := []favorite{}, []string{}
	for _, raw := range input.PageURLs {
		next := favorite{raw: raw, pageID: mcpLocalID(app, raw)}
		if containerID, found := resolveMCPDatabaseID(app, next.pageID); found {
			location, err := loadMCPDatabaseContainerLocation(app, containerID)
			if err == nil && mcpFullPageDatabaseContainer(app, location.PageID) == containerID {
				next.pageID = location.PageID
			} else if err == nil {
				next.pageID, next.containerID = location.PageID, containerID
			}
		}
		item, page, readable := mcpListedPage(app, authority, next.pageID)
		if !readable {
			if input.Favorited {
				failed = append(failed, raw)
			}
			continue
		}
		next.item, next.page = item, page
		if next.containerID != "" {
			next.item = mcpListedDatabase(app, authority, next.pageID, next.containerID)
		}
		if !slices.ContainsFunc(chosen, func(have favorite) bool { return have.pageID == next.pageID && have.containerID == next.containerID }) {
			chosen = append(chosen, next)
		}
	}
	if len(failed) > 0 {
		return nil, notionMCPPageListOutput{}, mcpDenied("Nothing changed: these aren't pages or databases you can open in the workspaces this connection was given: %s", strings.Join(failed, ", "))
	}
	output := notionMCPPageListOutput{Results: []notionMCPPageListItem{}}
	// New favorites go to the top in the order given, as Notion's: the last
	// given is favorited first.
	for index := len(chosen) - 1; index >= 0; index-- {
		next := chosen[index]
		var result nativeOperationResult
		switch {
		case next.containerID != "":
			state, stateErr := loadDatabaseFavoriteRevision(app, authority.PrincipalID, next.containerID)
			if stateErr != nil {
				return nil, notionMCPPageListOutput{}, failedBecause(errMCPToolFailed, stateErr)
			}
			result, err = mcpNativeCall(ctx, "PUT", "/database-containers/"+next.containerID+"/favorite", "", pageFavoriteMutationInput{
				RequestID: "mcp-favorite:" + newDiagnosticReference(), Favorite: input.Favorited, ExpectedRevision: state,
			})
		case isDatabaseRowPageRecord(next.page):
			row, rowErr := authorizeDatabaseRowPage(app, authority.PrincipalID, next.pageID, permissionWorkspaceRead)
			if rowErr != nil {
				return nil, notionMCPPageListOutput{}, mcpRefusedUnlessStorage(errMCPCantRead, rowErr)
			}
			result, err = mcpNativeCall(ctx, "PUT", "/database-rows/"+next.pageID+"/page/favorite", "", databaseRowPageFavoriteInput{
				Favorite: input.Favorited, ExpectedAuthorityRevision: row.AuthorityRevision,
			})
		default:
			state, stateErr := loadPageFavoriteState(app, authority.PrincipalID, next.pageID)
			if stateErr != nil {
				return nil, notionMCPPageListOutput{}, failedBecause(errMCPToolFailed, stateErr)
			}
			if state.Favorite == input.Favorited && state.Revision > 0 {
				break
			}
			result, err = mcpNativeCall(ctx, "PUT", "/pages/"+next.pageID+"/favorite", "", pageFavoriteMutationInput{
				RequestID: "mcp-favorite:" + newDiagnosticReference(), Favorite: input.Favorited, ExpectedRevision: state.Revision,
			})
		}
		if err != nil {
			return nil, notionMCPPageListOutput{}, err
		}
		if result.Status != 0 {
			if err := mcpNativeRefusal(result); err != nil {
				return nil, notionMCPPageListOutput{}, err
			}
		}
	}
	for _, next := range chosen {
		output.Results = append(output.Results, next.item)
	}
	return nil, output, nil
}

// loadDatabaseFavoriteRevision is the revision of the person's favorite of
// an inline database, 0 for none yet.
func loadDatabaseFavoriteRevision(app core.App, principalID, containerID string) (int, error) {
	revision := 0
	err := app.DB().NewQuery(`
		SELECT COALESCE((SELECT [[revision]] FROM {{leafen_database_favorites}}
			WHERE [[container_id]] = {:container} AND [[principal_id]] = {:principal}), 0)
	`).Bind(dbx.Params{"container": containerID, "principal": principalID}).Row(&revision)
	return revision, err
}

func viewMCPPage(ctx context.Context, app core.App, input notionMCPPageViewInput) (*mcp.CallToolResult, notionMCPPageViewOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPPageViewOutput{}, err
	}
	if strings.TrimSpace(input.PageID) == "" {
		address, err := mcpServerURL(authority.Resource, "/")
		if err != nil {
			return nil, notionMCPPageViewOutput{}, failedBecause(errMCPToolFailed, err)
		}
		return nil, notionMCPPageViewOutput{URL: address}, nil
	}
	pageID := mcpLocalID(app, input.PageID)
	if containerID, found := resolveMCPDatabaseID(app, pageID); found {
		if location, err := loadMCPDatabaseContainerLocation(app, containerID); err == nil {
			if _, _, readable := mcpListedPage(app, authority, location.PageID); readable {
				return nil, notionMCPPageViewOutput{URL: mcpDatabaseAddress(authority, containerID),
					Title: mcpListedDatabase(app, authority, location.PageID, containerID).Title}, nil
			}
		}
	}
	item, _, readable := mcpListedPage(app, authority, pageID)
	if !readable {
		return nil, notionMCPPageViewOutput{}, mcpNotFound("page", input.PageID)
	}
	return nil, notionMCPPageViewOutput{URL: item.URL, Title: item.Title}, nil
}
