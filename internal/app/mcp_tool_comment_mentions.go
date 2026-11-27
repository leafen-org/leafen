package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A comment's mention of a page, a database or a block reads as Notion's
// MCP writes it (seen 2026-11-21): notion-get-comments by its address
// alone, a database's without its type and a block's at the address of the
// block's ID alone, as https://app.notion.com/p/<block> — here the block's
// ID after this server's /p/, without its dashes as Notion's (seen
// 2026-11-22), which the MCP's Markdown reads back as the block's mention
// (mcpInternalPageID); fetch's discussions as a page's text
// names it, a page's with its title, a database's with its type and name.

// newMCPCommentReader is the reader of comments notion-get-comments lists
// (listed) or fetch's discussions show.
func newMCPCommentReader(app core.App, authority mcpAuthority, workspaceID string, listed bool) *mcpCommentReader {
	mentions := mcpPropertyMentions(app, authority, workspaceID)
	if listed {
		mentions.PageTitle, mentions.DatabaseTitle = nil, nil
	}
	return &mcpCommentReader{
		app: app, authority: authority, workspaceID: workspaceID, listed: listed,
		mentions: mentions, authors: map[string]mcpCommentAuthor{},
	}
}

// mcpCommentMarkdown writes a comment's text as inline "v2" page Markdown,
// as Notion's MCP writes a comment's; its words alone where a mention can't
// be written. A page or database a mention names by the public API's ID is
// named by the page's or database's own, as a page's text names it; listed,
// each mention by its address alone (mcpCommentMentionsByAddress).
func mcpCommentMarkdown(app core.App, rich []mcpCommentSpan, mentions notionMarkdownMentions, listed bool) string {
	spans := make([]graphTextSpan, 0, len(rich))
	plain := strings.Builder{}
	for _, span := range rich {
		plain.WriteString(span.Text)
		if span.Mention == "page" && span.PageID != "" {
			span.PageID = mcpLocalID(app, span.PageID)
		}
		// A data source's kept by its address stays so
		// (notionMarkdownKeptDataSourceTag).
		if _, kept := notionKeptDataSourceID(graphTextSpan{Mention: span.Mention, DatabaseID: span.DatabaseID}); !kept &&
			span.Mention == "database" && span.DatabaseID != "" {
			span.DatabaseID = mcpLocalID(app, span.DatabaseID)
		}
		spans = append(spans, graphTextSpan{
			Text: span.Text, Bold: span.Bold, Italic: span.Italic, Underline: span.Underline,
			Strikethrough: span.Strikethrough, Code: span.Code, Color: span.Color, Link: span.Link,
			Mention: span.Mention, PageID: span.PageID, BlockID: span.BlockID, DatabaseID: span.DatabaseID,
			CustomEmojiID: span.CustomEmojiID, Date: span.Date, DateEnd: span.DateEnd,
			DateTimeZone: span.DateTimeZone, UserID: span.UserID, Equation: span.Equation,
		})
	}
	if listed {
		spans, mentions = mcpCommentMentionsByAddress(spans, mentions)
	}
	text, written := notionMarkdownText(spans, mentions)
	if !written {
		text = notionMarkdownEscapedTextWith(plain.String(), notionMarkdownVersion2Escaped)
	}
	// A line break is <br />, its spaces and a line's leading marks as they
	// are (seen 2026-11-11).
	return strings.ReplaceAll(strings.ReplaceAll(text, notionMarkdownCodeBreak, "\n"), "\n", "<br />")
}

// mcpCommentMentionsByAddress makes each mention of a database and of a block
// on a page the reader opens a page's mention at its address, which a
// mention without a title writes alone: a database's at its address, or the
// one the reader can't open reads by; a block's at its own ID's address
// (notionMentionURL).
func mcpCommentMentionsByAddress(spans []graphTextSpan, mentions notionMarkdownMentions) ([]graphTextSpan, notionMarkdownMentions) {
	addresses := map[string]string{}
	for index := range spans {
		span := &spans[index]
		address, ok := "", false
		_, kept := notionKeptDataSourceID(*span)
		switch {
		case span.Mention == "database" && span.DatabaseID != "" && !kept:
			if address, ok = mentions.Database(span.DatabaseID); !ok && mentions.Unnamed != nil {
				address, ok = mentions.Unnamed(span.Mention, span.DatabaseID)
			}
		case span.Mention == "page" && span.BlockID != "":
			if _, open := mentions.Page(span.PageID); open {
				address, ok = notionMentionURL(mentions.Origin, span.BlockID), true
			}
		}
		if !ok {
			continue
		}
		// A key no page's ID is names the address.
		key := "address " + address
		addresses[key] = address
		span.Mention, span.PageID, span.BlockID, span.DatabaseID = "page", key, "", ""
	}
	if len(addresses) == 0 {
		return spans, mentions
	}
	page := mentions.Page
	mentions.Page = func(pageID string) (string, bool) {
		if address, named := addresses[pageID]; named {
			return address, true
		}
		return page(pageID)
	}
	return spans, mentions
}
