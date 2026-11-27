package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// leafen_call runs an API operation, or a tool by its name, which this
// server answers as that tool, through the same scope and record checks as a
// tool on the list (the full API and MCP decision's addition 6, 2026-10-28);
// leafen_operations finds the operations by words.

// leafenMCPFoundTool is one of Leafen's tools as a search finds it.
type leafenMCPFoundTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema,omitempty"`
	ReadOnly    bool   `json:"readOnly,omitempty"`
}

// mcpNamedTools runs a tool leafen_call names as that tool.
func mcpNamedTools(app core.App) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, request)
			}
			call, isCall := request.(*mcp.CallToolRequest)
			// leafen_operations names the tools that do the work too
			// (mcp_operations_found_tools.go).
			if isCall && call.Params != nil && call.Params.Name == "leafen_operations" {
				return mcpOperationsWithTools(ctx, next, call)
			}
			if isCall && call.Params != nil && call.Params.Name == "leafen_call" {
				if tool, arguments, named := mcpCallNamedTool(call.Params.Arguments); named {
					return mcpRunNamedTool(ctx, next, call, tool, arguments)
				}
			}
			return next(ctx, method, request)
		}
	}
}

// mcpCallNamedTool reads a leafen_call that names a tool rather than an
// operation.
func mcpCallNamedTool(raw json.RawMessage) (string, json.RawMessage, bool) {
	var input struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(raw, &input) != nil || strings.TrimSpace(input.Tool) == "" {
		return "", nil, false
	}
	if len(input.Arguments) == 0 || string(input.Arguments) == "null" {
		input.Arguments = json.RawMessage(`{}`)
	}
	return strings.TrimSpace(input.Tool), input.Arguments, true
}

// mcpRunNamedTool runs the tool a leafen_call names as that tool, and gives
// its answer in leafen_call's shape: the tool's result as the body, as the
// tool wrote it — its keys in their order, its text unescaped
// (mcpAnswersInOrder) — so that the body reads as the tool's own answer, not
// as it again with <, > and & as \u003c… and its keys sorted (2026-11-26: a
// page's values through leafen_call came wrapped in escapes a direct call
// hasn't).
func mcpRunNamedTool(ctx context.Context, next mcp.MethodHandler, call *mcp.CallToolRequest,
	tool string, arguments json.RawMessage) (mcp.Result, error) {
	if tool == "leafen_call" || tool == "leafen_operations" {
		return mcpToolText("Call "+tool+" itself.", true), nil
	}
	call.Params.Name, call.Params.Arguments = tool, arguments
	result, err := next(ctx, "tools/call", call)
	// An operation's name or id given as a tool says how to run it
	// (native_operation_lookup.go).
	if err != nil && strings.Contains(err.Error(), "unknown tool") {
		shaped := nativeOperationResult{Status: 404, Text: nativeOperationNotATool(tool)}
		data, _ := json.Marshal(shaped)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}, StructuredContent: shaped, IsError: true}, nil
	}
	if err != nil {
		return result, err
	}
	answer, ok := result.(*mcp.CallToolResult)
	if !ok || answer == nil {
		return result, nil
	}
	shaped := nativeOperationResult{Status: 200, Body: mcpNamedToolBody(answer)}
	if answer.IsError {
		shaped.Status = 400
	}
	if shaped.Body == nil {
		shaped.Text = mcpToolResultText(answer)
	}
	data, err := mcpFetchJSON(shaped)
	if err != nil {
		return nil, failedBecause(errMCPToolFailed, err)
	}
	out := *answer
	out.Content, out.StructuredContent = []mcp.Content{&mcp.TextContent{Text: data}}, shaped
	return &out, nil
}

// mcpNamedToolBody is a tool's answer as leafen_call's body: its text when
// that is JSON saying what its structured content does, as the tool wrote
// it; else its structured content; else its text when that is JSON; nil for
// a text that isn't.
func mcpNamedToolBody(answer *mcp.CallToolResult) any {
	text := mcpToolResultText(answer)
	if !json.Valid([]byte(text)) {
		return answer.StructuredContent
	}
	if answer.StructuredContent == nil {
		return json.RawMessage(text)
	}
	var written, structured any
	data, err := json.Marshal(answer.StructuredContent)
	if err != nil || json.Unmarshal([]byte(text), &written) != nil || json.Unmarshal(data, &structured) != nil ||
		!reflect.DeepEqual(written, structured) {
		return answer.StructuredContent
	}
	return json.RawMessage(text)
}

func mcpToolText(text string, failed bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, IsError: failed}
}

// mcpToolResultText is a result's text content.
func mcpToolResultText(result *mcp.CallToolResult) string {
	parts := []string{}
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}
