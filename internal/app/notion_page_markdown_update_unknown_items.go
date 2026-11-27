package app

import (
	"html"
	"net/url"
	"strings"
)

// notionMarkdownUpdateUnknownURL is the url an update's <unknown> tag holds
// in double quotes, without the backslashes that keep it unlinked
// (notionMarkdownUpdateUnlinkedAddresses), and whether it holds one.
func notionMarkdownUpdateUnknownURL(tag string) (string, bool) {
	address, named := notionMarkdownAttributes(tag)["url"]
	return notionMarkdownLinkedAddress(html.UnescapeString(address)), named
}

// notionMarkdownMarkedUnknownURL is the url of an <unknown> closed in
// itself standing as an item's text in an update, as
// notionMarkdownUpdateUnknownURL reads it, and whether it holds one: "v1"
// reads its values in single quotes or none as in double ones, and "v2"
// refuses the first such value instead, as Notion does (seen 2026-11-15:
// "- <unknown url='https://…'/>" says "v1"'s https://… names no page, "v2"'s
// that its "url" isn't in double quotes).
func notionMarkdownMarkedUnknownURL(tag string, version2 bool) (string, bool, string) {
	if !version2 {
		if read, ok := notionMarkdownVersion1ReadTag(tag); ok {
			tag = read.written
		}
	} else if attribute := notionMarkdownLooseAttribute.FindStringSubmatch(
		notionMarkdownQuotedValue.ReplaceAllString(tag, `=""`)); attribute != nil {
		return "", false, `<unknown> has its attribute "` + attribute[1] + `" written wrong: ` + notionMarkdownDoubleQuoteValueSaid
	}
	address, named := notionMarkdownUpdateUnknownURL(tag)
	return address, named, ""
}

// notionMarkdownMarkedUnknownRefusal is why an update's <unknown> standing
// as the text of an item, naming no block the page writes as unknown, makes
// none, as Notion's update-page says it (seen 2026-11-15, "v1" and "v2",
// replace_content and insert_content alike): a bare name is no address
// ("x"), and any other names no page or view block. Notion answers one of
// a scheme other than the web's ("mailto:", "tel:", "ftp:") with a 500
// "Unexpected error occurred."; Leafen refuses it as a request with the words
// it says of any other, keeping a refusal a client can fix.
func notionMarkdownMarkedUnknownRefusal(address string) string {
	if parsed, err := url.Parse(address); err == nil && parsed.Scheme == "" && !strings.HasPrefix(address, "/") {
		return `"` + address + `" is neither a full URL nor an ID or short form this can read`
	}
	return "Not the URL of a page or database: " + address
}

// notionMarkdownVersion1EmptiedUnknownItems is a "v1" update's Markdown with
// each list item, to-do included, whose text is an <unknown> closed in
// itself without a url, or with an empty one, left empty, as Notion reads
// it (seen 2026-11-15: "- <unknown alt=\"a\"/>" is an empty item, "- [ ]
// <unknown/>" an empty to-do; a quote's is its text).
func notionMarkdownVersion1EmptiedUnknownItems(markdown string) string {
	if !strings.Contains(markdown, "<unknown") {
		return markdown
	}
	lines, fence := strings.Split(markdown, "\n"), ""
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if notionMarkdownFenceCloses(trimmed, fence, true) {
				fence = ""
			}
			continue
		}
		if notionMarkdownVersion2Fence(trimmed) {
			fence = notionMarkdownFenceOf(trimmed)
			continue
		}
		marks := notionMarkdownVersion1ItemMarks(trimmed)
		tag := notionMarkdownUpdateMarkedUnknown.FindString(trimmed[len(marks):])
		if strings.TrimSpace(marks) == "" || tag == "" {
			continue
		}
		if address, named, _ := notionMarkdownMarkedUnknownURL(tag, false); named && address != "" {
			continue
		}
		lines[index] = line[:len(line)-len(strings.TrimLeft(line, " \t"))] + marks
	}
	return strings.Join(lines, "\n")
}
