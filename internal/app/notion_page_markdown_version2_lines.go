package app

import (
	"errors"
	"regexp"
	"strings"
)

// How a "v2" write's lines start and end, put the way Leafen's page Markdown
// reader takes them before it reads them (notionMarkdownVersion2Input).

// notionMarkdownVersion2Columns writes the tabs a line starts with as the
// spaces they stand for, to the next of every four columns, as Notion's
// "v2" reads them (seen 2026-11-10: "- a\n\t\t- b" goes on with a's text).
func notionMarkdownVersion2Columns(line string) string {
	lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	if !strings.Contains(lead, "\t") {
		return line
	}
	var spaces strings.Builder
	for _, character := range lead {
		width := 1
		if character == '\t' {
			width = 4 - spaces.Len()%4
		}
		spaces.WriteString(strings.Repeat(" ", width))
	}
	return spaces.String() + line[len(lead):]
}

var (
	notionMarkdownVersion2MarkerRun   = regexp.MustCompile(`^[ \t>]*(?:(?:[-*+]|\d{1,9}[.)])[ \t]+[ \t>]*)*`)
	notionMarkdownVersion2ParenNumber = regexp.MustCompile(`^(\d{1,9})\)(?:[ \t]|$)`)
	notionMarkdownVersion2EmptyItem   = regexp.MustCompile(`^(?:[-*+]|\d{1,9}[.)])$`)
	notionMarkdownVersion2DotRun      = regexp.MustCompile(`^[ \t>]*(?:(?:[-*+]|\d{1,9}\.)[ \t]+[ \t>]*)*`)
)

// notionMarkdownVersion2Unquoted is a line inside code or an equation
// without the > of the quotes it stands in, trimmed.
func notionMarkdownVersion2Unquoted(line string, quotes int) string {
	line = strings.TrimSpace(line)
	for ; quotes > 0 && strings.HasPrefix(line, ">"); quotes-- {
		line = strings.TrimSpace(line[1:])
	}
	return line
}

// notionMarkdownVersion2EquationCloses tells lines after a $$ that stands in
// quotes, quotes deep, that hold a $$ closing it: one that doesn't is text.
func notionMarkdownVersion2EquationCloses(after []string, quotes int) bool {
	for _, line := range after {
		if notionMarkdownVersion2Unquoted(line, quotes) == "$$" {
			return true
		}
	}
	return false
}

// notionMarkdownVersion2EquationEdge tells a blank line of an equation that
// only blank lines stand between and its $$ before or after it.
func notionMarkdownVersion2EquationEdge(lines []string, index, quotes int) bool {
	for _, step := range []int{-1, 1} {
		at := index + step
		for at >= 0 && at < len(lines) && notionMarkdownVersion2Unquoted(lines[at], quotes) == "" {
			at += step
		}
		if at >= 0 && at < len(lines) && notionMarkdownVersion2Unquoted(lines[at], quotes) == "$$" {
			return true
		}
	}
	return false
}

// notionMarkdownVersion2Numbered writes a numbered item's ")" as ".", as
// Leafen's reader takes it, where the item starts a block (seen 2026-11-10:
// "1) a" is an item in "v2", text in "v1"): after text only from 1.
func notionMarkdownVersion2Numbered(line string, afterText bool) string {
	at := len(notionMarkdownVersion2DotRun.FindString(line))
	match := notionMarkdownVersion2ParenNumber.FindStringSubmatchIndex(line[at:])
	if match == nil {
		return line
	}
	starts := strings.TrimLeft(line[:at], " \t>") == ""
	if starts && afterText && !notionMarkdownVersion2FromOne(line[at+match[2]:at+match[3]]) {
		return line
	}
	return line[:at+match[3]] + "." + line[at+match[3]+1:]
}

// notionMarkdownVersion2NumberedTodoMarker is a line whose last list item's
// marker is a number before a to-do's box.
var notionMarkdownVersion2NumberedTodoMarker = regexp.MustCompile(`^([ \t>]*(?:(?:[-*+]|\d{1,9}[.)])[ \t]+[ \t>]*)*?)(\d{1,9}\.)[ \t]+\[[ xX]\](?:[ \t]|$)`)

// notionMarkdownVersion2NumberedTodo writes a numbered item whose text starts
// with a to-do's box as the to-do, as Notion's "v2" reads it (seen
// 2026-11-15: "1. [ ] a", "1) [x] a", "2. [ ] a" and "- 1. [ ] a" hold the
// to-do "a", "1. [ ] a⏎   1. b" one holding the item "b"; "1. [ ]a" and
// "1. [] a" are items; "v1" reads them all as items), its box standing as
// far in as written ("1. " is "-  ").
func notionMarkdownVersion2NumberedTodo(line string) string {
	match := notionMarkdownVersion2NumberedTodoMarker.FindStringSubmatchIndex(line)
	if match == nil {
		return line
	}
	return line[:match[4]] + "-" + strings.Repeat(" ", match[5]-match[4]-1) + line[match[5]:]
}

var notionMarkdownVersion2LongNumber = regexp.MustCompile(`^\d{10,}[.)](?:[ \t]|$)`)

// notionMarkdownVersion2LongNumbered keeps a block's text that starts with a
// number of ten digits or more and a "." or ")" its text, as Notion's "v2"
// reads it, a Markdown item's number being one to nine digits (seen
// 2026-11-11: "1234567890. x" is a paragraph, or the text of the item or
// quote it stands in); Leafen's reader would make an item of it.
func notionMarkdownVersion2LongNumbered(line string) string {
	at := len(notionMarkdownVersion2MarkerRun.FindString(line))
	if !notionMarkdownVersion2LongNumber.MatchString(line[at:]) {
		return line
	}
	mark := at + strings.IndexAny(line[at:], ".)")
	return line[:mark] + `\` + line[mark:]
}

var notionMarkdownVersion2MentionOpen = regexp.MustCompile(`<(mention(?:-[^ \t<>/]*)?)([ \t]+[A-Za-z_][^<>]*|[ \t]*)>`)

// notionMarkdownVersion2DroppedMentions are v1's mentions, which the end
// drops said as read (notionMarkdownVersion2End).
var notionMarkdownVersion2DroppedMentions = map[string]bool{
	"mention-page": true, "mention-user": true, "mention-database": true, "mention-data-source": true,
}

// notionMarkdownVersion2End is a "v2" write's Markdown without what its
// end leaves unfinished, as Notion's reading drops it (seen 2026-11-10): a
// <mention …> its last line doesn't close, with the rest of the line, an
// equation's too, a mention's name it ends in before its > (seen
// 2026-11-15: "x<men" is "x"), and a < it ends in. A < that leaves nothing
// is refused.
// A <mention> of no attribute is no such mention (seen 2026-11-14:
// "$a<mention>b$" is an equation, "a<mention>b" refused unclosed), and v1's
// mentions are the <mention> each becomes ("a<mention-user url=…>b" is "a",
// said as read, and so is "a<mention-database>b", of type="database";
// notionMarkdownVersion1RenamedTag): it names one of v1's it drops. So is a
// tag of any other name after "mention-" holding an attribute (seen
// 2026-11-15, create and replace alike: "x <mention-link url=\"…\">y",
// "x <mention-foo a=\"b\">y" and "x <mention- url=…>y" are "x"), said
// nothing of; one closed in itself, or by "</mention>" or its own closing
// tag after it, and "<mention-link>y" and "<mentionx url=…>y" are text;
// v1's <mention-date> is the <date> it becomes ("x <mention-date
// start=\"…\">y" says its </date> missing).
func notionMarkdownVersion2End(markdown string) (string, string, error) {
	fences := 0
	for _, line := range notionMarkdownVersion2HeldFences(strings.Split(markdown, "\n")) {
		if notionMarkdownVersion2Fence(strings.TrimSpace(line)) {
			fences++
		}
	}
	if fences%2 == 1 {
		return markdown, "", nil
	}
	start, dropped := strings.LastIndexByte(markdown, '\n')+1, ""
	last := notionMarkdownVersion2BacktickRun.ReplaceAllStringFunc(markdown[start:], func(code string) string {
		return strings.Repeat("x", len(code))
	})
	for _, found := range notionMarkdownVersion2MentionOpen.FindAllStringSubmatchIndex(last, -1) {
		if strings.TrimSpace(last[found[4]:found[5]]) == "" && last[found[2]:found[3]] != "mention-database" ||
			last[found[2]:found[3]] == "mention-date" {
			continue
		}
		if !notionMarkdownEscapedAt(last, found[0]) && !strings.HasSuffix(last[found[0]:found[1]], "/>") &&
			!strings.Contains(last[found[1]:], "</mention>") && !strings.Contains(last[found[1]:], "</"+last[found[2]:found[3]]+">") {
			markdown = markdown[:start+found[0]]
			if dropped = last[found[2]:found[3]]; !notionMarkdownVersion2DroppedMentions[dropped] {
				dropped = ""
			}
			break
		}
	}
	// So does a mention's name it ends in before its > (notionMarkdownVersion2MentionOpenerAt).
	if at := notionMarkdownVersion2MentionOpenerAt(last); at >= 0 && start+at < len(markdown) {
		markdown = markdown[:start+at] + "<"
	}
	if strings.HasSuffix(markdown, "<") && !notionMarkdownEscapedAt(markdown, len(markdown)-1) {
		markdown = markdown[:len(markdown)-1]
		if strings.TrimSpace(markdown) == "" {
			return "", "", errors.New(notionMarkdownRefusedHead + "The unfinished opening tag at the end was dropped.")
		}
	}
	return markdown, dropped, nil
}

// notionMarkdownVersion2TildeFences writes code fenced with tildes in
// backticks, a fence longer than any run of them in the code: one that stands
// in quotes or list items too, and one no fence closes, which runs to the end
// as code (seen 2026-11-10: "> ~~~x" is a quote holding code). Code no
// fence closes before the end leaves out the lines of whitespace alone
// that end it (seen 2026-11-14: "```⏎a⏎ ⏎" is "a", "```⏎\t" and "- ```⏎    ⏎"
// code of nothing, "```⏎ ⏎a" " ⏎a"), where code a list item's end closes
// keeps them (notionMarkdownVersion2HeldFences). It tells too the line of the
// fence put in where Notion leaves a tag open, or -1 (notionMarkdownVersion2PairFences).
func notionMarkdownVersion2TildeFences(lines []string) ([]string, int) {
	lines, put := notionMarkdownVersion2HeldFencesPut(lines)
	pairing := notionMarkdownVersion2PairingMasks(lines, put)
	fence, quotes, cell := "", 0, false
	for index := 0; index < len(lines); index++ {
		prefix := notionMarkdownVersion2MarkerRun.FindString(lines[index])
		body := strings.TrimSpace(lines[index][len(prefix):])
		if fence != "" {
			if inside := notionMarkdownVersion2Unquoted(lines[index], quotes); strings.HasPrefix(inside, fence) &&
				strings.Trim(inside, "`") == "" {
				fence = ""
			}
			continue
		}
		// A cell its line leaves open holds the lines to its closing tag.
		if open := max(strings.LastIndex(body, "<td"), strings.LastIndex(body, "<th")); open >= 0 || cell {
			cell = open > max(strings.LastIndex(body, "</td>"), strings.LastIndex(body, "</th>")) ||
				cell && !strings.Contains(body, "</td>") && !strings.Contains(body, "</th>")
		}
		quotes = strings.Count(prefix, ">")
		if notionMarkdownVersion2Fence(body) {
			fence = notionMarkdownFenceOf(body)
			continue
		}
		if !strings.HasPrefix(body, "~~~") {
			continue
		}
		tildes := body[:len(body)-len(strings.TrimLeft(body, "~"))]
		end := index + 1
		for end < len(lines) {
			if inside := notionMarkdownVersion2Unquoted(lines[end], quotes); strings.HasPrefix(inside, tildes) &&
				strings.Trim(inside, "~") == "" {
				break
			}
			end++
		}
		// Tildes a fence closes in a cell are its text (seen 2026-11-14:
		// "<td>⏎~~~⏎b⏎~~~⏎</td>" is the cell "~~~⏎b⏎~~~"), as backticks are its
		// code (notionMarkdownFoldedCells).
		if cell && end < len(lines) {
			index = end
			continue
		}
		backticks := "```"
		for _, line := range lines[index+1 : end] {
			for strings.Contains(line, backticks) {
				backticks += "`"
			}
		}
		// Words holding a backtick, which a backtick fence's can't, name no
		// language Notion knows (notionMarkdownVersion2Fence).
		words := strings.TrimSpace(body[len(tildes):])
		if strings.Contains(words, "`") {
			words = ""
		}
		lines[index] = lines[index][:len(prefix)] + backticks + words
		if end < len(lines) {
			lines[end] = lines[end][:strings.Index(lines[end], tildes)] + backticks
		} else {
			fence = backticks
		}
		index = end
	}
	for fence != "" && len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return notionMarkdownVersion2PairFences(lines, pairing)
}

var (
	notionMarkdownVersion2ItemOpening = regexp.MustCompile(`^([ \t]*<li(?:[ \t]+[^<>]*)?>)[ \t]*(\S.*)$`)
	notionMarkdownVersion2CaptionTag  = regexp.MustCompile(`^<(bookmark|embed|image|file|pdf|video|audio)(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*>$`)
)

// notionMarkdownVersion2ItemLines puts an <li>'s text that stands on its
// tag's line on a line of its own, and an </li> that ends a line of text,
// as Notion's "v2" reads an item's blocks (seen 2026-11-10: "<li>a\n\n1.
// nested</li>" in an <ol> is an item "a" holding a list), and a block's tag
// a list item's text starts with on its own (notionMarkdownVersion2ItemTags);
// code is as written. An </li> closing an <li> that stands in text, after
// words or a to-do's box, stays in that text (seen 2026-11-14: "- [ ] <li>a⏎
// b</li>" and "- x <li>a⏎  b</li>" say the <li> inline alone), and so does
// one that closes no <li> a line before it opened (seen 2026-11-15: "x
// </li>", "- x </li>", "> x </li>", "<p>y </li>⏎z</p>" and "<callout>⏎y
// </li>⏎</callout>" keep "</li>" in their text).
func notionMarkdownVersion2ItemLines(lines []string) []string {
	lines = notionMarkdownVersion2ItemTags(lines)
	out, fence, inText, items := make([]string, 0, len(lines)), "", false, 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
		}
		// In a quote, past its > (notion_page_markdown_version2_flow_marks.go).
		quote := notionMarkdownVersion2QuoteMarks(line)
		rest := line[len(quote):]
		text := strings.TrimSpace(rest)
		held, opened := inText, items
		if fence == "" && !notionMarkdownVersion2Fence(trimmed) {
			inText, items = notionMarkdownVersion2TextItemOpen(text, inText), notionMarkdownVersion2ItemsOpen(text, items)
		}
		switch {
		case fence != "" || notionMarkdownVersion2Fence(trimmed):
		// An escaped one is text (seen 2026-11-14: "a⏎⏎\</li>" is the paragraph "</li>").
		case strings.HasSuffix(text, "</li>") && !strings.Contains(text, "<li") && text != "</li>" && !held && opened > 0 &&
			!notionMarkdownEscapedAt(rest, strings.LastIndex(rest, "</li>")):
			end := strings.LastIndex(rest, "</li>")
			out = append(out, quote+strings.TrimRight(rest[:end], " \t"), quote+rest[:len(rest)-len(strings.TrimLeft(rest, " \t"))]+"</li>")
			continue
		default:
			if match := notionMarkdownVersion2ItemOpening.FindStringSubmatch(rest); match != nil && !strings.HasSuffix(text, "</li>") {
				out = append(out, quote+match[1], quote+rest[:len(rest)-len(strings.TrimLeft(rest, " \t"))]+match[2])
				continue
			}
		}
		out = append(out, line)
	}
	return out
}

// notionMarkdownVersion2ItemsOpen is how many <li> stand open after a
// line's text, open before it: those it opens, outside code and not
// escaped, less those it closes.
func notionMarkdownVersion2ItemsOpen(text string, open int) int {
	codes := notionMarkdownInlineCodeRanges(text)
	for _, found := range notionMarkdownVersion2ListItemTag.FindAllStringSubmatchIndex(text, -1) {
		switch {
		case notionMarkdownEscapedAt(text, found[0]) || notionMarkdownInRanges(codes, found[0]) || strings.HasSuffix(text[found[0]:found[1]], "/>"):
		case found[3] > found[2]:
			open = max(open-1, 0)
		default:
			open++
		}
	}
	return open
}

var notionMarkdownVersion2ListItemTag = regexp.MustCompile(`<(/?)li(?:[ \t][^<>]*)?>`)

// notionMarkdownVersion2TextItemOpen tells, after a line's text, an <li>
// standing in text that no </li> has closed, open telling one before it.
func notionMarkdownVersion2TextItemOpen(text string, open bool) bool {
	for at := strings.IndexByte(text, '<'); at >= 0 && at < len(text); at++ {
		switch {
		case text[at] != '<' || notionMarkdownEscapedAt(text, at):
		case strings.HasPrefix(text[at:], "</li>"):
			open = false
		case at > 0 && notionMarkdownVersion2FlowOpening.MatchString(text[at:]) && strings.HasPrefix(text[at:], "<li"):
			open = notionMarkdownVersion2FlowClosing(text[at:], "li") < 0
		}
	}
	return open
}

// notionMarkdownVersion2Captions says, of a file's or a link's tag opening
// on lines[start] alone, whether its caption to its closing tag, or the end,
// is more than one paragraph, which Notion refuses (seen 2026-11-10).
func notionMarkdownVersion2Captions(lines []string, start int) (string, bool) {
	match := notionMarkdownVersion2CaptionTag.FindStringSubmatch(strings.TrimSpace(lines[start]))
	if match == nil {
		return "", false
	}
	paragraphs, blank := 0, true
	for _, line := range lines[start+1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "</"+match[1]+">" {
			break
		}
		if trimmed != "" && blank {
			paragraphs++
		}
		blank = trimmed == ""
	}
	return "<" + match[1] + "> takes a caption of one paragraph of text.", paragraphs > 1
}

// notionMarkdownVersion2Blockquotes puts a <blockquote> that starts a line of
// its own, its attributes too, and the line of </blockquote> closing it as a
// <quote>'s, as Notion's "v2" reads them (seen 2026-11-10), outside code;
// true when it put any.
func notionMarkdownVersion2Blockquotes(lines []string) bool {
	put, fence := false, ""
	for index := 0; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if fence != "" || notionMarkdownVersion2Fence(trimmed) {
			if fence == "" {
				fence = notionMarkdownFenceOf(trimmed)
			} else if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		}
		if trimmed == "" || notionMarkdownHTMLQuote.FindString(trimmed) != trimmed {
			continue
		}
		for end := index + 1; end < len(lines); end++ {
			if closing := strings.TrimSpace(lines[end]); closing == "</blockquote>" {
				lead := func(line string) string { return line[:len(line)-len(strings.TrimLeft(line, " \t"))] }
				lines[index], lines[end] = lead(lines[index])+"<quote>", lead(lines[end])+"</quote>"
				put = true
				break
			} else if notionMarkdownVersion2Fence(closing) {
				break
			}
		}
	}
	return put
}
