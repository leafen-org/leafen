package app

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A tool's answer text is its output as JSON. The SDK writes it after
// reading the output back through maps for its schema check, so every
// object's keys come out sorted and <, > and & escaped, where Notion's
// answers keep their keys in their own order and their text as it is (seen
// 2026-11-14: fetch's metadata, title, url, text…; search's results id,
// title, url, type…; view-version's type, url, configuration, rows,
// total_rows; "<database url=…"). Leafen's output types list their fields in
// Notion's order.

// mcpAnswersInOrder writes the answer text the SDK wrote again, from its
// structured content: each object's keys in the order its output type lists
// them, those its type doesn't name (a map's) after them sorted, and
// nothing escaped. An answer a tool wrote itself (mcpAnswerInOrder, a
// refusal) is left as it is.
func mcpAnswersInOrder() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, request)
			call, isCall := request.(*mcp.CallToolRequest)
			answer, isAnswer := result.(*mcp.CallToolResult)
			if err != nil || method != "tools/call" || !isCall || call.Params == nil || !isAnswer || answer == nil ||
				len(answer.Content) != 1 {
				return result, err
			}
			structured, ok := answer.StructuredContent.(json.RawMessage)
			text, isText := answer.Content[0].(*mcp.TextContent)
			if !ok || !isText || text.Text != string(structured) {
				return result, err
			}
			if written, ok := mcpAnswerWrittenInOrder(structured, mcpToolOutputSchema(ctx, next, call)); ok {
				answer.Content = []mcp.Content{&mcp.TextContent{Text: written, Meta: text.Meta, Annotations: text.Annotations}}
			}
			return answer, nil
		}
	}
}

// mcpToolOutputSchema is the output schema a call's tool was registered
// with, the order of its type's fields kept; nil when there is none.
func mcpToolOutputSchema(ctx context.Context, next mcp.MethodHandler, call *mcp.CallToolRequest) *jsonschema.Schema {
	if call.Session == nil {
		return nil
	}
	params := &mcp.ListToolsParams{}
	for range 100 {
		listed, err := next(ctx, "tools/list", &mcp.ListToolsRequest{Session: call.Session, Params: params})
		tools, ok := listed.(*mcp.ListToolsResult)
		if err != nil || !ok || tools == nil {
			return nil
		}
		for _, tool := range tools.Tools {
			if tool.Name == call.Params.Name {
				schema, _ := tool.OutputSchema.(*jsonschema.Schema)
				return schema
			}
		}
		if tools.NextCursor == "" {
			return nil
		}
		params = &mcp.ListToolsParams{Cursor: tools.NextCursor}
	}
	return nil
}

// mcpAnswerWrittenInOrder writes a JSON answer again in its schema's order.
func mcpAnswerWrittenInOrder(data json.RawMessage, schema *jsonschema.Schema) (string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return "", false
	}
	text, err := mcpFetchJSON(mcpValueInSchemaOrder(value, schema))
	return text, err == nil
}

// mcpValueInSchemaOrder orders a JSON value's objects as its schema lists
// their properties; keys the schema doesn't list follow, sorted.
func mcpValueInSchemaOrder(value any, schema *jsonschema.Schema) any {
	switch typed := value.(type) {
	case map[string]any:
		var order []string
		var properties map[string]*jsonschema.Schema
		var others *jsonschema.Schema
		if schema != nil {
			order, properties, others = schema.PropertyOrder, schema.Properties, schema.AdditionalProperties
		}
		ordered := make(mcpOrderedObject, 0, len(typed))
		placed := make(map[string]bool, len(typed))
		for _, key := range append(slices.Clone(order), slices.Sorted(maps.Keys(typed))...) {
			item, found := typed[key]
			if !found || placed[key] {
				continue
			}
			placed[key] = true
			property, named := properties[key]
			if !named {
				property = others
			}
			ordered = append(ordered, mcpOrderedEntry{Key: key, Value: mcpValueInSchemaOrder(item, property)})
		}
		return ordered
	case []any:
		var items *jsonschema.Schema
		if schema != nil {
			items = schema.Items
		}
		for index, item := range typed {
			typed[index] = mcpValueInSchemaOrder(item, items)
		}
		return typed
	}
	return value
}

// mcpAnswerInOrder is a tool's answer written from its output itself: its
// keys in the order its types list them, an ordered object's
// (mcpOrderedObject) as it holds them, and nothing escaped — for an answer
// whose parts a schema doesn't order (a list of results of more than one
// kind, a row's values). A tool returns it with its output, which stays the
// answer's structured content.
func mcpAnswerInOrder(output any) (*mcp.CallToolResult, error) {
	text, err := mcpFetchJSON(output)
	if err != nil {
		return nil, failedBecause(errMCPToolFailed, err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil
}
