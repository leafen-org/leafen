package app

import (
	"regexp"
	"strings"
)

// notionMarkdownVersion2ParagraphTag is a <p> a line starts with, whose text
// is a tag's text (notionMarkdownTitleLines).
var notionMarkdownVersion2ParagraphTag = regexp.MustCompile(`^<p(?:[ \t>/]|$)`)

// A text block's tag on a line of its own — <callout>, <toggle>, <quote>,
// <p>, <h1>…<h4>, <li>, <todo> — reads its text as Notion's "v2" reads a
// paragraph (seen 2026-11-14): a line four columns or more further in than
// the tag's goes on with its text whatever it starts with, its whitespace
// aside — "<callout>⏎T⏎    - a⏎</callout>" is the callout "T⏎- a", "T⏎\t\t- a",
// "T⏎    # h", "T⏎    > q", "T⏎    1. a", "T⏎    ---", "T⏎    $$⏎    x⏎    $$"
// alike, "T⏎U⏎    - a" "T⏎U⏎- a", a <toggle>'s, a <quote>'s, a <p>'s, an
// <h2>'s, an <li>'s and a <todo>'s too — and a line less far in that
// starts a block ends it — a fence and a block's tag so far in too, which
// are its words (notionMarkdownVersion2WrittenLines). A tag holding no text
// first holds its blocks as they read.

// notionMarkdownVersion2TitleLines puts the lines that go on with a text
// block's tag's text on the line before them, joined by a line break.
func notionMarkdownVersion2TitleLines(lines []string) []string {
	out, fence := make([]string, 0, len(lines)), ""
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		out = append(out, line)
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
			continue
		case !notionMarkdownVersion2TextTagLine.MatchString(trimmed) && !notionMarkdownVersion2CalloutLine.MatchString(trimmed):
			continue
		}
		base := notionMarkdownVersion2Indent(line, 0)
		at := index + 1
		for at < len(lines) && strings.TrimSpace(lines[at]) == "" {
			out, at = append(out, lines[at]), at+1
		}
		if at >= len(lines) || notionMarkdownVersion2TitleStarts(strings.TrimSpace(lines[at])) {
			index = at - 1
			continue
		}
		out = append(out, lines[at])
		last := len(out) - 1
		for at++; at < len(lines); at++ {
			text := strings.TrimSpace(lines[at])
			if text == "" {
				break
			}
			if notionMarkdownVersion2Indent(lines[at], 0) >= base+4 && !notionMarkdownVersion2Fence(text) &&
				!notionMarkdownVersion2BlockTag.MatchString(text) {
				out[last] = notionMarkdownVersion2Break(out[last]) + "<br>" + text
				continue
			}
			if notionMarkdownVersion2Starts(text, "", false) || notionMarkdownVersion2Fence(text) || text == "$$" {
				break
			}
			out = append(out, lines[at])
			last = len(out) - 1
		}
		index = at - 1
	}
	return out
}

// notionMarkdownVersion2TitleStarts tells a tag's first line that holds no
// text of the tag's but starts a block (notionMarkdownTitleLines).
func notionMarkdownVersion2TitleStarts(trimmed string) bool {
	if _, _, item := notionMarkdownFlowItem(trimmed); item {
		return true
	}
	return strings.HasPrefix(trimmed, ">") || notionMarkdownVersion2Heading.MatchString(trimmed) ||
		notionMarkdownVersion2Fence(trimmed) || notionMarkdownFlowThematic.MatchString(trimmed) || trimmed == "$$" ||
		notionMarkdownVersion2BlockTag.MatchString(trimmed) && !notionMarkdownVersion2ParagraphTag.MatchString(trimmed)
}
