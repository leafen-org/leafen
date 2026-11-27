package app

import (
	"errors"
	"math"
)

// A numbered list item may carry a start number and a marker format of its
// own, as Notion's does (seen 2026-11-10). One with a start counts anew from
// it wherever it stands; one without counts on from the item before it, or
// from 1, and one without a format takes the item before it's, or its
// depth's (web/src/lib/numberedList.ts). Notion's public API reads each
// item's own values and takes neither in a request; Notion's import brings
// them over through the block normalization. A start is any whole number
// from 1 a JavaScript number holds exactly, as Notion's page Markdown takes
// one (seen 2026-11-11: <ol start="100000000000"> and 9007199254740991 kept,
// 9007199254740993 refused).

const (
	numberedListFormatNumbers = "numbers"
	numberedListFormatLetters = "letters"
	numberedListFormatRoman   = "roman"
	maxNumberedListStartIndex = 1<<53 - 1
)

func validNumberedListFormat(value string) bool {
	return value == numberedListFormatNumbers || value == numberedListFormatLetters || value == numberedListFormatRoman
}

// validateNumberedListContent checks a numbered list item's stored start and
// format.
func validateNumberedListContent(content map[string]any) error {
	if raw, present := content["listStartIndex"]; present {
		number, ok := raw.(float64)
		if !ok || number != math.Trunc(number) || number < 1 || number > maxNumberedListStartIndex {
			return graphInputError(errorCodeGraphInvalid, "Check the numbered list start number.")
		}
	}
	if raw, present := content["listFormat"]; present {
		format, ok := raw.(string)
		if !ok || !validNumberedListFormat(format) {
			return graphInputError(errorCodeGraphInvalid, "Check the numbered list format.")
		}
	}
	return nil
}

// normalizeNotionNumberedListRequest moves the list_start_index and
// list_format an imported Notion block reads with into the native content;
// null clears a value. The public API's request shape takes neither.
func normalizeNotionNumberedListRequest(incoming, current map[string]any) error {
	if raw, specified := incoming["list_start_index"]; specified {
		delete(incoming, "list_start_index")
		if raw == nil {
			delete(current, "listStartIndex")
		} else {
			number, ok := raw.(float64)
			if !ok || number != math.Trunc(number) || number < 1 || number > maxNumberedListStartIndex {
				return errors.New("list_start_index must be a positive integer")
			}
			current["listStartIndex"] = number
		}
	}
	if raw, specified := incoming["list_format"]; specified {
		delete(incoming, "list_format")
		if raw == nil {
			delete(current, "listFormat")
		} else {
			format, ok := raw.(string)
			if !ok || !validNumberedListFormat(format) {
				return errors.New("list_format must be numbers, letters or roman")
			}
			current["listFormat"] = format
		}
	}
	return nil
}

// notionNumberedListFields adds a numbered item's own start and format to
// its official payload, each only when the item has it, as Notion's API reads
// them.
func notionNumberedListFields(blockType string, content, payload map[string]any) {
	if blockType != graphBlockTypeNumberedListItem {
		return
	}
	if start, ok := content["listStartIndex"].(float64); ok {
		payload["list_start_index"] = int(start)
	}
	if format, ok := content["listFormat"].(string); ok && validNumberedListFormat(format) {
		payload["list_format"] = format
	}
}
