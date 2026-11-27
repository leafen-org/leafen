package app

import (
	"regexp"
	"slices"
	"strings"
)

// A "v2" block whose tag holds nothing — an empty block, a table of contents
// and an <unknown> — is refused written as a pair of tags, as Notion's "v2"
// reads it (seen 2026-11-14): "<empty-block>⏎</empty-block>",
// "<table-of-contents></table-of-contents>", "<unknown url=…>x</unknown>",
// in a list item, a quote or a callout too, its attributes and what it holds
// whatever they are, each pair said in turn after the tags left open. Its
// opening tag alone is its words ("<table-of-contents>" too), and so is it
// in text ("x <empty-block></empty-block> y") or where text follows its
// closing tag on its line (notionMarkdownVersion2BrokenPairs). v1's
// <table_of_contents> is no such tag.

var (
	notionMarkdownVersion2HoldsNothing = regexp.MustCompile(`^<(empty-block|table-of-contents|unknown)(?:[ \t]+[^<>]*?)?[ \t]*>`)
	notionMarkdownVersion2WellQuoted   = regexp.MustCompile(`^<[a-z-]+(?:[ \t]+` + notionMarkdownAttributeName + `="[^"]*")*[ \t]*/?>$`)
)

// selfClosing says what Notion says of lines[index] when its text past its
// blocks' marks, body, starts with such a block's opening tag that a closing
// tag of its name after it closes, nothing after that on its line.
func (repair *notionMarkdownRepair) selfClosing(lines []string, index int, body string) {
	// One whose attributes aren't each name="value" is refused for them
	// alone (seen 2026-11-15: "<unknown url=…>x", "<unknown url=\"…\"
	// alt>x", "<unknown url='…'>x</unknown>", "<unknown url=\"…'>x",
	// "<empty-block alt>x", "<unknown url=\"…\" alt>⏎x";
	// notionMarkdownRepair.attributes, loose, openers).
	match := notionMarkdownVersion2HoldsNothing.FindStringSubmatch(body)
	if match == nil || strings.HasSuffix(match[0], "/>") || !notionMarkdownVersion2WellQuoted.MatchString(match[0]) {
		return
	}
	closing := "</" + match[1] + ">"
	from := strings.Index(lines[index], match[0]) + len(match[0])
	for at := index; at < len(lines); at++ {
		line, start := lines[at], 0
		if at == index {
			start = from
		}
		masked := notionMarkdownVersion2PairMasks(line)
		for offset := start; offset < len(line); {
			found := strings.Index(line[offset:], closing)
			if found < 0 {
				break
			}
			found += offset
			offset = found + len(closing)
			if notionMarkdownEscapedAt(line, found) ||
				slices.ContainsFunc(masked, func(code [2]int) bool { return code[0] < found && found < code[1] }) {
				continue
			}
			if strings.TrimSpace(line[offset:]) == "" {
				repair.missing = append(repair.missing, notionMarkdownSelfClosingOnly(match[1]))
			}
			return
		}
	}
	// One no closing tag closes is left open over what follows it to the
	// end, and said so, when anything follows it in the block it stands in
	// (seen 2026-11-14: "<unknown url=…>⏎x", "<unknown>⏎x", "<unknown url=…>
	// x", "> <unknown url=…>⏎> x", "<callout>⏎<unknown url=…>⏎x⏎</callout>",
	// "<empty-block>⏎x", "<table-of-contents>⏎x"; "<unknown url=…>" alone
	// and "<callout>⏎<unknown url=…>⏎</callout>" are its words).
	if notionMarkdownVersion2AnythingAfter(lines, index, from) {
		repair.missing = append(repair.missing, notionMarkdownUnclosedBlock(match[1]), notionMarkdownSelfClosingOnly(match[1]))
	}
}

// notionMarkdownVersion2AnythingAfter tells lines holding text after column
// from of lines[index], before the closing tag of a block that a line before
// it opened.
func notionMarkdownVersion2AnythingAfter(lines []string, index, from int) bool {
	if strings.TrimSpace(lines[index][from:]) != "" {
		return true
	}
	open := []string{}
	for _, line := range lines[:index] {
		body := strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):])
		if match := notionMarkdownVersion2HolderLine.FindStringSubmatch(body); match != nil {
			open = append(open, "</"+match[1]+">")
		} else if len(open) > 0 && body == open[len(open)-1] {
			open = open[:len(open)-1]
		}
	}
	for _, line := range lines[index+1:] {
		body := strings.TrimSpace(line[len(notionMarkdownVersion2MarkerRun.FindString(line)):])
		if len(open) > 0 && body == open[len(open)-1] {
			return false
		}
		if body != "" {
			return true
		}
	}
	return false
}

// notionMarkdownVersion2HolderLine is a line of a block's tag alone that
// holds blocks.
var notionMarkdownVersion2HolderLine = regexp.MustCompile(`^<(callout|toggle|details|quote|column|columns|tab|tabs|synced-block|li|ul|ol|td|th|tr|table)(?:[ \t]+[^<>]*)?>$`)
