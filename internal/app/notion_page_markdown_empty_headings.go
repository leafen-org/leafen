package app

import (
	"regexp"
	"strings"
)

// An empty heading reads as Notion's page Markdown reads one (seen
// 2026-11-14, its public API's "v1" and "v2", each written and read back as
// blocks):
//
//   - In "v2" and the MCP's Markdown a heading's marks alone are an empty
//     heading, with whitespace after them or none ("#", "# ", "##   "), in a
//     list item or a quote too ("- ##" holds one); <h3 color="red"></h3>,
//     on one line or two, is an empty heading of its color, and
//     <toggle heading="h2"></toggle> an empty toggle heading.
//   - In "v1" the marks alone are text as written ("##", "##   ", and an
//     item's "- ##   "), and a color or {toggle="true"} after them makes the
//     empty heading ("### {color=\"red\"}", "#    {color=\"red\"}",
//     "## {color=\"default\"}", "## {toggle=\"true\" color=\"red\"}").
//
// Notion writes an empty heading "#" in "v1" and "# " in "v2", one of a
// color "### {color=\"red\"}" and <h3 color="red"></h3>, a toggle heading
// "## {toggle=\"true\"}" and <toggle heading="h2"></toggle>, as Leafen writes
// them (notion_page_markdown_api_form.go, notion_page_markdown_writer.go).
// A "v1" write of a page's own "#" line reads it as the paragraph "#", in
// Notion and here alike, but a line Leafen finds unchanged keeps its block
// (notion_page_markdown_kept_lines.go).

// notionMarkdownEmptyHeadingMarks are a heading's marks alone.
var notionMarkdownEmptyHeadingMarks = regexp.MustCompile(`^#{1,6}$`)

// notionMarkdownVersion1HeldHeading is a heading a "v1" list item or to-do
// holds: its marks and some text after them (seen 2026-11-14: "- ##\tz"
// holds the heading "z", "- ##" and "- ##   " are items of that text).
var notionMarkdownVersion1HeldHeading = regexp.MustCompile(`^#{1,6}[ \t]+\S`)

// notionMarkdownEmptyHeading is the heading a line's marks alone make,
// trimmed.
func notionMarkdownEmptyHeading(trimmed string) (string, bool) {
	if !notionMarkdownEmptyHeadingMarks.MatchString(trimmed) {
		return "", false
	}
	return notionMarkdownHeadingKind(len(trimmed)), true
}

// A heading's five or six marks make a fourth-level heading, as four do
// (seen 2026-11-14: "##### h" and "###### h" are heading_4 in "v1" and
// "v2", in a list item, a quote or a callout too, with a tab after them,
// empty, colored or a toggle heading; "#######" is text, and so are <h5>
// and a <toggle heading="h5"> a toggle).

// notionMarkdownColoredMarks keeps a paragraph of a heading's marks alone a
// paragraph when a color follows it, which would read as an empty heading
// of that color, as Notion's "v1" reads its own (seen 2026-11-14: Notion
// writes the red paragraph "#####" as "##### {color=\"red\"}", and reads
// "\##### {color=\"red\"}" as that paragraph).
func notionMarkdownColoredMarks(text, color string) string {
	if color != "" && notionMarkdownEmptyHeadingMarks.MatchString(strings.TrimSpace(text)) {
		return `\` + text
	}
	return text
}

// notionMarkdownHeadingKind is the heading so many marks make.
func notionMarkdownHeadingKind(marks int) string {
	return []string{graphBlockTypeHeading1, graphBlockTypeHeading2, graphBlockTypeHeading3, graphBlockTypeHeading4}[min(marks, 4)-1]
}

// notionMarkdownToggleHeadingKind is the toggle heading so many marks make.
func notionMarkdownToggleHeadingKind(marks int) string {
	return []string{graphBlockTypeToggleHeading1, graphBlockTypeToggleHeading2, graphBlockTypeToggleHeading3,
		graphBlockTypeToggleHeading4}[min(marks, 4)-1]
}
