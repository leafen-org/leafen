package app

import (
	"regexp"
	"slices"
	"strings"
)

// How a "v2" write's indents read inside a text block's tag and after a
// quote's >, put before its lines are read (notionMarkdownVersion2Input).

var (
	notionMarkdownVersion2TextTagLine = regexp.MustCompile(`^<(toggle|quote|p|h[1-4]|li|todo)(?:[ \t]+[^<>]*)?>$`)
	notionMarkdownVersion2QuoteTab    = regexp.MustCompile(`^((?:[ \t]*>)+)[ \t]*\t[ \t]*`)
	notionMarkdownVersion2CalloutLine = regexp.MustCompile(`^<callout(?:[ \t]+[^<>]*)?>$`)
	notionMarkdownVersion2ColumnLine  = regexp.MustCompile(`^<column(?:[ \t]+[^<>]*)?>$`)
	notionMarkdownVersion2ColumnsLine = regexp.MustCompile(`^<columns(?:[ \t]+[^<>]*)?>$`)
)

// notionMarkdownVersion2Indents puts the indents of a "v2" write's lines as
// Notion's "v2" reads them (seen 2026-11-11):
//
//   - In a text block's tag — <toggle>, <quote>, <p>, <h1>…<h4>, <li>,
//     <todo> — its lines read as notionMarkdownVersion2DedentBody puts them.
//   - Whitespace holding a tab after a quote's > goes: "> \tq" and ">\t\tq"
//     are the quote "q", not one holding an indented line — save in a text
//     block's tag in a quote, whose lines read past the > and the space
//     after it as at the top (notionMarkdownVersion2QuotedTags).
//   - In a <callout> its lines read as at the top, as a <details>'s after
//     its title do (notionMarkdownVersion2CalloutIndents): "<callout>\n\tT\n
//     </callout>" and "  T" are the callout "T", "\t- a" and "    - a" hold
//     the item "a", "\t$$…$$⏎⏎\tb" the equation and "b".
//   - In a <details> its title stands as its tag and its blocks as at the
//     top (notionMarkdownVersion2DetailsIndents).
//   - In a <columns>' <column> its lines read as at the top
//     (notionMarkdownVersion2ColumnIndents).
//
// Code is as written.
func notionMarkdownVersion2Indents(lines []string) []string {
	lines = notionMarkdownVersion2QuotedTags(lines)
	fence := ""
	for index := 0; index < len(lines); index++ {
		prefix := notionMarkdownVersion2MarkerRun.FindString(lines[index])
		body := strings.TrimSpace(lines[index][len(prefix):])
		if fence != "" {
			if strings.HasPrefix(body, fence) && strings.Trim(body, "`") == "" {
				fence = ""
			}
			continue
		}
		if notionMarkdownVersion2Fence(body) {
			fence = notionMarkdownFenceOf(body)
			continue
		}
		if match := notionMarkdownVersion2QuoteTab.FindString(lines[index]); match != "" {
			rest := lines[index][len(match):]
			if !notionMarkdownVersion2Bullet.MatchString(rest) && !notionMarkdownVersion2Number.MatchString(rest) {
				lines[index] = strings.TrimRight(match, " \t") + " " + rest
			}
			continue
		}
		if notionMarkdownVersion2DetailsIndents(lines, index) {
			continue
		}
		if notionMarkdownVersion2CalloutLine.MatchString(strings.TrimSpace(lines[index])) {
			notionMarkdownVersion2CalloutIndents(lines, index)
			continue
		}
		if notionMarkdownVersion2ColumnsLine.MatchString(strings.TrimSpace(lines[index])) {
			notionMarkdownVersion2ColumnIndents(lines, index)
			continue
		}
		tag := notionMarkdownVersion2TextTagLine.FindStringSubmatch(strings.TrimSpace(lines[index]))
		if tag == nil {
			continue
		}
		end := notionMarkdownVersion2TagEnd(lines, index, tag[1])
		if end < 0 {
			continue
		}
		read := notionMarkdownVersion2DedentBody(slices.Clone(lines[index+1:end]), notionMarkdownVersion2Lead(lines[index]))
		lines = slices.Concat(lines[:index+1], read, lines[end:])
	}
	return lines
}

// notionMarkdownVersion2TagEnd is the line closing the tag that opens the
// line at start, the same tags opened and closed between counted and code
// passed over; -1 for none.
func notionMarkdownVersion2TagEnd(lines []string, start int, name string) int {
	opening := regexp.MustCompile(`^<` + name + `(?:[ \t]+[^<>]*)?>$`)
	depth, fence := 0, ""
	for index := start + 1; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
		case opening.MatchString(trimmed):
			depth++
		case trimmed == "</"+name+">":
			if depth == 0 {
				return index
			}
			depth--
		}
	}
	return -1
}

// notionMarkdownVersion2DedentBody is the lines of a text block's tag
// between its opening and closing ones (body), the opening one starting with
// lead, as Notion's "v2" reads them (seen 2026-11-14). When more than one of
// them holds anything, the fewest whitespace characters any of them starts
// with past lead go off them all, a tab one as a space is: "<toggle>⏎\tT⏎\tU
// ⏎</toggle>" is "T⏎U", "\tT⏎\t- a⏎  - b" "T" holding two items, "\tT⏎⏎\t\t- a"
// "T" holding the paragraph "- a". What the first line still starts with,
// when it is text — four columns or more of it, or text that starts no block
// — is its text, and so are the lines after it to a blank one, without what
// they start with, while the blocks after the first paragraph they make are
// the tag's too: "\t\tT⏎\t\t- a⏎\t- b" is "\tT⏎- a⏎- b" holding the item
// "b", "\t\tT⏎\t- a⏎\t\t- b" "\tT⏎- a⏎- b" holding "a" holding "b", "  T⏎- a"
// "  T⏎- a" holding "a", "\t\tT⏎\tU⏎\t\t- a" "\tT⏎U⏎- a" alone, "\t\tT⏎\t- a⏎⏎
// \t- b" "\tT⏎- a" holding two items. A first line alone keeps all it starts
// with ("<toggle>⏎  T⏎</toggle>" is "  T"). The reader keeps a first line's
// spaces as its text (notionMarkdownTitleEdges) and its tabs would be
// columns (notionMarkdownVersion2Columns), so they stand in a <span> it
// reads as the text it holds.
func notionMarkdownVersion2DedentBody(body []string, lead string) []string {
	fewest, first, count := -1, -1, 0
	for at, line := range body {
		if strings.TrimSpace(line) == "" {
			continue
		}
		extra := 0
		if strings.HasPrefix(line, lead) {
			extra = len(notionMarkdownVersion2Lead(line[len(lead):]))
		}
		if first < 0 {
			first = at
		}
		if fewest < 0 || extra < fewest {
			fewest = extra
		}
		count++
	}
	if first < 0 || !strings.HasPrefix(body[first], lead) {
		return body
	}
	for at, line := range body {
		if count > 1 && strings.TrimSpace(line) != "" && strings.HasPrefix(line, lead) {
			body[at] = lead + line[len(lead)+fewest:]
		}
	}
	inner := notionMarkdownVersion2Lead(body[first][len(lead):])
	if inner == "" || notionMarkdownVersion2Indent(inner, 0) < 4 &&
		notionMarkdownVersion2FlowStarts(strings.TrimSpace(body[first]), notionMarkdownVersion2NextText(body, first), false) {
		return body
	}
	// The first paragraph ends where a line less than four columns in starts
	// a block, its text where a blank line stands.
	paragraph, text := first+1, first+1
	for paragraph < len(body) {
		trimmed := strings.TrimSpace(body[paragraph])
		if trimmed == "" || notionMarkdownVersion2Indent(strings.TrimPrefix(body[paragraph], lead), 0) < 4 &&
			notionMarkdownVersion2FlowStarts(trimmed, notionMarkdownVersion2NextText(body, paragraph), false) {
			break
		}
		paragraph++
	}
	for text < len(body) && strings.TrimSpace(body[text]) != "" {
		text++
	}
	held := make([]string, 0, text-first)
	for _, line := range body[first:text] {
		held = append(held, strings.TrimPrefix(line, lead))
	}
	title := notionMarkdownVersion2LinesText(held)[len(inner):]
	if strings.Contains(inner, "\t") {
		title = "<span>" + inner + "</span>" + title
	} else {
		title = inner + title
	}
	return slices.Concat(body[:first], []string{lead + title}, body[paragraph:])
}

// notionMarkdownVersion2NextText is what the line after lines[at] holds, or
// nothing.
func notionMarkdownVersion2NextText(lines []string, at int) string {
	if at+1 < len(lines) {
		return strings.TrimSpace(lines[at+1])
	}
	return ""
}

// notionMarkdownVersion2LinesText is lines of text read as one, as Notion's
// "v2" reads a paragraph's (seen 2026-11-14): each after the first without
// the whitespace it starts with, after a line break — save in code a span
// runs over lines with, where a line's end is a space and its whitespace
// stays ("T⏎```⏎    x⏎    ```" is "T⏎" and the code "    x    ",
// notionMarkdownVersion2CodeLines).
func notionMarkdownVersion2LinesText(lines []string) string {
	text := lines[0]
	for at := 1; at < len(lines); at++ {
		if notionMarkdownVersion2SpanRuns(text, lines[at:]) {
			text += " " + lines[at]
			continue
		}
		text = notionMarkdownVersion2Break(text) + "<br>" + strings.TrimLeft(lines[at], " \t")
	}
	return text
}

// notionMarkdownVersion2SpanRuns tells text whose code span, opened and
// left open, the lines after it close.
func notionMarkdownVersion2SpanRuns(text string, after []string) bool {
	for from := 0; ; {
		open, tag, found := notionMarkdownCodeOpen(text, from)
		if !found {
			return false
		}
		if tag {
			from = open + len("<code>")
			continue
		}
		if _, closed := notionMarkdownCodeClosed(text+" "+strings.Join(after, " "), open, false); closed {
			return true
		}
		from = open + len(text[open:]) - len(strings.TrimLeft(text[open:], "`"))
	}
}

// notionMarkdownVersion2QuoteOpening is the quotes' > a line starts with and
// the space after them.
var notionMarkdownVersion2QuoteOpening = regexp.MustCompile(`^(?:[ \t]*>)+ ?`)

// notionMarkdownVersion2QuotedTags puts the lines of a text block's tag
// standing alone on a line in a quote as notionMarkdownVersion2DedentBody
// does at the top, past the quote's > and the space after it (seen
// 2026-11-14: "> <toggle>⏎> \t\tT⏎> \t- a⏎> </toggle>" is a quote holding the
// toggle "\tT⏎- a" holding "a", "> \tT⏎> \tU" the toggle "T⏎U").
func notionMarkdownVersion2QuotedTags(lines []string) []string {
	for index := 0; index < len(lines); index++ {
		marks := notionMarkdownVersion2QuoteOpening.FindString(lines[index])
		if marks == "" || notionMarkdownVersion2TextTagLine.FindString(strings.TrimSpace(lines[index][len(marks):])) == "" {
			continue
		}
		quotes, inner := strings.Count(marks, ">"), []string{}
		for end := index; end < len(lines); end++ {
			held := notionMarkdownVersion2QuoteOpening.FindString(lines[end])
			if strings.Count(held, ">") != quotes {
				break
			}
			inner = append(inner, lines[end][len(held):])
		}
		tag := notionMarkdownVersion2TextTagLine.FindStringSubmatch(strings.TrimSpace(inner[0]))
		end := notionMarkdownVersion2TagEnd(inner, 0, tag[1])
		if end < 0 {
			continue
		}
		read := notionMarkdownVersion2DedentBody(slices.Clone(inner[1:end]), notionMarkdownVersion2Lead(inner[0]))
		for at := range read {
			read[at] = strings.TrimRight(marks, " ") + " " + read[at]
		}
		lines = slices.Concat(lines[:index+1], read, lines[index+end:])
		index += len(read)
	}
	return lines
}
