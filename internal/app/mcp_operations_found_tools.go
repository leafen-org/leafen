package app

import (
	"context"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// leafen_operations lists, beside the operations, Leafen's tools, read from
// the tools this connection lists, so a name it gives is one the connection
// can call (2026-11-16, an agent calling every operation: its tools field was
// never filled, though its description promised Leafen's tools with their
// input): a group's tools with its operations and counted in the map, the
// tools English words find, an operation's group's tools its name names
// (POST /pages, createPage: notion-create-pages), and a tool's name given as
// operation, that tool with its input schema (native_operation_group_answers.go).
// A program that loads tools only when asked (deferred tools) finds them
// here; leafen_call runs one by tool and arguments.

type mcpOperationsToolsKey struct{}

// mcpOperationsWithTools answers leafen_operations with the connection's
// tools at hand: every tool its list may call that does something,
// leafen_operations and leafen_call aside.
func mcpOperationsWithTools(ctx context.Context, next mcp.MethodHandler, call *mcp.CallToolRequest) (mcp.Result, error) {
	listed, err := next(ctx, "tools/list", &mcp.ListToolsRequest{Session: call.Session, Params: &mcp.ListToolsParams{}, Extra: call.Extra})
	if err != nil {
		return next(ctx, "tools/call", call)
	}
	tools := []nativeOperationTool{}
	if typed, ok := listed.(*mcp.ListToolsResult); ok {
		for _, tool := range typed.Tools {
			// The map itself, and Notion's tools Leafen doesn't have, which a
			// connection showing Notion's names lists and which do nothing.
			if tool.Name == "leafen_operations" || tool.Name == "leafen_call" || notionMCPToolUnsupported(tool.Name) {
				continue
			}
			tools = append(tools, nativeOperationTool{name: tool.Name, title: tool.Title, description: tool.Description,
				readOnly: tool.Annotations != nil && tool.Annotations.ReadOnlyHint, inputSchema: tool.InputSchema})
		}
	}
	return next(context.WithValue(ctx, mcpOperationsToolsKey{}, slices.Clip(tools)), "tools/call", call)
}

// mcpOperationsToolsOf are the tools mcpOperationsWithTools put at hand.
func mcpOperationsToolsOf(ctx context.Context) []nativeOperationTool {
	tools, _ := ctx.Value(mcpOperationsToolsKey{}).([]nativeOperationTool)
	return tools
}

// mcpFirstSentence is a description's first sentence.
func mcpFirstSentence(text string) string {
	for index := range len(text) {
		if text[index] == '.' && (index+1 == len(text) || text[index+1] == ' ' || text[index+1] == '\n') {
			return text[:index+1]
		}
	}
	return text
}
