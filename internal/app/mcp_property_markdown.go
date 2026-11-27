package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A title's or a text property's value reads and writes through the MCP as
// Notion's MCP has it (seen 2026-11-11): fetch's properties and the rows of
// a query write it as a paragraph of the public API's "v2" page Markdown,
// a mark as its tag, _ and * escaped, text that would start a block
// escaped as a paragraph's is, edge spaces in <p> tags and a line break
// <br />; a value given to update_properties or notion-create-pages reads
// as that Markdown's text, inline: its marks, links, mentions, dates and
// math, the address of one of this server's pages written alone as its
// mention (notionMarkdownSitePages), a line break as one, a <br> as one
// where a block's text reads it so — outside code, equations and escapes,
// "\<br />" and "`a<br />b`" its words, "$a<br />b$" the equation "a⏎b"
// (notionMarkdownLineBreaks; Notion's MCP read a text property, a title and
// a comment so, seen 2026-11-22) — and the tags it
// doesn't read — Notion's older ones (<mention-page>, <mention-date>), HTML
// (<mark>, <del>, <div>) and any other — kept as text, an address in such a
// tag read as one in text is: `<mention-page url="…/p/ID"/>` keeps its words
// with "…/p/ID\"/" a link, which names no page (mcpInternalPageID), and
// `url=…/p/ID />` mentions the page, as Notion's MCP makes both (seen
// 2026-11-16). That is how a Public API "v2" comment reads, as Notion's; an
// MCP write reads those tags first as a page's body does
// (mcpInlineMarkdownSpans). SQL reads the value's words alone, as
// Notion's does, but this Markdown for one holding a mention, a link or an
// equation (databaseSQLTextValue).

// mcpPropertyMarkdownSpans reads a value given as inline Markdown into its
// words and their spans, none for plain words; sitePages names the pages
// its addresses mention.
func mcpPropertyMarkdownSpans(value string, sitePages *notionMarkdownSitePages) (string, []graphTextSpan) {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(value, "\n")
	for index := range lines {
		// Its tags first, then its references and escapes, as a body's line
		// reads (notionMarkdownVersion2Input).
		line := notionMarkdownVersion2Entities(mcpPropertyKeptTags(lines[index], sitePages))
		// A line's break marker goes with the break it makes.
		if index < len(lines)-1 {
			line = notionMarkdownVersion2Break(line)
		}
		lines[index] = strings.ReplaceAll(line, notionMarkdownEntitySpace, "")
	}
	// Its <br> as a block's text reads them, one rule for a page's body, a
	// value and a suggestion's words.
	return notionMarkdownSiteInlineSpans(notionMarkdownLineBreaks(strings.Join(lines, "\n"), false), false, sitePages)
}

// mcpPropertyKeptTags escapes the tags a value's text keeps as text, outside
// its code, and writes an HTML link as Markdown's, as Notion reads them.
func mcpPropertyKeptTags(line string, site *notionMarkdownSitePages) string {
	var out strings.Builder
	at := 0
	for _, code := range notionMarkdownCodeSpan.FindAllStringIndex(line, -1) {
		out.WriteString(mcpPropertyTagsAsText(line[at:code[0]], site))
		out.WriteString(line[code[0]:code[1]])
		at = code[1]
	}
	out.WriteString(mcpPropertyTagsAsText(line[at:], site))
	return out.String()
}

func mcpPropertyTagsAsText(text string, site *notionMarkdownSitePages) string {
	text = notionMarkdownHTMLLink.ReplaceAllStringFunc(text, func(link string) string {
		match := notionMarkdownHTMLLink.FindStringSubmatch(link)
		if href := notionMarkdownAttributes(match[1])["href"]; href != "" {
			return "[" + match[2] + "](" + notionMarkdownVersion2HrefWritten(href) + ")"
		}
		return link
	})
	var out strings.Builder
	at := 0
	// What the reading leaves out goes unsaid: Notion's answer to a value has
	// no warnings (seen 2026-11-11).
	unsaid, unpaired := notionMarkdownRepair{}, false
	for _, found := range notionMarkdownRepairTag.FindAllStringSubmatchIndex(text, -1) {
		out.WriteString(text[at:found[0]])
		tag, name := text[found[0]:found[1]], text[found[4]:found[5]]
		at = found[1]
		switch {
		case notionMarkdownEscapedAt(text, found[0]):
		case !notionMarkdownVersion2InlineTags[name]:
			tag = `\` + tag
		case name == "mention" && found[3] == found[2] && notionMarkdownValueMentionsNothing(notionMarkdownAttributes(text[found[6]:found[7]]), site):
			// One mentioning nothing is words (notionMarkdownValueMentionsNothing).
			if found[9] > found[8] || !strings.Contains(text[found[1]:], "</mention>") {
				tag = `\` + tag
			} else {
				tag, unpaired = "", true
			}
		case unpaired && name == "mention" && found[3] > found[2]:
			tag, unpaired = "", false
		case found[3] == found[2] && found[7] > found[6]:
			// Its attributes as a body's tag keeps them: the first of two,
			// and only those it takes, a color it knows.
			if kept, ok := unsaid.attributes(name, text[found[6]:found[7]]); ok {
				tag = "<" + name + kept + text[found[7]:found[1]]
			}
		}
		out.WriteString(tag)
	}
	out.WriteString(text[at:])
	return out.String()
}

// mcpPropertyMarkdown writes a value, its words and their spans, as the
// paragraph of "v2" page Markdown Notion's MCP gives it as; "" for none.
func mcpPropertyMarkdown(words string, spans []graphTextSpan, mentions notionMarkdownMentions) string {
	if words == "" && len(spans) == 0 {
		return ""
	}
	block := workspaceExportMarkdownBlock{
		ID: "value", Type: graphBlockTypeParagraph, Content: map[string]any{"text": words}, Spans: spans,
	}
	if len(spans) > 0 {
		if _, written := notionMarkdownText(spans, mentions); !written {
			block.Spans = nil
		}
	}
	markdown := renderNotionPageMarkdown(
		[]workspaceExportMarkdownBlock{block}, "", false, func(string) string { return "" }, nil, mentions, false,
	).Markdown
	// A value is a property's line, not a block: spaces at its ends stand
	// as they are, without the paragraph tag "v2" keeps a block's in (a
	// title "a " is "a ", not "<p>⏎a ⏎</p>"), and so does a value of
	// whitespace alone, which "v2" writes as "<p> </p>" on one line: a
	// value's reading keeps that tag as words, and reads " " as " "
	// (mcpPropertyMarkdownSpans), so what fetch gives writes back as the
	// same value; its line breaks are <br /> as any value's.
	line := strings.TrimRight(markdown, "\n")
	if inner, held := strings.CutPrefix(line, "<p>"); held {
		if inner, held = strings.CutSuffix(inner, "</p>"); held && notionMarkdownWhitespaceText(inner) {
			return strings.ReplaceAll(inner, "\n", "<br />")
		}
	}
	if inner, held := strings.CutPrefix(line, "<p>\n"); held {
		if inner, held = strings.CutSuffix(inner, "\n</p>"); held && !strings.Contains(inner, "\n") && notionMarkdownEdgeSpace(inner) {
			return inner
		}
	}
	return markdown
}

// mcpPropertyMentions names the mentions of values as the connection's
// fetch names a page's, in the workspace they are in.
func mcpPropertyMentions(app core.App, authority mcpAuthority, workspaceID string) notionMarkdownMentions {
	var failed error
	return mcpMarkdownReader(authority).mentions(app, workspaceID, &failed)
}

// mcpPropertyRichText is a value read from Markdown, its mentions prepared
// as a page's Markdown's are (narrowedMarkdownMentionInput), as the Public
// API's rich text a row page's write takes: its mentions name what they
// mention by the API's IDs, which the write checks again.
func mcpPropertyRichText(
	app core.App, authority mcpAuthority, current pageAuthorization, spans []graphTextSpan,
) ([]map[string]any, error) {
	prepared, err := mcpPreparedPropertySpans(app, authority, current, spans)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(prepared))
	for _, span := range prepared {
		item := map[string]any{"annotations": map[string]any{
			"bold": span.Bold, "italic": span.Italic, "strikethrough": span.Strikethrough,
			"underline": span.Underline, "code": span.Code, "color": notionSpanColor(span.Color, span.Background),
		}}
		mention, err := mcpPropertyMention(app, current.Workspace.WorkspaceID, span)
		if err != nil {
			return nil, err
		}
		switch {
		case span.Equation != "":
			item["type"], item["equation"] = "equation", map[string]any{"expression": span.Equation}
		case mention != nil:
			item["type"], item["mention"] = "mention", mention
		default:
			text := map[string]any{"content": span.Text}
			if span.Link != "" {
				text["link"] = map[string]any{"url": mcpPropertyLinkURL(app, current.Workspace.WorkspaceID, span.Link)}
			}
			item["type"], item["text"] = "text", text
		}
		items = append(items, item)
	}
	return items, nil
}

// mcpValueSitePages is the MCP's reader of a title, a value or a suggestion's
// words written on current, which asks it which data sources their
// mentions name (DataSourceNamed): one it reaches, by its own ID or the
// public API's, as its write names it (principalDataSourceNamed). A writer
// with no address of its own — an app's data session, whose resource is
// "leafen-app" — names data sources by the same rule, and reads no address
// as this server's, as it read none before (2026-11-22: its site was none,
// and setting the rule on it stopped every app making its data's pages).
func mcpValueSitePages(app core.App, authority mcpAuthority, current pageAuthorization) *notionMarkdownSitePages {
	site := mcpSitePages(authority)
	if site == nil {
		site = &notionMarkdownSitePages{InternalIDs: true}
	}
	reaches := mcpMentionReaches(authority)
	allows := func(target pageAuthorization) bool { return reaches == nil || reaches(app, target) }
	site.DataSourceNamed = func(id string) bool {
		_, named, err := principalDataSourceNamed(app, authority.PrincipalID, current, id, allows)
		return err == nil && named
	}
	return site
}

// mcpPreparedPropertySpans are a value's spans with their mentions named as
// the connection's Markdown names a page's: a page, a database or a person
// it may name, a :name: the workspace's custom emoji.
func mcpPreparedPropertySpans(
	app core.App, authority mcpAuthority, current pageAuthorization, spans []graphTextSpan,
) ([]graphTextSpan, error) {
	input, err := narrowedMarkdownMentionInput(authority.PrincipalID, mcpMentionReaches(authority))(
		app, current, nil, graphMutationInput{TextUpdates: []graphTextUpdate{{Spans: spans}}},
	)
	if err != nil {
		return nil, err
	}
	return input.TextUpdates[0].Spans, nil
}

// mcpPropertyMention is a prepared mention as the Public API's request names
// it, nil for a span that is no mention the API's rich text takes.
func mcpPropertyMention(app core.App, workspaceID string, span graphTextSpan) (map[string]any, error) {
	switch span.Mention {
	case "page":
		if span.PageID == "" {
			return nil, nil
		}
		// A block's mention names the block where its page's ID stands, which
		// the write anchors at the block again (page_mention_block_anchor.go).
		id, err := notionPagePublicID(app, span.PageID)
		if span.BlockID != "" {
			id, err = notionBlockPublicID(app, workspaceID, span.BlockID)
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "page", "page": map[string]any{"id": id}}, nil
	case "database":
		if span.DatabaseID == "" {
			return nil, nil
		}
		// A data source's kept by the ID written (notion_markdown_kept_data_sources.go)
		// goes by an identity of its address, which the write's database mention
		// names it by again (notionMentionPreparation.databaseContainerID).
		id, err := ensureNotionObjectIdentity(app, notionObjectIdentityDatabase, workspaceID, span.DatabaseID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "database", "database": map[string]any{"id": id}}, nil
	case "person":
		id, err := notionCompatibleUserID(span.UserID)
		// One kept by an ID no person here has goes by that ID, which the
		// write keeps (notionTextRequestSpans).
		if kept, isUUID := normalizeNotionUUIDRequest(span.UserID); err != nil && isUUID {
			id, err = kept, nil
		}
		if span.UserID == "" || err != nil {
			return nil, nil
		}
		return map[string]any{"type": "user", "user": map[string]any{"id": id}}, nil
	case "date":
		if span.Date == "" {
			return nil, nil
		}
		date := map[string]any{"start": span.Date}
		if span.DateEnd != "" {
			date["end"] = span.DateEnd
		}
		if span.DateTimeZone != "" {
			date["time_zone"] = span.DateTimeZone
		}
		return map[string]any{"type": "date", "date": date}, nil
	case "custom_emoji":
		if span.CustomEmojiID == "" {
			return nil, nil
		}
		return map[string]any{"type": "custom_emoji", "custom_emoji": map[string]any{"id": span.CustomEmojiID}}, nil
	}
	return nil, nil
}

// mcpPropertyLinkURL is a value's link as the Public API's request it
// becomes takes it: the app's link to a page here, as a link to Notion's
// web address of one is kept (notion_address_links_here.go), in Notion's
// form for it (/ID, /ID#block; notionAPIAppPageLink), which the write keeps
// as the app's link again (notionTextLinkRequest); a data source's or a
// view's address by an ID of no UUID's form where it points, as a page's
// write points one (notionOwnSourceLinkHere); any other as it is.
func mcpPropertyLinkURL(app core.App, workspaceID, link string) string {
	if pointed, own, err := notionOwnSourceLinkHere(app, workspaceID, link); own && err == nil {
		link = pointed
	}
	if strings.HasPrefix(link, "/p/") {
		if form, ok := notionAPIAppPageLink(app, workspaceID, link); ok {
			return form
		}
	}
	return link
}
