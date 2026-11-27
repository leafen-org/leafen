package app

import (
	"regexp"
	"slices"
	"strings"
)

// Where an item's own closing tag stands in the text of a block's tag left
// open there, Notion's "v2" reads its <li> inline, as Notion's API answered
// it (seen 2026-11-15, create and replace alike): "<li>a\</li>⏎<p>y </li> w"
// says the <p> closed as a flow tag, then the <li> left open and that it
// cannot stand inline — a <callout>'s, a heading's or a <p> on a line of its
// own before it alike, with more lines after it too; "…⏎z</p>" after it
// says only the <li>'s two. A block's tag standing in the item's text, not
// opening a line, closes with it, said nothing of ("<li>a\</li>⏎x <p>y
// </li> w" says only that the <li> cannot stand inline). An item in a list
// its closing tag made no list (notionMarkdownVersion2ListClosings) closes
// over nothing, the blocks after that list's closing tag being left open
// ("<ul>⏎<li>a\</li>⏎</ul> x⏎<p>y </li> w" says "Trailing unclosed …:
// <p>.", then its <li> left open and inline), and reads so where its
// closing tag ends the Markdown too, after a block's tag left open on its
// line or before it: "…⏎</ul> x⏎<p>y </li>", "…⏎<callout>y </li>",
// "…⏎<p>⏎y </li>" and "…⏎<p>y </li> " say the same, the items it is held in
// closed as flow tags and bare first ("<ul>⏎<li>a\</li>⏎<li>b\</li>⏎</ul> x⏎
// <p>y </li>"), "…⏎w <p>y </li>" that its <li> cannot stand inline alone;
// with a line after it, a blank one too, it closes the item as a flow tag
// (notionMarkdownBlockTagsOpen).

// notionMarkdownVersion2ItemHolderTag is a block's tag that may hold an
// item's closing tag in its text, opening a line, and one alone on its line.
var (
	notionMarkdownVersion2ItemHolderTag  = regexp.MustCompile(`^<(p|h[1-4]|callout|toggle|quote)(?:[ \t][^<>]*)?>`)
	notionMarkdownVersion2ItemHolderLine = regexp.MustCompile(`^<(p|h[1-4]|callout|toggle|quote)(?:[ \t][^<>]*)?>$`)
)

// notionMarkdownVersion2ItemHolding is the block's tag whose text holds an
// item's own closing tag, by its name, and the line it opens; name "" for
// none.
type notionMarkdownVersion2ItemHolding struct {
	name string
	line int
}

// notionMarkdownVersion2ItemHolder is the block's tag whose text holds the
// own closing tag of an item opening at lines[start] (end): one opening that
// tag's line, past its marks, or else one alone on a line between, that no
// closing tag of its own closed before it.
func notionMarkdownVersion2ItemHolder(lines []string, start int, end notionMarkdownVersion2ItemEnd) notionMarkdownVersion2ItemHolding {
	if end.line < 0 {
		return notionMarkdownVersion2ItemHolding{line: -1}
	}
	closing := lines[end.line]
	lead := len(closing) - len(strings.TrimLeft(closing[len(notionMarkdownVersion2MarkerRun.FindString(closing)):], " \t"))
	if match := notionMarkdownVersion2ItemHolderTag.FindStringSubmatch(closing[lead:]); match != nil && lead+len(match[0]) <= end.at {
		if !strings.Contains(closing[lead+len(match[0]):end.at], "</"+match[1]+">") {
			return notionMarkdownVersion2ItemHolding{name: match[1], line: end.line}
		}
		return notionMarkdownVersion2ItemHolding{line: -1}
	}
	for at := end.line - 1; at > start; at-- {
		text := strings.TrimSpace(lines[at][len(notionMarkdownVersion2MarkerRun.FindString(lines[at])):])
		if match := notionMarkdownVersion2ItemHolderLine.FindStringSubmatch(text); match != nil {
			closed := slices.ContainsFunc(lines[at+1:end.line+1], func(line string) bool { return strings.Contains(line, "</"+match[1]+">") })
			if closed && !strings.Contains(closing[:end.at], "</"+match[1]+">") {
				closed = false
			}
			if !closed {
				return notionMarkdownVersion2ItemHolding{name: match[1], line: at}
			}
		}
	}
	return notionMarkdownVersion2ItemHolding{line: -1}
}

// notionMarkdownVersion2ItemEndsInline tells an item in a list made text
// whose own closing tag, ending its line, ends the Markdown after a block's
// tag left open: holding it, or standing before it on its line.
func notionMarkdownVersion2ItemEndsInline(lines []string, end notionMarkdownVersion2ItemEnd, holder notionMarkdownVersion2ItemHolding, inText bool) bool {
	if !inText || end.line < 0 || end.line != len(lines)-1 {
		return false
	}
	return holder.name != "" || notionMarkdownVersion2BlockOpenBefore(lines[end.line][:end.at])
}

// notionMarkdownVersion2BlockOpenBefore tells text holding a block's tag no
// closing tag of its own after it closes, outside code and not escaped.
func notionMarkdownVersion2BlockOpenBefore(text string) bool {
	return notionMarkdownVersion2BlockOpenedBefore(text) != ""
}

// notionMarkdownVersion2BlockOpenedBefore is the name of the last such tag,
// "" for none.
func notionMarkdownVersion2BlockOpenedBefore(text string) string {
	codes, open := notionMarkdownInlineCodeRanges(text), []string{}
	for _, found := range notionMarkdownVersion2LeftOpenTag.FindAllStringSubmatchIndex(text, -1) {
		name := text[found[4]:found[5]]
		if found[7] > found[6] || notionMarkdownVersion2InlineTags[name] || notionMarkdownEscapedAt(text, found[0]) ||
			slices.ContainsFunc(codes, func(code [2]int) bool { return code[0] <= found[0] && found[0] < code[1] }) {
			continue
		}
		if found[3] == found[2] {
			open = append(open, name)
		} else if at := slices.Index(open, name); at >= 0 {
			open = slices.Delete(open, at, at+1)
		}
	}
	if len(open) == 0 {
		return ""
	}
	return open[len(open)-1]
}

// notionMarkdownVersion2TextListsAfter is how many lists made text
// (notionMarkdownVersion2ListClosings) stand open after a line, open before it.
func notionMarkdownVersion2TextListsAfter(line string, open int) int {
	if notionMarkdownVersion2TextListOpening(line) {
		return open + 1
	}
	for _, match := range notionMarkdownVersion2ListTag.FindAllStringSubmatch(line, -1) {
		if match[1] == "/" && open > 0 {
			open--
		}
	}
	return open
}

// notionMarkdownVersion2ItemSpan is where an item read inline opens, its
// <li>'s end (fromLine and from), to where its own closing tag stands
// (toLine and to): the block tags opened in it that no closing tag closed
// before it, it closes over (notionMarkdownVersion2LeftOpen). fromLine -1
// for none. left is where the items after it left open to the end stand,
// by line and byte (notionMarkdownVersion2ItemsPast), and read the lines,
// first and last, read as Markdown of their own, whose tags left open are
// said where they stand (notionMarkdownVersion2RestSaid). The list it
// stands in, opening on line lists (-1 for none), and those read so are
// said with it, not on their own (cut).
type notionMarkdownVersion2ItemSpan struct {
	fromLine, from, toLine, to int
	left, read                 [][2]int
	lists                      int
	cut                        bool
}

// listsBefore are the words of lists said (notionMarkdownVersion2FlowLines),
// each opening on its line of at, that the span's lists leave said.
func (span notionMarkdownVersion2ItemSpan) listsBefore(said []string, at []int) []string {
	if !span.cut {
		return said
	}
	kept := []string{}
	for index, words := range said {
		if at[index] != span.lists && !span.reads(at[index]) {
			kept = append(kept, words)
		}
	}
	return kept
}

// reads tells a line standing in lines read as Markdown of their own.
func (span notionMarkdownVersion2ItemSpan) reads(line int) bool {
	return slices.ContainsFunc(span.read, func(lines [2]int) bool { return lines[0] <= line && line <= lines[1] })
}

// holds tells a byte at of lines[line] standing in the span.
func (span notionMarkdownVersion2ItemSpan) holds(line, at int) bool {
	if span.fromLine < 0 || line < span.fromLine || line > span.toLine {
		return false
	}
	return (line > span.fromLine || at >= span.from) && (line < span.toLine || at < span.to)
}
