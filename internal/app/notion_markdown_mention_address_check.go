package app

import "strings"

// notionMarkdownMentionSaid is what "v2" and the MCP say refusing a mention
// tag that mentions nothing — a <mention>, or a "v1" <mention-…> one read
// as the <mention> it becomes — and "" for any other tag. It is the one
// check of a mention's address, for a page's "v2" body
// (notionMarkdownRepair.check) and a comment's "v2" and MCP Markdown
// (notionCommentMarkdownMentionsSaid) alike, as Notion reads both by one
// (seen 2026-11-22: the same words for a page's insert_content, its MCP's
// and its public API's, and for a comment, its MCP's and its API's): one
// with no url; one whose url is empty (or of spaces), a mail address, one
// without a scheme or of a scheme no mention takes
// (notionMarkdownMentionScheme), a user:// one naming no one
// (notionMarkdownUserAddressValid), any agent's, or a teamspace's or a
// group's naming none by its ID (notionMarkdownMentionNamesOne); and one
// whose type its address can't be (notionMarkdownMentionTypeClash).
func notionMarkdownMentionSaid(name string, attributes map[string]string) string {
	// v1's mentions are the <mention> each becomes, a <mention-database> of
	// type="database" but of a collection:// address (seen 2026-11-11); a
	// tag of another name after "mention-" is none, its words (seen
	// 2026-11-15: <mention-link url="mailto:…">y</mention-link> is text).
	mention := name == "mention" || strings.HasPrefix(name, "mention-") && name != "mention-date" && notionMarkdownVersion1Tags[name] != ""
	if !mention {
		return ""
	}
	address, addressed := attributes["url"]
	kind := attributes["type"]
	if name == "mention-database" && !strings.HasPrefix(strings.TrimSpace(address), notionMarkdownDataSourceAddress) {
		kind = "database"
	}
	switch {
	case !addressed:
		return `<mention> needs a url, as in <mention url="...">...</mention>.`
	case strings.TrimSpace(address) == "" || !notionMarkdownMentionScheme(address) ||
		!notionMarkdownUserAddressValid(address) || !notionMarkdownMentionNamesOne(address):
		// An empty url (or one of spaces), a mail address, or one without a
		// scheme, mentions nothing (seen 2026-11-10; an empty url and
		// <mention-user> too, 2026-11-11), nor a user:// one naming no one
		// (notionMarkdownPersonAddress; seen 2026-11-11).
		return notionMarkdownMentionPointSaid(address)
	case notionMarkdownMentionTypeClash(kind, address):
		return `Mention type "` + kind + `" doesn't go with URL "` + address + `".`
	}
	return ""
}

// notionMarkdownMentionNamesOne reports whether an agent's, a teamspace's or
// a group's address names one. A teamspace's or a group's does by an ID one
// could have: a UUID, as Notion's are (seen 2026-11-22: teamspace://x,
// teamspace://<15 letters> and group://x refused, teamspace://<UUID> and
// group://<UUID> taken, in a page's "v2" and a comment's alike), or one of
// Leafen's own IDs, as its teamspaces' are — its MCP gives teamspace://<its
// ID> (notion-get-sidebar-tabs) — though Notion refuses 15 letters of no
// UUID's form, as it has none such. A data source's of no UUID's form is
// refused where its write finds no data source of that ID
// (keepOrRefuseUnresolvedDataSource), as Leafen's own IDs of any form name
// its data sources. An agent's names none: Notion refuses
// one of a UUID no agent has, and of a person's ID (seen 2026-11-22, a
// page's "v2" by insert_content and create-pages, a comment's by its MCP
// and public API), and Leafen has no agents (user decision 2026-10-29) —
// an agent that works in it does so as the person who connected it. Any
// other address names one as far as this can tell.
func notionMarkdownMentionNamesOne(address string) bool {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(address), ":")
	switch strings.ToLower(scheme) {
	case "agent":
		return false
	case "teamspace", "group":
		id := strings.TrimPrefix(rest, "//")
		_, isUUID := normalizeNotionUUIDRequest(id)
		return isUUID || leafenOwnID.MatchString(id)
	}
	return true
}

// notionMarkdownMentionPointSaid is what refuses a mention of an address
// that names nothing, as Notion says 'Unsupported mention url "…" on
// <mention>. Use a user, page, database, data source, agent, teamspace, or
// group URL.' (its values and their order), in Leafen's words.
func notionMarkdownMentionPointSaid(address string) string {
	return `<mention> can't point to "` + address +
		`": give it the URL of a user, page, database, data source, agent, teamspace or group.`
}

// version1CommentMention is what a comment's "v1" Markdown reads a mention
// tag of kind (user, page, database, data-source) as, where it parts from a
// page's "v1" (version1MentionWords): Notion's comment refuses a person's
// tag whose url names no one and a data source's whose url is no
// collection:// address of an ID (notionCommentVersion1MentionSaid), and
// links a page's or a database's of an address on Notion's sites that names
// no page (notionMarkdownNotionAddressLink), which it takes, as every
// page's and database's tag (seen 2026-11-22, its public API's "v1"). A tag
// without a url is its words in either. ok is false for any other reader,
// tag or address, read as a page's "v1" reads it.
func (site *notionMarkdownSitePages) version1CommentMention(kind, address string, addressed bool, words string) (graphTextSpan, bool) {
	if site == nil || !site.Comment || !addressed {
		return graphTextSpan{}, false
	}
	if said := notionCommentVersion1MentionSaid(kind, address); said != "" {
		return graphTextSpan{Mention: notionMarkdownRefusedMention, Text: said}, true
	}
	if kind == "page" || kind == "database" {
		if link, _, linked := notionMarkdownNotionAddressLink(address, words, site); linked && link.Link != "" &&
			notionMarkdownNotionWebAddress(address) != "" {
			return link, true
		}
	}
	return graphTextSpan{}, false
}

// notionCommentVersion1MentionSaid is what refuses a comment's "v1" mention
// tag of kind whose url is address, "" for one it takes, as Notion's public
// API reads a comment's "v1" (seen 2026-11-22): a person's tag of an address
// naming no one (notionMarkdownPersonAddress — an empty one, of spaces
// around it, of another scheme or case, of an ID without its dashes or of
// another version), and a data source's of any but collection:// and an ID
// (notionCommentVersion1DataSourceAddress). A page's or a database's tag is
// never refused for its address; the first tag refused, in the order
// written, is said.
func notionCommentVersion1MentionSaid(kind, address string) string {
	switch kind {
	case "user":
		if _, person := notionMarkdownPersonAddress(address); !person {
			return notionCommentVersion1UserSaid
		}
	case "data-source":
		if !notionCommentVersion1DataSourceAddress(address) {
			return notionCommentVersion1DataSourceSaid
		}
	}
	return ""
}

// notionCommentVersion1DataSourceAddress is whether a comment's "v1" takes
// address for a data source's tag: collection:// written so, then an ID of
// 32 hexadecimal digits or a dashed UUID, nothing around them (seen
// 2026-11-22: " collection://…", "COLLECTION://…", "collection://ID/x" and
// "collection://ID?v=1" refused). Notion takes a dashed one only of a
// person's ID's form (notionMarkdownPersonUUID); Leafen takes any UUID, as
// its own data sources' public IDs are of another version
// (notionCompatibleObjectID), and one of them that names nothing leaves no
// words where Notion refuses it.
func notionCommentVersion1DataSourceAddress(address string) bool {
	id, collection := strings.CutPrefix(address, notionMarkdownDataSourceAddress)
	_, isUUID := normalizeNotionUUIDRequest(strings.ToLower(id))
	return collection && isUUID
}

// notionCommentVersion1UserSaid and notionCommentVersion1DataSourceSaid
// refuse a comment's "v1" mention tag (notionCommentVersion1MentionSaid),
// as Notion says "Invalid user mention URL. Expected a valid user ID." and
// "Invalid data source mention URL. Expected a collection:// URL.", in
// Leafen's words.
const (
	notionCommentVersion1UserSaid       = "This user mention's URL names no user: give it a valid user ID."
	notionCommentVersion1DataSourceSaid = "This data source mention's URL isn't a data source's: give it a collection:// URL."
)
