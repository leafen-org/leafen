package app

import (
	"regexp"
	"strings"
	"unicode"
)

// The edges of a "v2" text block's text in its tag — <p>, <li>, <todo>,
// <quote>, <h1>…<h4> and <toggle> (notionMarkdownEdgeTags); a callout's own
// line has none. Notion's "v2" reads the tag on lines of its own as holding
// its text between the line break after its opening and the one before its
// closing (seen 2026-11-14), so one blank line more at either end is a line
// break of the text: "<p>⏎⏎b⏎</p>" is "⏎b", "<toggle>⏎b⏎⏎</toggle>" "b⏎", a
// line of spaces too ("<p>⏎  ⏎b⏎</p>" is "⏎b", its tabs kept: "<p>⏎\t⏎b⏎</p>"
// is "\t⏎b"), and blank lines alone one line break fewer than they are
// ("<p>⏎⏎⏎</p>" is "⏎"). Two or more at an end are a blank line between
// blocks, which goes ("<p>⏎⏎⏎b⏎</p>" is "b"), and so is one after blocks the
// tag holds. The whitespace the text's first line starts with and its last
// line ends with stays, standing in <span>s, which the inline reader reads
// as the text they hold (seen 2026-11-10) — save beside such a line break,
// which takes it as any line's end.

// notionMarkdownTitleEdges is a text block's text, read from the tag's
// lines (body) as title, with its edges. A first line of whitespace that
// holds a tab is the text's (notionMarkdownVersion2TagFirstTabs), blank
// lines alone after it too ("<p>⏎\t⏎</p>" is "\t", "<p>⏎\t⏎\t⏎</p>"
// "\t⏎").
func notionMarkdownTitleEdges(body []string, title string) string {
	blank := 0
	for blank < len(body) && strings.TrimSpace(body[blank]) == "" {
		blank++
	}
	if blank == len(body) {
		breaks := strings.Repeat("<br>", max(blank-1, 0))
		if blank == 0 {
			return breaks
		}
		if kept := strings.TrimRight(body[0], " "); kept != "" {
			return "<span>" + kept + "</span>" + breaks
		}
		return breaks
	}
	lines, count := notionMarkdownTitleLines(body)
	if title == "" || count == 0 {
		return title
	}
	last := count - 1
	lead := lines[0][:len(lines[0])-len(strings.TrimLeftFunc(lines[0], unicode.IsSpace))]
	trail := lines[last][len(strings.TrimRightFunc(lines[last], unicode.IsSpace)):]
	if rest := lines[count:]; len(rest) == 1 && strings.TrimSpace(rest[0]) == "" {
		title, trail = notionMarkdownVersion2TextBackslash(title)+"<br>", ""
	}
	if blank == 1 {
		title, lead = "<br>"+title, ""
		if kept := strings.TrimRight(body[0], " "); kept != "" {
			title = "<span>" + kept + "</span>" + title
		}
	}
	if lead != "" {
		title = "<span>" + lead + "</span>" + title
	}
	if trail != "" {
		title = notionMarkdownVersion2TextBackslash(title) + "<span>" + trail + "</span>"
	}
	return title
}

// notionMarkdownTextEdges is one line of text with the whitespace at its
// ends (notionMarkdownTitleEdges), spaces alone too.
func notionMarkdownTextEdges(text string) string {
	if trimmed := strings.TrimSpace(text); trimmed != "" {
		return notionMarkdownTitleEdges([]string{text}, trimmed)
	}
	if text == "" {
		return ""
	}
	return "<span>" + text + "</span>"
}

// notionMarkdownVersion2TagFirstTabs reports whether lines[index], a line
// of whitespace holding a tab, is the first line under a text block's tag
// alone on its line, which keeps it as its text's first line without the
// spaces ending it (seen 2026-11-14: "<p>⏎\t⏎b⏎</p>" is "\t⏎b", " \t" and
// "\t " are " \t" and "\t", in <toggle>, <quote>, <h2>, <li>, <todo> and a
// callout's <p> too); a callout's own text has none ("<callout>⏎\t⏎b⏎
// </callout>" is "b"), and any other such line is a blank line.
func notionMarkdownVersion2TagFirstTabs(lines []string, index int) bool {
	if index == 0 || !strings.Contains(lines[index], "\t") {
		return false
	}
	match := notionMarkdownVersion2TextTagOpening.FindStringSubmatch(strings.TrimSpace(lines[index-1]))
	return match != nil && match[1] != "callout" && strings.TrimSpace(match[3]) == "" || notionMarkdownVersion2ItemTagAlone.MatchString(strings.TrimSpace(lines[index-1]))
}

// notionMarkdownVersion2ItemTagAlone is a list item's or a to-do's tag
// alone on its line.
var notionMarkdownVersion2ItemTagAlone = regexp.MustCompile(`^<(li|todo)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>$`)
