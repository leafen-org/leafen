package app

import "strings"

// A code span a "v2" flow tag's text runs over lines with goes on over them
// as Notion's "v2" reads it (seen 2026-11-14, CommonMark's code spans): its
// line ends are spaces, the whitespace around them kept, in a <summary>, a
// <td> and a <th> alike — "<summary>`a⏎b`</summary>" is the code "a b",
// "$`a⏎b`$" "$", the code "a b" and "$", "`a⏎  b`" "a   b", "`a  ⏎b`"
// "a   b", "`a\⏎b`" "a\ b", "`a⏎⏎b`" "a  b"; a next line's whitespace as far
// in as the tag's line stands goes ("> " for a quote's), and backticks that
// no run as long closes, or a backslash escapes, are words. A $ on each side
// of such a span is words, as at the top (notionMarkdownVersion2CodeLines).

// notionMarkdownVersion2FlowCode is a flow tag's pieces, as notionMarkdownVersion2FlowLines
// reads them, with those a code span runs over joined as code: raws are the
// pieces as written, each past the flow's marks and lead.
func notionMarkdownVersion2FlowCode(pieces, raws []string) []string {
	full := strings.Join(raws, "\n")
	spans := notionMarkdownVersion2CodeSpans(full)
	if len(spans) == 0 || len(raws) != len(pieces) {
		return pieces
	}
	// joints are where in the last of out a line end became a space.
	out, at, joints := []string{pieces[0]}, len(raws[0]), []int{}
	for index := 1; index < len(raws); index++ {
		inside := false
		for _, span := range spans {
			inside = inside || span[0] < at && at < span[1]
		}
		last := len(out) - 1
		if !inside {
			out[last] = notionMarkdownVersion2JoinedDollars(out[last], joints)
			out, joints = append(out, pieces[index]), nil
		} else {
			if len(joints) == 0 {
				previous := raws[index-1]
				out[last] += previous[len(strings.TrimRight(previous, " \t")):]
			}
			joints = append(joints, len(out[last]))
			out[last] += " " + raws[index]
		}
		at += 1 + len(raws[index])
	}
	out[len(out)-1] = notionMarkdownVersion2JoinedDollars(out[len(out)-1], joints)
	return out
}

// notionMarkdownVersion2JoinedDollars is text whose code spans running over
// the places joints, line ends become spaces, stand between a $ and a $ as
// words, no equation, as Notion reads them: an equation's $ and $ stand on
// one line (seen 2026-11-14: "$`a b`$" is the equation "`a b`", "$`a⏎b`$"
// "$", the code "a b" and "$").
func notionMarkdownVersion2JoinedDollars(text string, joints []int) string {
	if len(joints) == 0 {
		return text
	}
	spans := notionMarkdownVersion2CodeSpans(text)
	for at := len(spans) - 1; at >= 0; at-- {
		for _, joint := range joints {
			if spans[at][0] < joint && joint < spans[at][1] {
				text, _ = notionMarkdownVersion2SpanDollars(text, spans[at][0], spans[at][1])
				break
			}
		}
	}
	return text
}

// notionMarkdownVersion2SpanDollars is text with the $ right before the code
// span from open to end and the $ right after it written as words, and where
// the span ends then; text as it is when either is missing.
func notionMarkdownVersion2SpanDollars(text string, open, end int) (string, int) {
	if open == 0 || text[open-1] != '$' || end >= len(text) || text[end] != '$' || notionMarkdownEscapedAt(text, open-1) {
		return text, end
	}
	return text[:open-1] + `\$` + text[open:end] + `\$` + text[end+1:], end + 1
}

// notionMarkdownVersion2FlowRaw is a flow's next line as written, past the >
// of its quotes and as much whitespace as lead holds.
func notionMarkdownVersion2FlowRaw(line string, quotes int, lead string) string {
	if quotes > 0 {
		line = line[len(notionMarkdownVersion2QuoteMarks(line)):]
	}
	for taken := 0; taken < len(lead) && line != "" && (line[0] == ' ' || line[0] == '\t'); taken++ {
		line = line[1:]
	}
	return line
}

// notionMarkdownVersion2CodeSpans are where a text's code spans stand, each
// from its opening backticks to past its closing ones: a run of backticks a
// backslash doesn't escape opens one that the next run as long closes, and
// is words when none does.
func notionMarkdownVersion2CodeSpans(text string) [][2]int {
	spans := [][2]int{}
	run := func(at int) int {
		end := at
		for end < len(text) && text[end] == '`' {
			end++
		}
		return end - at
	}
	for at := 0; at < len(text); {
		switch {
		case text[at] == '\\':
			at += 2
			continue
		case text[at] != '`':
			at++
			continue
		}
		length, closed := run(at), -1
		for next := at + length; next < len(text); {
			if text[next] != '`' {
				next++
				continue
			}
			if found := run(next); found == length {
				closed = next
				break
			} else {
				next += found
			}
		}
		if closed < 0 {
			at += length
			continue
		}
		spans = append(spans, [2]int{at, closed + length})
		at = closed + length
	}
	return spans
}
