package app

import (
	"strings"
	"unicode"
)

// The whitespace at the ends of a block's text, as Notion's page Markdown
// writes it (seen 2026-11-14) and as Leafen's reads it back. "v2" writes a
// callout's text that starts or ends with whitespace in a <p> on lines of
// its own ("<callout>⏎<p>⏎  e  ⏎</p>⏎</callout>"), since a callout's own
// line reads without it; its blocks follow after a blank line. Text of
// whitespace alone is written as a block of spaces is.
//
// "v1" ends at its last block's last word, as Notion's does, which drops
// the whitespace a last paragraph's, list item's or to-do's text ends with;
// Leafen's escapes its last space there instead ("e \ "), which Notion's
// "v1" and Leafen's read as that space, so that the Markdown written back
// keeps the text as it was (an approved difference, as the escaped
// whitespace a block's text starts with). A heading's, a quote's and a
// toggle's end whitespace reads as none either way.

// version2CalloutEdges writes a callout whose text has whitespace at an end,
// in "v2", or reports false.
func (w *notionMarkdownWriter) version2CalloutEdges(block workspaceExportMarkdownBlock, prefix, opening, text string) ([]string, bool) {
	if !w.mentions.Version2 || !notionMarkdownEdgeSpace(text) || strings.TrimSpace(text) == "" {
		return nil, false
	}
	w.rendered = append(w.rendered, block.ID)
	w.calloutTextLine(block, text)
	lines := []string{prefix + opening, prefix + "<p>", prefix + w.version2Title(text), prefix + "</p>"}
	if inner := w.flow(w.children[block.ID], prefix); len(inner) > 0 {
		lines = append(append(lines, ""), inner...)
	}
	return append(lines, prefix+notionMarkdownCalloutClose), true
}

// notionMarkdownVersion1EndSpace is "v1" Markdown ended at its last word,
// trimmed, with the whitespace its last line's text ended with in written
// kept, its last space escaped, when that line is the text of a block of
// lastType that reads it so.
func notionMarkdownVersion1EndSpace(trimmed, written, lastType string) string {
	switch lastType {
	case "paragraph", "bulleted_list_item", "bullet", "numbered_list_item", "number", "to_do", "todo":
	default:
		return trimmed
	}
	ended := strings.TrimRightFunc(written, unicode.IsSpace)
	if trimmed == "" || !strings.HasSuffix(ended, trimmed) {
		return trimmed
	}
	tail := written[len(ended):]
	if line := strings.IndexByte(tail, '\n'); line >= 0 {
		tail = tail[:line]
	}
	if tail == "" || strings.TrimFunc(tail, unicode.IsSpace) != "" {
		return trimmed
	}
	return trimmed + tail[:len(tail)-1] + `\` + tail[len(tail)-1:]
}

// notionMarkdownLastBlockType is the type of the block written last.
func notionMarkdownLastBlockType(blocks []workspaceExportMarkdownBlock, rendered []string) string {
	if len(rendered) == 0 {
		return ""
	}
	last := rendered[len(rendered)-1]
	for _, block := range blocks {
		if block.ID == last {
			return block.Type
		}
	}
	return ""
}
