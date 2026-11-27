package app

import (
	"maps"
	"slices"
	"strings"
)

// A quote's lines of text are one text to Notion's "v2" reading of inline
// tags (seen 2026-11-15): "> x <b>y⏎> z</b>" is the quote "x **y⏎z**", "> x
// <b>y⏎> z</b> w <i>v" says only the <i>, "> x <ul> y⏎> z </ul>" only that
// the <ul> cannot stand inline, and "> <ul>⏎> x⏎> </ul> y" says so too, as
// a paragraph's lines do; in a quote in a quote, or in a list item, alike.
// A line holding as many > or fewer ("> > x <b>y⏎> z</b>" is the inner
// quote's "x **y⏎z**") that starts no block of its own goes on with the
// text the line before it holds; one holding more, a blank quote line, a
// heading, an item, a block's tag, code or $$ ends it ("> x <b>y⏎>⏎> z</b>"
// and "> x <b>y⏎> > z</b>" say the <b>).
// Leafen's reader joins those lines into the quote's text on its own
// (notionMarkdownFlowQuote): the text is joined here only to read its tags.

// notionMarkdownVersion2QuoteText is the quote text a line of out goes on
// with: the line in out holding its first line, and how many > it holds.
type notionMarkdownVersion2QuoteText struct{ first, quotes int }

// notionMarkdownVersion2QuoteTextAt tells how many > hold a line whose text
// starts a quote's text — its run of markers (prefix) ending in a quote and
// its text (body) starting no block of its own — or 0 when it starts none.
func notionMarkdownVersion2QuoteTextAt(prefix, body, next string) int {
	if !strings.HasSuffix(strings.TrimRight(prefix, " \t"), ">") || body == "" ||
		notionMarkdownVersion2Starts(body, strings.TrimSpace(strings.TrimLeft(next, " \t>")), false) {
		return 0
	}
	return strings.Count(prefix, ">")
}

// notionMarkdownVersion2QuoteGoesOn tells a line that goes on with the
// quote text open before it: one holding nothing but as many > before its
// text or fewer, which starts no block of its own (notionMarkdownVersion2Starts).
func notionMarkdownVersion2QuoteGoesOn(open notionMarkdownVersion2QuoteText, prefix, body, next, written string) bool {
	if open.quotes == 0 || body == "" || body == "$$" || strings.Trim(prefix, " \t>") != "" || strings.Count(prefix, ">") > open.quotes {
		return false
	}
	return !notionMarkdownVersion2TextListStart(written) &&
		!notionMarkdownVersion2Starts(body, strings.TrimSpace(strings.TrimLeft(next, " \t>")), false)
}

// notionMarkdownVersion2QuoteTexts are the blocks' texts by their first line
// in out with each quote's lines one text, the tags their lines met with
// them, and the last line each such text stands on.
func notionMarkdownVersion2QuoteTexts(texts map[int]string, places map[int]notionMarkdownVersion2BlockTags,
	joined map[int]notionMarkdownVersion2QuoteText) (map[int]string, map[int]notionMarkdownVersion2BlockTags, map[int]int) {
	if len(joined) == 0 {
		return texts, places, nil
	}
	texts, places, ends := maps.Clone(texts), maps.Clone(places), map[int]int{}
	for _, at := range slices.Sorted(maps.Keys(joined)) {
		quote := joined[at]
		texts[quote.first] += "\n" + notionMarkdownVersion2Unquoted(texts[at], quote.quotes)
		places[quote.first] = notionMarkdownVersion2BlockTags{places[quote.first].from, places[at].to}
		ends[quote.first] = at
		delete(texts, at)
		delete(places, at)
	}
	return texts, places, ends
}
