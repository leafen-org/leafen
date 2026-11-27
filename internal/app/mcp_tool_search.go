package app

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const maxMCPSearchResults = maxWorkspaceSearchResults

// An agent's search checks matches for up to mcpSearchCheckBudget in one
// call: an agent reads an empty answer as nothing found, and each call
// again is a turn of its own. The app's search checks 400 ms a call and
// calls again at once (workspace_search_query.go); an agent's search that
// stopped there answered an empty page on a NAS-class server, the page with
// the very title five to eight calls later (2026-11-13). One still going on
// past it answers with no results, its next_cursor and a notice saying so.
const mcpSearchCheckBudget = 10 * time.Second

const mcpSearchStillSearching = "Still searching: this search went on longer than one call checks, so no results are listed yet; " +
	"that doesn't mean nothing matches. Call again with the same query and options and start_cursor set to next_cursor to go on."

type notionMCPSearchInput struct {
	Query              string                  `json:"query" jsonschema:"Short, specific keywords to find in page titles and content, or empty to browse by filters or a time sort; for user search, a name or email address"`
	QueryType          string                  `json:"query_type,omitempty" jsonschema:"internal (the default) or user"`
	TeamspaceID        string                  `json:"teamspace_id,omitempty" jsonschema:"Restrict search to one team (teamspace) from notion-get-teams; a workspace from its workspaces searches all of that workspace"`
	PageURL            string                  `json:"page_url,omitempty" jsonschema:"Restrict search to a page and its sub-pages: the page's URL or ID"`
	DataSourceURL      string                  `json:"data_source_url,omitempty" jsonschema:"Restrict search to one data source's pages: its collection:// URL"`
	Filters            *notionMCPSearchFilters `json:"filters,omitempty" jsonschema:"Optional exact filters, nested here"`
	Sort               string                  `json:"sort,omitempty" jsonschema:"relevance (the default), last_edited or created; time sorts put the newest first"`
	MaxHighlightLength *int                    `json:"max_highlight_length,omitempty" jsonschema:"Longest result highlight in characters, from 0 to 500 (default 200); 0 leaves highlights out"`
	PageSize           int                     `json:"page_size,omitzero" jsonschema:"Maximum results to return, from 1 to 50 (default 10)"`
	StartCursor        string                  `json:"start_cursor,omitempty" jsonschema:"Opaque continuation returned by a prior search"`
	// native says leafen_search is on this endpoint, and once that the tool
	// takes no start_cursor (notion-memory-search), for its notice of the
	// workspaces not searched yet (notionMCPSearchLeft).
	native, once bool
}

type leafenMCPSearchInput struct {
	Query        string   `json:"query" jsonschema:"Text to find in page titles and content"`
	WorkspaceID  string   `json:"workspace_id,omitempty" jsonschema:"One workspace to search"`
	WorkspaceIDs []string `json:"workspace_ids,omitempty" jsonschema:"Several workspaces to search; leave both out to search every one this connection reaches"`
	PageSize     int      `json:"page_size,omitzero" jsonschema:"Maximum results to return, from 1 to 25"`
	StartCursor  string   `json:"start_cursor,omitempty" jsonschema:"Opaque continuation returned by a prior search"`
}

type mcpSearchInput struct {
	Query              string
	QueryType          string
	WorkspaceID        string
	WorkspaceIDs       []string
	TeamspaceID        string
	PageSize           int
	StartCursor        string
	AllowImplicitScope bool
	// Notion carries the official search options, read once the workspace
	// is chosen.
	Notion *notionMCPSearchInput
}

type mcpSearchResult struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	// Workspace is its workspace's name, for a search of several.
	Workspace string `json:"workspace,omitempty"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Highlight string `json:"highlight,omitempty"`
	// Path, Type, Timestamp and Verification are the result's facts, as
	// Notion's search gives them.
	Path         string          `json:"path,omitempty"`
	Type         string          `json:"type,omitempty"`
	Timestamp    string          `json:"timestamp,omitempty"`
	Verification *map[string]any `json:"verification,omitempty"`
	// ParentDataSourceURL is the data source a database's page — a row —
	// stands in, as Notion's search names it; a page under a row has none.
	ParentDataSourceURL string `json:"parent_data_source_url,omitempty"`
	// indexed is its title as Notion's search gives it (mcpTitleAsIndexed);
	// Title says it as it shows (mcpTitleAsShown), as the app's search does.
	indexed string
}

type mcpSearchOutput struct {
	Results    []mcpSearchResult `json:"results"`
	HasMore    bool              `json:"has_more"`
	NextCursor *string           `json:"next_cursor"`
	Indexing   bool              `json:"indexing"`
	Notice     string            `json:"notice,omitempty"`
	// Backlog says recently changed pages still wait for the index.
	Backlog string `json:"backlog,omitempty"`
	// Workspaces says where a leafen_search looked (mcp_tool_search_scope.go).
	Workspaces *mcpSearchWorkspaceSummary `json:"workspaces,omitempty"`
	// searched and asked count the workspaces searched so far and asked.
	searched, asked int
}

// Notion's search answers one page, {"results": […], "type":
// "workspace_search"}, and a user search {"type": "user_search", "results":
// [{"text": "<users-search-results>…"}]}, with no has_more or next_cursor,
// past page_size too (seen 2026-11-14). Leafen's says has_more and
// next_cursor only while there is more — more found, or a search still
// going on — for start_cursor to reach it.
type notionMCPSearchOutput struct {
	Results    []map[string]any `json:"results"`
	Type       string           `json:"type"`
	HasMore    bool             `json:"has_more,omitzero"`
	NextCursor *string          `json:"next_cursor,omitempty"`
	// Notice and Backlog say what leafen_search's say: a search still going
	// on, and recently changed pages still waiting for the index.
	Notice  string `json:"notice,omitempty"`
	Backlog string `json:"backlog,omitempty"`
	// listed are the pages and databases found as the answer text lists
	// them, their keys in Notion's order (answered); none for a user search.
	listed []any
}

// answered is the output as its answer text writes it: a user search's type
// first, as Notion's.
func (output notionMCPSearchOutput) answered() any {
	results := output.listed
	if results == nil {
		results = make([]any, len(output.Results))
		for index, result := range output.Results {
			results[index] = result
		}
	}
	if output.Type == notionMCPUserSearchType {
		return struct {
			Type       string  `json:"type"`
			Results    []any   `json:"results"`
			HasMore    bool    `json:"has_more,omitzero"`
			NextCursor *string `json:"next_cursor,omitempty"`
		}{output.Type, results, output.HasMore, output.NextCursor}
	}
	return struct {
		Results    []any   `json:"results"`
		Type       string  `json:"type"`
		HasMore    bool    `json:"has_more,omitzero"`
		NextCursor *string `json:"next_cursor,omitempty"`
		Notice     string  `json:"notice,omitempty"`
		Backlog    string  `json:"backlog,omitempty"`
	}{results, output.Type, output.HasMore, output.NextCursor, output.Notice, output.Backlog}
}

// notionMCPSearchResult is a page or database found, as Notion's search
// gives one, its keys in Notion's order (seen 2026-11-14: id, title, url,
// type, highlight, timestamp, path, verification, then a row's
// parent_data_source_url). Its highlight is written for every result, ""
// for none, as Notion's (seen 2026-11-22 five times: max_highlight_length 0
// answers "highlight":"" for each).
type notionMCPSearchResult struct {
	ID                  string          `json:"id"`
	Title               string          `json:"title"`
	URL                 string          `json:"url"`
	Type                string          `json:"type,omitempty"`
	Highlight           string          `json:"highlight"`
	Timestamp           *string         `json:"timestamp,omitempty"`
	Path                string          `json:"path,omitempty"`
	Verification        *map[string]any `json:"verification,omitempty"`
	ParentDataSourceURL string          `json:"parent_data_source_url,omitempty"`
}

// fields is the result as the structured content holds it.
func (result notionMCPSearchResult) fields() map[string]any {
	item := map[string]any{"id": result.ID, "title": result.Title, "url": result.URL}
	if result.Type != "" {
		item["type"] = result.Type
	}
	item["highlight"] = result.Highlight
	if result.Timestamp != nil {
		item["timestamp"] = *result.Timestamp
	}
	if result.Path != "" {
		item["path"] = result.Path
	}
	if result.Verification != nil {
		item["verification"] = *result.Verification
	}
	if result.ParentDataSourceURL != "" {
		item["parent_data_source_url"] = result.ParentDataSourceURL
	}
	return item
}

func registerMCPSearchTools(
	server *mcp.Server,
	app core.App,
	annotations *mcp.ToolAnnotations,
	surface mcpToolSurface,
) {
	notionSearch := &mcp.Tool{
		Name: "notion-search", Title: "Search Leafen content",
		Description: "Search every workspace this connection reaches, or the one teamspace_id, page_url or data_source_url " +
			"keeps to, using the bounded native index; leafen_search searches only the workspaces you name. " +
			"An answer with a notice and no results is still searching: call again with its next_cursor.",
		Annotations: annotations,
	}
	mcp.AddTool(server, notionSearch, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPSearchInput) (*mcp.CallToolResult, notionMCPSearchOutput, error) {
		input.native = surface == mcpToolSurfaceNative
		return searchNotionMCP(ctx, app, input)
	})
	if surface != mcpToolSurfaceNative {
		return
	}
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_search", Title: "Search Leafen workspaces",
		Description: "Search the workspaces you name, or every one this connection reaches, using the bounded native index. " +
			"Name the ones the conversation or your memory places the pages in; search all only when you don't know, " +
			"then narrow to where results came from; Leafen keeps no choice between calls. " +
			"An answer with indexing true is still searching: call again with its next_cursor.",
		Annotations: annotations,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPSearchInput) (*mcp.CallToolResult, mcpSearchOutput, error) {
		return searchMCPWorkspace(ctx, app, mcpSearchInput{
			Query: input.Query, WorkspaceID: input.WorkspaceID, WorkspaceIDs: input.WorkspaceIDs,
			PageSize: input.PageSize, StartCursor: input.StartCursor, AllowImplicitScope: true,
		})
	})
}

// searchNotionMCP answers the official search tool: people for a user
// search, otherwise pages under the official options.
func searchNotionMCP(
	ctx context.Context,
	app core.App,
	input notionMCPSearchInput,
) (*mcp.CallToolResult, notionMCPSearchOutput, error) {
	var output notionMCPSearchOutput
	var err error
	if strings.TrimSpace(input.QueryType) == "user" {
		output, err = searchMCPUsers(ctx, app, input)
	} else {
		output, err = searchNotionMCPPages(ctx, app, input)
	}
	if err != nil {
		return nil, notionMCPSearchOutput{}, err
	}
	// Its results' keys keep their order (mcp_answer_order.go).
	answer, err := mcpAnswerInOrder(output.answered())
	return answer, output, err
}

// searchNotionMCPPages searches pages under the official options.
func searchNotionMCPPages(ctx context.Context, app core.App, input notionMCPSearchInput) (notionMCPSearchOutput, error) {
	highlight := notionMCPSearchDefaultHighlight
	if input.MaxHighlightLength != nil {
		highlight = *input.MaxHighlightLength
	}
	if highlight < 0 || highlight > maxNotionMCPSearchHighlight {
		return notionMCPSearchOutput{}, mcpInvalid("max_highlight_length is from 0 to %d", maxNotionMCPSearchHighlight)
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = notionMCPSearchDefaultResults
	}
	_, output, err := searchMCPWorkspace(ctx, app, mcpSearchInput{
		Query: input.Query, QueryType: input.QueryType, TeamspaceID: input.TeamspaceID,
		PageSize: pageSize, StartCursor: input.StartCursor, AllowImplicitScope: true, Notion: &input,
	})
	if err != nil {
		return notionMCPSearchOutput{}, err
	}
	compatible := notionMCPSearchOutput{
		Results: []map[string]any{}, listed: []any{}, HasMore: output.HasMore,
		NextCursor: output.NextCursor, Type: "workspace_search",
		Notice: output.Notice, Backlog: output.Backlog,
	}
	for _, result := range output.Results {
		shown := result.Title
		result.Title = result.indexed
		title, listed := notionMCPSearchTitle(app, result, shown)
		if !listed {
			continue
		}
		item := notionMCPSearchResult{
			ID: result.ID, Title: title, URL: result.URL, Type: result.Type,
			Highlight: notionMCPSearchHighlight(result.Highlight, highlight), Path: result.Path,
			Verification: result.Verification, ParentDataSourceURL: result.ParentDataSourceURL,
		}
		if result.Type != "" {
			timestamp := result.Timestamp
			item.Timestamp = &timestamp
		}
		compatible.Results = append(compatible.Results, item.fields())
		compatible.listed = append(compatible.listed, item)
	}
	return compatible, nil
}

// notionMCPSearchTitle is a found page's or database's title as Notion's
// search gives it, and whether its search lists it at all (seen
// 2026-11-14): an untitled one with no body — a page or row with nothing
// written in it, whatever its values — and an untitled database are not
// listed. An untitled page is called what its fetch and the paths below it
// call it, shown (mcp_untitled_names.go), where Notion's says "Untitled" in
// every language.
func notionMCPSearchTitle(app core.App, result mcpSearchResult, shown string) (string, bool) {
	if strings.TrimSpace(result.Title) != "" {
		return result.Title, true
	}
	if result.Type == "database" {
		return "", false
	}
	var written bool
	if err := app.DB().NewQuery(`SELECT EXISTS(SELECT 1 FROM {{leafen_blocks}} WHERE [[page_id]] = {:pageId})`).
		Bind(dbx.Params{"pageId": result.ID}).Row(&written); err != nil || !written {
		return "", false
	}
	return mcpNamedOr(shown, "Untitled"), true
}

func searchMCPUsers(
	ctx context.Context,
	app core.App,
	input notionMCPSearchInput,
) (notionMCPSearchOutput, error) {
	query := strings.TrimSpace(input.Query)
	if utf8.RuneCountInString(query) < 1 ||
		utf8.RuneCountInString(query) > mcpDirectoryQueryMaxRunes {
		return notionMCPSearchOutput{}, mcpInvalid("a user search needs a name or email address of at most %d characters in query", mcpDirectoryQueryMaxRunes)
	}
	limit := input.PageSize
	if limit == 0 {
		limit = defaultWorkspaceSearchResults
	}
	if limit < 1 || limit > maxMCPSearchResults {
		return notionMCPSearchOutput{}, mcpInvalid("page_size is from 1 to %d", maxMCPSearchResults)
	}
	_, users, err := getMCPUsers(ctx, app, notionMCPGetUsersInput{
		TeamspaceID: input.TeamspaceID, Query: query,
		StartCursor: input.StartCursor, PageSize: limit,
	})
	if err != nil {
		return notionMCPSearchOutput{}, err
	}
	return notionMCPSearchOutput{
		Results: []map[string]any{{"text": notionMCPUserSearchText(users.Results)}},
		HasMore: users.HasMore, NextCursor: users.NextCursor, Type: notionMCPUserSearchType,
	}, nil
}

const notionMCPUserSearchType = "user_search"

// notionMCPUserSearchText lists the people a user search found as Notion's
// does, one <user> a line in <users-search-results> (seen 2026-11-14:
// `<user id="{{user://…}}" name="…" email="…"/>`, and an empty line for
// none): each by the user:// address of the ID notion-get-users gives, with
// their email where they have one.
func notionMCPUserSearchText(users []map[string]any) string {
	lines := make([]string, 0, len(users))
	for _, user := range users {
		id, _ := user["id"].(string)
		name, _ := user["name"].(string)
		line := `<user id="{{user://` + mcpFetchAttribute(id) + `}}" name="` + mcpFetchAttribute(name) + `"`
		if person, _ := user["person"].(map[string]any); person != nil {
			if email, _ := person["email"].(string); email != "" {
				line += ` email="` + mcpFetchAttribute(email) + `"`
			}
		}
		lines = append(lines, line+"/>")
	}
	return "<users-search-results>\n" + strings.Join(lines, "\n") + "\n</users-search-results>"
}

func searchMCPWorkspace(
	ctx context.Context,
	app core.App,
	input mcpSearchInput,
) (*mcp.CallToolResult, mcpSearchOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, mcpSearchOutput{}, err
	}
	query := strings.TrimSpace(input.Query)
	queryType := strings.TrimSpace(input.QueryType)
	if queryType != "" && queryType != "internal" {
		return nil, mcpSearchOutput{}, mcpInvalid("query_type is internal or user")
	}
	if utf8.RuneCountInString(query) > maxWorkspaceSearchQueryRunes {
		return nil, mcpSearchOutput{}, mcpInvalid("the query is at most %d characters", maxWorkspaceSearchQueryRunes)
	}
	limit := input.PageSize
	if limit == 0 {
		limit = defaultWorkspaceSearchResults
	}
	if limit < 1 || limit > maxMCPSearchResults {
		return nil, mcpSearchOutput{}, mcpInvalid("page_size is from 1 to %d", maxMCPSearchResults)
	}
	var workspaces []workspaceAuthorization
	filterOf := func(string) (workspaceSearchFilter, error) {
		return workspaceSearchFilter{Sort: workspaceSearchSortBest, Content: workspaceSearchContentActive}, nil
	}
	if input.Notion != nil {
		// A collection:// URL may carry the data source's API ID, as every
		// tool that takes a data source reads it.
		notion := *input.Notion
		if strings.TrimSpace(notion.DataSourceURL) != "" {
			notion.DataSourceURL = mcpLocalID(app, strings.TrimSpace(notion.DataSourceURL))
		}
		workspaces, err = notionMCPSearchWorkspaces(app, authority, notion)
		filterOf = func(workspaceID string) (workspaceSearchFilter, error) {
			return notionMCPSearchFilter(app, notion, authority.PrincipalID, workspaceID, authority.clock())
		}
	} else {
		workspaces, err = leafenMCPSearchWorkspaces(app, authority, input)
	}
	if err != nil {
		return nil, mcpSearchOutput{}, err
	}
	var output mcpSearchOutput
	if len(workspaces) > 1 {
		output, err = searchMCPAcross(app, authority, workspaces, filterOf, query, input.StartCursor, limit)
	} else {
		var filter workspaceSearchFilter
		if filter, err = filterOf(workspaces[0].WorkspaceID); err == nil {
			output, err = runMCPSearch(app, authority, workspaces[0], query, filter, input.StartCursor, limit)
			output.searched, output.asked = 1, 1
		}
	}
	if err != nil {
		return nil, mcpSearchOutput{}, err
	}
	// The official tool cuts highlights to the length asked for; Leafen's
	// own to the official default, and says where it looked.
	if input.Notion == nil {
		for index := range output.Results {
			output.Results[index].Highlight = notionMCPSearchHighlight(output.Results[index].Highlight, notionMCPSearchDefaultHighlight)
		}
		summarizeMCPSearchWorkspaces(authority, &output, workspaces)
	} else if left := notionMCPSearchLeft(output, *input.Notion); left != "" {
		output.Notice = strings.TrimSpace(output.Notice + " " + left)
	}
	if output.Indexing {
		output.Notice = strings.TrimSpace(mcpSearchStillSearching + " " + output.Notice)
	}
	return nil, output, nil
}
