package app

import (
	"regexp"
	"slices"
	"strings"
)

// One line can hold a block and the blocks in it — <columns><column>가</column>…,
// <ul><li>하나</li>…</ul> or <toggle>줄<p>안쪽</p></toggle> — which Notion's
// MCP reads as it reads them on lines of their own (its specification says
// nothing of lines). The reader takes a tag a line, so such a line is put on
// lines first (2026-11-07: an agent's one-line columns or list made an empty
// page, a one-line table stayed escaped text, and a one-line synced block lost
// its text). A block that carries text, alone on its line with it, as
// <toggle>글</toggle>, stays as it is; a table cell's or a summary's text stays
// with its tags, as the reader takes them. A table's <thead>, <tbody> and
// <colgroup> stand on lines of their own too.

var notionMarkdownBlockTag = regexp.MustCompile(`<(/?)(p|h[1-4]|quote|toggle|callout|li|todo|column|tab|synced-block|ul|ol|columns|tabs|details|summary|table|thead|tbody|colgroup|col|tr|td|th)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*(/?)>`)

// Blocks whose text is the line between their tags.
var notionMarkdownTextBlockTags = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "quote": true, "toggle": true, "callout": true,
	"li": true, "todo": true, "tab": true, "summary": true, "td": true, "th": true,
}

// Tags whose text stays on their line, whole.
var notionMarkdownInlineBlockTags = map[string]bool{"summary": true, "td": true, "th": true}

// Blocks whose text comes before the blocks they hold, a blank line between.
var notionMarkdownTitledBlockTags = map[string]bool{
	"toggle": true, "callout": true, "quote": true, "li": true, "todo": true, "tab": true,
}

// notionMarkdownOneLineTags puts blocks written on one line on lines of their
// own, outside code.
func notionMarkdownOneLineTags(lines []string) []string {
	out := make([]string, 0, len(lines))
	fence := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			out = append(out, line)
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			fence = notionMarkdownFenceOf(trimmed)
			out = append(out, line)
			continue
		}
		// Tags closed in themselves, more than one on a line, are its text
		// (notionMarkdownSelfClosedRun).
		if notionMarkdownSelfClosedRun(trimmed) {
			out = append(out, line)
			continue
		}
		out = append(out, notionMarkdownSplitBlockTags(line)...)
	}
	return out
}

func notionMarkdownSplitBlockTags(line string) []string {
	// A quote's > stand before each of its lines.
	trimmed := strings.TrimLeft(line, " \t>")
	lead := line[:len(line)-len(trimmed)]
	trimmed = strings.TrimRight(trimmed, " \t")
	// A tag escaped as \<tag> is text, and so is one in inline code (seen
	// 2026-11-14: "`<callout>`⏎</callout>" is the code "<callout>" and the
	// text "</callout>", "<callout>⏎`a</callout>`⏎</callout>" the callout
	// holding the code "a</callout>", in "v1" and "v2") or one an inline
	// equation holds (notion_page_markdown_version2_equation_tags.go).
	codes, equations := notionMarkdownInlineCodeRanges(trimmed), notionMarkdownVersion2EquationRanges(trimmed)
	tags := slices.DeleteFunc(notionMarkdownBlockTag.FindAllStringSubmatchIndex(trimmed, -1), func(match []int) bool {
		return notionMarkdownEscapedAt(trimmed, match[0]) || notionMarkdownInRanges(equations, match[0]) ||
			slices.ContainsFunc(codes, func(code [2]int) bool {
				return code[0] < match[0] && match[0] < code[1]
			})
	})
	if len(tags) == 0 {
		return []string{line}
	}
	name := func(match []int) string { return trimmed[match[4]:match[5]] }
	closing := func(match []int) bool { return match[3] > match[2] }
	selfClosed := func(match []int) bool { return match[9] > match[8] }
	// A tag alone on its line, or a block with its text between its tags. A
	// toggle opened with its title on its line stays so: the toggle reading
	// takes its blocks below it, and refuses it unclosed (notionMarkdownToggleBlocks).
	if len(tags) == 1 && tags[0][0] == 0 && tags[0][1] == len(trimmed) ||
		notionMarkdownOneLineDetailsPattern.MatchString(trimmed) {
		return []string{line}
	}
	if len(tags) == 2 && tags[0][0] == 0 && tags[1][1] == len(trimmed) && !closing(tags[0]) && closing(tags[1]) &&
		name(tags[0]) == name(tags[1]) && notionMarkdownTextBlockTags[name(tags[0])] {
		return []string{line}
	}
	parts := []string{}
	open := []string{}
	titled := false
	text := func(value string) {
		if value = strings.TrimSpace(value); value == "" {
			return
		}
		parts = append(parts, lead+value)
		titled = len(open) > 0 && notionMarkdownTitledBlockTags[open[len(open)-1]]
	}
	at := 0
	for index := 0; index < len(tags); index++ {
		match := tags[index]
		text(trimmed[at:match[0]])
		tag, end := trimmed[match[0]:match[1]], match[1]
		// A <p> and its text, closed at once, stand on a line of their own
		// with the spaces at its ends (seen 2026-11-13: a callout of one
		// line, "<callout><p>  a  </p></callout>", is the callout "  a  ").
		if next := index + 1; name(match) == "p" && !closing(match) && !selfClosed(match) && next < len(tags) &&
			closing(tags[next]) && name(tags[next]) == "p" {
			if titled {
				parts = append(parts, "")
			}
			parts = append(parts, lead+trimmed[match[0]:tags[next][1]])
			at, titled, index = tags[next][1], false, next
			continue
		}
		if !closing(match) && !selfClosed(match) && notionMarkdownInlineBlockTags[name(match)] {
			if until := strings.Index(trimmed[end:], "</"+name(match)+">"); until >= 0 {
				end += until + len("</"+name(match)+">")
				parts = append(parts, lead+trimmed[match[0]:end])
				for index+1 < len(tags) && tags[index+1][0] < end {
					index++
				}
				at, titled = end, false
				continue
			}
		}
		switch {
		case closing(match):
			if len(open) > 0 {
				open = open[:len(open)-1]
			}
			titled = false
		case !selfClosed(match):
			if titled {
				parts = append(parts, "")
			}
			open = append(open, name(match))
			titled = false
		}
		parts = append(parts, lead+tag)
		at = end
	}
	text(trimmed[at:])
	return parts
}

// notionMarkdownEscapedAt tells a character a backslash escapes.
func notionMarkdownEscapedAt(text string, at int) bool {
	backslashes := 0
	for at > 0 && text[at-1] == '\\' {
		backslashes++
		at--
	}
	return backslashes%2 == 1
}

// notionMarkdownInlineCodeRanges are where a line's inline code stands: its
// spans of backticks and its <code> tags, an escaped one none.
func notionMarkdownInlineCodeRanges(text string) [][2]int {
	if !strings.Contains(text, "`") && !strings.Contains(text, "<code>") {
		return nil
	}
	ranges := [][2]int{}
	for at := 0; at < len(text); {
		switch {
		case text[at] == '\\' && at+1 < len(text) && strings.IndexByte(notionMarkdownLeafenEscapes, text[at+1]) >= 0:
			at += 2
		case text[at] == '`':
			_, end, ok := notionMarkdownCodeSpanAt(text, at)
			if ok {
				ranges = append(ranges, [2]int{at, end})
			}
			at = end
		case strings.HasPrefix(text[at:], "<code>") && strings.Contains(text[at:], "</code>"):
			end := at + strings.Index(text[at:], "</code>") + len("</code>")
			ranges = append(ranges, [2]int{at, end})
			at = end
		default:
			at++
		}
	}
	return ranges
}
