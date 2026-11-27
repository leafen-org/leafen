package app

import (
	"regexp"
	"strings"
)

// A public API write's "v1" Markdown, the default, reads as Notion's "v1"
// reads it (seen 2026-11-10, each written with replace_content and read back
// as blocks): a code fence's language reads as Notion's
// (notionMarkdownFenceLanguage), and the tags
// only "v2" and the MCP's Markdown know are text as written, wherever they
// stand — <p>, <h1>…<h6>, <quote>, <toggle>, <ul>, <ol>, <li>, <todo>,
// <equation>, <table-of-contents>, <synced-block>, <bookmark> (save a line
// of one with its caption between its tags, as an <embed>'s, which closed in
// itself is text too), <breadcrumb>, <button>, <link-to-page>, <b>,
// <strong>, <i>, <em>, <s>, <del>, <u>, <code>, <mention> and <date>, and a
// <link-preview> save one closed in itself with an address. "v1"'s own tags
// — <details>, <callout>, <columns>, <tabs>, <table>, <table_of_contents/>,
// <synced_block>, <span>, <mention-…>, <br> and the rest its writer writes
// (notion_page_markdown_api_form.go) — read as they do, their values in
// single quotes or none as in double ones (notion_page_markdown_version1_quotes.go). An address between
// angle brackets, <https://…>, is its brackets as text around the address,
// linked as any bare address is (notionMarkdownBareLinks). Code between
// backticks is as written, and so are the lines of fenced code and of an
// equation, without the whitespace they all start with
// (notionMarkdownVersion1Verbatim). Lines nest by how far in they stand
// (notionMarkdownVersion1Depths), and each line of > is a quote, its text
// as written. A list item or a to-do whose text starts another block — a
// list item, a quote, a heading, code, an equation, dashes or a table's row
// — holds it, its own text empty, and the lines further in stand under that
// block ("- - b", "- --"), a table aside (notionMarkdownVersion1PipeTable).

var notionMarkdownVersion2OnlyTag = regexp.MustCompile(`^</?(?:p|h[1-6]|quote|toggle|ul|ol|li|todo|equation|table-of-contents|synced-block-reference|synced-block|bookmark|breadcrumb|button|link-to-page|b|strong|i|em|s|del|u|code|mention|date)(?:[ \t/>]|$)`)

// notionMarkdownVersion1Reading is "v1" Markdown as Leafen's page Markdown
// reader takes it; own for Leafen's own (notionMarkdownVersion1OwnInput).
func notionMarkdownVersion1Reading(markdown string, own bool) string {
	// Its tables are read first, and a table's tags anywhere else are text
	// (notionMarkdownVersion1TagTables).
	lines, tabled := notionMarkdownVersion1TagTables(notionMarkdownVersion1BlockQuotes(strings.Split(markdown, "\n")))
	verbatim := notionMarkdownVersion1Verbatim{}
	depths := notionMarkdownVersion1Depths{}
	tags := notionMarkdownVersion1OpenTags{}
	// Each line of > is a quote of its own in "v1", where "v2" joins them
	// (seen 2026-11-10): a blank line keeps it from the line after it.
	quoted := false
	// The line of the toggle opened last when the line before it, which its
	// summary's color goes to (notionMarkdownVersion1SummaryColored).
	details := -1
	for index, line := range lines {
		if line == notionMarkdownVersion1Dropped {
			continue
		}
		trimmed := strings.TrimSpace(line)
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		closing := notionMarkdownVersion1ClosingTag.FindStringSubmatch(trimmed)
		// An equation or code no $$ or fence closes ends where the tag
		// holding it closes (seen 2026-11-13: "<callout>\na\n$$\nb\n</callout>"
		// holds the equation "b", "<details>…\n```\na\n</details>" the code
		// "a"); a tag "v1" reads as text holds neither, nor one opened in it
		// ("<toggle>\nt\n$$\nb\n</toggle>" is the equation "b\n</toggle>").
		if verbatim.open() && closing != nil && tags.holds(closing[1]) {
			verbatim.end(lines, index)
			verbatim = notionMarkdownVersion1Verbatim{}
		}
		if verbatim.open() {
			verbatim.take(lines, index)
			continue
		}
		if trimmed == "" {
			quoted = false
			continue
		}
		stray := false
		if closing != nil && notionMarkdownVersion1ContainerTags[closing[1]] {
			tag, closed := tags.close(closing[1])
			if closed && tag.depths != nil {
				depths.open = tag.depths
				// A line further in after a callout's closing tag, or one an
				// item holds, stands beside it (seen 2026-11-14:
				// "<callout>⏎a⏎</callout>⏎  b" is the callout "a" and "b",
				// "- <callout>⏎  a⏎  </callout>⏎  b" an item holding both).
				if tag.name == "callout" || tag.item > 0 {
					depths.open = tag.depths[:len(tag.depths)-1]
				}
				lines[index], quoted = tag.closing(closing[0]), false
				continue
			}
			if closed && tag.item > 0 {
				lines[index], quoted = tag.written+closing[0], false
				continue
			}
			stray = !closed || tag.text
			// What follows a closing tag on its line goes (seen 2026-11-14:
			// "<details>…\n</details> x", "</callout>b").
			if !stray {
				line, trimmed = lead+closing[0], closing[0]
			}
		}
		// The lines in a tag standing less far in than it go (notionMarkdownVersion1OpenTags.drops).
		if tags.drops(lead, details >= 0 && strings.HasPrefix(trimmed, "<summary")) {
			lines[index] = notionMarkdownVersion1Dropped
			continue
		}
		if name, named := tags.name(trimmed, &depths); named {
			lines[index], quoted = name, false
			continue
		}
		// A <summary> other than its toggle's title is its words
		// (notionMarkdownVersion1DetailsTitle).
		summaryText := strings.HasPrefix(trimmed, "<summary") && details < 0
		if details >= 0 && strings.HasPrefix(trimmed, "<summary") {
			line, lines[details] = notionMarkdownVersion1SummaryColored(line[len(lead):], lines[details])
			line = lead + notionMarkdownVersion1TitleText(line)
		}
		details = -1
		// Written a tab a level, as Leafen writes its own Markdown, which its
		// reader takes at its depth as it is (notionMarkdownFlow).
		written := strings.Repeat("\t", depths.of(lead))
		apart := ""
		if quoted && !strings.HasPrefix(trimmed, "```") {
			apart = "\n"
		}
		quoted = strings.HasPrefix(trimmed, ">")
		items, rest := notionMarkdownVersion1Items(line[len(lead):])
		rest = notionMarkdownVersion1ItemText(rest)
		for range items {
			depths.open = append(depths.open, depths.open[len(depths.open)-1])
		}
		// A block an item holds on one line holds no blocks, so a line further
		// in after it stands beside it (seen 2026-11-14: "- <callout>a</callout>⏎
		// b" is an item holding the callout "a" and "b").
		if trimmed := strings.TrimSpace(rest); len(items) > 0 && notionMarkdownVersion1ItemTag(trimmed) {
			if _, size := notionMarkdownVersion1OpenedTag(trimmed); size != len(trimmed) {
				depths.open = depths.open[:len(depths.open)-1]
			}
		}
		// The items take the color of the block they hold
		// (notionMarkdownVersion1Colored), a table's aside.
		itemMark := ""
		if len(items) > 0 && !strings.HasPrefix(rest, "|") {
			_, itemMark = notionMarkdownVersion1TakeColor(rest)
		}
		for _, item := range items {
			apart += written + item + itemMark + "\n"
			written += "\t"
		}
		// A line starting with an empty block is that block, and what follows
		// it on the line a line of its own beside it (seen 2026-11-14:
		// "<empty-block/> {color=\"red\"}" is an empty paragraph and an empty
		// red one, "<empty-block/> abc" one and "abc", "<empty-block/>
		// <empty-block/>" two; an item's "- <empty-block/> x" is its text).
		emptyLead := false
		for len(items) == 0 && notionMarkdownVersion1EmptyLead(rest) {
			apart += written + notionMarkdownVersion1EmptyBlock + "\n"
			rest, emptyLead = strings.TrimLeft(rest[len(notionMarkdownVersion1EmptyBlock):], " \t"), true
		}
		// An empty block holds no blocks, so a line further in after it
		// stands beside it (seen 2026-11-11: "<empty-block/>\n\tdeep" two blocks,
		// and 2026-11-14 after "<empty-block/> {color=\"red\"}" too).
		// So does a heading, a toggle heading aside, in a list item too (seen
		// 2026-11-14: "## h⏎  b" is the heading and the paragraph "b",
		// "### h⏎\tb⏎\t\tc" the heading and "b" holding "c", "- # h⏎    b" an
		// item holding the heading and "b").
		if emptyLead || len(items) == 0 && strings.EqualFold(strings.ReplaceAll(strings.TrimSpace(rest), " ", ""), notionMarkdownVersion1EmptyBlock) ||
			notionMarkdownVersion1PlainHeading.MatchString(rest) && !strings.Contains(rest, `{toggle="true"`) {
			depths.open = depths.open[:len(depths.open)-1]
		}
		// Code and an equation hold no blocks either, so a line further in
		// after them stands beside them (seen 2026-11-14: "```js⏎a⏎```⏎  c"
		// is the code and "c", "- ```js⏎  a⏎  ```⏎    c" and "- $$⏎  x⏎
		// $$⏎  c" an item holding both).
		if strings.HasPrefix(rest, "```") || strings.TrimSpace(rest) == "$$" {
			depths.open = depths.open[:len(depths.open)-1]
		}
		if strings.HasPrefix(rest, "```") {
			quoted = false
			fence := notionMarkdownFenceOf(rest)
			verbatim = notionMarkdownVersion1Verbatim{fence: fence, written: written, held: len(items) > 0}
			lines[index] = apart + written + fence + notionMarkdownFenceLanguage(rest[len(fence):])
			continue
		}
		if strings.TrimSpace(rest) == "$$" {
			verbatim = notionMarkdownVersion1Verbatim{equation: true, written: written, own: own, opening: index}
			lines[index] = apart + written + "$$"
			continue
		}
		if strings.HasPrefix(rest, "|") {
			// A table holds no blocks either (notion_page_markdown_version1_tables.go).
			quoted, depths.open = false, depths.open[:len(depths.open)-1]
			lines[index] = apart + notionMarkdownVersion1PipeTable(lines, index, rest, written)
			continue
		}
		// A bookmark or an embed with its caption between its tags on one
		// line is that block in "v1" too, its caption trimmed; closed in
		// itself it is text (seen 2026-11-10). A file's, an image's and the
		// other media's caption is trimmed too.
		if match := notionMarkdownVersion1LinkBlock.FindStringSubmatch(strings.TrimSpace(rest)); match != nil {
			if match[1] == match[4] {
				lines[index] = apart + written + "<" + match[1] + match[2] + ">" + strings.TrimSpace(match[3]) + "</" + match[1] + ">"
				continue
			}
		} else if notionMarkdownLinkBlockPattern.MatchString(strings.TrimSpace(rest)) ||
			notionMarkdownVersion1EmbedOpening.MatchString(strings.TrimSpace(rest)) ||
			notionMarkdownVersion1MediaTag.MatchString(strings.TrimSpace(rest)) {
			// So is an embed's tag that its caption's lines follow, and a
			// file's or an image's tag alone on its line, whatever follows it,
			// or closed in itself (seen 2026-11-11).
			rest = `\` + rest
		} else if notionMarkdownVersion1OwnLineTag.MatchString(strings.TrimSpace(rest)) {
			// A sub-page's or a database's tag whose title stands on lines of
			// its own is no tag in "v1", where a tag and its title stand on one
			// line: its lines are text, and what it named goes unless named
			// elsewhere (seen 2026-11-13, refused without
			// allow_deleting_content).
			rest = `\` + rest
		} else if opened, closed := notionMarkdownVersion1ClosedReference(rest); closed {
			rest = opened
		} else if match := notionMarkdownVersion1MediaLine.FindStringSubmatch(strings.TrimSpace(rest)); match != nil && match[1] == match[4] {
			rest = "<" + match[1] + match[2] + ">" + strings.TrimSpace(match[3]) + "</" + match[1] + ">"
		}
		// Dashes with spaces between them are text in "v1" (seen
		// 2026-11-11: "-- -"), where the reader takes a divider.
		if trimmed := strings.TrimSpace(rest); notionMarkdownFlowThematic.MatchString(trimmed) &&
			!workspaceImportVersion1DividerPattern.MatchString(trimmed) {
			rest = `\` + rest
		}
		// A tag holding blocks is text where it stands not alone at its
		// line's start, as a callout on one line aside, or closes none
		// (notionMarkdownVersion1TagsAsText).
		rest, tagText := notionMarkdownVersion1TagsAsText(rest, closing != nil && notionMarkdownVersion1ContainerTags[closing[1]] && !stray)
		tagText = tagText || summaryText
		// A line of a block color alone is an empty paragraph of that color,
		// as Notion reads it (seen 2026-11-14: "{color=\"red\"}", a paragraph
		// of whitespace's line), where Leafen's reader takes a color after
		// a line's text.
		if len(items) == 0 && notionMarkdownVersion1ColorOnly.MatchString(strings.TrimSpace(rest)) {
			rest = "<empty-block/> " + strings.TrimSpace(rest)
		}
		// A callout's first line, a paragraph of its own, holds the lines
		// further in than it, as Notion's (seen 2026-11-14:
		// "<callout>\na\n\tb\n</callout>" shows "b" under "a"; its block list
		// gives "a" as the callout's text and leaves "b" out,
		// notionCalloutFirstLine).
		rest, mark := notionMarkdownVersion1Colored(rest)
		text := notionMarkdownVersion1LineEnd(notionMarkdownVersion1EdgeTrimmed(notionMarkdownVersion1Held(rest))) + mark
		opened := len(tags)
		if tags.open(strings.TrimSpace(rest), written, &depths) {
			lines[index] = notionMarkdownVersion1Dropped
			continue
		}
		// A tag an item holds holds the lines further in than the item: its
		// toggle's or synced block's lines stand where it does.
		if len(tags) > opened && len(items) > 0 {
			tags[len(tags)-1].item = notionMarkdownVersion1Width(lead) + 1
			if tags[len(tags)-1].depths == nil {
				depths.open = depths.open[:len(depths.open)-1]
			}
		} else if len(tags) == 1 && opened == 0 {
			tags[0].least = notionMarkdownVersion1Width(lead)
		}
		lines[index] = apart + notionMarkdownVersion1Escaped(written+text, tagText, !tabled[index])
		if name, size := notionMarkdownVersion1OpenedTag(strings.TrimSpace(rest)); name == "details" && size == len(strings.TrimSpace(rest)) &&
			notionMarkdownVersion1Titled(lines, index) {
			details = index
		}
	}
	verbatim.close(lines)
	// An equation no $$ closes runs to the end (seen 2026-11-10: "- $$" is an
	// item holding an equation of nothing), as does code, closed before the
	// tags left open.
	if verbatim.equation && len(lines) > 0 {
		lines[len(lines)-1] += "\n" + verbatim.written + "$$"
	} else if verbatim.fence != "" && len(tags) > 0 && len(lines) > 0 {
		lines[len(lines)-1] += "\n" + verbatim.written + verbatim.fence
	}
	kept := lines[:0]
	for _, line := range lines {
		if line != notionMarkdownVersion1Dropped {
			kept = append(kept, line)
		}
	}
	// A tag left open closes at the end (seen 2026-11-13: "<callout>\na" is
	// the callout "a", "<columns>\n<column>\nb" a column holding "b",
	// "<column>\na" a column holding "a", "<tab>\na" the paragraph "a").
	for at := len(tags) - 1; at >= 0; at-- {
		if line := tags[at].closing("</" + tags[at].name + ">"); line != notionMarkdownVersion1Dropped {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// notionMarkdownVersion1LineEnd is a line's text with the "\" ending it as
// "v1" reads it, where Leafen's reader would go on in the next line: text, the
// line ending there, or the whitespace after it when some follows (seen
// 2026-11-13: "a\\\nb" is "a\" and "b", "- a\\\n  b" the item "a\" holding
// "b", "a\\  \nb" "a  " and "b", "\\  \n\tb" the paragraph "  " holding "b",
// "\\\nb" "\" and "b").
func notionMarkdownVersion1LineEnd(text string) string {
	body := strings.TrimRight(text, " \t")
	if (len(body)-len(strings.TrimRight(body, `\`)))%2 == 0 {
		return text
	}
	if body == text {
		return text + `\`
	}
	return body[:len(body)-1] + "<span>" + text[len(body):] + "</span>"
}

// notionMarkdownVersion1EdgeTrimmed is a heading's or a quote's line without
// the whitespace it ends with, which "v1" drops before it reads the "\"
// ending it, so that "\" is text (seen 2026-11-14: "## \\  e \\ " is the
// heading "  e \", "> e\\ " and "> e \\\t" the quotes "e\" and "e \", in a
// list item or a to-do too, "## e  " "e"); a paragraph's, an item's or a
// to-do's line keeps it ("e \\ " is "e  ").
func notionMarkdownVersion1EdgeTrimmed(text string) string {
	if strings.HasPrefix(text, ">") || notionMarkdownVersion1HeldHeading.MatchString(text) {
		return strings.TrimRight(text, " \t")
	}
	return text
}

// notionMarkdownVersion1Dropped stands for a line the reading leaves out.
const notionMarkdownVersion1Dropped = "\x00"

// notionMarkdownVersion1ClosingTag is a closing tag a line starts with.
var notionMarkdownVersion1ClosingTag = regexp.MustCompile(`^</([a-z_-]+)>`)

var notionMarkdownVersion1ColorOnly = regexp.MustCompile(`^\{color="[a-z_]+"\}$`)

// notionMarkdownVersion1PlainHeading is a heading's line with its text.
var notionMarkdownVersion1PlainHeading = regexp.MustCompile(`^#{1,6}[ \t]+\S`)

var notionMarkdownVersion1EmbedOpening = regexp.MustCompile(`^<embed(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*>$`)

var (
	// A file's, an image's or another medium's tag alone on its line, which
	// "v1" reads as text (seen 2026-11-10 and 2026-11-11).
	notionMarkdownVersion1MediaTag  = regexp.MustCompile(`^<(?:image|file|pdf|video|audio)(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*/?>$`)
	notionMarkdownVersion1MediaLine = regexp.MustCompile(`^<(image|file|pdf|video|audio)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>(.*?)</(image|file|pdf|video|audio)>$`)
)

// notionMarkdownVersion1OwnLineTag is a sub-page's or a database's opening
// tag alone on its line, or its closing tag.
var notionMarkdownVersion1OwnLineTag = regexp.MustCompile(`^(?:<(?:page|database)(?:[ \t]+[a-z-]+="[^"]*")*[ \t]*>|</(?:page|database)>)$`)

var notionMarkdownVersion1LinkBlock = regexp.MustCompile(`^<(bookmark|embed)((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*>(.*?)</(bookmark|embed)>$`)

var notionMarkdownVersion1Item = regexp.MustCompile(`^(?:- \[[ xX]\]|[-*+]|\d{1,9}\.)(?:[ \t]+|$)`)

// A to-do in "v1" is "- [ ]" or "- [x]", one space between, and so is one
// a "-" before it holds, which stands for it (seen 2026-11-14: "- - [ ] b"
// and "-  - [x] b" are the to-do "b", "- - - [ ] b" an item holding it,
// "* - [ ] b" too); a box after any other marker is its item's text ("* [ ]
// b", "-  [ ] b", "-\t[ ] b", "1. [ ] b", "- -  [ ] b" is an item holding
// the item "[ ] b", "- - [ ]b" the item "[ ]b").
var (
	notionMarkdownVersion1Todo     = regexp.MustCompile(`^- \[[ xX]\](?:[ \t]|$)`)
	notionMarkdownVersion1HeldTodo = regexp.MustCompile(`^-[ \t]+- \[[ xX]\](?:[ \t]|$)`)
	notionMarkdownVersion1ItemBox  = regexp.MustCompile(`^(?:[-*+]|\d{1,9}\.)[ \t]+\[[ xX]\](?:[ \t]|$)`)
)

// notionMarkdownVersion1ItemText is a list item's line as Leafen's reader
// reads what "v1" reads: a box after another marker than a to-do's escaped
// as its text, and an empty block's tag its text alone in it gone (seen
// 2026-11-14: "- <empty-block/>" is an empty item).
func notionMarkdownVersion1ItemText(rest string) string {
	if marker := notionMarkdownVersion1Item.FindString(rest); marker != "" &&
		strings.EqualFold(strings.ReplaceAll(strings.TrimSpace(rest[len(marker):]), " ", ""), notionMarkdownVersion1EmptyBlock) {
		return strings.TrimRight(marker, " \t")
	}
	if !notionMarkdownVersion1ItemBox.MatchString(rest) || notionMarkdownVersion1Todo.MatchString(rest) {
		return rest
	}
	at := strings.IndexByte(rest, '[')
	return rest[:at] + `\` + rest[at:]
}

// notionMarkdownVersion1Dashes is an item's text of dashes alone, a divider
// the item holds (seen 2026-11-11: "- --").
var notionMarkdownVersion1Dashes = regexp.MustCompile(`^-{2,}[ \t]*$`)

// notionMarkdownVersion1EmptyBlock is an empty paragraph's tag.
const notionMarkdownVersion1EmptyBlock = "<empty-block/>"

// notionMarkdownVersion1EmptyLead tells a line's rest that starts with an
// empty block's tag and has more than whitespace after it.
func notionMarkdownVersion1EmptyLead(rest string) bool {
	tag := len(notionMarkdownVersion1EmptyBlock)
	return len(rest) > tag && strings.EqualFold(rest[:tag], notionMarkdownVersion1EmptyBlock) &&
		strings.TrimSpace(rest[tag:]) != ""
}

// notionMarkdownVersion1Items are the markers of the items a line starts
// with whose text starts another block, each its own empty item, and the
// line's rest.
func notionMarkdownVersion1Items(rest string) ([]string, string) {
	items := []string{}
	for {
		if notionMarkdownVersion1HeldTodo.MatchString(rest) {
			rest = strings.TrimLeft(rest[1:], " \t")
		}
		marker := notionMarkdownVersion1Item.FindString(rest)
		text := rest[len(marker):]
		if marker == "" || !(notionMarkdownVersion1Item.MatchString(text) || strings.HasPrefix(text, ">") || strings.HasPrefix(text, "|") ||
			notionMarkdownVersion1HeldHeading.MatchString(text) || strings.HasPrefix(text, "```") || strings.TrimSpace(text) == "$$" ||
			notionMarkdownVersion1Dashes.MatchString(text) || notionMarkdownVersion1ItemTag(text)) {
			return items, rest
		}
		items = append(items, strings.TrimRight(marker, " \t"))
		rest = text
	}
}

// notionMarkdownVersion1ItemTag tells an item's text that is a block's tag,
// which "v1" reads as a block the item holds (seen 2026-11-14: "- <callout>⏎
// a⏎  </callout>", "- <details>…", "- <columns>…", "- <synced_block>…",
// "- <callout>a</callout>", "- <table_of_contents/>", "- <image …>c</image>"
// and "- <bookmark …>c</bookmark>" each an empty item holding the block);
// "- <callout>a" and "- x</callout>" are text.
func notionMarkdownVersion1ItemTag(text string) bool {
	trimmed := strings.TrimSpace(text)
	name, size := notionMarkdownVersion1OpenedTag(trimmed)
	if size == len(trimmed) || name == "callout" && strings.Contains(trimmed[size:], "</callout>") ||
		notionMarkdownAPIContents.MatchString(trimmed) {
		return true
	}
	for _, line := range []*regexp.Regexp{notionMarkdownVersion1LinkBlock, notionMarkdownVersion1MediaLine} {
		if match := line.FindStringSubmatch(trimmed); match != nil && match[1] == match[4] {
			return true
		}
	}
	return false
}

// notionMarkdownVersion1Held is a quote's or a list item's text as written
// where the reader would read another block in it: a quote's text is text,
// and so is an item's that "v1" doesn't read as a block ("- ***").
func notionMarkdownVersion1Held(rest string) string {
	if strings.HasPrefix(rest, ">") {
		text := strings.TrimPrefix(rest[1:], " ")
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || len(text)-len(strings.TrimLeft(text, " ")) >= 4 || !notionMarkdownFlowStarts(trimmed, "", false) {
			return rest
		}
		// Its backticks and $ read as they do in "v1" text (seen 2026-11-10):
		// two backticks are code of nothing, so "> ```x" is "`x", and $$ an
		// equation of nothing (workspaceImportInlineParser.version1), which
		// the quote holds (seen 2026-11-13: "> $$⏎> q⏎> $$" is three quotes,
		// the first and last each an empty equation), written as "v1" writes one.
		for strings.HasPrefix(trimmed, "```") {
			trimmed = trimmed[2:]
		}
		if trimmed == "$$" {
			return "> $``$"
		}
		return "> " + notionMarkdownFlowLiteral(trimmed)
	}
	if marker, width, item := notionMarkdownFlowItem(rest); item && width <= len(rest) &&
		notionMarkdownFlowItemHolds(strings.TrimSpace(rest[width:])) {
		text := strings.TrimSpace(rest[width:])
		// A heading's marks alone keep the whitespace after them (seen
		// 2026-11-14: "- ##   " is the item "##   ").
		if strings.Trim(text, "#") == "" {
			text += rest[len(strings.TrimRight(rest, " \t")):]
		}
		return marker + " " + notionMarkdownVersion1Literal(text)
	}
	return rest
}

// notionMarkdownVersion1Literal is an item's text that would start another
// block, written so that "v1" reads it as text: a divider's * and _ each
// escaped, as Notion keeps "- ***" as written where "v1" pairs them in text
// ("> ***" is "*", its "**" an italic of nothing; seen 2026-11-10).
func notionMarkdownVersion1Literal(trimmed string) string {
	if notionMarkdownFlowThematic.MatchString(trimmed) {
		return strings.NewReplacer("*", `\*`, "_", `\_`).Replace(trimmed)
	}
	return notionMarkdownFlowLiteral(trimmed)
}

// notionMarkdownVersion1Depths reads how deep "v1" lines stand: by how far
// in each is against the lines before it, not by steps of two (seen
// 2026-11-10): a line further in than the one before is its child, by one
// space or by six; one less far in sits under the nearest line before it
// that stands less far in than it, or at the top. A tab is one space, as
// far in as a space (seen 2026-11-14: "\t1. a⏎  2. b" and "\ta⏎  b" hold
// "b" in "a", "  a⏎\t\tb", " a⏎\tb" and "\t1. a⏎\t2. b" are two blocks
// side by side, "- a⏎\t- b⏎  - c" holds "c" in "b").
type notionMarkdownVersion1Depths struct {
	open []int
}

func (depths *notionMarkdownVersion1Depths) of(lead string) int {
	width := notionMarkdownVersion1Width(lead)
	for len(depths.open) > 0 && depths.open[len(depths.open)-1] >= width {
		depths.open = depths.open[:len(depths.open)-1]
	}
	depths.open = append(depths.open, width)
	return len(depths.open) - 1
}

// notionMarkdownVersion1Width is how far in whitespace stands, a tab as one space.
func notionMarkdownVersion1Width(lead string) int {
	return len(lead)
}

// notionMarkdownVersion1Text escapes the "v2" tags in a line, outside its
// code and its equations.
func notionMarkdownVersion1Text(line string) string {
	return notionMarkdownVersion1Escaped(line, false, false)
}

// notionMarkdownVersion1Escaped escapes the "v2" tags in a line, outside its
// code and its equations (notionMarkdownVersion1EquationReader), with
// tagText the tags holding blocks too, and with tableText a table's.
func notionMarkdownVersion1Escaped(line string, tagText, tableText bool) string {
	if !strings.Contains(line, "<") {
		return line
	}
	var out strings.Builder
	// A link's address is as written, its < too, after a [ that opens its
	// text (seen 2026-11-11: "[a](<b>)" links "<b>"; version1Link).
	opened := false
	equations := notionMarkdownVersion1EquationReader{line: line}
	for index := 0; index < len(line); index++ {
		if end := equations.at(index); end > index {
			out.WriteString(line[index:end])
			index = end - 1
			continue
		}
		switch character := line[index]; {
		case character == '\\' && index+1 < len(line):
			out.WriteString(line[index : index+2])
			index++
		case character == '*' || character == '_' || character == '~':
			// Marks that pair and their text, or marks as text, as the reading
			// reads them (workspaceImportInlineParser.parse).
			next := equations.emphasis(index)
			out.WriteString(line[index:next])
			index = next - 1
		case character == '[':
			opened = true
			out.WriteByte(character)
		case opened && strings.HasPrefix(line[index:], "]("):
			opened = false
			end := index + 2 + notionMarkdownVersion1LinkClose(line[index+2:])
			if end < index+2 {
				end = index + 1
			}
			out.WriteString(line[index : end+1])
			index = end
		case character == '`':
			// Code runs to the next run of as many backticks, as in CommonMark;
			// a run without one is backticks as written.
			run := len(line[index:]) - len(strings.TrimLeft(line[index:], "`"))
			ticks := line[index : index+run]
			end := index + run
			for end < len(line) {
				at := strings.Index(line[end:], ticks)
				if at < 0 {
					end = -1
					break
				}
				end += at
				following := len(line[end:]) - len(strings.TrimLeft(line[end:], "`"))
				if following == run {
					break
				}
				end += following
			}
			if end < 0 || end >= len(line) {
				out.WriteString(ticks)
				index += run - 1
				continue
			}
			out.WriteString(line[index : end+run])
			index = end + run - 1
		case character == '<' && notionMarkdownVersion1InlineQuoted(line[index:]):
			// A mention's or a span's values in single quotes or none, put
			// in double quotes (notion_page_markdown_version1_quotes.go).
			tag, size := notionMarkdownVersion1InlineQuotes(line[index:])
			out.WriteString(tag)
			index += size - 1
		case character == '<' && (notionMarkdownVersion2OnlyTag.MatchString(line[index:]) ||
			tagText && notionMarkdownVersion1TagInText.MatchString(line[index:]) ||
			tableText && notionMarkdownVersion1TableTagText.MatchString(line[index:]) ||
			notionMarkdownVersion1TextPreview(line[index:]) ||
			notionMarkdownAutolinkAt(line[index:]) && !strings.HasSuffix(line[:index], "](")):
			out.WriteString(`\<`)
		default:
			out.WriteByte(character)
		}
	}
	return out.String()
}

var notionMarkdownVersion1LinkPreview = regexp.MustCompile(`^<link-preview((?:[ \t]+[A-Za-z][A-Za-z-]*="[^"]*")*)[ \t]*/>`)

// notionMarkdownVersion1TextPreview reports whether text starts with a
// <link-preview> tag "v1" reads as text: one not closed in itself, or
// without an address (seen 2026-11-10).
func notionMarkdownVersion1TextPreview(text string) bool {
	if !strings.HasPrefix(text, "<link-preview") || len(text) > len("<link-preview") && !strings.ContainsRune(" \t/>", rune(text[len("<link-preview")])) {
		return false
	}
	match := notionMarkdownVersion1LinkPreview.FindStringSubmatch(text)
	return match == nil || notionMarkdownAttributes(match[1])["url"] == ""
}

// notionMarkdownAutolinkAt reports whether text starts with an address
// between angle brackets, as CommonMark's autolinks are written.
func notionMarkdownAutolinkAt(text string) bool {
	end := strings.IndexByte(text, '>')
	return end > 1 && !strings.ContainsAny(text[1:end], " \t<") && workspaceImportLinkTarget(text[1:end]) != ""
}
