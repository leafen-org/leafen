package app

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// notionCommentMarkdown is a comment's Markdown as a Public API connection
// writes it: its text, the version it names — "v1", Notion's enhanced
// Markdown, by default, or "v2", Notion-flavored Markdown, as its docs have
// both (read 2026-11-13) — and the site it is read at
// (notionMarkdownSitePages), whose mention tags name its pages.
type notionCommentMarkdown struct {
	Text    string
	Version string
	Site    *notionMarkdownSitePages
}

// notionCommentMarkdownOf is a request's Markdown, nil for none, read as
// Notion's public API reads a comment's of its version, for a comment made
// or changed alike (seen 2026-11-22, its POST's answer and GET the same),
// its addresses as notionCommentMarkdownSite says.
func notionCommentMarkdownOf(event *core.RequestEvent, markdown *string, version string) *notionCommentMarkdown {
	if markdown == nil {
		return nil
	}
	origin, _ := outwardServerOrigin(event.Request)
	site := notionCommentMarkdownSite(notionMarkdownSitePagesAt(origin, false), version)
	return &notionCommentMarkdown{Text: *markdown, Version: version, Site: site}
}

// notionCommentMarkdownSite is site, or none, read as Notion reads a
// comment's Markdown of version — the one place a comment's addresses part
// from a page's, for its public API's comments and its MCP's alike, the
// MCP's read as "v2" (mcpCommentMarkdownSpans). An address standing bare —
// one of Notion's pages', this server's, example.com or www.… — stays its
// words (BareAsText: the API's "v1" and "v2" seen 2026-11-22, the MCP's
// 2026-11-21). One between angle brackets stays words with its brackets in
// "v1"; in "v2" and the MCP it is a link to the address as written, its
// brackets gone (AngleLinked), never a mention — one of Notion's pages' or
// databases' too (seen 2026-11-22, the API's "v2" and the MCP's) — and
// without a scheme it stays words with its brackets (the MCP's, seen
// 2026-11-22). A [words](address) link stays a link, and a link to one of
// Notion's web addresses then points here only when its ID answers here
// (notion_address_links_here.go); one to a data source is refused, in either
// version and the MCP (dataSourceLink), as is every <unknown_mention>
// (unknownMention).
func notionCommentMarkdownSite(site *notionMarkdownSitePages, version string) *notionMarkdownSitePages {
	if site == nil {
		site = &notionMarkdownSitePages{}
	}
	site.BareAsText, site.AngleLinked, site.Comment = true, version == notionMarkdownVersion2, true
	return site
}

// notionCommentMarkdownRead reads a comment's Markdown as the page's
// Markdown of its version reads a paragraph's text: inline only, as
// Notion's docs say of a comment's, a line break as one and a heading's or a
// list's marks as words. "v1" reads its <span underline="true" color="…">,
// <br>, <mention-user|page|database|data-source|date> tags and its marks as
// a "v1" page's text does (notionMarkdownVersion1Text), and the "v2" tags as
// words, but keeps no colour: a span's colour is left out and its words
// stay a piece of their own, as Notion's "v1" comment keeps them (seen
// 2026-11-21 twice, `<span color="red">빨강</span> 글` read as "빨강" and
// " 글" with no colour, its underline kept); "v2" reads a value's text as
// Notion's MCP reads a comment (mcpPropertyMarkdownSpans).
func notionCommentMarkdownRead(markdown notionCommentMarkdown) (string, []graphTextSpan) {
	if markdown.Version == notionMarkdownVersion2 {
		// Its <mention> tags are checked first, as Notion's comment refuses
		// one (notionCommentMarkdownMentionsSaid).
		if said := notionCommentMarkdownMentionsSaid(markdown.Text, markdown.Site); said != "" {
			return "", []graphTextSpan{{Mention: notionMarkdownRefusedMention, Text: said}}
		}
		return mcpPropertyMarkdownSpans(markdown.Text, markdown.Site)
	}
	text := strings.ReplaceAll(strings.ReplaceAll(markdown.Text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	for index := range lines {
		lines[index] = notionMarkdownVersion1Text(lines[index])
	}
	text = markdownLineBreakPattern.ReplaceAllString(strings.Join(lines, "\n"), "\n")
	words, spans := notionMarkdownSiteInlineSpans(text, true, markdown.Site)
	for index := range spans {
		spans[index].Color, spans[index].Background = "", ""
	}
	return words, spans
}

func notionCommentMarkdownSpans(
	app core.App,
	identity notionOAuthBearerIdentity,
	page pageAuthorization,
	markdown notionCommentMarkdown,
) ([]mcpCommentSpan, error) {
	// Markdown may be as long as a request holds, and empty for a comment of
	// no words, as Notion's (seen 2026-11-11: 300,000 characters taken, ""
	// read back as no rich text).
	words, read := notionCommentMarkdownRead(markdown)
	if read == nil && words != "" {
		read = []graphTextSpan{{Text: words}}
	}
	// Its links to Notion's web addresses point here only when something
	// here answers to their IDs, as a page's Markdown's do
	// (notion_address_links_here.go).
	if _, err := newNotionAddressLinksHere(app, page.Workspace.WorkspaceID).spans(read); err != nil {
		return nil, err
	}
	// A data source's mention is the source's own, or kept by the ID written
	// when the connection can't read it, as a page's Markdown keeps one.
	sources := newNotionMentionPreparation(app, identity, page)
	sources.markdown = true
	spans := []mcpCommentSpan{}
	for _, piece := range read {
		if err := sources.dataSourceMention(&piece); err != nil {
			// "v1" takes a page's or a database's tag of a data source's
			// address that names none, leaving no words, as Notion's does
			// (seen 2026-11-22); its data source tags of no ID are refused
			// before (notionCommentVersion1MentionSaid).
			if markdown.Version == notionMarkdownVersion2 || !notionCommentMarkdownSourceNotHere(err) {
				return nil, err
			}
			continue
		}
		// One kept by the ID written is kept so, as Notion's comment keeps it
		// (notion_markdown_kept_data_sources.go).
		if _, kept := notionKeptDataSourceID(piece); kept {
			spans = append(spans, mcpCommentSpanFromText(piece))
			continue
		}
		span, err := notionCommentMarkdownSpan(app, page, piece)
		if err != nil {
			return nil, err
		}
		if span.Text == "" {
			continue
		}
		if err := authorizeNotionCommentSpan(app, identity, page, &span); err != nil && !notionCommentMarkdownUnknownPerson(err, &span) {
			return nil, err
		}
		spans = append(spans, span)
	}
	if len(spans) > mcpCommentMaximumRichTextItems {
		return nil, notionInvalid(errCommentInvalid, fmt.Sprintf("The comment's markdown reads as at most %d rich text items.", mcpCommentMaximumRichTextItems))
	}
	return spans, nil
}

// notionCommentMarkdownSpan is a piece of a comment's Markdown as a comment
// keeps it, its mention naming its target as a rich text's would
// (populateNotionMarkdownCommentMention) for the comment's checks to follow
// (authorizeNotionCommentSpan); a :name: one of the workspace's custom
// emoji, or words.
func notionCommentMarkdownSpan(app core.App, page pageAuthorization, piece graphTextSpan) (mcpCommentSpan, error) {
	switch piece.Mention {
	case "custom_emoji":
		if piece.CustomEmojiID == "" {
			if err := resolveNotionMarkdownCustomEmoji(app, page.Workspace.WorkspaceID, &piece); err != nil {
				return mcpCommentSpan{}, err
			}
		}
	case "date":
		// A date of no start is its words.
		if piece.DateStartUnset || piece.Date == "" {
			piece = graphTextSpan{Text: piece.Text}
		}
	case notionMarkdownRefusedMention:
		return mcpCommentSpan{}, notionInvalid(errCommentInvalid, piece.Text)
	}
	span := mcpCommentSpanFromText(piece)
	switch piece.Mention {
	case "person":
		return span, populateNotionMarkdownCommentMention("user", notionCommentMarkdownMentionTarget("user", piece.UserID), piece.Text, &span)
	case "page":
		return span, populateNotionMarkdownCommentMention("page", notionCommentMarkdownMentionTarget("page", piece.PageID), piece.Text, &span)
	case "database":
		return span, populateNotionMarkdownCommentMention("database", notionCommentMarkdownMentionTarget("database", piece.DatabaseID), piece.Text, &span)
	}
	return span, nil
}

var notionCommentMarkdownMentionID = regexp.MustCompile(`([0-9a-fA-F]{8}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{12})$`)

// notionCommentMarkdownMentionTarget is the ID a mention's address in a
// comment's Markdown names: a person's after "user://" and before the "/"
// and email Notion may write after it, a page's or a database's the ID its
// address ends in, and one written bare itself.
func notionCommentMarkdownMentionTarget(kind, address string) string {
	id := strings.TrimSpace(address)
	if kind == "user" {
		id, _ = notionMarkdownUserAddressID(id)
	}
	if match := notionCommentMarkdownMentionID.FindString(strings.TrimRight(strings.SplitN(id, "?", 2)[0], "/")); match != "" {
		id = match
	}
	return id
}

func populateNotionMarkdownCommentMention(kind, publicID, label string, span *mcpCommentSpan) error {
	publicID = strings.TrimSpace(publicID)
	span.Text = label
	if span.Text == "" {
		span.Text = publicID
	}
	switch kind {
	case "user":
		userID, err := resolveNotionCompatibleUserID(publicID)
		if err != nil {
			// An ID Notion's Markdown takes for a person's that names no one
			// here is kept as written, for the comment's checks to find no one
			// (notionCommentMarkdownUnknownPerson); any other names no one.
			kept, isUUID := notionKeptUUID(publicID)
			if !isUUID || !notionMarkdownPersonUUID(strings.ToLower(publicID)) {
				return notionInvalid(errCommentInvalid, "A user mention in markdown needs a user ID.")
			}
			userID = kept
		}
		span.Mention, span.UserID = "person", userID
	case "page":
		pageID, err := resolveNotionCompatiblePageID(publicID)
		if err != nil {
			rowPublicID, ok := normalizeNotionUUIDRequest(publicID)
			if !ok {
				return notionInvalid(errCommentInvalid, "A page mention in markdown needs a page ID.")
			}
			pageID = notionPageMentionRowPrefix + rowPublicID
		}
		span.Mention, span.PageID = "page", pageID
	case "database":
		normalized, ok := normalizeNotionUUIDRequest(publicID)
		if !ok {
			return notionInvalid(errCommentInvalid, "A database mention in markdown needs a database ID.")
		}
		span.Mention, span.DatabaseID = "database", normalized
	default:
		return notionInvalid(errCommentInvalid, "A mention in markdown is a user, page or database.")
	}
	return nil
}

// notionCommentMarkdownUnknownPerson keeps a comment's Markdown mention of a
// person no one of the workspace is — an ID no one here has, someone who has
// left, a connection's bot — as one of no one's: by the person's public ID,
// as written for one no one here has, read "@Anonymous", as Notion's comment
// keeps one in "v1", "v2" and its MCP alike (seen 2026-11-22: <mention
// url="user://<a UUID no one has>"/> a user mention of that ID,
// "@Anonymous"), and as the MCP's and a page's Markdown keep one
// (resolveMarkdownPersonMentions). It tells whether err was that refusal.
func notionCommentMarkdownUnknownPerson(err error, span *mcpCommentSpan) bool {
	refusal := &notionMentionRefusal{}
	if span.Mention != "person" || !errors.As(err, &refusal) || refusal.missingUser == "" {
		return false
	}
	span.UserID, span.Text = refusal.missingUser, notionUnknownPersonMentionText
	return true
}
