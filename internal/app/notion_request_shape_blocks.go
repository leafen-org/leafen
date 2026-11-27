package app

import (
	"encoding/json"
	"regexp"
)

// The block shapes Notion checks a request against (seen 2026-11-10). A new
// block is one type's object with nothing else but the keys of a block
// object it may repeat, which Notion leaves; its children are new blocks a
// level down, and a child's child holds none. An update names exactly one
// type or only in_trash. Notion lists the types in these orders when none
// fits.

// notionCreateBlockTypes are the types a new block may be: at the top, as a
// block's child, and as a child's child.
var notionCreateBlockTypes = [3][]string{{
	"embed", "bookmark", "image", "video", "pdf", "file", "audio", "code", "equation", "divider",
	"breadcrumb", "tab", "table_of_contents", "link_to_page", "table_row", "ai_block", "meeting_notes",
	"custom_block", "table", "column_list", "column", "heading_1", "heading_2", "heading_3", "heading_4",
	"paragraph", "bulleted_list_item", "numbered_list_item", "quote", "to_do", "toggle", "template",
	"callout", "synced_block",
}, {
	"embed", "bookmark", "image", "video", "pdf", "file", "audio", "code", "equation", "divider",
	"breadcrumb", "tab", "table_of_contents", "link_to_page", "table_row", "ai_block", "meeting_notes",
	"custom_block", "heading_1", "heading_2", "heading_3", "heading_4", "paragraph", "bulleted_list_item",
	"numbered_list_item", "quote", "table", "to_do", "toggle", "template", "callout", "synced_block",
}, {
	"embed", "bookmark", "image", "video", "pdf", "file", "audio", "code", "equation", "divider",
	"breadcrumb", "tab", "table_of_contents", "link_to_page", "table_row", "ai_block", "meeting_notes",
	"custom_block", "heading_1", "heading_2", "heading_3", "heading_4", "paragraph", "bulleted_list_item",
	"numbered_list_item", "quote", "to_do", "toggle", "template", "callout", "synced_block",
}}

var notionUpdateBlockTypes = []string{
	"embed", "bookmark", "image", "video", "pdf", "file", "audio", "code", "equation", "divider",
	"breadcrumb", "tab", "table_of_contents", "link_to_page", "table_row", "ai_block", "meeting_notes",
	"custom_block", "heading_1", "heading_2", "heading_3", "heading_4", "paragraph", "bulleted_list_item",
	"numbered_list_item", "quote", "to_do", "toggle", "template", "callout", "synced_block", "table", "column",
}

// The types whose update needs its rich_text, as a new block of a type
// written as rich text does.
var notionUpdateNeedsRichText = map[string]bool{
	"bulleted_list_item": true, "numbered_list_item": true, "quote": true, "toggle": true, "template": true,
	"heading_1": true, "heading_2": true, "heading_3": true, "heading_4": true,
}

// notionBlockUUIDText is an ID a new block's id takes: a UUID of version 4
// or 8 written with its dashes, or the nil or max UUID (seen 2026-11-10:
// "4141fd67-9738-8000-0000-…", version 7 and the 32 digits alone are not).
var notionBlockUUIDText = regexp.MustCompile(`^(?i:[0-9a-f]{8}-[0-9a-f]{4}-[48][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}` +
	`|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$`)

// A rich text list is a hundred items at most, each of
// notionRichTextItem (notion_request_shape_properties.go).
var (
	notionRichTextItemShape = notionRichTextItem(false)
	notionRichTextShape     = notionArrayOf(notionRichTextItemShape).atMostItems(100)
	notionNewBlockShapes    = notionNewBlockLevels()
	notionBlockUpdateShape  = notionBlockUpdate()
)

// notionNewBlockEchoedKeys are the keys of a block object a new block may
// repeat beside its type, which the block code leaves.
var notionNewBlockEchoedKeys = []string{
	"object", "id", "created_time", "created_by", "last_edited_time", "last_edited_by", "has_children", "in_trash", "parent",
}

// notionNewBlock is one new block of blockType: its type's object, then the
// keys of a block object it may repeat, in the order Notion checks them, and
// no other key.
func notionNewBlock(blockType string, content *notionShape) *notionShape {
	moment := notionISODateOf("a valid ISO 8601 date-time string").opt()
	anyObject := shapeOf(notionObject).opt()
	return notionObjectOf(true,
		notionField(blockType, content),
		notionField("type", notionEnumOf(blockType).opt()),
		notionField("object", notionEnumOf("block").opt()),
		notionField("id", shapeOf(notionBlockUUID).opt()),
		notionField("created_time", moment),
		notionField("created_by", anyObject),
		notionField("last_edited_time", moment),
		notionField("last_edited_by", anyObject),
		notionField("has_children", shapeOf(notionBoolean).opt()),
		notionField("in_trash", shapeOf(notionBoolean).opt()),
		notionField("parent", anyObject),
	)
}

// notionNewBlockLevels are the new blocks a request may hold at each level,
// a level's types' children those of the level below.
func notionNewBlockLevels() [3]*notionShape {
	var levels [3]*notionShape
	for level := len(levels) - 1; level >= 0; level-- {
		types := notionCreateBlockTypes[level]
		branches := make([]*notionShape, len(types))
		for index, blockType := range types {
			branches[index] = notionNewBlock(blockType, notionNewBlockContent(blockType, level, levels))
		}
		levels[level] = notionUnionOf(branches...)
	}
	return levels
}

// notionNewBlockContent is what a new block's type's object holds
// (notionBlockContent), its children the next level's blocks. A column
// list's children are its columns, a table's its rows, a tab's its
// paragraphs; a column's are a child's even at the top.
func notionNewBlockContent(blockType string, level int, levels [3]*notionShape) *notionShape {
	children := func(below int) *notionShape {
		if below >= len(levels) {
			return nil
		}
		return notionArrayOf(levels[below])
	}
	switch blockType {
	case "column_list":
		column := notionNewBlock("column", notionNewBlockContent("column", level, levels))
		return notionBlockContent(blockType, notionArrayOf(column).atLeastItems(2), false)
	case "column":
		return notionBlockContent(blockType, children(1), false)
	case "table":
		row := notionNewBlock("table_row", notionBlockContent("table_row", nil, false))
		return notionBlockContent(blockType, notionArrayOf(row).atLeastItems(1), false)
	case "tab":
		if level+1 >= len(levels) {
			return notionBlockContent(blockType, nil, false)
		}
		paragraph := notionNewBlock("paragraph", notionNewBlockContent("paragraph", level+1, levels))
		return notionBlockContent(blockType, notionArrayOf(paragraph).atLeastItems(1), false)
	}
	below := children(level + 1)
	if below != nil {
		below = below.opt()
	}
	return notionBlockContent(blockType, below, false)
}

// notionBlockUpdateContent is what an update's type's object holds
// (notionBlockContent).
func notionBlockUpdateContent(blockType string) *notionShape {
	return notionBlockContent(blockType, nil, true)
}

// notionBlockUpdateTakenContent is an update's content with only the keys
// its type's object names: a paragraph's, a to-do's and a callout's take
// other keys, which Notion leaves (seen 2026-11-10), as Leafen does.
func notionBlockUpdateTakenContent(blockType string, raw json.RawMessage) json.RawMessage {
	shape := notionBlockUpdateContent(blockType)
	content := map[string]json.RawMessage{}
	if shape.kind != notionObject || shape.closed || json.Unmarshal(raw, &content) != nil {
		return raw
	}
	taken := make(map[string]json.RawMessage, len(shape.fields))
	for _, field := range shape.fields {
		if value, given := content[field.name]; given {
			taken[field.name] = value
		}
	}
	encoded, err := json.Marshal(taken)
	if err != nil {
		return raw
	}
	return encoded
}

// notionBlockUpdate is a block update: exactly one type's object, or only
// in_trash.
func notionBlockUpdate() *notionShape {
	block := func(fields ...notionShapeField) *notionShape {
		return notionObjectOf(true, append(fields,
			notionField("type", shapeOf(notionAny)),
			notionField("object", shapeOf(notionAny)),
			notionField("id", shapeOf(notionAny)),
			notionField("in_trash", shapeOf(notionBoolean).opt()),
		)...)
	}
	branches := make([]*notionShape, 0, len(notionUpdateBlockTypes)+1)
	for _, blockType := range notionUpdateBlockTypes {
		branches = append(branches, block(notionField(blockType, notionBlockUpdateContent(blockType))))
	}
	return notionUnionOf(append(branches, block())...)
}

// notionBlocksShape is a list of new top-level blocks, a hundred at most.
func notionBlocksShape() *notionShape {
	return notionArrayOf(notionNewBlockShapes[0]).atMostItems(100)
}
