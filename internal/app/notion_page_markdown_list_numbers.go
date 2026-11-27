package app

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// A numbered list's start number and marker format in page Markdown, as
// Notion's reads and writes them (seen 2026-11-10). "v2" and the MCP's
// Markdown number each item as the screen does — from its own start, or on
// from the item before it — and in its own format, numbers for one without:
// "10. ten" and "11. eleven"; items in letters or roman numerals are a list's
// tags, <ol type="a" start="3"> (type="i" for roman), the items after them in
// another format going on with the numbers without a blank line between. An
// item whose own start breaks the count opens a list of its own after a blank
// line, <ol start="7"> (start="1" too), holding the items after it in its
// format. "v1" numbers each list from 1. Read, each numbered item takes the
// format its line or its <ol type> gives, numbers for a plain line; a list's
// first item — one not right after another numbered item, or a plain line
// right after an <ol>'s items — takes the number its line starts with when
// that isn't 1 ("4. d" after </ol> keeps 4), and an item an <ol start> opens
// takes that start wherever it stands. "v1" reads no start. A plain line's
// number is one to nine digits, as Markdown's: "v2" reads "1234567890. x" as
// text, a paragraph or a line of the block above (seen 2026-11-11), where
// "v1" makes an item of it (notionMarkdownVersion2LongNumber).

// notionMarkdownListPlace is a numbered item's number and marker format
// among the blocks it stands with, and whether its own start breaks its
// list's count.
type notionMarkdownListPlace struct {
	number  int
	format  string
	restart bool
}

// notionMarkdownListMarkPattern is what Leafen's reading puts at the end of
// an <ol>'s item's line for its format and start, before any color
// (notionMarkdownGroup).
var notionMarkdownListMarkPattern = regexp.MustCompile(`^(.*?)[ \t]*\{list-format="(numbers|letters|roman)"(?: list-start="([0-9]{1,16})")?\}$`)

func notionMarkdownNumbered(blockType string) bool {
	return notionMarkdownListOf(blockType) == "ol"
}

// notionMarkdownStoredListStart is a numbered item's stored start; 0 for none.
func notionMarkdownStoredListStart(content map[string]any) int {
	value, ok := content["listStartIndex"].(float64)
	if !ok || value != math.Trunc(value) || value < 1 || value > maxNumberedListStartIndex {
		return 0
	}
	return int(value)
}

// notionMarkdownListPlaces numbers the numbered items among blocks standing
// one after another, as the screen counts them (numberedList.ts): from an
// item's own start, or on from the item before it, or from 1. Each is in its
// own format, numbers for one without, as Notion's Markdown writes a list
// whose first item alone is in letters: "a." then "2." (seen 2026-11-10).
func notionMarkdownListPlaces(blocks []workspaceExportMarkdownBlock) []notionMarkdownListPlace {
	places := make([]notionMarkdownListPlace, len(blocks))
	for index, block := range blocks {
		if !notionMarkdownNumbered(block.Type) {
			continue
		}
		format, _ := block.Content["listFormat"].(string)
		if !validNumberedListFormat(format) {
			format = numberedListFormatNumbers
		}
		places[index].format = format
		start, following := notionMarkdownStoredListStart(block.Content), index > 0 && notionMarkdownNumbered(blocks[index-1].Type)
		switch {
		case start > 0:
			places[index].number = start
			places[index].restart = following && start != places[index-1].number+1
		case following:
			places[index].number = places[index-1].number + 1
		default:
			places[index].number = 1
		}
	}
	return places
}

// notionMarkdownOrderedOpening is the <ol> tag a list's items in one format
// stand in, from the first item's number: named past 1, or always where it
// breaks its list's count.
func notionMarkdownOrderedOpening(place notionMarkdownListPlace) string {
	opening := "<ol"
	switch place.format {
	case numberedListFormatLetters:
		opening += ` type="a"`
	case numberedListFormatRoman:
		opening += ` type="i"`
	}
	if place.number > 1 || place.restart {
		opening += ` start="` + strconv.Itoa(place.number) + `"`
	}
	return opening + ">"
}

// notionMarkdownOrderedMark is what an <ol>'s item's line ends with as
// Leafen reads it: the list's format, and its start on the first item.
func notionMarkdownOrderedMark(attributes map[string]string, first bool) string {
	format := numberedListFormatNumbers
	switch attributes["type"] {
	case "a", "A":
		format = numberedListFormatLetters
	case "i", "I":
		format = numberedListFormatRoman
	}
	mark := ` {list-format="` + format + `"`
	if start, err := strconv.Atoi(attributes["start"]); first && err == nil && start >= 1 && start <= maxNumberedListStartIndex {
		mark += ` list-start="` + strconv.Itoa(start) + `"`
	}
	return mark + "}"
}

// notionMarkdownNumberedContent is a numbered item's text and content as its
// line gives them: its format, and its start when its number isn't 1 or
// its <ol> named one, which named holds wherever the item stands; tagged
// tells an <ol>'s item from a plain line. "v1" (keepLineEnds) reads the
// format alone.
func notionMarkdownNumberedContent(line, text string, content json.RawMessage, keepLineEnds bool) (string, json.RawMessage, bool, bool) {
	fields := map[string]any{}
	if len(content) > 0 && json.Unmarshal(content, &fields) != nil {
		return text, content, false, false
	}
	fields["listFormat"] = numberedListFormatNumbers
	named, tagged := false, false
	if !keepLineEnds {
		marker := strings.TrimLeft(line, " \t")
		if number, err := strconv.Atoi(marker[:strings.IndexByte(marker, '.')]); err == nil && number > 1 &&
			number <= maxNumberedListStartIndex {
			fields["listStartIndex"] = number
		}
		if match := notionMarkdownListMarkPattern.FindStringSubmatch(text); match != nil {
			text, fields["listFormat"], tagged = match[1], match[2], true
			delete(fields, "listStartIndex")
			if start, err := strconv.Atoi(match[3]); err == nil && start >= 1 && start <= maxNumberedListStartIndex {
				fields["listStartIndex"], named = start, true
			}
		}
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return text, content, false, false
	}
	return text, encoded, named, tagged
}

// notionMarkdownListStarts leaves a start only on an item that begins its
// list, save one its <ol> named (named, by item).
func notionMarkdownListStarts(items []workspaceImportStagedItem, named map[int]bool) {
	types := make(map[string]string, len(items))
	for _, item := range items {
		types[item.ID] = item.Type
	}
	for index, item := range items {
		if item.Type != graphBlockTypeNumberedListItem || named[index] || types[item.AfterID] != graphBlockTypeNumberedListItem {
			continue
		}
		fields := map[string]any{}
		if json.Unmarshal(item.Content, &fields) != nil || fields["listStartIndex"] == nil {
			continue
		}
		delete(fields, "listStartIndex")
		if encoded, err := json.Marshal(fields); err == nil {
			items[index].Content = encoded
		}
	}
}

// notionMarkdownListKindItems puts a <ul>'s or an <ol>'s Markdown items, the
// lines no <li> holds as the group reads them at its level, in its kind, as
// Notion's "v2" reads them (seen 2026-11-14, create and update alike):
// "<ul>⏎1. a⏎</ul>" and "<ul>⏎1) a⏎2. b⏎</ul>" are bulleted, "<ol>⏎- a⏎
// </ol>" and "<ol>⏎* a⏎</ol>" numbered; a to-do stays one ("<ol>⏎- [x] a⏎
// </ol>") and the items further in keep their own ("<ul>⏎- a⏎  1. b⏎</ul>"
// holds the numbered "b", "<ol>⏎- a⏎  - b⏎</ol>" the bulleted "b"). An
// <ol>'s numbered so carry its type, and its first item its start, as its
// <li>s do. "v1" reads the tags as text (notionMarkdownVersion1Input).
func notionMarkdownListKindItems(lines []string, level int, name string, attributes map[string]string, first bool) ([]string, bool) {
	tabs := strings.Repeat("\t", level)
	out := make([]string, len(lines))
	for at, line := range lines {
		out[at] = line
		rest, ok := strings.CutPrefix(line, tabs)
		if !ok || strings.HasPrefix(rest, "\t") {
			continue
		}
		marker, width, item := notionMarkdownFlowItem(rest)
		if !item {
			continue
		}
		text := rest[min(width, len(rest)):]
		numbered := strings.HasSuffix(marker, ".")
		switch {
		case name == "ul" && numbered:
			out[at] = tabs + "- " + text
		case name == "ol" && !numbered && !notionMarkdownFlowTodo.MatchString(text):
			out[at] = tabs + "1. " + text + notionMarkdownOrderedMark(attributes, first)
		}
		first = false
	}
	return out, first
}
