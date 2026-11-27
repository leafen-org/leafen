package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// A page command takes what it writes from the argument it names, and a call
// that leaves that argument out is refused, never read as empty. Notion's MCP
// (seen 2026-11-22, notion-update-page on a page of its own) refuses each
// command without its argument in these words — replace_content without
// new_str: `The "replace_content" command requires a "new_str" parameter.`;
// insert_content without content, update_content without content_updates (or
// with none in it), apply_template without template_id and
// update_verification without verification_status alike — and insert_content
// with content "" as `The "insert_content" command's "content" is empty.
// Provide the Markdown to insert.` A new_str given as "" empties the page, as
// asked, and an argument meant for another command is let be. Leafen read a
// missing new_str as "" and emptied the page without a word (2026-11-22: an
// agent that sent content in its place, with allow_deleting_content, lost
// the page's body).
var mcpPageCommandArguments = map[string]string{
	"replace_content": "new_str", "insert_content": "content", "update_content": "content_updates",
	"apply_template": "template_id", "update_verification": "verification_status",
}

// update_properties needs a change to one of its arguments, properties: {}
// among them — a change of nothing, which Notion's takes and answers (seen
// 2026-11-22 twice); with none it is refused in Notion's words, naming
// Leafen's small_text and font beside Notion's.
var mcpPagePropertyChanges = []string{"properties", "full_width", "small_text", "font", "icon", "cover", "is_skill"}

// mcpPageCommandRefusal is why a page command can't run on the arguments a
// call gave, in Notion's words; nil when it has the one it needs.
func mcpPageCommandRefusal(arguments json.RawMessage, command string) error {
	given := map[string]json.RawMessage{}
	_ = json.Unmarshal(arguments, &given)
	command = strings.TrimSpace(command)
	if command == "update_properties" {
		for _, name := range mcpPagePropertyChanges {
			if raw := bytes.TrimSpace(given[name]); len(raw) > 0 && string(raw) != "null" {
				return nil
			}
		}
		return mcpInvalid(`The "update_properties" command requires a change to "properties", "full_width", "small_text", "font", "icon", "cover", or "is_skill".`)
	}
	name, needs := mcpPageCommandArguments[command]
	if !needs {
		return nil
	}
	raw := bytes.TrimSpace(given[name])
	updates := []json.RawMessage{}
	if (len(raw) == 0 || string(raw) == "null") && mcpPageBodyUploadGiven(given, name) {
		// The body comes from an uploaded file (mcp_page_body_upload.go).
		return nil
	}
	if len(raw) == 0 || string(raw) == "null" || name == "content_updates" && json.Unmarshal(raw, &updates) == nil && len(updates) == 0 {
		return mcpInvalid("The %q command requires a %q parameter.", command, name)
	}
	if text := ""; name == "content" && json.Unmarshal(raw, &text) == nil && text == "" {
		return mcpInvalid(`The "insert_content" command's "content" is empty. Provide the Markdown to insert.`)
	}
	return nil
}

// mcpCallArguments are the arguments a tool call gave, as sent.
func mcpCallArguments(request *mcp.CallToolRequest) json.RawMessage {
	if request == nil || request.Params == nil {
		return nil
	}
	return request.Params.Arguments
}

// updateNotionMCPPage applies notion-update-page as Notion's MCP takes it,
// the call's arguments as it gave them.
func updateNotionMCPPage(ctx context.Context, app core.App, input notionMCPUpdatePageInput, arguments json.RawMessage) (notionMCPPageOutput, error) {
	if input.IsSkill != nil && *input.IsSkill {
		return notionMCPPageOutput{}, mcpSkillPageUnsupported()
	}
	if err := mcpPageCommandRefusal(arguments, input.Command); err != nil {
		return notionMCPPageOutput{}, err
	}
	if strings.TrimSpace(input.Command) == "insert_content" && input.Position != nil && notionMarkdownAnchored(input.Position.Type) {
		return insertMCPPageAnchored(ctx, app, input)
	}
	// is_skill false alone changes nothing, as properties {} doesn't.
	if strings.TrimSpace(input.Command) == "update_properties" && input.IsSkill != nil && input.Properties == nil {
		input.Properties = map[string]any{}
	}
	position := ""
	if input.Position != nil {
		position = input.Position.Type
	}
	newString, err := mcpPageBody(ctx, app, "new_str", input.NewString, mcpArgumentGiven(arguments, "new_str"), input.NewStrFileUploadID)
	if err != nil {
		return notionMCPPageOutput{}, err
	}
	_, output, err := updateMCPPage(ctx, app, mcpUpdatePageInput{
		PageID: mcpLocalID(app, input.PageID), Command: input.Command, NewString: newString,
		Content: input.Content, Position: position, ContentUpdates: input.ContentUpdates, Properties: input.Properties,
		Verification: mcpPageVerification{
			Status: input.VerificationStatus, ExpiryDays: input.VerificationExpiryDays, ExpiresOn: strings.TrimSpace(input.VerificationExpiresOn),
		},
		AllowDeletingContent: input.AllowDeletingContent, Icon: input.Icon, Cover: input.Cover,
		TemplateID: input.TemplateID, FullWidth: input.FullWidth, SmallText: input.SmallText, Font: input.Font,
	})
	if err != nil {
		return notionMCPPageOutput{}, err
	}
	return notionPageOutputFromMutation(output), nil
}
