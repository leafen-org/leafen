package app

import (
	"regexp"
	"strings"
)

// notionMarkdownVersion1Verbatim is code or an equation being read in "v1":
// its lines read as written, without the whitespace they all start with
// (seen 2026-11-10: "\tfoo\n  bar" is "foo\n bar", "\tfoo\nbar" stays), and
// stand as far in as its fence or $$, written. A line of whitespace alone in
// code loses as much of it as the others lose, and keeps it all when no other
// line holds more (seen 2026-11-14: "```⏎  a⏎    ⏎  b⏎```" is "a⏎  ⏎b",
// "```⏎  ⏎```" "  ", "```⏎ ⏎" " ⏎"); in code an item's text opens (held:
// "- ```") such lines go, empty ones too ("- ```⏎  a⏎⏎  b⏎  ```" is "a⏎b").
// An equation closes only on $$ alone (notionMarkdownVersion1EquationCloses),
// but in a page's current Markdown, which Leafen's own writer wrote (own,
// notionMarkdownVersion1OwnInput): there a colored one's closing $$ carries
// its color, which goes to the line opening it (opening), where Leafen's
// reader takes it.
type notionMarkdownVersion1Verbatim struct {
	fence    string
	equation bool
	held     bool
	own      bool
	opening  int
	written  string
	lines    []int
}

// notionMarkdownVersion1OwnEquationEnd is the closing line of a colored
// equation Leafen's "v1" writes, as Notion's does (notion_page_markdown_api_form.go).
var notionMarkdownVersion1OwnEquationEnd = regexp.MustCompile(`^\$\$[ \t]+(\{color="[a-z_]+"\})$`)

// notionMarkdownVersion1Input is a "v1" write's Markdown as Leafen's page
// Markdown reader takes it.
func notionMarkdownVersion1Input(markdown string) string {
	return notionMarkdownVersion1Reading(markdown, false)
}

// notionMarkdownVersion1OwnInput is a page's current "v1" Markdown, as
// Leafen's own writer wrote it, read as the blocks it came from
// (pageMarkdownReplacement.readBefore): a colored equation closes on its
// colored closing line.
func notionMarkdownVersion1OwnInput(markdown string) string {
	return notionMarkdownVersion1Reading(markdown, true)
}

func (verbatim *notionMarkdownVersion1Verbatim) open() bool {
	return verbatim.fence != "" || verbatim.equation
}

// take reads lines[index]: one of its lines, or the line closing it, a
// fence's written as the fence it opened with (notionMarkdownFenceCloses).
func (verbatim *notionMarkdownVersion1Verbatim) take(lines []string, index int) {
	trimmed := strings.TrimSpace(lines[index])
	colored := verbatim.equation && verbatim.own && notionMarkdownVersion1OwnEquationEnd.FindStringSubmatch(trimmed) != nil
	closes := verbatim.equation && notionMarkdownVersion1EquationCloses(trimmed) || colored ||
		verbatim.fence != "" && notionMarkdownFenceCloses(trimmed, verbatim.fence, false)
	if colored {
		lines[verbatim.opening] += " " + notionMarkdownVersion1OwnEquationEnd.FindStringSubmatch(trimmed)[1]
		trimmed = "$$"
	}
	if !closes {
		// An equation's blank lines go (seen 2026-11-10: "x\n\ny" is "x\ny").
		if (verbatim.equation || verbatim.held) && trimmed == "" {
			lines[index] = notionMarkdownVersion1Dropped
			return
		}
		verbatim.lines = append(verbatim.lines, index)
		return
	}
	verbatim.close(lines)
	if verbatim.fence != "" {
		trimmed = verbatim.fence
	}
	lines[index] = verbatim.written + trimmed
	*verbatim = notionMarkdownVersion1Verbatim{}
}

// end closes it before lines[index], where the tag holding it closes.
func (verbatim *notionMarkdownVersion1Verbatim) end(lines []string, index int) {
	verbatim.close(lines)
	last := index - 1
	for lines[last] == notionMarkdownVersion1Dropped {
		last--
	}
	closing := verbatim.fence
	if verbatim.equation {
		closing = "$$"
	}
	lines[last] += "\n" + verbatim.written + closing
}

// close puts its lines without what they all start with, as far in as it stands.
func (verbatim *notionMarkdownVersion1Verbatim) close(lines []string) {
	shared := -1
	for _, at := range verbatim.lines {
		if strings.TrimSpace(lines[at]) == "" {
			continue
		}
		if lead := len(lines[at]) - len(strings.TrimLeft(lines[at], " \t")); shared < 0 || lead < shared {
			shared = lead
		}
	}
	for _, at := range verbatim.lines {
		lines[at] = verbatim.written + lines[at][min(max(shared, 0), len(lines[at])):]
	}
	verbatim.lines = nil
}
