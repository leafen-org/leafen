package app

import (
	"encoding/json"
)

// A database's description keeps its formatting, as Notion's does (seen
// 2026-10-30: ⌘B in "설명 추가" bolds the words): the spans spell the text,
// each with its marks and link, as a text value's do. A description without
// formatting keeps none.

// decodeStoredTextSpans reads the stored formatting of a database's name or
// description; none, or any it can't read, is plain text.
func decodeStoredTextSpans(encoded string) []graphTextSpan {
	if encoded == "" {
		return nil
	}
	spans := []graphTextSpan{}
	if json.Unmarshal([]byte(encoded), &spans) != nil {
		return nil
	}
	return spans
}
