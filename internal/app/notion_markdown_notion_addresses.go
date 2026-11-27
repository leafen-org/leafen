package app

import (
	"cmp"
	"net/url"
	"regexp"
	"strings"
)

// A "v2" <mention> of an address on Notion's own sites (notion.so,
// notion.com, notion.site) names a page by the ID its path's last part ends
// in, after the page's title or not, as Notion's "v2" reads it (seen
// 2026-11-15, create and replace alike): one a page's ID could be — a
// UUID's version 1 to 8 and its standard variant, in 32 hexadecimal digits
// or a dashed UUID — is a mention of that page, of the page a Notion move
// made from it, or where neither is here a link to the address as written
// (notion_address_mentions_here.go: Notion keeps an "Untitled" mention, a
// user decision of 2026-11-21 differs, as its links do); any other address
// there names no page and is a link to it, its words the tag's text or the
// address as written when it holds none ("…/abc-0123456789abcdef0123456789abcdef",
// "…/abc-00000000000000000000000000000000", an ID too short or not
// hexadecimal, a database's mention too).

// notionMarkdownPageUUIDPattern is an ID a page's could be in Notion's
// reading of a mention's address.
var notionMarkdownPageUUIDPattern = regexp.MustCompile(`^[0-9a-f]{12}[1-8][0-9a-f]{3}[89ab][0-9a-f]{15}$`)

// notionMarkdownNotionAddressLink is the link a mention of an address reads
// as (notionMarkdownMentionLink, and one on Notion's sites naming no page),
// or else the ID of the page an address on Notion's sites names, without a
// fragment, "" for any other.
func notionMarkdownNotionAddressLink(address, text string, site *notionMarkdownSitePages) (graphTextSpan, string, bool) {
	if link, linked := notionMarkdownMentionLink(address, text, site); linked {
		return link, "", true
	}
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") ||
		!notionMarkdownNotionHost(parsed.Hostname()) {
		return graphTextSpan{}, "", false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	last := parts[len(parts)-1]
	id := ""
	if notionMarkdownDashedUUIDPattern.MatchString(last) {
		id = strings.ToLower(strings.ReplaceAll(last, "-", ""))
	} else if found := notionMarkdownTrailingIDPattern.FindString(strings.ToLower(last)); found != "" {
		id = found
	}
	if notionMarkdownPageUUIDPattern.MatchString(id) {
		if parsed.Fragment != "" {
			return graphTextSpan{}, "", false
		}
		return graphTextSpan{}, id, false
	}
	target := workspaceImportLinkTarget(address)
	if target == "" {
		return graphTextSpan{}, "", false
	}
	if strings.TrimSpace(text) == "" {
		text = strings.TrimSpace(address)
	}
	return graphTextSpan{Text: text, Link: target}, "", true
}

// notionMarkdownNotionHost tells a host of Notion's own sites.
func notionMarkdownNotionHost(host string) bool {
	host = strings.ToLower(host)
	for _, notion := range []string{"notion.so", "notion.com", "notion.site"} {
		if host == notion || strings.HasSuffix(host, "."+notion) {
			return true
		}
	}
	return false
}

// notionMarkdownAddressMention is the mention a <mention> of an address
// reads as, with its text: a data source's (collection://…) its database's
// (notion_markdown_data_source_mentions.go); a web address that is no
// page's a link (notionMarkdownMentionLink, which tells the writer's own
// server's, site's, addresses; notionMarkdownNotionAddressLink, Notion's
// sites' by the ID they end in); a person's (user://…) a person (an
// agent's is refused before, notionMarkdownMentionNamesOne); any other a
// page or, of type="database", a database —
// the writer finding which the ID names (seen 2026-11-11: a database's
// address of type="page" mentions it, and a page's of type="database" the
// page; notionMentionPreparation.databasePageMention,
// pageDatabaseMention).
func notionMarkdownAddressMention(address, text string, database bool, site *notionMarkdownSitePages) (graphTextSpan, bool) {
	if source, collection := strings.CutPrefix(address, notionMarkdownDataSourceAddress); collection {
		source = strings.TrimSpace(source)
		return graphTextSpan{Text: source, Mention: "database", DatabaseID: notionMarkdownDataSourceAddress + source}, source != ""
	}
	link, named, linked := notionMarkdownNotionAddressLink(address, text, site)
	if linked {
		return link, true
	}
	id, page := cmp.Or(named, notionMarkdownAddressID(address)), cmp.Or(named, notionMarkdownPageMentionID(address))
	return notionMarkdownIDMention(address, text, id, page, database)
}

// notionMarkdownIDMention is the mention of an address that is no link, by
// the ID it names (id) and the page or block that ID is (page). A page's or a
// database's mention of one of Notion's web addresses keeps the address as
// its link for the writer, which makes it a link to Notion where nothing
// here answers to its page (notion_address_mentions_here.go).
func notionMarkdownIDMention(address, text, id, page string, database bool) (graphTextSpan, bool) {
	// A teamspace's or a group's is kept by its address.
	if kept, teamOrGroup := notionMarkdownTeamOrGroupMention(address); teamOrGroup {
		return kept, true
	}
	span := graphTextSpan{Text: text}
	switch {
	case strings.HasPrefix(strings.ToLower(address), "agent://"):
		// An agent's names no one here (notionMarkdownMentionNamesOne): a
		// body or a comment refuses one before, a title or a value keeps it
		// as words (notionMarkdownValueMentionsNothing), and any other reader
		// reads none.
		return graphTextSpan{}, false
	case strings.HasPrefix(address, "user://"):
		span.Mention, span.UserID = "person", id
	case database:
		span.Mention, span.DatabaseID, span.Link = "database", id, notionMarkdownNotionWebAddress(address)
	default:
		span.Mention, span.PageID, span.Link = "page", page, notionMarkdownNotionWebAddress(address)
	}
	if span.Text == "" {
		// A tag without text keeps its place until the writer gives it
		// its target's name.
		span.Text = span.PageID + span.DatabaseID + span.UserID
	}
	return span, span.PageID+span.DatabaseID+span.UserID != ""
}

// notionMarkdownValueMentionsNothing is whether a title's or a value's
// <mention> tag of attributes mentions nothing, which Notion's MCP keeps as
// its words where a page's body refuses it (seen 2026-11-22, a page's title
// and a text property's value by create-pages and update-page alike): one
// the body's check refuses (notionMarkdownMentionSaid — no url, an address
// naming nothing, a type its address can't be), and one of a data source's
// address by an ID of no UUID's form that names no source its writer reaches
// (DataSourceNamed: a source's own ID is of any form, a Notion move's or an
// import's, the app's), or, with no writer to ask, of no form Leafen's IDs
// have. A comment's names nothing so: it is refused as a page's body
// refuses it (keepOrRefuseUnresolvedDataSource), as Notion refuses one (seen
// 2026-11-22). A tag closed in itself stays as written, a pair the words
// between them (mcpPropertyTagsAsText).
func notionMarkdownValueMentionsNothing(attributes map[string]string, site *notionMarkdownSitePages) bool {
	if notionMarkdownMentionSaid("mention", attributes) != "" {
		return true
	}
	id, source := strings.CutPrefix(strings.TrimSpace(attributes["url"]), notionMarkdownDataSourceAddress)
	id = strings.TrimSpace(id)
	if _, isUUID := normalizeNotionUUIDRequest(strings.ToLower(id)); !source || isUUID || site != nil && site.Comment {
		return false
	}
	if site != nil && site.DataSourceNamed != nil {
		return !site.DataSourceNamed(id)
	}
	return !leafenOwnID.MatchString(id)
}
