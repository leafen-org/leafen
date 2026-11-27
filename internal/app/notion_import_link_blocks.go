package app

import (
	"encoding/json"
	"errors"
	"strings"
)

// stageNotionLinkBlock stages a bookmark or link preview with its content. A
// link preview stays a link preview; a block without a usable URL is left out
// of an ordinary page, as other unsupported blocks are, and fails a blueprint.
func stageNotionLinkBlock(
	parentSourceID, parentKind string,
	blueprint bool,
	position int,
	block notionProviderObject,
) (stagedNotionSource, bool, error) {
	content := map[string]any{}
	if block.Type == graphBlockTypeLinkPreview {
		content["url"] = strings.TrimSpace(block.LinkPreview.URL)
	} else {
		content["url"] = strings.TrimSpace(block.Bookmark.URL)
		captionSource, err := notionProviderRichTextSourceText(block.Bookmark.Caption, blueprint)
		if err != nil {
			return stagedNotionSource{}, false, err
		}
		caption, err := notionSourceTextGraphSpans(captionSource)
		if err != nil {
			return stagedNotionSource{}, false, err
		}
		if len(caption) > 0 {
			content["caption"] = caption
		}
	}
	if !validGraphExternalFileURL(content["url"].(string)) {
		if blueprint {
			return stagedNotionSource{}, false, errors.New(errorCodeNotionImportProviderFailed)
		}
		return stagedNotionSource{}, false, nil
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return stagedNotionSource{}, false, err
	}
	return stagedNotionSource{
		SourceID: block.ID, Kind: block.Type, ParentSourceID: parentSourceID,
		ParentKind: parentKind, Position: position, Text: string(encoded), Blueprint: blueprint,
	}, true, nil
}
