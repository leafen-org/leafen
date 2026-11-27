package app

import "strings"

// A "v1" replace_content's or insert_content's Markdown reads code between
// single backticks across lines, as Notion's update-page does where its
// create-page reads each line as it stands (seen 2026-11-14): "`a⏎b`" is the
// code "a⏎b", "x `a⏎b` y" the text "x " and " y" around it, "`a⏎b⏎c`" one
// code of three lines, the lines in it as written ("`a⏎\tb`" keeps the tab,
// "`a⏎- b`" and "`a⏎# b`" start no block), in an item, a quote or a callout
// too, "$`a⏎b`$" an equation of two lines. A blank line in it goes in
// replace_content and stays in insert_content ("`a⏎⏎b`" is "a⏎b" and
// "a⏎⏎b"). A code fence's line ends the search, the backtick staying text,
// and so do the lines of fenced code and of an equation; two backticks open
// no such code ("``a⏎b``" is two paragraphs). An escaped backtick opens none
// either, where Notion joins its lines into one paragraph of text
// ("\`a⏎b\`", as its own "v1" writes two paragraphs "`a" and "b`"), so a
// page's Markdown written back keeps its blocks.

// notionMarkdownFenceCloses reports whether a line, its indent trimmed,
// closes code its fence opened. In "v2" it is a run of as many backticks or
// more and nothing else; in "v1" any line starting with three backticks
// closes it, what follows them on the line going, a shorter run than its
// fence's too (seen 2026-11-14: "```js⏎a⏎```a`b⏎c⏎```" is the code "a", the
// paragraph "c" and an empty code, "````js⏎a⏎```a`b" and "```js⏎a⏎```py"
// close it too; "x```" and a line of two backticks do not).
func notionMarkdownFenceCloses(trimmed, fence string, version2 bool) bool {
	if !version2 {
		return strings.HasPrefix(trimmed, "```")
	}
	return strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == ""
}

// notionMarkdownVersion1EquationCloses reports whether a line, its indent
// trimmed, closes an equation its $$ opened in "v1": $$ alone, whitespace
// around it aside, and nothing else; anything after it on its line, a color
// mark too, makes it one of the equation's lines (seen 2026-11-15, written
// twice: "$$⏎x⏎$$ {color=\"red\"}" is the equation "x⏎$$ {color=\"red\"}",
// and so are "$$ {_x=\"1\"}", "$$ x", "$$x", "$${color=…}", "$$\t{…}",
// "$$ {}" and "$$ $$", the equation running on to a later $$ or to the end;
// "$$ ", "  $$" and "\t$$" close it). Notion's own "v1" writes a colored
// equation's color after its closing $$, which so reads back as text.
func notionMarkdownVersion1EquationCloses(trimmed string) bool {
	return trimmed == "$$"
}

// notionMarkdownVersion1CodeLinesInput is a "v1" Markdown update with its
// replace_content's or insert_content's code across lines joined, and the
// web addresses it doesn't link escaped (notionMarkdownUpdateUnlinkedAddresses).
func notionMarkdownVersion1CodeLinesInput(input notionPageMarkdownUpdateRequest) notionPageMarkdownUpdateRequest {
	if value := input.ReplaceContent; value != nil && input.Type == "replace_content" {
		replaced := *value
		replaced.NewString = notionMarkdownUpdateUnlinkedAddresses(notionMarkdownVersion1CodeAcrossLines(value.NewString, false), false)
		input.ReplaceContent = &replaced
	}
	if value := input.InsertContent; value != nil && input.Type == "insert_content" {
		inserted := *value
		inserted.Content = notionMarkdownUpdateUnlinkedAddresses(notionMarkdownVersion1CodeAcrossLines(value.Content, true), false)
		input.InsertContent = &inserted
	}
	return input
}

// notionMarkdownVersion1CodeAcrossLines joins the lines of code that runs
// across them, a line break in it as <br>; keepBlank keeps the blank lines
// in it.
func notionMarkdownVersion1CodeAcrossLines(markdown string, keepBlank bool) string {
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
			if notionMarkdownFenceCloses(trimmed, fence, false) {
				fence = ""
			}
			out = append(out, line)
			continue
		case equation:
			equation = !notionMarkdownVersion1EquationCloses(trimmed)
			out = append(out, line)
			continue
		case strings.HasPrefix(trimmed, "```"):
			fence = notionMarkdownFenceOf(trimmed)
			out = append(out, line)
			continue
		case trimmed == "$$":
			equation = true
			out = append(out, line)
			continue
		}
		from := 0
		for {
			open := notionMarkdownVersion1OpenCode(line, from)
			if open < 0 {
				break
			}
			end, at := notionMarkdownVersion1CodeClose(lines, index)
			if end < 0 {
				break
			}
			// A backslash ending a line in it is the break (seen 2026-11-14:
			// "`a\⏎b`" is the code "a⏎b").
			joined := strings.TrimSuffix(line, `\`)
			for _, next := range lines[index+1 : end] {
				if strings.TrimSpace(next) == "" && !keepBlank {
					continue
				}
				joined += "<br>" + strings.TrimSuffix(next, `\`)
			}
			joined += "<br>"
			from = len(joined) + at + 1
			line, index = joined+lines[end], end
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// notionMarkdownVersion1OpenCode finds, in a line from a byte on, a single
// backtick that opens code no backtick on the line closes; -1 for none.
func notionMarkdownVersion1OpenCode(line string, from int) int {
	for index := from; index < len(line); index++ {
		if line[index] == '\\' {
			index++
			continue
		}
		if line[index] != '`' {
			continue
		}
		run := len(line[index:]) - len(strings.TrimLeft(line[index:], "`"))
		if run > 1 {
			// A longer run is code to as many backticks, or backticks.
			if close := notionMarkdownVersion1BacktickRun(line, index+run, run); close >= 0 {
				index = close + run - 1
			} else {
				index += run - 1
			}
			continue
		}
		// Code on the line closes as the line's text reads it
		// (notion_markdown_version1_code_spans.go).
		_, end, closed := notionMarkdownVersion1CodeSpan(line, index)
		if !closed {
			return index
		}
		index = end - 1
	}
	return -1
}

// notionMarkdownVersion1CodeClose finds the line after lines[index] that
// closes its open code, and the byte of the closing backtick in it; -1 for
// none before a code fence or the end.
func notionMarkdownVersion1CodeClose(lines []string, index int) (int, int) {
	for next := index + 1; next < len(lines); next++ {
		if strings.HasPrefix(strings.TrimSpace(lines[next]), "```") {
			return -1, -1
		}
		if at := notionMarkdownVersion1BacktickRun(lines[next], 0, 1); at >= 0 {
			return next, at
		}
	}
	return -1, -1
}

// notionMarkdownVersion1BacktickRun finds, from a byte on, a run of exactly
// that many backticks, a backslash before one as written, as in code; -1
// for none.
func notionMarkdownVersion1BacktickRun(line string, from, size int) int {
	for index := from; index < len(line); index++ {
		if line[index] != '`' {
			continue
		}
		run := len(line[index:]) - len(strings.TrimLeft(line[index:], "`"))
		if run == size {
			return index
		}
		index += run - 1
	}
	return -1
}
