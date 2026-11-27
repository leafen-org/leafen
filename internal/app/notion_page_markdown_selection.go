package app

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A selection — insert_content's after, replace_content_range's
// content_range — names one stretch of the page's Markdown as Notion's does
// (seen 2026-11-10, "v1" and "v2" alike). Text on both sides of its first
// ellipsis ("..." or "…") is "start...end": a stretch from an occurrence of
// start to an occurrence of end at or after it. Any other selection is its
// own start and end, so "gamma..." and "...gamma", with a side left empty,
// are text whose ellipsis stands for the rest of a word. Notion counts every
// pair of a start and an end at or after it, and only one pair is a
// selection.
//
// Its sides are found as written first. When that finds no pair, they are
// found by their words: letters and digits, in any case, whatever stands
// between them on the page — "al pha" finds "alpha", "foo bar" "foo-bar" and
// "one beta" "one" and "beta" on two lines — and each stretch found is
// widened to whole words, the words it names counted once. What a side holds
// before its first word takes as many characters before the words, a line
// break too; what it holds after its last takes as many after them, on the
// same line. An ellipsis at a side's start takes all that stands before its
// words back to the word before them, and at its end all that follows them
// on their line. A selection found by its words that names more than one
// stretch is refused as matching more than one place, as is one found as
// written.

// notionMarkdownStretch is where a selection's side stands in the page's
// Markdown, from start to end.
type notionMarkdownStretch struct{ start, end int }

// notionMarkdownSelection finds the one stretch of content a selection
// names, or says why it can't.
func notionMarkdownSelection(content, selection string) (int, int, error) {
	// A stored file's address is the page's own for its block and file
	// (notion_page_markdown_file_matches.go).
	selection = notionMarkdownFileAddressesRebased(content, selection)
	startText, endText, ranged := notionMarkdownCutEllipsis(selection)
	if !ranged || startText == "" || endText == "" {
		startText, endText = selection, selection
	}
	pattern := "<pattern>" + startText + "..." + endText + "</pattern> in the page as it is now: <target>" + content + "</target>."
	pairs, found := notionMarkdownPairs(content, startText, endText, notionMarkdownWrittenStretches)
	loose := pairs == 0
	if loose {
		pairs, found = notionMarkdownPairs(content, startText, endText, notionMarkdownWordStretches)
	}
	switch {
	case pairs == 0:
		return 0, 0, pageMarkdownMatchError{
			message:  "No match for " + pattern,
			template: "No match for <pattern>{pattern}</pattern> in the page as it is now: <target>{target}</target>.",
		}
	case pairs > 1:
		said := "More than one place matches: "
		return 0, 0, pageMarkdownMatchError{
			message: said + strconv.Itoa(pairs) + " places for " + pattern,
			template: said + "{count} places for <pattern>{pattern}</pattern> in the page " +
				"as it is now: <target>{target}</target>.",
		}
	}
	return found.start, found.end, nil
}

// notionMarkdownCutEllipsis cuts text at its first ellipsis, "..." or "…".
func notionMarkdownCutEllipsis(text string) (string, string, bool) {
	at, width := strings.Index(text, "..."), len("...")
	if other := strings.Index(text, "…"); other >= 0 && (at < 0 || other < at) {
		at, width = other, len("…")
	}
	if at < 0 {
		return text, "", false
	}
	return text[:at], text[at+width:], true
}

// notionMarkdownPairs counts the pairs of a start's stretch and an end's at
// or after it, as find finds them, and gives the last pair's stretch.
func notionMarkdownPairs(
	content, startText, endText string, find func(content, text string) []notionMarkdownStretch,
) (int, notionMarkdownStretch) {
	starts := find(content, startText)
	ends := starts
	if endText != startText {
		ends = find(content, endText)
	}
	// Both lists stand in order, so the ends at or after each start are the
	// last ones, counted without pairing them one by one.
	pairs, found, first := 0, notionMarkdownStretch{}, 0
	for _, from := range starts {
		for first < len(ends) && ends[first].start < from.start {
			first++
		}
		if after := len(ends) - first; after > 0 {
			pairs += after
			found = notionMarkdownStretch{start: from.start, end: max(ends[first].end, from.end)}
		}
	}
	return pairs, found
}

// notionMarkdownWrittenStretches are where text stands in content as
// written, overlapping ones too.
func notionMarkdownWrittenStretches(content, text string) []notionMarkdownStretch {
	found := []notionMarkdownStretch{}
	if text == "" {
		return found
	}
	for from := 0; from <= len(content)-len(text); {
		at := strings.Index(content[from:], text)
		if at < 0 {
			break
		}
		found = append(found, notionMarkdownStretch{start: from + at, end: from + at + len(text)})
		from += at + 1
	}
	return found
}

// notionMarkdownWordRune is a letter or digit, what a selection's words are
// made of.
func notionMarkdownWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsNumber(r) || unicode.Is(unicode.M, r)
}

// notionMarkdownWordStretches are where text's words stand in content, each
// widened to whole words and what text holds around them.
func notionMarkdownWordStretches(content, text string) []notionMarkdownStretch {
	leadingEllipsis, trailingEllipsis := false, false
	for _, ellipsis := range []string{"...", "…"} {
		if rest, cut := strings.CutPrefix(text, ellipsis); cut {
			text, leadingEllipsis = rest, true
			break
		}
	}
	for _, ellipsis := range []string{"...", "…"} {
		if rest, cut := strings.CutSuffix(text, ellipsis); cut {
			text, trailingEllipsis = rest, true
			break
		}
	}
	before, after := 0, 0
	wanted := strings.Builder{}
	for _, r := range text {
		switch {
		case notionMarkdownWordRune(r):
			wanted.WriteRune(unicode.ToLower(r))
			after = 0
		case wanted.Len() == 0:
			before++
		default:
			after++
		}
	}
	found := []notionMarkdownStretch{}
	if wanted.Len() == 0 {
		return found
	}
	// The page's words, folded, and where each of their letters stands.
	folded, foldedAt, contentAt := strings.Builder{}, []int{}, []int{}
	for index, r := range content {
		if notionMarkdownWordRune(r) {
			foldedAt, contentAt = append(foldedAt, folded.Len()), append(contentAt, index)
			folded.WriteRune(unicode.ToLower(r))
		}
	}
	words, needle := folded.String(), wanted.String()
	for from := 0; from < len(words); {
		at := strings.Index(words[from:], needle)
		if at < 0 {
			break
		}
		first := sort.SearchInts(foldedAt, from+at)
		last := sort.SearchInts(foldedAt, from+at+len(needle)) - 1
		_, lastSize := utf8.DecodeRuneInString(content[contentAt[last]:])
		stretch := notionMarkdownWidenedStretch(content, contentAt[first], contentAt[last]+lastSize,
			before, after, leadingEllipsis, trailingEllipsis)
		if count := len(found); count == 0 || found[count-1] != stretch {
			found = append(found, stretch)
		}
		_, size := utf8.DecodeRuneInString(words[from+at:])
		from += at + size
	}
	return found
}

// notionMarkdownWidenedStretch widens the words found from start to end to
// whole words, then takes what stands around them as the selection says:
// before characters before them (all to the word before after an ellipsis)
// and after characters after them on their line (all after an ellipsis).
func notionMarkdownWidenedStretch(content string, start, end, before, after int, leadingEllipsis, trailingEllipsis bool) notionMarkdownStretch {
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(content[:start])
		if !notionMarkdownWordRune(r) {
			break
		}
		start -= size
	}
	for end < len(content) {
		r, size := utf8.DecodeRuneInString(content[end:])
		if !notionMarkdownWordRune(r) {
			break
		}
		end += size
	}
	for taken := 0; start > 0 && (leadingEllipsis || taken < before); taken++ {
		r, size := utf8.DecodeLastRuneInString(content[:start])
		if notionMarkdownWordRune(r) {
			break
		}
		start -= size
	}
	for taken := 0; end < len(content) && (trailingEllipsis || taken < after); taken++ {
		r, size := utf8.DecodeRuneInString(content[end:])
		if notionMarkdownWordRune(r) || r == '\n' {
			break
		}
		end += size
	}
	return notionMarkdownStretch{start: start, end: end}
}
