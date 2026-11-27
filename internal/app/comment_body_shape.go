package app

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// A comment's body has one shape — comment-wire's CommentBody: its text as
// "rich" runs, each {"text": …}, with what a comment may also hold — and a
// body of another shape is refused with what is wrong (2026-10-16: an app's
// {"text": …} was stored and showed as an empty comment).

// commentBodyKeys are a body's parts: CommentBody's, and what Leafen itself
// writes too — the "rich_text" mark, a suggestion thread's facts and an
// imported comment's origin.
var commentBodyKeys = map[string]bool{
	"rich": true, "attachmentIds": true, "quote": true, "quoteStart": true, "quoteEnd": true,
	"quoteField": true, "quoteBlocks": true, "propertyId": true, "propertyName": true,
	"type": true, "suggestion": true, "notion": true,
}

// commentBodyShapeError is a body of another shape, and what is wrong.
type commentBodyShapeError struct{ reason string }

func (failure *commentBodyShapeError) Error() string { return "comment body: " + failure.reason }

func (failure *commentBodyShapeError) Is(target error) bool { return target == errCommentInvalid }

// commentBodyProblem says what is wrong with a body's shape, or nil.
func commentBodyProblem(body json.RawMessage) error {
	refuse := func(format string, args ...any) error {
		return &commentBodyShapeError{reason: fmt.Sprintf(format, args...)}
	}
	parts := map[string]json.RawMessage{}
	if json.Unmarshal(body, &parts) != nil {
		return refuse("it is an object, not text or a list")
	}
	for key := range parts {
		if !commentBodyKeys[key] {
			return refuse("%q isn't part of a comment's body", key)
		}
	}
	raw, found := parts["rich"]
	if !found {
		return refuse(`it has no "rich", the list of its text runs`)
	}
	spans := []map[string]any{}
	if json.Unmarshal(raw, &spans) != nil {
		return refuse(`"rich" is a list of runs, each {"text": "…"}`)
	}
	for index, span := range spans {
		if _, text := span["text"].(string); !text {
			return refuse(`"rich"[%d] has no "text"`, index)
		}
	}
	// A quote's other places: its own block's caption, and the blocks the
	// chosen text runs on into (comment_text_anchors.go).
	if raw, found := parts["quoteField"]; found {
		field := ""
		_ = json.Unmarshal(raw, &field)
		if _, _, named := commentQuoteFieldPlace(field); field == "" || !named {
			return refuse(`"quoteField" is "caption", "cell:<n>" for a table row's cell from 0, or left out`)
		}
	}
	if raw, found := parts["quoteBlocks"]; found {
		blocks := []map[string]json.RawMessage{}
		if json.Unmarshal(raw, &blocks) != nil {
			return refuse(`"quoteBlocks" is a list of {"blockId", "start", "end"}`)
		}
		for index, block := range blocks {
			place := commentQuoteBlock{}
			encoded, _ := json.Marshal(block)
			if json.Unmarshal(encoded, &place) != nil || place.BlockID == "" || block["start"] == nil || block["end"] == nil ||
				(place.Field != "" && place.Field != commentQuoteCaption) {
				return refuse(`"quoteBlocks"[%d] is {"blockId", "start", "end"}, with "field": "caption" for a caption`, index)
			}
		}
	}
	return nil
}

// A comment's "notion" part is how a connection wrote it
// (notionStoredCommentMetadata), and the app shows the comment as that
// connection's by it. Only the connection's API writes it: the app's own
// comments can't carry one, so a person's comment never shows as an
// integration's, and an edit keeps the one the comment was written with.

// refuseConnectionCommentPart refuses an app's body that carries one.
func refuseConnectionCommentPart(body json.RawMessage) error {
	parts := map[string]json.RawMessage{}
	if json.Unmarshal(body, &parts) == nil {
		if _, found := parts["notion"]; found {
			return &commentBodyShapeError{reason: `"notion" is written by a connection, not sent`}
		}
	}
	return nil
}

// keepConnectionCommentPart gives an edited body the "notion" part its
// comment was stored with, or none.
func keepConnectionCommentPart(body json.RawMessage, stored string) (json.RawMessage, error) {
	parts := map[string]json.RawMessage{}
	before := map[string]json.RawMessage{}
	if json.Unmarshal(body, &parts) != nil || json.Unmarshal([]byte(stored), &before) != nil {
		return nil, errCommentInvalid
	}
	kept, had := before["notion"]
	if _, has := parts["notion"]; !has && !had {
		return body, nil
	}
	delete(parts, "notion")
	if had {
		parts["notion"] = kept
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(parts); err != nil {
		return nil, errCommentInvalid
	}
	return json.RawMessage(bytes.TrimRight(out.Bytes(), "\n")), nil
}
