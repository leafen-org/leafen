package app

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A tool's arguments that its input schema refuses are answered in the
// words of the operations' refusals (api_error_missing.go): which arguments
// it doesn't take, which it needs, which have the wrong type, and every
// argument it takes, the needed ones marked — not the schema checker's own
// line alone (2026-11-26 scorecard: "validating root: unexpected additional
// properties [...]" left an agent guessing notion-update-data-source's
// arguments four times over and giving up). It is the one place every tool's
// schema refusal passes, on every MCP endpoint, whether the tool is called by
// its name or through leafen_call's tool (mcp_named_tools.go): the receiving
// middleware nearest the tools (mcp_transport.go).

// mcpArgumentsRefusalPrefix starts the line the MCP library answers a tool
// whose arguments its schema refuses with.
const mcpArgumentsRefusalPrefix = `validating "arguments": `

var (
	mcpArgumentsUnexpected = regexp.MustCompile(`unexpected additional properties \[([^\]]*)\]`)
	mcpArgumentsMissing    = regexp.MustCompile(`missing properties: \[([^\]]*)\]`)
	mcpArgumentsWrongType  = regexp.MustCompile(`validating /properties/([^:]+): type: .* has type "([^"]+)", want "([^"]+)"`)
	mcpArgumentsQuoted     = regexp.MustCompile(`"([^"]+)"`)
)

// mcpToolArgument is one argument a tool's input schema names.
type mcpToolArgument struct {
	name, kind string
	required   bool
}

// mcpToolArgumentRefusals explains every tool call's schema refusal.
func mcpToolArgumentRefusals() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, request)
			call, isCall := request.(*mcp.CallToolRequest)
			if err != nil || method != "tools/call" || !isCall || call.Params == nil {
				return result, err
			}
			return mcpExplainArgumentsRefusal(ctx, next, call, call.Params.Name, result), nil
		}
	}
}

// mcpExplainArgumentsRefusal gives a tool's schema refusal the arguments it
// took amiss and those it takes; any other answer stays as it is.
func mcpExplainArgumentsRefusal(ctx context.Context, next mcp.MethodHandler, call *mcp.CallToolRequest, tool string, result mcp.Result) mcp.Result {
	answer, ok := result.(*mcp.CallToolResult)
	if !ok || answer == nil || !answer.IsError {
		return result
	}
	detail, refused := strings.CutPrefix(mcpToolResultText(answer), mcpArgumentsRefusalPrefix)
	if !refused {
		return result
	}
	arguments, mapped := mcpToolArguments(ctx, next, call, tool)
	explained := *answer
	explained.Content = []mcp.Content{&mcp.TextContent{Text: mcpArgumentsRefusalWords(tool, detail, arguments, mapped)}}
	return &explained
}

// mcpArgumentsRefusalWords says what a schema refusal found and what the
// tool takes; mapped is the endpoint having leafen_operations, which gives
// each argument's description.
func mcpArgumentsRefusalWords(tool, detail string, arguments []mcpToolArgument, mapped bool) string {
	kindOf := map[string]string{}
	for _, argument := range arguments {
		kindOf[argument.name] = argument.kind
	}
	parts := []string{}
	if match := mcpArgumentsUnexpected.FindStringSubmatch(detail); match != nil {
		parts = append(parts, fmt.Sprintf("%s doesn't take %s.", tool, strings.Join(mcpArgumentsNames(match[1]), ", ")))
	}
	if match := mcpArgumentsMissing.FindStringSubmatch(detail); match != nil {
		needed := []string{}
		for _, name := range mcpArgumentsNames(match[1]) {
			needed = append(needed, mcpArgumentWithKind(name, kindOf[name]))
		}
		parts = append(parts, "Missing what this tool needs: "+strings.Join(needed, "; ")+".")
	}
	if match := mcpArgumentsWrongType.FindStringSubmatch(detail); match != nil {
		parts = append(parts, fmt.Sprintf("Check %s: it takes %s, not %s.", strings.ReplaceAll(match[1], "/properties/", "."), match[3], match[2]))
	}
	if len(parts) == 0 {
		parts = append(parts, "Check its arguments: "+detail+".")
	}
	if len(arguments) > 0 {
		takes := make([]string, 0, len(arguments))
		for _, argument := range arguments {
			words := argument.name + " (" + argument.kind
			if argument.required {
				words += ", required"
			}
			takes = append(takes, words+")")
		}
		parts = append(parts, fmt.Sprintf("%s takes: %s.", tool, strings.Join(takes, ", ")))
		// leafen_operations describes every tool but itself and leafen_call, whose descriptions the list holds.
		if mapped && tool != "leafen_operations" && tool != "leafen_call" {
			parts = append(parts, fmt.Sprintf("leafen_operations with operation %q gives each one's description.", tool))
		}
	}
	return strings.Join(parts, " ")
}

// mcpArgumentsNames are the quoted names a schema refusal lists.
func mcpArgumentsNames(list string) []string {
	names := []string{}
	for _, match := range mcpArgumentsQuoted.FindAllStringSubmatch(list, -1) {
		names = append(names, match[1])
	}
	return names
}

func mcpArgumentWithKind(name, kind string) string {
	if kind == "" {
		return name
	}
	return name + " (" + kind + ")"
}

// mcpToolArguments are the arguments a tool of the server's takes, the
// needed ones first and then by name, and whether the server has
// leafen_operations (the native endpoint).
func mcpToolArguments(ctx context.Context, next mcp.MethodHandler, call *mcp.CallToolRequest, tool string) ([]mcpToolArgument, bool) {
	var arguments []mcpToolArgument
	mapped, cursor := false, ""
	for range 10 {
		listed, err := next(ctx, "tools/list", &mcp.ListToolsRequest{Session: call.Session, Params: &mcp.ListToolsParams{Cursor: cursor}, Extra: call.Extra})
		typed, ok := listed.(*mcp.ListToolsResult)
		if err != nil || !ok {
			break
		}
		for _, candidate := range typed.Tools {
			switch candidate.Name {
			case tool:
				arguments = mcpSchemaArguments(candidate.InputSchema)
			case "leafen_operations":
				mapped = true
			}
		}
		if cursor = typed.NextCursor; cursor == "" {
			break
		}
	}
	return arguments, mapped
}

// mcpSchemaArguments reads an input schema's properties: each one's name and
// type, and whether the schema requires it.
func mcpSchemaArguments(schema any) []mcpToolArgument {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var read struct {
		Properties map[string]struct {
			Type any `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if json.Unmarshal(data, &read) != nil {
		return nil
	}
	arguments := make([]mcpToolArgument, 0, len(read.Properties))
	for name, property := range read.Properties {
		arguments = append(arguments, mcpToolArgument{name: name, kind: mcpSchemaKind(property.Type), required: slices.Contains(read.Required, name)})
	}
	slices.SortFunc(arguments, func(a, b mcpToolArgument) int {
		if a.required != b.required {
			if a.required {
				return -1
			}
			return 1
		}
		return strings.Compare(a.name, b.name)
	})
	return arguments
}

// mcpSchemaKind is a property's type as a word: its one type, or those other
// than null where it takes several.
func mcpSchemaKind(kind any) string {
	switch typed := kind.(type) {
	case string:
		return typed
	case []any:
		kinds := []string{}
		for _, one := range typed {
			if word, ok := one.(string); ok && word != "null" {
				kinds = append(kinds, word)
			}
		}
		return strings.Join(kinds, " or ")
	}
	return "any"
}
