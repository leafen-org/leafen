package app

import (
	"regexp"
	"time"
)

// A page's fetch gives a title or text value in <properties> as Notion's
// "v2" Markdown, so a value written with ~ or _ reads back escaped
// (mcpFetchTextValueForm). The fetch says so after the page while one of its
// values reads escaped or holds a line break, where an agent checks a write
// against what it wrote, and says nothing on the others, as every row an
// agent read said it again (user 2026-11-26, an agent's real use: its
// tokens ran out reading).

// mcpFetchEscapedValue finds what the Markdown of a value writes apart from
// its words: a literal character escaped, or a line break.
var mcpFetchEscapedValue = regexp.MustCompile("\\\\[~_*`\\[\\]|$\\\\<]|<br />")

// mcpFetchTextValueNote is what follows a page's fetch of how its text
// values read, "" while none of them reads escaped.
func mcpFetchTextValueNote(properties map[string]any) string {
	for _, value := range properties {
		if text, isText := value.(string); isText && mcpFetchEscapedValue.MatchString(text) {
			return "\n\n" + mcpFetchTextValueForm
		}
	}
	return ""
}

// mcpTimesNoteWhereHeld is what follows a data source's table head of the
// clock its times are on (mcpTimesOnClock): said only while the table has
// a column holding a time — a date, or the times Leafen sets — as a source
// without one said it on every fetch (user 2026-11-26, an agent's real use).
func mcpTimesNoteWhereHeld(properties []notionMCPDataSourceProperty, clock *time.Location) string {
	for _, property := range properties {
		switch property.Type {
		case graphValueTypeDate, graphValueTypeCreatedTime, graphValueTypeLastEditedTime:
			return " " + mcpTimesOnClock(clock)
		}
	}
	return ""
}
