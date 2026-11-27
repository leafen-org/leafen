package app

import (
	"regexp"
	"slices"
	"strings"
)

// The block tags a "v2" write's Markdown opens and closes, as Notion reads
// them to tell what it refuses (notionMarkdownVersion2Input): the tags left
// open at its end, the items no list, columns or tabs hold, and what
// columns hold besides columns.

// notionMarkdownBareItemSaid is what Notion says of an item no tag of its
// kind holds.
var notionMarkdownBareItemSaid = map[string]string{
	"li":     "<li> only goes inside <ul> or <ol>, as in <ul><li>Item</li></ul>.",
	"column": "<column> only goes inside <columns>.",
	"tab":    "<tab> only goes inside <tabs>.",
}

// notionMarkdownGroupItems are the tags holding items and their items' tags.
var notionMarkdownGroupItems = map[string]string{"ul": "li", "ol": "li", "columns": "column", "tabs": "tab"}

// stands records a block tag standing in text; one closed in itself, or one
// that holds nothing, closes nothing.
func (repair *notionMarkdownRepair) stands(name string, closing, closedInSelf bool) {
	if repair.sealed {
		return
	}
	if name != "empty-block" && name != "table-of-contents" && name != "col" {
		repair.inline = append(repair.inline, notionMarkdownInlineBlockTag{name: name, closing: closing, closedInSelf: closedInSelf,
			at: len(repair.missing)})
	}
}

// standsInText records a block tag standing in text (stands).
func (repair *notionMarkdownRepair) standsInText(name string, closing, closedInSelf bool) {
	repair.stands(name, closing, closedInSelf)
	if at := len(repair.inline) - 1; at >= 0 && !repair.sealed && repair.inline[at].name == name {
		repair.inline[at].text = true
	}
}

// openInText tells a block tag of the name left open last that stood in
// text.
func (repair *notionMarkdownRepair) openInText(name string) bool {
	stack := []notionMarkdownInlineBlockTag{}
	for _, tag := range repair.inline {
		switch {
		case tag.closedInSelf:
		case !tag.closing:
			stack = append(stack, tag)
		default:
			for at := len(stack) - 1; at >= 0; at-- {
				if stack[at].name == tag.name {
					stack = slices.Delete(stack, at, at+1)
					break
				}
			}
		}
	}
	for at := len(stack) - 1; at >= 0; at-- {
		if stack[at].name == name {
			return stack[at].text
		}
	}
	return false
}

// block records a block tag the reading meets, standing in text or on its
// line; opens is false for one Notion refuses. A list's item no <ul> or
// <ol> holds is refused (seen 2026-11-10), each, and so is a column no
// <columns> holds and a tab no <tabs> holds (seen 2026-11-13).
func (repair *notionMarkdownRepair) block(name string, closing, closedInSelf, opens bool) {
	if repair.sealed {
		return
	}
	open, tags := repair.openTags(), len(repair.inline)
	repair.stands(name, closing, closedInSelf)
	// What it says of itself, it says before what it holds.
	defer func() {
		if len(repair.inline) > tags {
			repair.inline[tags].at = len(repair.missing)
		}
	}()
	switch {
	case (name == "columns" || name == "tabs") && closing:
		delete(repair.unheld, name)
	case (name == "column" || name == "tab") && !closing && !closedInSelf &&
		(!slices.Contains(open, name+"s") || repair.unheld[name+"s"] || repair.openInText(name+"s")):
		repair.missing = append(repair.missing, notionMarkdownBareItemSaid[name])
		if len(repair.inline) > tags && name == "column" {
			repair.inline[tags].bare = true
		}
	case (name == "tr" || name == "td" || name == "th") && !closing && !closedInSelf &&
		(!slices.Contains(open, "table") || repair.openInText("table")):
		// A table's row or cell no table holds is refused as bare, a
		// <table> standing in text holding none (seen 2026-11-14:
		// "<tr>⏎<td>a</td>⏎</tr>" and "x <table>⏎<tr>⏎<td>a</td>⏎</tr>⏎
		// </table>" say both, "<td>a</td>" its own).
		repair.missing = append(repair.missing, notionMarkdownTablePartAlone(name))
	case (name == "ul" || name == "ol") && closing && len(repair.lists) > 0:
		repair.lists = repair.lists[:len(repair.lists)-1]
	case (name == "ul" || name == "ol") && !closing && !closedInSelf:
		// A list holding more than items holds none (notionMarkdownVersion2ListChildren).
		repair.lists = append(repair.lists, opens && !repair.listRefused)
		repair.listRefused = false
	case name == "li" && !closing:
		// The list it stands in last holds it (seen 2026-11-14: an item of a
		// list refused in another's item is bare).
		if len(repair.lists) == 0 || !repair.lists[len(repair.lists)-1] {
			repair.missing = append(repair.missing, notionMarkdownBareItemSaid[name])
		}
	}
}

// trailing is what Notion says of the block tags standing in text that no
// closing tag after them closes, in the Markdown as a whole (seen
// 2026-11-10: "x <ul> y\n\nz </ul> w" closes its <ul>): it closes them at
// the Markdown's end, and refuses it; the flow tags it says on their own
// (notion_page_markdown_version2_flow_tags.go), and those a block's closing
// tag closed over in that block's reading (notionMarkdownVersion2FramedTrailing).
func (repair *notionMarkdownRepair) trailing() string {
	// Those a block's own reading says are said there (notionMarkdownVersion2FramedTrailing).
	_, framed := notionMarkdownVersion2FramedTrailing(repair.inline)
	open := []string{}
	for _, one := range notionMarkdownBlockTagsOpen(repair.inline) {
		if (!one.flow || one.first) && !framed[one.event] {
			open = append(open, one.name)
		}
	}
	if len(open) == 0 {
		return ""
	}
	return notionMarkdownVersion2TrailingSaid + "<" + strings.Join(open, ">, <") + ">."
}

// openTags are the block tags met so far that no closing tag closed or
// closed over, in order (notionMarkdownBlockTagsLeft).
func (repair *notionMarkdownRepair) openTags() []string {
	open, _, _ := notionMarkdownBlockTagsLeft(repair.inline)
	return open
}

// stray is a line that is only a block's closing tags that no tag before
// them opened, or one standing in text opened (seen 2026-11-14: "- [ ]
// <callout>⏎  a⏎  </callout>" is the to-do "<callout>⏎a⏎</callout>", "x
// <toggle>⏎</toggle>" the paragraph "x <toggle>⏎</toggle>", which the
// closing tag closes, "x <callout>⏎⏎</callout>" two paragraphs), as Notion's
// "v2" reads it (seen 2026-11-11, 2026-11-14):
// text, going on with the text before it as another line or standing as a
// paragraph — "1. one⏎</ol>" is the item "one⏎</ol>", "x⏎</details>" the
// paragraph "x⏎</details>", "x⏎</details >", "x⏎</details></details>",
// "x⏎</empty-block>" and "x⏎</col>" alike — v1's <synced_block> and
// <synced_block_reference> named as the tags they become ("x⏎
// </synced-block>") and said so, a </summary> said as a tag neither reads,
// and v1's </table_of_contents> gone, a blank line left ("x⏎
// </table_of_contents>⏎y" is two paragraphs). A tag that holds nothing, an
// <unknown>, and v1's are never open (stands, repair.text): their closing
// tag is stray where the lines before open none. HTML's are put right
// (repair.tag).
func (repair *notionMarkdownRepair) stray(trimmed string, before []string) (string, bool) {
	if !notionMarkdownVersion2ClosingLine.MatchString(trimmed) {
		return "", false
	}
	found := notionMarkdownVersion2ClosingTag.FindAllStringSubmatchIndex(trimmed, -1)
	for _, tag := range found {
		if !repair.strayName(trimmed[tag[2]:tag[3]], before) {
			return "", false
		}
	}
	var out strings.Builder
	at := 0
	for _, tag := range found {
		out.WriteString(trimmed[at:tag[0]])
		at = tag[1]
		name := trimmed[tag[2]:tag[3]]
		mark := `\`
		if repair.openInText(name) {
			repair.standsInText(name, true, false)
			// A list's tag so is said among those it stands with (repair.line).
			if name == "ul" || name == "ol" || name == "li" || name == "todo" {
				mark = notionMarkdownListTagMark
			}
		}
		switch written := notionMarkdownLegacyTags[name]; {
		case written != "":
			repair.say("deprecated_tag", name, notionMarkdownOlderTagSaid(name, notionMarkdownVersion1Tags[name]))
			if written != "table-of-contents" {
				out.WriteString(`\</` + written + ">")
			}
			continue
		case name == "summary":
			repair.say("unsupported_tag", name, notionMarkdownUnsupportedSaid(name))
		}
		out.WriteString(mark + trimmed[tag[0]:tag[1]])
	}
	return strings.TrimSpace(out.String()), true
}

// strayName tells a block's closing tag of the name that no tag before it
// opened.
func (repair *notionMarkdownRepair) strayName(name string, before []string) bool {
	written := name
	if converted := notionMarkdownLegacyTags[name]; converted != "" {
		written = converted
	}
	switch {
	case notionMarkdownVersion2InlineTags[written] || !notionMarkdownVersion2Tags[written] && name != "details" && name != "summary":
		return false
	case written != name || notionMarkdownVersion2NeverOpen[name]:
		return !notionMarkdownVersion2OpensBefore(before, name, written)
	}
	// An item a list made no list closed over goes on to its own closing tag
	// (repair.itemOverByTextList).
	return !slices.Contains(repair.openTags(), name) && !(name == "li" && repair.itemOverByTextList()) || repair.openInText(name)
}

// closesNone tells a block's closing tag, standing in a line its tags are
// read in, that closes no tag of its name open, which Notion's "v2" reads as
// its words where it stands (seen 2026-11-15: "<p>y </li></p>" is the
// paragraph "y </li>", "<p>y </li> z</p>", "<h2>y </li></h2>", "<quote>y
// </li></quote>", "<callout>y </li></callout>", "<p><b>y </li></b></p>",
// "<p>y </ul></p>" and "<p>y </td></p>" alike; an item a list made no list
// closed over is still open, itemOverByTextList).
func (repair *notionMarkdownRepair) closesNone(name string) bool {
	return notionMarkdownVersion2Tags[name] && !notionMarkdownVersion2InlineTags[name] && !notionMarkdownVersion2NeverOpen[name] &&
		!slices.Contains(repair.openTags(), name) && !(name == "li" && repair.itemOverByTextList())
}

// notionMarkdownVersion2NeverOpen are the block tags no reading of a line
// holds open.
var notionMarkdownVersion2NeverOpen = map[string]bool{"empty-block": true, "table-of-contents": true, "col": true, "unknown": true}

// notionMarkdownVersion2OpensBefore tells lines that open a tag of one of
// the names that they don't close, outside code, one standing in text, after
// words or a to-do's box, opening none (stray).
func notionMarkdownVersion2OpensBefore(lines []string, names ...string) bool {
	open, fence := 0, ""
	for _, line := range lines {
		if trimmed := strings.TrimLeft(line, " \t>"); fence != "" || notionMarkdownVersion2Fence(trimmed) {
			if fence == "" {
				fence = notionMarkdownFenceOf(trimmed)
			} else if inside := strings.TrimSpace(trimmed); strings.HasPrefix(inside, fence) && strings.Trim(inside, "`") == "" {
				fence = ""
			}
			continue
		}
		codes, marks := notionMarkdownInlineCodeRanges(line), len(notionMarkdownVersion2MarkerRun.FindString(line))
		for _, found := range notionMarkdownRepairTag.FindAllStringSubmatchIndex(line, -1) {
			inCode := slices.ContainsFunc(codes, func(code [2]int) bool { return code[0] <= found[0] && found[0] < code[1] })
			switch name := line[found[4]:found[5]]; {
			case inCode || notionMarkdownEscapedAt(line, found[0]) || !slices.Contains(names, name) || found[9] > found[8]:
			case found[3] > found[2]:
				open = max(open-1, 0)
			case found[0] < marks || strings.TrimSpace(line[marks:found[0]]) == "":
				open++
			}
		}
	}
	return open > 0
}

var (
	notionMarkdownVersion2ClosingLine = regexp.MustCompile(`^(?:</[a-z][a-z0-9_-]*[ \t]*>[ \t]*)+$`)
	notionMarkdownVersion2ClosingTag  = regexp.MustCompile(`</([a-z][a-z0-9_-]*)[ \t]*>`)
)

// seal notes an equation's $$ alone on its line that nothing closes: in
// block tags left open, Notion reads it to the end as it does code nothing
// closes, the tags after it none of its, so those left open stay so (seen
// 2026-11-13: "<callout>\na\n$$\nb\n</callout>" is refused for its
// <callout>, a blank line or a $$ after the tag alike); in none it reads no
// differently ("p\n$$\nb\n<callout>\nc\n</callout>" is the callout "c",
// "p\n$$\n<li>x</li>" a bare <li>). "- $$" and "> $$" are text.
func (repair *notionMarkdownRepair) seal(index int) {
	if !repair.sealed && len(repair.openTags()) > 0 {
		repair.sealed, repair.swallowed = true, index
	}
}

// bareItems is what Notion says, after the tags left open, when an equation
// or code that nothing closes leaves them so (swallowed) and the first is a
// list, columns or tabs whose closing tag it took: each item that holds is
// bare, closed or not (seen 2026-11-13: "<columns>\n<column>\na\n</column>
// \n<column>\n$$\nb\n</column>\n</columns>" says two bare <column>s,
// "<columns>\n<column>\n$$\nb" none), and those in the code or equation
// too (seen 2026-11-14: "<columns>⏎<column>⏎```⏎a⏎</column>⏎<column>⏎b⏎
// </column>⏎</columns>" and "<columns>⏎<column>⏎$$⏎b⏎</column>⏎<column>⏎c⏎
// </column>⏎</columns>" say two, "<ul>⏎<li>⏎- ```⏎  a⏎  ```⏎</li>⏎<li>b</li>⏎
// </ul>" two bare <li>s); in a callout they are not.
func (repair *notionMarkdownRepair) bareItems(swallowed bool, lines []string) []string {
	if !repair.sealed && !swallowed {
		return nil
	}
	type opened struct {
		name string
		at   int
	}
	shut := func(stack []opened, name string) []opened {
		for at := len(stack) - 1; at >= 0; at-- {
			if stack[at].name == name {
				return stack[:at]
			}
		}
		return stack
	}
	stack := []opened{}
	for at, tag := range repair.inline {
		switch {
		case tag.closedInSelf:
		case !tag.closing:
			stack = append(stack, opened{tag.name, at})
		default:
			stack = shut(stack, tag.name)
		}
	}
	if len(stack) == 0 || notionMarkdownGroupItems[stack[0].name] == "" ||
		!slices.ContainsFunc(lines, func(line string) bool { return strings.TrimSpace(line) == "</"+stack[0].name+">" }) {
		return nil
	}
	item, said, held := notionMarkdownGroupItems[stack[0].name], []string{}, []opened{}
	for _, tag := range repair.inline[stack[0].at+1:] {
		switch {
		case tag.closedInSelf:
		case !tag.closing:
			if len(held) == 0 && tag.name == item {
				said = append(said, notionMarkdownBareItemSaid[item])
			}
			held = append(held, opened{name: tag.name})
		default:
			held = shut(held, tag.name)
		}
	}
	tags := regexp.MustCompile(`<(/?)` + item + `(?:[ \t]+[^<>]*)?>`)
	for _, line := range lines[min(repair.swallowed+1, len(lines)):] {
		for _, tag := range tags.FindAllStringSubmatch(line, -1) {
			switch {
			case strings.HasSuffix(tag[0], "/>"):
			case tag[1] == "":
				if len(held) == 0 {
					said = append(said, notionMarkdownBareItemSaid[item])
				}
				held = append(held, opened{name: item})
			default:
				held = shut(held, item)
			}
		}
	}
	return said
}

// columnsChild refuses a line standing in <columns> that is no column's
// tag (seen 2026-11-13: "<columns>\na\n</columns>"), and one standing in
// <tabs> that is no tab's (seen 2026-11-14: "<tabs>\n# h\n</tabs>" and
// "<tabs>\na\n</tabs>"), once each.
func (repair *notionMarkdownRepair) columnsChild(trimmed string) {
	// A group whose children are refused says what its lines say read
	// again (notionMarkdownVersion2Input).
	if said, ok := repair.groups.lines(trimmed); ok {
		repair.missing = append(repair.missing, said...)
		return
	}
	if read, ok := repair.groups.heldRead(trimmed); ok {
		repair.heldLines(read)
		return
	}
	open := repair.openTags()
	if len(open) == 0 {
		return
	}
	item := notionMarkdownGroupItems[open[len(open)-1]]
	// <columns> or <tabs> standing in text hold nothing (seen 2026-11-14:
	// "x <columns>⏎</columns>" is the paragraph of its lines).
	if (item != "column" && item != "tab") || strings.HasPrefix(trimmed, "<"+item) || strings.HasPrefix(trimmed, "</"+item) ||
		repair.openInText(item+"s") {
		return
	}
	said := notionMarkdownHoldsOnly(item+"s", "<"+item+">")
	if !slices.Contains(repair.missing, said) {
		repair.missing = append(repair.missing, said)
	}
}

// A block tag closed in itself alone on its line, in a list item or a quote
// too, is refused when its block has to hold something, and an item's tag
// no tag of its kind holds is refused as bare (seen 2026-11-14: "<h3
// color=\"red\" />", "- <h3 />", "> <h3 />", "1. <toggle />" and "<callout>⏎
// <h3 />⏎</callout>" are refused, its attributes whatever they are; "<p />"
// is an empty paragraph, "<h3 /> z", "<h3 /><h2 />", "- [ ] <quote />",
// "<H3 />" and "<h5 />" text, and <page />, <database />, <equation />,
// <image /> and the like blocks). A column or a tab closed in itself in its
// columns or tabs is refused as both, and each column or tab after it there
// as bare ("<tabs>⏎<tab />⏎<tab>…" says all three).
var (
	notionMarkdownVersion2SelfClosed = regexp.MustCompile(`^<([a-z0-9-]+)(?:[ \t]+[^\s=<>/]+="[^"]*")*[ \t]*/>$`)
	// notionMarkdownVersion2HoldingTags are the tags whose blocks hold
	// something.
	notionMarkdownVersion2HoldingTags = map[string]bool{
		"h1": true, "h2": true, "h3": true, "h4": true, "quote": true, "toggle": true, "callout": true, "details": true,
		"ul": true, "ol": true, "todo": true, "columns": true, "tabs": true, "table": true,
	}
	// notionMarkdownVersion2HeldItems are the items' tags with the tag that
	// holds them.
	notionMarkdownVersion2HeldItems = map[string]string{"column": "columns", "tab": "tabs", "tr": "table", "td": "table"}
)

// selfClosed says what Notion says of a line's text, past its list items'
// and quotes' marks, that is a block tag closed in itself it refuses.
func (repair *notionMarkdownRepair) selfClosed(body string) {
	match := notionMarkdownVersion2SelfClosed.FindStringSubmatch(body)
	switch {
	case match == nil:
	case notionMarkdownVersion2HoldingTags[match[1]]:
		repair.missing = append(repair.missing, notionMarkdownNotSelfClosing(match[1]))
	case notionMarkdownVersion2HeldItems[match[1]] != "":
		group := notionMarkdownVersion2HeldItems[match[1]]
		if slices.Contains(repair.openTags(), group) {
			if group == "table" {
				return
			}
			repair.missing = append(repair.missing, notionMarkdownNotSelfClosing(match[1]))
			if repair.unheld == nil {
				repair.unheld = map[string]bool{}
			}
			repair.unheld[group] = true
		}
		said := notionMarkdownBareItemSaid[match[1]]
		if said == "" {
			said = notionMarkdownTablePartAlone(match[1])
		}
		repair.missing = append(repair.missing, said)
	}
}
