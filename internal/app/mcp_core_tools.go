package app

import (
	"context"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// The native endpoint lists its core tools first (the Notion migration
// decision's change 4, 2026-11-20: an agent given about 90 tools, Notion's
// and Leafen's mixed, lost its way). Its list holds Leafen's own tools that
// agents actually called on Leafen — 338 calls of Claude Code on this
// project's Leafen servers, read 2026-11-20 —, the ones its instructions send
// an agent to and the Notion move's whole road, beside Notion's tools as
// Notion's own MCP lists them (mcpNotionListedTools). leafen_operations finds
// every other by words and leafen_call runs it by name and arguments, as a
// call by its name still does where a program allows one. Notion's other
// names stay, the strict endpoint's list keeping all of them: a connection
// turns them on for its list when it needs them (mcp_token_notion_tools.go),
// and one whose page lists Notion's tools Leafen doesn't have lists them too.

// mcpCoreTools are the tools every connection's list holds, its scope and
// its page allowing.
var mcpCoreTools = map[string]bool{
	// The person's notices, read when they ask about them or the work at
	// hand concerns one (mcp_inbox.go; 2026-11-24, no longer as a
	// conversation starts) — found on the list by its name, however they
	// put it —, and the workspaces the connection reaches.
	"leafen_inbox": true, "leafen_list_workspaces": true,
	// Finding: pages, databases, and every other tool or API operation.
	"leafen_search": true, "leafen_catalog": true, "leafen_operations": true,
	// Running what leafen_operations finds; the codes, the report and the
	// outside services the instructions name.
	"leafen_call": true, "leafen_sign_in_requests": true, "leafen_connection_requests": true, "leafen_bug_report": true,
	"leafen_services": true,
	// A Notion move, start to end (notion_migration_guide.md): the guide
	// always, the move's other tools where the connection has a move to make
	// (mcpNotionMoveTools).
	"leafen_notion_migration_guide": true,
}

// mcpNotionMoveTools are a Notion move's tools after its guide. Every
// connection lists them — a new Leafen's first connection too, whose first
// ten minutes they make — but one with no move to make, which leaves them
// out (mcp_notion_move_tools_left_out.go): its guide's answer runs them
// through leafen_call then (notion_migration_guide_calls.go).
var mcpNotionMoveTools = map[string]bool{
	"leafen_notion_migration_sources": true, "leafen_notion_migration_connect": true,
	"leafen_notion_migration_start": true, "leafen_notion_migration_scan": true, "leafen_notion_migration_items": true,
	"leafen_notion_migration_plan": true, "leafen_notion_migration_set_plan": true, "leafen_notion_migration_formats": true,
	"leafen_notion_migration_run": true, "leafen_notion_migration_status": true, "leafen_notion_migration_verify": true,
}

// mcpNotionListedTools are Notion's tools as Notion's own MCP lists them, by
// its supported-tools pages (developers.notion.com/guides/mcp/tools-*, read
// 2026-11-26) and in their order: its server lists every one to every
// connection, bounded only by the workspace's plan and an administrator's
// limit, with no shorter first list. A connection's list holds each of them
// Leafen has (the user, 2026-11-26: an agent tidying 200 rows had to look up
// adding a column, moving rows and deleting a database one by one, so the
// tools an agent first sees are Notion's first ones); those Leafen doesn't
// have stay off it unless its page lists them (mcp_notion_only_tools.go).
// The notion-* names Notion's pages don't list — the claude.ai connector's
// page view, sidebar tabs, favorites and setup guide, seen 2026-11-13 — wait
// behind the connection's switch with the rest.
var mcpNotionListedTools = map[string]bool{
	// Search and read.
	"notion-search": true, "notion-connector-search": true, "notion-fetch": true, "notion-list-private-pages": true,
	"notion-list-shared-pages": true, "notion-list-favorite-pages": true, "notion-list-recent-pages": true,
	"notion-list-page-backlinks": true, "notion-query-meeting-notes": true, "notion-get-self": true, "notion-get-teams": true,
	"notion-get-groups": true, "notion-get-users": true, "notion-get-tool-access": true, "notion-ai-search": true,
	// History, comments and suggested edits.
	"notion-get-version-changes": true, "notion-view-version": true, "notion-restore-version": true, "notion-restore-pages": true,
	"notion-create-comment": true, "notion-get-comments": true, "notion-update-comment": true, "notion-delete-comment": true,
	"notion-set-discussion-resolved": true, "notion-set-comment-reaction": true, "notion-create-suggested-edit": true,
	"notion-get-suggested-edit": true, "notion-resolve-suggested-edit": true,
	// Pages.
	"notion-create-pages": true, "notion-update-page": true, "notion-move-pages": true, "notion-duplicate-page": true,
	"notion-delete-pages": true, "notion-get-async-task": true,
	// Databases and views.
	"notion-create-database": true, "notion-update-data-source": true, "notion-create-view": true, "notion-update-view": true,
	"notion-query-data-sources": true, "notion-query-multiple-data-sources": true, "notion-delete-databases": true,
	// Files.
	"notion-create-file-upload": true, "notion-create-attachment": true, "notion-download-attachment": true,
	"notion-get-file-download-urls": true, "notion-create-folder": true, "notion-update-folder": true,
	// Skills, memory and Custom Agents.
	"notion-convert-page-to-skill": true, "notion-download-skill": true, "notion-upload-skill": true,
	"notion-get-enabled-skills": true, "notion-update-enabled-skills": true, "notion-memory-search": true,
	"notion-search-agents": true, "notion-query-sessions": true, "notion-search-sessions": true, "notion-spawn-session": true,
	"notion-send-message-to-session": true, "notion-get-session-status": true, "notion-wait-session": true,
	"notion-stop-session": true, "notion-list-session-events": true, "notion-read-session-event": true,
	// Calendars.
	"notion-calendar-list-calendars": true, "notion-calendar-list-events": true, "notion-calendar-list-coworkers-events": true,
	"notion-calendar-list-calendar-resources": true, "notion-calendar-list-contacts": true, "notion-calendar-create-events": true,
	"notion-calendar-update-events": true, "notion-calendar-cancel-events": true,
}

// The tools listed only where they have something to work on: calling an
// outside service where the connection reaches one, and the apps' tools
// where an app gives it any, as the instructions name them then
// (mcp_session_instructions.go).
var (
	mcpServiceTools = []string{"leafen_service_call"}
	mcpAppToolTools = []string{"leafen_app_tools", "leafen_app_call"}
)

// mcpCoreToolList leaves the native endpoint's list to its core tools. It
// wraps leafen_call's and leafen_operations' own handling
// (mcp_named_tools.go), whose own reading of the list still sees every tool
// the connection may call.
func mcpCoreToolList(app core.App) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, request)
			listed, ok := result.(*mcp.ListToolsResult)
			if err != nil || method != "tools/list" || !ok || listed == nil {
				return result, err
			}
			authority, authorityErr := mcpAuthorityFromContext(ctx)
			if authorityErr != nil {
				return result, err
			}
			apps := slices.ContainsFunc(listed.Tools, func(tool *mcp.Tool) bool { return mcpAppOwnTool(tool.Name) })
			services := false
			if found, _, err := listMCPServiceWindow(app, authority.owner(), authority.ConnectionID, "", 1); err == nil {
				services = len(found) > 0
			}
			moves := mcpNotionMoveToolsLeftOut(app, authority) == ""
			shown := *listed
			shown.Tools = slices.DeleteFunc(slices.Clone(listed.Tools), func(tool *mcp.Tool) bool {
				return !mcpToolOnCoreList(authority, tool.Name, services, apps) && !(moves && mcpNotionMoveTools[tool.Name])
			})
			return &shown, nil
		}
	}
}

// mcpToolOnCoreList says whether the connection's list holds the tool.
func mcpToolOnCoreList(authority mcpAuthority, name string, services, apps bool) bool {
	switch {
	case mcpCoreTools[name], mcpNotionListedTools[name], mcpAppOwnTool(name):
		return true
	case strings.HasPrefix(name, "notion-"):
		return authority.NotionTools || authority.NotionOnlyTools
	case slices.Contains(mcpServiceTools, name):
		return services
	case slices.Contains(mcpAppToolTools, name):
		return apps
	}
	return false
}

// mcpAppOwnTool is an installed app's own tool, which the person gave the
// connection by installing it (app_platform_agent_mcp.go).
func mcpAppOwnTool(name string) bool {
	return strings.HasPrefix(name, "app_")
}
