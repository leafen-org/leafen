package app

import (
	"fmt"
	"regexp"
	"strings"
)

// The public API's "v2" page Markdown (notionMarkdownVersion2), which
// Notion's MCP fetch writes too (seen 2026-11-11) and so Leafen's
// (mcpMarkdownReader), is Leafen's page Markdown (notion_page_markdown_writer.go)
// written as Notion's API writes it (seen 2026-11-10, every block and mark
// read at both Notion-Versions): a mark's edge spaces stay inside its tag, marks nest
// <s>, <i>, <b>, <span color>, <u> from the outside in, inside a link; text
// escapes _ but not >, the & of what has a character reference's shape
// (notionMarkdownVersion2EscapedText), and a line's start only where it
// would start a block;
// a code fence names its language by Notion's short name; a block whose
// text starts or ends with a space is its tag, and a run of list items one
// list's tags once one of them is, numbered items from their list's start
// and in letters or roman numerals as <ol type> (notion_page_markdown_list_numbers.go);
// the blocks the API doesn't write —
// breadcrumbs, links to pages, buttons — are <unknown … />, as in its "v1".

// notionMarkdownVersion2Escaped escapes text as the API's "v2" does.
var notionMarkdownVersion2Escaped = strings.NewReplacer(
	`\`, `\\`, `*`, `\*`, `_`, `\_`, `~`, `\~`, "`", "\\`", `$`, `\$`, `[`, `\[`, `]`, `\]`,
	`<`, `\<`, `{`, `\{`, `}`, `\}`, `|`, `\|`, `^`, `\^`,
)

// notionMarkdownVersion2EntityShaped is an & before what has a character
// reference's shape — a name of two or more letters and digits, a number,
// a hex number, and its ";" — which "v2" escapes whether or not HTML names
// it (seen 2026-11-11: "\&amp;", "\&foo;", "\&#123456789012;"; "&a;",
// "&1a;", "&a_b;", "&#;", "&copy" and "R&D;" as they are).
var notionMarkdownVersion2EntityShaped = regexp.MustCompile(`&(?:[A-Za-z][A-Za-z0-9]+|#[0-9]+|#[xX][0-9A-Fa-f]+);`)

// notionMarkdownVersion2EscapedText is text escaped as the API's "v2"
// escapes it, so that its reading (notionMarkdownVersion2Entities) reads it
// back as it is.
func notionMarkdownVersion2EscapedText(text string) string {
	escaped := notionMarkdownEscapedTextWith(text, notionMarkdownVersion2Escaped)
	if !strings.Contains(escaped, "&") {
		return escaped
	}
	return notionMarkdownVersion2EntityShaped.ReplaceAllString(escaped, `\$0`)
}

// notionMarkdownVersion2Languages are the code languages whose fence names
// them by another name, Notion's short one.
var notionMarkdownVersion2Languages = map[string]string{
	"assembly": "asm", "c#": "cs", "coffeescript": "coffee", "f#": "fs", "fortran": "f03", "gherkin": "feature",
	"idris": "idr", "julia": "jl", "lisp": "cl", "llvm ir": "llvm", "makefile": "mk", "notion formula": "notion",
	"objective-c": "m", "ocaml": "mli", "plain text": "txt", "prolog": "pro", "protobuf": "proto", "python": "py",
	"reason": "re", "ruby": "rb", "rust": "rs", "typescript": "ts", "vb.net": "vb", "visual basic": "visual-basic",
	"webassembly": "wasm",
}

// notionMarkdownFenceNames are the names a public API Markdown fence reads,
// "v1" and "v2" alike, besides a language's own and the short names any
// fence reads (codeLanguageShortNames).
var notionMarkdownFenceNames = map[string]string{
	"cl": "lisp", "f03": "fortran", "idr": "idris", "mli": "ocaml", "pro": "prolog", "txt": "plain text",
	"text": "plain text", "plaintext": "plain text", "visual-basic": "visual basic",
}

// notionMarkdownFenceLanguage is the language a public API Markdown fence
// names, as Notion's reads it (seen 2026-11-10): a language's own name in
// any case ("PYTHON", "Plain Text"), a short one in small letters ("py",
// not "Py"); none, or one it doesn't know — "zsh", "python3", "python x",
// "java/c/c++/c#" — is JavaScript.
func notionMarkdownFenceLanguage(written string) string {
	name := strings.TrimSpace(written)
	if lower := strings.ToLower(name); lower != "java/c/c++/c#" && validNotionCodeLanguage(lower) {
		return lower
	}
	if language, ok := notionMarkdownFenceNames[name]; ok {
		return language
	}
	if language, ok := codeLanguageShortNames[name]; ok {
		return language
	}
	return "javascript"
}

func notionMarkdownVersion2Language(language string) string {
	// Code without a language is plain text, which a fence without one
	// wouldn't read back as.
	if strings.TrimSpace(language) == "" {
		return notionMarkdownVersion2Languages[plainTextCodeLanguage]
	}
	if short, ok := notionMarkdownVersion2Languages[language]; ok {
		return short
	}
	return language
}

// notionMarkdownVersion2SpanText writes a span as the API's "v2" does, in
// the discussions its words were left on where it names them (discussions,
// "" for none: notionMarkdownDiscussed).
func notionMarkdownVersion2SpanText(span graphTextSpan, discussions string) string {
	var body string
	switch {
	case graphTextSpanIsEquation(span):
		// An equation is written as it is held, as Notion's "v2" writes it;
		// the reading keeps an escaped "\$" as written, so it reads back.
		body = "$" + span.Equation + "$"
	case span.Code:
		// A </code> it holds is written <\\/code>, which reads back as it
		// (seen 2026-11-11: "a</code>b" is "<code>a<\\/code>b</code>"), and a
		// line break as one (notion_markdown_code_breaks.go).
		body = strings.ReplaceAll(span.Text, "</code>", notionMarkdownVersion2CodeClose)
		body = "<code>" + strings.ReplaceAll(body, "\n", notionMarkdownCodeBreak) + "</code>"
	default:
		body = notionMarkdownVersion2EscapedText(span.Text)
	}
	if body == "" {
		return ""
	}
	body = notionMarkdownVersion2Marked(notionMarkdownDiscussed(body, discussions), span)
	if target := notionMarkdownLinkTarget(span.Link); target != "" && span.Mention == "" && !graphTextSpanIsEquation(span) {
		body = "[" + body + "](" + notionMarkdownVersion2Destination(target) + ")"
	}
	return body
}

// notionMarkdownVersion2CodeClose is a </code> inside code as "v2" writes it.
const notionMarkdownVersion2CodeClose = `<\\/code>`

// notionMarkdownVersion2Marked puts a span's marks around what it writes,
// text or a mention's tag, as "v2" does: <s>, <i>, <b>, <span color>, <u>
// from the outside in (seen 2026-11-10, a mention's too).
func notionMarkdownVersion2Marked(body string, span graphTextSpan) string {
	if span.Underline {
		body = "<u>" + body + "</u>"
	}
	if color := notionMarkdownShownColor(span); color != "" {
		body = `<span color="` + color + `">` + body + "</span>"
	}
	if span.Bold {
		body = "<b>" + body + "</b>"
	}
	if span.Italic {
		body = "<i>" + body + "</i>"
	}
	if span.Strikethrough {
		body = "<s>" + body + "</s>"
	}
	return body
}

// notionMarkdownDiscussed puts the discussions words were left on around
// what a span writes, inside its marks, as Notion's MCP fetch does with
// include_discussions (seen 2026-11-11: `<b><span discussion-urls="…">
// word</span></b>`, a run in two discussions naming both, comma-joined);
// as it is for none.
func notionMarkdownDiscussed(body, discussions string) string {
	if discussions == "" || body == "" {
		return body
	}
	return `<span discussion-urls="` + discussions + `">` + body + "</span>"
}

// discussionsOf are the addresses of the discussions a span's words were
// left on that its reader names, comma-joined; "" for none.
func (mentions notionMarkdownMentions) discussionsOf(span graphTextSpan) string {
	if mentions.Discussions == nil || strings.TrimSpace(span.CommentID) == "" {
		return ""
	}
	return mentions.Discussions(span.CommentID, mentions.TextBlock)
}

// notionMarkdownShownColor is the color a span's text shows, as its Markdown
// names it, "" for none: a span with both colors keeps its text color, as
// Notion's Markdown does (seen 2026-10-10).
func notionMarkdownShownColor(span graphTextSpan) string {
	color, background := textSpanColors(span.Color, span.Background)
	if color == "" || color == "default" {
		color = background
	}
	if color == "" || color == "default" || color == "default_background" {
		return ""
	}
	return strings.Replace(color, "_background", "_bg", 1)
}

// notionMarkdownFlavoredSpanText writes a span as its reader's Markdown does.
func notionMarkdownFlavoredSpanText(span graphTextSpan, mentions notionMarkdownMentions) string {
	switch {
	case mentions.APIForm:
		return notionMarkdownAPISpanText(span, mentions.discussionsOf(span))
	case mentions.Version2:
		return notionMarkdownVersion2SpanText(span, mentions.discussionsOf(span))
	}
	return notionMarkdownSpanText(span)
}

var (
	notionMarkdownVersion2Marker  = regexp.MustCompile(`^(?:#{1,6}|[-+])(?:[ \t]|$)|^>|^-{3,}[ \t-]*$`)
	notionMarkdownVersion2Ordered = regexp.MustCompile(`^\d{1,9}[.)](?:[ \t]|$)`)
)

// notionMarkdownVersion2Paragraph keeps a paragraph whose text would start
// another block a paragraph, as "v2" escapes it, and so a list item's, a
// to-do's and a quote's text (seen 2026-11-10: `- \- b`, `> \>r`, `\>q`);
// a number of ten digits or more starts no item, and stays (2026-11-11).
func notionMarkdownVersion2Paragraph(text string) string {
	if notionMarkdownVersion2Marker.MatchString(text) {
		return `\` + text
	}
	if notionMarkdownVersion2Ordered.MatchString(text) {
		at := strings.IndexAny(text, ".)")
		return text[:at] + `\` + text[at:]
	}
	return text
}

// version2Title is a held block's first line in "v2": text that would start
// another block escaped as a paragraph's is, after the spaces it starts with,
// as Notion's writes the text of every tag (seen 2026-11-21: a toggle's, a
// toggle heading's, a callout's, a quote's and a colored paragraph's,
// item's, to-do's or heading's "\- a", "\# h", "\> q", "1\. a", "\---",
// "\+ a", "1\) a" and " \- a", blocks under it or none), and whitespace
// alone in a <p>; as it is otherwise.
func (w *notionMarkdownWriter) version2Title(title string) string {
	if !w.mentions.Version2 {
		return title
	}
	if notionMarkdownWhitespaceText(title) {
		return "<p>" + title + "</p>"
	}
	text := strings.TrimLeft(title, " \t")
	return title[:len(title)-len(text)] + notionMarkdownVersion2Paragraph(text)
}

// notionMarkdownEdgeSpace tells text that starts or ends with a space, which
// "v2" writes in its block's tag.
func notionMarkdownEdgeSpace(text string) bool {
	return text != "" && text != strings.TrimSpace(text)
}

// version2ListRun writes the bulleted or numbered items starting blocks as
// one list's tags, each its <li>, when one of them is a tag, or when they
// are numbered in letters or roman numerals (places, by block); used is
// how many blocks it wrote, none when they stay lines. Numbered items in
// one format stand in one <ol>, from the first one's number, up to one whose
// own start breaks the count, which opens its own.
func (w *notionMarkdownWriter) version2ListRun(
	blocks []workspaceExportMarkdownBlock, places []notionMarkdownListPlace, prefix string,
) (lines []string, used int) {
	list := notionMarkdownListOf(blocks[0].Type)
	if list == "" {
		return nil, 0
	}
	texts := []string{}
	tagged := list == "ol" && (places[0].format != numberedListFormatNumbers || places[0].restart)
	for index, block := range blocks {
		if notionMarkdownListOf(block.Type) != list || w.visited[block.ID] ||
			list == "ol" && (places[index].format != places[0].format || index > 0 && places[index].restart) {
			break
		}
		text, written := notionPageMarkdownText(block, w.mentions)
		if !written {
			break
		}
		texts = append(texts, strings.ReplaceAll(text, "\n", "<br />"))
		// An empty item is its tag too (seen 2026-11-10: <li></li>); a line
		// break at an edge is no space there (seen 2026-11-14: "- <br />b").
		tagged = tagged || w.mentions.Version2 &&
			(notionMarkdownBlockColorSuffix(block.Content) != "" || notionMarkdownEdgeSpace(texts[len(texts)-1]) || text == "")
	}
	if !tagged || len(texts) == 0 {
		return nil, 0
	}
	lines = []string{prefix + "<" + list + ">"}
	if list == "ol" {
		lines[0] = prefix + notionMarkdownOrderedOpening(places[0])
	}
	for index, block := range blocks[:len(texts)] {
		if !w.admit(block) {
			continue
		}
		color := ""
		if notionMarkdownBlockColorSuffix(block.Content) != "" {
			color = ` color="` + notionMarkdownColorName(notionBlockColor(block.Content)) + `"`
		}
		if texts[index] == "" && len(w.children[block.ID]) == 0 {
			w.rendered = append(w.rendered, block.ID)
			lines = append(lines, w.marked(block.ID, block.Type, prefix, []string{prefix + "<li" + color + "></li>"})...)
			continue
		}
		lines = append(lines, w.marked(block.ID, block.Type, prefix, w.held(block, prefix, "<li"+color+">", texts[index], "</li>"))...)
	}
	return append(lines, prefix+"</"+list+">"), len(texts)
}

// notionMarkdownListOf names the list tag a list item stands in.
func notionMarkdownListOf(blockType string) string {
	switch blockType {
	case "bulleted_list_item", "bullet":
		return "ul"
	case "numbered_list_item", "number":
		return "ol"
	}
	return ""
}

// version2Unknown writes a block "v2" doesn't, as Notion names it: a link
// to a page is its "alias". As in "v1" it isn't among the IDs the read
// couldn't load (seen 2026-11-10).
func (w *notionMarkdownWriter) version2Unknown(block workspaceExportMarkdownBlock, prefix, id string) []string {
	kind := block.Type
	if kind == graphBlockTypeLinkToPage {
		kind = "alias"
	}
	w.unknownLine(block, id)
	line := fmt.Sprintf(`<unknown url="%s" alt="%s" />`, w.unknownAddress(id), kind)
	return w.under(block, prefix, line, "  ", false)
}

// notionMarkdownVersion2DatabaseMention writes a database's mention as "v2"
// does: type="database", and the name of one the reader opens; one in the
// trash by its address alone (seen 2026-11-13).
func notionMarkdownVersion2DatabaseMention(span graphTextSpan, mentions notionMarkdownMentions) (string, bool) {
	address := notionMarkdownAttributeEscaped.Replace
	target, readable := mentions.Database(span.DatabaseID)
	if readable && mentions.inTrash(span) {
		return `<mention url="` + address(target) + `" />`, true
	}
	// One without a name is its pair of tags with nothing between (seen
	// 2026-11-14).
	if readable && mentions.DatabaseTitle != nil {
		name := mentions.DatabaseTitle(span.DatabaseID)
		return `<mention url="` + address(target) + `" type="database">` + notionMarkdownEscaped.Replace(name) + `</mention>`, true
	}
	if !readable && mentions.Unnamed != nil {
		target, readable = mentions.Unnamed(span.Mention, span.DatabaseID)
	}
	return `<mention url="` + address(target) + `" type="database" />`, readable
}
