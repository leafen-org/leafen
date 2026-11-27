package app

import (
	"regexp"
	"slices"
)

// Notion's "v1" page Markdown carries bold, italic and strikethrough across
// the pieces of a block's text (seen 2026-11-10): a mark opens before the
// first of a run of pieces that has it and closes after the last; marks
// opening together open bold, italic, strikethrough, and marks closing
// together close strikethrough, italic, bold — "**a <mention-page …>…
// </mention-page> b**", "**a *b* c**", "~~a **b** c~~", "**a<m>*c*" for a
// bold "a", a bold italic mention and an italic "c". A mention's tag keeps
// its color and underline in a <span> inside the marks. Text with a color,
// an underline or a link, and code, stands whole with its own marks inside,
// the marks around it closed before it and opened again after it, and an
// equation drops them ("**a **$`x^2`$** c**"). The spaces at a piece's
// edges stand inside its marks, as Notion's ("a** b **c"). Where marks
// carried across pieces wouldn't read back as "v1" reads them
// (notionMarkdownVersion1Emphasis) the way each piece's own marks do —
// Notion's "***a** b*" for a bold italic "a" and an italic " b" reads as a
// bold "*a" — each piece keeps its own marks, as notionMarkdownAPISpanText
// writes them. Underscores stand bare, as Notion's "v1" writes
// them, where the text reads back the same so (seen 2026-11-11: "$`x`$_ b",
// "**l**_ b"); otherwise each that would read as emphasis is escaped
// (notionMarkdownEscapedUnderscores).

// notionMarkdownVersion1Piece is a piece of a block's text as "v1" writes
// it: its text, and the marks carried across pieces it has, none for one
// that stands whole.
type notionMarkdownVersion1Piece struct {
	text  string
	marks [3]bool
	whole bool
}

// notionMarkdownVersion1Marks are bold's, italic's and strikethrough's
// marks, in the order they open.
var notionMarkdownVersion1Marks = [3]string{"**", "*", "~~"}

// notionMarkdownVersion1MarkedText writes formatted text as the public API's
// "v1" does, or reports false when it holds a mention the Markdown can't
// write. Code holding a backtick that wouldn't read back so is written as
// its text (notion_markdown_version1_code_spans.go).
func notionMarkdownVersion1MarkedText(spans []graphTextSpan, mentions notionMarkdownMentions) (string, bool) {
	text, written := notionMarkdownVersion1MarkedPieces(spans, mentions)
	if written && !notionMarkdownVersion1CodeReadsBack(text, spans) {
		return notionMarkdownVersion1MarkedPieces(notionMarkdownVersion1BacktickCodeAsText(spans), mentions)
	}
	return text, written
}

func notionMarkdownVersion1MarkedPieces(spans []graphTextSpan, mentions notionMarkdownMentions) (string, bool) {
	escaped, written := notionMarkdownVersion1PiecesText(spans, mentions, notionMarkdownEscapedText)
	if !written {
		return "", false
	}
	bare, _ := notionMarkdownVersion1PiecesText(spans, mentions, func(text string) string {
		return notionMarkdownEscapedTextWith(text, notionMarkdownEscaped)
	})
	return notionMarkdownVersion1Underscores(escaped, bare), true
}

// notionMarkdownVersion1Divider is a line of underscores alone, which "v1"
// reads as a divider.
var notionMarkdownVersion1Divider = regexp.MustCompile(`^[ \t]*(?:_[ \t]*){3,}$`)

// notionMarkdownVersion1Underscores is text with its underscores bare where
// that reads back as it does with them escaped.
func notionMarkdownVersion1Underscores(escaped, bare string) string {
	if escaped == bare || notionMarkdownVersion1Divider.MatchString(bare) || !notionMarkdownVersion1ReadsAlike(bare, escaped) {
		return escaped
	}
	return bare
}

// notionMarkdownVersion1PiecesText writes formatted text with its words
// escaped by escape.
func notionMarkdownVersion1PiecesText(
	spans []graphTextSpan, mentions notionMarkdownMentions, escape func(string) string,
) (string, bool) {
	pieces := make([]notionMarkdownVersion1Piece, 0, len(spans))
	for _, span := range spans {
		if span.Mention != "" {
			tag, text, written := notionMarkdownMentionTag(span, mentions)
			if !written {
				return "", false
			}
			if text == nil {
				if tag != "" {
					pieces = append(pieces, notionMarkdownVersion1Piece{
						text:  notionMarkdownVersion1Span(notionMarkdownDiscussed(tag, mentions.discussionsOf(span)), span),
						marks: notionMarkdownVersion1MarksOf(span),
					})
				}
				continue
			}
			span = *text
		} else {
			span = notionMarkdownReaderLink(span, mentions)
		}
		// An empty piece, as a table cell keeps one, writes nothing and
		// parts no marks (seen 2026-11-11: an italic "a ", "" and an italic
		// " b" are "*a  b*").
		if span.Text == "" && !graphTextSpanIsEquation(span) {
			continue
		}
		pieces = append(pieces, notionMarkdownVersion1TextPiece(span, escape, mentions.discussionsOf(span)))
	}
	each := notionMarkdownVersion1Write(pieces, false)
	carried := notionMarkdownVersion1Write(pieces, true)
	if carried != each && notionMarkdownVersion1ReadsAlike(carried, each) {
		return carried, true
	}
	return each, true
}

func notionMarkdownVersion1MarksOf(span graphTextSpan) [3]bool {
	return [3]bool{span.Bold, span.Italic, span.Strikethrough}
}

// notionMarkdownVersion1TextPiece is a piece of text as "v1" writes it, in
// the discussions its words were left on inside its marks, as Notion's
// ("**<span discussion-urls=…>sierra</span>**", seen 2026-11-22).
func notionMarkdownVersion1TextPiece(span graphTextSpan, escape func(string) string, discussions string) notionMarkdownVersion1Piece {
	switch {
	case graphTextSpanIsEquation(span):
		span.Bold, span.Italic, span.Strikethrough = false, false, false
		return notionMarkdownVersion1Piece{text: notionMarkdownAPISpanText(span, discussions), whole: true}
	case span.Code, span.Underline, notionMarkdownShownColor(span) != "", notionMarkdownLinkTarget(span.Link) != "":
		return notionMarkdownVersion1Piece{text: notionMarkdownAPISpanText(span, discussions), whole: true}
	}
	// An address in unlinked text is escaped so that it reads back
	// unlinked, as notionMarkdownAPISpanText escapes it.
	return notionMarkdownVersion1Piece{
		text: notionMarkdownDiscussed(notionMarkdownUnlinkedText(span.Text, escape), discussions), marks: notionMarkdownVersion1MarksOf(span),
	}
}

// notionMarkdownVersion1Write writes the pieces with their marks carried
// across them, or each with its own.
func notionMarkdownVersion1Write(pieces []notionMarkdownVersion1Piece, carry bool) string {
	var out []byte
	var open [3]bool
	// closing closes the open marks keep hasn't.
	closing := func(keep [3]bool) {
		for mark := len(open) - 1; mark >= 0; mark-- {
			if open[mark] && !keep[mark] {
				out = append(out, notionMarkdownVersion1Marks[mark]...)
				open[mark] = false
			}
		}
	}
	for _, piece := range pieces {
		if piece.whole {
			closing([3]bool{})
			out = append(out, piece.text...)
			continue
		}
		closing(piece.marks)
		for mark, marked := range piece.marks {
			if marked && !open[mark] {
				out = append(out, notionMarkdownVersion1Marks[mark]...)
				open[mark] = true
			}
		}
		out = append(out, piece.text...)
		if !carry {
			closing([3]bool{})
		}
	}
	closing([3]bool{})
	return string(out)
}

// notionMarkdownVersion1ReadsAlike reports whether two ways of writing a
// block's text read back as the same text and marks.
func notionMarkdownVersion1ReadsAlike(first, second string) bool {
	return slices.Equal(notionMarkdownVersion1Read(first), notionMarkdownVersion1Read(second))
}

// notionMarkdownVersion1Atom is a character of text read back, or a
// mention or an equation, with its marks.
type notionMarkdownVersion1Atom struct {
	character rune
	span      graphTextSpan
}

func notionMarkdownVersion1Read(text string) []notionMarkdownVersion1Atom {
	plain, spans := notionMarkdownVersionInlineSpans(text, true)
	if spans == nil {
		spans = []graphTextSpan{{Text: plain}}
	}
	atoms := []notionMarkdownVersion1Atom{}
	for _, span := range spans {
		text := span.Text
		span.Text = ""
		if span.Mention != "" || graphTextSpanIsEquation(span) {
			atoms = append(atoms, notionMarkdownVersion1Atom{span: span})
			continue
		}
		for _, character := range text {
			atoms = append(atoms, notionMarkdownVersion1Atom{character: character, span: span})
		}
	}
	return atoms
}
