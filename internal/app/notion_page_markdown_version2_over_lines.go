package app

import "strings"

// A "v2" block's opening tag starting a line whose ">" stands on a later
// line — its name ending its line, past spaces or not, or an attribute —
// Notion's "v2" reads as one tag where it starts a block, its line breaks
// and the blank lines between them spaces (seen 2026-11-15, create and
// replace alike): "<callout⏎color=\"red\">x</callout>" is the red callout
// "x", and so are "<callout\t⏎…", "<callout ⏎…", "<callout⏎⏎color=…",
// "<callout⏎icon=…⏎color=…>", "<callout color=…⏎icon=…>",
// "<callout⏎color=…⏎⏎>x</callout>" and "<callout⏎>x</callout>" the callout
// "x"; first in the Markdown, after a blank line, a heading or a line a tag
// starts, past an item's mark or a quote's > ("- <callout⏎  color=…",
// "> <callout⏎> color=…", "> <callout⏎color=…"); a <p>'s, a heading's, a
// toggle's, a quote's, a <details>, an image's, an empty block's, a
// contents' and an <unknown>'s alike ("<unknown⏎url=\"…\"/>" is refused by
// its address as "<unknown url=\"…\"/>" is). One its reading leaves as words
// is its words as written, its lines a paragraph's ("<callout⏎color=\"red\">x
// </callout> y⏎z" is the paragraph of those three lines, "<callout⏎⏎
// color=…>x</callout> y" two paragraphs). Where a line of text it would go
// on with stands before it, Notion refuses it (notionMarkdownMalformedTag)
// — after an
// item's or a quote's text too ("a⏎<callout⏎color=…>x</callout>", "- a⏎
// <callout⏎…", "> a⏎> <callout⏎> …"), said left open first where no closing
// tag of its name follows and it isn't closed in itself ("a⏎<callout⏎
// color=\"red\">x"; "a⏎<unknown⏎url=\"…\"/>" is said malformed alone). An inline tag
// opens no block so ("<b⏎>y</b>" is "<b" and the quote "y</b>"), a tag of no
// name Notion reads is words ("<foo⏎bar=\"1\">x</foo>"), and one whose ">"
// no later line holds is left for the end to read (notionMarkdownVersion2End).

// notionMarkdownVersion2OverTag is a tag put on one line from lines, where
// it stands on its line, as written and as put.
type notionMarkdownVersion2OverTag struct {
	at  int
	tag notionMarkdownVersion2SpacedTag
}

// notionMarkdownVersion2OverLines is lines with each such tag put on its
// first line, each by its line in the lines put, and what Notion refuses of
// those after text, "" for none.
func notionMarkdownVersion2OverLines(lines []string) ([]string, map[int]notionMarkdownVersion2OverTag, string) {
	out, joined, fence := make([]string, 0, len(lines)), map[int]notionMarkdownVersion2OverTag{}, ""
	trailing, malformed := []string{}, []string{}
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		body := strings.TrimSpace(line[len(prefix):])
		if fence != "" || notionMarkdownVersion2Fence(body) || strings.HasPrefix(body, "~~~") {
			if fence == "" {
				fence = body[:len(body)-len(strings.TrimLeft(body, body[:1]))]
			} else if strings.HasPrefix(body, fence) && strings.Trim(body, fence[:1]) == "" {
				fence = ""
			}
			out = append(out, line)
			continue
		}
		at := len(line) - len(strings.TrimLeft(line[len(prefix):], " \t"))
		name, put, rest, used, written := notionMarkdownVersion2OverLineTag(lines, index, at, strings.Count(prefix, ">"))
		if used == 0 {
			out = append(out, line)
			continue
		}
		if len(out) > 0 && notionMarkdownVersion2OverLinesAfterText(out[len(out)-1], prefix) {
			malformed = append(malformed, notionMarkdownMalformedTag(name))
			if !strings.HasSuffix(put, "/>") && !strings.Contains(strings.Join(lines[index+1:], "\n"), "</"+name+">") {
				trailing = append(trailing, name)
			}
			out = append(out, line)
			continue
		}
		joined[len(out)] = notionMarkdownVersion2OverTag{at: at, tag: notionMarkdownVersion2SpacedTag{written: written, put: put}}
		out = append(out, line[:at]+put+rest)
		index += used - 1
	}
	refused := strings.Join(malformed, "\n")
	if len(trailing) > 0 {
		refused = notionMarkdownVersion2TrailingSaid + "<" + strings.Join(trailing, ">, <") + ">.\n" + refused
	}
	return out, joined, refused
}

// notionMarkdownVersion2OverLineTag reads the opening tag of a block's name
// starting lines[index] at byte at, quotes deep, whose ">" stands on a later
// line: its name, the tag put on one line (its line breaks one space, none
// before its ">" or "/>"), what follows it on its last line, how many lines it took
// and its words as written (lines after its first joined by line breaks, by
// a paragraph's break past blank lines); used 0 for none.
func notionMarkdownVersion2OverLineTag(lines []string, index, at, quotes int) (name, put, rest string, used int, written string) {
	line := strings.TrimRight(lines[index], " \t")
	if at >= len(line) {
		return "", "", "", 0, ""
	}
	match := notionMarkdownVersion2TagName.FindStringSubmatch(line[at:])
	if match == nil || notionMarkdownEscapedAt(line, at) {
		return "", "", "", 0, ""
	}
	name = match[1]
	if !notionMarkdownVersion2Tags[name] && name != "details" || notionMarkdownVersion2InlineTags[name] {
		return "", "", "", 0, ""
	}
	after := line[at+len(match[0]):]
	if after != "" && after[0] != ' ' && after[0] != '\t' || strings.ContainsAny(after, "<>") || strings.Count(after, `"`)%2 == 1 {
		return "", "", "", 0, ""
	}
	put, written = line[at:], line[at:]
	blank := false
	for next := index + 1; next < len(lines); next++ {
		text := lines[next]
		for range quotes {
			if trimmed := strings.TrimLeft(text, " \t"); strings.HasPrefix(trimmed, ">") {
				text = trimmed[1:]
			}
		}
		text = strings.TrimSpace(text)
		if text == "" {
			blank = true
			continue
		}
		end, quoted := -1, false
		for position := 0; position < len(text) && end < 0; position++ {
			switch {
			case text[position] == '"':
				quoted = !quoted
			case quoted:
			case text[position] == '<':
				return "", "", "", 0, ""
			case text[position] == '>':
				end = position
			}
		}
		piece := text
		if end >= 0 {
			piece = text[:end+1]
		} else if quoted || notionMarkdownVersion2Fence(text) || strings.HasPrefix(text, "~~~") {
			return "", "", "", 0, ""
		}
		if strings.HasPrefix(piece, ">") || strings.HasPrefix(piece, "/>") {
			put += piece
		} else {
			put += " " + piece
		}
		if blank {
			written += "\n\n" + piece
		} else {
			written += "<br>" + piece
		}
		blank = false
		if end >= 0 {
			return name, put, text[end+1:], next - index + 1, written
		}
	}
	return "", "", "", 0, ""
}

// notionMarkdownVersion2OverLinesAfterText tells a line, the one before a
// tag over lines whose own marks are prefix, of text that the tag's line
// would go on with: a paragraph's, an item's or a quote's text, the tag
// starting no item nor a quote further in.
func notionMarkdownVersion2OverLinesAfterText(before, prefix string) bool {
	beforePrefix := notionMarkdownVersion2MarkerRun.FindString(before)
	if strings.Trim(prefix, " \t>") != "" || strings.Count(prefix, ">") > strings.Count(beforePrefix, ">") {
		return false
	}
	body := strings.TrimSpace(before[len(beforePrefix):])
	return body != "" && !notionMarkdownVersion2Heading.MatchString(body) && !notionMarkdownVersion2Fence(body) &&
		!strings.HasPrefix(body, "~~~") && body != "$$" && !notionMarkdownFlowThematic.MatchString(body) &&
		!notionMarkdownVersion2BlockTag.MatchString(body) && !strings.HasPrefix(body, "|")
}

// notionMarkdownVersion2OverLinesUnknown is a "v2" update's Markdown with
// each <unknown> tag over lines that its reading takes as one on its first
// line, a tab after its name: Notion's update-page check before the reading
// takes it no more than one a tab follows (notionMarkdownUpdateReadUnknownLine;
// seen 2026-11-15: "<unknown⏎url=\"…\"/>" and "<unknown url=\"…\"⏎/>" are
// refused by their address, "<unknown⏎alt=\"a\"/>" as missing its url).
func notionMarkdownVersion2OverLinesUnknown(markdown string) string {
	if !strings.Contains(markdown, "<unknown") {
		return markdown
	}
	lines, over, _ := notionMarkdownVersion2OverLines(strings.Split(markdown, "\n"))
	for index, tag := range over {
		if name := lines[index][tag.at:]; strings.HasPrefix(name, "<unknown") {
			lines[index] = lines[index][:tag.at] + "<unknown\t" + strings.TrimLeft(name[len("<unknown"):], " \t")
		}
	}
	return strings.Join(lines, "\n")
}
