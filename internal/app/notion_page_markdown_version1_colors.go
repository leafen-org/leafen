package app

import (
	"regexp"
	"strings"
)

// A public API write's "v1" Markdown takes a block's color from the first
// {color="…"} in its line, wherever it stands and with or without a space
// before it, as Notion's does (seen 2026-11-14, each written and read back
// as blocks): "y{color=\"red\"}" is the red paragraph "y", "a {color=\"red\"}
// b" the red "a  b", "{color=\"red\"}a", "`a{color=\"red\"}`" and "*b{color=
// \"red\"}*" each red with the mark gone from their text and code, and a
// heading's, a list item's, a to-do's and a quote's line alike; its } may
// be missing ("a{color=\"red\"" is red "a"). The line stands as the block it
// starts with the mark in it, so "#{color=\"red\"}", "-{color=\"red\"}",
// "---{color=\"red\"}" and "- [ ]{color=\"red\"}" are a red "#", "-", "---"
// and an item "[ ]", and "$${color=\"red\"}" a red empty equation; the
// whitespace its text ends with goes. A color Notion's Markdown doesn't name
// ("nope", "RED", "blue_background", "default_bg") is the default, and a
// second mark is text ("a {color=\"red\"}{color=\"blue\"}" is the red
// "a {color=\"blue\"}"). A callout's first line, its text, keeps its color
// as that line's ("<callout>⏎a{color=\"red\"}⏎</callout>" is the callout
// "a" whose Markdown writes "a {color=\"red\"}", notionCalloutFirstLine).
// The list items and to-dos a line starts that hold the block it
// goes on with take its color too, as their own lines' marks (seen
// 2026-11-14: "- ## h {color=\"red\"}", "- > q {color=\"red\"}", "- [ ] ##
// {color=\"red\"}", "1. - 2. b {color=\"green\"}" each all red or green,
// "- ``` {color=\"red\"}" the red item holding code of no color), but for
// one with {toggle="true" color="…"}. A heading's {toggle="true"} after its
// text needs no space before it either ("## h{toggle=\"true\"}",
// "## h{toggle=\"true\"}{color=\"red\"}"). Leafen's reader takes the color
// after a space at the line's end (notion_page_markdown_block_color.go).

var (
	notionMarkdownVersion1ColorMark  = regexp.MustCompile(`\{color="([A-Za-z0-9_]+)"\}?`)
	notionMarkdownVersion1ToggleMark = regexp.MustCompile(`[ \t]*(\{toggle="true"(?:[ \t]+color="[a-z_]+")?\})$`)
	notionMarkdownVersion1ColorNames = map[string]bool{
		"gray": true, "brown": true, "orange": true, "yellow": true, "green": true,
		"blue": true, "purple": true, "pink": true, "red": true,
	}
)

// notionMarkdownVersion1Colored is a line's rest without the first color
// mark in it, written to read as the block the line starts, and the mark
// Leafen's reader takes after it, " {color=\"default\"}" for a color Notion
// names none of; none without a mark. Lines of tags other than text keep
// theirs.
func notionMarkdownVersion1Colored(rest string) (string, string) {
	if tag, read := notionMarkdownVersion1ReadTag(strings.TrimSpace(rest)); read &&
		(notionMarkdownVersion1BlockTags[tag.name] || notionMarkdownVersion1CaptionTags[tag.name]) {
		return rest, ""
	}
	head := notionMarkdownVersion1BlockHead(rest)
	text, mark := notionMarkdownVersion1TakeColor(rest[len(head):])
	if mark == "" {
		if strings.HasPrefix(head, "#") {
			return head + notionMarkdownVersion1ToggleMark.ReplaceAllString(text, " $1"), ""
		}
		return rest, ""
	}
	switch {
	case strings.HasPrefix(head, "#"):
		text = notionMarkdownVersion1ToggleMark.ReplaceAllString(text, " $1")
	case head == "" && (notionMarkdownFlowStarts(text, "", true) || strings.HasPrefix(text, "|") || strings.HasPrefix(text, "<")):
		text = notionMarkdownVersion1TabName(text)
	case head != "" && !strings.HasPrefix(head, ">") && notionMarkdownFlowTodo.MatchString(text) && !notionMarkdownFlowTodo.MatchString(strings.TrimSpace(head[1:])):
		// An item's text that would make it a to-do is its text.
		text = `\` + text
	case head != "" && !strings.HasPrefix(head, ">") && text == "$$":
		// And $$ an equation of nothing in it, as "v1" writes one (seen
		// 2026-11-14: "- $$ {color=\"red\"}"), where "- $$" holds one.
		text = "$``$"
	}
	return head + text, mark
}

// notionMarkdownVersion1TakeColor is a block's text without the first color
// mark in it and the whitespace it then ends with, and the mark Leafen's
// reader takes after it; none without a mark.
func notionMarkdownVersion1TakeColor(text string) (string, string) {
	at := notionMarkdownVersion1ColorMark.FindStringSubmatchIndex(text)
	if at == nil {
		return text, ""
	}
	name, color := text[at[2]:at[3]], "default"
	if base := strings.TrimSuffix(name, "_bg"); notionMarkdownVersion1ColorNames[base] {
		color = name
	}
	return strings.TrimRight(text[:at[0]]+text[at[1]:], " \t"), ` {color="` + color + `"}`
}

// notionMarkdownVersion1SummaryColored is a toggle's <summary> line without
// the color mark in its text, and its <details> line with the color it
// names, when it names none of its own (seen 2026-11-14:
// "<details>⏎<summary>s{color=\"red\"}</summary>⏎</details>" is the red
// toggle "s").
func notionMarkdownVersion1SummaryColored(summary, details string) (string, string) {
	opening, closing := strings.Index(summary, ">"), strings.LastIndex(summary, "</summary>")
	if !strings.HasPrefix(summary, "<summary") || opening < 0 || closing < opening {
		return summary, details
	}
	text, mark := notionMarkdownVersion1TakeColor(summary[opening+1 : closing])
	if mark == "" {
		return summary, details
	}
	if color := strings.TrimSuffix(strings.TrimPrefix(mark, ` {color="`), `"}`); color != "default" &&
		strings.HasSuffix(details, ">") && !strings.Contains(details, ` color="`) {
		details = strings.TrimSuffix(details, ">") + ` color="` + color + `">`
	}
	return summary[:opening+1] + text + summary[closing:], details
}

// notionMarkdownVersion1BlockHead is what a line's rest starts its block
// with before its text: a heading's marks and the space after them, a list
// item's or a to-do's marker and box, a quote's >, or none for a paragraph.
func notionMarkdownVersion1BlockHead(rest string) string {
	if strings.HasPrefix(rest, ">") {
		return ">"
	}
	if notionMarkdownVersion1HeldHeading.MatchString(rest) {
		return rest[:len(rest)-len(strings.TrimLeft(strings.TrimLeft(rest, "#"), " \t"))]
	}
	if marker := notionMarkdownVersion1Item.FindString(rest); marker != "" {
		return marker
	}
	return ""
}
