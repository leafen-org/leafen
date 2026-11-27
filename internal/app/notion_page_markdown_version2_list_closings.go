package app

import (
	"regexp"
	"slices"
	"strings"
)

// Where a "v2" <ul> or <ol> on a line of its own ends, as Notion's "v2"
// reads it (seen 2026-11-15): at the first line after it holding its
// closing tag, the lists opened in it after it closing first. A line that
// ends in the tag closes the list, the text before it the list's last line
// ("<ul>⏎<li>a</li>⏎x </ul>" holds "x", "<ul>⏎- a⏎x </ul>" is the item
// "a⏎x", "<ul>⏎<li>a</li> </ul>" the item "a"). One with more after the
// tag — "</ul> x", "x </ul> y", "</ul></ul>" — makes no list of it: its
// tags are text, read as a list's tags in text are
// (notionMarkdownListTagMark), its opening tag's line a paragraph's first
// that the lines of text after it go on with ("<ul>⏎x⏎</ul> y" says the
// <ul> cannot stand inline; "<ul>⏎- a⏎</ul> x" is the paragraph "<ul>" and
// the item "a⏎</ul> x"; "x⏎<ul>⏎y⏎</ul> z" the paragraph "x" before it),
// its items bare (but for those it leaves open where what follows ends in
// a bare item, notionMarkdownVersion2EndsInBareItem). A closing tag in code
// or escaped closes nothing, nor
// does another list's ("<ul>⏎<li>a</li>⏎</ol>" leaves the <ul> open, the
// </ol> one of its children); one of a list around it closes it over.

var notionMarkdownVersion2ListTag = regexp.MustCompile(`<(/?)(ul|ol)(?:[ \t][^<>]*)?>`)

// notionMarkdownVersion2ListClosings puts each list's closing tag that
// text stands before on a line of its own, and marks the tags of each list
// its closing tag makes none as text.
func notionMarkdownVersion2ListClosings(lines []string) []string {
	// items are the lines in out of the items a list holds open.
	type list struct {
		line, quotes int
		kind         string
		items        []int
	}
	out := make([]string, 0, len(lines))
	open, fence := []list{}, ""
	// closeAfter are the lines closing code a line of which closed an item
	// (notionMarkdownVersion2EndsInBareItem), after which its closing tag
	// stands for the reading, by the quotes they stand in.
	closeAfter := map[int]string{}
	for index, line := range lines {
		closed := false
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		body := strings.TrimSpace(line[len(prefix):])
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(strings.TrimLeft(strings.TrimSpace(line), " \t>"), fence, true) {
				fence = ""
			}
		case notionMarkdownVersion2Fence(body):
			fence = notionMarkdownFenceOf(body)
		case notionMarkdownVersion2ListLine.MatchString(body):
			open = append(open, list{line: len(out), quotes: strings.Count(prefix, ">"), kind: body[1:3]})
		case len(open) > 0:
			last := open[len(open)-1]
			text := notionMarkdownVersion2Unquoted(line, last.quotes)
			closing := notionMarkdownVersion2ListClosing(text, last.kind)
			if closing == nil {
				// Another list's closing tag alone closes the lists after it.
				if match := notionMarkdownVersion2ListTag.FindStringSubmatch(text); match != nil && match[0] == text && match[1] == "/" {
					if at := slices.IndexFunc(open, func(one list) bool { return one.kind == match[2] }); at >= 0 {
						open = open[:at]
					}
				}
				break
			}
			open, closed = open[:len(open)-1], true
			before, after := strings.TrimSpace(text[:closing[0]]), strings.TrimSpace(text[closing[1]:])
			if after != "" {
				out[last.line] = notionMarkdownVersion2MarkListTag(out[last.line])
				if before == "" {
					line = notionMarkdownVersion2MarkListTag(line)
				}
				// Its items left open that no closing tag after it closes are
				// text where what follows ends in a bare item, a closing tag
				// ending its own line too (notionMarkdownVersion2EndsInBareItem).
				following := lines[index+1:]
				if steps := notionMarkdownVersion2ItemSteps(after); len(steps) > 0 && steps[0] < 0 && strings.HasSuffix(after, "</li>") {
					following = append([]string{after}, following...)
				}
				closes, ends, fenced := notionMarkdownVersion2EndsInBareItem(following, last.quotes, len(last.items))
				if fenced >= 0 {
					closeAfter[index+1+fenced-(len(following)-len(lines[index+1:]))] = strings.Repeat("> ", last.quotes)
				}
				left := last.items[:len(last.items)-closes]
				// Code ending the Markdown closes the innermost as words
				// (notionMarkdownVersion2EndsInCode).
				switch kind, bare := notionMarkdownVersion2EndsInCode(lines[index:], last.quotes); {
				case len(left) == 0 || ends && !bare:
				case kind == "words":
					out[left[len(left)-1]] = notionMarkdownVersion2WordsListTag(out[left[len(left)-1]])
					left, ends = left[:len(left)-1], true
				case kind == "inline":
					ends = true
				}
				for _, item := range left {
					if ends {
						out[item] = notionMarkdownVersion2MarkListTag(out[item])
					}
				}
				break
			}
			if before != "" {
				cut := len(strings.TrimRight(line, " \t")) - (closing[1] - closing[0])
				out = append(out, strings.TrimRight(line[:cut], " \t"))
				line = line[:len(line)-len(strings.TrimLeft(line, " \t>"))] + text[closing[0]:closing[1]]
			}
		}
		// An item the innermost list opens on the line, past those it closes.
		if len(open) > 0 && fence == "" && !closed {
			inner := &open[len(open)-1]
			if text := notionMarkdownVersion2Unquoted(line, inner.quotes); strings.HasPrefix(text, "<li") || strings.Contains(text, "li>") {
				for _, step := range notionMarkdownVersion2ItemSteps(text) {
					switch {
					case step > 0 && strings.HasPrefix(text, "<li"):
						inner.items = append(inner.items, len(out))
					case step < 0 && len(inner.items) > 0:
						inner.items = inner.items[:len(inner.items)-1]
					}
				}
			}
		}
		out = append(out, line)
		if quotes, ok := closeAfter[index]; ok {
			out = append(out, quotes+"</li>")
		}
	}
	return out
}

// notionMarkdownVersion2ListClosing is where the closing tag of a list of
// the kind stands in a line's text, past the lists of its kind the line
// opens, outside code and not escaped, or nil.
func notionMarkdownVersion2ListClosing(text, kind string) []int {
	codes, depth := notionMarkdownInlineCodeRanges(text), 0
	for _, found := range notionMarkdownVersion2ListTag.FindAllStringSubmatchIndex(text, -1) {
		if text[found[4]:found[5]] != kind || notionMarkdownEscapedAt(text, found[0]) ||
			slices.ContainsFunc(codes, func(code [2]int) bool { return code[0] <= found[0] && found[0] < code[1] }) {
			continue
		}
		switch {
		case found[3] == found[2]:
			depth++
		case depth > 0:
			depth--
		case found[1]-found[0] == len("</ul>"):
			return found[:2]
		}
	}
	return nil
}

// notionMarkdownVersion2MarkListTag marks the list tag a line's text starts
// with as text.
func notionMarkdownVersion2MarkListTag(line string) string {
	at := len(notionMarkdownVersion2MarkerRun.FindString(line))
	return line[:at] + notionMarkdownListTagMark + line[at:]
}

// notionMarkdownVersion2TextListOpening tells a line whose text is a list's
// opening tag its list's closing tag made text.
func notionMarkdownVersion2TextListOpening(line string) bool {
	body := strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):])
	rest, ok := strings.CutPrefix(body, notionMarkdownListTagMark)
	return ok && notionMarkdownVersion2ListLine.MatchString(rest)
}

// notionMarkdownVersion2ClosedBeforeText tells a line whose text, past its
// quotes, starts with a block's closing tag followed by more than closing
// tags, which Notion's "v2" reads as text (seen 2026-11-15: "</p> <li>b</li>"
// and "</callout> <li>b</li>" say the <li> cannot stand inline, "</ul>
// <p>x</p>" is that paragraph) — but for an item's, a cell's or a table
// part's, the next standing after it ("</li> <li>b</li>" is an item,
// "</td> <td>b</td>" a cell).
func notionMarkdownVersion2ClosedBeforeText(line string) bool {
	text := strings.TrimLeft(line, " \t>")
	match := notionMarkdownVersion2ClosingTag.FindStringSubmatchIndex(text)
	if match == nil || match[0] != 0 || notionMarkdownVersion2FlowItems[text[match[2]:match[3]]] {
		return false
	}
	rest := strings.TrimSpace(text[match[1]:])
	return rest != "" && !notionMarkdownVersion2ClosingLine.MatchString(rest)
}

// notionMarkdownVersion2FlowItems are the tags of items, cells and a
// table's parts, and a toggle's title.
var notionMarkdownVersion2FlowItems = map[string]bool{
	"li": true, "td": true, "th": true, "tr": true, "thead": true, "tbody": true, "colgroup": true, "col": true,
	"summary": true, "column": true, "tab": true,
}
