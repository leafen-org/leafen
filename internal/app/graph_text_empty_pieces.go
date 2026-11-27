package app

import (
	"unicode/utf16"

	"github.com/reearth/ygo/crdt"
)

// A piece of rich text with no characters, as Notion keeps one (seen
// 2026-11-11): an inline equation with no expression, which "v1" page
// Markdown reads from "$$" in text, or two backticks between $ marks, and a
// text piece with no words, which a code block made from page Markdown with
// nothing in it reads as. A block's collaborative text holds characters, so such a piece
// stands in it as an embed — {"equation": ""} or {"text": ""} — at its place,
// with its formatting; an embed counts one in the text's positions and
// nothing in its plain text. A span stands for one with no text and its kind
// in emptyPiece.
const (
	graphTextEmptyEquation = "equation"
	graphTextEmptyText     = "text"
)

// graphTextEmptyPieceEmbed is the embed that stands for an empty piece of a
// kind in a block's collaborative text.
func graphTextEmptyPieceEmbed(kind string) map[string]any {
	if kind == graphTextEmptyEquation {
		return map[string]any{"equation": ""}
	}
	return map[string]any{"text": ""}
}

// graphTextEmptyPieceKind is the kind of empty piece an insert of a block's
// collaborative text stands for, "" for characters or any other embed.
func graphTextEmptyPieceKind(insert any) string {
	embed, ok := insert.(map[string]any)
	if !ok || len(embed) != 1 {
		return ""
	}
	if value, ok := embed["equation"].(string); ok && value == "" {
		return graphTextEmptyEquation
	}
	if value, ok := embed["text"].(string); ok && value == "" {
		return graphTextEmptyText
	}
	return ""
}

// graphTextEmptyPieceSpan reports whether a span stands for an empty piece.
func graphTextEmptyPieceSpan(span graphTextSpan) bool {
	return span.EmptyPiece == graphTextEmptyEquation || span.EmptyPiece == graphTextEmptyText
}

// graphTextDeltaLength is how many positions an insert of a text takes: its
// UTF-16 units, or one for an embed.
func graphTextDeltaLength(delta crdt.Delta) int {
	if text, ok := delta.Insert.(string); ok {
		return len(utf16.Encode([]rune(text)))
	}
	if delta.Insert == nil {
		return 0
	}
	return 1
}

// graphTextSpanIsEquation reports whether a span is an inline equation, an
// empty one too.
func graphTextSpanIsEquation(span graphTextSpan) bool {
	return span.Equation != "" || span.EmptyPiece == graphTextEmptyEquation
}
