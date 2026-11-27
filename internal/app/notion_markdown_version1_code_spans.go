package app

import (
	"slices"
	"strings"
)

// Inline code in the public API's "v1" Markdown. Notion's "v1" writes code
// between single backticks, a backslash before each backtick it holds and
// its backslashes as they are ("a`b" is "`a\`b`", "a\`b" "`a\\`b`", "a\"
// "`a\`"), and reads code to the next backtick, a backslash before it as
// written, two or more backticks opening no longer code (seen 2026-11-14):
// it reads its own "`a\`b`" back as the code "a\" and the text "b`".
//
// Leafen writes code as Notion's "v1" does and reads it back the same:
// a backslash before a backtick in code is that backtick, unless
// the code can close there with the text after it read whole, each backtick
// there opening code something closes ("`C:\` and `D:\`" is the code "C:\",
// " and " and the code "D:\", as in Notion). Code nothing closes so ends at
// the next backtick, as Notion's.

// notionMarkdownVersion1CodeSpan reads the code a backtick at index opens in
// "v1": its text and the byte after the backtick that closes it, or false
// when none does.
func notionMarkdownVersion1CodeSpan(value string, index int) (string, int, bool) {
	reader := notionMarkdownVersion1CodeReader{value: value, closes: map[int]int{}, whole: map[int]bool{}}
	if end := reader.close(index); end >= 0 {
		return strings.ReplaceAll(value[index+1:end], "\\`", "`"), end + 1, true
	}
	end := strings.IndexByte(value[index+1:], '`')
	if end < 0 {
		return "", index, false
	}
	return value[index+1 : index+1+end], index + end + 2, true
}

// workspaceImportCodeSpan reads the code a backtick at index opens: in "v1"
// as notionMarkdownVersion1CodeSpan reads it, two backticks together code of
// nothing, and elsewhere to the next backtick, two together none.
func workspaceImportCodeSpan(value string, index int, version1 bool) (string, int, bool) {
	if version1 {
		return notionMarkdownVersion1CodeSpan(value, index)
	}
	end := strings.IndexByte(value[index+1:], '`')
	if end <= 0 {
		return "", index, false
	}
	return value[index+1 : index+1+end], index + end + 2, true
}

// notionMarkdownVersion1CodeReader finds where "v1" code closes in a text,
// keeping what it has found.
type notionMarkdownVersion1CodeReader struct {
	value string
	// closes is the closing backtick of the code each opens, -1 for none.
	closes map[int]int
	// whole is whether the text from a byte on reads whole.
	whole map[int]bool
}

// close is the backtick that closes the code one at index opens, read back
// as Leafen writes it, or -1.
func (reader *notionMarkdownVersion1CodeReader) close(index int) int {
	if end, known := reader.closes[index]; known {
		return end
	}
	end := -1
	for at := index + 1; at < len(reader.value); at++ {
		if reader.value[at] != '`' {
			continue
		}
		if reader.value[at-1] != '\\' || reader.wholeFrom(at+1) {
			end = at
			break
		}
	}
	reader.closes[index] = end
	return end
}

// wholeFrom reports whether the text from a byte on reads whole: a
// backslash before a character that character, and each backtick opening
// code that closes.
func (reader *notionMarkdownVersion1CodeReader) wholeFrom(from int) bool {
	if whole, known := reader.whole[from]; known {
		return whole
	}
	whole := true
	for at := from; at < len(reader.value) && whole; at++ {
		switch reader.value[at] {
		case '\\':
			at++
		case '`':
			end := reader.close(at)
			whole = end >= 0
			at = end
		}
	}
	reader.whole[from] = whole
	return whole
}

// A block's text whose code holding a backtick would read back otherwise —
// beside code ending in a backslash, where both readings close ("`a\`b`
// x\` `c\``") — writes that code as its text, escaped, which reads back as
// that text.

// notionMarkdownVersion1CodeReadsBack reports whether a block's text written
// in "v1" reads back as the code its spans hold.
func notionMarkdownVersion1CodeReadsBack(written string, spans []graphTextSpan) bool {
	if !slices.ContainsFunc(spans, notionMarkdownVersion1BacktickCode) {
		return true
	}
	read := []string{}
	open := false
	for _, atom := range notionMarkdownVersion1Read(written) {
		if !atom.span.Code || atom.character == 0 {
			open = false
			continue
		}
		if !open {
			read = append(read, "")
		}
		read[len(read)-1] += string(atom.character)
		open = true
	}
	held := []string{}
	open = false
	for _, span := range spans {
		switch {
		case span.Text == "" && !graphTextSpanIsEquation(span):
		case !span.Code || span.Mention != "" || graphTextSpanIsEquation(span):
			open = false
		case open:
			held[len(held)-1] += span.Text
		default:
			held, open = append(held, span.Text), true
		}
	}
	return slices.Equal(read, held)
}

func notionMarkdownVersion1BacktickCode(span graphTextSpan) bool {
	return span.Code && span.Mention == "" && strings.Contains(span.Text, "`")
}

// notionMarkdownVersion1BacktickCodeAsText is the spans with the code
// holding a backtick made text.
func notionMarkdownVersion1BacktickCodeAsText(spans []graphTextSpan) []graphTextSpan {
	spans = slices.Clone(spans)
	for index := range spans {
		if notionMarkdownVersion1BacktickCode(spans[index]) {
			spans[index].Code = false
		}
	}
	return spans
}
