package app

import (
	"regexp"
	"slices"
	"strings"
)

// A "v2" table's <colgroup> holds its columns' <col>s, each closed in itself
// or by its </col> on its line or a later one, blank lines between, as
// Notion's "v2" reads it (seen 2026-11-14): "<col>⏎</col>", "<col></col>",
// "<col></col><col></col>", "<col color=\"red\">⏎</col>" and "<col
// width=\"120\">⏎⏎</col>" are the <col … />s they stand for, the column's
// color and width kept. A line there that is no column — text, a </col> no
// <col> opened, a <col> no </col> closes before the </colgroup> or another
// <col> — is refused, and so is a column holding text, the table's tags
// then each bare, and one no </col> closes before the end, read to it.

var (
	notionMarkdownVersion2ColgroupOpening = regexp.MustCompile(`^<colgroup(?:[ \t][^<>]*)?>$`)
	notionMarkdownVersion2ColOpening      = regexp.MustCompile(`^<col((?:[ \t]+[^<>]*?)?)[ \t]*(/?)>(.*)$`)
	notionMarkdownVersion2ColRun          = regexp.MustCompile(`^(?:<col(?:[ \t]+[^<>]*?)?[ \t]*(?:/>|>[ \t]*</col>)[ \t]*){2,}$`)
	notionMarkdownVersion2ColTag          = regexp.MustCompile(`<col(?:[ \t]+[^<>]*?)?[ \t]*(?:/>|>[ \t]*</col>)`)
	notionMarkdownVersion2TableStructure  = regexp.MustCompile(`<(colgroup|col|tr|td|th)(?:[ \t][^<>]*)?>`)
)

const (
	notionMarkdownColgroupChildSaid = "<colgroup> holds only <col>." + notionMarkdownPartialChildSaid
	notionMarkdownColumnEmptySaid   = `A table column holds nothing: write <col width="120" />.`
)

// notionMarkdownVersion2Colgroups writes each <colgroup>'s columns closed in
// themselves, one to a line, outside code, and what Notion refuses of them,
// if anything.
func notionMarkdownVersion2Colgroups(lines []string) ([]string, string) {
	out, fence, grouped := make([]string, 0, len(lines)), "", false
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
		case !grouped:
			grouped = notionMarkdownVersion2ColgroupOpening.MatchString(trimmed)
		case trimmed == "" || strings.HasSuffix(trimmed, "/>") && notionMarkdownVersion2ColOpening.MatchString(trimmed) &&
			!notionMarkdownVersion2ColRun.MatchString(trimmed):
		case trimmed == "</colgroup>":
			grouped = false
		default:
			columns, used, said := notionMarkdownVersion2ColgroupColumns(lines, index)
			// A <colgroup> no </colgroup> closes is said so first (seen
			// 2026-11-14: "<colgroup><col /></colgroup >").
			if said == notionMarkdownColgroupChildSaid && !notionMarkdownVersion2ColgroupCloses(lines[index:]) {
				said = notionMarkdownUnclosedBlock("colgroup") + "\n" + said
			}
			if said != "" {
				return lines, said
			}
			out = append(out, columns...)
			index += used - 1
			continue
		}
		out = append(out, line)
	}
	return out, ""
}

// notionMarkdownVersion2ColgroupCloses tells a <colgroup>'s lines from one
// of its own that a </colgroup> closes it before its table's end.
func notionMarkdownVersion2ColgroupCloses(lines []string) bool {
	for _, line := range lines {
		switch strings.TrimSpace(strings.TrimLeft(line, " \t>")) {
		case "</colgroup>":
			return true
		case "</table>":
			return false
		}
	}
	return false
}

// notionMarkdownVersion2ColgroupColumns reads the column a <colgroup>'s line,
// lines[at], opens: the <col … />s it stands for and the lines it took, or
// what Notion refuses of it.
func notionMarkdownVersion2ColgroupColumns(lines []string, at int) ([]string, int, string) {
	line := lines[at]
	lead, trimmed := line[:len(line)-len(strings.TrimLeft(line, " \t"))], strings.TrimSpace(line)
	if notionMarkdownVersion2ColRun.MatchString(trimmed) {
		columns := []string{}
		for _, tag := range notionMarkdownVersion2ColTag.FindAllString(trimmed, -1) {
			match := notionMarkdownVersion2ColOpening.FindStringSubmatch(tag)
			columns = append(columns, lead+"<col"+match[1]+" />")
		}
		return columns, 1, ""
	}
	match := notionMarkdownVersion2ColOpening.FindStringSubmatch(trimmed)
	if match == nil || strings.HasPrefix(trimmed, "<colgroup") {
		return nil, 0, notionMarkdownColgroupChildSaid
	}
	column := []string{lead + "<col" + match[1] + " />"}
	closed := slices.ContainsFunc(lines[at+1:], func(line string) bool { return strings.TrimSpace(line) == "</col>" })
	if held := strings.TrimSpace(match[3]); held == "</col>" {
		return column, 1, ""
	} else if held != "" {
		if strings.HasSuffix(held, "</col>") || closed {
			return nil, 0, notionMarkdownVersion2ColumnHolds(lines, at)
		}
		return nil, 0, notionMarkdownVersion2ColumnOpen(lines, at)
	}
	end := at + 1
	for end < len(lines) && strings.TrimSpace(lines[end]) == "" {
		end++
	}
	next := ""
	if end < len(lines) {
		next = strings.TrimSpace(lines[end])
	}
	switch {
	case next == "</col>":
		return column, end - at + 1, ""
	case end == len(lines) || next == "</colgroup>" || closed && strings.HasPrefix(next, "<col"):
		return nil, 0, notionMarkdownColgroupChildSaid
	case closed:
		return nil, 0, notionMarkdownVersion2ColumnHolds(lines, at)
	}
	return nil, 0, notionMarkdownVersion2ColumnOpen(lines, at)
}

// notionMarkdownVersion2ColumnHolds is what Notion says of a column, at
// lines[at], holding text: that, then each of its table's tags bare, from
// its <colgroup> to its </table>.
func notionMarkdownVersion2ColumnHolds(lines []string, at int) string {
	start := at
	for start > 0 && !notionMarkdownVersion2ColgroupOpening.MatchString(strings.TrimSpace(lines[start])) {
		start--
	}
	said := []string{notionMarkdownColumnEmptySaid}
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "</table>" {
			break
		}
		for _, tag := range notionMarkdownVersion2TableStructure.FindAllStringSubmatch(line, -1) {
			said = append(said, notionMarkdownTablePartAlone(tag[1]))
		}
	}
	return strings.Join(said, "\n")
}

// notionMarkdownVersion2ColumnOpen is what Notion says of a column, at
// lines[at], that no </col> closes, read as a flow tag to the end twice (seen
// 2026-11-14: "<col>⏎<col>⏎</colgroup>…", "<col color=\"red\" >⏎</col >…"):
// its closing tag missing, the <colgroup>'s too where no </colgroup>
// follows, and then the table's tags bare, its rows' and cells' after it
// but no other column's.
func notionMarkdownVersion2ColumnOpen(lines []string, at int) string {
	missing := []string{}
	if !slices.ContainsFunc(lines[at:], func(line string) bool { return strings.TrimSpace(line) == "</colgroup>" }) {
		missing = append(missing, notionMarkdownUnclosedBlock("colgroup"))
	}
	missing = append(missing, notionMarkdownUnclosedBlock("col"))
	said := slices.Concat(missing, []string{notionMarkdownColumnEmptySaid}, missing[:len(missing)-1],
		[]string{notionMarkdownTablePartAlone("colgroup"), missing[len(missing)-1],
			notionMarkdownTablePartAlone("col")})
	for _, line := range lines[at+1:] {
		if strings.TrimSpace(line) == "</table>" {
			break
		}
		for _, tag := range notionMarkdownVersion2TableStructure.FindAllStringSubmatch(line, -1) {
			if tag[1] != "col" && tag[1] != "colgroup" {
				said = append(said, notionMarkdownTablePartAlone(tag[1]))
			}
		}
	}
	return strings.Join(said, "\n")
}

// A table's <colgroup> written with its columns on one line, or a line of it
// holding more than one tag, reads as each tag on a line of its own, as
// Notion's "v2" and its MCP read it (seen 2026-11-14): "<colgroup><col
// color=\"red\" /></colgroup>" is the column of that color,
// "<colgroup><col></col></colgroup>" a column, "<colgroup><col
// color=\"red\"></colgroup>" and "<colgroup>⏎<col /><col color=\"red\">⏎
// </colgroup>" a column no </col> closes before the </colgroup>, refused,
// "<colgroup><col><col></colgroup>" a column holding one, "<colgroup>x
// </colgroup>" text there.

var notionMarkdownVersion2ColgroupPiece = regexp.MustCompile(`<[^<>]*>|[^<>]+`)

// notionMarkdownVersion2ColgroupLines puts each tag and text of a table's
// <colgroup> on a line of its own, outside code, to the </colgroup> or a
// line of the table's rows.
func notionMarkdownVersion2ColgroupLines(lines []string) []string {
	out, fence, table, grouped := make([]string, 0, len(lines)), "", false, false
	for _, line := range lines {
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t>"))]
		body := strings.TrimSpace(line[len(lead):])
		if grouped && notionMarkdownVersion2ColgroupEnd.MatchString(body) {
			grouped = false
		}
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(body, fence, true) {
				fence = ""
			}
		case notionMarkdownVersion2Fence(body):
			fence = notionMarkdownFenceOf(body)
		case !grouped && notionMarkdownVersion2TableLine.MatchString(body):
			table = true
		case !grouped && strings.HasPrefix(body, "</table>"):
			table = false
		case table && body != "" && (grouped || notionMarkdownVersion2ColgroupStart.MatchString(body)):
			grouped = true
			for _, piece := range notionMarkdownVersion2ColgroupPiece.FindAllString(body, -1) {
				if piece = strings.TrimSpace(piece); piece != "" {
					out = append(out, lead+piece)
					grouped = grouped && piece != "</colgroup>"
				}
			}
			continue
		}
		out = append(out, line)
	}
	return out
}

var (
	notionMarkdownVersion2ColgroupStart = regexp.MustCompile(`^<colgroup(?:[ \t][^<>]*)?>`)
	// notionMarkdownVersion2ColgroupEnd is a line of a table's rows, which no
	// <colgroup> left open holds.
	notionMarkdownVersion2ColgroupEnd = regexp.MustCompile(`^</?(?:tr|thead|tbody|table)(?:[ \t>]|$)`)
)
