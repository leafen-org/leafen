package app

import (
	"regexp"
	"strings"
)

// An update's <unknown> whose name whitespace other than a space follows
// ("<unknown\turl=\"…\"/>") escapes the check Notion's update-page makes
// before it reads the Markdown, which takes "<unknown " alone
// (notionMarkdownUpdateUnknownLine): its Markdown reads it, as a new page's
// does (seen 2026-11-15, replace_content, "v1" and "v2" alike). One closed
// in itself naming no block the page writes as unknown is refused by its
// address, as no page's or database's URL — Notion
// answers one of a scheme other than the web's with a 500, which Leafen
// refuses so too (notionMarkdownMarkedUnknownRefusal) — and one
// without a url, or with an empty one, "v2" refuses as it reads the
// Markdown (notionMarkdownCreateUnknownMissingURL) and "v1" reads as
// nothing. One holding text between its tags "v2" refuses as any
// (notionMarkdownCreateUnknownPaired) and "v1" reads as its words, as written.

// notionMarkdownUpdateReadUnknownLine is such a line's <unknown> tag, past
// its indent.
var notionMarkdownUpdateReadUnknownLine = regexp.MustCompile(`^<unknown[\t\v\f][^<>]*>`)

// notionMarkdownUpdateReadUnknown is what an update says of such a line's
// tag naming no block the page writes as unknown: a fault "v2" says as it
// reads the Markdown, or the address it refuses; both "" for none.
func notionMarkdownUpdateReadUnknown(tag string, unknown map[string]string, version2 bool) (fault, refused string) {
	address, named := notionMarkdownUpdateUnknownURL(tag)
	switch {
	case !strings.HasSuffix(tag, "/>") || named && unknown[notionMarkdownUnknownKey(address)] != "":
	case version2 && !named:
		return notionMarkdownCreateUnknownMissingURL, ""
	case version2 && address == "":
		return notionMarkdownCreateUnknownEmptyURL, ""
	case address != "":
		return "", address
	}
	return "", ""
}

// notionMarkdownVersion1ReadUnknownLines is a "v1" update's Markdown with
// each such line read as its Markdown reads it: one closed in itself
// without a url, or with an empty one, gone, and one holding text its
// words.
func notionMarkdownVersion1ReadUnknownLines(markdown string) string {
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
		tag := notionMarkdownUpdateReadUnknownLine.FindString(trimmed)
		if tag == "" {
			continue
		}
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if !strings.HasSuffix(tag, "/>") {
			lines[index] = lead + `\` + trimmed
		} else if address, _ := notionMarkdownUpdateUnknownURL(tag); address == "" && tag == trimmed {
			lines[index] = ""
		}
	}
	return strings.Join(lines, "\n")
}
