package app

import (
	"net/url"
	"strings"
)

// notionMarkdownPageLinkTarget is the app's link that a path in a page
// Markdown write names, or "" for a path that names no page: Notion's
// address of a page in its app (/ID, /Title-ID, /p/ID, /name/ID), kept at
// the app's address as Notion's write keeps it (notionMarkdownAppAddress),
// or the app's own (/p/ID, or for the MCP, /pages/ID, as it names a page)
// by an ID a page could have (notionMarkdownOwnPageRef), kept as the app's
// link to that page. The app's own anchor (#block-ID) names its block there
// too, not the page itself (appAnchorBlockID); any other is left off, as
// Notion's write leaves it (seen 2026-11-11). A path in the app's form with
// no page's ID in it ("/p/abc#x", "/pages/abc"), and /pages/ID in the public
// API's Markdown, which names no page of Notion's app (seen 2026-11-11, "v1"
// and "v2": "[a](/pages/ID)" reads https://app.notion.com/pages/ID), is the
// path as written, as Notion's write keeps a path of its app that names no
// page. The app's own address of a page here, leafen://page/ID, is its path
// in the app (/p/ID), its anchor kept, as the app's paste reads it
// (markdownPasteSyntax.ts) and Notion's Markdown reads its app's own address
// of a page (seen 2026-11-23, "v2": "[a](notion://www.notion.so/ID#block)"
// links https://app.notion.com/p/ID#block).
func notionMarkdownPageLinkTarget(raw string, mcp bool) string {
	address := strings.TrimSpace(raw)
	if rest, own := leafenPageSchemeRest(address); own {
		address = "/p/" + rest
	}
	parsed, err := url.Parse(address)
	if err != nil || !strings.HasPrefix(address, "/") || strings.HasPrefix(address, "//") ||
		!mcp && strings.HasPrefix(address, mcpPageURLPrefix) {
		return ""
	}
	if kept, named := notionMarkdownAppAddress(address); named {
		return kept
	}
	own := notionMarkdownPageLinkID(address)
	if own == "" || !notionMarkdownOwnPageRef(own) {
		return ""
	}
	page := "/p/" + own
	if pageID, _, named := notionAddressPageIDs("/"+own, "", false); named {
		page = notionAppPageLink(pageID, "")
	}
	if notionAddressBlockPattern.MatchString(parsed.Fragment) && !sameAddressID(parsed.Fragment, own) {
		return page + "#block-" + notionLinkedID(parsed.Fragment)
	}
	if block := appAnchorBlockID(parsed.Fragment, own); block != "" {
		return page + "#block-" + block
	}
	return page
}

// leafenPageSchemeRest is what follows leafen://page/, in any case, in the
// app's own address of a page (the page's ID and any anchor); false for any
// other address.
func leafenPageSchemeRest(address string) (string, bool) {
	const scheme = "leafen://page/"
	if len(address) <= len(scheme) || !strings.EqualFold(address[:len(scheme)], scheme) {
		return "", false
	}
	return address[len(scheme):], true
}
