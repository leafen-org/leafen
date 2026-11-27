package app

import (
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A page's Markdown writes each block's text as Notion's page Markdown does
// now (seen 2026-10-29): <b>, <i>, <s>, <u>, <code>, [links](…), $equations$
// and <span color="…">, with the characters that mean something in it
// escaped, mentions of pages, databases and people as <mention url="…" />
// naming their targets as the reader names them, dates as <date … />,
// custom emoji as :name:, lowercase, and a link mention as a link with its
// title, as Notion writes one; a changed line makes it a link. A template's
// mention writes nothing, as in Notion. A block mentioning something the
// reader may not open, a custom emoji whose name Markdown can't write, or a
// mention Markdown doesn't write, is an unknown block; a custom emoji the
// workspace hasn't is Notion's <unknown_mention> tag
// (notion_missing_custom_emoji.go). A line break inside a block is <br />,
// and a code block keeps its text as it is under its language.

// notionMarkdownMentions names the targets of mentions as the page's reader
// names them, reporting false for one the reader may not open.
type notionMarkdownMentions struct {
	Page func(pageID string) (string, bool)
	// TextLink is the address a text link takes for the reader (notionMarkdownReader.textLink), "" to keep only
	// its words, false for a link it writes as kept; PageID is the page read, whose block #block-ID names.
	TextLink func(link, pageID string) (string, bool)
	PageID   string
	Database func(databaseID string) (string, bool)
	// DataSource is the ID a data source's mention reads by, of the ID it keeps (data_source_mentions.go).
	DataSource func(dataSourceID string) string
	// Person is a mentioned person, by their ID and the mention's text, as
	// the reader names them: their address, their name now ("" for none or
	// one the reader may not read), and false for one Markdown can't write;
	// a connection's or a token's bot reads by its ID and its name
	// (notion_markdown_person_mentions.go).
	Person func(userID, text string) (address, name string, ok bool)
	// UserName is a mentioned person's name now, "" to keep the name the
	// mention was written with (person_mention_names.go).
	UserName func(userID string) string
	// CustomEmoji is a custom emoji's name, false for one deleted.
	CustomEmoji func(emojiID string) (string, bool)
	// MissingCustomEmoji is the address a custom emoji the workspace hasn't
	// reads by, "" to write the text it shows.
	MissingCustomEmoji func(emojiID string) string
	// Origin is where the reader's addresses begin.
	Origin string
	// APIForm writes text and tags as the public API's Markdown does, and
	// Version2 as its "v2" does.
	APIForm  bool
	Version2 bool
	// Discussions names the discussions a run of words in TextBlock's text
	// was left on, by the threads its text marks it with (IDs, space-separated),
	// for its Markdown to write around it (notionMarkdownDiscussed); nil names none.
	Discussions func(threads, blockID string) string
	TextBlock   string
	// PageTitle is a mentioned page's title now, which its mention reads
	// with; PageMissing is whether no page answers to its ID (titled).
	PageTitle   func(pageID string) string
	PageMissing func(pageID string) bool
	// Block is a page mention's block on that page, at the page's address
	// for the reader: the address the mention reads by and the block's
	// words now; false for a block no longer there, whose address names it
	// alone (page_mention_block_anchor.go).
	Block func(pageAddress, pageID, blockID string) (address, words string, there bool)
	// DatabaseTitle is a mentioned database's name now, "" for one with none.
	DatabaseTitle func(databaseID string) string
	// Unnamed is the address a page or database mention its reader can't
	// open reads by, with no title, as one nothing answers to does and as
	// Notion's Markdown gives either; nil leaves its line unknown.
	Unnamed func(mention, targetID string) (string, bool)
	// Targets is what a page's or a database's mention reads of what it
	// names: whether one is in the trash itself, which "v2" writes by its
	// address alone (notion_markdown_mention_targets.go); zero names none so.
	Targets notionMarkdownMentionTargets
	// SyncedPage is the page a synced block stands on, as the reader names it, and SyncedBlocks what it holds, for a
	// reference to it, and whether it holds more than the Markdown's records take; SyncedPageBody reads a page for
	// a reference to its body (notion_page_markdown_synced_page.go).
	SyncedPage     func(blockID string) string
	SyncedBlocks   func(blockID string) ([]workspaceExportMarkdownBlock, bool)
	SyncedPageBody func(pageID string) notionMarkdownSyncedPage
	// FileAddress is the address a stored file takes for the reader, with the block that holds it.
	FileAddress func(blockID, fileID, name string) string
}

// notionMarkdownListStart is text that would start a numbered item: digits,
// a full stop and a space or nothing after them. "1.a" stays as it is, as in
// Notion (seen 2026-11-11).
var notionMarkdownListStart = regexp.MustCompile(`^(\d+)\.(?:[ \t]|$)`)

func notionPageMarkdownPlainText(block workspaceExportMarkdownBlock) string {
	plain := workspaceExportRichText(block.Content["rich"])
	if plain == "" {
		plain, _ = block.Content["text"].(string)
	}
	return plain
}

// notionPageMarkdownText writes a block's own text, or reports false for text Markdown doesn't write.
func notionPageMarkdownText(block workspaceExportMarkdownBlock, mentions notionMarkdownMentions) (string, bool) {
	if len(block.Spans) > 0 {
		return notionMarkdownText(block.Spans, mentions.at(block.ID))
	}
	if mentions.Version2 {
		return notionMarkdownVersion2EscapedText(notionPageMarkdownPlainText(block)), true
	}
	plain := notionPageMarkdownPlainText(block)
	if mentions.APIForm {
		return notionMarkdownVersion1Underscores(
			notionMarkdownEscapedText(plain), notionMarkdownEscapedTextWith(plain, notionMarkdownEscaped),
		), true
	}
	return notionMarkdownEscapedText(plain), true
}

// notionMarkdownHeadingStart is text that would start a heading: # marks, a
// space and words. "#tag", "#" alone and "# " stay as they are, as in Notion,
// whose "v1" reads them back as text (seen 2026-11-10 and 2026-11-11).
var notionMarkdownHeadingStart = regexp.MustCompile(`^#+[ \t]+\S`)

// notionMarkdownBulletStart is text that would start a bulleted item or a
// divider: - or + alone or before a space, or dashes alone, which Notion's
// "v1" reads as a divider from two (seen 2026-11-11: "--" is one; "-a",
// "+a", "-- a" and "---a" are text, which Notion writes bare).
var notionMarkdownBulletStart = regexp.MustCompile(`^(?:[-+](?:[ \t]|$)|-+[ \t]*$)`)

// notionMarkdownParagraph keeps a paragraph that starts like a heading, a
// list or a divider a paragraph.
func notionMarkdownParagraph(text string) string {
	switch {
	case notionMarkdownHeadingStart.MatchString(text), notionMarkdownBulletStart.MatchString(text):
		return `\` + text
	case notionMarkdownListStart.MatchString(text):
		index := strings.IndexByte(text, '.')
		return text[:index] + `\` + text[index:]
	}
	return text
}

// notionMarkdownText writes formatted text as Notion's enhanced Markdown, or
// reports false when it holds a mention the Markdown can't write. A
// mention's tag stands in its marks — bold, italic, strikethrough, underline
// and color, not code — as text does, as Notion's Markdown writes it (seen
// 2026-11-10); "v1" carries marks across pieces
// (notion_page_markdown_version1_marks.go).
func notionMarkdownText(spans []graphTextSpan, mentions notionMarkdownMentions) (string, bool) {
	spans = notionMarkdownJoinedSpans(spans)
	if mentions.APIForm {
		return notionMarkdownVersion1MarkedText(spans, mentions)
	}
	var result strings.Builder
	for _, span := range spans {
		if span.Mention != "" {
			tag, text, written := notionMarkdownMentionTag(span, mentions)
			if !written {
				return "", false
			}
			if text == nil {
				if tag != "" && mentions.Version2 {
					tag = notionMarkdownVersion2Marked(notionMarkdownDiscussed(tag, mentions.discussionsOf(span)), span)
				} else if tag != "" {
					tag = notionMarkdownMarked(tag, span)
				}
				result.WriteString(tag)
				continue
			}
			result.WriteString(notionMarkdownFlavoredSpanText(*text, mentions))
			continue
		}
		written := notionMarkdownFlavoredSpanText(notionMarkdownReaderLink(span, mentions), mentions)
		// A "!" a link follows is escaped in "v2", which would read an
		// image there (seen 2026-11-11: "\![a](…)").
		if mentions.Version2 && strings.HasPrefix(written, "[") {
			if before := result.String(); strings.HasSuffix(before, "!") && !notionMarkdownEscapedAt(before, len(before)-1) {
				result.Reset()
				result.WriteString(before[:len(before)-1] + `\!`)
			}
		}
		result.WriteString(written)
	}
	return result.String(), true
}

// notionMarkdownJoinedSpans are a text's spans as Notion's Markdown writes
// them (seen 2026-11-11, worker w255): neighbours alike in all but their
// words are one, and a span of no words that is no mention or equation goes
// ("<i>a </i><i> b</i>" is "<i>a  b</i>", "[a](u)[b](u)" is "[ab](u)").
func notionMarkdownJoinedSpans(spans []graphTextSpan) []graphTextSpan {
	joined := make([]graphTextSpan, 0, len(spans))
	for _, span := range spans {
		plain := span.Mention == "" && !graphTextSpanIsEquation(span)
		if plain && span.Text == "" {
			continue
		}
		if last := len(joined) - 1; plain && last >= 0 && joined[last].Mention == "" && !graphTextSpanIsEquation(joined[last]) {
			alike := joined[last]
			alike.Text = span.Text
			if alike == span {
				joined[last].Text += span.Text
				continue
			}
		}
		joined = append(joined, span)
	}
	return joined
}

// notionMarkdownReaderLink points a span's link to one of the workspace's
// pages, or to a block, at the address it takes for the reader.
func notionMarkdownReaderLink(span graphTextSpan, mentions notionMarkdownMentions) graphTextSpan {
	if mentions.TextLink == nil || span.Link == "" {
		return span
	}
	if address, mapped := mentions.TextLink(span.Link, mentions.PageID); mapped {
		span.Link = address
	}
	return span
}

// textLink is the address a text link to one of the workspace's pages, or
// to a block on one or on the page read, takes for the reader, as
// notionMarkdownMentions.TextLink: through the public API, the address its
// JSON reads (notionAPITextLink: /ID, /ID#block), as Notion's page Markdown
// writes a link as its JSON reads it (seen 2026-11-11), and an anchor Notion
// takes (#block) as written; by Leafen's IDs, this server's /p/ID and the
// app's anchor; to anyone else, the page's address here with Notion's anchor,
// as the page's block mentions read. A page the reader may not open keeps
// only its words, and so does an anchor in the page alone that names nothing
// to follow there but through the API.
func (reader notionMarkdownReader) textLink(
	app core.App, workspaceID string, readable func(string) bool,
) func(link, pageID string) (string, bool) {
	api := !reader.InternalIDs && (reader.APIForm || reader.Version2)
	return func(link, pageID string) (string, bool) {
		// One a write kept at the app's address reads with the server's
		// address before it, to every reader (notionAppAddressLink).
		if notionAppAddressLink(link) {
			return reader.Origin + link, true
		}
		address := strings.TrimSpace(link)
		if api && reader.Origin != "" && notionAppPathLink(link) {
			if _, page := notionAPIAppLink(app, workspaceID, pageID, link); !page || address != link {
				// A path in the app that names no page reads with the
				// server's address before it, as its JSON does
				// (notionAPITextLink), and as written after it.
				return reader.Origin + link, true
			}
		}
		// One written with whitespace about it names no page: a "v1" write
		// kept it as written (seen 2026-11-11: "[a]( /p/x)" and "[a]( #x)"
		// read back so).
		if address != link {
			return "", false
		}
		if strings.HasPrefix(address, "#block-") && pageID != "" {
			address = "/p/" + pageID + address
		}
		id := notionMarkdownPageLinkID(address)
		switch {
		case id == "" && strings.HasPrefix(address, "#") && api:
			return address, true
		case id == "" && strings.HasPrefix(address, "#"):
			return "", true
		case id == "":
			return "", false
		case reader.InternalIDs:
			return "/p/" + id + notionMarkdownLinkAnchor(address), true
		case api:
			// The public API's Markdown reads a link as its JSON does, one to
			// a page the reader can't open or that isn't there as written
			// (seen 2026-11-11: Notion's "v1" and "v2" keep "[there](/<id>)").
			return notionAPITextLink(app, workspaceID, pageID, link, reader.Origin), true
		}
		if _, err := notionPagePublicID(app, id); err == nil && !readable(id) {
			return "", true
		}
		read, ok := notionAPIAppPageLink(app, workspaceID, "/p/"+id+notionMarkdownLinkAnchor(address))
		if !ok {
			return read, true
		}
		publicID, err := notionPagePublicID(app, id)
		if err != nil {
			return "", true
		}
		if _, anchor, anchored := strings.Cut(read, "#"); anchored {
			return notionPageURL(reader.Origin, publicID) + "#" + anchor, true
		}
		return notionPageURL(reader.Origin, publicID), true
	}
}

// notionMarkdownMentionTag writes a mention as its <mention> or <date> tag,
// without its marks, "" for one Markdown leaves out; a mention written as
// text — a link mention, a deleted custom emoji — is the text span it
// writes as instead.
func notionMarkdownMentionTag(span graphTextSpan, mentions notionMarkdownMentions) (string, *graphTextSpan, bool) {
	switch span.Mention {
	case "custom_emoji":
		// One deleted is Notion's <unknown_mention> tag for a workspace that
		// hasn't it, or the text it shows.
		name, found := mentions.CustomEmoji(span.CustomEmojiID)
		if !found && mentions.MissingCustomEmoji != nil {
			if address := mentions.MissingCustomEmoji(span.CustomEmojiID); address != "" {
				return notionMarkdownMissingCustomEmojiTag(address, mentions.APIForm), nil, true
			}
		}
		if !found {
			plain := span
			plain.Mention, plain.CustomEmojiID = "", ""
			return "", &plain, true
		}
		name = strings.ToLower(name)
		return ":" + name + ":", nil, notionMarkdownEmojiName(name)
	case "external":
		link := span
		link.Mention, link.IconURL = "", ""
		if strings.TrimSpace(link.Text) == "" {
			link.Text = span.Link
		}
		return "", &link, notionMarkdownLinkTarget(span.Link) != ""
	}
	tag, ok := notionMarkdownKeptDataSourceTag(span, mentions)
	return tag, nil, ok
}

// notionMarkdownMentionText writes the tag of a mention of a page, a
// database, a person or a date.
func notionMarkdownMentionText(span graphTextSpan, mentions notionMarkdownMentions) (string, bool) {
	if mentions.APIForm {
		if text, ok, handled := notionMarkdownAPIMentionText(span, mentions); handled {
			return text, ok
		}
	}
	if mentions.Version2 && span.Mention == "database" {
		return notionMarkdownVersion2DatabaseMention(span, mentions)
	}
	address := notionMarkdownAttributeEscaped.Replace
	switch span.Mention {
	case "page":
		// It reads with the page's title now, whatever it was written with
		// (seen 2026-11-04), or a block's words now (seen 2026-11-11), which
		// "v2" writes as they are (seen 2026-11-11: "a*b* <c> $e$ ^f").
		words := notionMarkdownEscaped.Replace
		if mentions.Version2 {
			words = func(text string) string { return text }
		}
		target, ok := mentions.Page(span.PageID)
		if ok && span.BlockID != "" && mentions.Block != nil {
			at, said, there := mentions.Block(target, span.PageID, span.BlockID)
			if !there {
				return `<mention url="` + address(at) + `" />`, true
			}
			return `<mention url="` + address(at) + `">` + words(said) + `</mention>`, true
		}
		if ok && mentions.titled(span) {
			return `<mention url="` + address(target) + `">` + words(mentions.PageTitle(span.PageID)) + `</mention>`, ok
		}
		if !ok && mentions.Unnamed != nil {
			target, ok = mentions.Unnamed(span.Mention, span.PageID)
		}
		return `<mention url="` + address(target) + `" />`, ok
	case "database":
		target, ok := mentions.Database(span.DatabaseID)
		if !ok && mentions.Unnamed != nil {
			target, ok = mentions.Unnamed(span.Mention, span.DatabaseID)
		}
		return `<mention url="` + address(target) + `" />`, ok
	case "person":
		// "v2" names the person by their name now, whatever the mention was
		// written with (seen 2026-11-11), as it does a page by its title.
		target, name, ok := mentions.Person(span.UserID, span.Text)
		if ok && target == "" {
			return "", true
		}
		if ok && name != "" && mentions.Version2 {
			return `<mention url="` + address(target) + `">` + name + `</mention>`, true
		}
		return `<mention url="` + address(target) + `" />`, ok
	case "date":
		return notionMarkdownDateTag(span, false), true
	case graphTemplateMention:
		// A template's "when duplicated" mention, which Notion's Markdown
		// leaves out; a line left as it was keeps it.
		return "", true
	default:
		return "", false
	}
}

// notionMarkdownLinkTarget is the address page Markdown writes for a link: a
// web, mail or phone address as it is, and a link to one of the workspace's
// pages by the page address fetch writes (/pages/ID). One a "v1" write kept
// as written (notionMarkdownKeptLink) is written as it is, whitespace and
// all, as Notion's writes it back in each form (seen 2026-11-11: "v1"'s
// "[a](  example.com)", "v2"'s and the MCP's percent-encoded).
func notionMarkdownLinkTarget(link string) string {
	if notionMarkdownKeptLink(link) {
		return link
	}
	if target := workspaceImportLinkTarget(link); target != "" {
		return target
	}
	if id := notionMarkdownPageLinkID(link); id != "" {
		return "/pages/" + id + notionMarkdownLinkAnchor(link)
	}
	// Any other link the text keeps — an address written without its scheme,
	// as the public API takes it, or an anchor its reader keeps
	// (notionMarkdownReader.textLink) — is written as kept, as Notion's page
	// Markdown writes it in both versions (worker w220, 2026-11-10).
	if kept := strings.TrimSpace(link); validGraphTextLink(kept) {
		return kept
	}
	return ""
}

// notionMarkdownKeptLink reports a link a "v1" write kept as written
// (graphKeptTextLink), or a JSON write took so (graphRequestTextLink:
// "javascript:a b.com", "//a|b.com" read back so in Notion's "v1", seen
// 2026-11-11), that reads otherwise trimmed: one with whitespace about it,
// or one that is no web, mail or phone address, page or anchor.
func notionMarkdownKeptLink(link string) bool {
	if !graphKeptTextLink(link) && !graphRequestTextLink(link) {
		return false
	}
	return link != strings.TrimSpace(link) ||
		workspaceImportLinkTarget(link) == "" && notionMarkdownPageLinkID(link) == "" && !validGraphTextLink(link)
}

// notionMarkdownBlankText is whether a paragraph's text is spaces alone, or
// an empty piece of text alone, which Notion's Markdown reads as no block
// as it does spaces (seen 2026-11-11: rich text [""] reads "").
func notionMarkdownBlankText(text string, spans []graphTextSpan) bool {
	if strings.TrimSpace(text) != "" {
		return false
	}
	for _, span := range spans {
		if span.EmptyPiece == graphTextEmptyText {
			return true
		}
	}
	return text != ""
}

func holdsTemplateMention(spans []graphTextSpan) bool {
	for _, span := range spans {
		if span.Mention == graphTemplateMention {
			return true
		}
	}
	return false
}

// notionMarkdownSpanText writes a span as Leafen's page Markdown does (a
// reader neither "v1" nor "v2", notionMarkdownReader); the public API's
// asks notionMarkdownAPISpanText, and "v2"'s notionMarkdownVersion2SpanText.
func notionMarkdownSpanText(span graphTextSpan) string {
	var body string
	switch {
	case graphTextSpanIsEquation(span):
		body = "$" + notionMarkdownEquationEscaped(span.Equation) + "$"
	case span.Code && !strings.Contains(span.Text, "</code>"):
		// Code is its text as written, as Notion writes it.
		body = "<code>" + span.Text + "</code>"
	default:
		body = notionMarkdownEscapedText(span.Text)
	}
	// Emphasis wraps the text between its edge spaces, which stay outside.
	lead, trail := "", ""
	if !graphTextSpanIsEquation(span) && !span.Code {
		trimmed := strings.TrimLeft(body, " ")
		lead, body = body[:len(body)-len(trimmed)], trimmed
		trimmed = strings.TrimRight(body, " ")
		trail, body = body[len(trimmed):], trimmed
	}
	if body == "" {
		return lead + trail
	}
	body = notionMarkdownMarked(body, span)
	if target := notionMarkdownLinkTarget(span.Link); target != "" && span.Mention == "" && !graphTextSpanIsEquation(span) {
		destination := strings.NewReplacer("(", "%28", ")", "%29", " ", "%20").Replace(target)
		if notionMarkdownKeptLink(span.Link) {
			// Percent-encoded, as Notion's "v2" and its MCP write one (seen
			// 2026-11-11: "[a](%3Ca%20b.com%3E)" for "<a b.com>"); the MCP
			// reads by "v2" (notionMarkdownVersion2SpanText).
			destination = notionMarkdownVersion2Destination(target)
		}
		body = "[" + body + "](" + destination + ")"
	}
	return lead + body + trail
}

// notionMarkdownMarked puts a span's marks around what it writes, text or a
// mention's tag, as Leafen's page Markdown does: <span color>, <u>, <s>,
// <b>, <i> from the outside in.
func notionMarkdownMarked(body string, span graphTextSpan) string {
	if span.Italic {
		body = "<i>" + body + "</i>"
	}
	if span.Bold {
		body = "<b>" + body + "</b>"
	}
	if span.Strikethrough {
		body = "<s>" + body + "</s>"
	}
	if span.Underline {
		body = "<u>" + body + "</u>"
	}
	if color := notionMarkdownShownColor(span); color != "" {
		body = `<span color="` + color + `">` + body + "</span>"
	}
	return body
}
