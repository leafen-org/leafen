package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"unicode/utf8"
)

// mcpPageMarkdownLimit is the most Markdown, in characters, one MCP page
// update may send.
const mcpPageMarkdownLimit = 2_000_000

// notionPageGrowingRecordRule sends a record that keeps growing to the
// page's body, in notion-update-page's description (the user, 2026-11-26: an
// agent added lines to a note property of over 6,000 characters ten times,
// reading and writing it whole each time, and a person editing it at once
// would have lost theirs): a property is only ever replaced whole, as in
// Notion, while the body takes an addition at its end.
const notionPageGrowingRecordRule = "A record that keeps growing (a log, notes added to again and again) belongs in the page's body, not a property: " +
	"a property is replaced whole each time, while insert_content without a position adds to the body's end."

// notionMCPInsertPosition is where insert_content puts its content, as
// Notion's MCP takes it: at the page's start or end, or by an anchor
// (notion_page_markdown_anchor.go).
type notionMCPInsertPosition struct {
	Type   string `json:"type" jsonschema:"start or end to put the content at the top or bottom of the page; after_content or before_content to put it after the block where anchor ends or before the block where it starts"`
	Anchor string `json:"anchor,omitempty" jsonschema:"For after_content and before_content: text copied exactly from the page, starting at the beginning of a block with its leading syntax (such as ## or - ) and keeping its inline formatting and escapes; enough of it to match one place only. It places the content at that block's nesting level, so anchoring after_content on a parent block puts it after the blocks the parent holds"`
}

// mcpMarkdownWarning is something a page write left out or changed, as
// Notion's MCP says it (seen 2026-11-11): an update_content replacement whose
// old_str the page didn't hold is no_match, and what reading the Markdown as
// the public API's "v2" changed or kept as text (notionMarkdownVersion2Input)
// is markdown_parser_warning, the API's code its category, with whether the
// reading put it right; notion-create-pages names the page by its index.
type mcpMarkdownWarning struct {
	Index    *int   `json:"index,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Category string `json:"category,omitempty"`
	Autofix  *bool  `json:"autofix,omitempty"`
}

// mcpMarkdownWarnings are a write's warnings as Notion's MCP says them: what
// update_content left out, then what reading its Markdown said.
func mcpMarkdownWarnings(matched, parsed []notionMarkdownWarning) []mcpMarkdownWarning {
	warnings := make([]mcpMarkdownWarning, 0, len(matched)+len(parsed))
	for _, warning := range matched {
		warnings = append(warnings, mcpMarkdownWarning{Code: warning.Code, Message: warning.Message})
	}
	for _, warning := range parsed {
		// A tag read as its "v2" one, HTML put right, or an embed made the
		// editor's block was fixed; a tag kept as text or an attribute left
		// out was not.
		fixed := warning.Code == "deprecated_tag" || warning.Code == "html_tag_autofix" || warning.Code == "embed_converted"
		warnings = append(warnings, mcpMarkdownWarning{
			Code: "markdown_parser_warning", Message: warning.Message, Category: warning.Code, Autofix: &fixed,
		})
	}
	if len(warnings) == 0 {
		return nil
	}
	return warnings
}

// mcpPageMarkdownChange is the Markdown request an MCP page command makes, as
// the Public API takes it in "v2", as Notion's MCP reads its Markdown (seen
// 2026-11-11: the same blocks and warnings as its API's "v2"), with the hash
// that tells a replay of it from another request. Its replace_content and
// insert_content read as the API's "v2" update does
// (notionMarkdownVersion2CodeLinesInput: backticks across lines, addresses
// it leaves unlinked); the hash is the request's as sent. It is false for a
// command that changes no Markdown and for input that doesn't fit its command.
func mcpPageMarkdownChange(pageID string, input mcpUpdatePageInput) (notionPageMarkdownUpdateRequest, string, bool) {
	change, hash, ok := mcpPageMarkdownCommand(pageID, input)
	change.MarkdownVersion = notionMarkdownVersion2
	return notionMarkdownVersion2CodeLinesInput(change), hash, ok
}

// mcpPageMarkdownCommand is the request a command makes and its hash, the one
// its earlier receipts were written with.
func mcpPageMarkdownCommand(pageID string, input mcpUpdatePageInput) (notionPageMarkdownUpdateRequest, string, bool) {
	change := notionPageMarkdownUpdateRequest{Type: input.Command}
	size := utf8.RuneCountInString(input.NewString) + utf8.RuneCountInString(input.Content)
	switch input.Command {
	case "replace_content":
		change.ReplaceContent = &notionMarkdownReplaceContentRequest{
			NewString: input.NewString, AllowDeletingContent: input.AllowDeletingContent,
		}
	case "insert_content":
		if input.Content == "" {
			return change, "", false
		}
		change.InsertContent = &notionMarkdownInsertContentRequest{Content: input.Content}
		if input.Position != "" {
			change.InsertContent.Position = &notionMarkdownInsertPosition{Type: input.Position}
		}
	case "update_content":
		for _, update := range input.ContentUpdates {
			size += utf8.RuneCountInString(update.OldString) + utf8.RuneCountInString(update.NewString)
		}
		change.UpdateContent = &notionMarkdownUpdateContentRequest{
			ContentUpdates: input.ContentUpdates, AllowDeletingContent: input.AllowDeletingContent,
		}
	default:
		return change, "", false
	}
	if size > mcpPageMarkdownLimit {
		return change, "", false
	}
	if input.Command == "replace_content" {
		// A replacement keeps the hash its earlier receipts were written with.
		digest := sha256.Sum256([]byte("leafen:mcp-page-replace-content:v1\x00" + pageID + "\x00" + input.NewString))
		return change, hex.EncodeToString(digest[:]), true
	}
	encoded, err := json.Marshal(change)
	if err != nil {
		return change, "", false
	}
	digest := sha256.Sum256(append([]byte("leafen:mcp-page-markdown:v1\x00"+pageID+"\x00"), encoded...))
	return change, hex.EncodeToString(digest[:]), true
}
