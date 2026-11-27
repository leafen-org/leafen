package app

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// The tools an MCP token's page offers, in Notion's six groups (its token
// page, seen 2026-11-09: 콘텐츠 읽기 · 콘텐츠 업데이트 · 콘텐츠 삽입 · 댓글 ·
// 사용자 · 작업). Leafen's tools, on its own endpoint and on Notion's, are
// placed in them by what they do; the person turns tools off per token, and
// a tool turned off is neither listed to the agent nor run for it.

const (
	mcpToolGroupRead     = "readContent"
	mcpToolGroupUpdate   = "updateContent"
	mcpToolGroupInsert   = "insertContent"
	mcpToolGroupComments = "comments"
	mcpToolGroupUsers    = "users"
	mcpToolGroupTasks    = "tasks"
)

var mcpToolGroups = []string{mcpToolGroupRead, mcpToolGroupUpdate, mcpToolGroupInsert, mcpToolGroupComments, mcpToolGroupUsers, mcpToolGroupTasks}

// The tools that run work rather than read or write a page: Notion's async
// task, the API and apps' tools, outside services, migrations and the
// server's own.
var mcpTaskTools = map[string]bool{
	"notion-get-async-task": true, "leafen_operations": true, "leafen_call": true,
	"leafen_app_tools": true, "leafen_app_call": true, "leafen_app_storage": true,
	"leafen_services": true, "leafen_service_call": true, "leafen_gmail_guide": true,
	"leafen_remote_access": true, "leafen_server_move": true, "leafen_workspace_move": true, "leafen_standby": true,
	"leafen_connection_requests": true, "leafen_sign_in_requests": true, "leafen_server_version": true,
	"leafen_bug_report": true,
}

// The order Notion's token page lists its tools in, group by group (seen
// 2026-11-10). get-teams, which that page doesn't list, follows Notion's
// reading tools; Leafen's own come after Notion's in their group, by name.
var mcpNotionToolOrder = []string{
	"notion-search", "notion-ai-search", "notion-fetch", "notion-get-self", "notion-get-tool-access",
	"notion-download-attachment", "notion-get-file-download-urls", "notion-query-data-sources",
	"notion-query-multiple-data-sources", "notion-get-teams",
	"notion-update-page", "notion-convert-page-to-skill", "notion-update-data-source", "notion-create-view",
	"notion-update-view", "notion-move-pages",
	"notion-create-attachment", "notion-create-file-upload", "notion-create-pages", "notion-create-database",
	"notion-duplicate-page",
	"notion-create-comment", "notion-get-comments", "notion-get-users", "notion-get-async-task",
}

// mcpToolRank is a tool's place in its group: Notion's order, then the rest.
func mcpToolRank(name string) int {
	if index := slices.Index(mcpNotionToolOrder, name); index >= 0 {
		return index
	}
	return len(mcpNotionToolOrder)
}

// mcpToolGroup is the group a tool is shown in on a token's page.
func mcpToolGroup(name string) string {
	switch {
	case name == "notion-create-view":
		// Notion's page shows it under 콘텐츠 업데이트, a view being part of
		// the database it changes.
		return mcpToolGroupUpdate
	case strings.Contains(name, "comment"):
		return mcpToolGroupComments
	case name == "notion-get-users":
		return mcpToolGroupUsers
	case mcpTaskTools[name] || strings.HasPrefix(name, "leafen_notion_migration_"):
		return mcpToolGroupTasks
	case strings.Contains(name, "create") || strings.Contains(name, "duplicate") || name == "leafen_send":
		return mcpToolGroupInsert
	case mcpToolScope(name) <= userKeyScopeRead:
		return mcpToolGroupRead
	}
	return mcpToolGroupUpdate
}

type mcpCatalogTool struct {
	Name  string
	Title string
	Group string
}

var (
	mcpToolCatalogOnce sync.Once
	mcpToolCatalogList []mcpCatalogTool
)

// mcpToolCatalog lists the tools of both endpoints once, by name: a token
// works at either. Notion's calls Leafen answers only as unsupported are left
// out — there is nothing to turn on or off.
func mcpToolCatalog(app core.App) []mcpCatalogTool {
	mcpToolCatalogOnce.Do(func() {
		seen := map[string]bool{}
		for _, surface := range []mcpToolSurface{mcpToolSurfaceNative, mcpToolSurfaceStrict} {
			server := mcp.NewServer(&mcp.Implementation{Name: "leafen-catalog", Version: "2"}, nil)
			registerMCPTools(server, app, surface)
			for _, tool := range listRegisteredMCPTools(server) {
				if seen[tool.Name] || notionMCPToolUnsupported(tool.Name) {
					continue
				}
				seen[tool.Name] = true
				title := tool.Title
				if title == "" {
					title = tool.Name
				}
				mcpToolCatalogList = append(mcpToolCatalogList, mcpCatalogTool{Name: tool.Name, Title: title, Group: mcpToolGroup(tool.Name)})
			}
		}
		slices.SortStableFunc(mcpToolCatalogList, func(a, b mcpCatalogTool) int {
			return cmp.Or(slices.Index(mcpToolGroups, a.Group)-slices.Index(mcpToolGroups, b.Group),
				mcpToolRank(a.Name)-mcpToolRank(b.Name), strings.Compare(a.Name, b.Name))
		})
	})
	return mcpToolCatalogList
}

// listRegisteredMCPTools reads a server's tools as a client would, over an
// in-process connection.
func listRegisteredMCPTools(server *mcp.Server) []*mcp.Tool {
	ctx := context.Background()
	serverSide, clientSide := mcp.NewInMemoryTransports()
	session, err := server.Connect(ctx, serverSide, nil)
	if err != nil {
		return nil
	}
	defer session.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "leafen-catalog", Version: "2"}, nil).Connect(ctx, clientSide, nil)
	if err != nil {
		return nil
	}
	defer client.Close()
	tools := []*mcp.Tool{}
	for tool, err := range client.Tools(ctx, nil) {
		if err != nil {
			return tools
		}
		tools = append(tools, tool)
	}
	return tools
}

// mcpScopeForTools is the scope a token holds with these tools on: what the
// most demanding of them needs, read at least. The API, which a token also
// reaches, follows it.
func mcpScopeForTools(app core.App, disabled []string) userKeyScope {
	scope := userKeyScopeRead
	for _, tool := range mcpToolCatalog(app) {
		if !slices.Contains(disabled, tool.Name) {
			scope = max(scope, mcpToolScope(tool.Name))
		}
	}
	return scope
}

// parseMCPNameList reads a JSON list of names a connection keeps ("[]" for
// none).
func parseMCPNameList(raw string) []string {
	names := []string{}
	if json.Unmarshal([]byte(raw), &names) != nil {
		return []string{}
	}
	return names
}

// Tools a token narrowed to chosen pages keeps: those that read or change a
// page they are given, each checking it lies under the chosen ones
// (mcpAuthorityReachesPage), and those that touch no page. Every other tool
// — moving, copying, databases, views, files, the API — names places it
// can't be held to, so it is closed to such a token.
var mcpPageScopedTools = map[string]bool{
	"notion-fetch": true, "leafen_fetch": true, "notion-search": true, "leafen_search": true,
	"notion-update-page": true, "leafen_update_page": true, "notion-create-pages": true,
	"notion-get-comments": true, "notion-create-comment": true,
	"notion-get-users": true, "notion-get-teams": true, "leafen_list_workspaces": true, "notion-get-tool-access": true,
}

// mcpToolClosed says why a token can't use a tool, or "" when it can.
func mcpToolClosed(authority mcpAuthority, name string) string {
	if slices.Contains(authority.DisabledTools, name) {
		return "this token's page has " + name + " turned off"
	}
	if authority.PagesNarrowed && !mcpPageScopedTools[name] {
		return "this token reaches only the pages chosen on its page, and " + name + " can't be held to them"
	}
	return ""
}

// errMCPOutsideTokenPages refuses what lies outside the pages a token was
// narrowed to.
var errMCPOutsideTokenPages = mcpDenied("it isn't under the pages this token was narrowed to on its page in Leafen")

// mcpAuthorityReachesPage says whether a token reaches a page: any page while
// its page's "모든 페이지" is on, else a chosen page or one under it, a
// database's pages under the page holding the database — none with none
// chosen.
func mcpAuthorityReachesPage(app core.App, authority mcpAuthority, page *core.Record) bool {
	if !authority.PagesNarrowed {
		return true
	}
	for current, seen := page, map[string]bool{}; current != nil && !seen[current.Id]; {
		seen[current.Id] = true
		if slices.Contains(authority.PageIDs, current.Id) {
			return true
		}
		parentID, err := pageAncestryParentID(app, current)
		if err != nil || parentID == "" {
			return false
		}
		if current, err = app.FindRecordById(pagesCollection, parentID); err != nil {
			return false
		}
	}
	return false
}

// mcpReachKey names a token's reach for what is kept by it: every page, or
// the pages it was held to.
func mcpReachKey(authority mcpAuthority) []string {
	if !authority.PagesNarrowed {
		return []string{"*"}
	}
	return append([]string{"narrowed"}, authority.PageIDs...)
}

// mcpAuthorityReachesPageID is mcpAuthorityReachesPage for a page's ID.
func mcpAuthorityReachesPageID(app core.App, authority mcpAuthority, pageID string) bool {
	if !authority.PagesNarrowed {
		return true
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	return err == nil && mcpAuthorityReachesPage(app, authority, page)
}
