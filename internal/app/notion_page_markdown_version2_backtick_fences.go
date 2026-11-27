package app

import "strings"

// A "v2" line starting with three or more backticks opens code only when no
// backtick follows them on it, as CommonMark's backtick fences and Notion's
// "v2" read it (seen 2026-11-14): "```a`b", "```a```" and "``` a ` b" are
// text — "```a```" the code "a", "```a`b`" the words "```a" and the code
// "b" — in a paragraph, a list item, a quote or a block's tag alike, going
// on with the text before it ("x⏎```a`b⏎y" is one paragraph) and taking no
// tag's closing tag as code would ("<callout>⏎```a`b⏎</callout>" is the
// callout "```a`b"); a ~~~ fence's words may hold one ("~~~a`b" is code). In
// a fence, such a line is the code's. "v1" reads it as a fence still.

// notionMarkdownVersion2Fence tells a line's text past its blocks' marks that
// opens backtick code.
func notionMarkdownVersion2Fence(body string) bool {
	return strings.HasPrefix(body, "```") && !strings.Contains(strings.TrimLeft(body, "`"), "`")
}

// notionMarkdownVersion2FlowStarts is notionMarkdownFlowStarts for "v2",
// whose backticks that open no code start no block.
func notionMarkdownVersion2FlowStarts(trimmed, next string, afterItem bool) bool {
	if strings.HasPrefix(trimmed, "```") && !notionMarkdownVersion2Fence(trimmed) {
		return false
	}
	return notionMarkdownFlowStarts(trimmed, next, afterItem)
}

// notionMarkdownVersion2BacktickLines writes each text block's line in out
// (inline) whose text starts with backticks that open no code so that
// Leafen's page Markdown, which takes any such line for a fence, reads it as
// that text: the code span they open, closed by as many backticks on the
// line, between one or two backticks the code doesn't hold a run of (or a
// <code> tag), and backticks nothing closes as words.
func notionMarkdownVersion2BacktickLines(out []string, inline map[int]string) []string {
	for index := range inline {
		line := out[index]
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		at := len(line) - len(strings.TrimLeft(line[len(prefix):], " \t"))
		// A text block's tag's text too ("<p>```a`b</p>").
		if match := notionMarkdownVersion2TextTagOpening.FindStringSubmatch(line[at:]); match != nil {
			at = len(line) - len(match[3])
		}
		text := line[at:]
		// Words as written stand after a mark (notionMarkdownVersion2WrittenLines).
		if strings.HasPrefix(text, notionMarkdownEntitySpace+"```") {
			text = strings.ReplaceAll(text, notionMarkdownEntitySpace, "")
		}
		if !strings.HasPrefix(text, "```") {
			continue
		}
		// A span over lines stands on one by now (notionMarkdownVersion2CodeLines).
		code, end, closed := notionMarkdownCodeSpanAt(text, 0)
		if !closed {
			out[index] = line[:at] + strings.Repeat("\\`", end) + text[end:]
			continue
		}
		out[index] = line[:at] + notionMarkdownVersion2CodeSpan(code) + text[end:]
	}
	return out
}

// notionMarkdownVersion2CodeSpan is code written as a span of one or two
// backticks it holds no run of, a space inside each end where its own would
// go or a backtick stands there, or else as a <code> tag.
func notionMarkdownVersion2CodeSpan(code string) string {
	runs := map[int]bool{}
	for at := 0; at < len(code); {
		if code[at] != '`' {
			at++
			continue
		}
		run := len(code[at:]) - len(strings.TrimLeft(code[at:], "`"))
		runs[run], at = true, at+run
	}
	for _, size := range []int{2, 1} {
		if runs[size] {
			continue
		}
		ticks, inner := strings.Repeat("`", size), code
		if strings.HasPrefix(code, "`") || strings.HasSuffix(code, "`") ||
			len(code) >= 2 && code[0] == ' ' && code[len(code)-1] == ' ' && strings.Trim(code, " ") != "" {
			inner = " " + code + " "
		}
		return ticks + inner + ticks
	}
	return "<code>" + strings.ReplaceAll(code, "</code>", notionMarkdownVersion2CodeClose) + "</code>"
}
