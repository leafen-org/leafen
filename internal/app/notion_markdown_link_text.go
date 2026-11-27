package app

import (
	"net/url"
	"strings"
)

// notionMarkdownLinkTextEnd is where the text of a link opening with the [ at
// value[open] ends, at its "](", as Notion's "v2" and MCP Markdown read it
// (seen 2026-11-10): code, an equation, and an inline tag with what it holds
// up to its closing tag, stand whole, so a "](" inside them is theirs. "[a <b>x](…)
// </b>" is no link, its <b> bold over "x](…) ", and "[a <b>x](…) [y</b>
// z](…)" one link, to the second address. Brackets in its text pair, and a
// link in it leaves it none, as CommonMark's: in "[a <b>x</b> [c](…)" only
// "[c](…)" is one. It is -1 when no "](" ends it.
func notionMarkdownLinkTextEnd(value string, open int) int {
	depth := 0
	for index := open + 1; index < len(value); index++ {
		switch {
		case value[index] == '\\':
			index++
		case value[index] == '`':
			_, end, _ := notionMarkdownCodeSpanAt(value, index)
			index = end - 1
		case value[index] == '$':
			if _, end, ok := notionMarkdownVersion2Equation(value, index); ok {
				index = end - 1
			}
		case value[index] == '[':
			depth++
		case strings.HasPrefix(value[index:], "](") && depth > 0:
			return -1
		case value[index] == ']' && depth > 0:
			depth--
		case strings.HasPrefix(value[index:], "]("):
			return index
		case value[index] == '<':
			if closed := notionMarkdownInlineTagEnd(value, index); closed > index {
				index = closed - 1
			}
		}
	}
	return -1
}

// notionMarkdownInlineTagEnd is where an inline tag opening at value[index:]
// closes, past what it holds and its closing tag; 0 for none.
func notionMarkdownInlineTagEnd(value string, index int) int {
	if _, _, _, _, closed, ok := notionMarkdownStyleTag(value, index, workspaceImportInlineStyle{}); ok {
		return closed
	}
	if _, _, _, closed, ok := notionMarkdownSpan(value, index, workspaceImportInlineStyle{}); ok {
		return closed
	}
	if _, end, ok := notionMarkdownCurrentTag(value, index, nil); ok {
		return end
	}
	if _, end, ok := notionMarkdownMention(value, index, false, nil); ok {
		return end
	}
	if _, _, _, closed, ok := notionMarkdownAnchor(value, index); ok {
		return closed
	}
	return 0
}

// notionMarkdownMentionLink is the link a mention of a web address that is
// no page's stands for, as Notion reads one (seen 2026-11-10: <mention
// url="https://example.com/p">x</mention> is "x" linked there, and one
// without text the address linked): its text, its edge spaces left out, or
// the address. A page's address is Notion's own, or a Leafen page's /p/ID
// or /pages/ID, which name the page by the ID it ends in — at the writer's
// own server (site) by any ID the MCP tools name one by
// (notionMarkdownSitePages.ownsAddress).
func notionMarkdownMentionLink(address, text string, site *notionMarkdownSitePages) (graphTextSpan, bool) {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") ||
		parsed.Host == "" || notionMarkdownLeafenPageAddress(parsed.Path) || site.ownsAddress(address) {
		return graphTextSpan{}, false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, notion := range []string{"notion.so", "notion.com", "notion.site"} {
		if host == notion || strings.HasSuffix(host, "."+notion) {
			return graphTextSpan{}, false
		}
	}
	target := workspaceImportLinkTarget(address)
	if target == "" {
		return graphTextSpan{}, false
	}
	if text = strings.TrimSpace(text); text == "" {
		text = target
	}
	return graphTextSpan{Text: text, Link: target}, true
}

// notionMarkdownLeafenPageAddress reports whether an address's path names a
// Leafen page as its Markdown writes one: /p/ID or /pages/ID by the page's
// public ID or its own. Another site's address of that form reads as one
// too, as the server's own address changes with how it is reached; one
// by an ID of another form, which a site's own paths may have too, only at
// the writer's server (notionMarkdownSitePages.ownsAddress).
func notionMarkdownLeafenPageAddress(path string) bool {
	id := notionMarkdownPageLinkID(path)
	_, public := normalizeNotionUUIDRequest(id)
	return public || leafenOwnID.MatchString(id)
}
