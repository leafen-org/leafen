package app

import (
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Notion keeps a custom emoji mention of an emoji its workspace hasn't — one
// written by an ID nothing answers to, or one deleted — and its app draws a
// dashed "?" in its place, "(삭제됨)" when pointed at, in a page's title, its
// path and its text alike (seen 2026-11-10). Its API leaves the mention out
// of rich text, the text on either side staying two pieces; its page
// Markdown reads it as
// <unknown_mention url="notion://custom_emoji/<workspace ID>/<emoji ID>" alt="custom_emoji"/>
// ("… />" in "v2"), and takes that tag back as the same mention.
//
// Leafen keeps such a mention by its ID: a deleted emoji's with the :name:
// it had, one never named here with ":?:" (notionMissingCustomEmojiText),
// the words a title read as plain text shows where Notion draws its "?".
// Notion keeps the ID as it was written (seen 2026-11-11): the API's in
// capitals as they were, one without dashes given them; page Markdown's
// exactly as its tag wrote it, another workspace's, "nope" or capitals
// alike. Leafen keeps one of this workspace by its ID as written, one of
// another workspace by the tag's whole address (notionMissingCustomEmojiKept).
// Every reader reads it as Notion's: the app as the deleted emoji's dashed
// "?" (InlineCustomEmoji.tsx), the API leaving it out, page Markdown as the
// <unknown_mention> tag.

// notionMissingCustomEmojiText is the text of a custom emoji mention kept for
// an emoji never named here.
const notionMissingCustomEmojiText = ":?:"

// customEmojiSpanNeverHad reports a custom emoji mention of one the
// workspace never had, known without asking: its words ":?:"
// (notionMissingCustomEmojiText) or an ID Leafen doesn't give, as the app
// tells one (customEmojiNeverHad).
func customEmojiSpanNeverHad(span graphTextSpan) bool {
	return span.Mention == "custom_emoji" && (span.Text == notionMissingCustomEmojiText || !validCustomEmojiID(span.CustomEmojiID))
}

// titleWithoutNeverHadCustomEmoji is a title, as written with its spans, in
// plain words: a custom emoji mention of one the workspace never had says
// nothing, as the app's plain title (closedMentions.ts) and Notion's window
// title and search line say it (seen 2026-11-11: "emo ", that emoji and
// " end" reads "emo end"); any other title as it is.
func titleWithoutNeverHadCustomEmoji(title string, spans []graphTextSpan) string {
	if spelledName(spans) != title {
		return title
	}
	var said strings.Builder
	dropped := false
	for _, span := range spans {
		if customEmojiSpanNeverHad(span) {
			dropped = true
			continue
		}
		said.WriteString(span.Text)
	}
	if !dropped {
		return title
	}
	return said.String()
}

// notionMissingCustomEmojiAddressPrefix begins the address page Markdown
// names a missing custom emoji by; a kept mention of another workspace's
// emoji keeps that whole address as its ID.
const notionMissingCustomEmojiAddressPrefix = "notion://custom_emoji/"

// notionMissingCustomEmojiAddress is the address page Markdown names a
// missing custom emoji by, in the workspace the reader reads it in: the
// address it was written by when it names another workspace's.
func notionMissingCustomEmojiAddress(workspacePublicID, kept string) string {
	if strings.HasPrefix(kept, notionMissingCustomEmojiAddressPrefix) {
		return kept
	}
	return notionMissingCustomEmojiAddressPrefix + workspacePublicID + "/" + kept
}

// notionMissingCustomEmojiKept is the ID a custom emoji mention the
// workspace hasn't is kept by: one written by a tag's address naming this
// workspace (its public ID workspacePublicID) is its emoji ID as written,
// any other the ID as given.
func notionMissingCustomEmojiKept(workspacePublicID, id string) string {
	if address, ok := strings.CutPrefix(id, notionMissingCustomEmojiAddressPrefix); ok {
		if space, emojiID, _ := strings.Cut(address, "/"); space == workspacePublicID && emojiID != "" {
			return emojiID
		}
	}
	return id
}

// notionCustomEmojiMentionWritten is a custom emoji mention's ID as Notion's
// API takes it (seen 2026-11-11): with dashes in any case as written, 32
// small hex digits with dashes put in; false for any other.
func notionCustomEmojiMentionWritten(value string) (string, bool) {
	if notionDashedUUIDText.MatchString(value) {
		return value, true
	}
	if notionPlainLowerUUIDText.MatchString(value) {
		return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:], true
	}
	return "", false
}

var (
	notionDashedUUIDText     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	notionPlainLowerUUIDText = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// notionTitleWithoutMissingCustomEmoji is a page's title as Notion's API
// says it in a mention of the page: a custom emoji the workspace hasn't in
// it says nothing (seen 2026-11-11: "emo ", that emoji and " end" reads
// "emo  end", one leading " lead"). Spans that don't spell the title leave
// it as it is, and so does a reading for no one.
func notionTitleWithoutMissingCustomEmoji(app core.App, principalID, workspaceID, title string, spans []graphTextSpan) string {
	if principalID == "" {
		return title
	}
	var spelled, said strings.Builder
	missing := false
	for _, span := range spans {
		spelled.WriteString(span.Text)
		if span.Mention == "custom_emoji" {
			if _, there, err := loadNotionCustomEmojiMention(app, principalID, workspaceID, span.CustomEmojiID); err == nil && !there {
				missing = true
				continue
			}
		}
		said.WriteString(span.Text)
	}
	if !missing || spelled.String() != title {
		return title
	}
	return said.String()
}

// notionMarkdownMissingCustomEmojiTag is a missing custom emoji as page
// Markdown writes it: "v1" closes the tag with "/>", "v2" with " />".
func notionMarkdownMissingCustomEmojiTag(address string, version1 bool) string {
	end := ` />`
	if version1 {
		end = `/>`
	}
	return `<unknown_mention url="` + notionMarkdownAttributeEscaped.Replace(address) + `" alt="custom_emoji"` + end
}

// notionMarkdownRefusedMention marks a span the Markdown's reading refuses,
// its text the words Notion refuses it with; the mention's check says them
// (notionMentionPreparation.prepare).
const notionMarkdownRefusedMention = "\x00refused"

var notionMarkdownUnknownMentionPattern = regexp.MustCompile(`^<unknown_mention((?:\s+[A-Za-z][A-Za-z_-]*="[^"]*")*)\s*/>`)

// notionMarkdownUnknownMention reads an <unknown_mention … /> tag opening at
// value[start:], as Notion takes one (seen 2026-11-10 and 2026-11-11, "v1"
// and "v2" alike): one of notion://custom_emoji/<workspace>/<emoji ID>
// with alt="custom_emoji" is that emoji's mention, its address kept as
// written up to the emoji ID (a part after it goes) — any workspace, any
// ID; one with no workspace or no ID in its address the text "‣"; one
// without alt, of another alt or of another kind is refused. The mention's
// check keeps it (notionMissingCustomEmojiKept) or names this workspace's
// emoji by it. It returns where the tag ends; a tag not closed in itself
// isn't one.
func notionMarkdownUnknownMention(value string, start int) (graphTextSpan, int, bool) {
	match := notionMarkdownUnknownMentionPattern.FindStringSubmatch(value[start:])
	if match == nil {
		return graphTextSpan{}, 0, false
	}
	end := start + len(match[0])
	attributes := map[string]string{}
	for _, attribute := range notionMarkdownAttributePattern.FindAllStringSubmatch(match[1], -1) {
		attributes[attribute[1]] = strings.NewReplacer(`&quot;`, `"`, `&lt;`, `<`, `&gt;`, `>`, `&amp;`, `&`).Replace(attribute[2])
	}
	address := attributes["url"]
	alt, named := attributes["alt"]
	path, emoji := strings.CutPrefix(address, notionMissingCustomEmojiAddressPrefix)
	switch {
	case !named:
		return graphTextSpan{Mention: notionMarkdownRefusedMention, Text: "This unknown mention can't be read: " + address}, end, true
	case alt != "custom_emoji" || !emoji:
		return graphTextSpan{Mention: notionMarkdownRefusedMention, Text: `Unknown mention "` + alt + `" can't be read: ` + address}, end, true
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return graphTextSpan{Text: "‣"}, end, true
	}
	kept := notionMissingCustomEmojiAddressPrefix + parts[0] + "/" + parts[1]
	return graphTextSpan{Text: notionMissingCustomEmojiText, Mention: "custom_emoji", CustomEmojiID: kept}, end, true
}
