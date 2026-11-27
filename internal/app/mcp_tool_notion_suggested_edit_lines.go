package app

import (
	"slices"
	"strings"
)

// new_text's lines are a block each, one a line, as the tool says: lines of
// text next to each other, which the page's Markdown would read as one
// block's line breaks ("가 줄\n나 줄"), suggest a block each, and a line
// break inside a block is <br>, or a backslash ending the line before one
// that goes on with it, as the page's Markdown writes the two. A blank line
// between lines or none reads the same. Inside a block tag
// (<toggle>…</toggle>) the lines read as the page's Markdown reads them, the
// tag's text and blocks as it holds them; a line a list item's marker width
// in still stands under it, a block inside a block, wherever it stands —
// the line after the first under the block's own line too, and the last
// line, which the block's words after the edit go on (mcpSuggestedUnder).
// A block's tag over
// lines, as fetch writes one ("<p color=\"red\">⏎words⏎</p>",
// "<callout>⏎words⏎</callout>"), is one line: new_text's first and last
// lines are cut where its tags close (mcpSuggestedTagLines).

// mcpSuggestedTagLines is new_text's lines, a block's tag going on over
// lines kept whole with them, up to the line that closes it — or to the
// end, when none does.
func mcpSuggestedTagLines(markdown string) []string {
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0, len(lines))
	depth := 0
	for _, line := range lines {
		if last := len(out) - 1; depth > 0 && last >= 0 {
			out[last] += "\n" + line
		} else {
			out = append(out, line)
		}
		depth = max(depth+mcpSuggestedTagDepth(line), 0)
	}
	return out
}

// mcpSuggestedTagOpening is the name of the block's tag a line opens at its
// start ("<p color=\"red\">" is "p"), "" for none: a closing tag, or a tag
// that isn't a block's, is words.
func mcpSuggestedTagOpening(line string) string {
	line = strings.TrimSpace(line)
	if match := notionMarkdownVersion2BlockTag.FindStringSubmatch(line); match != nil && !strings.HasPrefix(line, "</") {
		return match[1]
	}
	return ""
}

// mcpSuggestedTagWithin is the name of a block's tag a line opens after its
// start that goes on over lines, as a refusal names it.
func mcpSuggestedTagWithin(line string) string {
	first, _, _ := strings.Cut(line, "\n")
	for at := strings.IndexByte(first, '<'); at >= 0; at = strings.IndexByte(first, '<') {
		first = first[at:]
		if name := mcpSuggestedTagOpening(first); name != "" {
			return name
		}
		first = first[1:]
	}
	return "block"
}

// mcpSuggestedBackslashBreaks is new_text with each line a backslash ends,
// outside block tags, going on with the next as a <br>, as the page's
// Markdown reads such a break (notionMarkdownVersion2Break).
func mcpSuggestedBackslashBreaks(markdown string) string {
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0, len(lines))
	depth := 0
	for index, line := range lines {
		last := len(out) - 1
		if words, goes := mcpSuggestedGoesOn(out, lines, index); depth == 0 && goes {
			out[last] = out[last][:len(out[last])-1] + "<br>" + words
		} else {
			out = append(out, line)
		}
		depth = max(depth+mcpSuggestedTagDepth(line), 0)
	}
	return strings.Join(out, "\n")
}

// mcpSuggestedGoesOn is the words of the line at index that go on from the
// line before it, which a backslash ends in a break: the line not blank and
// starting no block — a heading's line is one line — and of a quote's line
// after a quote's, its words after the ">".
func mcpSuggestedGoesOn(out, lines []string, index int) (string, bool) {
	if index == 0 || len(out) == 0 {
		return "", false
	}
	line, trimmed := out[len(out)-1], strings.TrimSpace(lines[index])
	before := strings.TrimSpace(line)
	if trailing := len(line) - len(strings.TrimRight(line, `\`)); trailing%2 == 0 || trimmed == "" ||
		notionMarkdownVersion2Heading.MatchString(before) {
		return "", false
	}
	if strings.HasPrefix(before, ">") && strings.HasPrefix(trimmed, ">") {
		return strings.TrimPrefix(trimmed[1:], " "), true
	}
	next := ""
	if index+1 < len(lines) {
		next = strings.TrimSpace(lines[index+1])
	}
	return trimmed, !notionMarkdownVersion2Starts(trimmed, next, false)
}

// mcpSuggestedOwnLines is new_text's lines with a blank line put between
// every two next to each other outside block tags, so the page's Markdown
// reads each as a block of its own.
func mcpSuggestedOwnLines(markdown string) string {
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0, 2*len(lines))
	depth := 0
	for index, line := range lines {
		if index > 0 && depth == 0 && strings.TrimSpace(line) != "" && strings.TrimSpace(lines[index-1]) != "" {
			out = append(out, "")
		}
		out = append(out, line)
		depth = max(depth+mcpSuggestedTagDepth(line), 0)
	}
	return strings.Join(out, "\n")
}

// mcpSuggestedTagDepth is how many block tags a line leaves open: one for
// each it opens, not closed in itself, less one for each it closes; a tag in
// code or escaped is words.
func mcpSuggestedTagDepth(line string) int {
	line = strings.ReplaceAll(notionMarkdownCodeSpan.ReplaceAllString(line, ""), `\<`, "")
	depth := 0
	for at := strings.IndexByte(line, '<'); at >= 0; at = strings.IndexByte(line, '<') {
		line = line[at:]
		if !notionMarkdownVersion2BlockTag.MatchString(line) {
			line = line[1:]
			continue
		}
		if strings.HasPrefix(line, "</") {
			depth--
			line = line[2:]
			continue
		}
		end := mcpSuggestedTagEnd(line)
		if end < 0 {
			// Its attributes go on over lines: it stands open.
			return depth + 1
		}
		if !strings.HasSuffix(strings.TrimRight(line[:end], " \t"), "/") {
			depth++
		}
		line = line[end+1:]
	}
	return depth
}

// mcpSuggestedTagEnd is where a tag's opening ends, its ">" outside quoted
// attribute values, -1 for none on the line.
func mcpSuggestedTagEnd(tag string) int {
	quote := byte(0)
	for index := 1; index < len(tag); index++ {
		switch char := tag[index]; {
		case quote != 0:
			if char == quote {
				quote = 0
			}
		case char == '"' || char == '\'':
			quote = char
		case char == '>':
			return index
		}
	}
	return -1
}

// mcpSuggestedUnder refuses new_text's lines after its first when the
// page's Markdown, reading each a block of its own (mcpSuggestedOwnLines),
// puts one inside another: a line a list item's marker width in after a list
// item's line ("- 목록⏎  이어짐"), the last line as the lines before it, and
// the line after the first under the block's own line, where the first line
// goes on in a list item, a to-do or a numbered item. At the block's very
// start (before) the lines stand before it, the first a block of its own.
// Markdown the page's reading refuses is left to the reading of the lines.
func mcpSuggestedUnder(kind string, before bool, rest []string, site *notionMarkdownSitePages) error {
	lines := slices.Clone(rest)
	if own := mcpSuggestedOwnMarkers[kind]; !before {
		lines = append([]string{own + "x"}, lines...)
	}
	read, _, err := notionMarkdownVersion2Input(mcpSuggestedOwnLines(strings.Join(lines, "\n")))
	if err != nil {
		return nil
	}
	items, err := parseNotionMarkdown(read, false, site)
	if err != nil {
		return nil
	}
	for _, item := range mcpSuggestedCalloutWords(items) {
		if item.ParentID != "" {
			return mcpSuggestedInsideRefusal()
		}
	}
	return nil
}

// mcpSuggestedOwnMarkers are the markers a block's own line starts with,
// for the kinds whose line a line put a marker's width in stands under.
var mcpSuggestedOwnMarkers = map[string]string{
	graphBlockTypeBulletedListItem: "- ", graphBlockTypeNumberedListItem: "1. ", graphBlockTypeToDo: "- [ ] ",
}

// mcpSuggestedInsideRefusal refuses new_text whose lines put a block inside
// a block.
func mcpSuggestedInsideRefusal() error {
	return mcpInvalid("new_text's lines hold a block inside a block: a suggested edit suggests blocks one after another, each with its words; suggest what goes inside one once it is there")
}
