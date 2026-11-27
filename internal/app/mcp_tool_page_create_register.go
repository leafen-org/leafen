package app

import (
	"context"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// leafenMCPCreatePagesInput is the Leafen-native endpoint's create: a new
// page's <unknown url="…"/> line moves a database's item into it only with
// leave_database (the rule notion-move-pages keeps there, 2026-11-16: an
// item leaves its database only when the call says so, its values kept
// should it go back). The Notion-compatible endpoint moves it as Notion's
// MCP does (seen 2026-11-17), its answer saying so (mcpCreatePageMovesOf).
type leafenMCPCreatePagesInput struct {
	Parent        notionMCPCreatePagesParent      `json:"parent,omitempty" jsonschema:"Parent page, database, data source or team; left out, the pages are private pages at the workspace's top (on a connection reaching several, the one workspace_id names)"`
	Pages         []notionMCPCreatePageDefinition `json:"pages" jsonschema:"One to 100 pages created atomically under the parent"`
	AllowAsync    *bool                           `json:"allow_async,omitempty" jsonschema:"Leafen creates the pages within the call and answers with them, so true and false answer the same"`
	CreationMode  string                          `json:"creation_mode,omitempty" jsonschema:"draft makes private pages at the workspace's top, as leaving the parent out does; not with a parent"`
	LeaveDatabase bool                            `json:"leave_database,omitzero" jsonschema:"true to let a page's <unknown url=\"…\"/> line naming a database's item move that item into the new page, out of its database as a page of its own; without it such a call is refused and nothing is made"`
	Position      *notionMCPInsertPosition        `json:"position,omitempty" jsonschema:"Where the pages go in a page parent's content, in the order given: start or end (the default) for its top or bottom; after_content or before_content with an anchor to put them after the block where the anchor ends or before the block where it starts, at that block's level. Only for a page_id parent"`
}

func registerMCPPageCreateTool(server *mcp.Server, app core.App, surface mcpToolSurface) {
	tool := &mcp.Tool{
		Name: "notion-create-pages", Title: "Create Leafen pages",
		Description: notionPageMarkdownSpecRule + " Atomically create one to 100 pages with title and optional Markdown under one authorized parent: a page, or a database or data source to add them as its rows (entries), their values in properties. " +
			"A page's title goes in properties, not in content: content whose first line is a first-level heading (# …) — its only one, with more after it — " +
			"has that line taken as the title line, as Notion's: it isn't kept in the body, and it titles a page under a page given no title. " +
			"A body too long to write in the call comes from an uploaded .md file: content_file_upload_id.",
	}
	if surface == mcpToolSurfaceNative {
		tool.Description += " A line <unknown url=\"…\"/> naming a page here moves that page into the new page; a database's item moves so " +
			"only with leave_database true: it leaves its database as a page of its own, its values kept should it go back."
		mcp.AddTool(server, tool, func(ctx context.Context, request *mcp.CallToolRequest, input leafenMCPCreatePagesInput) (*mcp.CallToolResult, notionMCPCreatePagesOutput, error) {
			pages := slices.Clone(input.Pages)
			for index := range pages {
				pages[index].rowsStay = !input.LeaveDatabase
			}
			result, output, err := createMCPPagesPlaced(ctx, request, app, notionMCPCreatePagesInput{
				Parent: input.Parent, Pages: pages, AllowAsync: input.AllowAsync, CreationMode: input.CreationMode, Position: input.Position,
			})
			return result, output, mcpNamedRefusal(ctx, app, err, mcpPage(input.Parent.PageID), mcpDatabase(input.Parent.DatabaseID))
		})
		return
	}
	mcp.AddTool(server, tool, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPCreatePagesInput) (*mcp.CallToolResult, notionMCPCreatePagesOutput, error) {
		result, output, err := createMCPPagesPlaced(ctx, request, app, input)
		return result, output, mcpNamedRefusal(ctx, app, err, mcpPage(input.Parent.PageID), mcpDatabase(input.Parent.DatabaseID))
	})
}
