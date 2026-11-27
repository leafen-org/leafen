package app

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// What Leafen says of a call beside what the call answers — the workspaces
// it worked in (mcp_answer_workspaces.go), that the connection's key ends
// soon (mcp_token_expiry_notice.go) — goes inside the answer, in a field of
// its own, never after it (2026-11-26: an agent tidying some 200 rows read
// each answer's JSON with a workspace's name on a line after it, and could
// not read the result as it came). Notion's MCP answers a call with one
// text, its result as JSON, and nothing after it (mcp_answer_rule.go, read
// from Notion's answers on 2026-11-21), and so does Leafen's:
//
//   - an answer whose text is one JSON object takes the note as its last
//     field, in its text and its structured content alike, the fields
//     before it as they were;
//   - any other answer — a refusal's words, a text a tool wrote — takes it
//     as its first part, before the answer, which stays whole.
//
// The notes' fields are named so that no tool's own field is one of them,
// and are there only when there is something to say: mcpAnswerNoteFields
// says each and why, as mcpNotionAnswerFields' Leafen fields say theirs.
var mcpAnswerNoteFields = map[string]string{
	mcpAnswerWorkedInField: "the workspaces the call worked in, by id and name, on a connection reaching several",
	mcpAnswerKeyEndsField:  "that the connection's key ends soon and what its person does before then",
}

const (
	mcpAnswerWorkedInField = "workspaces_worked_in"
	mcpAnswerKeyEndsField  = "connection_notice"
)

// mcpAnswerNote puts a note in an answer as its field, or as its first part
// where its text isn't one JSON object; said is the note in words, for that
// part.
func mcpAnswerNote(answer *mcp.CallToolResult, field string, value any, said string) {
	if answer == nil {
		return
	}
	if len(answer.Content) == 1 {
		if text, isText := answer.Content[0].(*mcp.TextContent); isText {
			if written, ok := mcpJSONWithField(text.Text, field, value); ok {
				answer.Content = []mcp.Content{&mcp.TextContent{Text: written, Meta: text.Meta, Annotations: text.Annotations}}
				answer.StructuredContent = mcpStructuredWithField(answer.StructuredContent, field, value)
				return
			}
		}
	}
	answer.Content = append([]mcp.Content{&mcp.TextContent{Text: said}}, answer.Content...)
}

// mcpJSONWithField is a JSON object's text with field as its last; false
// for a text that isn't one JSON object, or one that has the field already.
func mcpJSONWithField(text, field string, value any) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") || !json.Valid([]byte(trimmed)) {
		return "", false
	}
	held := map[string]json.RawMessage{}
	if json.Unmarshal([]byte(trimmed), &held) != nil {
		return "", false
	}
	if _, taken := held[field]; taken {
		return "", false
	}
	name, err := mcpFetchJSON(field)
	if err != nil {
		return "", false
	}
	written, err := mcpFetchJSON(value)
	if err != nil {
		return "", false
	}
	body := strings.TrimSpace(strings.TrimSuffix(trimmed, "}"))
	if len(held) > 0 {
		body += ","
	}
	return body + name + ":" + written + "}", true
}

// mcpStructuredWithField is an answer's structured content with field as
// its last, as its text has it; content that isn't a JSON object is kept.
func mcpStructuredWithField(structured any, field string, value any) any {
	if structured == nil {
		return nil
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(structured) != nil {
		return structured
	}
	written, ok := mcpJSONWithField(buffer.String(), field, value)
	if !ok {
		return structured
	}
	return json.RawMessage(written)
}
