package app

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// A title or text cell keeps its plain text as its value, as every reader
// of it reads, and its formatting, links and mentions beside it as spans:
// the same bounded rich text a caption keeps, whose mentions each reader
// checks again. Plain text alone is a cell without formatting.

// normalizeGraphRowValueSpans admits a cell's spans: only a title or text
// cell has them, and together they spell its value. A title's neighbouring
// pieces that read alike are joined and an empty one goes, as Notion keeps a
// page's title (seen 2026-11-10: bold "a", "b", "c", "", "d" reads bold
// "a", "bcd"); a text cell's pieces stay as written.
func normalizeGraphRowValueSpans(valueType string, value json.RawMessage, spans []graphTextSpan) ([]graphTextSpan, error) {
	if valueType == graphValueTypeTitle {
		spans = joinedGraphTextSpans(spans)
	}
	return admitGraphRichTextSpans(valueType, value, spans)
}

// normalizeGraphNameSpans admits a database's name's spans: a title's, its
// pieces kept as written, as Notion keeps a database's name (seen
// 2026-11-10: "w241 pieces" and " db" read back two pieces).
func normalizeGraphNameSpans(value json.RawMessage, spans []graphTextSpan) ([]graphTextSpan, error) {
	return admitGraphRichTextSpans(graphValueTypeTitle, value, spans)
}

// joinedGraphTextSpans joins neighbouring spans that read alike — the same
// marks, link and comment, neither a mention nor an equation — and leaves
// out empty ones.
func joinedGraphTextSpans(spans []graphTextSpan) []graphTextSpan {
	joined := make([]graphTextSpan, 0, len(spans))
	for _, span := range spans {
		if span.Text == "" {
			continue
		}
		if last := len(joined) - 1; last >= 0 && span.Mention == "" && span.Equation == "" &&
			joined[last].Mention == "" && joined[last].Equation == "" {
			alike := joined[last]
			alike.Text = span.Text
			if alike == span {
				joined[last].Text += span.Text
				continue
			}
		}
		joined = append(joined, span)
	}
	return joined
}

// admitGraphRichTextSpans admits a title's or text's spans that spell its
// value.
func admitGraphRichTextSpans(valueType string, value json.RawMessage, spans []graphTextSpan) ([]graphTextSpan, error) {
	if len(spans) == 0 {
		return nil, nil
	}
	// A cell's text keeps the threads left on it, as Notion's (seen
	// 2026-10-31: a comment on a cell's words marks them); a caption keeps none.
	unmarked := make([]graphTextSpan, len(spans))
	for index, span := range spans {
		if span.CommentID != "" && !validCommentIdentifier(span.CommentID) {
			return nil, graphInputError(errorCodeGraphInvalid, "Check the formatted text value.")
		}
		span.CommentID = ""
		unmarked[index] = span
	}
	if valueType != graphValueTypeTitle && valueType != graphValueTypeRichText ||
		valueType == graphValueTypeTitle && !validGraphTitleSpans(unmarked) ||
		valueType == graphValueTypeRichText && !validGraphBlockCaption(unmarked) {
		return nil, graphInputError(errorCodeGraphInvalid, "Check the formatted text value.")
	}
	var text string
	if json.Unmarshal(value, &text) != nil {
		return nil, graphInputError(errorCodeGraphInvalid, "Check the formatted text value.")
	}
	var spelled strings.Builder
	formatted := false
	for _, span := range spans {
		spelled.WriteString(span.Text)
		formatted = formatted || span != graphTextSpan{Text: span.Text}
	}
	if spelled.String() != text {
		return nil, graphInputError(errorCodeGraphInvalid, "The formatted text doesn't match the text value.")
	}
	// One plain piece is the value itself. Plain pieces more than one keep
	// where they fall, as Notion reads a text value, a caption and a
	// database's name and description as written (seen 2026-11-10: 100
	// pieces of 2,000 characters read back 100; "a", "", "b" read back
	// three).
	if !formatted && len(spans) <= 1 {
		return nil, nil
	}
	return spans, nil
}

// validGraphTitleSpans admits a title's spans as a caption's are, for words
// as long as a title's (maxGraphTitleRunes).
func validGraphTitleSpans(spans []graphTextSpan) bool {
	return validGraphRichText(spans, maxGraphTitleRunes)
}

// validGraphRichText admits the spans of a rich text — a title, a text cell,
// a caption, a description — as the same bounded rich text: each span one
// validGraphContentTextSpan admits, within graphRichTextFits.
func validGraphRichText(spans []graphTextSpan, words int) bool {
	for _, span := range spans {
		if !validGraphContentTextSpan(span) {
			return false
		}
	}
	return graphRichTextFits(spans, words)
}

// graphRichTextFits is the one bound of a rich text's size: what Notion's API
// writes in one (seen 2026-11-10 for a text property, a caption, a block's
// text and a description: 100 pieces of 2,000 characters, each linking an
// address of 2,000 characters, are kept whole; a 101st piece is refused). Its
// words run to the given bound (maxGraphTextCharacters, a title's
// maxGraphTitleRunes); its marks, links and mentions take at most twice that
// besides, as a link may be as long as its piece's words.
func graphRichTextFits(spans []graphTextSpan, words int) bool {
	if len(spans) > 100 {
		return false
	}
	spelled, characters := 0, 0
	for _, span := range spans {
		spelled += utf8.RuneCountInString(span.Text)
		characters += graphTextSpanCharacters(span)
	}
	return spelled <= words && characters-spelled <= 2*words
}

// graphTextWithin is text's first characters, at most bound of them.
func graphTextWithin(text string, bound int) string {
	if len(text) <= bound || utf8.RuneCountInString(text) <= bound {
		return text
	}
	return string([]rune(text)[:bound])
}

// databaseStoredSpans reads the spans a projection row keeps.
func databaseStoredSpans(raw string) []graphTextSpan {
	if raw == "" {
		return nil
	}
	spans := []graphTextSpan{}
	if json.Unmarshal([]byte(raw), &spans) != nil || len(spans) == 0 {
		return nil
	}
	return spans
}
