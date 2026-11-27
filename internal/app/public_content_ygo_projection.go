package app

import (
	"errors"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A block keeps its text in the page's collaborative document. A public page
// shows each block with that live text, as plain text and as rich text, so
// its formatting and mentions show as they do in the app. A paragraph always
// reads it; another block reads it once it holds any, and keeps the text its
// content holds before then.

type publicBlockTextProjection struct {
	index     int
	id        string
	paragraph bool
}

func projectPublicBlockText(
	app core.App,
	pageID string,
	items []graphReadItem,
) ([]graphReadItem, error) {
	blocks := publicBlockTextProjections(items)
	if len(blocks) == 0 {
		return items, nil
	}

	document, _, err := loadYgoDoc(app, pageID)
	if err != nil {
		return nil, err
	}

	projected := append([]graphReadItem(nil), items...)
	for _, block := range blocks {
		text := document.GetText(ygoBlockTextRootPrefix + block.id)
		if text.Len() == 0 && !block.paragraph {
			continue
		}
		value, err := publicBlockValueWithText(items[block.index].Value, graphTextSpansAsTheyStand(graphTextSpansFromYText(text), false))
		if err != nil {
			return nil, err
		}
		projected[block.index].Value = value
	}
	return projected, nil
}

func publicBlockTextProjections(items []graphReadItem) []publicBlockTextProjection {
	blocks := make([]publicBlockTextProjection, 0, len(items))
	for index, item := range items {
		if item.Kind != graphReadKindBlock {
			continue
		}
		id, ok := item.Value["id"].(string)
		blockType, _ := item.Value["type"].(string)
		if !ok || id == "" || graphBlockWordless(blockType) {
			continue
		}
		blocks = append(blocks, publicBlockTextProjection{
			index: index, id: id, paragraph: item.Value["type"] == graphBlockTypeParagraph,
		})
	}
	return blocks
}

func publicBlockValueWithText(value map[string]any, spans []graphTextSpan) (map[string]any, error) {
	content, ok := value["content"].(map[string]any)
	if !ok {
		return nil, errors.New(errorCodeGraphReadFailed)
	}
	projectedContent := make(map[string]any, len(content)+2)
	for key, item := range content {
		projectedContent[key] = item
	}
	rich := make([]any, 0, len(spans))
	var text strings.Builder
	for _, span := range spans {
		rich = append(rich, publicTextSpanValue(span))
		text.WriteString(span.Text)
	}
	projectedContent["text"] = text.String()
	projectedContent["rich"] = rich

	projectedValue := make(map[string]any, len(value))
	for key, item := range value {
		projectedValue[key] = item
	}
	projectedValue["content"] = projectedContent
	return projectedValue, nil
}

// publicTextSpanValue writes a span in the form block content holds it.
func publicTextSpanValue(span graphTextSpan) map[string]any {
	value := map[string]any{"text": span.Text}
	for key, flag := range map[string]bool{
		"bold": span.Bold, "italic": span.Italic, "underline": span.Underline,
		"strikethrough": span.Strikethrough, "code": span.Code, "dateStartUnset": span.DateStartUnset,
	} {
		if flag {
			value[key] = true
		}
	}
	for key, text := range map[string]string{
		"color": span.Color, "background": span.Background, "link": span.Link, "commentId": span.CommentID, "mention": span.Mention,
		"pageId": span.PageID, "blockId": span.BlockID, "databaseId": span.DatabaseID, "customEmojiId": span.CustomEmojiID,
		"date": span.Date, "dateEnd": span.DateEnd, "dateTimeZone": span.DateTimeZone,
		"dateReminder": span.DateReminder, "dateFormat": span.DateFormat, "dateTimeFormat": span.DateTimeFormat, "userId": span.UserID, "iconUrl": span.IconURL,
		"provider": span.Provider, "description": span.Description, "imageUrl": span.ImageURL, "equation": span.Equation,
		"emptyPiece": span.EmptyPiece,
	} {
		if text != "" {
			value[key] = text
		}
	}
	return value
}
