package app

import (
	"encoding/json"
	"html"
	"maps"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// Notion's page Markdown writes a table of contents as
// <table-of-contents color="…" />, and a web bookmark or embed as
// <bookmark source="…">caption</bookmark> and <embed …> (seen 2026-10-29),
// which Leafen's page Markdown reads and writes alike.

var (
	notionMarkdownContentsPattern  = regexp.MustCompile(`^<table-of-contents((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*/?>(?:</table-of-contents>)?$`)
	notionMarkdownLinkBlockPattern = regexp.MustCompile(`^<(bookmark|embed)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*(?:/>|>(.*?)</(?:bookmark|embed)>)$`)
)

// notionMarkdownLinkBlockLine reads a line holding a table of contents, a
// bookmark or an embed into its block, its caption as notionMarkdownLinkCaption
// reads it; version1 reads it as the public API's "v1" does.
func notionMarkdownLinkBlockLine(line string, version1 bool, sitePages *notionMarkdownSitePages) (string, json.RawMessage, bool) {
	if kind, content, own := notionMarkdownOwnBlockLine(line); own {
		return kind, content, true
	}
	trimmed := strings.TrimSpace(line)
	if match := notionMarkdownContentsPattern.FindStringSubmatch(trimmed); match != nil {
		color := notionMarkdownAttributes(match[1])["color"]
		_, color = takeNotionMarkdownBlockColor(`x {color="` + color + `"}`)
		return graphBlockTypeTableOfContents, withNotionMarkdownBlockColor(json.RawMessage(`{}`), color), true
	}
	match := notionMarkdownLinkBlockPattern.FindStringSubmatch(trimmed)
	if match == nil {
		return "", nil, false
	}
	if as, made := notionMarkdownEmbedLineAs(match); made {
		if as.kind == graphBlockTypeVideo {
			return notionMarkdownFileLine(as.line, version1, sitePages)
		}
		trimmed = as.line
		if match = notionMarkdownLinkBlockPattern.FindStringSubmatch(trimmed); match == nil {
			return "", nil, false
		}
	}
	attributes := notionMarkdownAttributes(match[2])
	address := attributes["source"]
	if address == "" {
		address = attributes["src"]
	}
	// "v1" reads a bookmark's url too, not an embed's (seen 2026-11-10); "v2"
	// leaves it out first (notionMarkdownTagAttributes).
	if address == "" && match[1] == "bookmark" {
		address = attributes["url"]
	}
	address = html.UnescapeString(address)
	kind := graphBlockTypeBookmark
	if match[1] == "embed" {
		kind = graphBlockTypeEmbed
	}
	service := attributes["service"]
	// A bookmark or embed without its address yet waits for one, as a
	// person's, keeping its caption (seen 2026-11-10: <bookmark>cap</bookmark>
	// is a bookmark of no address captioned "cap"); so does an embed whose
	// address Notion's page Markdown doesn't keep (notionMarkdownWebAddress,
	// seen 2026-11-11: <embed source="mailto:…">), where a bookmark keeps any
	// as given, which its line then leaves out (linkBlock).
	placement, stored := notionMarkdownStoredFile(address)
	if address == "" || kind == graphBlockTypeEmbed && !stored && !notionMarkdownWebAddress(address) {
		content := map[string]any{}
		if service != "" {
			content["service"] = service
		}
		if caption := notionMarkdownLinkCaption(match[3], version1, sitePages); caption != nil {
			content["caption"] = caption
		}
		raw, err := json.Marshal(content)
		return kind, raw, err == nil
	}
	// An uploaded file in an embed is drawn: HTML in its sandbox, as Notion's.
	if stored && kind == graphBlockTypeEmbed {
		raw, err := json.Marshal(notionMarkdownFileRequest{File: placement, Caption: notionMarkdownLinkCaption(match[3], version1, sitePages)})
		return kind, raw, err == nil
	}
	// A link stays as written, as Notion's reads back (seen 2026-11-05); the
	// screen draws it in its service's frame.
	content := map[string]any{"url": address}
	if kind == graphBlockTypeEmbed && service != "" {
		content["service"] = service
	}
	if caption := notionMarkdownLinkCaption(match[3], version1, sitePages); caption != nil {
		content["caption"] = caption
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return "", nil, false
	}
	return kind, raw, true
}

// notionMarkdownEmbedAs is the block an embed of an address is made as when
// it can't be one, as the editor makes it from the link a person gives
// (LinkEmbedBlock.tsx commitEmbed, as Notion's editor, seen 2026-11-05/07): a
// Replit repl, which refuses to be framed, a bookmark, and a Loom video a
// video; "" for any other. Notion's MCP and API keep both an embed, the repl
// a grey broken frame and no word of it (seen 2026-11-13); Leafen's make the
// person's block, and its page Markdown says so (notionMarkdownEmbedWarning),
// so what an agent embeds draws as what a person's does (2026-11-13: the
// agent's repl stayed a broken frame it couldn't see).
func notionMarkdownEmbedAs(address, service string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || !notionMarkdownWebAddress(strings.TrimSpace(address)) {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	parts := strings.FieldsFunc(parsed.Path, func(r rune) bool { return r == '/' })
	switch {
	case host == "replit.com" && (service == "" || service == "replit") && len(parts) >= 2 && strings.HasPrefix(parts[0], "@"):
		return graphBlockTypeBookmark
	case host == "loom.com" && (service == "" || service == "loom") && len(parts) >= 2 && (parts[0] == "share" || parts[0] == "embed"):
		return graphBlockTypeVideo
	}
	return ""
}

// notionMarkdownEmbedLine is an embed's line written as the block it is made
// as (notionMarkdownEmbedAs).
type notionMarkdownEmbedLine struct {
	kind, line string
}

// notionMarkdownEmbedLineAs writes an embed line, matched by
// notionMarkdownLinkBlockPattern, as the bookmark or video line of the block
// its address is made as, its caption kept.
func notionMarkdownEmbedLineAs(match []string) (notionMarkdownEmbedLine, bool) {
	if match[1] != "embed" {
		return notionMarkdownEmbedLine{}, false
	}
	attributes := notionMarkdownAttributes(match[2])
	address := attributes["source"]
	if address == "" {
		address = attributes["src"]
	}
	address = html.UnescapeString(address)
	written := html.EscapeString(address)
	switch notionMarkdownEmbedAs(address, attributes["service"]) {
	case graphBlockTypeBookmark:
		return notionMarkdownEmbedLine{graphBlockTypeBookmark, `<bookmark source="` + written + `">` + match[3] + `</bookmark>`}, true
	case graphBlockTypeVideo:
		return notionMarkdownEmbedLine{graphBlockTypeVideo, `<video src="` + written + `">` + match[3] + `</video>`}, true
	}
	return notionMarkdownEmbedLine{}, false
}

// notionMarkdownEmbedWarning is what a write says of an embed, its tag's
// attributes, made as another block (notionMarkdownEmbedAs), as the
// editor's notice says it to a person.
func notionMarkdownEmbedWarning(attributes map[string]string) (notionMarkdownWarning, bool) {
	address := html.UnescapeString(firstNonempty(attributes["source"], attributes["src"]))
	switch notionMarkdownEmbedAs(address, attributes["service"]) {
	case graphBlockTypeVideo:
		return notionMarkdownWarning{Code: "embed_converted", Message: "<embed> of the Loom video " + address +
			" was made a <video>, as Leafen's editor makes a Loom link. Write <video source=\"…\"> for it."}, true
	case graphBlockTypeBookmark:
		return notionMarkdownWarning{Code: "embed_converted", Message: "<embed> of " + address +
			" can't be shown in a frame (the site refuses it), so it was made a <bookmark>, a preview card, " +
			"as Leafen's editor makes it. Write <bookmark source=\"…\"> for it."}, true
	}
	return notionMarkdownWarning{}, false
}

// notionMarkdownLinkKinds are the blocks a link line makes: a web bookmark
// and an embed.
var notionMarkdownLinkKinds = map[string]bool{graphBlockTypeBookmark: true, graphBlockTypeEmbed: true}

// notionMarkdownLinkSaid are what a link line says of its block.
var notionMarkdownLinkSaid = []string{"url", "caption", "service"}

// notionMarkdownWebLink tells whether a bookmark or embed, its line's content
// (line) or its stored content, names a web address or none yet, not a file.
func notionMarkdownWebLink(kind string, content map[string]any) bool {
	if !notionMarkdownLinkKinds[kind] || content == nil {
		return false
	}
	for _, file := range []string{"markdownFile", "fileId", "fileUploadId", "hostedFile"} {
		if _, ok := content[file]; ok {
			return false
		}
	}
	return true
}

// notionMarkdownLinkWrite is the content a bookmark or embed line writes, and,
// when the line changed a block of its kind (before, that block's line, and
// stored, its content), the content it changes. Notion's page Markdown never
// reads a bookmark's page (seen 2026-11-11): a new line's bookmark is the
// plain card the block API makes (plainNotionAPIBookmark), and a changed line
// keeps its block, which takes what the line changed, its address and
// caption; a bookmark keeps its preview over a new address, the old page's
// name and all, and one never read takes the plain card. In "v1" the line
// changed may be the bookmark's <unknown> one, which says nothing of it.
func notionMarkdownLinkWrite(
	kind string, line json.RawMessage, before *workspaceImportStagedItem, stored map[string]any,
) (json.RawMessage, json.RawMessage, error) {
	written := map[string]any{}
	if err := json.Unmarshal(line, &written); err != nil {
		return nil, nil, err
	}
	if before == nil || stored == nil {
		plainNotionAPIBookmark(kind, nil, true, written)
		content, err := json.Marshal(written)
		return content, nil, err
	}
	said := map[string]any{}
	if before.Type == kind && json.Unmarshal(before.Content, &said) != nil {
		return nil, nil, errNotionMarkdownUnparsed
	}
	next := maps.Clone(stored)
	for _, key := range notionMarkdownLinkSaid {
		value, writes := written[key]
		if before.Type == kind && graphViewValuesEqual(graphViewMergeValue(said, key), graphViewMergeValue(written, key)) {
			continue
		}
		switch {
		case writes:
			next[key] = value
		case key != "service":
			delete(next, key)
		}
	}
	address, _ := next["url"].(string)
	if storedAddress, _ := stored["url"].(string); kind == graphBlockTypeBookmark &&
		strings.TrimSpace(address) != "" && strings.TrimSpace(address) != strings.TrimSpace(storedAddress) {
		for _, key := range notionBookmarkPreviewKeys {
			if _, kept := next[key]; !kept {
				next[key] = ""
			}
		}
		next["metadataUrl"] = strings.TrimSpace(address)
	}
	base, err := json.Marshal(stored)
	if err != nil {
		return nil, nil, err
	}
	content, err := json.Marshal(next)
	return content, base, err
}

// notionMarkdownCaptionTags are the tags whose text is their block's caption.
var notionMarkdownCaptionTags = map[string]bool{
	"bookmark": true, "embed": true, "image": true, "file": true, "pdf": true, "video": true, "audio": true,
}

// notionMarkdownCaptionLines is a caption written on the lines between its
// tags, as Notion's "v2" reads it (seen 2026-11-10): one line as written;
// several each without the whitespace it starts with and, but the last,
// ends with, joined by line breaks, a blank line at either edge one too
// ("\n\n  cap  \n\n" is "\ncap\n"). False for one of more than a paragraph.
func notionMarkdownCaptionLines(body []string) (string, bool) {
	if len(body) == 1 {
		return body[0], true
	}
	parts, text, gap := make([]string, len(body)), false, false
	for index, line := range body {
		if strings.TrimSpace(line) == "" {
			gap = text
			continue
		}
		if gap {
			return "", false
		}
		text = true
		parts[index] = strings.TrimLeftFunc(line, unicode.IsSpace)
		if index < len(body)-1 {
			parts[index] = strings.TrimRightFunc(parts[index], unicode.IsSpace)
		}
	}
	return strings.Join(parts, "<br>"), true
}

// notionMarkdownLinkCaption reads a block's caption written on its line: a
// bookmark's, an embed's, a file's, an image's or another medium's, the
// addresses of sitePages' pages standing bare in it as their mentions. In
// "v1" it reads as a "v1" table cell's text does, as Notion's (seen
// 2026-11-11): its links, emphasis, escapes and code as "v1" reads them
// ("[a](a b)" a link to "a b", "*a *b* c*" an italic "a ", a "b" and an
// italic " c", "\a" an "a"), a piece for each thing read in it.
func notionMarkdownLinkCaption(written string, version1 bool, sitePages *notionMarkdownSitePages) []graphTextSpan {
	caption := notionMarkdownLineBreaks(written, version1)
	if strings.TrimSpace(caption) == "" {
		return nil
	}
	parser := workspaceImportInlineParser{notion: true, version1: version1, cell: version1, sitePages: sitePages}
	parser.parse(caption, workspaceImportInlineStyle{}, 0)
	return parser.spans
}
