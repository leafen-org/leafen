package app

import (
	"regexp"
	"strings"
)

// A "v2" write's v1 <table_of_contents> is the <table-of-contents /> Notion's
// "v2" puts it as before it reads the line, its closing tag gone (seen
// 2026-11-14): "<table_of_contents></table_of_contents>",
// "<table_of_contents>⏎</table_of_contents>" and "<table_of_contents>" alone
// are a table of contents, in a list item, a quote, a callout, a toggle or a
// column too, its color kept ("x⏎<table_of_contents>⏎</table_of_contents>⏎y"
// stands between the paragraphs "x" and "y", "<table_of_contents>⏎x⏎
// </table_of_contents>" before the paragraph "x"); where text stands with it
// on its line it is that text, as written so ("x <table_of_contents> y" is
// "x <table-of-contents /> y", "<table_of_contents>x</table_of_contents>"
// "<table-of-contents />x"). Each is said as v1's, where it stands.

var notionMarkdownVersion1ContentsTag = regexp.MustCompile(`<(/?)table_of_contents((?:[ \t]+[^<>]*?)?)[ \t]*/?>`)

// notionMarkdownVersion1ContentsLine is a line with its v1 <table_of_contents>
// tags put as Notion puts them, their attributes as written.
func notionMarkdownVersion1ContentsLine(line string) string {
	if !strings.Contains(line, "table_of_contents") {
		return line
	}
	var out strings.Builder
	at := 0
	for _, found := range notionMarkdownVersion1ContentsTag.FindAllStringSubmatchIndex(line, -1) {
		if notionMarkdownEscapedAt(line, found[0]) {
			continue
		}
		out.WriteString(line[at:found[0]])
		if found[3] == found[2] {
			out.WriteString("<table-of-contents" + line[found[4]:found[5]] + " />")
		}
		at = found[1]
	}
	return out.String() + line[at:]
}

// contents is a v1 <table_of_contents> tag of the attributes written put as
// Notion puts it, said as v1's: its closing tag nothing, its opening one a
// <table-of-contents /> of the attributes it keeps, as text unless the
// line's tags are all it holds (structured).
func (repair *notionMarkdownRepair) contents(attributes string, closing, structured bool) string {
	repair.say("deprecated_tag", "table_of_contents", notionMarkdownOlderTagSaid("table_of_contents", notionMarkdownVersion1Tags["table_of_contents"]))
	if closing {
		return ""
	}
	kept, _ := repair.attributes("table_of_contents", attributes)
	tag := "<table-of-contents" + kept + " />"
	if !structured {
		return `\` + tag
	}
	return tag
}
