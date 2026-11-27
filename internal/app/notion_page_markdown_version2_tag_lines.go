package app

import (
	"slices"
	"strings"
)

// notionMarkdownVersion2TagLines puts a text block's tag that runs over
// lines on one, its lines joined by line breaks, as Notion's "v2" and its
// MCP read it (seen 2026-11-14): <p>, <h1>…<h4>, <quote>, <toggle> or
// <callout> holding text on its opening line or on its closing one — "<p>a⏎
// b</p>", "<p>a⏎b⏎</p>", "<p>⏎a⏎b</p>", "<callout>a⏎b</callout>" — is the
// block of text "a⏎b", its lines' ends as a paragraph's (what the lines
// after its first start with goes, the text's edges stay), and in a holder's
// first line its text (notionMarkdownTitleParagraph); so is v1's <details>
// holding text on its opening line, read as the <toggle> it is converted to
// (seen 2026-11-21: "<details>x⏎y⏎</details>" is the toggle "x⏎y"). With a
// blank line or a line starting a block before its closing tag, or text
// starting a block on its opening line, its text stands under its opening
// tag instead, and toggled tells whether a <details> so read as a <toggle>
// (notionMarkdownVersion2SpreadTags); what follows its closing tag leaves it
// as written. A tag on lines of its own, its text between them, is read as
// it stands.
func notionMarkdownVersion2TagLines(lines []string) ([]string, bool) {
	lines, toggled := notionMarkdownVersion2SpreadTags(lines)
	lines = notionMarkdownVersion2BrokenPairs(lines)
	out := make([]string, 0, len(lines))
	fence := ""
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			out = append(out, line)
			continue
		}
		if notionMarkdownVersion2Fence(trimmed) {
			fence = notionMarkdownFenceOf(trimmed)
			out = append(out, line)
			continue
		}
		if joined, used := notionMarkdownVersion2TagJoined(lines, index); used > 0 {
			out = append(out, joined)
			index += used - 1
			continue
		}
		out = append(out, line)
	}
	return out, toggled
}

// notionMarkdownVersion2TagJoined is the text block's tag opening at
// lines[start] on one line, and how many lines it took; none when it stays
// as written.
func notionMarkdownVersion2TagJoined(lines []string, start int) (string, int) {
	line := lines[start]
	lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	opened := strings.TrimRight(line[len(lead):], " \t")
	match := notionMarkdownVersion2TextTagOpening.FindStringSubmatch(opened)
	// A <details> holding its title on its line is the <toggle> it reads as
	// (notion_page_markdown_version2_details_toggles.go).
	if match == nil {
		if match = notionMarkdownVersion2TitledDetails.FindStringSubmatch(opened); match != nil && !notionMarkdownVersion2DetailsTitleStarts(match[3]) {
			match = nil
		}
	}
	if match == nil {
		return "", 0
	}
	opening, closing := line[len(lead):len(lead)+len(match[0])-len(match[3])], "</"+match[1]+">"
	// Text starting a block is that block (notionMarkdownVersion2SpreadTags).
	if notionMarkdownVersion2ClosingOutsideCode(match[3], closing) || strings.Contains(match[3], "<"+match[1]) ||
		notionMarkdownVersion2SpreadStarts(lines, start, match[3]) {
		return "", 0
	}
	bare := strings.TrimSpace(match[3]) == ""
	pieces := []string{}
	if !bare {
		pieces = append(pieces, match[3])
	}
	for end := start + 1; end < len(lines); end++ {
		trimmed := strings.TrimSpace(lines[end])
		if trimmed == closing {
			// On lines of its own it is read as it stands.
			if bare || len(pieces) == 0 {
				return "", 0
			}
			return lead + opening + notionMarkdownVersion2TagText(pieces) + closing, end - start + 1
		}
		// A <summary> is its words there (repair.summary).
		if trimmed == "" || trimmed == "$$" || notionMarkdownVersion2Fence(trimmed) ||
			notionMarkdownVersion2Starts(trimmed, "", false) && !notionMarkdownVersion2SummaryOpening.MatchString(trimmed) {
			return "", 0
		}
		text := strings.TrimRight(lines[end], " \t")
		closed := strings.HasSuffix(text, closing)
		text = strings.TrimSuffix(text, closing)
		// One its own text opens it closes ("<callout>⏎T⏎    <callout>⏎    x⏎
		// </callout>⏎</callout>", notionMarkdownVersion2WrittenLines).
		closed = closed && !notionMarkdownVersion2Opened(text, match[1])
		// A tag closing inside its line, what follows it there, leaves it as written.
		if notionMarkdownVersion2ClosingOutsideCode(text, closing) {
			return "", 0
		}
		// The first line under a bare opening keeps its whitespace, as on
		// lines of its own; the next ones go without what they start with.
		if !(bare && len(pieces) == 0) {
			text = strings.TrimLeft(text, " \t")
		}
		pieces = append(pieces, text)
		if closed {
			return lead + opening + notionMarkdownVersion2TagText(pieces) + closing, end - start + 1
		}
	}
	return "", 0
}

// notionMarkdownVersion2TagText is a tag's lines of text as one, joined by
// line breaks as a paragraph's lines are.
func notionMarkdownVersion2TagText(pieces []string) string {
	text := ""
	for index, piece := range pieces {
		if index > 0 {
			text = notionMarkdownVersion2Break(text) + "<br>"
		}
		text += piece
	}
	return text
}

// notionMarkdownVersion2Opened tells text holding a tag of the name no
// backslash escapes.
func notionMarkdownVersion2Opened(text, name string) bool {
	for at := strings.Index(text, "<"+name); at >= 0; {
		if end := at + 1 + len(name); !notionMarkdownEscapedAt(text, at) && (end == len(text) || strings.ContainsRune(" \t/>", rune(text[end]))) {
			return true
		}
		next := strings.Index(text[at+1:], "<"+name)
		if next < 0 {
			return false
		}
		at += 1 + next
	}
	return false
}

// notionMarkdownVersion2ClosingOutsideCode tells text holding the closing
// tag outside its code: one in code closes nothing, between any number of
// backticks (seen 2026-11-14: "<p>`a</p>`⏎b</p>" is the paragraph of the
// code "a</p>" and "⏎b").
func notionMarkdownVersion2ClosingOutsideCode(text, closing string) bool {
	codes := notionMarkdownInlineCodeRanges(text)
	for at := strings.Index(text, closing); at >= 0; {
		if !slices.ContainsFunc(codes, func(code [2]int) bool { return code[0] <= at && at < code[1] }) {
			return true
		}
		next := strings.Index(text[at+1:], closing)
		if next < 0 {
			return false
		}
		at += 1 + next
	}
	return false
}
