package app

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionMCPToolAvailable  = "available"
	notionMCPToolNotEnabled = "not_enabled"
)

type notionMCPToolAccessInput struct {
	ToolNames *[]string `json:"tool_names,omitempty" jsonschema:"Optional official base names to inspect, without the notion- prefix"`
}

type notionMCPToolAccessEntry struct {
	Status string `json:"status"`
}

type notionMCPToolAccessOutput struct {
	CurrentToolAccess map[string]notionMCPToolAccessEntry `json:"current_tool_access"`
}

// notion-get-tool-access names each of Notion's tools this server lists, by
// Notion's base name: available for the ones Leafen does, not_enabled for
// those it answers with why and what to use instead
// (mcp_tool_unsupported.go). It reads them from the tools registered, so it
// can't fall behind the list (2026-11-13: 44 of Notion's 78 were neither); the
// gate holds that list to Notion's own, as read on a date.
func notionMCPToolAccess(app core.App) map[string]string {
	access := map[string]string{notionMCPToolBaseName(notionMeetingNotesMCPToolName): notionMCPToolNotEnabled}
	for _, spec := range notionMCPUnsupportedTools {
		access[notionMCPToolBaseName(spec.Name)] = notionMCPToolNotEnabled
	}
	for _, tool := range mcpToolCatalog(app) {
		if strings.HasPrefix(tool.Name, "notion-") {
			access[notionMCPToolBaseName(tool.Name)] = notionMCPToolAvailable
		}
	}
	return access
}

// notionMCPToolBaseName is a tool's name as Notion's tool access names it:
// notion-get-tool-access is get_tool_access.
func notionMCPToolBaseName(name string) string {
	return strings.ReplaceAll(strings.TrimPrefix(name, "notion-"), "-", "_")
}

func registerMCPToolAccess(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-get-tool-access", Title: "Check Leafen tool access",
		Description: "Report access for the official tools exposed by this fixed-scope connection.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPToolAccessInput) (*mcp.CallToolResult, notionMCPToolAccessOutput, error) {
		return readMCPToolAccess(ctx, app, input)
	})
}

func readMCPToolAccess(
	ctx context.Context,
	app core.App,
	input notionMCPToolAccessInput,
) (*mcp.CallToolResult, notionMCPToolAccessOutput, error) {
	if _, err := mcpAuthorityFromContext(ctx); err != nil {
		return nil, notionMCPToolAccessOutput{}, err
	}
	requested := map[string]struct{}{}
	if input.ToolNames != nil {
		for _, name := range *input.ToolNames {
			requested[name] = struct{}{}
		}
	}
	current := map[string]notionMCPToolAccessEntry{}
	for name, status := range notionMCPToolAccess(app) {
		if input.ToolNames != nil {
			if _, ok := requested[name]; !ok {
				continue
			}
		}
		current[name] = notionMCPToolAccessEntry{Status: status}
	}
	return nil, notionMCPToolAccessOutput{CurrentToolAccess: current}, nil
}
