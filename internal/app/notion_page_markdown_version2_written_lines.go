package app

import "strings"

// A line four columns or more past where the text before it starts goes on
// with that text whatever it starts with, as Notion's "v2" reads it (seen
// 2026-11-14) — a fence and a block's tag so far in too, which are its words
// ("T⏎    ```⏎    x" is the paragraph "T⏎```⏎x", "T⏎    <callout>⏎    x⏎
// </callout>⏎U" "T⏎<callout>⏎x⏎</callout>⏎U"), in a list item past its
// text's column, in a quote past its > and the space after it, in a block's
// tag alike ("<callout>⏎T⏎    ```⏎    x" is the callout "T⏎```⏎x"); at the
// top, out of tags and lists, a fence four columns in after a blank line is
// words of its own ("T⏎⏎    ```⏎    x⏎    ```" is "T" and the paragraph of
// the code "    x    "), and so is a block's tag ("\t<unknown url=\"…\">x"
// is the paragraph of those words, "\t<li>x</li>" says its <li> cannot
// stand inline, seen 2026-11-15). Code a span runs over such lines with keeps their
// whitespace past where the text starts, their ends spaces ("T⏎    ```⏎
// x⏎    ```" is "T⏎" and the code "    x    ", "- a⏎      ```⏎      x⏎
// ```" the item "a⏎" and the same code, notionMarkdownVersion2LinesText).
// Code less far in starts its block.

// notionMarkdownVersion2WrittenLines puts each such fence or tag, with the
// lines after it so far in, on one line of text after
// notionMarkdownEntitySpace, and each fence's backticks there after one too
// (the reading drops the mark at its end), so that no reading takes them for
// a fence or a block's tag: they read as a tag standing in text does
// (repair.line, "<ul>" refused as inline).
func notionMarkdownVersion2WrittenLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	fence, base, listed, quotes := "", -1, 0, 0
	// opened are the block tags open, by name.
	opened := []string{}
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		marks := notionMarkdownVersion2QuoteOpening.FindString(line)
		inner := line[len(marks):]
		trimmed := strings.TrimSpace(inner)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
				fence = ""
			}
			out = append(out, line)
			continue
		}
		if count := strings.Count(marks, ">"); count != quotes {
			base, quotes = -1, count
		}
		if trimmed == "" {
			out, base = append(out, line), -1
			continue
		}
		// Code a list item's mark opens stands in the item.
		prefix := notionMarkdownVersion2MarkerRun.FindString(line)
		body := strings.TrimSpace(line[len(prefix):])
		opens := notionMarkdownVersion2Fence(body) || strings.HasPrefix(body, "~~~")
		code := opens && strings.Trim(prefix[len(marks):], " \t") == ""
		tag := notionMarkdownVersion2BlockTag.MatchString(trimmed)
		from := base
		if from < 0 && quotes == 0 && len(opened) == 0 && (code || tag) {
			from = listed
		}
		if (code || tag) && from >= 0 && notionMarkdownVersion2Past(inner, from) {
			run := []string{}
			for ; index < len(lines); index++ {
				held := notionMarkdownVersion2QuoteOpening.FindString(lines[index])
				text := lines[index][len(held):]
				if strings.Count(held, ">") != quotes || strings.TrimSpace(text) == "" || !notionMarkdownVersion2Past(text, from) {
					break
				}
				text, _ = notionMarkdownVersion2Strip(text, 0, from)
				run = append(run, notionMarkdownVersion2WrittenFence(text))
			}
			index--
			run[0] = strings.TrimPrefix(strings.TrimLeft(run[0], " \t"), notionMarkdownEntitySpace)
			out = append(out, marks+inner[:len(inner)-len(strings.TrimLeft(inner, " \t"))]+notionMarkdownEntitySpace+
				notionMarkdownVersion2LinesText(run))
			base = max(base, from)
			continue
		}
		out = append(out, line)
		if opens {
			fence, base = notionMarkdownFenceOf(body), -1
			if strings.HasPrefix(body, "~~~") {
				fence = body[:len(body)-len(strings.TrimLeft(body, "~"))]
			}
			continue
		}
		// A tag whose opening is refused is text (notionMarkdownVersion2RefusedOpening),
		// and a closing tag closes only a tag held open (seen 2026-11-15:
		// "<columns>⏎\t<column>⏎\t\tx⏎\t\t</ul>⏎\t</column>⏎\t<column>…" is
		// two columns, the first's paragraph "x⏎</ul>").
		if match := notionMarkdownVersion2BlockTag.FindStringSubmatch(trimmed); match != nil && !strings.Contains(trimmed[1:], "<") &&
			!notionMarkdownVersion2RefusedOpening(trimmed) {
			if strings.HasPrefix(trimmed, "</") {
				for at := len(opened) - 1; at >= 0; at-- {
					if opened[at] == match[1] {
						opened = opened[:at]
						break
					}
				}
			} else if !strings.HasSuffix(trimmed, "/>") {
				opened = append(opened, match[1])
			}
		}
		if base >= 0 && (notionMarkdownVersion2Past(inner, base) || !notionMarkdownVersion2FlowStarts(trimmed, "", false)) {
			continue
		}
		base, listed = notionMarkdownVersion2TextColumn(inner, trimmed, listed, quotes == 0 && len(opened) == 0)
	}
	return out
}

// notionMarkdownVersion2Past tells a line four columns or more past column,
// its tabs counted from there.
func notionMarkdownVersion2Past(line string, column int) bool {
	if notionMarkdownVersion2Indent(line, 0) < column {
		return false
	}
	rest, _ := notionMarkdownVersion2Strip(line, 0, column)
	return notionMarkdownVersion2Indent(rest, 0) >= 4
}

// notionMarkdownVersion2WrittenFence is a line of such text whose fence's
// backticks or tildes stand after notionMarkdownEntitySpace.
func notionMarkdownVersion2WrittenFence(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	if !notionMarkdownVersion2Fence(strings.TrimSpace(trimmed)) && !strings.HasPrefix(trimmed, "~~~") {
		return line
	}
	return line[:len(line)-len(trimmed)] + notionMarkdownEntitySpace + trimmed
}

// notionMarkdownVersion2TextColumn is the column a block's line's text
// starts at — a list item's past its marks, text four columns in at the top
// (top) where blocks start — or -1 for a block holding no text, and the
// column the text of the list it stands in starts at, 0 for none (listed, as
// it stood before). In a tag the lines read past what they all start with
// (notionMarkdownVersion2Indents), so text starts where it stands.
func notionMarkdownVersion2TextColumn(inner, trimmed string, listed int, top bool) (int, int) {
	column := notionMarkdownVersion2Indent(inner, 0)
	if notionMarkdownVersion2Heading.MatchString(trimmed) || notionMarkdownFlowThematic.MatchString(trimmed) || trimmed == "$$" ||
		strings.HasPrefix(trimmed, "|") || notionMarkdownVersion2BlockTag.MatchString(trimmed) && !notionMarkdownVersion2RefusedOpening(trimmed) {
		if column < listed {
			listed = 0
		}
		return -1, listed
	}
	if notionMarkdownVersion2ItemMarker.FindString(trimmed) == "" {
		if column < listed {
			listed = 0
		}
		// Text four columns in starts where blocks start (seen 2026-11-14:
		// "    x⏎    ```" is the paragraph "x⏎```").
		if top && notionMarkdownVersion2Past(inner, listed) {
			return listed, listed
		}
		return column, listed
	}
	for text := trimmed; ; {
		marker := strings.TrimRight(notionMarkdownVersion2ItemMarker.FindString(text), " \t")
		column += len(marker)
		rest := text[len(marker):]
		after, spaces := strings.TrimLeft(rest, " \t"), notionMarkdownVersion2Indent(rest, column)
		if notionMarkdownVersion2ItemMarker.FindString(after) == "" || notionMarkdownFlowThematic.MatchString(after) {
			return column + max(spaces, 1), column + max(spaces, 1)
		}
		column, text = column+spaces, after
	}
}
