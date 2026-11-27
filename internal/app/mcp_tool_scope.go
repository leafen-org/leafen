package app

import (
	"context"
	"fmt"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// What a tool needs, as the native API's routes do: a tool that reads needs
// read, one that changes pages, rows, comments or files needs content, and
// one that changes a database's structure or its views needs structure. A
// tool not named here changes content. A connection does what its person
// may — an MCP token with the tools its page leaves on (mcpScopeForTools) —,
// held to what its workspaces' organizations allow keys
// (connection_person_rights.go).
var mcpToolScopes = map[string]userKeyScope{
	"leafen_fetch": userKeyScopeRead, "leafen_search": userKeyScopeRead, "leafen_list_workspaces": userKeyScopeRead,
	"leafen_get_file": userKeyScopeRead, "leafen_workday": userKeyScopeRead,
	// Calling a service's tool that changes things needs content, checked in the call.
	"leafen_services": userKeyScopeRead, "leafen_service_call": userKeyScopeRead,
	"leafen_notion_migration_sources": userKeyScopeRead, "leafen_notion_migration_plan": userKeyScopeRead,
	"leafen_notion_migration_status": userKeyScopeRead, "leafen_notion_migration_guide": userKeyScopeRead,
	"leafen_notion_migration_items": userKeyScopeRead, "leafen_gmail_guide": userKeyScopeRead,
	// The operation it runs holds it to that operation's scope. Listing apps'
	// tools reads; running one (leafen_app_call) changes content.
	"leafen_operations": userKeyScopeRead, "leafen_call": userKeyScopeRead, "leafen_app_tools": userKeyScopeRead,
	// Changing it needs content, checked in the call.
	"leafen_app_storage": userKeyScopeRead,
	"notion-fetch":       userKeyScopeRead, "notion-search": userKeyScopeRead, "notion-ai-search": userKeyScopeRead,
	"notion-connector-search": userKeyScopeRead,
	"notion-get-comments":     userKeyScopeRead, "notion-get-teams": userKeyScopeRead, "notion-get-users": userKeyScopeRead,
	"notion-query-data-sources": userKeyScopeRead, "notion-get-async-task": userKeyScopeRead,
	"notion-download-attachment": userKeyScopeRead, "notion-get-tool-access": userKeyScopeRead,
	"notion-get-session-status": userKeyScopeRead, "notion-list-agents": userKeyScopeRead,
	"notion-list-session-events": userKeyScopeRead, "notion-query-sessions": userKeyScopeRead,
	"notion-read-session-event": userKeyScopeRead, "notion-search-agents": userKeyScopeRead,
	"notion-search-sessions": userKeyScopeRead, "notion-wait-session": userKeyScopeRead,
	"notion-calendar-list-calendars": userKeyScopeRead, "notion-calendar-list-events": userKeyScopeRead,
	"notion-calendar-list-coworkers-events": userKeyScopeRead, "notion-calendar-list-calendar-resources": userKeyScopeRead,
	"notion-calendar-list-contacts": userKeyScopeRead, "notion-get-enabled-skills": userKeyScopeRead,
	"notion-get-setup-guide": userKeyScopeRead, "notion-memory-search": userKeyScopeRead,
	"notion-get-self": userKeyScopeRead, "notion-get-groups": userKeyScopeRead,
	"notion-list-page-backlinks": userKeyScopeRead, "notion-list-favorite-pages": userKeyScopeRead,
	"notion-list-recent-pages": userKeyScopeRead, "notion-page-view": userKeyScopeRead,
	"notion-query-multiple-data-sources": userKeyScopeRead, "notion-get-file-download-urls": userKeyScopeRead,
	"notion-list-private-pages": userKeyScopeRead, "notion-list-shared-pages": userKeyScopeRead,
	"notion-get-sidebar-tabs": userKeyScopeRead, "notion-view-version": userKeyScopeRead,
	"notion-get-version-changes": userKeyScopeRead, "notion-get-suggested-edit": userKeyScopeRead,
	// A sidebar's tabs are kept as the screen keeps them, by its structure rights.
	"notion-update-sidebar-tabs": userKeyScopeStructure,
	"notion-create-database":     userKeyScopeStructure, "notion-update-data-source": userKeyScopeStructure,
	"notion-create-view": userKeyScopeStructure, "notion-update-view": userKeyScopeStructure, "leafen_delete_view": userKeyScopeStructure,
	"leafen_database_page_layout": userKeyScopeStructure, "leafen_inbox": userKeyScopeRead, "leafen_inbox_item": userKeyScopeRead,
	"leafen_inbox_remind": userKeyScopeRead, "leafen_inbox_read": userKeyScopeContent, "leafen_send": userKeyScopeContent,
	// The server's remote access is its administrator's, on an admin connection.
	"leafen_remote_access": userKeyScopeAdmin,
	"leafen_server_move":   userKeyScopeAdmin,
	// A workspace's move is followed by reading; its owner checks in the call.
	"leafen_workspace_move": userKeyScopeRead,
	"leafen_standby":        userKeyScopeAdmin,
	// What build the server runs is any connection's to read, and so are a
	// workspace's databases and columns.
	"leafen_server_version": userKeyScopeRead, "leafen_catalog": userKeyScopeRead,
	// Drafting a bug report changes nothing; the agent posts it on GitHub.
	"leafen_bug_report": userKeyScopeRead,
	// Another agent's connection is the person's own, as its key's making is.
	"leafen_connection_requests": userKeyScopeAdmin,
	// A browser signed in as the person is the person's own, as a device code is.
	"leafen_sign_in_requests": userKeyScopeAdmin,
}

func mcpToolScope(name string) userKeyScope {
	if scope, ok := mcpToolScopes[name]; ok {
		return scope
	}
	return userKeyScopeContent
}

// mcpToolScopeGuard refuses a tool the connection's tools and its
// organizations' key policy don't reach, or its token's page closed
// (mcp_token_tools.go) or, for the server's administration, the rule of the
// routes it does the work of (mcpToolRouteRefusal), and leaves such tools
// out of its list, so an agent is offered only what it can call (agent
// full-permission decision of 2026-10-16, 3); Notion's tools Leafen doesn't
// have stay off the list too unless its page lists them (mcp_notion_only_tools.go).
func mcpToolScopeGuard(app core.App) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				result, err := next(ctx, method, request)
				listed, ok := result.(*mcp.ListToolsResult)
				authority, authorityErr := mcpAuthorityFromContext(ctx)
				if err != nil || !ok || authorityErr != nil {
					return result, err
				}
				// The server's administration only where its routes' rule lets the connection (connection_kind_ways.go).
				refused := map[string]bool{}
				for name := range mcpServerAdministrationTools {
					refusal, err := mcpToolRouteRefusal(app, authority, name)
					refused[name] = err != nil || refusal != ""
				}
				listed.Tools = slices.DeleteFunc(listed.Tools, func(tool *mcp.Tool) bool {
					return authority.Scope < mcpToolScope(tool.Name) || mcpToolClosed(authority, tool.Name) != "" ||
						mcpNotionOnlyToolHidden(authority, tool.Name) || refused[tool.Name]
				})
				return listed, nil
			}
			call, ok := request.(*mcp.CallToolRequest)
			if method != "tools/call" || !ok || call.Params == nil {
				return next(ctx, method, request)
			}
			authority, err := mcpAuthorityFromContext(ctx)
			if err != nil {
				return next(ctx, method, request)
			}
			// The refusal is the tool's answer, which the agent reads, as any
			// other refusal of a call is.
			if required := mcpToolScope(call.Params.Name); authority.Scope < required {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(
					"%s needs the %s scope and this connection reaches %s here. %s",
					call.Params.Name, userKeyScopeNames[required], userKeyScopeNames[authority.Scope], mcpConnectionWiden(authority))}}}, nil
			}
			if closed := mcpToolClosed(authority, call.Params.Name); closed != "" {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: closed}}}, nil
			}
			if refusal, err := mcpToolRouteRefusal(app, authority, call.Params.Name); err != nil {
				return nil, err
			} else if refusal != "" {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: refusal}}}, nil
			}
			return next(ctx, method, request)
		}
	}
}

// mcpInvalidParams refuses a request's parameters with JSON-RPC's own code,
// as the SDK refuses a request it can't read, rather than a bare error the
// wire would carry as code 0.
func mcpInvalidParams(message string) error {
	return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: message}
}
