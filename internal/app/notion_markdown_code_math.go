package app

import "strings"

// Code and equations in a line of Notion's Markdown, which stand whole: what
// they hold is no emphasis, link or tag (seen 2026-11-10, each line written
// with replace_content and read back as blocks).
//
// "v2" and MCP read code as CommonMark's code spans: a run of backticks
// closes at the next run of as many, "``a`b``" code "a`b", a space kept at
// each end dropped where both ends have one and it isn't all spaces ("`` a
// ``" is "a", "` `" a space); a run nothing closes is its backticks as words.
// An equation runs from a $ to the next $ that isn't escaped and that no $
// or digit follows, on its line (seen 2026-11-13: "$a⏎b$" words): "$$x$$" is
// a "$" and an equation "x$", "$a$5" words, "$ $" an equation of a space; it
// holds its words as written, "\$" and all. A $ another follows opens none.
//
// "v1" reads an equation as $…$ or $$…$$ with nothing that would make a word
// on either side of it — no letter, digit, _ or $ before it or after it —
// and no space, tab or line break just inside it: "$a$" and "($a$)" are
// equations, "$a$b", "a$b$", "$ a$" and "$a<br>$" words (seen 2026-11-14).
// $…$ ends at the first $ after it, $$…$$ at the first $$, which may hold a
// $: "$a$b$" is words, "$$a$b$$" the equation "a$b", "$$a$$b$$" words. "$$"
// with nothing on either side is an equation of nothing.

// notionMarkdownCodeSpanAt reads the code span whose backticks start at
// value[index]: its code, where it ends, and whether it is one; when it
// isn't, end is where its run of backticks ends.
func notionMarkdownCodeSpanAt(value string, index int) (string, int, bool) {
	run := len(value[index:]) - len(strings.TrimLeft(value[index:], "`"))
	for at := index + run; at < len(value); {
		next := strings.IndexByte(value[at:], '`')
		if next < 0 {
			break
		}
		start := at + next
		closing := len(value[start:]) - len(strings.TrimLeft(value[start:], "`"))
		if closing != run {
			at = start + closing
			continue
		}
		code := value[index+run : start]
		if len(code) >= 2 && code[0] == ' ' && code[len(code)-1] == ' ' && strings.Trim(code, " ") != "" {
			code = code[1 : len(code)-1]
		}
		return code, start + closing, true
	}
	return "", index + run, false
}

// notionMarkdownVersion2Equation reads the "v2" equation whose $ is at
// value[index]: what it holds as written, where it ends, and whether it is
// one.
func notionMarkdownVersion2Equation(value string, index int) (string, int, bool) {
	if index+1 >= len(value) || value[index+1] == '$' {
		return "", 0, false
	}
	for at := index + 1; at < len(value); at++ {
		switch {
		case value[at] == '\\':
			at++
		case value[at] == '\n':
			return "", 0, false
		case value[at] != '$':
		case at+1 < len(value) && (value[at+1] == '$' || value[at+1] >= '0' && value[at+1] <= '9'):
		default:
			return value[index+1 : at], at + 1, true
		}
	}
	return "", 0, false
}

// notionMarkdownVersion1Equation reads the "v1" equation whose $ is at
// value[index]: what it holds, where it ends, and whether it is one.
func notionMarkdownVersion1Equation(value string, index int) (string, int, bool) {
	word := func(at int) bool {
		if at < 0 || at >= len(value) {
			return false
		}
		character := value[at]
		return character == '$' || character == '_' || character >= '0' && character <= '9' ||
			character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
	}
	space := func(at int) bool {
		return at >= 0 && at < len(value) && (value[at] == ' ' || value[at] == '\t' || value[at] == '\n')
	}
	// "$`…`$", as "v1" writes one, is an equation whatever stands about it
	// (seen 2026-11-11: "b$`x`$ c", "$`x`$b", "$`x`$$`y`$" two, "$``$b"
	// an empty one before "b"), to the first `$ after it, what it holds as
	// written (seen 2026-11-14: "$`a$`$" is "a$", "$`a\$`$" "a\$",
	// "$`a`$`$" "a" before "`$").
	if index+1 < len(value) && value[index+1] == '`' {
		if closing := strings.Index(value[index+2:], "`$"); closing >= 0 {
			end := index + 2 + closing + 1
			return value[index+1 : end], end + 1, true
		}
	}
	if word(index - 1) {
		return "", 0, false
	}
	// $$…$$, a $ inside it too, ends at the first $$ after it; an escaped $
	// in either is a $.
	if strings.HasPrefix(value[index:], "$$") && !space(index+2) {
		if closing := strings.Index(value[min(index+3, len(value)):], "$$"); closing >= 0 {
			end := min(index+3, len(value)) + closing
			if !space(end-1) && !word(end+2) {
				return strings.ReplaceAll(value[index+2:end], `\$`, "$"), end + 2, true
			}
		}
	}
	// $…$ ends at the first $ after it; "$$" holds nothing.
	if space(index + 1) {
		return "", 0, false
	}
	closing := strings.IndexByte(value[index+1:], '$')
	if closing < 0 {
		return "", 0, false
	}
	end := index + 1 + closing
	if closing > 0 && space(end-1) || word(end+1) {
		return "", 0, false
	}
	return strings.ReplaceAll(value[index+1:end], `\$`, "$"), end + 1, true
}

// notionMarkdownEquationEscaped writes an equation between $ marks so it reads
// back as itself as far as it can: a bare $ escaped, an escaped one, "\$",
// as it is, since an equation keeps what it holds as written.
func notionMarkdownEquationEscaped(expression string) string {
	var out strings.Builder
	for index := 0; index < len(expression); index++ {
		switch expression[index] {
		case '\\':
			out.WriteByte('\\')
			if index+1 < len(expression) {
				index++
				out.WriteByte(expression[index])
			}
		case '$':
			out.WriteString(`\$`)
		default:
			out.WriteByte(expression[index])
		}
	}
	return out.String()
}

// notionMarkdownVersion2CodeLines puts each code span a "v2" write's block
// runs over lines with on one line, its line ends spaces, before the lines
// are read (notionMarkdownVersion2Input), as Notion's "v2" and its MCP read
// code (seen 2026-11-13, CommonMark's code spans): "`a⏎b`" is the code
// "a b", "`a  ⏎b`" "a   b" and "`a\⏎b`" "a\ b"; what the next line starts
// with past the block it stands in stays — a paragraph's all of it ("`a⏎
// b`" is "a   b", a tab too), a list item's past its text's column, a
// quote's past its > and one space — and its references and tags are code
// as written ("&amp;", "<b>"). A run of backticks the block's lines don't
// close, or close only past a blank line or a line starting another block,
// is backticks as words, and its lines stay apart. A <code> tag's lines go
// on one with notionMarkdownCodeBreak (notion_markdown_code_breaks.go). A $
// right before such a span over lines and one right after it are words, as
// Notion's "v2" reads an equation's $ on one line only (seen 2026-11-14:
// "$`a b`$" is an equation, "$`a⏎b`$" "$", the code "a b" and "$").
func notionMarkdownVersion2CodeLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	fence, equation, quotes := "", false, 0
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		body := strings.TrimSpace(line[len(prefix):])
		switch {
		case fence != "":
			if inside := notionMarkdownVersion2Unquoted(line, quotes); strings.HasPrefix(inside, fence) && strings.Trim(inside, "`") == "" {
				fence = ""
			}
		case equation:
			equation = notionMarkdownVersion2Unquoted(line, quotes) != "$$"
		case notionMarkdownVersion2Fence(body):
			fence, quotes = notionMarkdownFenceOf(body), strings.Count(prefix, ">")
		case body == "$$" && notionMarkdownVersion2EquationCloses(lines[index+1:], strings.Count(prefix, ">")):
			equation, quotes = true, strings.Count(prefix, ">")
		case notionMarkdownVersion2TakesCode(strings.TrimSpace(line)):
			joined, used := notionMarkdownVersion2CodeJoined(lines, index, prefix)
			out = append(out, joined)
			index += used - 1
			continue
		}
		out = append(out, line)
	}
	return out
}

// notionMarkdownVersion2TakesCode tells a line holding code whose text a
// next line may go on with: a paragraph's, a list item's, a quote's, or a
// text block's tag's holding text (notion_markdown_code_breaks.go).
func notionMarkdownVersion2TakesCode(trimmed string) bool {
	return trimmed != "" && (strings.Contains(trimmed, "`") || strings.Contains(trimmed, "<code>")) &&
		!notionMarkdownVersion2Heading.MatchString(trimmed) && !notionMarkdownFlowThematic.MatchString(trimmed) &&
		(!notionMarkdownVersion2BlockTag.MatchString(trimmed) || notionMarkdownVersion2CodeTagLine(trimmed)) &&
		!strings.HasPrefix(trimmed, "|")
}

// notionMarkdownVersion2CodeJoined is lines[start] with the lines after it
// that a code span it opens runs over, and how many lines it took.
func notionMarkdownVersion2CodeJoined(lines []string, start int, prefix string) (string, int) {
	quotes := strings.Count(prefix, ">")
	_, _, item := notionMarkdownFlowItem(strings.TrimLeft(strings.TrimLeft(prefix, " \t>"), " \t") + "x")
	column := len(prefix)
	// piece is what a next line adds to the block, or false where it ends;
	// a <code> tag (tag) goes on over a divider's dashes and $$.
	piece := func(line string, tag bool) (string, bool) {
		if strings.TrimSpace(line) == "" {
			return "", false
		}
		held := line
		switch {
		case quotes > 0 && strings.HasPrefix(strings.TrimLeft(line, " \t"), ">"):
			for count := 0; count < quotes; count++ {
				held = strings.TrimLeft(held, " \t")
				if !strings.HasPrefix(held, ">") {
					return "", false
				}
				held = held[1:]
			}
			held = strings.TrimPrefix(held, " ")
		case quotes > 0:
			held = strings.TrimLeft(held, " \t")
		case item:
			// A tab in as one place, as an item a tab in is written (seen
			// 2026-11-14: "- x⏎\t- <code>a⏎\tb</code>" is the code "a⏎b").
			lead := len(held) - len(strings.TrimLeft(held, " \t"))
			held = held[min(lead, column):]
		}
		trimmed := strings.TrimSpace(held)
		if tag && (trimmed == "$$" || notionMarkdownFlowThematic.MatchString(trimmed)) {
			return held, true
		}
		if trimmed == "$$" || notionMarkdownVersion2Fence(trimmed) || strings.HasPrefix(trimmed, ">") ||
			notionMarkdownVersion2Starts(trimmed, "", item) {
			return "", false
		}
		return held, true
	}
	text, used := lines[start], 1
	from := len(prefix)
	for {
		open, tag, found := notionMarkdownCodeOpen(text, from)
		if !found {
			return text, used
		}
		// A code span's line end is a space, a <code> tag's a line break.
		joint := " "
		if tag {
			joint = notionMarkdownCodeBreak
		}
		extended, taken, closed := text, used, false
		for start+taken < len(lines) {
			next, goes := piece(lines[start+taken], tag)
			if !goes {
				break
			}
			extended, taken = extended+joint+next, taken+1
			if _, closed = notionMarkdownCodeClosed(extended, open, tag); closed {
				break
			}
		}
		if !closed {
			// Backticks as words, or a tag left open: what follows on the line.
			from = open + len(text[open:]) - len(strings.TrimLeft(text[open:], "`"))
			if tag {
				from = open + len("<code>")
			}
			continue
		}
		end, _ := notionMarkdownCodeClosed(extended, open, tag)
		if !tag && taken > used {
			// A $ on each side of a code span over lines is words (notionMarkdownVersion2SpanDollars).
			extended, end = notionMarkdownVersion2SpanDollars(extended, open, end)
		}
		text, used, from = extended, taken, end
	}
}
