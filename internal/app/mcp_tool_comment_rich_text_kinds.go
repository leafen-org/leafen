package app

import "github.com/pocketbase/pocketbase/core"

// A rich_text item of notion-create-comment, and a mention in one, may leave
// out its "type", as Notion's MCP takes them (seen 2026-11-21: {"text":{…}}
// and {"mention":{"page":{…}}} kept as text and as a page's mention). Its
// kind is then the first its tool's input schema lists that it holds (read
// 2026-11-22): an item's text, mention or equation; a mention's user, date,
// page, database, template mention or custom emoji — one holding a page and
// a database is the page's (seen 2026-11-21).

// mcpCommentRichTextKind is the kind a rich_text item is of: its type, or
// without one the first kind it holds; "" for none.
func mcpCommentRichTextKind(item map[string]any) string {
	return mcpCommentKindHeld(item, "text", "mention", "equation")
}

// mcpCommentMentionKind is the kind a rich_text mention is of: its type, or
// without one the first kind it holds; "" for none.
func mcpCommentMentionKind(mention map[string]any) string {
	return mcpCommentKindHeld(mention, "user", "date", "page", "database", "template_mention", "custom_emoji")
}

func mcpCommentKindHeld(value map[string]any, kinds ...string) string {
	if kind, _ := value["type"].(string); kind != "" {
		return kind
	}
	for _, kind := range kinds {
		if _, held := value[kind]; held {
			return kind
		}
	}
	return ""
}

// mcpCommentDatabaseMention makes span the mention of the database
// databaseID names — by its own ID or the public API's — named now, as a
// comment's Markdown names one (principalDatabaseMention); false for an ID
// no database here answers to.
func mcpCommentDatabaseMention(
	app core.App, authority mcpAuthority, current pageAuthorization, databaseID string, span *mcpCommentSpan,
) (bool, error) {
	prepared, err := mcpPreparedPropertySpans(app, authority, current, mcpCommentLocalMentions(app,
		[]graphTextSpan{{Text: databaseID, Mention: "database", DatabaseID: databaseID}}))
	if err != nil || prepared[0].Mention != "database" {
		return false, err
	}
	span.Mention, span.DatabaseID, span.Text = "database", prepared[0].DatabaseID, prepared[0].Text
	return true, nil
}
