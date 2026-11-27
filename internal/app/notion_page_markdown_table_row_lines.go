package app

import (
	"regexp"
	"slices"
	"strings"
)

// A "v2" table's row written on a line with its cells, a cell's text running
// on to lines its row's tags stand beside — "<tr><td>a⏎- b</td></tr>",
// "<tr><td>a⏎b</td><td>c</td></tr>", "<tr><td>a⏎</td><td>b</td></tr>" —
// reads as each tag on a line of its own, its cell's text keeping its lines,
// as Notion's "v2" reads it (seen 2026-11-14): the row's tags stand apart
// from its cells, whose lines read as a cell's on lines of its own do
// (notionMarkdownFoldedCells). A <thead> or <tbody> starting the row's line
// stands apart too, and so does a table's opening tag its row follows on its
// line ("<table><tr><td>a⏎b</td></tr></table>"), in a quote too. A line
// holding text between the tags stays as written. "v1" reads its tables
// otherwise (notionMarkdownVersion1TagTables).

var (
	notionMarkdownTableRowLineOpen = regexp.MustCompile(`^<table(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*>`)
	notionMarkdownTableRowLineTag  = regexp.MustCompile(`^</?(tr|thead|tbody)(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*>`)
	notionMarkdownTableRowLineCell = regexp.MustCompile(`^<(t[dh])(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*>`)
)

// notionMarkdownTableRowLines puts each table's row tags and cells written
// on one line on lines of their own, outside code.
func notionMarkdownTableRowLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	// cell is the name of the cell whose text runs on past its line, or "".
	fence, table, cell := "", false, ""
	for index, line := range lines {
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t>"))]
		rest := line[len(lead):]
		body := strings.TrimRight(rest, " \t")
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(body, fence, true) {
				fence = ""
			}
			out = append(out, line)
			continue
		// Code a fence closes in a cell left open is its text
		// (notionMarkdownTableCellCloses).
		case notionMarkdownVersion2Fence(body) && cell != "":
			fence = notionMarkdownFenceOf(body)
			out = append(out, line)
			continue
		case notionMarkdownVersion2Fence(body):
			fence, table, cell = notionMarkdownFenceOf(body), false, ""
			out = append(out, line)
			continue
		}
		if cell != "" {
			// A line of the cell's text: the one its closing tag stands on ends it.
			at := notionMarkdownVersion2FlowClosing(line, cell)
			if at < 0 {
				out = append(out, line)
				continue
			}
			end := at + len("</"+cell+">")
			pieces, open, ok := notionMarkdownTableRowPieces(line[end:])
			if cell = ""; !ok || len(pieces) == 0 || !notionMarkdownTableCellCloses(lines[index+1:], open) {
				out = append(out, line)
				continue
			}
			out = append(out, line[:end])
			table, cell = notionMarkdownTableRowEmit(&out, lead, pieces, open)
			continue
		}
		if !table {
			opening := notionMarkdownTableRowLineOpen.FindString(body)
			if opening == "" {
				out = append(out, line)
				continue
			}
			if rest = rest[len(opening):]; strings.TrimSpace(rest) == "" {
				table = strings.TrimSpace(rest) == ""
				out = append(out, line)
				continue
			}
			pieces, open, ok := notionMarkdownTableRowPieces(rest)
			if !ok || !notionMarkdownTableCellCloses(lines[index+1:], open) {
				out = append(out, line)
				continue
			}
			out = append(out, lead+opening)
			table, cell = notionMarkdownTableRowEmit(&out, lead, pieces, open)
			continue
		}
		pieces, open, ok := notionMarkdownTableRowPieces(rest)
		if ok && !notionMarkdownTableCellCloses(lines[index+1:], open) {
			pieces, open = nil, ""
		}
		if !ok || len(pieces) <= 1 {
			if ok && len(pieces) == 1 {
				table, cell = !strings.HasPrefix(pieces[0], "</table>"), open
			}
			out = append(out, line)
			continue
		}
		table, cell = notionMarkdownTableRowEmit(&out, lead, pieces, open)
	}
	return out
}

// notionMarkdownTableRowPieces reads a table's line past what was taken of
// it: its row tags and its cells closed on it, each a piece, the cell left
// open at its end the last (open its name), and the table's closing tag
// with all after it as written; false for a line holding anything else.
func notionMarkdownTableRowPieces(text string) (pieces []string, open string, ok bool) {
	for {
		// The cell left open keeps the whitespace ending its line.
		text = strings.TrimLeft(text, " \t")
		switch {
		case strings.TrimSpace(text) == "":
			return pieces, "", true
		case strings.HasPrefix(text, "</table>"):
			return append(pieces, strings.TrimRight(text, " \t")), "", true
		}
		if tag := notionMarkdownTableRowLineTag.FindStringSubmatch(text); tag != nil {
			pieces, text = append(pieces, tag[0]), text[len(tag[0]):]
			continue
		}
		match := notionMarkdownTableRowLineCell.FindStringSubmatch(text)
		if match == nil {
			return nil, "", false
		}
		at := notionMarkdownVersion2FlowClosing(text[len(match[0]):], match[1])
		if at < 0 {
			return append(pieces, text), match[1], true
		}
		end := len(match[0]) + at + len("</"+match[1]+">")
		pieces, text = append(pieces, text[:end]), text[end:]
	}
}

// notionMarkdownTableCellCloses tells the lines after a cell left open on
// its line (name) that close it past any code, each fence closed on a line of
// its own: one they don't is left as written, as Notion's "v2" leaves code
// there to break the table (seen 2026-11-14: "<tr><td>a⏎```⏎b⏎```</td></tr>"
// leaves its tags open, and so does "~~~" so, "<tr><td>a⏎```⏎b⏎```⏎c</td></tr>"
// is the cell "a⏎", the code "b" and "⏎c"). No cell left open closes.
func notionMarkdownTableCellCloses(lines []string, name string) bool {
	if name == "" {
		return true
	}
	for at := 0; at < len(lines); at++ {
		if notionMarkdownCellFence(lines[at]) {
			if at = notionMarkdownCellFenceEnd(lines, at); at < 0 {
				return false
			}
			continue
		}
		// An equation no "$$" line of its own closes breaks it so too (seen
		// 2026-11-14: "<tr><td>a⏎$$⏎b⏎$$</td></tr>").
		if strings.TrimSpace(lines[at]) == "$$" && !slices.ContainsFunc(lines[at+1:], func(line string) bool { return strings.TrimSpace(line) == "$$" }) {
			return false
		}
		if notionMarkdownVersion2FlowClosing(lines[at], name) >= 0 {
			return true
		}
	}
	return false
}

// notionMarkdownTableRowEmit puts a line's pieces on lines of their own,
// each standing as far in as the line, and tells whether the table is still
// open and the cell left open.
func notionMarkdownTableRowEmit(out *[]string, lead string, pieces []string, open string) (bool, string) {
	for _, piece := range pieces {
		*out = append(*out, lead+piece)
	}
	if len(pieces) > 0 && strings.HasPrefix(pieces[len(pieces)-1], "</table>") {
		return false, ""
	}
	return true, open
}
