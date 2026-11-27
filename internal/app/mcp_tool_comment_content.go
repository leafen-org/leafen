package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

const (
	mcpCommentMaximumRichTextItems = 100
	mcpCommentMaximumTextRunes     = 32_768
)

type mcpCommentSpan struct {
	Text          string `json:"text"`
	Bold          bool   `json:"bold,omitempty"`
	Italic        bool   `json:"italic,omitempty"`
	Underline     bool   `json:"underline,omitempty"`
	Strikethrough bool   `json:"strikethrough,omitzero"`
	Code          bool   `json:"code,omitempty"`
	Color         string `json:"color,omitempty"`
	Link          string `json:"link,omitempty"`
	Mention       string `json:"mention,omitempty"`
	PageID        string `json:"pageId,omitempty"`
	BlockID       string `json:"blockId,omitempty"`
	DatabaseID    string `json:"databaseId,omitempty"`
	CustomEmojiID string `json:"customEmojiId,omitempty"`
	Date          string `json:"date,omitempty"`
	DateEnd       string `json:"dateEnd,omitempty"`
	DateTimeZone  string `json:"dateTimeZone,omitempty"`
	UserID        string `json:"userId,omitempty"`
	Equation      string `json:"equation,omitempty"`
}

type mcpCommentBodyValue struct {
	Type          string           `json:"type"`
	Rich          []mcpCommentSpan `json:"rich"`
	AttachmentIDs []string         `json:"attachmentIds,omitempty"`
	Quote         string           `json:"quote,omitempty"`
	// QuoteStart and QuoteEnd name the text a thread was left on, as the
	// screen's do, so the text takes its mark.
	QuoteStart *int   `json:"quoteStart,omitempty"`
	QuoteEnd   *int   `json:"quoteEnd,omitempty"`
	QuoteField string `json:"quoteField,omitempty"`
}

func mcpCommentBody(
	app core.App,
	authority mcpAuthority,
	current pageAuthorization,
	markdown *string,
	richText []map[string]any,
	quote string,
	quoteRange *mcpCommentQuoteRange,
	said *mcpMarkdownSaid,
) (json.RawMessage, error) {
	if (markdown == nil) == (richText == nil) {
		return nil, mcpInvalid("a comment is given one way: markdown, or rich_text")
	}
	spans := []mcpCommentSpan{}
	attachments := []string(nil)
	var err error
	if markdown != nil {
		// A line attaching a file leaves the text (mcp_tool_comment_attachments.go);
		// a comment of files alone has no words.
		text := *markdown
		text, attachments, err = mcpCommentAttachments(app, authority, current, text)
		if err == nil && (len(attachments) == 0 || strings.TrimSpace(text) != "") {
			spans, err = mcpCommentMarkdownSpans(app, authority, current, text, said)
		}
	} else {
		spans, err = mcpCommentRichTextSpans(app, authority, current, richText)
	}
	// A refused piece says why (a mention out of reach, an empty item, the
	// item count), as Notion's MCP names the user it couldn't find.
	if err != nil {
		return nil, err
	}
	if len(spans) == 0 && len(attachments) == 0 || len(spans) > mcpCommentMaximumRichTextItems {
		return nil, mcpInvalid("the comment has no text, or more than %d pieces of rich text", mcpCommentMaximumRichTextItems)
	}
	textRunes := 0
	for _, span := range spans {
		textRunes += utf8.RuneCountInString(span.Text)
	}
	if textRunes == 0 && len(attachments) == 0 || textRunes > mcpCommentMaximumTextRunes {
		return nil, mcpInvalid("a comment has from 1 to %d characters", mcpCommentMaximumTextRunes)
	}
	value := mcpCommentBodyValue{Type: "rich_text", Rich: spans, AttachmentIDs: attachments, Quote: quote}
	if quoteRange != nil {
		value.QuoteStart, value.QuoteEnd, value.QuoteField = &quoteRange.start, &quoteRange.end, quoteRange.field
	}
	body, err := json.Marshal(value)
	if err != nil || !validCommentBody(body) {
		return nil, mcpInvalid("the comment's text didn't make a comment: give it words")
	}
	return body, nil
}

// mcpCommentMarkdownSpans reads a comment written as inline Markdown as
// Notion's MCP reads one (seen 2026-11-11): as a page's "v2" Markdown reads
// a value (mcp_property_markdown.go) — its marks, links, colors, math,
// mentions and dates, escapes, a line break as one, and what isn't inline
// Markdown, a heading's or a list's marks, as text — its tags first as an
// MCP write's (mcpInlineMarkdownTags: Notion's older ones read as a page's
// body reads them, told in said), but its addresses as Notion's public API
// reads a "v2" comment's
// (notionCommentMarkdownSite): one standing bare as text, one between angle
// brackets a link to it as written (seen 2026-11-21 and 2026-11-22), its
// <mention> tags refused as a "v2" comment's are (notionCommentMarkdownRead).
// Its mentions name what they mention as a page's do, by the tools' IDs or
// the public API's.
func mcpCommentMarkdownSpans(
	app core.App, authority mcpAuthority, current pageAuthorization, markdown string, said *mcpMarkdownSaid,
) ([]mcpCommentSpan, error) {
	if utf8.RuneCountInString(markdown) > mcpCommentMaximumTextRunes {
		return nil, mcpInvalid("a comment has at most %d characters", mcpCommentMaximumTextRunes)
	}
	site := notionCommentMarkdownSite(mcpSitePages(authority), notionMarkdownVersion2)
	markdown, tagged := mcpInlineMarkdownTags(markdown)
	words, read := notionCommentMarkdownRead(notionCommentMarkdown{Text: markdown, Version: notionMarkdownVersion2, Site: site})
	// A piece its reading refuses refuses the comment in Notion's words, as
	// a public API comment's does (notionCommentMarkdownSpan).
	if refused := slices.IndexFunc(read, func(span graphTextSpan) bool { return span.Mention == notionMarkdownRefusedMention }); refused >= 0 {
		return nil, mcpInvalid("%s", read[refused].Text)
	}
	if strings.TrimSpace(words) == "" {
		return nil, mcpInvalid("the comment needs words, in at most %d pieces of rich text", mcpCommentMaximumRichTextItems)
	}
	if read == nil {
		read = []graphTextSpan{{Text: words}}
	}
	prepared, err := mcpPreparedPropertySpans(app, authority, current, mcpCommentLocalMentions(app, read))
	if refusal := (&graphRequestError{}); errors.As(err, &refusal) && refusal.status == http.StatusBadRequest {
		// A mention its write refuses refuses the comment in those words
		// (keepOrRefuseUnresolvedDataSource).
		return nil, mcpGraphReason(errMCPToolInvalid, err)
	} else if err != nil {
		return nil, err
	}
	spans := make([]mcpCommentSpan, 0, len(prepared))
	for _, span := range prepared {
		if span.Text == "" {
			continue
		}
		spans = append(spans, mcpCommentSpanFromText(span))
	}
	if len(spans) > mcpCommentMaximumRichTextItems || strings.TrimSpace(mcpCommentSpanText(spans)) == "" {
		return nil, mcpInvalid("the comment needs words, in at most %d pieces of rich text", mcpCommentMaximumRichTextItems)
	}
	said.add(tagged)
	return spans, nil
}

// mcpCommentLocalMentions names the pages, databases and people a comment's
// mentions name by the public API's IDs — as notion-get-users and the API
// give them — by Leafen's own, which its text keeps.
func mcpCommentLocalMentions(app core.App, spans []graphTextSpan) []graphTextSpan {
	for index := range spans {
		span := &spans[index]
		switch span.Mention {
		case "page":
			if local := mcpLocalID(app, span.PageID); !strings.HasPrefix(local, mcpDataSourceURLPrefix) {
				span.PageID = local
			}
		case "database":
			span.DatabaseID = mcpLocalID(app, span.DatabaseID)
		case "person":
			if internalID, err := resolveNotionCompatibleUserID(span.UserID); err == nil {
				// One written without words keeps its ID as them, until it is named.
				if span.Text == span.UserID {
					span.Text = internalID
				}
				span.UserID = internalID
			}
		}
	}
	return spans
}

// mcpCommentSpanFromText is a piece of text as a comment keeps it: one
// color, its background when it has one, as Notion's rich text has it.
func mcpCommentSpanFromText(span graphTextSpan) mcpCommentSpan {
	color := notionSpanColor(span.Color, span.Background)
	if color == "default" {
		color = ""
	}
	return mcpCommentSpan{
		Text: span.Text, Bold: span.Bold, Italic: span.Italic, Underline: span.Underline,
		Strikethrough: span.Strikethrough, Code: span.Code, Color: color, Link: span.Link,
		Mention: span.Mention, PageID: span.PageID, BlockID: span.BlockID, DatabaseID: span.DatabaseID,
		CustomEmojiID: span.CustomEmojiID, Date: span.Date, DateEnd: span.DateEnd,
		DateTimeZone: span.DateTimeZone, UserID: span.UserID, Equation: span.Equation,
	}
}

func mcpCommentRichTextSpans(
	app core.App,
	authority mcpAuthority,
	current pageAuthorization,
	richText []map[string]any,
) ([]mcpCommentSpan, error) {
	if len(richText) == 0 || len(richText) > mcpCommentMaximumRichTextItems {
		return nil, mcpInvalid("rich_text has from 1 to %d items", mcpCommentMaximumRichTextItems)
	}
	// Its links and its page and database mentions are pointed here as a
	// comment's Markdown's are (notion_address_links_here.go): at what a
	// Notion move made from what they name, when nothing here answers to it.
	here := newNotionAddressLinksHere(app, current.Workspace.WorkspaceID)
	spans := make([]mcpCommentSpan, 0, len(richText))
	for _, item := range richText {
		span, err := mcpCommentRichTextSpan(app, authority, current, here, item)
		if err != nil {
			return nil, err
		}
		// An empty text item is left out, as Notion's MCP leaves it (seen
		// 2026-11-09); an empty equation is still refused.
		if span.Text == "" && mcpCommentRichTextKind(item) == "text" {
			continue
		}
		if span.Text == "" {
			return nil, mcpInvalid("each rich_text item is text, an equation or a mention, with something in it")
		}
		spans = append(spans, span)
	}
	if strings.TrimSpace(mcpCommentSpanText(spans)) == "" {
		return nil, mcpInvalid("the comment needs words")
	}
	return spans, nil
}

func mcpCommentRichTextSpan(
	app core.App,
	authority mcpAuthority,
	current pageAuthorization,
	here *notionAddressLinksHere,
	item map[string]any,
) (mcpCommentSpan, error) {
	span := mcpCommentSpan{}
	if annotations, ok := item["annotations"].(map[string]any); ok {
		span.Bold, _ = annotations["bold"].(bool)
		span.Italic, _ = annotations["italic"].(bool)
		span.Underline, _ = annotations["underline"].(bool)
		span.Strikethrough, _ = annotations["strikethrough"].(bool)
		span.Code, _ = annotations["code"].(bool)
		span.Color, _ = annotations["color"].(string)
		if utf8.RuneCountInString(span.Color) > 64 {
			return mcpCommentSpan{}, mcpInvalid("annotations.color is a color's name, as blue or red_background, at most 64 characters")
		}
	}
	switch mcpCommentRichTextKind(item) {
	case "text":
		text, ok := item["text"].(map[string]any)
		if !ok {
			return mcpCommentSpan{}, mcpInvalid("a text item holds text: {\"type\":\"text\",\"text\":{\"content\":\"…\"}}")
		}
		span.Text, ok = text["content"].(string)
		if !ok {
			return mcpCommentSpan{}, mcpInvalid("a text item's text.content is a string")
		}
		if link, ok := text["link"].(map[string]any); ok {
			span.Link, _ = link["url"].(string)
			if !validMCPCommentLink(span.Link) {
				return mcpCommentSpan{}, mcpInvalid("a link is an http or https address")
			}
			if span.Link != "" {
				var err error
				if span.Link, err = here.address(span.Link); err != nil {
					return mcpCommentSpan{}, err
				}
			}
		}
	case "equation":
		equation, ok := item["equation"].(map[string]any)
		if !ok {
			return mcpCommentSpan{}, mcpInvalid("an equation item holds equation: {\"expression\":\"…\"}")
		}
		span.Equation, ok = equation["expression"].(string)
		if !ok {
			return mcpCommentSpan{}, mcpInvalid("an equation's expression is a string")
		}
		span.Text = span.Equation
	case "mention":
		mention, ok := item["mention"].(map[string]any)
		if !ok {
			return mcpCommentSpan{}, mcpInvalid("a mention item holds mention: a user, a page, a database, a date or a custom emoji")
		}
		plain, _ := item["plain_text"].(string)
		if err := populateMCPCommentMention(app, authority, current, here, mention, plain, &span); err != nil {
			return mcpCommentSpan{}, err
		}
	default:
		return mcpCommentSpan{}, mcpInvalid("a rich_text item's type is text, equation or mention")
	}
	if utf8.RuneCountInString(span.Text) > mcpCommentMaximumTextRunes {
		return mcpCommentSpan{}, mcpInvalid("a rich_text item has at most %d characters", mcpCommentMaximumTextRunes)
	}
	return span, nil
}

func populateMCPCommentMention(
	app core.App,
	authority mcpAuthority,
	current pageAuthorization,
	here *notionAddressLinksHere,
	mention map[string]any,
	plain string,
	span *mcpCommentSpan,
) error {
	actorID, workspaceID, reaches := authority.PrincipalID, current.Workspace.WorkspaceID, mcpMentionReaches(authority)
	switch mcpCommentMentionKind(mention) {
	case "user":
		user, ok := mention["user"].(map[string]any)
		publicID, _ := user["id"].(string)
		if !ok || publicID == "" {
			return mcpInvalid("a user mention names the user: {\"user\":{\"id\":\"…\"}}, an ID from notion-get-users")
		}
		internalID, err := resolveNotionCompatibleUserID(publicID)
		if err != nil {
			return mcpInvalid("that user ID isn't one notion-get-users gives")
		}
		person, found, err := loadNotionUserDirectoryUser(app, actorID, workspaceID, internalID)
		if err != nil || !found {
			return mcpInvalid("that user isn't in this workspace")
		}
		// Without words, it reads "@" and their name, as a typed one does.
		span.Mention, span.UserID, span.Text = "person", internalID, plain
		if name := strings.TrimSpace(person.Name); span.Text == "" && name != "" {
			span.Text = "@" + name
		} else if span.Text == "" {
			span.Text = "@" + publicID
		}
	case "page":
		page, ok := mention["page"].(map[string]any)
		pageID, _ := page["id"].(string)
		if !ok || !validCommentIdentifier(pageID) {
			return mcpInvalid("a page mention names the page: {\"page\":{\"id\":\"…\"}}")
		}
		// One of an ID nothing here answers to names what a Notion move made
		// from that Notion page, block or database, as the Markdown's
		// <mention url> does (notionAddressLinksHere.mention).
		pointed := graphTextSpan{Mention: "page", PageID: pageID}
		if _, err := here.mention(&pointed); err != nil {
			return err
		}
		pageID = pointed.PageID
		if pointed.Mention == "database" {
			if made, err := mcpCommentDatabaseMention(app, authority, current, pointed.DatabaseID, span); err != nil || made {
				return err
			}
			pageID = pointed.DatabaseID
		} else if holder, err := mcpLinkedDatabasePage(app, workspaceID, pageID); err != nil {
			return err
		} else if holder != "" {
			// One of a database here is the database's, as Notion's MCP keeps a
			// page mention of a database's ID (seen 2026-11-22: read back as
			// the database's mention, by its type and name in fetch).
			if made, err := mcpCommentDatabaseMention(app, authority, current, pageID, span); err != nil || made {
				return err
			}
		}
		// One named by the public API's ID keeps the page's own, as its text
		// does (mcpCommentLocalMentions).
		pageID = mcpCommentLocalMentions(app, []graphTextSpan{{Mention: "page", PageID: pageID}})[0].PageID
		// The target may be a page or a database row page, one the agent's
		// token reaches when it was narrowed to chosen pages.
		var allows func(pageAuthorization) bool
		if reaches != nil {
			allows = func(target pageAuthorization) bool { return reaches(app, target) }
		}
		// One naming a block names the block's page at the block, by its
		// words, as a block's text does (page_mention_block_anchor.go).
		if block, found, err := pageMentionBlock(app, workspaceID, pageID); err != nil || found {
			if err != nil {
				return err
			}
			_, allowed, err := authorizePageMentionTarget(app, actorID, "", workspaceID, block.PageID, allows)
			span.Mention, span.PageID, span.BlockID, span.Text = "page", block.PageID, block.ID, ""
			if err == nil && allowed {
				span.Text = pageMentionBlockWords(app, block, pageMentionBlockSayer(app, actorID, workspaceID))
			}
			if span.Text == "" {
				span.Text = notionUnresolvedMentionTitle
			}
			return nil
		}
		// One it can't read, or that isn't there, is kept as Notion's MCP keeps
		// it (seen 2026-11-09: "사용 권한 없음" in the comment): its readers are
		// shown it in no-access marks, and nothing says which it was.
		target, allowed, err := authorizePageMentionTarget(app, actorID, "", workspaceID, pageID, allows)
		span.Mention, span.PageID, span.Text = "page", pageID, plain
		if span.Text == "" && err == nil && allowed {
			span.Text = target.Title
		}
		// Unnamed, as page text keeps an ID no page answers to ("Untitled").
		if span.Text == "" {
			span.Text = notionUnresolvedMentionTitle
		}
	case "date":
		date, ok := mention["date"].(map[string]any)
		span.Date, _ = date["start"].(string)
		span.DateEnd, _ = date["end"].(string)
		span.DateTimeZone, _ = date["time_zone"].(string)
		if !ok || span.Date == "" {
			return mcpInvalid("a date mention gives date.start, as 2026-10-23")
		}
		span.Mention, span.Text = "date", plain
		if span.Text == "" {
			span.Text = span.Date
		}
	case "database":
		database, ok := mention["database"].(map[string]any)
		databaseID, _ := database["id"].(string)
		if !ok || !validCommentIdentifier(databaseID) {
			return mcpInvalid("a database mention names the database: {\"database\":{\"id\":\"…\"}}")
		}
		if made, err := mcpCommentDatabaseMention(app, authority, current, databaseID, span); err != nil || made {
			return err
		}
		// One no database answers to is the mention of the page of that ID,
		// as Notion's keeps any database mention in a comment (seen
		// 2026-11-11): a database's page by its own ID, or "Untitled".
		return populateMCPCommentMention(app, authority, current, here,
			map[string]any{"type": "page", "page": map[string]any{"id": databaseID}}, plain, span)
	case "custom_emoji":
		emoji, ok := mention["custom_emoji"].(map[string]any)
		written, _ := emoji["id"].(string)
		emojiID, valid := notionCustomEmojiMentionWritten(strings.TrimSpace(written))
		if !ok || !valid {
			return mcpInvalid("a custom emoji mention names the emoji: {\"custom_emoji\":{\"id\":\"…\"}}")
		}
		found, allowed, err := loadNotionCustomEmojiMention(app, actorID, workspaceID, strings.ToLower(emojiID))
		if err != nil {
			return failedBecause(errMCPToolFailed, err)
		}
		// One the workspace hasn't is kept by its ID as written, as Notion's
		// MCP keeps it (seen 2026-11-11: read back as its <unknown_mention>)
		// and the API's comments keep one.
		span.Mention, span.CustomEmojiID, span.Text = "custom_emoji", emojiID, notionMissingCustomEmojiText
		if allowed {
			span.CustomEmojiID, span.Text = strings.ToLower(emojiID), ":"+strings.TrimSpace(found.Name)+":"
		}
	case "template_mention":
		// As Notion's refuses one (seen 2026-11-11).
		return mcpInvalid(notionTemplateMentionRefused)
	default:
		return mcpInvalid("a mention is of a user, a page, a database, a date or a custom emoji")
	}
	return nil
}

func mcpCommentSpanText(spans []mcpCommentSpan) string {
	var builder strings.Builder
	for _, span := range spans {
		builder.WriteString(span.Text)
	}
	return builder.String()
}

func validMCPCommentLink(raw string) bool {
	if raw == "" {
		return true
	}
	return validGraphTextLink(raw)
}
