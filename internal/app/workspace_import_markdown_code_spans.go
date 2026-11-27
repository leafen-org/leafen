package app

import "strings"

// Notion's Markdown import reads a run of three or more backticks (or
// tildes) that a block's words leave open as code going on over the block's
// next lines, closed by a line that is that run, where CommonMark reads that
// line as a fence opening code of its own (seen 2026-11-13): "- [ ] ```",
// "c", "```" is a to-do of code "c", not a to-do "```\nc" and empty code, and
// "- a ```", "c", "```" an item "a" and code "c". A list item opening with a
// fence that nothing in it closes is its words: "- ```", "c" is an item
// "```\nc", where CommonMark reads code to the item's end. A fence opening a
// block's words, closed, is code, as CommonMark reads it.

// workspaceImportMarkdownCodeSpanLines writes a line closing a run left open
// four spaces further in, so it goes on the block's words, and escapes the
// fence of a list item's code that nothing closes.
func workspaceImportMarkdownCodeSpanLines(markdown string) string {
	lines := strings.Split(markdown, "\n")
	open := ""
	for index := 0; index < len(lines); index++ {
		prefix := workspaceImportMarkdownQuotePrefix(lines[index])
		body := lines[index][len(prefix):]
		trimmed := strings.TrimLeft(body, " \t")
		if trimmed == "" {
			open = ""
			continue
		}
		item := workspaceImportMarkdownListMarker.MatchString(body)
		// A list item's line starts a block of its own, as CommonMark reads it.
		if open != "" && !item {
			if strings.HasPrefix(trimmed, open) && !strings.HasPrefix(trimmed[len(open):], open[:1]) {
				lines[index] = prefix + "    " + body
			}
			open = workspaceImportMarkdownOpenRun(trimmed, open)
			continue
		}
		open = ""
		words := trimmed
		if item {
			words = workspaceImportMarkdownItemWords(body)
		}
		if fence := workspaceImportMarkdownFence(words); fence != "" &&
			!(fence[0] == '`' && strings.Contains(words[len(fence):], "`")) {
			at := len(body) - len(words)
			end, closed := workspaceImportMarkdownFenceEnd(lines, index, fence, len(prefix), at)
			switch {
			case closed:
				index = end
			case item:
				lines[index] = prefix + body[:at] + `\` + body[at:]
				open = workspaceImportMarkdownOpenRun(words[1:], "")
			default:
				// Code nothing closes goes on to the end, as CommonMark reads it.
				return strings.Join(lines, "\n")
			}
			continue
		}
		open = workspaceImportMarkdownOpenRun(words, "")
	}
	return strings.Join(lines, "\n")
}

// workspaceImportMarkdownQuotePrefix is a line's quote markers.
func workspaceImportMarkdownQuotePrefix(line string) string {
	at := 0
	for marker := workspaceImportMarkdownQuoteMarker.FindString(line); marker != ""; marker = workspaceImportMarkdownQuoteMarker.FindString(line[at:]) {
		at += len(marker)
	}
	return line[:at]
}

// workspaceImportMarkdownFenceEnd is the line closing the code a fence opens
// on a line, the fence standing at column at past quote markers as long as
// quoted: the line that is a fence of its kind at least as long, with the
// code's own quote markers; none when a line left of the fence or outside
// its quote ends the block holding the code first.
func workspaceImportMarkdownFenceEnd(lines []string, index int, fence string, quoted, at int) (int, bool) {
	for next := index + 1; next < len(lines); next++ {
		prefix := workspaceImportMarkdownQuotePrefix(lines[next])
		body := lines[next][len(prefix):]
		trimmed := strings.TrimRight(strings.TrimLeft(body, " \t"), " \t")
		if len(prefix) != quoted && (quoted > 0 || trimmed != "") {
			return next - 1, false
		}
		if trimmed == "" {
			continue
		}
		if len(body)-len(strings.TrimLeft(body, " \t")) < at && at > 0 {
			return next - 1, false
		}
		if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
			return next, true
		}
	}
	return len(lines) - 1, false
}

// workspaceImportMarkdownOpenRun is the run of three or more backticks or
// tildes a line's words leave open, given the run left open before them: a
// run of backticks opens code its length closes, as a tilde run of three or
// more does here; what code holds opens nothing.
func workspaceImportMarkdownOpenRun(words, open string) string {
	for at := 0; at < len(words); {
		character := words[at]
		if character == '\\' && open == "" {
			at += 2
			continue
		}
		if character != '`' && character != '~' {
			at++
			continue
		}
		end := at
		for end < len(words) && words[end] == character {
			end++
		}
		switch run := words[at:end]; {
		case open == "" && (character == '`' || len(run) >= 3):
			open = run
		case run == open:
			open = ""
		}
		at = end
	}
	if len(open) < 3 {
		return ""
	}
	return open
}
