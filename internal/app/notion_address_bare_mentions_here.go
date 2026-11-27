package app

import (
	"cmp"
	"strings"
)

// One of Notion's web addresses naming a page, a database or a block by its
// ID (notionAddressNamedIDs) standing bare in Markdown a connection writes is
// read as the mention of what that ID names, as Notion reads the address of
// one of its own pages standing bare (seen 2026-11-21, public API "v1" and
// "v2" alike: app.notion.com/p/ID, app.notion.com/ID, notion.so and
// www.notion.so with or without the scheme, the page's title before its ID,
// a query, a notion.site of any workspace, a dashed ID, a database's ID its
// database's mention, a block's ID or one after # the block's mention on its
// page, bold around it, and <address> in "v2", whose brackets go; "v1" keeps
// the brackets as text around the mention; [address](address) and
// [words](address) stay links). The writer's boundary names what the ID names
// here, or what a Notion move brought from it (notion_address_mentions_here.go);
// for one naming nothing, it is the link to the address it was before, its
// words as written (Notion keeps an "Untitled" mention).
//
// The MCP tools read every one of those forms so too, as Notion's MCP now
// does in notion-create-pages, with or without preserve_internal_links, and
// in notion-update-page (seen 2026-11-21: app.notion.com, notion.so with and
// without www or the scheme, a notion.site, the title before the ID and
// <address> alike; on 2026-11-11 it kept a notion.so address a link), and
// notion-create-pages makes a link to one the same mention unless asked to
// keep links (mcp_page_internal_links.go).

// notionAddressMention is the mention one of Notion's web addresses standing
// bare makes, marked as the text it stands in (style), its words that text's
// (or the address for none): of the block its anchor names, or else of the
// page, database or block its ID names, keeping the address for the writer
// as a mention of a Notion address does (notionMarkdownIDMention). ok is
// false for any other address, which stays a link.
func (site *notionMarkdownSitePages) notionAddressMention(address string, style graphTextSpan) (graphTextSpan, bool) {
	web := notionMarkdownNotionWebAddress(address)
	if site == nil || web == "" {
		return graphTextSpan{}, false
	}
	_, id, anchor := notionAddressNamedIDs(web)
	if id == "" {
		return graphTextSpan{}, false
	}
	return graphTextSpan{
		Text: cmp.Or(style.Text, strings.TrimSpace(address)), Mention: "page", PageID: cmp.Or(anchor, id), Link: web,
		Bold: style.Bold, Italic: style.Italic, Strikethrough: style.Strikethrough, Underline: style.Underline,
		Color: style.Color, Background: style.Background,
	}, true
}
