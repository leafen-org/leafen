package app

import (
	"regexp"
	"strings"
)

// A "v2" text block's tag on one line whose text opens code — "<p>```a</p>",
// a ~~~ fence too, after spaces — holds the code as its block, its own text
// empty, as Notion's "v2" reads it (seen 2026-11-14): "<p>```a</p>" is an
// empty paragraph holding an empty code, "<callout>```a</callout>",
// <quote>'s, <toggle>'s, <todo>'s and a list's <li>'s too, and a heading's
// "<h2>```a</h2>" an empty heading with the code after it. Where a fence on
// a later line closes that code, the code runs to it and its first line
// holds the closing tag, so the tag is left open ("<p>```a</p>⏎b⏎```" says
// "Trailing unclosed …: <p>."). Text before the backticks, ones a backtick
// follows on the line, a <summary>'s, or text after the closing tag leave
// the line as it is (notionMarkdownVersion2BacktickLines,
// notionMarkdownVersion2BrokenPairs).

var notionMarkdownVersion2TagFenceLine = regexp.MustCompile(`^(<(p|h[1-4]|quote|toggle|callout|todo|li)(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*>)[ \t]*((?:` + "```" + `|~~~)[^\n]*)</(p|h[1-4]|quote|toggle|callout|todo|li)>$`)

// notionMarkdownVersion2TagFences puts each such tag's code on lines of its
// own.
func notionMarkdownVersion2TagFences(lines []string) []string {
	out := make([]string, 0, len(lines))
	fence := ""
	for index, line := range lines {
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if notionMarkdownVersion2FenceClosesRun(trimmed, fence) {
				fence = ""
			}
			out = append(out, line)
			continue
		}
		if notionMarkdownVersion2Fence(trimmed) || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:len(trimmed)-len(strings.TrimLeft(trimmed, trimmed[:1]))]
			out = append(out, line)
			continue
		}
		match := notionMarkdownVersion2TagFenceLine.FindStringSubmatch(trimmed)
		if match == nil || match[2] != match[4] || strings.Contains(match[3], "</"+match[2]+">") ||
			!notionMarkdownVersion2Fence(match[3]) && !strings.HasPrefix(match[3], "~~~") {
			out = append(out, line)
			continue
		}
		run := match[3][:len(match[3])-len(strings.TrimLeft(match[3], match[3][:1]))]
		closed := false
		for _, next := range lines[index+1:] {
			if notionMarkdownVersion2FenceClosesRun(strings.TrimSpace(next), run) {
				closed = true
				break
			}
		}
		if closed {
			// The code holds the closing tag, and runs on to its fence.
			out = append(out, lead+match[1], lead+match[3]+"</"+match[4]+">")
			fence = run
			continue
		}
		out = append(out, lead+match[1], lead+match[3], lead+run, lead+"</"+match[4]+">")
	}
	return out
}

// notionMarkdownVersion2FenceClosesRun reports whether a line closes code a
// run of backticks or tildes opened: as many or more of them and nothing else.
func notionMarkdownVersion2FenceClosesRun(trimmed, run string) bool {
	return strings.HasPrefix(trimmed, run) && strings.Trim(trimmed, run[:1]) == ""
}
