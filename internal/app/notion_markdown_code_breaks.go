package app

import (
	"regexp"
	"strings"
)

// Line breaks in inline code, as Notion's "v2" and its MCP read and write
// them (seen 2026-11-14). A <br> that code holds is its words as written:
// "`a<br>b`" and "<code>a<br />b</code>" are the code "a<br>b" and
// "a<br />b", in a table cell too, while a <br> outside code is a line
// break. A <code> tag that runs over a block's lines holds their line
// breaks: "<code>a⏎b</code>" is the code "a⏎b", what each next line starts
// with past the block it stands in staying — a paragraph's all of it, a
// list item's past its text's column, a quote's past its > and one space —
// and so do the spaces ending a line, a backslash there, a divider's dashes,
// $$, references and tags, as code is its words as written. A blank line, a
// heading, a list item, a quote, a fence or a block's tag ends the block
// there and leaves the tag unclosed. "v1" reads a <br> in code as a line
// break and no <code> tag.
//
// "v2" writes a line break in inline code as one, each line after it as far
// in as the block's own line and a quote's with its > ("> <code>a⏎> b</code>"),
// a heading's text then in its tag ("<h2>⏎<code>a⏎b</code>⏎</h2>"), and a
// table cell's as <br />.

// notionMarkdownCodeBreak stands for a line break in inline code: while a
// "v2" write's lines are read, from where they are joined until the text is
// read (notionMarkdownLineBreaks), and while the writer writes its lines,
// until they are put as Markdown (notionMarkdownCodeBreaksWritten). No text a
// write sends holds the byte.
const notionMarkdownCodeBreak = "\xfc"

// notionMarkdownVersion2TextTagOpening is a text block's tag opening a line,
// with what follows it there.
var notionMarkdownVersion2TextTagOpening = regexp.MustCompile(`^<(p|h[1-4]|quote|toggle|callout)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>(.*)$`)

// notionMarkdownLineBreaks is a block's text with its line breaks read: in
// "v1" (version1) each <br>, a backslash just before it going with it, in
// code and equations too (seen 2026-11-14: "$a\<br>b$" is the equation
// "a⏎b", "$a\\\<br>b$" "a\\⏎b", "`a\<br>b`" the code "a⏎b"), and in "v2"
// each that stands outside code, equations and escapes
// (notionMarkdownWholeRanges) and each line break its code holds.
func notionMarkdownLineBreaks(text string, version1 bool) string {
	if version1 {
		return notionMarkdownVersion1LineBreak.ReplaceAllString(text, "\n")
	}
	var out strings.Builder
	at := 0
	for _, whole := range notionMarkdownWholeRanges(text) {
		out.WriteString(markdownLineBreakPattern.ReplaceAllString(text[at:whole[0]], "\n"))
		out.WriteString(text[whole[0]:whole[1]])
		at = whole[1]
	}
	out.WriteString(markdownLineBreakPattern.ReplaceAllString(text[at:], "\n"))
	return strings.ReplaceAll(out.String(), notionMarkdownCodeBreak, "\n")
}

// notionMarkdownVersion1LineBreak is a <br> as "v1" reads it, with a
// backslash just before it.
var notionMarkdownVersion1LineBreak = regexp.MustCompile(`(?i)\\?<br\s*/?>`)

// notionMarkdownCodeOpen is where the first code from text[from] stands
// that its line doesn't close — a run of backticks, or a <code> tag (tag) —
// and whether there is one; an escaped backtick or tag is none.
func notionMarkdownCodeOpen(text string, from int) (int, bool, bool) {
	for at := from; at < len(text); {
		switch {
		case text[at] == '\\' && at+1 < len(text) && strings.IndexByte(notionMarkdownLeafenEscapes, text[at+1]) >= 0:
			at += 2
		case text[at] == '`':
			_, end, ok := notionMarkdownCodeSpanAt(text, at)
			if !ok {
				return at, false, true
			}
			at = end
		case strings.HasPrefix(text[at:], "<code>"):
			closing := strings.Index(text[at:], "</code>")
			if closing < 0 {
				return at, true, true
			}
			at += closing + len("</code>")
		default:
			at++
		}
	}
	return 0, false, false
}

// notionMarkdownCodeClosed is where the code opening at text[open] ends —
// a code span's backticks or a <code> tag (tag) — and whether text closes
// it.
func notionMarkdownCodeClosed(text string, open int, tag bool) (int, bool) {
	if !tag {
		_, end, ok := notionMarkdownCodeSpanAt(text, open)
		return end, ok
	}
	closing := strings.Index(text[open:], "</code>")
	if closing < 0 {
		return 0, false
	}
	return open + closing + len("</code>"), true
}

// notionMarkdownVersion2CodeTagLine tells a block's tag holding text on its
// line that may go on over the next ones: "<p><code>a".
func notionMarkdownVersion2CodeTagLine(trimmed string) bool {
	match := notionMarkdownVersion2TextTagOpening.FindStringSubmatch(trimmed)
	return match != nil && strings.TrimSpace(match[3]) != ""
}

// notionMarkdownVersion2Before is Markdown "v2" writes as a write's lines
// are read before they are joined: the indents its text blocks' tags hold
// as their text's (notionMarkdownVersion2Indents) and each <code> tag that
// runs over lines on one, its line breaks notionMarkdownCodeBreak — the body
// before a change, read as its blocks are matched.
func notionMarkdownVersion2Before(markdown string) string {
	if markdown == "" {
		return markdown
	}
	return strings.Join(notionMarkdownVersion2CodeLines(notionMarkdownVersion2Indents(strings.Split(markdown, "\n"))), "\n")
}

// notionMarkdownEquationLineBreak stands for a line break in an inline
// equation "v1" writes, which Notion's writes as one, the line after it at
// the line's start whatever the block (seen 2026-11-14: "$`a⏎b`$", a
// callout's "\t$`a⏎b`$", a quote's "> $`a⏎b`$", a table cell's
// "<td>$`a⏎b`$</td>"), while its text's are <br>; from where the writer
// writes the span until its Markdown is joined (notionMarkdownCodeBreaksWritten).
// No text holds the byte.
const notionMarkdownEquationLineBreak = "\xfa"

// notionMarkdownCodeBreaksWritten puts the line breaks the writer's
// Markdown holds in inline code as Markdown: each line after one as far in
// as the line it breaks, after its quotes' > marks too; and a "v1"
// equation's as they are (notionMarkdownEquationLineBreak).
func notionMarkdownCodeBreaksWritten(markdown string) string {
	markdown = strings.ReplaceAll(markdown, notionMarkdownEquationLineBreak, "\n")
	if !strings.Contains(markdown, notionMarkdownCodeBreak) {
		return markdown
	}
	lines := strings.Split(markdown, "\n")
	for index, line := range lines {
		if strings.Contains(line, notionMarkdownCodeBreak) {
			lines[index] = strings.ReplaceAll(line, notionMarkdownCodeBreak, "\n"+notionMarkdownCodeBreakLead.FindString(line))
		}
	}
	return strings.Join(lines, "\n")
}

// notionMarkdownCodeBreakLead is how far in a line stands, its quotes' >
// marks too.
var notionMarkdownCodeBreakLead = regexp.MustCompile(`^[ \t]*(?:>[ \t]?)*`)

// notionMarkdownCellLinesJoined is a table cell's lines as one, each line
// break a <br> — but inside a "v2" <code> tag (version2), which holds it, and
// a "v2" code span, where it is a space.
func notionMarkdownCellLinesJoined(texts []string, version2 bool) string {
	joined := ""
	for index, text := range texts {
		if index > 0 {
			joint := "<br>"
			if _, tag, open := notionMarkdownCodeOpen(joined, 0); version2 && open && tag {
				joint = notionMarkdownCodeBreak
			} else if version2 && open {
				joint = "\n"
			}
			joined += joint
		}
		joined += text
	}
	if !strings.Contains(joined, "\n") {
		return joined
	}
	// A line break a code span closed later holds is a space
	// (notionMarkdownFoldedCells), any other a <br>.
	var out strings.Builder
	for at := 0; at < len(joined); {
		switch {
		case joined[at] == '\\' && at+1 < len(joined) && strings.IndexByte(notionMarkdownLeafenEscapes, joined[at+1]) >= 0:
			out.WriteString(joined[at : at+2])
			at += 2
		case joined[at] == '`':
			_, end, ok := notionMarkdownCodeSpanAt(joined, at)
			if ok {
				out.WriteString(strings.ReplaceAll(joined[at:end], "\n", " "))
			} else {
				out.WriteString(joined[at:end])
			}
			at = end
		case joined[at] == '\n':
			out.WriteString("<br>")
			at++
		default:
			out.WriteByte(joined[at])
			at++
		}
	}
	return out.String()
}
