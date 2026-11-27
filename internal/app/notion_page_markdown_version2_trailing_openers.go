package app

import (
	"regexp"
	"strings"
)

// A tag a block's text ends in before its > is Notion's "Trailing
// incomplete <x> tag opener" when what follows its < or </ is the start of
// a tag name Notion's "v2" knows, named as written (seen 2026-11-15:
// "x</co", "x</l", "x<t", "x<tabl", "x</ta", "x<de", "x<e", "x<w", "x<r",
// "x<lin", "x<ex" and "x<t y" say <co>, </l>, <t>, …; "x<sp", "x</st",
// "x<cod", "x<summ", "x<date", "x<img", "x<h5", "x<br", "x<link-",
// "x<table_", "x<routines", "x<T" and "x<1" are text, and "x</" too). The
// names are those of the tags it reads in a block's text
// (notionMarkdownVersion2Unclosed) and four more it reads no block of:
// <routine>, <worker>, <line> and <external_object_instance>, "x<routine",
// "x<worker y", "x<line y" and "x<external_o" are said (Leafen reads no
// block of their whole tags, which stay text). One only a mention's name starts,
// "x<men", "x<mention-p", goes with the rest of the line, as an unclosed
// mention does (notionMarkdownVersion2End) — but "x<me", <meeting-notes>'s
// start too, is said.

// notionMarkdownVersion2OpenerNames are the tag names whose start a
// trailing tag opener is.
var notionMarkdownVersion2OpenerNames = []string{
	"b", "i", "s", "u", "em", "a", "p", "h1", "h2", "h3", "h4", "quote", "toggle", "callout", "ul", "ol", "li", "todo",
	"column", "columns", "tab", "tabs", "table", "thead", "tbody", "colgroup", "col", "tr", "td", "th", "details",
	"equation", "image", "file", "pdf", "video", "audio", "bookmark", "embed", "page", "database", "synced-block",
	"synced-block-reference", "empty-block", "table-of-contents", "mail", "meeting-notes", "unknown", "custom-block",
	"folder", "routine", "worker", "line", "external_object_instance",
}

var (
	notionMarkdownVersion2TrailingTag   = regexp.MustCompile(`<(/?)([a-z][a-z0-9_-]*)(?:[ \t\n][^<>]*)?$`)
	notionMarkdownVersion2MentionOpener = regexp.MustCompile(`<men[a-z-]*$`)
)

// notionMarkdownVersion2TrailingOpener is where a tag a text ends in before
// its > stands, as notionMarkdownVersion2TrailingTag's submatches, or nil
// when it names no tag's start or is escaped.
func notionMarkdownVersion2TrailingOpener(text string) []int {
	found := notionMarkdownVersion2TrailingTag.FindStringSubmatchIndex(text)
	if found == nil || notionMarkdownEscapedAt(text, found[0]) {
		return nil
	}
	name := text[found[4]:found[5]]
	for _, known := range notionMarkdownVersion2OpenerNames {
		if strings.HasPrefix(known, name) {
			return found
		}
	}
	return nil
}

// notionMarkdownVersion2MentionOpenerAt is where a mention's name the
// Markdown's last line ends in starts, which goes; -1 for none.
func notionMarkdownVersion2MentionOpenerAt(last string) int {
	found := notionMarkdownVersion2MentionOpener.FindStringIndex(last)
	if found == nil || notionMarkdownEscapedAt(last, found[0]) ||
		!strings.HasPrefix("mention-", last[found[0]+1:min(found[1], found[0]+1+len("mention-"))]) {
		return -1
	}
	return found[0]
}
