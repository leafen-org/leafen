package app

import "github.com/pocketbase/pocketbase/core"

// notionMarkdownWrittenMentions checks the mentions of Markdown someone
// writes through the MCP tools, then names what they name here
// (narrowedMarkdownMentionInput). A piece its reading refuses — an
// <unknown_mention> of no custom emoji's (notionMarkdownUnknownMention) —
// refuses the write in its words, in a page's body, its title and a
// property's value alike, as Notion's MCP refuses one (seen 2026-11-22:
// notion-create-pages' content, a page's title and a text property's value,
// and insert_content, "Unsupported unknown mention with x", its alt given
// before its address); its :name: then become the workspace's custom emoji,
// its addresses of Notion's pages the pages here, and its people theirs —
// those the page holds as held (notion_mention_held.go).
func notionMarkdownWrittenMentions(app core.App, current pageAuthorization, updates []graphTextUpdate, operations []graphOperation) error {
	workspaceID := current.Workspace.WorkspaceID
	for _, update := range updates {
		for _, span := range update.Spans {
			if span.Mention == notionMarkdownRefusedMention {
				return graphInputError(errorCodeGraphInvalid, span.Text)
			}
		}
	}
	if err := resolveNotionMarkdownCustomEmojis(app, workspaceID, updates); err != nil {
		return err
	}
	if err := resolveNotionMarkdownWorkspaceTargets(app, workspaceID, updates, operations); err != nil {
		return err
	}
	var held *notionHeldMentions
	if current.Page != nil {
		held = newNotionHeldMentions(app, current.Workspace.OwnerID, current.Page.Id)
	}
	return resolveMarkdownPersonMentions(app, workspaceID, held, updates)
}
