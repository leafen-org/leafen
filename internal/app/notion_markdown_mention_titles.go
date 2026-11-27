package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// mentionPageTitle is the title a page's mention reads with: a row page's
// title cell, what it mentions by name now, any other page's title, a
// custom emoji no longer there left out; "" for a page not found. One
// without a title reads empty, but for a reader naming it (NamesUntitled)
// what an agent's answers call it (mcp_untitled_names.go), as "<mention
// url=…></mention>" said nothing of what it was (2026-11-26, an agent's
// real use); the words a mention is written with are not read back, its
// address alone names it (notion_page_markdown_mentions.go).
func (reader notionMarkdownReader) mentionPageTitle(app core.App, workspaceID string, targets notionMarkdownMentionTargets, pageID string) string {
	stored, found := targets.page(app, pageID)
	if !found {
		return ""
	}
	title := stored.Title
	switch {
	case stored.DataSourceID != "":
		title = databaseRowPageTitleAsMentioned(app, reader.PrincipalID, pageID)
	// Only a title with a ":" may hold a custom emoji, which its record's
	// spans say; any other reads no record (2026-11-25).
	case strings.Contains(stored.Title, ":"):
		if record, err := app.FindRecordById(pagesCollection, pageID); err == nil {
			title = notionTitleWithoutMissingCustomEmoji(app, reader.PrincipalID, workspaceID, stored.Title, ordinaryPageTitleSpans(record))
		}
	}
	if title != "" || !reader.NamesUntitled {
		return title
	}
	record, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return title
	}
	return mcpUntitledPageName(app, reader.PrincipalID, record)
}
