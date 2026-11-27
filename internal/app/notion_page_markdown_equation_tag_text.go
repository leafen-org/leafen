package app

import (
	"regexp"
	"strings"
)

// An equation's tag holds its expression as written, as Notion's "v2" and
// its MCP read it (seen 2026-11-14): on one line, its whitespace too —
// "<equation> </equation>" is " ", "<equation> a </equation>" " a " — and on
// lines of its own, its lines between the tags with their whitespace, blank
// ones too — "<equation>⏎ ⏎</equation>" is " ", "<equation>⏎⏎⏎</equation>"
// "⏎", "<equation>⏎  a  ⏎</equation>" "  a  ", "<equation>⏎a⏎ ⏎b⏎</equation>"
// "a⏎ ⏎b", "<equation>⏎a⏎  b⏎</equation>" "a⏎  b". Its "v2" Markdown writes
// the expression without the line breaks it starts and ends with
// (notionMarkdownWriter, seen 2026-11-14: "a⏎" is "$$⏎a⏎$$"), "v1" as it is.

// notionMarkdownVersion2EquationTagOpening is an equation's opening tag on a
// line of its own.
var notionMarkdownVersion2EquationTagOpening = regexp.MustCompile(`^<equation(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*>$`)

// notionMarkdownEquationTagText is the expression an equation's tag on lines
// of its own holds: its lines as written.
func notionMarkdownEquationTagText(body []string) string {
	return strings.Join(body, "\n")
}

// notionMarkdownVersion2EquationTagLines are the lines between an
// equation's tags on lines of their own, whose lines of whitespace are its
// expression's, not blank lines.
func notionMarkdownVersion2EquationTagLines(lines []string) map[int]bool {
	held := map[int]bool{}
	for index := 0; index < len(lines); index++ {
		if !notionMarkdownVersion2EquationTagOpening.MatchString(strings.TrimSpace(lines[index])) {
			continue
		}
		for end := index + 1; end < len(lines); end++ {
			if strings.TrimSpace(lines[end]) == "</equation>" {
				for at := index + 1; at < end; at++ {
					held[at] = true
				}
				index = end
				break
			}
		}
	}
	return held
}

// notionMarkdownWrittenExpression is an equation's expression as the
// writer writes it: in "v2" without the line breaks at its ends.
func notionMarkdownWrittenExpression(expression string, version2 bool) string {
	if version2 {
		return strings.Trim(expression, "\n")
	}
	return expression
}

// An equation's tag over lines whose expression starts on its opening tag's
// line or ends on its closing tag's line reads as on lines of its own, its
// expression all between its tags, as Notion's "v2" and its MCP read it
// (seen 2026-11-14): "<equation> a⏎b </equation>" is " a⏎b ",
// "<equation>a⏎b⏎</equation>" "a⏎b", "<equation>a⏎⏎</equation>" "a⏎",
// "<equation>⏎a⏎b </equation>" "a⏎b ", "<equation> ⏎a</equation>" " ⏎a",
// "<equation>a⏎  b⏎  </equation>" "a⏎  b⏎  ", in a list item ("- <equation>a⏎
// b</equation>" an item holding "a⏎b"), a quote, a callout or a toggle too.
// One with text before its opening tag or after its closing tag on their
// lines is text ("x <equation>a⏎b</equation>", "<equation>a⏎</equation> x"),
// and one a $$ or a code fence stands in is left as it is.

var notionMarkdownVersion2EquationTagRun = regexp.MustCompile(`^((?:[ \t]*>)*[ \t]*(?:(?:[-*+]|\d{1,9}[.)])[ \t]+)?)(<equation(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*>)(.*)$`)

// notionMarkdownVersion2EquationTagRuns writes each such equation's tags on
// lines of their own, outside code.
func notionMarkdownVersion2EquationTagRuns(lines []string) []string {
	out, fence := make([]string, 0, len(lines)), ""
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		body := strings.TrimSpace(strings.TrimLeft(line, " \t>"))
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(body, fence, true) {
				fence = ""
			}
		case notionMarkdownVersion2Fence(body):
			fence = notionMarkdownFenceOf(body)
		default:
			if written, used := notionMarkdownVersion2EquationTagRunAt(lines, index); used > 0 {
				out = append(out, written...)
				index += used - 1
				continue
			}
		}
		out = append(out, line)
	}
	return out
}

// notionMarkdownVersion2EquationTagRunAt is the equation whose opening tag
// stands at lines[start] on lines of its own and the lines it took; none for
// a line that opens no such equation.
func notionMarkdownVersion2EquationTagRunAt(lines []string, start int) ([]string, int) {
	match := notionMarkdownVersion2EquationTagRun.FindStringSubmatch(lines[start])
	if match == nil || strings.Contains(match[3], "</equation>") {
		return nil, 0
	}
	// The lines after the opening one stand as far in as its text: a list
	// item's marker is spaces there, a quote's > stay.
	lead := strings.Map(func(character rune) rune {
		if character == ' ' || character == '\t' || character == '>' {
			return character
		}
		return ' '
	}, match[1])
	for end := start + 1; end < len(lines); end++ {
		at := strings.Index(lines[end], "</equation>")
		inside := strings.TrimSpace(strings.TrimLeft(lines[end], " \t>"))
		if at < 0 && (inside == "$$" || notionMarkdownVersion2Fence(inside)) {
			return nil, 0
		}
		if at < 0 {
			continue
		}
		if strings.TrimSpace(lines[end][at+len("</equation>"):]) != "" {
			return nil, 0
		}
		last, _ := strings.CutPrefix(lines[end][:at], lead)
		if strings.TrimSpace(match[3]) == "" && strings.TrimSpace(last) == "" {
			// Its tags on lines of their own already.
			return nil, 0
		}
		written := []string{match[1] + match[2]}
		if match[3] != "" {
			written = append(written, lead+match[3])
		}
		written = append(written, lines[start+1:end]...)
		if last != "" {
			written = append(written, lead+last)
		}
		return append(written, lead+"</equation>"), end - start + 1
	}
	return nil, 0
}
