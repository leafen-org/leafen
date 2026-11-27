package app

// A bulleted list item may carry its list's marker and a quote its size, as
// the block menu's "목록 형식" and "인용 크기" set them (Notion's, seen
// 2026-10-12). A bullet that was a numbered item before keeps that list's
// format dormant, as a numbered item keeps its own.

var bulletedListFormats = map[string]bool{
	"disc": true, "circle": true, "square": true,
	numberedListFormatNumbers: true, numberedListFormatLetters: true, numberedListFormatRoman: true,
}

func validateBlockMarkerContent(blockType string, content map[string]any) error {
	switch blockType {
	case graphBlockTypeBulletedListItem:
		if raw, present := content["listFormat"]; present {
			if format, ok := raw.(string); !ok || !bulletedListFormats[format] {
				return graphInputError(errorCodeGraphInvalid, "Check the bulleted list format.")
			}
		}
	case graphBlockTypeQuote:
		if raw, present := content["quoteSize"]; present && raw != "large" {
			return graphInputError(errorCodeGraphInvalid, "Check the quote size.")
		}
	}
	return nil
}
