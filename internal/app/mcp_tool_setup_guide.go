package app

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's setup guide and memory search, as Leafen has them (Notion's MCP,
// seen 2026-11-13): the guide is how agents use Leafen — what the native
// endpoint tells each one at the start of a session — and Leafen's memory is
// the person's workspace itself, so a memory search searches it.

type notionMCPSetupGuideInput struct{}

type notionMCPSetupGuideOutput struct {
	Guide string `json:"guide"`
}

type notionMCPMemorySearchInput struct {
	Query string `json:"query" jsonschema:"Words or a short phrase to find in what the person and their agents keep in Leafen"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum results to return, from 1 to 50 (default 10)"`
}

func registerMCPSetupGuideTools(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations, surface mcpToolSurface) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-get-setup-guide", Title: "Get Leafen's guide for agents",
		Description: "Leafen's guide for agents: how to use Leafen as the person's shared workspace and memory, as this server tells each agent when it connects. Follow it with the person.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ notionMCPSetupGuideInput) (*mcp.CallToolResult, notionMCPSetupGuideOutput, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, notionMCPSetupGuideOutput{}, err
		}
		return nil, notionMCPSetupGuideOutput{Guide: mcpPersonalizedInstructions(ctx, app, authority, mcpNativeInstructionsIn(mcpScreenLanguage(app, authority.owner())), 0)}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-memory-search", Title: "Search what Leafen keeps",
		Description: "Search the person's memory: in Leafen that is their workspace — the pages and databases where they and their agents keep context, preferences, corrections and decisions — searched as notion-search searches it. To keep something new, write it into the page or database where it belongs.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPMemorySearchInput) (*mcp.CallToolResult, notionMCPSearchOutput, error) {
		if input.Limit < 0 || input.Limit > 50 {
			return nil, notionMCPSearchOutput{}, mcpInvalid("limit is from 1 to 50")
		}
		return searchNotionMCP(ctx, app, notionMCPSearchInput{Query: input.Query, PageSize: input.Limit, native: surface == mcpToolSurfaceNative, once: true})
	})
}
