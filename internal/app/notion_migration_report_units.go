package app

import (
	"regexp"
	"strings"
)

// The report's merge (notion_migration_report_merge.go) compares what the
// run wrote with what its page reads back, and the page reads a toggle back
// in another form than the run writes it (seen 2026-11-22): the run writes
// "\t<details>⏎\t<summary>title</summary>⏎\t\t- item⏎\t</details>", the
// page reads "  <toggle>⏎  title⏎⏎  - item⏎  </toggle>", its blocks two
// spaces in a level and a blank line after a heading or a toggle. Line by
// line, no toggle line of the one matched the other's, so a rewrite whose
// toggle said a new count kept the page's old toggles as if a person had
// written them and put the new title, its <details> left behind, among them
// as words: "\<summary>… 301개\</summary>" over an older toggle of 304. So
// the merge takes a toggle whole, as one thing that says its title and its
// lines, however indented; a line says what it says at its depth, a tab or
// two spaces a level; and a blank line goes with what is before it, saying
// nothing of its own.

var (
	notionMigrationReportToggleOpen  = regexp.MustCompile(`^<(details|toggle)(?:\s[^>]*)?>$`)
	notionMigrationReportToggleClose = regexp.MustCompile(`^</(details|toggle)>$`)
	notionMigrationReportSummary     = regexp.MustCompile(`^<summary>(.*)</summary>$`)
)

// notionMigrationReportUnits are a report's lines as the merge compares them:
// each toggle whole, with its lines, and each other line, with the blank
// lines after it; texts as written or read, keys as key says each line.
func notionMigrationReportUnits(markdown string, key func(string) string) (texts, keys []string) {
	lines := strings.Split(markdown, "\n")
	for index := 0; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" && len(texts) > 0 {
			texts[len(texts)-1] += "\n" + lines[index]
			continue
		}
		end := -1
		if notionMigrationReportToggleOpen.MatchString(trimmed) {
			end = notionMigrationReportToggleEnd(lines, index)
		}
		if end < 0 {
			texts, keys = append(texts, lines[index]), append(keys, notionMigrationReportLineAt(lines[index], key))
			continue
		}
		texts = append(texts, strings.Join(lines[index:end+1], "\n"))
		keys = append(keys, notionMigrationReportToggleKey(lines[index+1:end], key))
		index = end
	}
	return texts, keys
}

// notionMigrationReportToggleEnd is the line closing the toggle opened at
// open, past the toggles inside it, or -1.
func notionMigrationReportToggleEnd(lines []string, open int) int {
	depth := 0
	for at := open; at < len(lines); at++ {
		trimmed := strings.TrimSpace(lines[at])
		switch {
		case notionMigrationReportToggleOpen.MatchString(trimmed):
			depth++
		case notionMigrationReportToggleClose.MatchString(trimmed):
			if depth--; depth == 0 {
				return at
			}
		}
	}
	return -1
}

// notionMigrationReportToggleKey says what a toggle says from the lines
// inside it: its title — the <summary> the run writes, or the line the page
// reads first — then its lines without their indent or blank lines.
func notionMigrationReportToggleKey(inside []string, key func(string) string) string {
	said := []string{"\x00toggle"}
	for at, line := range inside {
		trimmed := strings.TrimSpace(line)
		if at == 0 {
			if summary := notionMigrationReportSummary.FindStringSubmatch(trimmed); summary != nil {
				trimmed = summary[1]
			}
		} else if trimmed == "" {
			continue
		}
		said = append(said, key(trimmed))
	}
	return strings.Join(said, "\n")
}

// notionMigrationReportLineAt says what a line says at its depth: a tab or
// two spaces a level.
func notionMigrationReportLineAt(line string, key func(string) string) string {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return ""
	}
	depth, spaces := 0, 0
	for _, r := range line[:len(line)-len(trimmed)] {
		if r == '\t' {
			depth++
		} else {
			spaces++
		}
	}
	return strings.Repeat("\t", depth+spaces/2) + key(strings.TrimRight(trimmed, " \t"))
}

// notionMigrationReportTitleAsWords tells a line of the page that reads a
// toggle's title as words, "\<summary>…": a report an earlier merge broke so
// (before 2026-11-22), which the run then writes whole once.
func notionMigrationReportTitleAsWords(key string) bool {
	return strings.HasPrefix(strings.TrimLeft(key, "\t"), "<summary>")
}
