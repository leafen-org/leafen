package app

import (
	"encoding/json"
	"regexp"
	"unicode/utf8"
)

// A copy is named as Notion names one (seen 2026-10-06, a database item's
// ⌘D and a page's "복제"; 2026-11-13 under a test page): "Name" becomes
// "Name (1)" and a copy's copy "Name (1)" becomes "Name (2)", whatever else
// is named so. The number counts when it closes the name after a space or
// alone — "(5)" becomes "(6)", "Name (12345678901)" "Name (12345678902)" —
// written as a count is, from 1 without a leading zero: "Name(1)", "Name
// (0)" and "Name (05)" are names, their copies "… (1)". An untitled page's
// copy stays untitled.
var duplicateCopyNumber = regexp.MustCompile(`(?:^| )\(([1-9][0-9]*)\)$`)

// duplicateCopyTitle is the name of a copy of what is named title, and the
// length of title the copy's name keeps before its number: all of it, or as
// much as a title holds beside the number.
func duplicateCopyTitle(title string) (string, int) {
	if title == "" {
		return "", 0
	}
	base, suffix := title, " (1)"
	if match := duplicateCopyNumber.FindStringSubmatchIndex(title); match != nil {
		base, suffix = title[:match[2]-1], "("+nextCopyNumber(title[match[2]:match[3]])+")"
	}
	// A title as long as a title runs gives up its last words to its number.
	if room := max(0, maxGraphTitleRunes-utf8.RuneCountInString(suffix)); utf8.RuneCountInString(base) > room {
		base = string([]rune(base)[:room])
	}
	return base + suffix, len(base)
}

// nextCopyNumber is the count one past digits, however long it is written.
func nextCopyNumber(digits string) string {
	next := []byte(digits)
	for at := len(next) - 1; at >= 0; at-- {
		if next[at] < '9' {
			next[at]++
			return string(next)
		}
		next[at] = '0'
	}
	return "1" + string(next)
}

// duplicateCopyTitleValue names a database item's copy by its title value,
// keeping the formatting of the part of the name it keeps.
func duplicateCopyTitleValue(value graphRowValue) graphRowValue {
	title := ""
	if json.Unmarshal(value.Value, &title) != nil || title == "" {
		return value
	}
	named, kept := duplicateCopyTitle(title)
	encoded, err := json.Marshal(named)
	if err != nil {
		return value
	}
	value.Value = encoded
	if len(value.Spans) == 0 {
		return value
	}
	value.Spans = respelledCopySpans(value.Spans, named, kept)
	return value
}

// duplicateCopyName is a database copy's name and its formatting, as
// Notion's copy keeps it (seen 2026-11-09: a full-page and an inline
// database's copies are "Name (1)", the database's and its source's name
// keeping the formatting of the words they keep, " (1)" plain).
func duplicateCopyName(title string, spans []graphTextSpan) (string, []graphTextSpan) {
	named, kept := duplicateCopyTitle(title)
	if len(spans) == 0 || named == "" {
		return named, nil
	}
	return named, respelledCopySpans(spans, named, kept)
}

// copiedNameSpans is the formatting a copy keeps for a name copied from one
// to another: all of it for the same name, the words a copy's name keeps for
// that name, none for any other.
func copiedNameSpans(spans []graphTextSpan, from, to string) []graphTextSpan {
	if len(spans) == 0 {
		return nil
	}
	if to == from {
		return spans
	}
	if named, kept := duplicateCopyTitle(from); named == to {
		return respelledCopySpans(spans, named, kept)
	}
	return nil
}

// respelledCopySpans keeps the formatting of the first kept bytes of a name
// and spells the rest of the copy's name plain.
func respelledCopySpans(source []graphTextSpan, named string, kept int) []graphTextSpan {
	spans := make([]graphTextSpan, 0, len(source)+1)
	remaining := kept
	for _, span := range source {
		if remaining <= 0 {
			break
		}
		if len(span.Text) > remaining {
			span.Text = span.Text[:remaining]
		}
		remaining -= len(span.Text)
		spans = append(spans, span)
	}
	return append(spans, graphTextSpan{Text: named[kept:]})
}
