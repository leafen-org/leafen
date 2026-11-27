package app

import "strings"

func notionBlockPayload(
	blockType string,
	content map[string]any,
	plainText string,
) (string, map[string]any) {
	return notionBlockPayloadWithRichText(
		blockType, content, plainText, notionPageTitleRichText(plainText),
	)
}

func notionBlockPayloadWithRichText(
	blockType string,
	content map[string]any,
	plainText string,
	richText []map[string]any,
) (string, map[string]any) {
	color := notionBlockColor(content)
	switch blockType {
	case "paragraph":
		return blockType, map[string]any{
			"rich_text": richText, "color": color, "icon": notionBlockIcon(content),
		}
	case "heading_1", "heading_2", "heading_3", "heading_4":
		return blockType, map[string]any{"rich_text": richText, "color": color, "is_toggleable": false}
	case "bulleted_list_item", "numbered_list_item", "quote", "toggle":
		return blockType, map[string]any{"rich_text": richText, "color": color}
	case "toggle_heading_1", "toggle_heading_2", "toggle_heading_3", "toggle_heading_4":
		externalType := strings.TrimPrefix(blockType, "toggle_")
		return externalType, map[string]any{"rich_text": richText, "color": color, "is_toggleable": true}
	case "to_do":
		checked, _ := content["checked"].(bool)
		return blockType, map[string]any{"rich_text": richText, "checked": checked, "color": color}
	case graphBlockTypeTemplate:
		return blockType, map[string]any{"rich_text": richText}
	case "callout":
		return blockType, map[string]any{
			"rich_text": richText, "icon": notionCalloutIcon(content), "color": color,
		}
	case "code":
		language, _ := content["language"].(string)
		language = strings.TrimSpace(language)
		if language == "" {
			language = "plain text"
		} else if !validNotionCodeLanguage(language) {
			return notionUnsupportedBlockPayload(blockType)
		}
		caption, ok := notionBlockCaption(content)
		if !ok {
			return notionUnsupportedBlockPayload(blockType)
		}
		return blockType, map[string]any{"rich_text": richText, "caption": caption, "language": canonicalNotionCodeLanguage(language)}
	case "equation":
		expression, _ := content["expression"].(string)
		if expression == "" {
			expression = plainText
		}
		return blockType, map[string]any{"expression": expression}
	case "divider", "breadcrumb", "column_list":
		return blockType, map[string]any{}
	case graphBlockTypeTable:
		width, widthOK := notionIntegerContentValue(content["tableWidth"])
		columnHeader, columnOK := content["hasColumnHeader"].(bool)
		rowHeader, rowOK := content["hasRowHeader"].(bool)
		if !widthOK || !columnOK || !rowOK || width < 0 || width > maxGraphTableWidth {
			return notionUnsupportedBlockPayload(blockType)
		}
		// One without width, as Notion's Markdown makes a table without
		// rows, reads as null (seen 2026-11-14).
		var tableWidth any = width
		if width == 0 {
			tableWidth = nil
		}
		return blockType, map[string]any{
			"table_width": tableWidth, "has_column_header": columnHeader, "has_row_header": rowHeader,
		}
	case "table_of_contents":
		return blockType, map[string]any{"color": color}
	case "column":
		if payload, ok := notionColumnPayload(content); ok {
			return blockType, payload
		}
		return notionUnsupportedBlockPayload(blockType)
	case "synced_block":
		if syncedFrom, ok := content["synced_from"].(map[string]any); ok {
			return blockType, map[string]any{"synced_from": syncedFrom}
		}
		if sourceID, _ := content["syncedBlockId"].(string); strings.TrimSpace(sourceID) != "" {
			return notionUnsupportedBlockPayload(blockType)
		}
		return blockType, map[string]any{"synced_from": nil}
	case "bookmark", "embed":
		// Its address as it was given, any text or none, as Notion's reads a
		// bookmark's or an embed's (seen 2026-11-10).
		url, _ := content["url"].(string)
		caption, ok := notionBlockCaption(content)
		if !ok {
			return notionUnsupportedBlockPayload(blockType)
		}
		return blockType, map[string]any{"url": url, "caption": caption}
	case graphBlockTypeLinkPreview:
		url, _ := content["url"].(string)
		if notionPageExternalFile(url) == nil {
			return notionUnsupportedBlockPayload(blockType)
		}
		return blockType, map[string]any{"url": url}
	case "image", "video", "audio", graphBlockTypePDF, "file":
		// Its address as it was given, any text Notion's API takes
		// (notion_block_media_address.go); one waiting for its file or link
		// has none, an external address of nothing, as Notion's reads an empty
		// one (seen 2026-11-11).
		url, _ := content["url"].(string)
		if _, file := content["fileId"]; url == "" && file {
			return notionUnsupportedBlockPayload(blockType)
		}
		caption, ok := notionBlockCaption(content)
		if !ok {
			return notionUnsupportedBlockPayload(blockType)
		}
		payload := map[string]any{
			"type": "external", "external": map[string]any{"url": url}, "caption": caption,
		}
		if blockType == "file" {
			// A file kept without a name reads with the one its address gives.
			name, _ := content["name"].(string)
			if name == "" {
				name = notionExternalFileName(nil, url)
			}
			payload["name"] = name
		}
		return blockType, payload
	case "child_page", "child_database":
		return notionUnsupportedBlockPayload(blockType)
	case "link_to_page":
		kind, _ := content["type"].(string)
		identifier, _ := content[kind].(string)
		if normalized, ok := normalizeNotionUUIDRequest(identifier); ok &&
			(kind == "page_id" || kind == "database_id" || kind == "comment_id") {
			return blockType, map[string]any{"type": kind, kind: normalized}
		}
		// A link whose target can't be read, or to nothing, reads as Notion's
		// does: by the block's name in Notion (seen 2026-11-10).
		return notionUnsupportedBlockPayload("alias")
	default:
		return notionUnsupportedBlockPayload(blockType)
	}
}

func notionIntegerContentValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case float64:
		return int(typed), typed == float64(int(typed))
	default:
		return 0, false
	}
}

func notionUnsupportedBlockPayload(blockType string) (string, map[string]any) {
	return "unsupported", map[string]any{"block_type": blockType}
}

func notionBlockColor(content map[string]any) string {
	color, _ := content["color"].(string)
	if validNotionBlockColor(color) {
		return color
	}
	return "default"
}

func validNotionBlockColor(color string) bool {
	_, valid := map[string]struct{}{
		"default": {}, "gray": {}, "brown": {}, "orange": {}, "yellow": {},
		"green": {}, "blue": {}, "purple": {}, "pink": {}, "red": {},
		"default_background": {},
		"gray_background":    {}, "brown_background": {}, "orange_background": {},
		"yellow_background": {}, "green_background": {}, "blue_background": {},
		"purple_background": {}, "pink_background": {}, "red_background": {},
	}[color]
	return valid
}

// notionCalloutIcon is a callout's icon: one never set is the light bulb the
// callout shows and its Markdown writes, as Notion's API names it (seen
// 2026-11-10: a callout made without an icon reads 💡); one taken away is none.
func notionCalloutIcon(content map[string]any) any {
	if icon := notionBlockIcon(content); icon != nil {
		return icon
	}
	value, _ := content["icon"].(string)
	emojiID, _ := content["customEmojiId"].(string)
	if strings.TrimSpace(value) != "" || strings.TrimSpace(emojiID) != "" ||
		content["iconType"] == calloutIconNone || content["iconType"] == graphCalloutIconTypeImage {
		return nil
	}
	return map[string]any{"type": "emoji", "emoji": "💡"}
}

func notionBlockIcon(content map[string]any) any {
	if value, ok := content["icon"].(map[string]any); ok {
		return value
	}
	value, _ := content["icon"].(string)
	if content["iconType"] == notionIconTypeExternal && value != "" {
		// Kept as written, a character alone or a space too, as Notion's
		// Markdown keeps a callout's icon (notion_page_markdown_callout.go).
		return map[string]any{"type": "external", "external": map[string]any{"url": value}}
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if content["iconType"] == nativeIconType {
		return nativeIconObject(value)
	}
	// An uploaded picture reads with its address where a reader is given one
	// (notion_block_api.go), and as no icon elsewhere, not as its file's ID.
	if content["iconType"] == graphCalloutIconTypeImage {
		return nil
	}
	if external := notionPageExternalFile(value); external != nil {
		return external
	}
	return map[string]any{"type": "emoji", "emoji": value}
}
