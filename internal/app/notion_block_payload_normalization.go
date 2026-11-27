package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Notion's API takes a table row of at most 100 cells (seen 2026-10-13: 101
// is refused), though a table it reads from Markdown is wider.
const notionAPIMaxTableCells = 100

var notionTextMutationBlockTypes = map[string]struct{}{
	"paragraph": {}, "heading_1": {}, "heading_2": {}, "heading_3": {}, "heading_4": {},
	"bulleted_list_item": {}, "numbered_list_item": {}, "quote": {}, "to_do": {},
	"toggle": {}, "callout": {}, "code": {}, "equation": {}, "divider": {},
}

func normalizeNotionBlockMutationPayload(
	blockType string,
	raw json.RawMessage,
	existing *storedNotionBlock,
) (string, map[string]any, *[]graphTextSpan, error) {
	incoming := map[string]any{}
	if json.Unmarshal(raw, &incoming) != nil || incoming == nil {
		return "", nil, nil, errors.New("block content is invalid")
	}
	if _, nested := incoming["children"]; nested {
		return "", nil, nil, errors.New("a block's children are added with append block children, not in its content")
	}
	current, err := notionExistingBlockContent(existing)
	if err != nil {
		return "", nil, nil, err
	}
	if _, supported := notionTextMutationBlockTypes[blockType]; supported {
		blockType, content, spans, err := normalizeNotionTextBlockMutation(blockType, incoming, current, existing)
		if err == nil && existing == nil {
			closeNewToggle(blockType, content)
		}
		return blockType, content, spans, err
	}
	switch blockType {
	case "bookmark":
		return normalizeNotionURLBlockMutation(blockType, incoming, current, existing != nil)
	case "embed":
		return normalizeNotionEmbedMutation(incoming, current, existing != nil)
	case "image", "video", "audio", graphBlockTypePDF, graphBlockTypeFile:
		// A new one's address Notion doesn't take is a fault in its content.
		if fault := notionMediaLinkRequestFault(blockType, incoming, existing == nil); fault != "" {
			return "", nil, nil, fault
		}
		if blockType == graphBlockTypeFile {
			return normalizeNotionExternalFileMutation(incoming, current, existing)
		}
		return normalizeNotionExternalMediaMutation(blockType, incoming, current, existing != nil)
	case "breadcrumb":
		if len(incoming) != 0 {
			return "", nil, nil, errors.New("breadcrumb content must be empty")
		}
		return blockType, map[string]any{}, nil, nil
	case "table_of_contents":
		if err := allowOnlyNotionBlockKeys(incoming, "color"); err != nil {
			return "", nil, nil, err
		}
		if rawColor, specified := incoming["color"]; specified {
			color, ok := rawColor.(string)
			if !ok || !validNotionBlockColor(color) {
				return "", nil, nil, errors.New("block color is invalid")
			}
		}
		for key, value := range incoming {
			current[key] = value
		}
		// One made without a color is gray, as Notion's (seen 2026-11-10);
		// one made from Markdown keeps none, as there.
		if _, specified := incoming["color"]; !specified && existing == nil {
			current["color"] = "gray"
		}
		return blockType, current, nil, nil
	case "link_to_page":
		return normalizeNotionLinkToPageMutation(incoming, current, existing)
	case "synced_block":
		return normalizeNotionSyncedBlockMutation(incoming, existing)
	case graphBlockTypeColumnList, graphBlockTypeColumn:
		return normalizeNotionColumnMutation(blockType, incoming, current, existing)
	case graphBlockTypeTab:
		if len(incoming) != 0 {
			return "", nil, nil, errors.New("tab content must be empty")
		}
		if existing != nil {
			return graphBlockTypeTab, current, nil, nil
		}
		return graphBlockTypeTab, map[string]any{}, nil, nil
	case graphBlockTypeTemplate:
		return normalizeNotionTemplateBlockMutation(incoming, current, existing)
	case graphBlockTypeTable:
		return normalizeNotionTableBlockMutation(incoming, current, existing)
	case graphBlockTypeTableRow:
		return normalizeNotionTableRowBlockMutation(incoming, current, existing)
	case graphBlockTypeLinkPreview:
		// Notion returns link previews but never creates or changes one.
		return "", nil, nil, errors.New("link_preview blocks cannot be created or updated")
	default:
		return "", nil, nil, fmt.Errorf("block type %q is not supported", blockType)
	}
}

func normalizeNotionTableBlockMutation(
	incoming map[string]any,
	current map[string]any,
	existing *storedNotionBlock,
) (string, map[string]any, *[]graphTextSpan, error) {
	if err := allowOnlyNotionBlockKeys(
		incoming, "table_width", "has_column_header", "has_row_header",
	); err != nil {
		return "", nil, nil, err
	}
	// A new table needs its width; its headers are off unless given, as
	// Notion's (seen 2026-10-17: a table without has_row_header was refused).
	if existing == nil {
		if _, specified := incoming["table_width"]; !specified {
			return "", nil, nil, errors.New("table.table_width is required")
		}
		current["hasColumnHeader"], current["hasRowHeader"] = false, false
	}
	if rawWidth, specified := incoming["table_width"]; specified {
		if existing != nil {
			return "", nil, nil, errors.New("table.table_width cannot be updated")
		}
		// A width past a row's most cells is said by its rows, none of
		// which matches it, as Notion's (seen 2026-11-10: 101).
		width, ok := rawWidth.(float64)
		if !ok || width != float64(int(width)) || int(width) < minGraphTableWidth || int(width) > maxGraphTableWidth {
			return "", nil, nil, errors.New("table.table_width is invalid")
		}
		current["tableWidth"] = int(width)
	}
	for _, pair := range []struct{ external, internal string }{
		{"has_column_header", "hasColumnHeader"},
		{"has_row_header", "hasRowHeader"},
	} {
		if raw, specified := incoming[pair.external]; specified {
			value, ok := raw.(bool)
			if !ok {
				return "", nil, nil, errors.New("table." + pair.external + " must be a boolean")
			}
			current[pair.internal] = value
		}
	}
	return graphBlockTypeTable, current, nil, nil
}

func normalizeNotionTableRowBlockMutation(
	incoming map[string]any,
	current map[string]any,
	existing *storedNotionBlock,
) (string, map[string]any, *[]graphTextSpan, error) {
	if err := allowOnlyNotionBlockKeys(incoming, "cells"); err != nil {
		return "", nil, nil, err
	}
	rawCells, specified := incoming["cells"]
	if !specified {
		if existing == nil {
			return "", nil, nil, errors.New("table_row.cells is required")
		}
		return graphBlockTypeTableRow, current, nil, nil
	}
	// No cells is said by the table's width (notionTableRowCellsRefused),
	// as Notion's (seen 2026-11-14).
	cellValues, ok := rawCells.([]any)
	if !ok || len(cellValues) > notionAPIMaxTableCells {
		return "", nil, nil, errors.New("table_row.cells is invalid")
	}
	cells := make([][]graphTextSpan, len(cellValues))
	for index, rawCell := range cellValues {
		spans, err := notionTextRequestGraphSpans(rawCell)
		if refusal := (&notionMentionRefusal{}); errors.As(err, &refusal) {
			return "", nil, nil, refusal
		}
		if err != nil {
			return "", nil, nil, errors.New("table_row.cells is invalid")
		}
		cells[index] = spans
	}
	current["cells"] = cells
	return graphBlockTypeTableRow, current, nil, nil
}

func normalizeNotionTemplateBlockMutation(
	incoming map[string]any,
	current map[string]any,
	existing *storedNotionBlock,
) (string, map[string]any, *[]graphTextSpan, error) {
	if existing == nil {
		return "", nil, nil, errors.New("template blocks cannot be created through the Notion-compatible API")
	}
	if err := allowOnlyNotionBlockKeys(incoming, "rich_text"); err != nil {
		return "", nil, nil, err
	}
	rawRich, specified := incoming["rich_text"]
	if !specified {
		return graphBlockTypeTemplate, current, nil, nil
	}
	spans, err := notionTextRequestGraphSpans(rawRich)
	if err != nil {
		return "", nil, nil, err
	}
	delete(current, "rich_text")
	return graphBlockTypeTemplate, current, &spans, nil
}

func normalizeNotionSyncedBlockMutation(
	incoming map[string]any,
	existing *storedNotionBlock,
) (string, map[string]any, *[]graphTextSpan, error) {
	if err := allowOnlyNotionBlockKeys(incoming, "synced_from"); err != nil {
		return "", nil, nil, err
	}
	rawSource, specified := incoming["synced_from"]
	// A synced block's source is the one it was made with: Notion refuses an update
	// naming another (seen 2026-11-15); the same is left out (notionSyncedBlockUpdateTaken).
	if specified && existing != nil {
		return "", nil, nil, errors.New("synced_from can't be changed.")
	}
	if !specified {
		if existing != nil {
			current, err := notionExistingBlockContent(existing)
			return graphBlockTypeSyncedBlock, current, nil, err
		}
		return "", nil, nil, errors.New("synced_block.synced_from is required")
	}
	if rawSource == nil {
		return graphBlockTypeSyncedBlock, map[string]any{}, nil, nil
	}
	// Notion takes the source with its type or without it.
	source, ok := rawSource.(map[string]any)
	if !ok {
		return "", nil, nil, errors.New("synced_block.synced_from is invalid")
	}
	for key, value := range source {
		if key != "block_id" && (key != "type" || value != "block_id") {
			return "", nil, nil, errors.New("synced_block.synced_from is invalid")
		}
	}
	publicBlockID, _ := source["block_id"].(string)
	blockID, ok := normalizeNotionUUIDRequest(publicBlockID)
	if !ok {
		return "", nil, nil, errors.New("synced_block source block_id is invalid")
	}
	return graphBlockTypeSyncedBlock, map[string]any{"syncedBlockId": blockID}, nil, nil
}

// normalizeNotionLinkToPageMutation takes a link to a page, a database or a
// comment as Notion's API does (seen 2026-11-10): a new one with its type
// given has its target checked as it is written; one without has none and
// may link to nothing (notionLinkTargetUncheckedKey). A link changed keeps
// its target, whatever is sent.
func normalizeNotionLinkToPageMutation(
	incoming map[string]any,
	current map[string]any,
	existing *storedNotionBlock,
) (string, map[string]any, *[]graphTextSpan, error) {
	if err := allowOnlyNotionBlockKeys(incoming, "type", "page_id", "database_id", "comment_id"); err != nil {
		return "", nil, nil, err
	}
	if existing != nil {
		current[notionLinkTargetUncheckedKey] = true
		return graphBlockTypeLinkToPage, current, nil, nil
	}
	// The type, when left out, is the one key given, as Notion takes it.
	kind, typed := incoming["type"].(string)
	for _, key := range []string{"page_id", "database_id", "comment_id"} {
		if _, given := incoming[key]; given && kind == "" {
			kind = key
		}
	}
	link := map[string]any{}
	if !typed {
		link[notionLinkTargetUncheckedKey] = true
	}
	switch kind {
	case "page_id":
		publicPageID, _ := incoming["page_id"].(string)
		// A page made from a block is linked by its own ID, which is its
		// record's; the link's target is checked as it is written.
		pageID, err := resolvePageOwnPublicID(strings.TrimSpace(publicPageID))
		if err != nil {
			normalized, isUUID := normalizeNotionUUIDRequest(publicPageID)
			if typed && !isUUID {
				return "", nil, nil, errors.New(notionUnreachableBlockTarget)
			}
			// A database page is named by its stored public ID, which the
			// link's check finds (notionLinkTargetOf); a link to nothing
			// keeps the ID it was given.
			pageID = normalized
			if isUUID {
				pageID = notionPageMentionRowPrefix + normalized
			}
		}
		link["childPageId"] = pageID
	case "database_id":
		publicDatabaseID, _ := incoming["database_id"].(string)
		databaseID, ok := normalizeNotionUUIDRequest(publicDatabaseID)
		if !ok {
			return "", nil, nil, errors.New("link_to_page.database_id is invalid")
		}
		link["databaseContainerId"] = databaseID
	case "comment_id":
		publicCommentID, _ := incoming["comment_id"].(string)
		commentID, ok := normalizeNotionUUIDRequest(publicCommentID)
		if !ok {
			return "", nil, nil, errors.New("link_to_page.comment_id is invalid")
		}
		link["notionCommentId"] = commentID
	default:
		return "", nil, nil, errors.New("link_to_page type is invalid")
	}
	return graphBlockTypeLinkToPage, link, nil, nil
}

func notionExistingBlockContent(existing *storedNotionBlock) (map[string]any, error) {
	content := map[string]any{}
	if existing == nil {
		return content, nil
	}
	if json.Unmarshal([]byte(existing.Content), &content) != nil || content == nil {
		return nil, errors.New("stored block content is invalid")
	}
	return content, nil
}

func normalizeNotionTextBlockMutation(
	blockType string,
	incoming map[string]any,
	current map[string]any,
	existing *storedNotionBlock,
) (string, map[string]any, *[]graphTextSpan, error) {
	allowed := []string{"rich_text", "color"}
	switch blockType {
	case "paragraph", "callout":
		allowed = append(allowed, "icon")
	case "heading_1", "heading_2", "heading_3", "heading_4":
		allowed = append(allowed, "is_toggleable")
	case "to_do":
		allowed = append(allowed, "checked")
	case graphBlockTypeNumberedListItem:
		allowed = append(allowed, "list_start_index", "list_format")
	case "code":
		allowed = []string{"rich_text", "language", "caption"}
	case "equation":
		allowed = []string{"expression"}
	case "divider":
		allowed = nil
	}
	if err := allowOnlyNotionBlockKeys(incoming, allowed...); err != nil {
		return "", nil, nil, err
	}
	if existing == nil {
		required := "rich_text"
		if blockType == "equation" {
			required = "expression"
		} else if blockType == "divider" {
			required = ""
		}
		if required != "" {
			if _, found := incoming[required]; !found {
				return "", nil, nil, errors.New("required block content is missing")
			}
		}
		if blockType == "code" {
			if _, found := incoming["language"]; !found {
				return "", nil, nil, errors.New("code.language is required")
			}
		}
	}
	if rawColor, specified := incoming["color"]; specified {
		color, ok := rawColor.(string)
		if !ok || !validNotionBlockColor(color) {
			return "", nil, nil, errors.New("block color is invalid")
		}
	}
	if rawIcon, specified := incoming["icon"]; specified {
		if rawIcon == nil && blockType != graphBlockTypeParagraph && blockType != graphBlockTypeCallout {
			return "", nil, nil, errors.New("block icon is invalid")
		}
		clearNotionBlockIcon(current)
		// A callout's icon taken away leaves it none, as Notion's (2026-11-05).
		if rawIcon == nil && blockType == graphBlockTypeCallout {
			current["icon"], current["iconType"] = "", calloutIconNone
		}
		if rawIcon != nil {
			icon, err := notionNativeBlockIconRequest(rawIcon, blockType == "callout")
			if err != nil {
				return "", nil, nil, err
			}
			for key, value := range icon {
				current[key] = value
			}
		}
		delete(incoming, "icon")
	}
	if rawCaption, specified := incoming["caption"]; specified {
		caption, err := notionNativeCaptionRequest(rawCaption)
		if err != nil {
			return "", nil, nil, err
		}
		incoming["caption"] = caption
	}
	if rawLanguage, specified := incoming["language"]; specified {
		language, ok := rawLanguage.(string)
		language = strings.TrimSpace(language)
		if !ok || !validNotionCodeLanguage(language) {
			return "", nil, nil, errors.New("code.language is invalid")
		}
		incoming["language"] = canonicalNotionCodeLanguage(language)
	}
	// An equation's expression is any text, none or long too, as Notion's
	// API takes it (seen 2026-11-10: "", "   ", 20,000 characters).
	if rawExpression, specified := incoming["expression"]; specified {
		expression, ok := rawExpression.(string)
		if !ok {
			return "", nil, nil, errors.New("equation.expression is invalid")
		}
		incoming["expression"] = expression
	}
	if blockType == graphBlockTypeNumberedListItem {
		if err := normalizeNotionNumberedListRequest(incoming, current); err != nil {
			return "", nil, nil, err
		}
	}
	for _, key := range []string{"is_toggleable", "checked"} {
		if raw, specified := incoming[key]; specified {
			if _, ok := raw.(bool); !ok {
				return "", nil, nil, errors.New(key + " must be a boolean")
			}
		}
	}
	for key, value := range incoming {
		current[key] = value
	}
	if strings.HasPrefix(blockType, "heading_") {
		if _, specified := incoming["is_toggleable"]; !specified && existing != nil {
			current["is_toggleable"] = strings.HasPrefix(existing.Type, "toggle_heading_")
		}
		if toggleable, _ := current["is_toggleable"].(bool); toggleable {
			blockType = "toggle_" + blockType
		}
	}
	var textSpans *[]graphTextSpan
	if rawRich, ok := incoming["rich_text"]; ok {
		spans, err := notionTextRequestGraphSpans(rawRich)
		if err != nil {
			return "", nil, nil, err
		}
		textSpans = &spans
	}
	delete(current, "rich_text")
	if blockType == "equation" {
		textSpans = nil
	}
	return blockType, current, textSpans, nil
}

// notionNativeBlockIconRequest is a paragraph's or a callout's icon as the
// block keeps it. An uploaded file is no block's icon in Notion's API (seen
// 2026-11-10: a callout's and a tab's paragraph's, new or changed, whatever
// the upload), only a page's.
func notionNativeBlockIconRequest(raw any, allowCustomEmoji bool) (map[string]any, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, errors.New("block icon is invalid")
	}
	presentation, err := notionPagePresentation(encoded, nil)
	if err == nil && presentation.IconManaged {
		return nil, notionContentFault(notionBlockFileIconRefused)
	}
	if err != nil || presentation.Icon == nil || presentation.IconType == nil ||
		strings.TrimSpace(*presentation.Icon) == "" ||
		presentation.IconCustomEmoji && !allowCustomEmoji {
		return nil, errors.New("block icon is invalid")
	}
	if presentation.IconCustomEmoji {
		return map[string]any{
			"customEmojiId": *presentation.Icon,
			"iconType":      "custom_emoji",
		}, nil
	}
	iconType := *presentation.IconType
	if iconType == "image" {
		iconType = "external"
	}
	return map[string]any{"icon": *presentation.Icon, "iconType": iconType}, nil
}

// normalizeNotionURLBlockMutation takes a bookmark's or an embed's address as
// Notion's API does (notion_block_link_value.go): a new one any text as
// given, a changed one an address it takes, "" leaving the one it has. A file
// address Leafen gave is not one to keep.
func normalizeNotionURLBlockMutation(
	blockType string,
	incoming map[string]any,
	current map[string]any,
	update bool,
) (string, map[string]any, *[]graphTextSpan, error) {
	if err := allowOnlyNotionBlockKeys(incoming, "url", "caption"); err != nil {
		return "", nil, nil, err
	}
	if rawURL, specified := incoming["url"]; specified {
		url, ok := rawURL.(string)
		switch {
		case !ok || notionHostedFileURL(url):
			return "", nil, nil, errors.New("block url is invalid")
		case update && url == "":
		case update && !notionLinkTaken(url):
			return "", nil, nil, notionContentFault(notionLinkRefusedOnUpdate(url))
		default:
			current["url"] = url
		}
	}
	if _, ok := current["url"].(string); !ok {
		current["url"] = ""
	}
	if rawCaption, specified := incoming["caption"]; specified {
		caption, err := notionNativeCaptionRequest(rawCaption)
		if err != nil {
			return "", nil, nil, err
		}
		current["caption"] = caption
	}
	return blockType, current, nil, nil
}

func allowOnlyNotionBlockKeys(content map[string]any, allowed ...string) error {
	accepted := map[string]struct{}{}
	for _, key := range allowed {
		accepted[key] = struct{}{}
	}
	for key := range content {
		if _, ok := accepted[key]; !ok {
			return errors.New("block content contains an unsupported field")
		}
	}
	return nil
}

func stringNotionBlockValue(value any) string {
	text, _ := value.(string)
	return text
}

// closeNewToggle closes a toggle written from outside — the API, an agent's
// Markdown or an import — as Notion shows one closed to whoever hasn't
// opened it and keeps no open state to give (seen 2026-10-31: a toggle and
// a toggle heading made through its API both closed).
func closeNewToggle(blockType string, content map[string]any) {
	switch blockType {
	case graphBlockTypeToggle, graphBlockTypeToggleHeading1, graphBlockTypeToggleHeading2,
		graphBlockTypeToggleHeading3, graphBlockTypeToggleHeading4:
		content["collapsed"] = true
	}
}

// closeNewToggleContent closes a new toggle whose content is kept encoded.
func closeNewToggleContent(blockType string, content json.RawMessage) (json.RawMessage, error) {
	decoded := map[string]any{}
	if len(content) > 0 && json.Unmarshal(content, &decoded) != nil {
		return content, nil
	}
	before := len(decoded)
	closeNewToggle(blockType, decoded)
	if len(decoded) == before {
		return content, nil
	}
	return json.Marshal(decoded)
}
