package app

import (
	"encoding/json"
	"errors"
)

// notionCommentAnnotations are the annotations a comment's rich text takes.
var notionCommentAnnotations = map[string]bool{
	"bold": true, "italic": true, "strikethrough": true, "underline": true, "code": true, "color": true,
}

// notionCommentGraphSpans reads a comment's rich text, which its request
// shape has checked (notionCommentRichTextShape), as Notion takes it in a
// comment (seen 2026-11-11 with a test integration allowed to insert
// comments): none at all is a comment of no words; an annotation it doesn't
// know is said by name, the first as sent; an equation may be any
// expression, an empty one too; a mention or a link it refuses is said as
// Notion says it (notionMentionRefusal, said by notionCommentAPIError).
func notionCommentGraphSpans(items []json.RawMessage) ([]graphTextSpan, error) {
	spans := make([]graphTextSpan, 0, len(items))
	for _, raw := range items {
		value, err := parseNotionValue(raw)
		if err != nil || value.kind != 'o' {
			return nil, notionCommentRichTextRefused()
		}
		if annotations := value.field("annotations"); annotations != nil && annotations.kind == 'o' {
			for _, key := range annotations.keys {
				if !notionCommentAnnotations[key] {
					return nil, notionInvalid(errCommentInvalid, "Unrecognized annotation type: "+key)
				}
			}
		}
		item := map[string]any{}
		if json.Unmarshal(raw, &item) != nil {
			return nil, notionCommentRichTextRefused()
		}
		// An equation is read with a stand-in expression, which the block
		// text it is read as would hold to a block's bounds, then given its
		// own.
		expression, isEquation := notionCommentEquation(value)
		if isEquation {
			item["equation"] = map[string]any{"expression": "x"}
		}
		read, err := notionTextRequestGraphSpans([]any{item})
		if refusal := (&notionMentionRefusal{}); errors.As(err, &refusal) {
			return nil, refusal
		}
		if err != nil || len(read) != 1 {
			return nil, notionCommentRichTextRefused()
		}
		if isEquation {
			read[0].Text, read[0].Equation = expression, expression
		}
		spans = append(spans, read[0])
	}
	return spans, nil
}

// notionCommentEquation is an item's expression when the item is an
// equation: said so by its type, or holding an equation and nothing else.
func notionCommentEquation(item *notionValue) (string, bool) {
	equation := item.field("equation")
	if equation == nil || equation.kind != 'o' {
		return "", false
	}
	kind := item.field("type")
	if kind != nil && (kind.kind != 's' || kind.text != "equation") ||
		kind == nil && (item.field("text") != nil || item.field("mention") != nil) {
		return "", false
	}
	expression := equation.field("expression")
	if expression == nil || expression.kind != 's' {
		return "", false
	}
	return expression.text, true
}

func notionCommentRichTextRefused() error {
	return notionInvalid(errCommentInvalid, "rich_text must hold rich text objects of a kind comments take.")
}
