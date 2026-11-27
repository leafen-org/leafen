package app

import (
	"errors"
	"regexp"
	"strings"
)

// Notion reads a toggle's blocks as the lines between its <details> and
// </details> however they are indented, and its <summary> on the opening
// line too (seen 2026-10-28 through its MCP: `<details><summary>Title</summary>`,
// the blocks unindented below and </details> after, makes a toggle; left
// unclosed, Notion refuses it). Leafen's reading takes a toggle's blocks one
// level in, as it wrote them before 2026-10-29, so such a toggle is first put that way: its
// title on a line of its own and its blocks one level in. A toggle whose
// blocks are already in, as Leafen writes it, stays as it is.

var notionMarkdownOneLineDetailsPattern = regexp.MustCompile(`^(<details(?:[ \t]+color="[a-z_]*")?[ \t]*>)[ \t]*(<summary>.*</summary>)[ \t]*$`)

var errNotionMarkdownToggleOpen = errors.New("A toggle opened as <details><summary>…</summary> on one line isn't closed: put </details> on a line of its own after its blocks.")

// notionMarkdownToggleBlocks puts each toggle whose blocks stand at its own
// level, or whose title shares its opening line, as Leafen reads a toggle.
func notionMarkdownToggleBlocks(markdown string) (string, error) {
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0, len(lines))
	fenced := false
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			fenced = !fenced
		}
		if fenced {
			out = append(out, line)
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		opening, summary, body := "", "", 0
		if match := notionMarkdownOneLineDetailsPattern.FindStringSubmatch(trimmed); match != nil {
			opening, summary, body = match[1], match[2], index+1
		} else if notionMarkdownDetailsPattern.MatchString(trimmed) && index+1 < len(lines) &&
			notionMarkdownSummaryPattern.MatchString(strings.TrimSpace(lines[index+1])) {
			opening, summary, body = trimmed, strings.TrimSpace(lines[index+1]), index+2
		} else {
			out = append(out, line)
			continue
		}
		end := notionMarkdownDetailsEnd(lines, body)
		if end < 0 {
			if body == index+1 {
				return "", errNotionMarkdownToggleOpen
			}
			// Unclosed as Leafen may read it: its blocks are those one level in.
			out = append(out, line)
			continue
		}
		if !notionMarkdownBlocksIn(lines[body:end], notionMarkdownIndent(line)) {
			for at := body; at < end; at++ {
				if strings.TrimSpace(lines[at]) != "" {
					lines[at] = "\t" + lines[at]
				}
			}
		}
		out = append(out, indent+opening, indent+summary)
		index = body - 1
	}
	return strings.Join(out, "\n"), nil
}

// notionMarkdownDetailsEnd is the line closing the toggle whose blocks begin
// at body, past the toggles inside it, or -1.
func notionMarkdownDetailsEnd(lines []string, body int) int {
	depth, fenced := 0, false
	for at := body; at < len(lines); at++ {
		trimmed := strings.TrimSpace(lines[at])
		if strings.HasPrefix(trimmed, "```") {
			fenced = !fenced
		}
		switch {
		case fenced:
		case notionMarkdownDetailsPattern.MatchString(trimmed) || notionMarkdownOneLineDetailsPattern.MatchString(trimmed):
			depth++
		case trimmed == "</details>" && depth == 0:
			return at
		case trimmed == "</details>":
			depth--
		}
	}
	return -1
}

// notionMarkdownBlocksIn says whether every written line stands deeper than
// a toggle's own level: the lines of code and of an equation stand as
// written, the public API's "v1" writing them at the left (seen
// 2026-11-10), so their fences and $$ say where they stand.
func notionMarkdownBlocksIn(lines []string, level int) bool {
	fence, equation := "", false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		inside := fence != "" || equation && trimmed != "$$"
		switch {
		case fence != "":
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, "`") == "" {
				fence, inside = "", false
			}
		case equation:
			// Only $$ closes it, a line with a color after its $$ being one
			// of its lines (notionMarkdownVersion1EquationCloses).
			equation = trimmed != "$$"
		case strings.HasPrefix(trimmed, "```"):
			fence = notionMarkdownFenceOf(trimmed)
		case trimmed == "$$" || strings.HasPrefix(trimmed, "$$ {"):
			// A colored one carries its color on its opening line.
			equation = true
		}
		if !inside && trimmed != "" && notionMarkdownIndent(line) <= level {
			return false
		}
	}
	return true
}
