package app

import (
	"regexp"
	"strings"
)

// Where an anchor can't place content, as Notion's MCP refuses it (seen
// 2026-11-22 on a page of its own): an anchor standing inside a table —
// at a row's <tr>, a pipe table's row after its first, a cell's words — is
// refused before all else, "inside a table"; a column or a tab of a tabs
// block, where the content would go beside it (after_content's anchor ending
// in its own lines, before_content's starting at it), is refused naming it.
// An anchor on a table's first line or a column's or tab's words places as
// any other: after the table, inside the column or tab. An item of a list
// written as its tags (<ul>, <ol>: an item with a color, an empty one)
// splits its list when content goes beside it, as Notion's does (seen
// 2026-11-22: a paragraph after the first <li color> of two left that item
// in its own <ul>, the paragraph, and the other item after it): the tags
// close before the content and open again after it, unless the item ends or
// starts its list. Each refusal is in Leafen's words, with Notion's template
// form.

// notionMarkdownTabSection is the kind a tabs block's tab is marked with,
// apart from the tabs block itself, whose type is "tab".
const notionMarkdownTabSection = "tab section"

// notionMarkdownAnchorInTable refuses an anchor inside a table.
func notionMarkdownAnchorInTable(anchor string) error {
	return pageMarkdownMatchError{
		message:  "The anchor " + anchor + " is inside a table: anchor on what stands before or after the table.",
		template: "The anchor {anchor} is inside a table: anchor on what stands before or after the table.",
	}
}

// notionMarkdownAnchorBeside refuses content beside a block it can't stand
// beside: a table's row, a column or a tab; nil for any other.
func notionMarkdownAnchorBeside(span notionMarkdownBlockSpan, anchor string) error {
	section := ""
	switch span.Kind {
	case graphBlockTypeTableRow:
		return notionMarkdownAnchorInTable(anchor)
	case graphBlockTypeColumn:
		section = "column"
	case notionMarkdownTabSection:
		section = "tab"
	default:
		return nil
	}
	return pageMarkdownMatchError{
		message:  "Content doesn't go beside a " + section + ": anchor on what the " + section + " holds, or on the block that holds it.",
		template: "Content doesn't go beside a {section}: anchor on what the {section} holds, or on the block that holds it.",
	}
}

// notionMarkdownTaggedList is the list an item written as its tags stands
// in: its opening and closing tags.
type notionMarkdownTaggedList struct {
	opening, closing string
}

var (
	notionMarkdownListOpening        = regexp.MustCompile(`^<(ul|ol)(?:\s[^>]*)?>$`)
	notionMarkdownListStartAttribute = regexp.MustCompile(`\s+start="[^"]*"`)
)

// notionMarkdownTaggedListOf is the list of tags a block is an item of; none
// for a block that is no such item.
func notionMarkdownTaggedListOf(markdown string, span notionMarkdownBlockSpan) notionMarkdownTaggedList {
	if !strings.HasPrefix(markdown[span.start:], "<li") {
		return notionMarkdownTaggedList{}
	}
	indentation := span.indentation(markdown)
	lines := strings.Split(markdown[:max(span.line-1, 0)], "\n")
	depth := 0
	for index := len(lines) - 1; index >= 0; index-- {
		line, inside := strings.CutPrefix(lines[index], indentation)
		if !inside {
			continue
		}
		if line == "</ul>" || line == "</ol>" {
			depth++
			continue
		}
		match := notionMarkdownListOpening.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if depth > 0 {
			depth--
			continue
		}
		return notionMarkdownTaggedList{opening: notionMarkdownListStartAttribute.ReplaceAllString(line, ""), closing: "</" + match[1] + ">"}
	}
	return notionMarkdownTaggedList{}
}

// placed is the page's Markdown with content placed beside an item of the
// list, the list's tags closed before the content and opened after it, or
// the content before or after the whole list when the item starts or ends it.
func (list notionMarkdownTaggedList) placed(markdown string, span notionMarkdownBlockSpan, before bool, placed, last string) string {
	indentation := span.indentation(markdown)
	closing, opening := indentation+list.closing, indentation+list.opening
	if before {
		head, tail := markdown[:span.line], markdown[span.line:]
		previous := strings.TrimSuffix(head, "\n")
		at := strings.LastIndexByte(previous, '\n') + 1
		if line, inside := strings.CutPrefix(previous[at:], indentation); inside && notionMarkdownListOpening.MatchString(line) {
			// The list's first item: the content goes before the list.
			return notionMarkdownPlacedBesideLine(markdown, at, before, placed, last)
		}
		return head + closing + "\n\n" + placed + "\n\n" + opening + "\n" + tail
	}
	head, tail := markdown[:span.end], markdown[span.end:]
	if strings.HasPrefix(tail, "\n"+closing) {
		// The list's last item: the content goes after the list.
		end := span.end + 1 + len(closing)
		return notionMarkdownPlacedBesideLine(markdown, end, before, placed, last)
	}
	return head + "\n" + closing + "\n\n" + placed + "\n\n" + opening + tail
}

// notionMarkdownPlacedBesideLine places content at a line's start (before)
// or end (after) as notionMarkdownPlacedBeside places it beside a block.
func notionMarkdownPlacedBesideLine(markdown string, at int, before bool, placed, last string) string {
	span := notionMarkdownBlockSpan{line: at, start: at, end: at}
	for span.start < len(markdown) && (markdown[span.start] == ' ' || markdown[span.start] == '\t') {
		span.start++
	}
	if before {
		head, tail := markdown[:span.line], markdown[span.line:]
		if head != "" && !strings.HasSuffix(head, "\n\n") {
			head += strings.Repeat("\n", 2-min(2, len(head)-len(strings.TrimRight(head, "\n"))))
		}
		return head + placed + "\n\n" + notionMarkdownRunRestarted(tail, "", last)
	}
	head, tail := markdown[:at], markdown[at:]
	if next := strings.TrimLeft(strings.TrimLeft(tail, "\n"), " \t"); tail != "" && !strings.HasPrefix(tail, "\n\n") &&
		!strings.HasPrefix(next, "</") {
		placed += "\n"
	}
	return head + "\n\n" + placed + tail
}
