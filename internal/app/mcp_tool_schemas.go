package app

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool schemas every model and client takes. A schema of "anything" may be
// written `true`, as the MCP library writes a Go `any`, but some engines'
// tool-call grammars — llama.cpp's, which servers a person runs (Ollama, LM
// Studio) use — take only objects there and refuse the whole request, and the
// official MCP SDK refuses a whole tool list with one in an output schema
// (2026-11-03 audit: Claude Code could use none of the tools). The tools are
// listed with such a schema written `{}`, the same "anything", in what they
// take and what they give.
//
// What a tool gives is listed open to fields its schema doesn't name, at its
// top and in every object inside it: the library writes each Go struct of an
// answer as an object that allows no other field (additionalProperties
// false), and an agent checks each answer against the list it read when it
// connected, which it keeps until it reads the list again. One field added to
// an answer then failed every call of that tool from an agent connected
// before the server was upgraded — "Structured content does not match the
// tool's output schema: data must NOT have additional properties" (2026-11-23,
// leafen_notion_migration_status's pages_link). What a tool takes is listed
// as it is: a field it doesn't take is still refused.
//
// So an answer only adds fields: a field it gives keeps its name, its meaning
// and its type, and is not dropped — one that has to change is added under a
// new name beside the old one, which stays (mcp_answer_rule.go). An agent
// holding the last version's list then reads the next version's answers.
//
// The schemas are listed with their keys in the order they were written in.

// mcpPlainToolSchemas lists the tools with their `true` schemas written `{}`,
// and what they give open to fields they don't name.
func mcpPlainToolSchemas() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, request)
			listed, ok := result.(*mcp.ListToolsResult)
			if err != nil || method != "tools/list" || !ok || listed == nil {
				return result, err
			}
			plain := *listed
			plain.Tools = make([]*mcp.Tool, len(listed.Tools))
			for index, tool := range listed.Tools {
				each := *tool
				each.InputSchema = plainToolSchema(tool.InputSchema, false)
				each.OutputSchema = plainToolSchema(tool.OutputSchema, true)
				plain.Tools[index] = &each
			}
			return &plain, nil
		}
	}
}

// plainToolSchema is a tool's schema as it is listed; output says it is what
// the tool gives.
func plainToolSchema(schema any, output bool) any {
	data, err := json.Marshal(schema)
	if err != nil || !bytes.Contains(data, []byte("true")) && !(output && bytes.Contains(data, []byte("false"))) {
		return schema
	}
	decoded, err := decodeSchemaInOrder(data)
	if err != nil {
		return schema
	}
	return plainSchema(decoded, output)
}

// The keywords whose value is a schema, a map of schemas or a list of them.
var (
	schemaValueKeywords = map[string]bool{"items": true, "additionalItems": true, "contains": true, "propertyNames": true, "not": true,
		"if": true, "then": true, "else": true, "unevaluatedItems": true, "unevaluatedProperties": true}
	schemaMapKeywords  = map[string]bool{"properties": true, "patternProperties": true, "$defs": true, "definitions": true, "dependentSchemas": true}
	schemaListKeywords = map[string]bool{"allOf": true, "anyOf": true, "oneOf": true, "prefixItems": true}
	// schemaClosingKeywords close an object to the fields it doesn't name.
	schemaClosingKeywords = []string{"additionalProperties", "unevaluatedProperties"}
)

// plainSchema writes a schema's `true` subschemas as `{}`, and in what a tool
// gives (output) leaves out the keywords that close its objects;
// additionalProperties keeps its `true`, which the engines take, and a map's
// schema of its values is read through as any other.
func plainSchema(value any, output bool) any {
	if value == true {
		return newSchemaObject()
	}
	schema, ok := value.(*schemaObject)
	if !ok {
		return value
	}
	if output {
		for _, key := range schemaClosingKeywords {
			if closingSchema(schema.values[key]) {
				schema.remove(key)
			}
		}
	}
	for _, key := range schema.keys {
		child := schema.values[key]
		switch {
		case schemaValueKeywords[key]:
			schema.values[key] = plainSchema(child, output)
		case key == "additionalProperties":
			if _, isSchema := child.(*schemaObject); isSchema {
				schema.values[key] = plainSchema(child, output)
			}
		case schemaMapKeywords[key]:
			if each, ok := child.(*schemaObject); ok {
				for _, name := range each.keys {
					each.values[name] = plainSchema(each.values[name], output)
				}
			}
		case schemaListKeywords[key]:
			if each, ok := child.([]any); ok {
				for index, sub := range each {
					each[index] = plainSchema(sub, output)
				}
			}
		}
	}
	return schema
}

// closingSchema is whether a schema takes no value: `false`, or
// `{"not": {}}`, as the library holds `false`.
func closingSchema(value any) bool {
	if value == false {
		return true
	}
	schema, ok := value.(*schemaObject)
	if !ok || len(schema.keys) != 1 {
		return false
	}
	not, ok := schema.values["not"]
	empty, isObject := not.(*schemaObject)
	return ok && (not == true || isObject && len(empty.keys) == 0)
}
