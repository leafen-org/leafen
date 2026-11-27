package app

import (
	"regexp"
	"strings"
)

// A text block whose text is whitespace alone writes that text as a <p> of
// its own on its tag's first line, in "v2" and the MCP's Markdown, as
// Notion's does (seen 2026-11-14): "<p>⏎<p>  </p>⏎⏎b⏎</p>" for a paragraph
// holding "b", "<p color=\"red\">⏎<p>  </p>⏎</p>" for a colored one, and so a
// quote's, a list item's, a to-do's, a heading's, a toggle's, a toggle
// heading's and a callout's, whether or not they hold blocks; a paragraph
// of the default color holding none stays "<p>  </p>" on one line. That <p>
// reads back as the block's text, whitespace and all
// (notionMarkdownParagraphTitled), so a fetch written back keeps the block
// and what it holds. An empty colored paragraph, quote, to-do or heading
// holding nothing closes on its line, as Notion's: "<p color=\"red\" />",
// "<quote color=\"red\"></quote>", "<todo checked=\"false\" color=\"red\"></todo>",
// "<h3 color=\"red\"></h3>".

// notionMarkdownWhitespaceText tells text of whitespace alone.
func notionMarkdownWhitespaceText(text string) bool {
	return text != "" && strings.TrimSpace(text) == ""
}

// version2SpacesParagraph writes a paragraph of whitespace that holds
// blocks or has a color in "v2": its tag, the whitespace's <p> and its blocks.
func (w *notionMarkdownWriter) version2SpacesParagraph(
	block workspaceExportMarkdownBlock, prefix, text, color string,
) ([]string, bool) {
	if !w.mentions.Version2 || !notionMarkdownWhitespaceText(text) || len(w.children[block.ID]) == 0 && color == "" ||
		holdsTemplateMention(block.Spans) {
		return nil, false
	}
	return w.held(block, prefix, "<p"+color+">", text, "</p>"), true
}

// notionMarkdownVersion2ClosedEmpty are the text blocks an empty tag of
// theirs closes on its line in "v2", a toggle heading's too (seen
// 2026-11-14: <toggle heading="h2"></toggle>).
var notionMarkdownVersion2ClosedEmpty = map[string]bool{
	"paragraph": true, "quote": true, "to_do": true, "todo": true,
	"heading_1": true, "heading_2": true, "heading_3": true, graphBlockTypeHeading4: true,
	graphBlockTypeToggleHeading1: true, graphBlockTypeToggleHeading2: true, graphBlockTypeToggleHeading3: true,
	graphBlockTypeToggleHeading4: true,
}

// version2ClosedEmpty is an empty text block held in its tag — a colored
// one — that holds nothing, written on one line in "v2".
func (w *notionMarkdownWriter) version2ClosedEmpty(
	block workspaceExportMarkdownBlock, prefix, opening, title, closing string,
) ([]string, bool) {
	if !w.mentions.Version2 || title != "" || len(w.children[block.ID]) > 0 || !notionMarkdownVersion2ClosedEmpty[block.Type] {
		return nil, false
	}
	w.rendered = append(w.rendered, block.ID)
	if block.Type == "paragraph" {
		return []string{prefix + strings.TrimSuffix(opening, ">") + " />"}, true
	}
	return []string{prefix + opening + closing}, true
}

// An empty paragraph of a color reads back so, written "<p color=\"red\" />"
// on its line or "<p color=\"red\">⏎<p />⏎…</p>" holding blocks, as
// Notion's "v2" reads it (seen 2026-11-14); Leafen's reader takes it as
// "<empty-block/> {color=\"red\"}" (workspace_import_parser.go).
var notionMarkdownEmptyColoredParagraph = regexp.MustCompile(`^<p((?:[ \t]+[a-z-]+="[^"]*")+)[ \t]*/>$`)

// notionMarkdownEmptyBlockColor is an empty paragraph's color after its
// <empty-block/>, as Leafen reads it; none for the default.
func notionMarkdownEmptyBlockColor(attributes map[string]string) string {
	if value := attributes["color"]; value != "" && value != "default" {
		return ` {color="` + value + `"}`
	}
	return ""
}
