package app

import "strings"

// A "v2" replace_content's or insert_content's Markdown reads what stands
// between single backticks across lines as one block's code, as Notion's
// update-page does where its create-page reads each line as it stands (seen
// 2026-11-14): the lines between are the code's, its line breaks "<br>"
// ("`a⏎⏎b`" is the code "a<br><br>b"). A code fence's line ends the search,
// as in "v1" (notionMarkdownVersion1CodeAcrossLines), and so does an
// equation's; two backticks open none, nor one no later line closes. An
// escaped backtick, and one inside a <code> tag or an inline equation,
// pairs with none: Notion's update-page pairs those too and merges the
// blocks between into one, which its create-page keeps apart (measured
// 2026-11-26: "a \` b⏎⏎mid⏎⏎c \` d" is one paragraph holding "⏎⏎",
// "- a \` b⏎- c \` d" one item, and "<code>a`b</code>⏎⏎c \` d" one
// paragraph) — so a page's own Markdown, which writes a backtick in text
// escaped and one in code or an equation as it is, written back keeps its
// blocks (an approved difference: data is kept).

// notionMarkdownVersion2CodeLinesInput is a "v2" Markdown update with its
// replace_content's or insert_content's backticks across lines joined, and
// the web addresses it doesn't link escaped (notionMarkdownUpdateUnlinkedAddresses).
func notionMarkdownVersion2CodeLinesInput(input notionPageMarkdownUpdateRequest) notionPageMarkdownUpdateRequest {
	if value := input.ReplaceContent; value != nil && input.Type == "replace_content" {
		replaced := *value
		replaced.NewString = notionMarkdownUpdateUnlinkedAddresses(notionMarkdownVersion2BackticksAcrossLines(value.NewString), true)
		input.ReplaceContent = &replaced
	}
	if value := input.InsertContent; value != nil && input.Type == "insert_content" {
		inserted := *value
		inserted.Content = notionMarkdownUpdateUnlinkedAddresses(notionMarkdownVersion2BackticksAcrossLines(value.Content), true)
		input.InsertContent = &inserted
	}
	return input
}

// notionMarkdownVersion2BackticksAcrossLines joins the lines between single
// backticks that run across them, a line break as <br>.
func notionMarkdownVersion2BackticksAcrossLines(markdown string) string {
	if !strings.Contains(markdown, "`") {
		return markdown
	}
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0, len(lines))
	fence, equation := "", false
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(trimmed, fence, true) {
				fence = ""
			}
			out = append(out, line)
			continue
		case equation:
			equation = trimmed != "$$"
			out = append(out, line)
			continue
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
			out = append(out, line)
			continue
		case trimmed == "$$":
			equation = true
			out = append(out, line)
			continue
		}
		for from := 0; ; {
			singles := notionMarkdownVersion2SingleBackticks(line, from)
			if len(singles)%2 == 0 {
				break
			}
			end, at := -1, 0
			for next := index + 1; next < len(lines) && end < 0; next++ {
				if strings.HasPrefix(strings.TrimSpace(lines[next]), "```") || strings.TrimSpace(lines[next]) == "$$" {
					break
				}
				if found := notionMarkdownVersion2SingleBackticks(lines[next], 0); len(found) > 0 {
					end, at = next, found[0]
				}
			}
			if end < 0 {
				break
			}
			joined := line
			for _, next := range lines[index+1 : end] {
				joined += "<br>" + next
			}
			joined += "<br>" + lines[end][:at+1]
			from = len(joined) + 1
			line, index = joined+lines[end][at+1:], end
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// notionMarkdownVersion2SingleBackticks are where the single backticks that
// may open or close code across lines stand in a line, from a byte on: not
// an escaped one, nor one inside a <code> tag or an inline equation, which
// a page's Markdown writes as they are.
func notionMarkdownVersion2SingleBackticks(line string, from int) []int {
	singles := []int{}
	equations := notionMarkdownVersion2EquationRanges(line)
	for index := from; index < len(line); {
		switch {
		case notionMarkdownInRanges(equations, index):
			index++
		case strings.HasPrefix(line[index:], "<code>") && !notionMarkdownEscapedAt(line, index):
			closing := strings.Index(line[index:], "</code>")
			if closing < 0 {
				return singles
			}
			index += closing + len("</code>")
		case line[index] != '`' || notionMarkdownEscapedAt(line, index):
			index++
		default:
			start := index
			for index < len(line) && line[index] == '`' {
				index++
			}
			if index-start == 1 {
				singles = append(singles, start)
			}
		}
	}
	return singles
}
