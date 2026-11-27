package app

import (
	"regexp"
	"slices"
	"strings"
)

// A public API write's "v1" Markdown reads a table's tags line by line, as
// Notion's "v1" reads them (seen 2026-11-14, each written and read back as
// blocks), before any code or quote in them. A table opens on a line holding
// its opening tag alone, its whitespace aside ("<table header-row=\"true\">",
// "<table> ", "<table />", whose closing it ignores); one with more on its
// line ("<table><tr>…", "<table> x") is no table, its lines text as
// written. It runs to the line its closing tag ends ("</table>",
// "<tr><td>a</td></tr></table>"), a table opened within it closed first,
// or, no line closing it, to the end (seen 2026-11-14: "<table>⏎b" and
// "<table />⏎b" are a table without rows). A row starts on a line starting
// with <tr>: a line that holds a </tr> is that row alone, the cells all
// through it its cells
// ("<tr><td>a</td></tr><tr><td>b</td></tr>" is one row of two, "…</tr>x<tr>…"
// and "…</tr><td>b</td>" too, a cell left open running to its line's end:
// "<tr><td>a</td></tr><tr><td>b⏎c</td></tr>" is "a" and "b"); else the row
// runs to the line holding a </tr>, the cells through its lines its cells. A
// row holding no cell goes ("<tr></tr>", "<tr>⏎</tr>"). Any other line in
// the table goes — a <thead> or <tbody> with a row on its line, text, a
// cell no row holds — but for a <colgroup> on lines of its own, its <col>s
// one to a line, whose columns' colors it keeps; a <col> on the
// <colgroup>'s line or the </colgroup>'s keeps none. A cell's text keeps its
// lines, a code fence among them as text ("<td>a⏎```⏎b⏎```</td>" is "a⏎",
// code "⏎b⏎" between backticks read as "v1" reads them), without its blank
// lines and lines of whitespace, but for the whitespace after its opening
// tag on that tag's line ("<td>a⏎⏎⏎b</td>" is "a⏎b", "<td>  ⏎a</td>"
// "  ⏎a", "  a  ⏎  b  " as written, an item's line its indent). A table is
// written in Leafen's form, a tag or a cell a line, as its reader takes one;
// a table's tags on any line no table holds are text as written, in a quote
// too, each of whose lines is a quote of its own ("> <table>⏎> <tr><td>a⏎…"
// is the quotes "<table>", "<tr><td>a" and on).

var (
	notionMarkdownVersion1TagTableRow  = regexp.MustCompile(`^<tr((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>`)
	notionMarkdownVersion1TagTableCell = regexp.MustCompile(`<(t[dh])((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>`)
	notionMarkdownVersion1TagTableEnd  = regexp.MustCompile(`</t[dh]>|</tr>|<t[dh](?:[ \t][^<>]*)?>`)
	// notionMarkdownVersion1TableTagText is a table's tag where it stands in a
	// line no table holds, text as written (seen 2026-11-14: "<table><tr>…",
	// "> <tr><td>a", "a <td>b</td>", "> a</tr>").
	notionMarkdownVersion1TableTagText = regexp.MustCompile(`^</?(?:table|tr|td|th|thead|tbody|colgroup|col)(?:[ \t/>]|$)`)
	// notionMarkdownVersion1TagTableHeld is a table's opening tag alone on its
	// line, or after one list item's or to-do's marker.
	notionMarkdownVersion1TagTableHeld = regexp.MustCompile(`^((?:(?:[-*+]|\d{1,9}[.)])(?:[ \t]+\[[ xX]\])?[ \t]+)?)(<table(?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*/?>$`)
	// notionMarkdownVersion1TagTableColumn is a <colgroup>'s column alone on its line.
	notionMarkdownVersion1TagTableColumn = regexp.MustCompile(`^<col(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*/?>$`)
)

// notionMarkdownVersion1TagTables are the lines with each table written in
// Leafen's form, and which of them a table's form writes.
func notionMarkdownVersion1TagTables(lines []string) ([]string, map[int]bool) {
	out, tabled := make([]string, 0, len(lines)), map[int]bool{}
	fence, equation := false, false
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		_, rest := notionMarkdownVersion1Items(strings.TrimLeft(line, " \t"))
		switch {
		case fence:
			fence = !notionMarkdownFenceCloses(trimmed, "```", false)
		case equation:
			equation = !notionMarkdownVersion1EquationCloses(trimmed)
		case strings.HasPrefix(rest, "```"):
			fence = true
		case strings.TrimSpace(rest) == "$$":
			equation = true
		case notionMarkdownVersion1TagTableHeld.MatchString(trimmed):
			// A list item's or a to-do's line holding the opening tag holds
			// the table, its own text empty (seen 2026-11-14: "- <table>⏎
			// <tr>…", "- [ ] <table>…").
			match := notionMarkdownVersion1TagTableHeld.FindStringSubmatch(trimmed)
			lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			if table, used := notionMarkdownVersion1TagTable(lines, index, lead+strings.Repeat(" ", len(match[1])), match[2]+">"); used > 0 {
				if match[1] != "" {
					out = append(out, lead+strings.TrimRight(match[1], " \t"))
				}
				for _, written := range table {
					tabled[len(out)] = true
					out = append(out, written)
				}
				index += used - 1
				continue
			}
		}
		out = append(out, line)
	}
	return out, tabled
}

// notionMarkdownVersion1TagTable reads the table whose opening tag stands at
// lines[start]: its lines in Leafen's form, standing as far in as lead, and
// the lines it took, to the end for one no line closes.
func notionMarkdownVersion1TagTable(lines []string, start int, lead, opening string) ([]string, int) {
	end, depth := start+1, 1
	for ; end < len(lines); end++ {
		trimmed := strings.TrimSpace(lines[end])
		if notionMarkdownVersion1TagTableHeld.MatchString(trimmed) {
			depth++
		} else if strings.HasSuffix(trimmed, "</table>") {
			if depth--; depth == 0 {
				break
			}
		}
	}
	out := []string{lead + opening}
	body := slices.Clone(lines[start+1 : min(end+1, len(lines))])
	if end < len(lines) {
		body[len(body)-1] = strings.TrimSuffix(strings.TrimRight(body[len(body)-1], " \t"), "</table>")
	} else {
		end = len(lines) - 1
	}
	// group is where in out a <colgroup> left open stands, or -1.
	group := -1
	for at := 0; at < len(body); at++ {
		trimmed := strings.TrimSpace(body[at])
		if group >= 0 {
			switch {
			case trimmed == "":
				continue
			case notionMarkdownVersion1TagTableColumn.MatchString(trimmed):
				out = append(out, lead+trimmed)
				continue
			case trimmed == "</colgroup>":
				out, group = append(out, lead+trimmed), -1
				continue
			}
			out, group = out[:group], -1
		}
		if trimmed == "<colgroup>" {
			out, group = append(out, lead+trimmed), len(out)
			continue
		}
		row := notionMarkdownVersion1TagTableRow.FindStringSubmatch(strings.TrimLeft(body[at], " \t"))
		if row == nil {
			continue
		}
		last := at
		for last < len(body)-1 && !strings.Contains(body[last], "</tr>") {
			last++
		}
		text := strings.Join(body[at:last+1], "\n")
		text = text[strings.Index(text, "<tr")+len(row[0]):]
		at = last
		cells := notionMarkdownVersion1TagTableCells(text)
		if len(cells) == 0 {
			continue
		}
		out = append(out, lead+"<tr"+notionMarkdownVersion1TagTableColor(row[1])+">")
		for _, cell := range cells {
			out = append(out, lead+cell)
		}
		out = append(out, lead+"</tr>")
	}
	if group >= 0 {
		out = out[:group]
	}
	return append(out, lead+"</table>"), end - start + 1
}

// notionMarkdownVersion1TagTableCells are the cells a row's text holds, each
// a line in Leafen's form.
func notionMarkdownVersion1TagTableCells(text string) []string {
	cells := []string{}
	for {
		match := notionMarkdownVersion1TagTableCell.FindStringSubmatchIndex(text)
		if match == nil {
			return cells
		}
		color := notionMarkdownVersion1TagTableColor(text[match[4]:match[5]])
		text = text[match[1]:]
		held := text
		if stop := notionMarkdownVersion1TagTableEnd.FindStringIndex(text); stop != nil {
			held = text[:stop[0]]
			if strings.HasPrefix(text[stop[0]:], "</t") && !strings.HasPrefix(text[stop[0]:], "</tr") {
				text = text[stop[1]:]
			} else {
				text = text[stop[0]:]
			}
		} else {
			text = ""
		}
		text := notionMarkdownVersion1CellText(strings.Split(held, "\n"))
		if text == "" {
			text = notionMarkdownEmptyCellText
		}
		cells = append(cells, "<td"+color+">"+text+"</td>")
	}
}

// notionMarkdownEmptyCellText stands for the text of a "v1" write's cell
// that holds none, which holds one empty piece, as Notion's "v1" writes it
// (seen 2026-11-14: "<td></td>", "<th></th>", "<td>⏎</td>", "<td>⏎ ⏎</td>"
// read back as [""], where "v2"'s and a "|"-row's empty cell is [] and so
// is a cell filling a row to its table's width); its Markdown is "<td></td>"
// either way. No text a write sends holds the byte.
const notionMarkdownEmptyCellText = "\xfc"

// notionMarkdownVersion1TagTableColor is a row's or a cell's color="…" as
// Leafen's form writes it, from its tag's attributes.
func notionMarkdownVersion1TagTableColor(attributes string) string {
	return notionMarkdownColorAttribute(notionMarkdownColorValue(notionMarkdownAttributes(attributes)["color"]))
}

// notionMarkdownVersion1CellText is a cell's text over its lines as "v1"
// reads it: without its blank lines and its lines of whitespace, but for
// its first, joined by <br>.
func notionMarkdownVersion1CellText(texts []string) string {
	kept := []string{}
	for at, text := range texts {
		if at == 0 && text != "" || at > 0 && strings.TrimSpace(text) != "" {
			kept = append(kept, text)
		}
	}
	return notionMarkdownCellLinesJoined(kept, false)
}
