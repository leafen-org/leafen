package app

import (
	"regexp"
	"slices"
	"strings"
)

// The tags holding blocks that a public API write's "v1" Markdown opens and
// closes, as its reading follows them (notionMarkdownVersion1Input).

// notionMarkdownVersion1ContainerTags are "v1"'s tags that hold blocks.
var notionMarkdownVersion1ContainerTags = map[string]bool{
	"callout": true, "details": true, "columns": true, "column": true, "tabs": true, "tab": true, "synced_block": true,
}

// notionMarkdownVersion1OpeningTag is the tag holding blocks a line's text
// opens with, which "v1" ends at its first > (seen 2026-11-14:
// "<callout icon=\">\">" is the tag "<callout icon=\">" and text after it,
// "<callout icon=\"<\">a</callout>" the callout "a").
var notionMarkdownVersion1OpeningTag = regexp.MustCompile(`^<(callout|details|columns|column|tabs|tab|synced_block)(?:[ \t][^>]*)?>`)

// notionMarkdownVersion1Tag is a tag holding blocks that a "v1" write
// opened and hasn't closed: its name, the tabs its line stands at, and, for
// one alone on its line that holds its lines, how deep the lines stood
// before it. A tab is in its tabs or not (held), and has read its name or
// not (named).
type notionMarkdownVersion1Tag struct {
	name, written string
	depths        []int
	held, named   bool
	// text marks a tag written as text, whose closing tag is text too.
	text bool
	// item is, for a tag a list item holds, one more than how far in the
	// item stands (notionMarkdownVersion1ItemTag), and 0 for another;
	// least, for one no other tag holds, how far in it stands.
	item, least int
}

type notionMarkdownVersion1OpenTags []notionMarkdownVersion1Tag

// drops tells a line, standing lead in, that a tag open drops: one a list
// item holds drops the lines standing no further in than the item (seen
// 2026-11-14: "- <callout>⏎a⏎</callout>⏎z" is an item holding an empty
// callout, and "z"), one no tag holds the lines less far in than it (seen
// 2026-11-15: "\t<callout>⏎x⏎</callout>", "  <callout>⏎x⏎</callout>",
// "a⏎\t<callout>⏎x⏎</callout>", "\t<callout>⏎- x⏎</callout>" and
// "\t<callout>⏎x⏎\t</callout>" are an empty callout, "\t<callout>⏎\tx⏎</callout>"
// the callout "x"; a <details>'s, a synced block's and columns' lines alike),
// but for a <details>'s title (title: "\t<details>⏎<summary>s</summary>⏎x⏎</details>"
// is the toggle "s").
func (tags notionMarkdownVersion1OpenTags) drops(lead string, title bool) bool {
	width := notionMarkdownVersion1Width(lead)
	return slices.ContainsFunc(tags, func(tag notionMarkdownVersion1Tag) bool {
		return tag.item > 0 && width < tag.item || tag.least > 0 && width < tag.least && !title
	})
}

// open notes a tag a line starts that its line doesn't close, and tells a
// line that reads as no line: a tab's out of its tabs.
//
// A callout alone on its line reads its lines as "v1" reads lines at the
// top, by how far in each stands against the ones before it, so the first
// is its text and the others its blocks (seen 2026-11-13:
// "<callout>\na\nb\n</callout>", with its lines a tab in or not, or in a list
// item, is the callout "a" holding "b"; a line less far in than its first is
// one of its blocks). Columns, a column and tabs hold their lines so, wherever
// they stand — a column alone at the top, in a list item, a toggle or a
// callout, and a line in columns or tabs that no column or tab holds (seen
// 2026-11-13: "<column>\na\n</column>" is a column holding "a",
// "<columns>\na\n</columns>" columns holding "a", "<tabs>\na\n</tabs>" tabs
// holding the tab "a"). Leafen's own form writes their blocks a tab in, as
// its reader takes them (notionMarkdownFlow), and a tab's name and blocks
// two (notionMarkdownVersion1OpenTags.name).
func (tags *notionMarkdownVersion1OpenTags) open(rest, written string, depths *notionMarkdownVersion1Depths) bool {
	name, size := notionMarkdownVersion1OpenedTag(rest)
	if size != len(rest) {
		return false
	}
	tag := notionMarkdownVersion1Tag{name: name, written: written}
	switch name {
	case "tab":
		// Its name is the line after it (name), its blocks under it.
		tag.depths = slices.Clone(depths.open)
		tag.held = len(*tags) > 0 && (*tags)[len(*tags)-1].name == "tabs"
	case "callout", "columns", "column", "tabs":
		tag.depths = slices.Clone(depths.open)
		depths.open[len(depths.open)-1] = -1
	}
	*tags = append(*tags, tag)
	return name == "tab" && !tag.held
}

// notionMarkdownVersion1OpenedTag is the tag holding blocks a line's text
// opens with and how long it stands; none for one closed in itself.
func notionMarkdownVersion1OpenedTag(text string) (string, int) {
	match := notionMarkdownVersion1OpeningTag.FindStringSubmatch(text)
	if match == nil || strings.HasSuffix(match[0], "/>") {
		return "", -1
	}
	return match[1], len(match[0])
}

// notionMarkdownVersion1TagInText is a tag holding blocks, or a toggle's
// summary, where it stands in a line.
var notionMarkdownVersion1TagInText = regexp.MustCompile(`^</?(?:callout|details|summary|columns|column|tabs|tab|synced_block)(?:[ \t/>]|$)`)

// notionMarkdownVersion1TagsAsText is a line's rest as "v1" reads the tags
// holding blocks in it, and whether they are its text: a tag opens only
// alone at its line's start, and a callout on one line, what follows its
// closing tag gone; a tag that closes one (closes) stands at its line's start.
// Anywhere else such a tag is text, and opens or closes none (seen
// 2026-11-14: "<callout>a⏎b</callout>" is the paragraphs "<callout>a" and
// "b</callout>", "<callout>a⏎</callout>" "<callout>a" and "</callout>",
// "<callout>⏎a⏎b </callout>" the callout "a" holding "b </callout>", "x
// <callout>", "<details><summary>s</summary>…" and "<columns>a</columns>"
// text as written, "<callout>a</callout> b" the callout "a", in a list item
// too: "- x</callout>"), a <summary> in it too. A list item holding such a
// tag stands as before, and so does a toggle's summary line.
func notionMarkdownVersion1TagsAsText(rest string, closes bool) (string, bool) {
	trimmed := strings.TrimSpace(rest)
	if closes || !strings.Contains(trimmed, "<") {
		return rest, false
	}
	head, body := "", trimmed
	if _, width, item := notionMarkdownFlowItem(body); item && width <= len(body) {
		head, body = body[:width], body[width:]
		if box := notionMarkdownFlowTodo.FindString(body); box != "" {
			head, body = head+box, strings.TrimLeft(body[len(box):], " \t")
		}
	}
	name, size := notionMarkdownVersion1OpenedTag(body)
	if size == len(body) || strings.HasPrefix(body, "<summary") {
		return rest, false
	}
	if end := strings.Index(body[max(size, 0):], "</callout>"); name == "callout" && end >= 0 {
		return head + body[:size+end+len("</callout>")], false
	}
	return rest, true
}

// name reads a tab's first line, its name, when the last tag open is a tab
// that hasn't read one: "v1" reads it as text whatever it starts, and the
// lines after it as its blocks, nested by how far in they stand (seen
// 2026-11-13: "<tabs>\n<tab>\na\nb\n</tab>\n</tabs>" is the tab "a" holding
// "b", "# h", "- x", "> q", "---" and "<callout>" each a tab named so, "$$" a
// tab named an equation of nothing). A tab out of its tabs is that paragraph
// alone, its tags gone ("<tab>\na\nb\n</tab>" is "a" holding "b"). In its
// tabs it stands as Leafen's form writes one, its name and its blocks
// two tabs in from its tabs (notionMarkdownWriter.apiTabs).
//
// A tag its name opens is text, and so is its closing tag
// ("<tabs>\n<tab>\n<tab>\na\n</tab>\n</tab>\n</tabs>" is the tab "<tab>"
// holding "a" and "</tab>").
func (tags *notionMarkdownVersion1OpenTags) name(trimmed string, depths *notionMarkdownVersion1Depths) (string, bool) {
	open := *tags
	if len(open) == 0 || open[len(open)-1].name != "tab" || open[len(open)-1].named || open[len(open)-1].depths == nil {
		return "", false
	}
	tab := &open[len(open)-1]
	tab.named = true
	depth := len(tab.depths) - 1
	depths.open = append(slices.Clone(tab.depths[:depth]), -1)
	if tab.held {
		depth++
	}
	if match := notionMarkdownVersion1OpeningTag.FindStringSubmatch(trimmed); match != nil &&
		!strings.HasSuffix(trimmed, "/>") && !strings.Contains(trimmed, "</"+match[1]+">") {
		*tags = append(*tags, notionMarkdownVersion1Tag{name: match[1], text: true})
	}
	// Its color mark is its paragraph's (notionMarkdownVersion1TakeColor).
	named, mark := notionMarkdownVersion1TakeColor(trimmed)
	return notionMarkdownVersion1Text(strings.Repeat("\t", depth)+notionMarkdownVersion1LineEnd(notionMarkdownVersion1TabName(named))) + mark, true
}

// notionMarkdownVersion1TabName is a tab's name line written as its text:
// what would start a block or a tag escaped, its backticks and $ as "v1"
// reads them in text ("```" is "`", "$$" an equation of nothing, written as
// "v1" writes one).
func notionMarkdownVersion1TabName(trimmed string) string {
	if strings.EqualFold(strings.ReplaceAll(trimmed, " ", ""), "<empty-block/>") {
		return trimmed
	}
	for strings.HasPrefix(trimmed, "```") {
		trimmed = trimmed[2:]
	}
	if trimmed == "$$" {
		return "$``$"
	}
	if strings.HasPrefix(trimmed, "<") {
		return `\` + trimmed
	}
	return notionMarkdownFlowLiteral(trimmed)
}

// closing is the line a tag's closing tag stands as: as its opening tag, a
// tab's out of its tabs none, or its name of nothing when it read none (seen
// 2026-11-13: "<tab>\n</tab>" is an empty paragraph), and none for a tag
// written as text, whose closing tag reads as text where it stands.
func (tag notionMarkdownVersion1Tag) closing(trimmed string) string {
	if tag.text {
		return notionMarkdownVersion1Dropped
	}
	if tag.name != "tab" || tag.held {
		return tag.written + trimmed
	}
	if !tag.named {
		return tag.written + "<empty-block/>"
	}
	return notionMarkdownVersion1Dropped
}

// holds tells a tag open by that name.
func (tags notionMarkdownVersion1OpenTags) holds(name string) bool {
	return slices.ContainsFunc(tags, func(tag notionMarkdownVersion1Tag) bool { return tag.name == name })
}

// close closes the last tag of that name open and those opened after it,
// giving it; the closing tag of one that holds its lines stands as its
// opening one, its lines' depths as before it, and another tag's line reads
// as any other.
func (tags *notionMarkdownVersion1OpenTags) close(name string) (notionMarkdownVersion1Tag, bool) {
	for at := len(*tags) - 1; at >= 0; at-- {
		if tag := (*tags)[at]; tag.name == name {
			*tags = (*tags)[:at]
			return tag, true
		}
	}
	return notionMarkdownVersion1Tag{}, false
}

// notionMarkdownVersion1DetailsTitle is the line of a toggle's title for the
// <details> at lines[index], or -1 for none: "v1" takes the first line in it
// that is a <summary> whole, <summary>…</summary>, wherever it stands
// before its </details>, in a toggle it holds too, and reads its other
// lines as its blocks, a <summary> among them its words (seen 2026-11-14:
// "<details>⏎x⏎<summary>a</summary>⏎</details>" is the toggle "a" holding
// "x", "<details>⏎<summary>a⏎b</summary>⏎</details>" an untitled toggle
// holding "<summary>a" and "b</summary>", "<details>⏎<summary>a</summary>
// b⏎</details>" one holding "<summary>a</summary>b", "<details>⏎<summary>
// a⏎b</summary>⏎<details>⏎<summary>c</summary>⏎d⏎</details>⏎</details>" the
// toggle "c" holding them and an untitled toggle holding "d").
func notionMarkdownVersion1DetailsTitle(lines []string, index int) int {
	depth, fence := 0, ""
	for at := index + 1; at < len(lines); at++ {
		trimmed := strings.TrimSpace(lines[at])
		switch {
		case fence != "":
			if notionMarkdownFenceCloses(trimmed, fence, false) {
				fence = ""
			}
		case strings.HasPrefix(trimmed, "```"):
			fence = notionMarkdownFenceOf(trimmed)
		case trimmed == "</details>" && depth == 0:
			return -1
		case trimmed == "</details>":
			depth--
		case notionMarkdownSummaryPattern.MatchString(trimmed):
			return at
		default:
			if name, size := notionMarkdownVersion1OpenedTag(trimmed); name == "details" && size == len(trimmed) {
				depth++
			}
		}
	}
	return -1
}

// notionMarkdownVersion1Titled puts the title notionMarkdownVersion1DetailsTitle
// finds for the <details> at lines[index], read as written there, on the line
// after it, as Leafen reads a toggle's title, or writes an empty one there for
// none; it tells whether the next line is a title.
func notionMarkdownVersion1Titled(lines []string, index int) bool {
	title := notionMarkdownVersion1DetailsTitle(lines, index)
	if title < 0 {
		last := lines[index][strings.LastIndexByte(lines[index], '\n')+1:]
		lines[index] += "\n" + last[:len(last)-len(strings.TrimLeft(last, "\t"))] + "<summary></summary>"
		return false
	}
	line := lines[title]
	copy(lines[index+2:title+1], lines[index+1:title])
	lines[index+1] = line
	return true
}

// notionMarkdownVersion1TitleText is a toggle's title line with the
// </summary> tags before its last as its words (seen 2026-11-14:
// "<summary>a</summary>b</summary>" is the title "a</summary>b").
func notionMarkdownVersion1TitleText(line string) string {
	last := strings.LastIndex(line, "</summary>")
	var out strings.Builder
	for at := 0; at < last; {
		found := strings.Index(line[at:last], "</summary>")
		if found < 0 {
			out.WriteString(line[at:last])
			break
		}
		out.WriteString(line[at : at+found])
		if !notionMarkdownEscapedAt(line, at+found) {
			out.WriteByte('\\')
		}
		out.WriteString("</summary>")
		at += found + len("</summary>")
	}
	if last < 0 {
		return line
	}
	return out.String() + line[last:]
}

// notionMarkdownVersion1ClosedReference is a line's rest that is a sub-page's
// or a database's tag closed in itself naming its url, as "v1" reads it: the
// tag opened and closed at once, untitled (seen 2026-11-15: "<page
// url=\"https://example.com\"/>", a tab in too, is refused as "<page
// url=\"https://example.com\">x</page>" is, notionMarkdownElsewhereError).
func notionMarkdownVersion1ClosedReference(rest string) (string, bool) {
	match := notionMarkdownVersion1ClosedReferenceTag.FindStringSubmatch(strings.TrimSpace(rest))
	if match == nil || !strings.Contains(match[2], ` url="`) {
		return rest, false
	}
	return "<" + match[1] + match[2] + "></" + match[1] + ">", true
}

var notionMarkdownVersion1ClosedReferenceTag = regexp.MustCompile(`^<(page|database)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*/>$`)
