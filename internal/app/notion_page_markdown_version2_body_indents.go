package app

import (
	"regexp"
	"strings"
)

// How a "v2" write's lines read inside a <details>, as Notion's "v2" reads
// them (seen 2026-11-14): its first line, a <summary>, is its title however
// far in it stands ("<details>⏎\t<summary>a</summary>⏎</details>" and "  \t"
// alike are the toggle "a"), and its lines after that are blocks of their
// own, read as at the top once the fewest whitespace characters any of them
// starts with go, and the whitespace its first line still starts with:
// "<details>⏎\t<summary>a</summary>⏎\tc⏎</details>" is the toggle "a"
// holding "c", "\t- b⏎\t\t- c" an item holding an item, "\t\t- a⏎\tb" the
// item "a⏎b", "\t- a⏎  - b" two items, "\t\tT⏎\t\t- a⏎\t- b" the paragraph
// "T⏎- a" and the item "b" — a tab one character as a space is. Code stands
// as its fence does ("\t```⏎\tx⏎\t```" is the code "x"); a tag standing in a
// list item or a quote counts from where its own line starts.

var notionMarkdownVersion2DetailsLine = regexp.MustCompile(`^<details(?:[ \t][^<>]*)?>`)

// notionMarkdownVersion2DetailsIndents puts the lines of the <details> whose
// opening tag is lines[start] as Notion's "v2" reads them, and tells whether
// it is one.
func notionMarkdownVersion2DetailsIndents(lines []string, start int) bool {
	lead := notionMarkdownVersion2Lead(lines[start])
	opening := strings.TrimSpace(lines[start])
	if !notionMarkdownVersion2DetailsLine.MatchString(opening) {
		return false
	}
	end := notionMarkdownVersion2BodyEnd(lines, start, "details")
	if end < 0 {
		return true
	}
	body := start + 1
	if !notionMarkdownVersion2SummaryOpening.MatchString(strings.TrimSpace(opening[len(notionMarkdownVersion2DetailsLine.FindString(opening)):])) {
		for body < end && strings.TrimSpace(lines[body]) == "" {
			body++
		}
		if body < end && notionMarkdownVersion2SummaryOpening.MatchString(strings.TrimSpace(lines[body])) &&
			strings.HasPrefix(lines[body], lead) {
			lines[body] = lead + strings.TrimLeft(lines[body][len(lead):], " \t")
			body++
		} else {
			body = start + 1
		}
	}
	notionMarkdownVersion2BodyDedent(lines[body:end], lead)
	if strings.HasPrefix(lines[end], lead) {
		lines[end] = lead + strings.TrimLeft(lines[end][len(lead):], " \t")
	}
	return true
}

// notionMarkdownVersion2CalloutIndents puts the lines of the <callout> whose
// opening tag is lines[start] as Notion's "v2" reads them (seen 2026-11-14):
// as a <details>'s blocks, its first line its text — "<callout>⏎\t- a⏎- b⏎
// </callout>" and "  - a⏎\t- b" hold two items, "\t\t- a⏎\tb" the item
// "a⏎b", "\t\tT⏎\t- a⏎\t\t- b" the callout "T" holding an item holding
// an item.
func notionMarkdownVersion2CalloutIndents(lines []string, start int) {
	lead := notionMarkdownVersion2Lead(lines[start])
	if end := notionMarkdownVersion2BodyEnd(lines, start, "callout"); end >= 0 {
		notionMarkdownVersion2BodyDedent(lines[start+1:end], lead)
		if strings.HasPrefix(lines[end], lead) {
			lines[end] = lead + strings.TrimLeft(lines[end][len(lead):], " \t")
		}
	}
}

// notionMarkdownVersion2ColumnIndents puts the lines of the <columns> whose
// opening tag is lines[start] as Notion's "v2" reads them (seen 2026-11-15,
// written twice each): each column's lines as at the top, past the fewest
// whitespace characters any of them starts with — "<columns>⏎\t<column>⏎
// \t\t- a⏎\t</column>…" holds the item "a", "\t\t> q" the quote "q", and a
// closing tag closing none on a line of its own after a blank line, "\t\tx⏎
// ⏎\t\t</ul>" or "\t\t\t</ul>", is a paragraph after "x" as at the top, not
// one "x" holds. Its columns' tags and lines stand as far in as its own tag,
// as "<columns>⏎<column>⏎…" is written. A <tab>'s lines are not read so
// ("\t\tx⏎⏎\t\ty" in a tab is "x" holding "y").
func notionMarkdownVersion2ColumnIndents(lines []string, start int) {
	lead := notionMarkdownVersion2Lead(lines[start])
	end := notionMarkdownVersion2BodyEnd(lines, start, "columns")
	for at := start + 1; end >= 0 && at < end; at++ {
		trimmed := strings.TrimSpace(lines[at])
		if !notionMarkdownVersion2ColumnLine.MatchString(trimmed) {
			continue
		}
		close := notionMarkdownVersion2BodyEnd(lines, at, "column")
		if close < 0 || close > end {
			return
		}
		lines[at], lines[close] = lead+trimmed, lead+strings.TrimSpace(lines[close])
		body, fewest := lines[at+1:close], -1
		for _, line := range body {
			if strings.TrimSpace(line) != "" && (fewest < 0 || len(notionMarkdownVersion2Lead(line)) < fewest) {
				fewest = len(notionMarkdownVersion2Lead(line))
			}
		}
		for index, line := range body {
			if strings.TrimSpace(line) != "" {
				body[index] = lead + line[fewest:]
			}
		}
		at = close
	}
}

// notionMarkdownVersion2BodyDedent takes off a tag's lines, past lead (what
// its opening tag's line starts with), the fewest whitespace characters any
// of them that isn't blank starts with after lead, and off the first of them
// all it still starts with.
func notionMarkdownVersion2BodyDedent(body []string, lead string) {
	fewest, first := -1, -1
	for at, line := range body {
		if strings.TrimSpace(line) == "" {
			continue
		}
		extra := 0
		if strings.HasPrefix(line, lead) {
			extra = len(notionMarkdownVersion2Lead(line[len(lead):]))
		}
		if first < 0 {
			first = at
		}
		if fewest < 0 || extra < fewest {
			fewest = extra
		}
	}
	if first < 0 {
		return
	}
	for at, line := range body {
		if strings.TrimSpace(line) != "" && fewest > 0 {
			body[at] = lead + line[len(lead)+fewest:]
		}
	}
	if line := body[first]; strings.HasPrefix(line, lead) {
		body[first] = lead + strings.TrimLeft(line[len(lead):], " \t")
	}
}

// notionMarkdownVersion2BodyEnd is the line closing the tag that opens the
// line at start, the same tags opened and closed between counted and code
// passed over; -1 for none.
func notionMarkdownVersion2BodyEnd(lines []string, start int, name string) int {
	opening := regexp.MustCompile(`^<` + name + `(?:[ \t][^<>]*)?>`)
	depth, fence := 0, ""
	for index := start + 1; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence = ""
			}
		case notionMarkdownVersion2Fence(trimmed):
			fence = notionMarkdownFenceOf(trimmed)
		case opening.MatchString(trimmed) && !strings.Contains(trimmed, "</"+name+">"):
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

// notionMarkdownVersion2Lead is the whitespace a line starts with.
func notionMarkdownVersion2Lead(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
