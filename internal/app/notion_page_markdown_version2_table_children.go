package app

import (
	"regexp"
	"strings"
)

// An HTML table's lines that are no child of the table's, or of its <thead>
// or <tbody>, are refused as Notion's "v2" refuses them (seen 2026-11-14),
// once for each table and each such part: a line of text, a cell outside a
// row, a closing tag of no part open there ("</td>" after its row's </tr>,
// "</tr>", "</p>"), a comment — what <table> holds and that the unfinished
// part after was skipped (notionMarkdownHoldsOnly,
// notionMarkdownPartialChildSaid), and what <tbody> holds in a <tbody>, said
// after the block tags left open and before the rest. Blank lines are none,
// and a row's lines, from its <tr> to its </tr> or the table's end, are its
// own. A table in a quote reads past its >.

var (
	notionMarkdownVersion2TableLine     = regexp.MustCompile(`^<table(?:[ \t][^<>]*)?>$`)
	notionMarkdownVersion2TableTextLine = regexp.MustCompile(`^(<table(?:[ \t][^<>]*)?>)([^<>]*\S[^<>]*)$`)
	notionMarkdownVersion2TablePart     = regexp.MustCompile(`^<(tr|thead|tbody|colgroup)(?:[ \t/>]|$)`)
)

// notionMarkdownVersion2TableChildren is what Notion says of the lines
// standing in HTML tables, or their parts, as no child of theirs.
func notionMarkdownVersion2TableChildren(lines []string) []string {
	said, fence := []string{}, ""
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
		quotes := strings.Count(notionMarkdownVersion2QuoteMarks(lines[index]), ">")
		if !notionMarkdownVersion2TableLine.MatchString(notionMarkdownVersion2Unquoted(lines[index], quotes)) {
			continue
		}
		var used int
		said, used = notionMarkdownVersion2TableChildLines(lines[index+1:], quotes, said)
		index += used
	}
	return said
}

// notionMarkdownVersion2TableChildLines reads a table's lines after its
// opening tag to its closing tag, saying what it holds as no child of its
// own or of its parts, and tells how many lines it read.
func notionMarkdownVersion2TableChildLines(lines []string, quotes int, said []string) ([]string, int) {
	group, row, tableSaid, groupSaid := "", false, false, false
	for at, line := range lines {
		text := notionMarkdownVersion2Unquoted(line, quotes)
		switch {
		case text == "" || notionMarkdownVersion2Fence(text):
		case row:
			row = !strings.HasPrefix(text, "</tr>") && !strings.HasPrefix(text, "</table>")
			if strings.HasPrefix(text, "</table>") {
				return said, at + 1
			}
		case strings.HasPrefix(text, "</table>"):
			return said, at + 1
		case group != "" && strings.HasPrefix(text, "</"+group+">"):
			group, groupSaid = "", false
		case group == "colgroup":
			// Its columns are read on their own (notionMarkdownVersion2Colgroups).
		case strings.HasPrefix(text, "<tr") && notionMarkdownVersion2TablePart.MatchString(text):
			row = !strings.Contains(text, "</tr>")
		case group == "" && notionMarkdownVersion2TablePart.MatchString(text):
			if name := notionMarkdownVersion2TablePart.FindStringSubmatch(text)[1]; !strings.Contains(text, "</"+name+">") {
				group = name
			}
		case group != "":
			if !groupSaid {
				said = append(said, notionMarkdownHoldsOnly(group, "<tr>")+notionMarkdownPartialChildSaid)
				groupSaid = true
			}
		case !tableSaid:
			said = append(said, notionMarkdownHoldsOnly("table", "<colgroup>, <thead>, <tbody>, <tr>")+
				notionMarkdownPartialChildSaid)
			tableSaid = true
		}
	}
	return said, len(lines)
}

// notionMarkdownVersion2TableTextLines puts text standing in a table beside
// its tags on a line of its own, where it is a child of no part of the
// table's (seen 2026-11-14: "<table> x" and "<tr><td>a</td></tr>x<tr>…" say
// what <table> holds, and nothing more).
func notionMarkdownVersion2TableTextLines(lines []string) []string {
	out, table, fence := make([]string, 0, len(lines)), false, ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(trimmed, fence, true) {
				fence = ""
			}
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
		case !table && notionMarkdownVersion2TableTextLine.MatchString(trimmed):
			match := notionMarkdownVersion2TableTextLine.FindStringSubmatch(trimmed)
			out, table = append(out, lead+match[1], lead+strings.TrimSpace(match[2])), true
			continue
		case notionMarkdownVersion2TableLine.MatchString(trimmed):
			table = true
		case table && strings.HasPrefix(trimmed, "</table>"):
			table = false
		case table && strings.HasPrefix(trimmed, "<tr"):
			out = append(out, notionMarkdownVersion2RowTextLines(lead, trimmed)...)
			continue
		}
		out = append(out, line)
	}
	return out
}

// notionMarkdownVersion2RowTextLines is a line of rows with the text between
// them on lines of their own.
func notionMarkdownVersion2RowTextLines(lead, line string) []string {
	out := []string{}
	for {
		end := notionMarkdownVersion2FlowClosing(line, "tr")
		if end < 0 {
			break
		}
		end += len("</tr>")
		next := strings.Index(line[end:], "<")
		text := line[end:]
		if next >= 0 {
			text = line[end : end+next]
		}
		if strings.TrimSpace(text) == "" {
			if next < 0 {
				break
			}
			out, line = append(out, lead+line[:end]), strings.TrimLeft(line[end:], " \t")
			continue
		}
		out = append(out, lead+line[:end], lead+strings.TrimSpace(text))
		if line = line[end+len(text):]; line == "" {
			return out
		}
	}
	return append(out, lead+line)
}
