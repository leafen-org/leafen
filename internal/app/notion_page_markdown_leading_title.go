package app

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode"
)

// A page made from Markdown takes its first line as its title line when that
// line is a first-level heading, as Notion's create-pages does (seen
// 2026-11-13, fourteen shapes): content beginning "# X" — on its very first
// line, the content's only line starting "# ", with more after it — leaves
// that line out of the body; an ordinary page given no title takes X as its
// title, a database page keeps the title it was given. A second line
// starting "# " (in a code block too), the heading alone, a blank line or
// another block before it, or "## " keeps every line as a block. Writing into
// a page that is there (replace and insert) keeps the heading, as Notion's
// update-page does, and an imported file keeps its first heading in the body
// under the file's name, as Notion's import does
// (workspace_import_field_pages.go). A toggle heading, whose children would
// go with it, stays a block.
//
// The public API's create-page reads the line so too, at "v1" and "v2", in
// a page and a database alike (seen 2026-11-14, sixteen shapes in a page,
// ten in a database): its title is the line's words as written, trimmed —
// "# h {color=\"red\"}" titles a page "h {color=\"red\"}", "# **h** x"
// "**h** x" — when the request sends no properties; properties sent, even
// none ({}), an empty title or a database page's other values alone, keep
// the page's title as they give it, the line gone all the same. A toggle
// heading's line goes too, the lines under it standing as blocks of the
// page (notionMarkdownAPITitleLine).

// notionMarkdownAttributeSuffix is a block line's closing {…} attributes.
var notionMarkdownAttributeSuffix = regexp.MustCompile(`\s*\{[^{}]*\}\s*$`)

// notionMarkdownLeadingTitle reads an agent's Markdown's title line: the
// heading's words and the Markdown after it, or false when it has none.
func notionMarkdownLeadingTitle(markdown string) (string, string, bool) {
	first, rest, found := notionMarkdownTitleLine(markdown)
	if !found || strings.Contains(first, `toggle="true"`) {
		return "", markdown, false
	}
	title := strings.TrimSpace(notionMarkdownAttributeSuffix.ReplaceAllString(strings.TrimPrefix(first, "# "), ""))
	if title == "" {
		return "", markdown, false
	}
	return title, rest, true
}

// notionMarkdownAPITitleLine takes a public API create-page request's title
// line out of its Markdown: the page's title from it, when the request sends
// no properties, or false.
func notionMarkdownAPITitleLine(input *notionPageCreateRequest) (string, bool) {
	if input.Markdown == nil {
		return "", false
	}
	line, rest, found := notionMarkdownAPILeadingTitle(*input.Markdown)
	if !found {
		return "", false
	}
	input.Markdown = &rest
	return line, !input.Properties.given()
}

// notionMarkdownAPILeadingTitle reads the public API's create-page Markdown's
// title line: the line's words as written and the Markdown after it, or
// false when it has none.
func notionMarkdownAPILeadingTitle(markdown string) (string, string, bool) {
	first, rest, found := notionMarkdownTitleLine(markdown)
	title := strings.TrimSpace(strings.TrimPrefix(first, "# "))
	if !found || title == "" {
		return "", markdown, false
	}
	return title, rest, true
}

// notionMarkdownTitleLine finds a title line, the content's first and its
// only one starting "# ", with more after it. The whitespace lines start
// with goes first, however much, a tab's and a no-break space's too (seen
// 2026-11-14, API "v1" and "v2" and MCP alike: "\t# h⏎x", " # h⏎x" and
// "    # h⏎x" are the page "h" holding "x", "# h⏎\t# i" two lines kept).
func notionMarkdownTitleLine(markdown string) (string, string, bool) {
	normalized := strings.ReplaceAll(markdown, "\r\n", "\n")
	first, rest, _ := strings.Cut(normalized, "\n")
	first = strings.TrimLeftFunc(first, notionMarkdownTitleSpace)
	if !strings.HasPrefix(first, "# ") || strings.TrimSpace(rest) == "" {
		return "", "", false
	}
	// Notion counts the lines, a code block's among them: a "# " line in
	// one keeps the heading too.
	for _, line := range strings.Split(rest, "\n") {
		if strings.HasPrefix(strings.TrimLeftFunc(line, notionMarkdownTitleSpace), "# ") {
			return "", "", false
		}
	}
	return first, rest, true
}

// notionMarkdownTitleSpace tells the whitespace a title line's start goes
// without, as a script's trimming takes it.
func notionMarkdownTitleSpace(r rune) bool {
	return r == '\ufeff' || r != '\u0085' && unicode.IsSpace(r)
}

// notionPlainPageTitle is a page's title of plain words, as its title
// property reads them.
func notionPlainPageTitle(words string) (string, []graphTextSpan, error) {
	if len([]rune(words)) > maxGraphTitleRunes {
		return "", nil, errors.New("title is too long")
	}
	value, _ := json.Marshal(words)
	spans, err := normalizeGraphRowValueSpans(graphValueTypeTitle, value, nil)
	if err != nil {
		return "", nil, errors.New("title rich text is invalid")
	}
	return words, spans, nil
}
