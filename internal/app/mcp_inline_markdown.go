package app

import (
	"slices"
	"strings"
)

// An MCP write's inline Markdown — a page's title, a database page's title
// or text value, a comment, a reply or a comment's new words, a suggested
// edit's words — is read by one reader (mcpInlineMarkdownSpans): its tags
// first as a page's body reads the tags standing in its text
// (notionMarkdownRepair.tag) — v1's mentions and date (<mention-user>,
// <mention-page>, <mention-database>, <mention-data-source>,
// <mention-date>) as the <mention> or <date> each becomes, HTML put right
// or taken out (<mark>, <kbd>, <div>, <script> …), a tag neither reads kept
// as its words — each said once in the body's words, then as a value reads
// its text (mcpPropertyMarkdownSpans). Notion's MCP keeps the older tags in
// a title, a value and a comment as words and says nothing (seen 2026-11-11
// and 2026-11-16, its reading the Public API's "v2" comment keeps); an agent
// that mentions a person so in a comment is told it succeeded while the
// comment mentions no one and notifies no one (2026-11-24, an agent's real
// use), so Leafen reads them as its body does and says so.

// mcpInlineMarkdownSpans reads an MCP write's inline Markdown into its words
// and their spans, none for plain words, and what reading its tags said.
func mcpInlineMarkdownSpans(value string, site *notionMarkdownSitePages) (string, []graphTextSpan, []notionMarkdownWarning) {
	read, said := mcpInlineMarkdownTags(value)
	words, spans := mcpPropertyMarkdownSpans(read, site)
	return words, spans, said
}

// mcpInlineMarkdownTags is value with its tags outside code read as a
// page's body reads those standing in its text, and what that said, in the
// body's order (notionMarkdownRepair.ordered).
func mcpInlineMarkdownTags(value string) (string, []notionMarkdownWarning) {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	repair := &notionMarkdownRepair{}
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		var out strings.Builder
		at := 0
		for _, code := range notionMarkdownCodeSpan.FindAllStringIndex(line, -1) {
			out.WriteString(mcpInlineTextTags(repair, line[at:code[0]]))
			out.WriteString(line[code[0]:code[1]])
			at = code[1]
		}
		out.WriteString(mcpInlineTextTags(repair, line[at:]))
		lines[index] = notionMarkdownRemovedSpaces(out.String())
	}
	return strings.Join(lines, "\n"), repair.ordered()
}

// mcpInlineTextTags reads the tags of a stretch of text outside code: v1's
// mentions and date renamed and HTML put right as the body puts them, each
// said; a tag the body keeps as its words said and left for the value's
// reading to keep so (mcpPropertyTagsAsText); "v2"'s own tags, escaped
// ones and those whose attributes no reading takes as written.
func mcpInlineTextTags(repair *notionMarkdownRepair, text string) string {
	text = repair.dropped(text)
	var out strings.Builder
	at := 0
	for _, found := range notionMarkdownRepairTag.FindAllStringSubmatchIndex(text, -1) {
		out.WriteString(text[at:found[0]])
		tag := text[found[0]:found[1]]
		at = found[1]
		written, attributes := text[found[4]:found[5]], text[found[6]:found[7]]
		name, closing := strings.ToLower(written), found[3] > found[2]
		known := notionMarkdownVersion2Tags[name] || notionMarkdownVersion1Tags[name] != "" || name == "summary"
		switch {
		case notionMarkdownEscapedAt(text, found[0]), written == name && notionMarkdownVersion2Tags[name]:
		case known && !notionMarkdownHTMLTag(name) && !notionMarkdownRepairAttributes.MatchString(attributes):
		case written != name:
			repair.unsupported(written, attributes)
		case strings.HasPrefix(name, "mention-") && notionMarkdownVersion1Renamed[name] != "", notionMarkdownHTMLTag(name):
			tag = repair.tag(tag, name, closing, notionMarkdownAttributes(attributes), false, false)
		case name == "summary":
			repair.tag(tag, name, closing, nil, false, false)
		case !known:
			repair.unsupported(written, attributes)
		}
		out.WriteString(tag)
	}
	out.WriteString(text[at:])
	return out.String()
}

// mcpMarkdownSaid gathers what reading a write's inline Markdown said, each
// once, in the order said; nil gathers nothing.
type mcpMarkdownSaid struct {
	warnings []notionMarkdownWarning
}

func (said *mcpMarkdownSaid) add(warnings []notionMarkdownWarning) {
	if said == nil {
		return
	}
	for _, warning := range warnings {
		if !slices.Contains(said.warnings, warning) {
			said.warnings = append(said.warnings, warning)
		}
	}
}

// answer is what was said as an MCP answer's warnings, nil for nothing.
func (said *mcpMarkdownSaid) answer() []mcpMarkdownWarning {
	if said == nil {
		return nil
	}
	return mcpMarkdownWarnings(nil, said.warnings)
}
