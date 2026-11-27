package app

import (
	"net/url"
	"strings"
)

// The address of one of this server's pages standing bare in Markdown a
// connection writes is a mention of the page, as Notion's Markdown makes the
// address of one of its pages written alone (seen 2026-11-11): in the public
// API's "v1" and "v2", in its MCP's page body and in the values and titles
// it sets, wherever text is read — a block's text, a table cell, a caption.
// The address may have a query, the page's title before its ID and bold or
// other marks around it, which the mention keeps; one with a block's ID
// after # mentions that block. An address between angle brackets (<…>) is
// one written alone in "v2" and Notion's MCP, the brackets gone; "v1" keeps
// the brackets as text around it (notionMarkdownVersion1Input). A link
// written with words ([words](address)), the address as its words
// ([address](address)) too, stays a link, and so does an address of the
// page at another host — but for one of Notion's, which mentions what its ID
// names here (notion_address_bare_mentions_here.go). A comment's Markdown
// mentions no address written alone (BareAsText).

// notionMarkdownSitePages is this server, origin, as a connection writing
// Markdown names its pages: by Leafen's IDs, as the MCP tools do (/p/ID or
// /pages/ID, a block by #block-ID or Notion's #ID), or by the public API's
// (/p/ID, with or without dashes and maybe after the page's title, a block
// by #ID).
type notionMarkdownSitePages struct {
	Origin      string
	InternalIDs bool
	// Discussions reads a <span discussion-urls>'s names as the threads its
	// words are marked with (notion_page_markdown_discussion_marks.go).
	Discussions func(addresses string) string
	// LinksMentioned is a writer that makes the links to the workspace's
	// pages and databases it reads their mentions after (notion-create-pages
	// unless asked to keep them, mentionMCPInternalLinks): Notion's MCP
	// makes a link to a data source its mention (seen 2026-11-11), and one to
	// a web address of its own the mention of what the address names (seen
	// 2026-11-21), so the reader leaves those links as written for the writer
	// to tell them (mcpLinkAsWritten).
	LinksMentioned bool
	// BareAsText reads an address standing bare, or between angle brackets,
	// as the words it is, as Notion reads a comment's Markdown
	// (notionCommentMarkdownSite); a [words](address) link stays a link.
	BareAsText bool
	// AngleLinked is a BareAsText reader that links an address between angle
	// brackets, its brackets gone, to the address as written, never its
	// mention, as Notion's public API reads a comment's "v2" Markdown and its
	// MCP a comment's (notionCommentMarkdownSite).
	AngleLinked bool
	// Comment is a comment's reader (notionCommentMarkdownSite), which
	// refuses a link to a data source (dataSourceLink) and every
	// <unknown_mention> (unknownMention).
	Comment bool
	// DataSourceNamed is whether an ID names a data source a title's or a
	// value's writer reaches, by its own ID of any form or the public API's
	// (notionMarkdownValueMentionsNothing, mcpValueSitePages).
	DataSourceNamed func(id string) bool
}

// linksMentioned is whether the site's writer makes links mentions
// (LinksMentioned).
func (site *notionMarkdownSitePages) linksMentioned() bool {
	return site != nil && site.LinksMentioned
}

// bareAsText is whether the site's reader keeps addresses standing bare as
// text (BareAsText).
func (site *notionMarkdownSitePages) bareAsText() bool {
	return site != nil && site.BareAsText
}

// angleAsText is whether the site's reader keeps an address between angle
// brackets as text (BareAsText, unless AngleLinked).
func (site *notionMarkdownSitePages) angleAsText() bool {
	return site.bareAsText() && !site.AngleLinked
}

// notionCommentInvalidLink refuses a comment whose link's address is none a
// link takes — a data source's, between angle brackets or in
// [words](collection://…) — where Notion refuses one (seen 2026-11-22: its
// public API's "v1" and "v2" and its MCP), in Leafen's words.
const notionCommentInvalidLink = "This Markdown links to a URL a link can't take."

// angleDataSource is mention, a data source's that its address between
// angle brackets makes, as an AngleLinked reader reads it: a link to that
// address, which Notion refuses as one no link takes (seen 2026-11-22: its
// MCP's comment of <collection://…> refused, as one of [words](collection://…)).
func (site *notionMarkdownSitePages) angleDataSource(mention graphTextSpan) graphTextSpan {
	if site != nil && site.AngleLinked {
		return graphTextSpan{Mention: notionMarkdownRefusedMention, Text: notionCommentInvalidLink}
	}
	return mention
}

// notionCommentUnknownMention refuses a comment holding an <unknown_mention>
// tag, a custom emoji's too, where Notion refuses one (seen 2026-11-22: its
// public API's "v1" and "v2" and its MCP, naming the tag's type, unknown,
// then the types a comment's mention takes), in Leafen's words.
var notionCommentUnknownMention = notionCommentMentionTypeSaid("unknown")

// notionCommentMentionTypeSaid refuses a comment holding a mention of a
// type, as Notion names it, that a comment can't hold, naming the types it
// holds (notionCommentUnknownMention, notionCommentMentionHeld).
func notionCommentMentionTypeSaid(kind string) string {
	return "This Markdown can't hold a mention of type " + kind +
		"; it holds mentions of type user, page, database, data source and date."
}

// unknownMention is mention, which an <unknown_mention> tag makes
// (notionMarkdownUnknownMention), as the site's reader reads it: a
// comment's refuses every one, whatever its alt and address
// (notionCommentUnknownMention), where a page's keeps a custom emoji's.
func (site *notionMarkdownSitePages) unknownMention(mention graphTextSpan) graphTextSpan {
	if site != nil && site.Comment {
		return graphTextSpan{Mention: notionMarkdownRefusedMention, Text: notionCommentUnknownMention}
	}
	return mention
}

// dataSourceLink is a [words](address) link to a data source as a comment's
// reader reads it, in "v1", "v2" and the MCP alike: refused as one no link
// takes, as Notion's comment refuses it (seen 2026-11-22), where a page's
// Markdown makes it the source's mention or a link to it
// (mcpLinkAsWritten). ok is false for any other reader or address.
func (site *notionMarkdownSitePages) dataSourceLink(address string) (graphTextSpan, bool) {
	if site == nil || !site.Comment || !strings.HasPrefix(strings.TrimSpace(address), notionMarkdownDataSourceAddress) {
		return graphTextSpan{}, false
	}
	return graphTextSpan{Mention: notionMarkdownRefusedMention, Text: notionCommentInvalidLink}, true
}

// notionMarkdownSitePagesAt is a writer's site at origin; nil, which reads
// every address as a link, for none.
func notionMarkdownSitePagesAt(origin string, internalIDs bool) *notionMarkdownSitePages {
	if strings.TrimSpace(origin) == "" {
		return nil
	}
	return &notionMarkdownSitePages{Origin: origin, InternalIDs: internalIDs}
}

// sitePages is the site a reader's writes name pages at
// (notionMarkdownSitePages).
func (reader notionMarkdownReader) sitePages() *notionMarkdownSitePages {
	site := notionMarkdownSitePagesAt(reader.Origin, reader.InternalIDs)
	if site != nil {
		site.LinksMentioned, site.Discussions = reader.LinksMentioned, reader.Threads
	}
	return site
}

// mention is the mention an address standing bare makes, marked as the text
// it stands in (style): of the page it names, or of the block its anchor
// names, by the ID it gives in the page's place — which the writer checks,
// anchors at the block's page and names (anchorPageMention) — unnamed until
// then. One of Notion's web addresses is the mention notionAddressMention
// makes, its words style's text. ok is false for an address of no page here,
// which stays a link, and for any address a BareAsText reader reads.
func (site *notionMarkdownSitePages) mention(address string, style graphTextSpan) (graphTextSpan, bool) {
	// A site with no address — a writer of none of its own (mcpValueSitePages) —
	// reads an address as none does.
	if site == nil || site.BareAsText || site.Origin == "" {
		return graphTextSpan{}, false
	}
	parsed, err := url.Parse(strings.TrimSpace(address))
	here, hereErr := url.Parse(site.Origin)
	if err != nil || hereErr != nil || here.Host == "" || !strings.EqualFold(parsed.Scheme, here.Scheme) ||
		!strings.EqualFold(parsed.Host, here.Host) {
		return site.notionAddressMention(address, style)
	}
	id := ""
	if site.InternalIDs {
		pageID, blockID := mcpInternalPageID(address)
		if id = pageID; blockID != "" {
			id = blockID
		}
	} else if path, named := strings.CutPrefix(parsed.Path, "/p/"); named {
		id = notionMarkdownAddressHex(path)
		if block := notionMarkdownAddressHex(parsed.Fragment); block != "" &&
			len(strings.ReplaceAll(parsed.Fragment, "-", "")) == len(block) {
			id = block
		}
	}
	if id == "" {
		return graphTextSpan{}, false
	}
	return graphTextSpan{
		Text: id, Mention: "page", PageID: id, Bold: style.Bold, Italic: style.Italic, Strikethrough: style.Strikethrough,
		Underline: style.Underline, Color: style.Color, Background: style.Background,
	}, true
}

// names tells an address on this site, or one with no host, which a page
// of the site's names by its path.
func (site *notionMarkdownSitePages) names(address string) bool {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	if parsed.Host == "" {
		return parsed.Scheme == ""
	}
	if site == nil {
		return false
	}
	here, err := url.Parse(site.Origin)
	return err == nil && strings.EqualFold(parsed.Scheme, here.Scheme) && strings.EqualFold(parsed.Host, here.Host)
}

// ownsAddress tells an address at this server that names a page or a
// database by Leafen's ID as the MCP tools give it, /p/ID or /pages/ID,
// whatever the ID's form: one given through an API, such as
// one-database-2, is kept as it is (notionMentionID), and only here is
// such a path known to be a page's (notionMarkdownLeafenPageAddress).
func (site *notionMarkdownSitePages) ownsAddress(address string) bool {
	if site == nil || !site.InternalIDs || !site.names(address) {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(address))
	return err == nil && parsed.Host != "" && notionMarkdownPageLinkID(parsed.Path) != ""
}

// mcpSitePages is this server as the MCP tools name its pages, at the
// address the connection reaches it by (mcpMarkdownReader).
func mcpSitePages(authority mcpAuthority) *notionMarkdownSitePages {
	origin, _ := mcpServerURL(authority.Resource, "")
	return notionMarkdownSitePagesAt(origin, true)
}
