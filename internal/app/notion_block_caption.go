package app

import (
	"bytes"
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A block caption is rich text kept in the block's own content, next to its
// url or file. A mention in it keeps its target's ID and the title it was
// written with. The public API checks and titles each reference when a
// mutation is prepared, and a reader sees the caption only after each
// reference is checked again for them, as block text is.

// notionBlockCaptionSpans reads a block's stored caption whole, however long:
// its bound is on what is written (graphRichTextFits), and Notion's API
// reads a caption its app made longer (seen 2026-11-10: 300,001 characters).
func notionBlockCaptionSpans(content map[string]any) ([]graphTextSpan, bool) {
	value, present := content["caption"]
	if !present {
		return []graphTextSpan{}, true
	}
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	spans := make([]graphTextSpan, 0, len(items))
	for _, raw := range items {
		if _, ok := raw.(map[string]any); !ok {
			return nil, false
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, false
		}
		span := graphTextSpan{}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&span) != nil {
			return nil, false
		}
		spans = append(spans, span)
	}
	for _, span := range spans {
		if !validGraphContentTextSpan(span) {
			return nil, false
		}
	}
	return spans, true
}

// notionBlockCaption projects a stored caption without a reader, which is
// enough to tell whether a block is supported. A mention stands in as its
// written text; projectNotionReaderCaption replaces the caption of every
// block a reader receives.
func notionBlockCaption(content map[string]any) ([]map[string]any, bool) {
	spans, ok := notionBlockCaptionSpans(content)
	if !ok {
		return nil, false
	}
	caption := make([]map[string]any, 0, len(spans))
	for _, span := range spans {
		link := any(nil)
		href := any(nil)
		if span.Link != "" && span.Mention == "" {
			address := notionAPILinkAddress(span.Link)
			link = map[string]any{"url": address}
			href = address
		}
		color := notionSpanColor(span.Color, span.Background)
		itemType := "text"
		itemPayload := map[string]any{"content": span.Text, "link": link}
		if span.Equation != "" {
			itemType = "equation"
			itemPayload = map[string]any{"expression": span.Equation}
		}
		caption = append(caption, map[string]any{
			"type": itemType, itemType: itemPayload,
			"annotations": map[string]any{
				"bold": span.Bold, "italic": span.Italic, "strikethrough": span.Strikethrough,
				"underline": span.Underline, "code": span.Code, "color": color,
			},
			"plain_text": span.Text, "href": href,
		})
	}
	return caption, true
}

// projectNotionReaderCaption gives a projected block the caption its reader
// may see. A caption with a reference the reader can no longer see leaves the
// block unsupported, as block text does.
func projectNotionReaderCaption(
	app core.App,
	authorization notionBlockAuthorization,
	fileIssuer *notionBlockFileCapabilityIssuer,
	blockType string,
	content map[string]any,
	externalType string,
	payload map[string]any,
) (string, map[string]any, error) {
	if _, captioned := payload["caption"]; !captioned {
		return externalType, payload, nil
	}
	spans, ok := notionBlockCaptionSpans(content)
	if !ok {
		externalType, payload = notionUnsupportedBlockPayload(blockType)
		return externalType, payload, nil
	}
	// Its pieces read as written, empty ones too, as Notion's caption reads
	// (seen 2026-11-10: "x"×1500, "y"×1500, "a", "b" read back four).
	caption, supported, err := notionSpansRichText(app, authorization, spans, fileIssuer)
	if err != nil {
		return "", nil, err
	}
	if !supported {
		externalType, payload = notionUnsupportedBlockPayload(blockType)
		return externalType, payload, nil
	}
	payload["caption"] = caption
	return externalType, payload, nil
}

// prepareCaption checks and titles the references in an upserted block's
// caption. A caption of another shape is left to graph validation.
func (preparation *notionMentionPreparation) prepareCaption(raw json.RawMessage) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &fields) != nil {
		return raw, nil
	}
	encoded, present := fields["caption"]
	spans := []graphTextSpan{}
	if !present || json.Unmarshal(encoded, &spans) != nil {
		return raw, nil
	}
	referenced := false
	for _, span := range spans {
		referenced = referenced || span.Mention != ""
	}
	if !referenced {
		return raw, nil
	}
	if err := preparation.prepare(spans); err != nil {
		return nil, err
	}
	var err error
	if fields["caption"], err = json.Marshal(spans); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}
