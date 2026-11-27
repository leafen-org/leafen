package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// The guide's answer leads, for a connection whose list leaves the move's
// tools out (mcp_notion_move_tools_left_out.go), with why and what to do
// now, then how to call each through leafen_call with what its input takes
// — its fields, the required ones marked, the shape of a list's or an
// object's, and in a few words what each field takes, from its own
// description — read from the tools themselves, so an agent calls each
// without first looking it up (2026-11-26, Codex runs of a whole move:
// without the fields it asked leafen_operations for four of the inputs;
// given them bare, it put the workspace's name in workspace_id, which every
// tool takes and the lead leaves to the tool). A connection that lists them
// reads the guide as it was.

// notionMigrationGuideCallsOnce holds the inputs' lines, the same for every server.
var notionMigrationGuideCallsOnce struct {
	once sync.Once
	text string
}

// notionMigrationGuideLead is the guide's lead for the connection, or "".
func notionMigrationGuideLead(ctx context.Context, app core.App, server *mcp.Server) string {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return ""
	}
	why := mcpNotionMoveToolsLeftOut(app, authority)
	if why == "" {
		return ""
	}
	notionMigrationGuideCallsOnce.once.Do(func() {
		notionMigrationGuideCallsOnce.text = notionMigrationGuideInputs(listRegisteredMCPTools(server))
	})
	now := "This connection's workspaces have had their Notion move, so your list leaves the move's tools out; run them through leafen_call. " +
		"To bring what changed in Notion since, run that move again (section 7), not a new one, which would bring it all a second time: " +
		`leafen_call {"tool": "leafen_notion_migration_status", "arguments": {}} names it, and leafen_notion_migration_run takes its migration_id and plan revision. ` +
		"Only to move more of Notion — pages that move didn't take — start one with leafen_notion_migration_sources, as this guide says."
	if why == mcpNotionMoveCannot {
		now = "This connection can't move Notion into Leafen — its page left only reading tools on, or holds it to chosen pages, or its person " +
			"can add pages in none of its workspaces — so your list leaves the move's tools out. A move needs a connection that can " +
			"add pages: ask your person for one. A move's state still reads through leafen_call " +
			`{"tool": "leafen_notion_migration_status", "arguments": {}}.`
	}
	return now + "\n\nThe move's tools, each run with leafen_call " + `{"tool": "<its name>", "arguments": {…}}` +
		", their arguments as below (* required; a list's items and an object's fields in brackets; workspace_id, as every tool takes, " +
		"left out for the migration's own):\n" + notionMigrationGuideCallsOnce.text + "\n\n"
}

func notionMigrationGuideInputs(tools []*mcp.Tool) string {
	lines := []string{}
	for _, tool := range tools {
		if !mcpNotionMoveTools[tool.Name] {
			continue
		}
		lines = append(lines, "- "+tool.Name+": "+mcpToolInputLine(tool))
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}

// mcpToolInputLine names a tool's input fields and their shapes.
func mcpToolInputLine(tool *mcp.Tool) string {
	var schema map[string]any
	if data, err := json.Marshal(tool.InputSchema); err != nil || json.Unmarshal(data, &schema) != nil {
		return "{}"
	}
	return mcpSchemaShape(schema, 0)
}

// mcpSchemaShape is a schema's shape on one line, two levels deep.
func mcpSchemaShape(schema map[string]any, depth int) string {
	types := mcpSchemaTypes(schema)
	switch {
	case slices.Contains(types, "object"):
		properties, _ := schema["properties"].(map[string]any)
		if len(properties) == 0 {
			return "{}"
		}
		if depth > 1 {
			return "{…}"
		}
		required := map[string]bool{}
		if list, ok := schema["required"].([]any); ok {
			for _, name := range list {
				if text, ok := name.(string); ok {
					required[text] = true
				}
			}
		}
		names := make([]string, 0, len(properties))
		for name := range properties {
			// Fields every tool takes are left to the tool: workspace_id
			// given a workspace's name was refused (2026-11-26, a Codex run).
			if name == "instructions_version" || name == "skill" || name == "workspace_id" {
				continue
			}
			names = append(names, name)
		}
		slices.Sort(names)
		fields := make([]string, 0, len(names))
		for _, name := range names {
			mark := ""
			if required[name] {
				mark = "*"
			}
			child, _ := properties[name].(map[string]any)
			field := fmt.Sprintf("%s%s: %s", name, mark, mcpSchemaShape(child, depth+1))
			// A tool's own fields say what each takes, in a word or a few.
			if note := mcpSchemaNote(child); depth == 0 && note != "" {
				field += " (" + note + ")"
			}
			fields = append(fields, field)
		}
		return "{" + strings.Join(fields, ", ") + "}"
	case slices.Contains(types, "array"):
		items, _ := schema["items"].(map[string]any)
		return "[" + mcpSchemaShape(items, depth) + "]"
	case len(types) > 0:
		if values, ok := schema["enum"].([]any); ok && len(values) > 0 && len(values) <= 6 {
			words := make([]string, 0, len(values))
			for _, value := range values {
				words = append(words, fmt.Sprint(value))
			}
			return strings.Join(words, "|")
		}
		return strings.Join(types, "|")
	}
	return "any"
}

// mcpSchemaNote is a field's description to its first pause, short.
func mcpSchemaNote(schema map[string]any) string {
	note, _ := schema["description"].(string)
	note = strings.TrimSpace(note)
	for _, pause := range []string{"; ", ": ", ". "} {
		if index := strings.Index(note, pause); index > 0 {
			note = note[:index]
		}
	}
	if runes := []rune(note); len(runes) > 90 {
		note = string(runes[:89]) + "…"
	}
	return strings.TrimSuffix(note, ".")
}

// mcpSchemaTypes are a schema's types, null aside.
func mcpSchemaTypes(schema map[string]any) []string {
	types := []string{}
	switch typed := schema["type"].(type) {
	case string:
		types = append(types, typed)
	case []any:
		for _, value := range typed {
			if text, ok := value.(string); ok && text != "null" {
				types = append(types, text)
			}
		}
	}
	return types
}
