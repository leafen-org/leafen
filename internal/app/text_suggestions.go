package app

import (
	"encoding/json"
	"strings"

	"github.com/reearth/ygo/crdt"
)

// A suggested edit is written into a block's text as the spans it makes,
// each marked with its kind and the comment thread that holds it
// (suggestionId): text suggested in ("insert"), text suggested away
// ("delete"), and text whose formatting is suggested ("format"), which keeps
// the formatting it had before as JSON (suggestionBefore). A replacement's
// spans are both, under one thread, the new words before the old. Readers that show
// a page as it stands leave suggested text out and read the rest as it was,
// as Notion's Public API does; Notion's page Markdown keeps suggested text in
// and reads formatting as it was. Copying, moving and restoring text keep
// the suggestions with it.

const (
	textSuggestionInsert = "insert"
	textSuggestionDelete = "delete"
	textSuggestionFormat = "format"
	// A request, and the suggestion it makes, to suggest text in place of a
	// range of a block's text: the range suggested away and the text
	// suggested in before it, one suggestion (page_suggestion_text.go).
	textSuggestionReplace = "replace"
	// A request to suggest a new block after a block's end.
	textSuggestionBlock = "block"
	// A request to suggest a line break before a block's end, which splits
	// it at once.
	textSuggestionSplit = "split"
	// A request to turn the person's own open suggested block into another
	// kind of text block (page_suggestion_turns.go).
	textSuggestionTurn = "turn"
)

// textSuggestionMarks are the formatting a format suggestion can change.
var textSuggestionMarks = []string{"bold", "italic", "underline", "strikethrough", "code", "color", "background", "link"}

// textSuggestionBefore reads a format suggestion's formatting before it.
func textSuggestionBefore(raw string) map[string]any {
	before := map[string]any{}
	if raw != "" && json.Unmarshal([]byte(raw), &before) != nil {
		return map[string]any{}
	}
	return before
}

// textAttributesAsTheyStand are a text run's attributes without its
// suggestion, and whether its text stands: suggested text stands only when
// keepSuggested.
func textAttributesAsTheyStand(attributes crdt.Attributes, keepSuggested bool) (crdt.Attributes, bool) {
	kind, _ := attributes["suggestion"].(string)
	if kind == "" && attributes["suggestionId"] == nil && attributes["suggestionBefore"] == nil {
		return attributes, true
	}
	if kind == textSuggestionInsert && !keepSuggested {
		return nil, false
	}
	result := make(crdt.Attributes, len(attributes))
	for key, value := range attributes {
		if !strings.HasPrefix(key, "suggestion") {
			result[key] = value
		}
	}
	if kind == textSuggestionFormat {
		before, _ := attributes["suggestionBefore"].(string)
		for mark, value := range textSuggestionBefore(before) {
			if !textSuggestionMark(mark) {
				continue
			}
			if value == nil || value == false || value == "" {
				delete(result, mark)
			} else {
				result[mark] = value
			}
		}
	}
	return result, true
}

func textSuggestionMark(name string) bool {
	for _, mark := range textSuggestionMarks {
		if mark == name {
			return true
		}
	}
	return false
}

// textDeltaAsItStands is a text's delta as it stands, suggested text kept
// only when keepSuggested.
func textDeltaAsItStands(deltas []crdt.Delta, keepSuggested bool) []crdt.Delta {
	result := make([]crdt.Delta, 0, len(deltas))
	for _, delta := range deltas {
		attributes, stands := textAttributesAsTheyStand(delta.Attributes, keepSuggested)
		if !stands {
			continue
		}
		delta.Attributes = attributes
		result = append(result, delta)
	}
	return result
}

// textAsItStands is a text's plain text as it stands.
func textAsItStands(text *crdt.YText) string {
	var result strings.Builder
	for _, delta := range textDeltaAsItStands(text.ToDelta(), false) {
		if content, ok := delta.Insert.(string); ok {
			result.WriteString(content)
		}
	}
	return result.String()
}

// graphTextSpansAsTheyStand are spans as they stand, suggested text kept only
// when keepSuggested.
func graphTextSpansAsTheyStand(spans []graphTextSpan, keepSuggested bool) []graphTextSpan {
	result := make([]graphTextSpan, 0, len(spans))
	for _, span := range spans {
		if span.Suggestion == "" && span.SuggestionID == "" && span.SuggestionBefore == "" {
			result = append(result, span)
			continue
		}
		if span.Suggestion == textSuggestionInsert && !keepSuggested {
			continue
		}
		if span.Suggestion == textSuggestionFormat {
			for mark, value := range textSuggestionBefore(span.SuggestionBefore) {
				active, _ := value.(bool)
				text, _ := value.(string)
				switch mark {
				case "bold":
					span.Bold = active
				case "italic":
					span.Italic = active
				case "underline":
					span.Underline = active
				case "strikethrough":
					span.Strikethrough = active
				case "code":
					span.Code = active
				case "color":
					span.Color = text
				case "background":
					span.Background = text
				case "link":
					span.Link = text
				}
			}
		}
		span.Suggestion, span.SuggestionID, span.SuggestionBefore = "", "", ""
		result = append(result, span)
	}
	return result
}
