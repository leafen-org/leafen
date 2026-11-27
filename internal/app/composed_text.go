package app

import (
	"io"

	"golang.org/x/text/unicode/norm"
)

// Text is stored composed (Unicode NFC): Hangul as its syllables, not as the
// letters spelled one by one (NFD) that macOS file names and some Notion pages
// hold, an accented letter as one character. The two read the same, yet a
// search or a database's SQL comparing them finds nothing, so what comes in —
// a Notion import's every answer, a change's text and values — is composed
// before it is kept, and what reads the same is stored the same.

// composedText is value composed, as it is when it already is.
func composedText(value string) string {
	if norm.NFC.IsNormalString(value) {
		return value
	}
	return norm.NFC.String(value)
}

// composedJSON is encoded JSON with its text composed: what composition
// changes are letters and marks, never the quotes, brackets or escapes
// between them, so the JSON reads as before but for its words.
func composedJSON(encoded []byte) []byte {
	if norm.NFC.IsNormal(encoded) {
		return encoded
	}
	return norm.NFC.Bytes(encoded)
}

// composedReader reads JSON (or any text) composed as it streams.
func composedReader(source io.Reader) io.Reader {
	return norm.NFC.Reader(source)
}

// composedTitle is a page's title and the spans written with it, composed.
func composedTitle(title string, spans *[]graphTextSpan) (string, *[]graphTextSpan) {
	title = composedText(title)
	if spans == nil {
		return title, nil
	}
	composed := make([]graphTextSpan, len(*spans))
	for index, span := range *spans {
		span.Text = composedText(span.Text)
		composed[index] = span
	}
	return title, &composed
}
