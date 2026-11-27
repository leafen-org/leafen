package app

import (
	"regexp"
	"strings"
	"unicode"
)

// Notion's page Markdown now writes a block that needs attributes or holds
// others as a tag around it (seen 2026-10-29 in its MCP's specification and
// pages read back): the opening tag on a line of its own, the block's text
// on the next, then after a blank line its blocks, not indented, and the
// closing tag — <p color="…">, <h1>…<h4>, <quote>, <toggle heading="h2">,
// <callout>, <li> in <ul> and <ol>, <todo>, <column> in <columns>, <tab> in
// <tabs> and <table> rows with each cell's text on its own line. A list's
// blocks stand a list marker's width in. An empty paragraph is <p />, lines
// of > one after another are one quote, and a media or link block's caption
// is its tag's text. Leafen reads blocks one level in under the block that
// holds them, so this puts Notion's forms that way first; what Leafen wrote
// before reads as it did.

var (
	notionMarkdownContainerOpen   = regexp.MustCompile(`^<(p|h[1-4]|quote|toggle|callout|li|todo|column|tab|equation|bookmark|embed|image|file|pdf|video|audio|synced-block-reference|synced-block|ul|ol|columns|tabs|page|database|link-to-page|button)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>(.*)$`)
	notionMarkdownSelfClosedMedia = regexp.MustCompile(`^<(image|file|pdf|video|audio)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*/>$`)
	notionMarkdownAttributeValue  = regexp.MustCompile(`([a-z-]+)="([^"]*)"`)
	notionMarkdownEmptyParagraph  = regexp.MustCompile(`^<p[ \t]*/>$`)
)

// notionMarkdownContainerForms puts Notion's current forms the way Leafen
// reads blocks: each block's blocks one tab further in.
func notionMarkdownContainerForms(markdown string) string {
	markdown = strings.ReplaceAll(strings.ReplaceAll(markdown, "\r\n", "\n"), "\r", "\n")
	return strings.Join(notionMarkdownFlow(notionMarkdownAPILines(notionMarkdownOneLineTags(strings.Split(markdown, "\n"))), 0), "\n")
}

// notionMarkdownListItem is a list item still open while its blocks follow:
// the column its text starts at and its depth.
type notionMarkdownListItem struct {
	content, depth int
}

// notionMarkdownFlow reads lines of blocks that stand at one depth, nesting
// them as notion_page_markdown_flow_blocks.go tells.
func notionMarkdownFlow(lines []string, depth int) []string {
	out := make([]string, 0, len(lines))
	items := []notionMarkdownListItem{}
	// Code stands as far in as its fence, which the parser takes off its lines:
	// fenceLead is what the fence stands in now, fenceWritten what it stood in.
	// An equation's lines Leafen wrote a tab a level in stand so too, and
	// others as notionMarkdownEquationLines reads them.
	fence, fenceLead, fenceWritten := "", "", ""
	equation := false
	tabs := func(level int) string { return strings.Repeat("\t", level) }
	// open is the line in out whose text a next line may go on with, or -1.
	open := -1
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			out = append(out, fenceLead+strings.TrimPrefix(line, fenceWritten))
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		}
		if equation {
			out, equation = append(out, fenceLead+strings.TrimPrefix(line, fenceWritten)), trimmed != "$$"
			continue
		}
		if trimmed == "" {
			out, open = append(out, ""), -1
			continue
		}
		// What Leafen wrote, a tab for each level, reads as it is, save the
		// tags only the public API's form writes with their blocks a tab in,
		// which read wherever they stand as they do at the top.
		if strings.HasPrefix(line, "\t") {
			if blocks, used := notionMarkdownTabbedContainer(lines, index, depth); used > 0 {
				out, open = append(out, blocks...), -1
				index += used - 1
				continue
			}
			out, open = append(out, tabs(depth)+line), -1
			if strings.HasPrefix(trimmed, "```") {
				fence, fenceLead, fenceWritten = notionMarkdownFenceOf(trimmed), tabs(depth), ""
			} else if notionMarkdownEquationOpens(lines, index) {
				equation, fenceLead, fenceWritten = true, tabs(depth), ""
			}
			continue
		}
		column := len(line) - len(strings.TrimLeft(line, " "))
		for len(items) > 0 && column < items[len(items)-1].content {
			items = items[:len(items)-1]
		}
		base, level := 0, depth
		if len(items) > 0 {
			base, level = items[len(items)-1].content, items[len(items)-1].depth+1
		}
		// Four columns further in than its blocks start, a line goes on with
		// the text before it, or is text as written; save code, and an
		// equation where no text goes on (seen 2026-11-13, "v2": "    $$⏎
		// x⏎    $$" at the top is the equation "    x", "p" over it text).
		if column >= base+4 && !strings.HasPrefix(trimmed, "```") &&
			(open >= 0 || !notionMarkdownEquationOpens(lines, index)) {
			if open >= 0 {
				out[open] = notionMarkdownVersion2Break(out[open]) + "<br>" + trimmed
				continue
			}
			out = append(out, tabs(level)+notionMarkdownFlowLiteral(trimmed))
			open = len(out) - 1
			continue
		}
		open = -1
		if strings.HasPrefix(trimmed, "```") {
			fence, fenceLead, fenceWritten = notionMarkdownFenceOf(trimmed), tabs(level), line[:column]
			out = append(out, tabs(level)+trimmed)
			continue
		}
		if notionMarkdownEquationOpens(lines, index) {
			end := index + 1
			for strings.TrimSpace(lines[end]) != "$$" {
				end++
			}
			out = append(out, tabs(level)+trimmed)
			for _, held := range notionMarkdownEquationLines(lines[index+1:end], line[:base]) {
				out = append(out, tabs(level)+held)
			}
			out = append(out, tabs(level)+"$$")
			index = end
			continue
		}
		// $$ that no later $$ closes starts text of its own, as words, which
		// the lines after it go on with (seen 2026-11-13, "v2": "p⏎$$⏎b" is
		// "p" and "$$⏎b", "- a⏎  $$⏎  b" an item holding "$$⏎b").
		if trimmed == "$$" {
			if open >= 0 {
				out = append(out, "")
			}
			out = append(out, tabs(level)+notionMarkdownFlowLiteral(trimmed))
			open = len(out) - 1
			continue
		}
		// A file's tag closed in itself, as Notion's suggested Markdown writes it.
		if match := notionMarkdownSelfClosedMedia.FindStringSubmatch(trimmed); match != nil {
			trimmed = "<" + match[1] + match[2] + "></" + match[1] + ">"
			lines[index] = line[:column] + trimmed
		}
		if match := notionMarkdownContainerOpen.FindStringSubmatch(trimmed); match != nil {
			if blocks, used := notionMarkdownContainer(lines, index, column, match, level); used > 0 {
				out = append(out, blocks...)
				index += used - 1
				continue
			}
		}
		// A pipe table reads as a <table> with a header row, its cells as Notion's text.
		if strings.HasPrefix(trimmed, "|") && index+1 < len(lines) &&
			workspaceImportTableSeparatorPattern.MatchString(strings.TrimSpace(lines[index+1])) {
			out = append(out, tabs(level)+`<table header-row="true">`)
			for row := index; row < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[row]), "|"); row++ {
				index = row
				// The line under the header row only marks it.
				if workspaceImportTableSeparatorPattern.MatchString(strings.TrimSpace(lines[row])) {
					continue
				}
				out = append(out, tabs(level)+"<tr>")
				for _, cell := range workspaceImportTableCells(lines[row]) {
					out = append(out, tabs(level)+"<td>"+cell+"</td>")
				}
				out = append(out, tabs(level)+"</tr>")
			}
			out = append(out, tabs(level)+"</table>")
			continue
		}
		if notionMarkdownTableOpenPattern.MatchString(trimmed) {
			if blocks, used := notionMarkdownTableRows(lines, index, level); used > 0 {
				out = append(out, blocks...)
				index += used - 1
				continue
			}
		}
		if strings.HasPrefix(trimmed, ">") {
			blocks, used := notionMarkdownFlowQuote(lines, index, base, level)
			out = append(out, blocks...)
			index += used - 1
			continue
		}
		if notionMarkdownFlowThematic.MatchString(trimmed) {
			out = append(out, tabs(level)+"---")
			continue
		}
		if marker, width, item := notionMarkdownFlowItem(trimmed); item {
			items = append(items, notionMarkdownListItem{content: column + width, depth: level})
			text := strings.TrimSpace(trimmed[min(width, len(trimmed)):])
			// An item whose text starts another block holds it; $$ that no
			// later $$ closes is its text (seen 2026-11-10).
			if notionMarkdownFlowItemHolds(text) && (text != "$$" || notionMarkdownEquationCloses(lines[index+1:])) {
				out = append(out, tabs(level)+marker+" ")
				lines[index] = strings.Repeat(" ", column+width) + text
				index--
				continue
			}
			// An empty item's text may stand on the line under it, in as far as its blocks.
			if next := index + 1; text == "" && next < len(lines) && !strings.HasPrefix(lines[next], "\t") {
				in := len(lines[next]) - len(strings.TrimLeft(lines[next], " "))
				if after := strings.TrimSpace(lines[next]); after != "" && in >= column+width && in < column+width+4 &&
					!notionMarkdownFlowStarts(after, "", false) {
					text, index = after, next
				}
			}
			trimmed = marker + " " + text
		}
		if notionMarkdownEmptyParagraph.MatchString(trimmed) {
			trimmed = "<empty-block/>"
		} else if colored := notionMarkdownEmptyColoredParagraph.FindStringSubmatch(trimmed); colored != nil {
			trimmed = "<empty-block/>" + notionMarkdownEmptyBlockColor(notionMarkdownAttributes(colored[1]))
		}
		// A line ending in a backslash goes on in the next one.
		for strings.HasSuffix(trimmed, `\`) && !strings.HasSuffix(trimmed, `\\`) && index+1 < len(lines) &&
			strings.TrimSpace(lines[index+1]) != "" {
			index++
			trimmed = strings.TrimSuffix(trimmed, `\`) + "<br>" + strings.TrimSpace(lines[index])
		}
		// A line of text keeps the whitespace ending it, for the reader that
		// keeps it (markdownParseOptions.KeepLineEnds); a tag's, a quote's and
		// a table's go.
		ending := ""
		if !strings.HasPrefix(trimmed, "<") && !strings.HasPrefix(trimmed, ">") && !strings.HasPrefix(trimmed, "|") {
			ending = lines[index][len(strings.TrimRightFunc(lines[index], unicode.IsSpace)):]
		}
		out = append(out, tabs(level)+trimmed+ending)
		if notionMarkdownFlowTakesText(trimmed) {
			open = len(out) - 1
		}
	}
	return out
}

// notionMarkdownEquationOpens tells a $$ line that opens an equation: one
// closed by a later $$ line.
func notionMarkdownEquationOpens(lines []string, index int) bool {
	trimmed := strings.TrimSpace(lines[index])
	if trimmed != "$$" && !strings.HasPrefix(trimmed, "$$ {color=") {
		return false
	}
	return notionMarkdownEquationCloses(lines[index+1:])
}

// notionMarkdownEquationCloses tells lines after a $$ that hold one closing it.
func notionMarkdownEquationCloses(after []string) bool {
	for _, line := range after {
		if strings.TrimSpace(line) == "$$" {
			return true
		}
	}
	return false
}

func notionMarkdownFenceOf(trimmed string) string {
	return trimmed[:len(trimmed)-len(strings.TrimLeft(trimmed, "`"))]
}

// notionMarkdownContainer reads a tag that holds a block from lines[start] to
// its closing tag, giving the block's lines as Leafen reads them and the
// lines used; none when the tag doesn't close.
func notionMarkdownContainer(lines []string, start, column int, match []string, level int) ([]string, int) {
	name, attributes, rest := match[1], notionMarkdownAttributes(match[2]), strings.TrimSpace(match[3])
	tabs := strings.Repeat("\t", level)
	closing := "</" + name + ">"
	// A sub-page's, database's, link's or button's title on a line of its own
	// reads as the tag on one line, as Notion's MCP reads it (seen 2026-11-04); a title of one
	// line keeps the spaces at its ends, its indent aside, spaces alone too,
	// as Notion's "v2" does (seen 2026-11-13: "  v2쪽  " and "   " name a page
	// so).
	if name == "page" || name == "database" || name == "link-to-page" || name == "button" {
		if rest != "" {
			return nil, 0
		}
		end := notionMarkdownClosing(lines, start, name)
		if end < 0 {
			return nil, 0
		}
		title := []string{}
		for _, line := range lines[start+1 : end] {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				title = append(title, trimmed)
			}
		}
		written := strings.Join(title, " ")
		if end == start+2 {
			written = strings.TrimRight(strings.TrimLeft(notionMarkdownDedentColumns(lines[start+1], column), "\t"), "\r")
		}
		return []string{tabs + "<" + name + match[2] + ">" + written + closing}, end - start + 1
	}
	// A tag closed on its own line: its text, or its blocks; a caption as
	// written (notionMarkdownCaptionTags).
	if rest != "" {
		if !strings.HasSuffix(rest, closing) {
			return nil, 0
		}
		title := strings.TrimSpace(strings.TrimSuffix(rest, closing))
		// A caption, and an equation's expression, as written.
		if notionMarkdownCaptionTags[name] || name == "equation" {
			title = strings.TrimSuffix(strings.TrimRight(match[3], " \t"), closing)
		}
		// A text block's text keeps the spaces at its ends, as on lines of
		// its own (seen 2026-11-13: "<p>  a  </p>", "<h2>  a  </h2>" and
		// "<toggle>  a  </toggle>" keep them; a callout's goes).
		if notionMarkdownEdgeTags[name] {
			title = notionMarkdownTextEdges(strings.TrimSuffix(strings.TrimRight(match[3], " \t"), closing))
		}
		return notionMarkdownContainerBlock(name, attributes, title, nil, level, tabs), 1
	}
	end := notionMarkdownClosing(lines, start, name)
	if end < 0 {
		return nil, 0
	}
	body := make([]string, 0, end-start-1)
	for _, line := range lines[start+1 : end] {
		body = append(body, notionMarkdownDedentColumns(line, column))
	}
	// Leafen's own form, the tag's lines a tab in, reads as it is, save the
	// tags only the public API's form writes that way.
	for _, line := range body {
		if strings.TrimSpace(line) != "" {
			if strings.HasPrefix(line, "\t") && !notionMarkdownCaptionTags[name] && name != "equation" {
				if !notionMarkdownTabbedTags[name] {
					return nil, 0
				}
				for at := range body {
					body[at] = strings.TrimPrefix(body[at], "\t")
				}
			}
			break
		}
	}
	switch name {
	case "ul", "ol", "columns", "tabs":
		return notionMarkdownGroup(name, attributes, body, level, tabs), end - start + 1
	}
	title, children := notionMarkdownTitle(body)
	if caption, single := notionMarkdownCaptionLines(body); single && notionMarkdownCaptionTags[name] {
		title, children = caption, nil
	}
	if notionMarkdownEdgeTags[name] {
		title = notionMarkdownTitleEdges(body, title)
	}
	// Its text in a <p> of its own, as "v2" writes text with edge spaces,
	// is its text with them; the blocks the <p> holds are kept
	// (notion_page_markdown_title_blocks.go).
	var under []string
	if text, rest, held := notionMarkdownTitleParagraph(body); held && notionMarkdownParagraphTitled[name] {
		title, children = text, rest
		under, children = notionMarkdownHeldTitleBlocks(name, title, notionMarkdownTitleParagraphBlocks(body), children, level)
		if name == "callout" {
			title = notionMarkdownCalloutTitleLine(title, body)
		}
	}
	if name == "equation" {
		title, children = notionMarkdownEquationTagText(body), nil
	}
	if name == "synced-block" || name == "column" {
		// All it holds is its blocks.
		title, children = "", body
	}
	return notionMarkdownCalloutWithHeld(notionMarkdownContainerBlock(name, attributes, title, children, level, tabs), under), end - start + 1
}

// notionMarkdownTabbedTags are the tags only the public API's form writes
// with their blocks a tab further in; Leafen's own form writes these
// otherwise (notionMarkdownContainerBlock).
var notionMarkdownTabbedTags = map[string]bool{"tabs": true, "tab": true, "synced-block": true, "synced-block-reference": true}

// notionMarkdownTabbedContainer reads one of those tags standing tabs in, as
// the public API's form writes it in a block that holds it (a toggle, a list
// item, a quote, a callout, a column, another synced block): from its line
// to its closing tag, without the tabs its line starts with, as at the top
// and as many levels further in. None for any other line.
func notionMarkdownTabbedContainer(lines []string, start, depth int) ([]string, int) {
	match := notionMarkdownContainerOpen.FindStringSubmatch(strings.TrimSpace(lines[start]))
	if match == nil || !notionMarkdownTabbedTags[match[1]] {
		return nil, 0
	}
	end := notionMarkdownClosing(lines, start, match[1])
	if end < 0 {
		return nil, 0
	}
	in := len(lines[start]) - len(strings.TrimLeft(lines[start], "\t"))
	held := make([]string, 0, end-start+1)
	for _, line := range lines[start : end+1] {
		taken := 0
		for taken < in && taken < len(line) && line[taken] == '\t' {
			taken++
		}
		held = append(held, line[taken:])
	}
	blocks, used := notionMarkdownContainer(held, 0, 0, match, depth+in)
	if used != len(held) {
		return nil, 0
	}
	return blocks, used
}

// notionMarkdownClosing finds the line closing the tag opened at start, past
// the same tags opened inside it and code, a list item's ending with the
// item (notionMarkdownItemFence).
func notionMarkdownClosing(lines []string, start int, name string) int {
	depth, fence := 0, ""
	var items []notionMarkdownVersion2Held
	opening := regexp.MustCompile(`^<` + regexp.QuoteMeta(name) + `(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*>$`)
	for index := start + 1; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if fence != "" && len(items) > 0 {
			if in, closed := notionMarkdownItemFenceLine(lines[index], fence, items); in {
				if closed {
					fence, items = "", nil
				}
				continue
			}
			fence, items = "", nil
		}
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
			continue
		}
		if fence, items = notionMarkdownItemFence(lines[index]); fence != "" {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "```"):
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

// notionMarkdownEdgeTags are the text blocks' tags whose text keeps the
// whitespace at its ends.
var notionMarkdownEdgeTags = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "quote": true, "li": true, "todo": true, "toggle": true,
}

// notionMarkdownParagraphTitled are the tags whose text a <p> standing first
// in them may hold, as "v2" reads them (seen 2026-11-13; a paragraph's, a
// to-do's and a heading's too, 2026-11-14: "<p>⏎<p>  </p>⏎⏎b⏎</p>" is the
// paragraph "  " holding "b", "<todo …>⏎<p>a</p>⏎⏎b⏎</todo>" the to-do "a",
// "<h2>⏎<p>a</p>⏎</h2>" the heading "a").
var notionMarkdownParagraphTitled = map[string]bool{
	"callout": true, "toggle": true, "quote": true, "li": true, "p": true, "todo": true,
	"h1": true, "h2": true, "h3": true, "h4": true,
}

var notionMarkdownParagraphOpening = regexp.MustCompile(`^<p((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*(/?)>(.*)$`)

// notionMarkdownTitleParagraph reads a tag's lines that start with its
// text in a <p>: the text, with its edge spaces, and the lines after it.
// The <p> may stand on one line ("<p>  a  </p>" is "  a  ", its color
// gone), on lines of its own ("<p>⏎  a  ⏎</p>"), or hold nothing ("<p />",
// "<p></p>"); the lines after it are blocks, a blank line between or none
// (seen 2026-11-13, "v2": a callout, a toggle, a quote and a list item
// read so).
func notionMarkdownTitleParagraph(body []string) (string, []string, bool) {
	start := 0
	for start < len(body) && strings.TrimSpace(body[start]) == "" {
		start++
	}
	if start == len(body) {
		return "", nil, false
	}
	match := notionMarkdownParagraphOpening.FindStringSubmatch(strings.TrimSpace(body[start]))
	switch {
	case match == nil:
		return "", nil, false
	case match[2] == "/":
		if match[3] != "" {
			return "", nil, false
		}
		return "", body[start+1:], true
	case strings.HasSuffix(match[3], "</p>"):
		return notionMarkdownTextEdges(strings.TrimSuffix(match[3], "</p>")), body[start+1:], true
	case match[3] == "":
		end := notionMarkdownClosing(body, start, "p")
		if end < 0 {
			return "", nil, false
		}
		inner := body[start+1 : end]
		title, _ := notionMarkdownTitle(inner)
		return notionMarkdownTitleEdges(inner, title), body[end+1:], true
	}
	return "", nil, false
}

// notionMarkdownTitle splits a tag's lines into the block's text — its
// lines as one text (notionMarkdownTitleLines) — and its blocks after it.
func notionMarkdownTitle(body []string) (string, []string) {
	body, count := notionMarkdownTitleLines(body)
	children := body[count:]
	if len(children) > 0 && strings.TrimSpace(children[0]) == "" {
		children = children[1:]
	}
	if len(children) == 0 {
		children = nil
	}
	return strings.Join(trimmedLines(body[:count]), "<br>"), children
}

// notionMarkdownTitleLines is a tag's lines from the first that isn't blank
// and how many of them are its text, as Notion's "v2" reads a tag's (seen
// 2026-11-10, "<li>a\n- nested</li>", and 2026-11-13, "v1" too for a
// callout): its text ends at a blank line or at a line that starts a block —
// a list item, a quote, a heading, code, a divider, an equation — and is
// none when an equation stands first ("<callout>\n$$\nx\n$$\nafter" holds
// the equation and "after") or a block's tag ("<callout>\n<callout>\na\n
// </callout>\n</callout>" holds the callout "a"), a table's too, whatever
// its first line under its tag ("<callout>\n<table>\n<thead>…" holds the
// table, seen 2026-11-14, "v1" and "v2"); a first line that takes
// no text, a heading's, is the text alone ("<callout>\n# h\nb\n</callout>"
// holds the heading "h" and "b", seen 2026-11-13); a quote, a list item or
// a heading standing first is none, the tag holding it as its block
// ("<callout>\n> $$\n> b\n</callout>" the quote "$$⏎b", "<toggle>\n- a\n
// </toggle>" the item "a", "<quote>\n# h\n</quote>" the heading "h", seen
// 2026-11-14), and so are code and a divider ("<p>\n```txt\nb\n```\n</p>",
// "<p>\n---\n</p>", as Notion writes an empty paragraph holding them).
func notionMarkdownTitleLines(body []string) ([]string, int) {
	for len(body) > 0 && strings.TrimSpace(body[0]) == "" {
		body = body[1:]
	}
	for index, line := range body {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			return body, index
		}
		if index == 0 && !strings.HasPrefix(line, "\t") {
			if match := notionMarkdownContainerOpen.FindStringSubmatch(trimmed); match != nil && match[1] != "p" ||
				notionMarkdownTableOpenPattern.MatchString(trimmed) {
				return body, 0
			}
			if _, _, item := notionMarkdownFlowItem(trimmed); item || strings.HasPrefix(trimmed, ">") ||
				notionMarkdownVersion2Heading.MatchString(trimmed) || strings.HasPrefix(trimmed, "```") ||
				strings.HasPrefix(trimmed, "~~~") || notionMarkdownFlowThematic.MatchString(trimmed) {
				return body, 0
			}
		}
		if trimmed == "$$" {
			if notionMarkdownEquationOpens(body, index) {
				return body, index
			}
			continue
		}
		if index > 0 && !strings.HasPrefix(line, "\t") && notionMarkdownFlowStarts(trimmed, "", false) {
			return body, index
		}
	}
	return body, len(body)
}

func trimmedLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
