package app

import (
	"regexp"
	"strconv"
	"strings"
)

// A container's tag lines and a <table>'s rows as Leafen reads them
// (notion_page_markdown_containers.go).

// notionMarkdownDedentColumns takes a tag's own indentation off a line inside it.
func notionMarkdownDedentColumns(line string, column int) string {
	taken := 0
	for taken < column && taken < len(line) && line[taken] == ' ' {
		taken++
	}
	return line[taken:]
}

var notionMarkdownTableGroup = regexp.MustCompile(`^</?(?:colgroup|thead|tbody)(?:[ \t][^<>]*)?>$`)

var notionMarkdownTableCellOpen = regexp.MustCompile(`^<t[dh]((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>(.*)$`)

// notionMarkdownTableRows reads a <table> to its </table> as Leafen reads one:
// a <tr> line for each row and a <td>…</td> line for each cell, whose text
// Notion writes on lines of its own. The columns' colors go on the opening
// tag as column-colors, their widths as column-widths and how many they are
// as columns, the rows' and cells' colors stay on <tr> and <td>.
func notionMarkdownTableRows(lines []string, start, level int) ([]string, int) {
	tabs := strings.Repeat("\t", level)
	attributes := notionMarkdownAttributes(notionMarkdownTableOpenPattern.FindStringSubmatch(strings.TrimSpace(lines[start]))[1])
	opening := "<table"
	for _, key := range []string{"fit-page-width", "header-row", "header-column"} {
		if attributes[key] == "true" {
			opening += " " + key + `="true"`
		}
	}
	colorOf := func(attributes string) string {
		return notionMarkdownColorAttribute(notionMarkdownColorValue(notionMarkdownAttributes(attributes)["color"]))
	}
	out := []string{tabs + opening + ">"}
	columnColors, columnWidths := []string{}, []string{}
	for index := start + 1; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		switch {
		case trimmed == "", notionMarkdownTableGroup.MatchString(trimmed):
			// A <thead>'s and a <tbody>'s rows are the table's, in order
			// (seen 2026-11-10); no header comes of them.
		case strings.HasPrefix(trimmed, "<col ") || strings.HasPrefix(trimmed, "<col>") || strings.HasPrefix(trimmed, "<col/"):
			column := notionMarkdownAttributes(strings.TrimPrefix(trimmed, "<col"))
			columnColors = append(columnColors, notionMarkdownColorValue(column["color"]))
			columnWidths = append(columnWidths, column["width"])
			out[0] = tabs + opening + ` columns="` + strconv.Itoa(len(columnColors)) + `"`
			if strings.Join(columnColors, "") != "" {
				out[0] += ` column-colors="` + strings.Join(columnColors, ",") + `"`
			}
			if strings.Join(columnWidths, "") != "" {
				out[0] += ` column-widths="` + strings.Join(columnWidths, ",") + `"`
			}
			out[0] += ">"
		case strings.HasPrefix(trimmed, "<tr"):
			out = append(out, tabs+"<tr"+colorOf(strings.TrimSuffix(strings.TrimPrefix(trimmed, "<tr"), ">"))+">")
		case trimmed == "</tr>":
			out = append(out, tabs+"</tr>")
		case trimmed == "</table>":
			return append(out, tabs+"</table>"), index - start + 1
		default:
			match := notionMarkdownTableCellOpen.FindStringSubmatch(trimmed)
			if match == nil {
				return nil, 0
			}
			cell := []string{}
			rest := match[2]
			closed := false
			for first := true; ; first = false {
				if text, found := strings.CutSuffix(rest, "</td>"); found {
					rest, closed = text, true
				} else if text, found := strings.CutSuffix(rest, "</th>"); found {
					rest, closed = text, true
				}
				// A cell on one line keeps the spaces at its edges, as
				// Notion's (seen 2026-11-11: "<td> a </td>" is " a ").
				if first && closed && rest != "" {
					cell = append(cell, rest)
				} else if strings.TrimSpace(rest) != "" {
					cell = append(cell, strings.TrimSpace(rest))
				}
				if closed || index+1 >= len(lines) {
					break
				}
				index++
				rest = strings.TrimSpace(lines[index])
			}
			out = append(out, tabs+"<td"+colorOf(match[1])+">"+strings.Join(cell, "<br>")+"</td>")
		}
	}
	return nil, 0
}
