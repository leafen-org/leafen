package app

import (
	"regexp"
	"strings"
)

// Line breaks and inline equations, as Notion's "v2" and its MCP read them
// (seen 2026-11-14). A <br> an inline equation holds is a line break in it:
// "$a<br>b$", "$a<br />b$" and "$a<br  />b$" are the equation "a⏎b", "$a<br>$"
// "a⏎", "$a\<br>b$" "a\⏎b" (the equation keeps what it holds as written),
// while a tag named otherwise, "<BR>" or "<br/ >", is its words there. A
// line break ends any equation, so lines of text joined into one hold none
// that runs over them ("$a⏎b$" is words, notionMarkdownVersion2Break). Code
// holds its <br> as words (notion_markdown_code_breaks.go), and so does an
// escape: "\<br>" is the words "<br>". "v2" writes an equation's line break
// as <br />, as text's.

// notionMarkdownEquationBreak is a <br> inside an equation, which is a line
// break of the equation's.
var notionMarkdownEquationBreak = regexp.MustCompile(`<br\s*/?>`)

// notionMarkdownLineBreakAt is a <br> standing at a text's start.
var notionMarkdownLineBreakAt = regexp.MustCompile(`^(?i)<br\s*/?>`)

// notionMarkdownEquationBreaks is what an equation "v2" reads holds, its
// <br> line breaks.
func notionMarkdownEquationBreaks(expression string) string {
	if !strings.Contains(expression, "<br") {
		return expression
	}
	return notionMarkdownEquationBreak.ReplaceAllString(expression, "\n")
}

// notionMarkdownWholeRanges are where a line of Leafen's page Markdown holds
// what "v2" reads as written, its <br> no line break of the text: an escaped
// character, code spans and <code> tags, and equations, whose <br> are their
// own (notionMarkdownEquationBreaks).
func notionMarkdownWholeRanges(text string) [][2]int {
	if !strings.ContainsAny(text, "`\\$") && !strings.Contains(text, "<code>") {
		return nil
	}
	ranges := [][2]int{}
	for at := 0; at < len(text); {
		end, whole := notionMarkdownWholeAt(text, at)
		if whole {
			ranges = append(ranges, [2]int{at, end})
		}
		at = end
	}
	return ranges
}

// notionMarkdownWholeAt reads what stands at text[at] as "v2" reads a line
// from the left: where it ends, and whether it is one whole — an escaped
// character, code, or an equation (notionMarkdownVersion2Equation). A tag
// stands whole as well, a $ in it opening nothing, though its words are the
// text's.
func notionMarkdownWholeAt(text string, at int) (int, bool) {
	switch {
	case text[at] == '\\' && at+1 < len(text) && strings.IndexByte(notionMarkdownLeafenEscapes, text[at+1]) >= 0:
		return at + 2, true
	case text[at] == '`':
		_, end, ok := notionMarkdownCodeSpanAt(text, at)
		return end, ok
	case strings.HasPrefix(text[at:], "<code>"):
		if closing := strings.Index(text[at:], "</code>"); closing >= 0 {
			return at + closing + len("</code>"), true
		}
		return at + len("<code>"), false
	case text[at] == '$':
		if _, end, ok := notionMarkdownVersion2Equation(text, at); ok {
			return end, true
		}
	case text[at] == '<' && !notionMarkdownLineBreakAt.MatchString(text[at:]):
		if tag := notionMarkdownVersion2TagAt.FindString(text[at:]); tag != "" {
			return at + len(tag), false
		}
	}
	return at + 1, false
}

// notionMarkdownEquationsEnded is a line with each $ it holds outside its
// equations, code and escapes escaped, so that the line a next one joins
// with a <br> opens no equation running over the break.
func notionMarkdownEquationsEnded(line string) string {
	if !strings.Contains(line, "$") {
		return line
	}
	var out strings.Builder
	for at := 0; at < len(line); {
		end, whole := notionMarkdownWholeAt(line, at)
		if !whole && line[at] == '$' {
			out.WriteByte('\\')
		}
		out.WriteString(line[at:end])
		at = end
	}
	return out.String()
}
