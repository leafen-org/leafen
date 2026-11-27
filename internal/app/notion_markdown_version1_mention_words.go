package app

import (
	"net/url"
	"strings"
)

// A "v1" mention tag names a page or a database by its address — a path
// ending in its ID (/ID, /p/ID), or a web address — a data source by its
// collection:// address, and a person by their user:// address, whatever
// kind the tag names (notionMarkdownMention). Notion's "v1" reads one whose
// address is none of these as words (seen 2026-11-11): a page's or a
// database's, its words or, when it holds none, its address as written
// ("notaurl", "docs/readme.md", "#x", an ID alone with or without dashes,
// "x/ID" — even a page's that is there); a person's, its address as
// written, whatever words it holds ("relative.html", an ID alone,
// "USER://ID", " user://ID"), a page's, a database's or a data source's
// the app's address of it (version1AddressWords), and one of a user://
// address naming no one (notionMarkdownPersonAddress: "user://ID/x", an ID
// without its dashes or of another kind of UUID) the app's address,
// whatever words it holds. A page's, a database's or a data source's tag of
// a person's address is a link to the app's address, as a [words](user://…)
// link is, its words the app's address when it holds none, and of a
// user:// address naming no one the app's address unlinked; a data
// source's of any other address is a [words](address) link, its words what
// a person's tag would read when it holds none. One of a host with a colon
// after it ("example.com:8080/x") reads as nothing
// (notionMarkdownHostPortAddress). A page's or database's path in the app
// whose last part is no ID ("/p/abc") is a link to that path, its words the
// app's address of it when it holds none. An empty address names nothing
// either: a page's, a database's or a data source's tag reads as its words,
// or "Source" when it holds none; a person's as nothing; and one of spaces
// alone, of any kind, as its words or nothing (seen 2026-11-11).

// version1MentionWords is the words or link a "v1" mention tag opening at
// value[start:] reads as by that rule, with where the tag ends; false for a
// tag whose address names something or has a scheme, which is read as
// notionMarkdownMention reads it.
func (parser *workspaceImportInlineParser) version1MentionWords(value string, start int) (graphTextSpan, int, bool) {
	match := notionMarkdownMentionPattern.FindStringSubmatch(value[start:])
	if match == nil || match[1] == "date" {
		return graphTextSpan{}, 0, false
	}
	end := start + len(match[0])
	words := ""
	if match[3] == ">" {
		closing := strings.Index(value[end:], "</mention-"+match[1]+">")
		if closing < 0 {
			return graphTextSpan{}, 0, false
		}
		words = strings.TrimSpace(unescapeNotionMarkdown(value[end : end+closing]))
		end += closing + len("</mention-"+match[1]+">")
	}
	address, named := "", false
	for _, attribute := range notionMarkdownAttributePattern.FindAllStringSubmatch(match[2], -1) {
		if attribute[1] == "url" {
			address = strings.NewReplacer(`&quot;`, `"`, `&lt;`, `<`, `&gt;`, `>`, `&amp;`, `&`).Replace(attribute[2])
			named = true
		}
	}
	// A comment's "v1" refuses or links some where a page's reads words.
	if read, comment := parser.sitePages.version1CommentMention(match[1], address, named, words); comment {
		return read, end, true
	}
	trimmed := strings.TrimSpace(address)
	if named && trimmed == "" {
		switch {
		case address == "" && match[1] == "user":
			// Notion's reads one as a piece of no text, whatever words it
			// holds, which a block whose words stay keeps (seen 2026-11-11;
			// notionMarkdownKeptPieces).
			return graphTextSpan{EmptyPiece: graphTextEmptyText}, end, true
		case address == "" && words == "":
			words = "Source"
		}
		return graphTextSpan{Text: words}, end, true
	}
	// One of other attributes but no address reads as nothing, one of none
	// as text (seen 2026-11-11: <mention-page start="…"/>).
	if !named && strings.TrimSpace(match[2]) != "" {
		return graphTextSpan{}, end, true
	}
	if !named || strings.HasPrefix(trimmed, notionMarkdownDataSourceAddress) && match[1] != "user" {
		return graphTextSpan{}, 0, false
	}
	origin := address
	if parser.sitePages != nil {
		origin = strings.TrimRight(parser.sitePages.Origin, "/")
	}
	// A page's or a database's tag of a view's address is a link to the
	// app's address of its ID (notionMarkdownViewAddress).
	if viewWords, view, viewed := parser.viewAddress(address); viewed && (match[1] == "page" || match[1] == "database") {
		if words == "" {
			words = viewWords
		}
		return graphTextSpan{Text: words, Link: view}, end, true
	}
	_, person := notionMarkdownPersonAddress(address)
	switch {
	case match[1] == "user" && !strings.HasPrefix(address, "user://"):
		return graphTextSpan{Text: parser.version1AddressWords(address)}, end, true
	case match[1] == "user" && person:
		return graphTextSpan{}, 0, false
	case match[1] == "user" && !person:
		// One naming no one reads as the app's address, its words
		// dropped (seen 2026-11-11: "https://app.notion.com").
		return graphTextSpan{Text: origin}, end, true
	case strings.HasPrefix(address, "user://") && !person && match[1] != "data-source":
		if words == "" {
			words = origin
		}
		return graphTextSpan{Text: words}, end, true
	case strings.HasPrefix(address, "user://"):
		if words == "" {
			words = origin
		}
		return graphTextSpan{Text: words, Link: parser.linkTarget(address)}, end, true
	case match[1] == "data-source":
		if words == "" {
			words = parser.version1AddressWords(address)
		}
		return graphTextSpan{Text: words, Link: parser.linkTarget(address)}, end, true
	}
	if notionMarkdownHostPortAddress(trimmed) {
		return graphTextSpan{}, end, true
	}
	if scheme, _, _ := strings.Cut(trimmed, ":"); notionLinkSchemePattern.MatchString(trimmed) &&
		!strings.EqualFold(scheme, "user") {
		// A web, mail or phone address is read as notionMarkdownMention
		// reads it; a person's in another case, "USER://…", is words (seen
		// 2026-11-11).
		return graphTextSpan{}, 0, false
	}
	if words == "" {
		words = address
	}
	if !strings.HasPrefix(trimmed, "/") {
		return graphTextSpan{Text: words}, end, true
	}
	if parsed, err := url.Parse(trimmed); err != nil || strings.HasPrefix(trimmed, "//") ||
		notionMarkdownMentionPathID(parsed.Path) {
		return graphTextSpan{}, 0, false
	}
	if match[3] != ">" && parser.sitePages != nil {
		words = strings.TrimRight(parser.sitePages.Origin, "/") + trimmed
	}
	return graphTextSpan{Text: words, Link: trimmed}, end, true
}

// notionMarkdownMentionPathID is whether a path's last part is an ID a
// mention names its target by: Notion's (32 hexadecimal digits, with or
// without dashes, maybe after a title) or a record's of the app.
func notionMarkdownMentionPathID(path string) bool {
	last := path[strings.LastIndex(path, "/")+1:]
	return notionMarkdownAddressHex(last) != "" || leafenOwnID.MatchString(last)
}

// version1AddressWords is the words "v1" reads a person's mention tag of
// another address as (seen 2026-11-11): of a page's, a database's or a data
// source's address — Notion's, the app's, a path ending in the ID, or
// collection://ID — the app's address of it, /p/ and the ID's 32 digits;
// of any other its address as written.
func (parser *workspaceImportInlineParser) version1AddressWords(address string) string {
	trimmed := strings.TrimSpace(address)
	if parser.sitePages == nil {
		return address
	}
	id, named := strings.CutPrefix(trimmed, notionMarkdownDataSourceAddress)
	if !named {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return address
		}
		web := strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")
		if _, linked := notionMarkdownMentionLink(trimmed, "", parser.sitePages); web && parsed.Host != "" && !linked ||
			parsed.Scheme == "" && strings.HasPrefix(trimmed, "/") && !strings.HasPrefix(trimmed, "//") &&
				notionMarkdownMentionPathID(parsed.Path) {
			id, named = notionMarkdownAddressID(trimmed), true
		}
	}
	if hex := notionMarkdownAddressHex(id); named && hex != "" {
		return strings.TrimRight(parser.sitePages.Origin, "/") + "/p/" + hex
	}
	return address
}
